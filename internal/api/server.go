// Package api hosts the HTTP API foundation: a chi router, the huma
// OpenAPI 3.1 adapter mounted at /api/v1, and optional mount points
// for the MCP and WebUI sub-handlers. Business handlers, the auth
// middleware, the canonical error envelope, and the embedded JSON
// schemas land in subsequent iterations; this file only stands up the
// skeleton.
//
// Layer rules: api may import internal/services and stdlib. It must
// not import any other internal package (loop, supervision, supervision/dag,
// cli, tui, mcp, executor adapters, or git adapters).
package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/fgmacedo/buchecha/internal/api/handlers"
	"github.com/fgmacedo/buchecha/internal/services"
)

// shutdownGracePeriod bounds how long Listen waits for in-flight
// requests to drain after ctx is cancelled before forcing close.
const shutdownGracePeriod = 5 * time.Second

// Mounts collects the optional sub-handlers a Server can host
// alongside the /api/v1 subtree. Both handler fields are nil-safe; a
// nil handler means the corresponding mount point is not registered.
type Mounts struct {
	// MCP is the MCP HTTP handler mounted at /mcp/ when non-nil.
	MCP http.Handler
	// MCPAuth, when non-nil, wraps MCP with a path-scoped auth
	// middleware. The composition root supplies it so the api package
	// does not import internal/supervision/dag or internal/mcp; see
	// MCPAuth in this package for the canonical implementation. nil
	// leaves /mcp/* unauthenticated.
	MCPAuth func(http.Handler) http.Handler
	// WebUI is the embedded WebUI handler mounted at / when non-nil.
	// When the Server has an auth token set via WithAuth, the WebUI
	// mount is wrapped with SessionAuth so the dashboard URL printed
	// by the run banner ("/?t=<token>") sets the session cookie and
	// redirects to "/" with the token stripped.
	WebUI http.Handler
}

// Server is the HTTP API surface. It owns the chi router, the huma
// adapter, and the lifecycle of the underlying http.Server.
//
// Construction uses the two-step pattern: New(svc) returns a Server
// with no optional mounts, and WithMounts attaches them. Both calls
// return a *Server so they can be chained, e.g.
//
//	s := api.New(svc).WithMounts(api.Mounts{MCP: mcp, WebUI: webui})
//
// The svc handle is reserved for the business handlers wired in P3;
// this iteration registers no operation that consumes it, so callers
// may pass nil while the foundation is being assembled.
type Server struct {
	svc          *services.Services
	mounts       Mounts
	authToken    string
	humaAPI      huma.API
	apiRouter    chi.Router
	sseHeartbeat time.Duration
}

// New builds a Server bound to svc. svc may be nil during the
// foundation iteration; subsequent iterations will require a non-nil
// value when business handlers are registered.
func New(svc *services.Services) *Server {
	return &Server{svc: svc}
}

// WithMounts attaches the optional sub-handler mounts and returns the
// receiver so calls can be chained. Passing the zero Mounts value is
// equivalent to leaving mounts unset.
func (s *Server) WithMounts(m Mounts) *Server {
	s.mounts = m
	return s
}

// WithAuth installs the per-run session token used by the SessionAuth
// middleware on the /api/v1 subtree and on the WebUI mount when
// present. An empty token leaves the foundation usable in tests that
// exercise the router without auth. The MCP mount has its own
// path-scoped auth supplied via Mounts.MCPAuth; this setter does not
// touch it.
func (s *Server) WithAuth(token string) *Server {
	s.authToken = token
	return s
}

// WithSSEHeartbeat overrides the SSE handler's :heartbeat cadence.
// The production default is 15 seconds; tests use a small interval
// to assert heartbeat behaviour without long waits. Zero or negative
// values revert to the production default.
func (s *Server) WithSSEHeartbeat(d time.Duration) *Server {
	s.sseHeartbeat = d
	return s
}

