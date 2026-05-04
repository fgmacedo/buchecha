// Package api hosts the HTTP API foundation: a chi router, the huma
// OpenAPI 3.1 adapter mounted at /api/v1, and optional mount points
// for the MCP and WebUI sub-handlers. Business handlers, the auth
// middleware, the canonical error envelope, and the embedded JSON
// schemas land in subsequent iterations; this file only stands up the
// skeleton.
//
// Layer rules: api may import internal/services and stdlib. It must
// not import any other internal package (loop, director, director/dag,
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

	"github.com/fgmacedo/buchecha/internal/services"
)

// shutdownGracePeriod bounds how long Listen waits for in-flight
// requests to drain after ctx is cancelled before forcing close.
const shutdownGracePeriod = 5 * time.Second

// Mounts collects the optional sub-handlers a Server can host
// alongside the /api/v1 subtree. Both fields are nil-safe; a nil
// handler means the corresponding mount point is not registered.
type Mounts struct {
	// MCP is the MCP HTTP handler mounted at /mcp/ when non-nil.
	MCP http.Handler
	// WebUI is the embedded WebUI handler mounted at / when non-nil.
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
	svc    *services.Services
	mounts Mounts
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

// Routes builds and returns the configured chi.Router. The /api/v1
// subtree (including the huma OpenAPI 3.1 document at
// /api/v1/openapi.json) is always registered. Optional MCP and WebUI
// handlers from Mounts attach at /mcp/ and / respectively when
// non-nil.
func (s *Server) Routes() http.Handler {
	root := chi.NewRouter()

	// /api/v1 subtree backed by the huma adapter. We mount a
	// dedicated sub-router so the huma routes live below /api/v1
	// without leaking onto the root.
	apiRouter := chi.NewRouter()
	humachi.New(apiRouter, huma.DefaultConfig("bcc", APIVersion))
	root.Mount("/api/"+APIVersion, apiRouter)

	if s.mounts.MCP != nil {
		root.Mount("/mcp", s.mounts.MCP)
	}
	if s.mounts.WebUI != nil {
		root.Mount("/", s.mounts.WebUI)
	}

	return root
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
	ln, err := net.Listen("tcp", bind)
	if err != nil {
		return fmt.Errorf("api: listen %s: %w", bind, err)
	}

	srv := &http.Server{
		Handler:           s.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
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
