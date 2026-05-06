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

// seedBriefing mirrors internal/services.seedBriefing: writes the
// briefings/<iter>.json metadata pair plus the
// briefings/<iter>.prompt.md content so BriefingService.Get has the
// on-disk shape it expects. The
// mtime offset controls attempt ordering on filesystems that round to
// the second.
func seedBriefing(t *testing.T, sessionDir, iterationID, phaseID, markdown string, mtimeOffset time.Duration) {
	t.Helper()
	briefingsDir := filepath.Join(sessionDir, "briefings")
	if err := os.MkdirAll(briefingsDir, 0o755); err != nil {
		t.Fatalf("mkdir briefings: %v", err)
	}
	body := []byte(`{"iteration_id":"` + iterationID + `","phase_id":"` + phaseID + `"}`)
	jsonPath := filepath.Join(briefingsDir, iterationID+".json")
	if err := os.WriteFile(jsonPath, body, 0o644); err != nil {
		t.Fatalf("write briefing json: %v", err)
	}
	at := time.Now().Add(mtimeOffset)
	if err := os.Chtimes(jsonPath, at, at); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(briefingsDir, iterationID+".prompt.md"), []byte(markdown), 0o644); err != nil {
		t.Fatalf("write prompt.md: %v", err)
	}
}

func briefingsServer(t *testing.T) (*httptest.Server, director.Session) {
	t.Helper()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := director.Session{
		ID:        "abcdef011111",
		SpecPath:  "/spec/p.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	sessionDir := filepath.Join(baseDir, "sessions", sess.ID)
	seedBriefing(t, sessionDir, "P1-100", "P1", "# attempt 1\n", -2*time.Second)
	seedBriefing(t, sessionDir, "P1-200", "P1", "# attempt 2\n", -1*time.Second)

	svc := services.New(services.Deps{SessionsBaseDir: baseDir})
	srv := httptest.NewServer(api.New(svc).Routes())
	t.Cleanup(srv.Close)
	return srv, sess
}

func TestBriefings_HappyPath(t *testing.T) {
	t.Parallel()
	srv, sess := briefingsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + sess.ID + "/briefings/P1/2")
	if err != nil {
		t.Fatalf("get briefing: %v", err)
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
	if string(body) != "# attempt 2\n" {
		t.Errorf("body: got %q, want %q", body, "# attempt 2\n")
	}
}

func TestBriefings_PhaseNotFound(t *testing.T) {
	t.Parallel()
	srv, sess := briefingsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + sess.ID + "/briefings/P-unknown/1")
	if err != nil {
		t.Fatalf("get briefing: %v", err)
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
	if env.Code != services.CodePhaseNotFound {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodePhaseNotFound)
	}
}

func TestBriefings_AttemptNotFound(t *testing.T) {
	t.Parallel()
	srv, sess := briefingsServer(t)

	resp, err := http.Get(srv.URL + "/api/v1/sessions/" + sess.ID + "/briefings/P1/9")
	if err != nil {
		t.Fatalf("get briefing: %v", err)
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
	if env.Code != services.CodeAttemptNotFound {
		t.Errorf("envelope code: got %q, want %q", env.Code, services.CodeAttemptNotFound)
	}
}
