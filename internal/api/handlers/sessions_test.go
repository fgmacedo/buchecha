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
	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/services"
)

// writeManifest mirrors internal/services/sessions_test.writeManifest
// so handler tests can seed archived sessions without re-exporting the
// helper. The shape is the canonical director.Session JSON form.
func writeManifest(t *testing.T, baseDir string, sess director.Session) {
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

// twoSessionsServer returns a Server backed by services.New with one
// archived session and one live session seeded under a fresh temp dir.
// Tests that exercise the sessions handlers reuse it so the on-disk
// fixture stays consistent.
func twoSessionsServer(t *testing.T) (*httptest.Server, director.Session, director.Session) {
	t.Helper()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	archived := director.Session{
		ID:        "111111111111",
		SpecPath:  "/spec/a.md",
		SpecHash:  "h",
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Hour),
		Status:    director.SessionDone,
	}
	live := director.Session{
		ID:        "222222222222",
		SpecPath:  "/spec/b.md",
		SpecHash:  "h",
		CreatedAt: now.Add(-30 * time.Minute),
		UpdatedAt: now.Add(-20 * time.Minute),
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, archived)
	writeManifest(t, baseDir, live)

	store, err := director.OpenSession(baseDir, live.ID)
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	svc := services.New(services.Deps{
		SessionsBaseDir: baseDir,
		SessionStore:    store,
	})
	srv := httptest.NewServer(api.New(svc).Routes())
	t.Cleanup(srv.Close)
	return srv, archived, live
}

func TestSessions_ListIncludesLiveAndArchived(t *testing.T) {
	t.Parallel()

	srv, archived, live := twoSessionsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions")
	if err != nil {
		t.Fatalf("get sessions: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	var body struct {
		Sessions []services.SessionMeta `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Sessions) != 2 {
		t.Fatalf("len(sessions): got %d, want 2 (got %v)", len(body.Sessions), body.Sessions)
	}
	ids := []string{body.Sessions[0].ID, body.Sessions[1].ID}
	if ids[0] != live.ID || ids[1] != archived.ID {
		t.Fatalf("ids: got %v, want [%q %q]", ids, live.ID, archived.ID)
	}
	if body.Sessions[0].Status != string(director.SessionRunning) {
		t.Errorf("live status: got %q, want %q", body.Sessions[0].Status, director.SessionRunning)
	}
	if body.Sessions[1].Status != string(director.SessionDone) {
		t.Errorf("archived status: got %q, want %q", body.Sessions[1].Status, director.SessionDone)
	}
}

func TestSessions_GetLive(t *testing.T) {
	t.Parallel()
	srv, _, live := twoSessionsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + live.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var meta services.SessionMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if meta.ID != live.ID {
		t.Fatalf("ID: got %q, want %q", meta.ID, live.ID)
	}
	if meta.Status != string(director.SessionRunning) {
		t.Errorf("status: got %q, want %q", meta.Status, director.SessionRunning)
	}
}

func TestSessions_GetArchived(t *testing.T) {
	t.Parallel()
	srv, archived, _ := twoSessionsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + archived.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var meta services.SessionMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if meta.ID != archived.ID {
		t.Fatalf("ID: got %q, want %q", meta.ID, archived.ID)
	}
	if meta.Status != string(director.SessionDone) {
		t.Errorf("status: got %q, want %q", meta.Status, director.SessionDone)
	}
}

func TestSessions_GetUnknownReturnsNotFound(t *testing.T) {
	t.Parallel()
	srv, _, _ := twoSessionsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/000000000000")
	if err != nil {
		t.Fatalf("get unknown: %v", err)
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
		t.Fatalf("decode: %v", err)
	}
	if env.Code != services.CodeSessionNotFound {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeSessionNotFound)
	}
}
