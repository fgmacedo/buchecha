package handlers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/api"
	"github.com/fgmacedo/buchecha/internal/services"
	"github.com/fgmacedo/buchecha/internal/supervision"
)

// seedPrompt writes <sessionDir>/prompts/<role>.md with the supplied
// body so PromptService.Get has the same on-disk shape the run boot
// produces.
func seedPrompt(t *testing.T, sessionDir, role, body string) {
	t.Helper()
	dir := filepath.Join(sessionDir, "prompts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, role+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
}

func promptsServer(t *testing.T) (*httptest.Server, supervision.Session) {
	t.Helper()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := supervision.Session{
		ID:        "abcdef022222",
		SpecPath:  "/spec/q.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    supervision.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	sessionDir := filepath.Join(baseDir, "sessions", sess.ID)
	seedPrompt(t, sessionDir, "planner", "# planner prompt\n")
	seedPrompt(t, sessionDir, "briefer", "# briefer prompt\n")
	seedPrompt(t, sessionDir, "executor", "# executor prompt\n")
	seedPrompt(t, sessionDir, "reviewer", "# reviewer prompt\n")

	svc := services.New(services.Deps{SessionsBaseDir: baseDir})
	srv := httptest.NewServer(api.New(svc).Routes())
	t.Cleanup(srv.Close)
	return srv, sess
}

func TestPrompts_AllFourRolesServed(t *testing.T) {
	t.Parallel()
	srv, sess := promptsServer(t)

	cases := []struct {
		role     string
		wantBody string
	}{
		{"planner", "# planner prompt\n"},
		{"briefer", "# briefer prompt\n"},
		{"executor", "# executor prompt\n"},
		{"reviewer", "# reviewer prompt\n"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.role, func(t *testing.T) {
			t.Parallel()
			resp, err := http.Get(srv.URL + "/api/v1/sessions/" + sess.ID + "/prompts/" + tc.role)
			if err != nil {
				t.Fatalf("get prompt: %v", err)
			}
			t.Cleanup(func() { _ = resp.Body.Close() })

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status: got %d, want 200 (body=%s)", resp.StatusCode, body)
			}
			if got := resp.Header.Get("Content-Type"); got != "text/markdown; charset=utf-8" {
				t.Errorf("content-type: got %q, want text/markdown; charset=utf-8", got)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != tc.wantBody {
				t.Errorf("body: got %q, want %q", body, tc.wantBody)
			}
		})
	}
}

func TestPrompts_InvalidRoleReturnsBadRequest(t *testing.T) {
	t.Parallel()
	srv, sess := promptsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + sess.ID + "/prompts/operator")
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 400 (body=%s)", resp.StatusCode, body)
	}
	var env struct {
		Code services.ErrorCode `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != services.CodeInvalidRequest {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeInvalidRequest)
	}
}

func TestPrompts_UnknownSessionReturnsNotFound(t *testing.T) {
	t.Parallel()
	srv, _ := promptsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/000000000000/prompts/planner")
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 404 (body=%s)", resp.StatusCode, body)
	}
	var env struct {
		Code services.ErrorCode `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != services.CodeSessionNotFound {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeSessionNotFound)
	}
}

func TestPrompts_RoleFileMissingReturnsRoleNotFound(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := supervision.Session{
		ID:        "abcdef033333",
		SpecPath:  "/spec/r.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    supervision.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	// No prompts seeded.
	svc := services.New(services.Deps{SessionsBaseDir: baseDir})
	srv := httptest.NewServer(api.New(svc).Routes())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + sess.ID + "/prompts/executor")
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 404 (body=%s)", resp.StatusCode, body)
	}
	var env struct {
		Code services.ErrorCode `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != services.CodeRoleNotFound {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeRoleNotFound)
	}
}

// seedSpawnPrompt writes <sessionDir>/spawns/<spawn_id>.md with the
// supplied body so the HTTP handler can serve it.
func seedSpawnPromptHTTP(t *testing.T, sessionDir, spawnID, body string) {
	t.Helper()
	dir := filepath.Join(sessionDir, "spawns")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir spawns: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, spawnID+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write spawn prompt: %v", err)
	}
}

func TestSpawnPrompts_HappyPath(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := supervision.Session{
		ID:        "abcdef044444",
		SpecPath:  "/spec/s.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    supervision.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	sessionDir := filepath.Join(baseDir, "sessions", sess.ID)

	spawnID := "0123456789abcdef"
	spawnBody := "# executor spawn prompt\n## System prompt\nYou are an assistant.\n"
	seedSpawnPromptHTTP(t, sessionDir, spawnID, spawnBody)

	svc := services.New(services.Deps{SessionsBaseDir: baseDir})
	srv := httptest.NewServer(api.New(svc).Routes())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + sess.ID + "/spawns/" + spawnID + "/prompt")
	if err != nil {
		t.Fatalf("get spawn prompt: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200 (body=%s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Errorf("content-type: got %q, want text/markdown; charset=utf-8", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != spawnBody {
		t.Errorf("body: got %q, want %q", body, spawnBody)
	}
}

func TestSpawnPrompts_MalformedSpawnIDReturnsBadRequest(t *testing.T) {
	t.Parallel()
	srv, sess := promptsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + sess.ID + "/spawns/INVALID/prompt")
	if err != nil {
		t.Fatalf("get spawn prompt: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 400 (body=%s)", resp.StatusCode, body)
	}
	var env struct {
		Code services.ErrorCode `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != services.CodeInvalidRequest {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeInvalidRequest)
	}
}

func TestSpawnPrompts_UnknownSessionReturnsNotFound(t *testing.T) {
	t.Parallel()
	srv, _ := promptsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/000000000000/spawns/0123456789abcdef/prompt")
	if err != nil {
		t.Fatalf("get spawn prompt: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 404 (body=%s)", resp.StatusCode, body)
	}
	var env struct {
		Code services.ErrorCode `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != services.CodeSessionNotFound {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeSessionNotFound)
	}
}

func TestSpawnPrompts_UnknownSpawnReturnsRoleNotFound(t *testing.T) {
	t.Parallel()
	srv, sess := promptsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + sess.ID + "/spawns/99999999999999aa/prompt")
	if err != nil {
		t.Fatalf("get spawn prompt: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 404 (body=%s)", resp.StatusCode, body)
	}
	var env struct {
		Code services.ErrorCode `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != services.CodeRoleNotFound {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeRoleNotFound)
	}
}
