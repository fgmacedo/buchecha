package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/fgmacedo/buchecha/internal/api"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/mcp"
	"github.com/fgmacedo/buchecha/internal/services"
	"github.com/fgmacedo/buchecha/internal/supervision/dag"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

// TestRunListener_E2ESmoke boots the run-wide HTTP surface end to end:
// it builds a Services aggregate against a fixture session under a
// temp .bcc/, starts the listener, hits /api/v1/sessions with the
// session bearer, hits /mcp/ initialize with the agent bearer plus a
// registered role, and asserts cross-protocol isolation. The test
// never invokes a real agent CLI; it covers the listener wiring only.
func TestRunListener_E2ESmoke(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)

	// Seed a single archived session so /api/v1/sessions has a row to
	// return. The smoke test does not exercise the live DAG view; the
	// archived path is enough to validate the schema + auth wiring.
	archived := session.Session{
		ID:        "smoke0001smoke",
		SpecPath:  filepath.Join(tmp, "spec.md"),
		SpecHash:  "h",
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now,
		Status:    session.SessionDone,
	}
	writeSmokeManifest(t, baseDir, archived)

	svc := services.New(services.Deps{
		LoopEvents:      make(<-chan loop.Event),
		SessionsBaseDir: baseDir,
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	listener, err := startRunListener(ctx, nil, svc, nil, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startRunListener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Stop() })

	// /api/v1/sessions with the session bearer.
	apiURL := "http://" + listener.addr + "/api/v1/sessions"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	req.Header.Set("Authorization", "Bearer "+listener.sessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("api do: %v", err)
	}
	apiBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("api status: got %d, want 200 (body=%s)", resp.StatusCode, apiBody)
	}

	// Validate the body against session.schema.json. The list wraps
	// SessionMeta in a `sessions` array; pull each row out and check
	// it individually.
	schemaBytes, err := api.LoadSchema("session.schema.json")
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	rawSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("bcc:///smoke/session.schema.json", rawSchema); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	sch, err := c.Compile("bcc:///smoke/session.schema.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	var out struct {
		Sessions []json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(apiBody, &out); err != nil {
		t.Fatalf("decode api body: %v", err)
	}
	if len(out.Sessions) != 1 {
		t.Fatalf("sessions count: got %d, want 1 (body=%s)", len(out.Sessions), apiBody)
	}
	for i, raw := range out.Sessions {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("parse session[%d]: %v", i, err)
		}
		if err := sch.Validate(doc); err != nil {
			t.Fatalf("validate session[%d]: %v\nbody=%s", i, err, raw)
		}
	}

	// /mcp/ initialize with the agent bearer + role.
	mcpURL := listener.boot.MCPURL()
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, body)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+listener.boot.token())
	req2.Header.Set(mcp.RoleHeader, string(dag.RolePlanner))
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("mcp do: %v", err)
	}
	mcpBody, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("mcp status: got %d, want 200 (body=%s)", resp2.StatusCode, mcpBody)
	}
	if !strings.Contains(string(mcpBody), `"protocolVersion"`) {
		t.Errorf("mcp body missing protocolVersion: %s", mcpBody)
	}

	// Cross-protocol isolation: agent bearer on /api/v1/sessions -> 401.
	req3, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	req3.Header.Set("Authorization", "Bearer "+listener.boot.token())
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("isolation api do: %v", err)
	}
	_ = resp3.Body.Close()
	if resp3.StatusCode != http.StatusUnauthorized {
		t.Errorf("/api/v1/sessions with agent token: got %d, want 401", resp3.StatusCode)
	}

	// Cross-protocol isolation: session bearer on /mcp/ -> 401.
	body2 := strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`)
	req4, _ := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, body2)
	req4.Header.Set("Content-Type", "application/json")
	req4.Header.Set("Authorization", "Bearer "+listener.sessionToken)
	req4.Header.Set(mcp.RoleHeader, string(dag.RolePlanner))
	resp4, err := http.DefaultClient.Do(req4)
	if err != nil {
		t.Fatalf("isolation mcp do: %v", err)
	}
	_ = resp4.Body.Close()
	if resp4.StatusCode != http.StatusUnauthorized {
		t.Errorf("/mcp/ with session token: got %d, want 401", resp4.StatusCode)
	}
}

// writeSmokeManifest persists a session manifest under the test base
// directory so SessionService.List can enumerate it. Mirrors the
// helper in internal/api/integration_test.go without taking a
// dependency on test code from another package.
func writeSmokeManifest(t *testing.T, baseDir string, sess session.Session) {
	t.Helper()
	dir := filepath.Join(baseDir, "sessions", sess.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}
