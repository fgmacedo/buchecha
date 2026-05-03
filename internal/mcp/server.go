// Package mcp implements a minimal MCP-over-HTTP server bound to
// loopback. The bcc process runs a single instance of this server per
// `bcc run`; agent invocations (Planner, Briefer, Reviewer, Executor)
// connect to it through their per-spawn mcp-config.
//
// The server is the transport. It validates the bearer token, validates
// the per-role connection name (the X-BCC-Role header), and delegates
// every tools/call to a Handler the caller wires at Start time. The
// handler is the protocol of record: schema validation, agent identity
// checks, scope enforcement, and DAG mutations all live there. The
// stdlib-only server keeps no MCP semantics beyond JSON-RPC framing.
//
// Tool descriptors advertised via tools/list are passed through to the
// agent CLI so it knows what to call; the descriptors do not constrain
// the handler dispatch table.
package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
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

// Handler is the protocol of record for tools/call dispatch. Implementations
// are responsible for schema validation, authorization beyond connection
// name, and any state mutation. resultText is the plain-text body the
// server returns wrapped in the MCP `content[0].text` field on success;
// returning an error converts to a JSON-RPC error response so the agent
// reads a structured failure and can correct.
type Handler interface {
	HandleCall(ctx context.Context, connectionName, methodName string, input map[string]any) (resultText string, err error)
}

// ServerConfig parameterizes Start. Tools is the descriptor set
// advertised on tools/list. Handler dispatches tools/call. ConnectionNames
// is the closed set of values accepted in the X-BCC-Role header; an
// empty set rejects every authenticated request.
type ServerConfig struct {
	Tools           []Tool
	Handler         Handler
	ConnectionNames []string
}

// Server is the live MCP HTTP endpoint. Construct via Start, dispose
// via Close. Safe to access URL and Token concurrently.
type Server struct {
	tools   []Tool
	handler Handler
	roleSet map[string]struct{}
	token   string
	addr    string
	httpSrv *http.Server

	wg     sync.WaitGroup
	closed chan struct{}
}

// Start binds 127.0.0.1:0, generates a 32-byte hex bearer token, and
// serves until Close. Returns the running server. Caller must Close.
//
// cfg.Handler must be non-nil and cfg.ConnectionNames must list every
// role expected to call the server.
func Start(cfg ServerConfig) (*Server, error) {
	if cfg.Handler == nil {
		return nil, errors.New("mcp: nil Handler")
	}
	if len(cfg.ConnectionNames) == 0 {
		return nil, errors.New("mcp: empty ConnectionNames")
	}
	tok, err := newToken()
	if err != nil {
		return nil, fmt.Errorf("mcp token: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("mcp listen: %w", err)
	}
	roles := make(map[string]struct{}, len(cfg.ConnectionNames))
	for _, name := range cfg.ConnectionNames {
		roles[name] = struct{}{}
	}
	s := &Server{
		tools:   cfg.Tools,
		handler: cfg.Handler,
		roleSet: roles,
		token:   tok,
		addr:    ln.Addr().String(),
		closed:  make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handle)
	s.httpSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = err
		}
	}()
	return s, nil
}

// URL returns the http://host:port/mcp endpoint the agent should be
// configured with.
func (s *Server) URL() string {
	return "http://" + s.addr + "/mcp"
}

// Token returns the bearer token the agent must present in the
// Authorization header.
func (s *Server) Token() string {
	return s.token
}

// Close stops accepting new connections and waits for in-flight
// handlers to return. Idempotent.
func (s *Server) Close() error {
	select {
	case <-s.closed:
		return nil
	default:
		close(s.closed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.httpSrv.Shutdown(ctx)
	s.wg.Wait()
	return err
}

func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
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
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
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
	switch req.Method {
	case "initialize":
		writeResp(w, req.ID, map[string]any{
			"protocolVersion": "2025-03-26",
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
	})
}
