package mcp_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/director/dag"
	"github.com/fgmacedo/buchecha/internal/mcp"
)

// TestEndToEnd_DAGHandlerOverHTTP exercises the real Handler over the
// HTTP transport: a Planner agent_id is registered, the X-BCC-Role
// header carries the connection name, the body carries the agent_id
// alongside a valid Plan, and the resulting audit log records the
// dispatched method.
func TestEndToEnd_DAGHandlerOverHTTP(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "mcp-log.jsonl")
	registry := dag.NewAgentRegistry(nil)
	handler := dag.NewHandlerWithOptions(nil, registry, dag.HandlerOptions{
		Audit: dag.NewAuditLog(auditPath),
	})
	srv, err := mcp.Start(mcp.ServerConfig{
		Handler:         handler,
		ConnectionNames: []string{string(dag.RolePlanner)},
	})
	if err != nil {
		t.Fatalf("start mcp: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	plannerID, err := registry.Register(dag.RolePlanner, dag.RegisterArgs{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	planBody := map[string]any{
		"goal":             "demo",
		"success_criteria": []any{"works"},
		"spec_hash":        "abc",
		"planned_at":       "2026-05-02T12:00:00Z",
		"phases": []any{
			map[string]any{
				"id":     "P1",
				"title":  "p1",
				"intent": "p1",
				"tasks": []any{
					map[string]any{
						"id":     "t1",
						"title":  "t1",
						"intent": "intent",
						"acceptance": []any{
							map[string]any{
								"id":          "a1",
								"description": "d",
								"evidence":    "diff",
							},
						},
						"status": "pending",
					},
				},
			},
		},
	}
	rpcBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": dag.MethodPlanEmit,
			"arguments": map[string]any{
				"agent_id": string(plannerID),
				"plan":     planBody,
			},
		},
	}

	body, _ := json.Marshal(rpcBody)
	req, _ := http.NewRequest(http.MethodPost, srv.URL(), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(mcp.RoleHeader, string(dag.RolePlanner))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	respBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(respBody), `\"ok\":true`) {
		t.Fatalf("response missing ok=true: %s", respBody)
	}

	if handler.Plan() == nil {
		t.Fatal("plan not stored")
	}

	if err := dag.NewAuditLog(auditPath).Close(); err != nil {
		t.Fatalf("close audit: %v", err)
	}
	// The audit log handle owned by the handler still points at the
	// open file. Read the file directly to assert the recorded entry.
	logged, err := readJSONL(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if len(logged) != 1 {
		t.Fatalf("audit lines = %d, want 1: %v", len(logged), logged)
	}
	if logged[0]["method"] != dag.MethodPlanEmit {
		t.Errorf("audit method = %v", logged[0]["method"])
	}
	if logged[0]["agent_id"] != string(plannerID) {
		t.Errorf("audit agent_id = %v", logged[0]["agent_id"])
	}
}

// TestEndToEnd_RoleHeaderRejected exercises the X-BCC-Role authz: a
// Planner agent_id presented over a Briefer connection must fail at
// the connection-name layer, before any agent_id lookup.
func TestEndToEnd_RoleHeaderRejected(t *testing.T) {
	registry := dag.NewAgentRegistry(nil)
	handler := dag.NewHandler(nil, registry)
	srv, err := mcp.Start(mcp.ServerConfig{
		Handler: handler,
		ConnectionNames: []string{
			string(dag.RolePlanner),
			string(dag.RoleBriefer),
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	plannerID, _ := registry.Register(dag.RolePlanner, dag.RegisterArgs{})
	rpcBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": dag.MethodPlanEmit,
			"arguments": map[string]any{
				"agent_id": string(plannerID),
				"plan":     map[string]any{},
			},
		},
	}
	body, _ := json.Marshal(rpcBody)
	req, _ := http.NewRequest(http.MethodPost, srv.URL(), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(mcp.RoleHeader, string(dag.RoleBriefer))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(respBody), "not allowed") {
		t.Errorf("expected connection authz rejection, got %s", respBody)
	}
}

func readJSONL(path string) ([]map[string]any, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
