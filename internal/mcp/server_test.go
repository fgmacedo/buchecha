package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingHandler captures the dispatched calls in order so tests can
// assert which (connection, method, input) triples reached the
// Handler.HandleCall fan-in. callsResultText replies to every call;
// callsErr, when non-nil, is returned to drive the JSON-RPC error path.
type recordingHandler struct {
	mu              sync.Mutex
	calls           []handlerCall
	callsResultText string
	callsErr        error
}

type handlerCall struct {
	Connection string
	Method     string
	Input      map[string]any
}

func (h *recordingHandler) HandleCall(_ context.Context, conn, method string, input map[string]any) (string, error) {
	h.mu.Lock()
	h.calls = append(h.calls, handlerCall{Connection: conn, Method: method, Input: input})
	h.mu.Unlock()
	if h.callsErr != nil {
		return "", h.callsErr
	}
	if h.callsResultText == "" {
		return "ok", nil
	}
	return h.callsResultText, nil
}

func (h *recordingHandler) Calls() []handlerCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]handlerCall, len(h.calls))
	copy(out, h.calls)
	return out
}

// newTestServer builds an *httptest.Server fronting the MCP handler.
// The composition root in production installs a path-scoped bearer
// middleware in front of Routes(); the server itself does not validate
// bearer tokens. Tests drive Routes() directly so assertions on the
// transport (role header, JSON-RPC framing, dispatch) stay isolated
// from the auth middleware.
func newTestServer(t *testing.T) (*httptest.Server, *recordingHandler) {
	t.Helper()
	h := &recordingHandler{}
	srv, err := New(ServerConfig{
		Tools: []Tool{
			{Name: "task_started", Description: "begin a unit", InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":      map[string]any{"type": "string"},
					"summary": map[string]any{"type": "string"},
				},
				"required": []string{"id", "summary"},
			}},
			{Name: "task_completed", Description: "end a unit"},
		},
		Handler:         h,
		ConnectionNames: []string{"bcc-executor", "bcc-planner"},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts, h
}