// Routes builds and returns the configured chi.Router. The /api/v1
// subtree (including the huma OpenAPI 3.1 document at
// /api/v1/openapi.json) is always registered. Optional MCP and WebUI
// handlers from Mounts attach at /mcp/ and / respectively when
// non-nil.
func (s *Server) Routes() http.Handler {
	root := chi.NewRouter()
	root.Use(RequestContext)

	// /api/v1 subtree backed by the huma adapter. We mount a
	// dedicated sub-router so the huma routes live below /api/v1
	// without leaking onto the root.
	apiRouter := chi.NewRouter()
	if s.authToken != "" {
		apiRouter.Use(SessionAuth(s.authToken))
	}
	cfg := huma.DefaultConfig("bcc", APIVersion)
	// Disable huma's auto-mounted OpenAPI, docs, and schemas routes.
	// T3.2 serves the embedded openapi.json verbatim, T3.3 owns the
	// /schemas/{name} subtree, and bcc does not expose a docs UI in
	// V1. Leaving huma's defaults active would either duplicate
	// chi routes (panic) or shadow ours.
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	s.humaAPI = humachi.New(apiRouter, cfg)
	s.apiRouter = apiRouter
	handlers.Register(s.humaAPI, apiRouter, s.svc, handlers.Deps{
		APIVersion:    APIVersion,
		BinaryVersion: BinaryVersion(),
		OpenAPIJSON:   OpenAPIJSON,
		LoadSchema:    LoadSchema,
		WriteError:    WriteError,
		HumaError:     HumaServiceError,
		NewSSEEmitter: func(w http.ResponseWriter) handlers.SSEEmitter {
			ew, err := NewSSEWriter(w)
			if err != nil {
				return nil
			}
			return ew
		},
		SetSSEHeaders:     SetSSEHeaders,
		HeartbeatInterval: s.sseHeartbeat,
	})
	root.Mount("/api/"+APIVersion, apiRouter)

	if s.mounts.MCP != nil {
		mcpHandler := s.mounts.MCP
		if s.mounts.MCPAuth != nil {
			mcpHandler = s.mounts.MCPAuth(mcpHandler)
		}
		root.Mount("/mcp", mcpHandler)
	}
	if s.mounts.WebUI != nil {
		webuiHandler := s.mounts.WebUI
		if s.authToken != "" {
			webuiHandler = SessionAuth(s.authToken)(webuiHandler)
		}
		root.Mount("/", webuiHandler)
	}

	return root
}

// HumaAPI returns the huma.API constructed by the most recent
// Routes() call. It is nil before the first call. Tests use it to
// register probe operations against the same registry that production
// handlers will share in P3.
func (s *Server) HumaAPI() huma.API {
	return s.humaAPI
}

// APIRouter returns the chi sub-router mounted under /api/v1. It is
// nil before the first Routes() call. Handlers that cannot flow
// through huma (notably the SSE stream) mount directly on this
// router so they inherit the auth middleware and request-context
// stamping like every other operation under the subtree.
func (s *Server) APIRouter() chi.Router {
	return s.apiRouter
}

// Services returns the services aggregator the Server was built with.
// Handlers registered against the huma adapter consume it to satisfy
// requests. Returns nil for foundation-only servers (e.g. the OpenAPI
// generator) where no business handler is expected to fire.
func (s *Server) Services() *services.Services {
	return s.svc
}

// OpenAPI returns the OpenAPI 3.1 document built by the huma adapter.
// It is the source of truth for the generator command (T2.6) and any
// future tooling that walks the schema. The first call materializes
// the router and adapter via Routes() so the document reflects the
// configured Server.
func (s *Server) OpenAPI() *huma.OpenAPI {
	if s.humaAPI == nil {
		_ = s.Routes()
	}
	return s.humaAPI.OpenAPI()
}

// Listen binds a TCP listener at bind and serves Routes() until ctx
// is cancelled. On cancellation it triggers a graceful shutdown
// bounded by shutdownGracePeriod and then returns. Any non-nil
// listen or serve error other than http.ErrServerClosed is returned
// to the caller.
//
// Listen does not start any goroutine that outlives the call: when
// it returns, the listener is fully closed.
func (s *Server) Listen(ctx context.Context, bind string) error {
	return s.ListenAndNotify(ctx, bind, nil)
}

// ListenAndNotify is Listen with a callback that fires once the
// listener has bound but before the serve loop accepts a connection.
// The callback receives the resolved address (host:port) so the
// composition root can publish it to dependencies that need a live
// URL (the MCP boot's MCPURL, the stderr banner, the smoke test).
// Pass nil for ready to fall back to Listen's behavior.
//
// ListenAndNotify does not start any goroutine that outlives the
// call: when it returns, the listener is fully closed.
func (s *Server) ListenAndNotify(ctx context.Context, bind string, ready func(addr string)) error {
	ln, err := net.Listen("tcp", bind)
	if err != nil {
		return fmt.Errorf("api: listen %s: %w", bind, err)
	}

	srv := &http.Server{
		Handler:           s.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if ready != nil {
		ready(ln.Addr().String())
	}

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
		defer cancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
		// Drain the serve goroutine; Serve returns
		// http.ErrServerClosed when Shutdown completes.
		serveErr := <-serveErrCh
		if shutdownErr != nil {
			return fmt.Errorf("api: shutdown: %w", shutdownErr)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("api: serve: %w", serveErr)
		}
		return nil
	case err := <-serveErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("api: serve: %w", err)
		}
		return nil
	}
}
