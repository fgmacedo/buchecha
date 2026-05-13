// Package mcp implements a minimal MCP-over-HTTP request handler. The
// bcc process owns a single composition root that mounts this handler
// at /mcp/ on the run-wide API listener; agent invocations (Planner,
// Briefer, Reviewer, Executor) connect to it through their per-spawn
// mcp-config.
//
// The handler is the transport. It validates the per-role connection
// name (the X-BCC-Role header) and delegates every tools/call to a
// Handler the caller wires at construction time. The handler is the
// protocol of record: schema validation, agent identity checks, scope
// enforcement, and DAG mutations all live there. The stdlib-only
// transport keeps no MCP semantics beyond JSON-RPC framing.
//
// Bearer-token enforcement is not the transport's job: the composition
// root installs a path-scoped auth middleware on /mcp/* that validates
// the agent registry token before requests reach this handler. Tests
// that need a bearer probe install their own middleware in front of
// Routes().
//
// Tool descriptors advertised via tools/list are passed through to the
// agent CLI so it knows what to call; the descriptors do not constrain
// the handler dispatch table.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/fgmacedo/buchecha/internal/supervision/dag"
)

// RoleHeader names the request header an agent must set so the server
// can decide which role is calling. The header value is one of the
// strings configured in ServerConfig.ConnectionNames.
const RoleHeader = "X-BCC-Role"

// Tool advertises a callable name to the agent. InputSchema is a JSON
// Schema (object form) that the agent's UI may use to validate args
// before sending; it is not enforced by the server.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ToolFromDescriptor converts a dag.ToolDescriptor to a Tool at the
// MCP adapter boundary. Call this when wiring dag.Tools() output into
// a ServerConfig so the dag package stays free of the mcp import.
func ToolFromDescriptor(d dag.ToolDescriptor) Tool {
	return Tool{
		Name:        d.Name,
		Description: d.Description,
		InputSchema: d.InputSchema,
	}
}

// Handler is the protocol of record for tools/call dispatch. Implementations
// are responsible for schema validation, authorization beyond connection
// name, and any state mutation. resultText is the plain-text body the
// server returns wrapped in the MCP `content[0].text` field on success;
// returning an error converts to a JSON-RPC error response so the agent
// reads a structured failure and can correct.
type Handler interface {
	HandleCall(ctx context.Context, connectionName, methodName string, input map[string]any) (resultText string, err error)
}

// ServerConfig parameterizes New. Tools is the descriptor set
// advertised on tools/list. Handler dispatches tools/call. ConnectionNames
// is the closed set of values accepted in the X-BCC-Role header; an
// empty set rejects every authenticated request.
type ServerConfig struct {
	Tools           []Tool
	Handler         Handler
	ConnectionNames []string
}

// Server is the MCP request handler. Construct via New, mount via
// Routes. The receiver carries no listener state and is safe to share
// across goroutines for the lifetime of one composition root.
type Server struct {
	tools   []Tool
	handler Handler
	roleSet map[string]struct{}
	onCall  func(role, method string) // optional connection-level logger
}

// New validates cfg and returns a Server ready to mount via Routes.
// cfg.Handler must be non-nil and cfg.ConnectionNames must list every
// role expected to call the server.
func New(cfg ServerConfig) (*Server, error) {
	if cfg.Handler == nil {
		return nil, errors.New("mcp: nil Handler")
	}
	if len(cfg.ConnectionNames) == 0 {
		return nil, errors.New("mcp: empty ConnectionNames")
	}
	roles := make(map[string]struct{}, len(cfg.ConnectionNames))
	for _, name := range cfg.ConnectionNames {
		roles[name] = struct{}{}
	}
	return &Server{
		tools:   cfg.Tools,
		handler: cfg.Handler,
		roleSet: roles,
	}, nil
}

// SetOnCall installs an optional callback invoked after every
// authenticated MCP HTTP request (role check passed). The arguments are
// the X-BCC-Role value and the JSON-RPC method name. The callback is
// called synchronously before the response is written; keep it fast.
// Pass nil to clear. Safe to call concurrently with in-flight requests.
func (s *Server) SetOnCall(fn func(role, method string)) {
	s.onCall = fn
}

// Routes returns an http.Handler ready to mount at any prefix on a
// host router. Both `/` and `/<segment>` reach the same dispatch path
// so the handler works whether mounted with or without a trailing
// slash strip.
func (s *Server) Routes() http.Handler {
	return http.HandlerFunc(s.handle)
}

// ConnectionNames returns the registered role allow-list as a copy.
// The composition root reuses this list to drive the path-scoped MCP
// auth middleware without re-stating the allowed roles separately.
func (s *Server) ConnectionNames() []string {
	out := make([]string, 0, len(s.roleSet))
	for name := range s.roleSet {
		out = append(out, name)
	}
	return out
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErrObj struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErrObj      `json:"error,omitempty"`
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get(RoleHeader)
	if _, ok := s.roleSet[role]; !ok {
		http.Error(w, "forbidden role", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleSSE(w, r)
	case http.MethodPost:
		s.handlePost(w, r, role)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSSE keeps a server-initiated notification stream open. The bcc
// MCP server never pushes notifications; the handler holds the
// connection open until the request context cancels.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	<-r.Context().Done()
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request, role string) {
	var req rpcReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, nil, -32700, "parse error: "+err.Error())
		return
	}
	if s.onCall != nil {
		s.onCall(role, req.Method)
	}
	switch req.Method {
	case "initialize":
		writeResp(w, req.ID, map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "bcc",
				"version": "0.1.0",
			},
		})
	case "notifications/initialized", "notifications/cancelled":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		listed := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			listed = append(listed, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		writeResp(w, req.ID, map[string]any{"tools": listed})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeErr(w, req.ID, -32602, "invalid params: "+err.Error())
			return
		}
		text, err := s.handler.HandleCall(r.Context(), role, p.Name, p.Arguments)
		if err != nil {
			writeErr(w, req.ID, -32602, err.Error())
			return
		}
		writeResp(w, req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
		})
	case "ping":
		writeResp(w, req.ID, map[string]any{})
	default:
		writeErr(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

func writeResp(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rpcResp{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeErr(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rpcResp{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcErrObj{Code: code, Message: msg},
		Result:  nil,
	})
}