// post sends a JSON-RPC request and returns the parsed response. role
// becomes the X-BCC-Role header value; pass empty to omit the header.
func post(t *testing.T, ts *httptest.Server, body any, role string) *http.Response {
	t.Helper()
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(body); err != nil {
		t.Fatalf("encode: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL, buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if role != "" {
		req.Header.Set(RoleHeader, role)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func decodeResp(t *testing.T, r *http.Response) rpcResp {
	t.Helper()
	body, _ := io.ReadAll(r.Body)
	var out rpcResp
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	return out
}

func TestServer_Initialize(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := post(t, ts, map[string]any{ //nolint:bodyclose // closed via t.Cleanup in post
		"jsonrpc": "2.0",
		"id":      0,
		"method":  "initialize",
		"params":  map[string]any{},
	}, "bcc-executor")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	r := decodeResp(t, resp)
	if r.Error != nil {
		t.Fatalf("error: %+v", r.Error)
	}
	got, _ := r.Result.(map[string]any)
	if got["protocolVersion"] != "2025-03-26" {
		t.Errorf("protocolVersion = %v", got["protocolVersion"])
	}
	srvInfo, _ := got["serverInfo"].(map[string]any)
	if srvInfo["name"] != "bcc" {
		t.Errorf("serverInfo.name = %v", srvInfo["name"])
	}
}

func TestServer_ToolsList(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := post(t, ts, map[string]any{ //nolint:bodyclose // closed via t.Cleanup in post
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	}, "bcc-executor")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	r := decodeResp(t, resp)
	if r.Error != nil {
		t.Fatalf("error: %+v", r.Error)
	}
	got, _ := r.Result.(map[string]any)
	tools, _ := got["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools count = %d, want 2", len(tools))
	}
	names := make([]string, 0, len(tools))
	for _, item := range tools {
		m, _ := item.(map[string]any)
		names = append(names, m["name"].(string))
	}
	want := []string{"task_started", "task_completed"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("tool[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestServer_ToolsCall_DispatchesToHandler(t *testing.T) {
	ts, h := newTestServer(t)
	resp := post(t, ts, map[string]any{ //nolint:bodyclose // closed via t.Cleanup in post
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "task_started",
			"arguments": map[string]any{"id": "P1.1", "summary": "begin"},
		},
	}, "bcc-executor")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	r := decodeResp(t, resp)
	if r.Error != nil {
		t.Fatalf("error: %+v", r.Error)
	}
	got, _ := r.Result.(map[string]any)
	content, _ := got["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content count = %d", len(content))
	}
	first, _ := content[0].(map[string]any)
	if first["text"] != "ok" {
		t.Errorf("text = %v, want ok", first["text"])
	}
	calls := h.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].Connection != "bcc-executor" {
		t.Errorf("call connection = %q, want bcc-executor", calls[0].Connection)
	}
	if calls[0].Method != "task_started" {
		t.Errorf("call method = %q", calls[0].Method)
	}
	if calls[0].Input["id"] != "P1.1" || calls[0].Input["summary"] != "begin" {
		t.Errorf("call input = %v", calls[0].Input)
	}
}

func TestServer_ToolsCall_HandlerErrorReturnsRPCError(t *testing.T) {
	ts, h := newTestServer(t)
	h.callsErr = errors.New("invalid sub_dag_task_ids")
	resp := post(t, ts, map[string]any{ //nolint:bodyclose // closed via t.Cleanup in post
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params":  map[string]any{"name": "task_started", "arguments": map[string]any{}},
	}, "bcc-executor")
	r := decodeResp(t, resp)
	if r.Error == nil {
		t.Fatal("expected JSON-RPC error from handler error")
	}
	if !strings.Contains(r.Error.Message, "invalid sub_dag_task_ids") {
		t.Errorf("error message = %q", r.Error.Message)
	}
}

func TestServer_RejectsUnknownRole(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := post(t, ts, map[string]any{ //nolint:bodyclose // closed via t.Cleanup in post
		"jsonrpc": "2.0",
		"id":      6,
		"method":  "initialize",
	}, "bcc-mystery")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestServer_RejectsMissingRole(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := post(t, ts, map[string]any{ //nolint:bodyclose // closed via t.Cleanup in post
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "initialize",
	}, "")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (empty role rejected)", resp.StatusCode)
	}
}

func TestServer_NotificationAccepted(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := post(t, ts, map[string]any{ //nolint:bodyclose // closed via t.Cleanup in post
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}, "bcc-executor")
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
}

func TestServer_UnknownMethodErrors(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := post(t, ts, map[string]any{ //nolint:bodyclose // closed via t.Cleanup in post
		"jsonrpc": "2.0",
		"id":      8,
		"method":  "resources/list",
	}, "bcc-executor")
	r := decodeResp(t, resp)
	if r.Error == nil || r.Error.Code != -32601 {
		t.Errorf("error = %+v, want code -32601", r.Error)
	}
}

func TestServer_GetIsSSEAndClosesOnContextCancel(t *testing.T) {
	ts, _ := newTestServer(t)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Set(RoleHeader, "bcc-executor")
	req.Header.Set("Accept", "text/event-stream")
	start := time.Now()
	resp, err := client.Do(req)
	if err == nil {
		// We expect the client to time out reading the body, since the
		// server holds the connection open with no data.
		_, err = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected timeout reading SSE body")
	}
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Errorf("returned too quickly: %v", elapsed)
	}
	if resp != nil && resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
}

func TestServer_RejectsNilHandler(t *testing.T) {
	_, err := New(ServerConfig{
		Tools:           nil,
		Handler:         nil,
		ConnectionNames: []string{"bcc-executor"},
	})
	if err == nil {
		t.Fatal("expected error when Handler is nil")
	}
}

func TestServer_RejectsEmptyConnectionNames(t *testing.T) {
	_, err := New(ServerConfig{
		Tools:   nil,
		Handler: &recordingHandler{},
	})
	if err == nil {
		t.Fatal("expected error when ConnectionNames is empty")
	}
}

func TestServer_ConnectionNamesReturnsConfiguredRoles(t *testing.T) {
	srv, err := New(ServerConfig{
		Handler:         &recordingHandler{},
		ConnectionNames: []string{"a", "b", "c"},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	got := srv.ConnectionNames()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	want := map[string]bool{"a": true, "b": true, "c": true}
	for _, name := range got {
		if !want[name] {
			t.Errorf("unexpected role %q", name)
		}
	}
}
