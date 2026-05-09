package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

// withWorkingDir cd's into dir for the duration of the test and
// restores the previous wd on cleanup. The sessions subcommand reads
// `.bcc/sessions/...` relative to the cwd, so tests need to anchor the
// search at a TempDir-controlled location.
func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestRunSessionsList_TextWhenEmpty(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)

	var w bytes.Buffer
	if err := runSessionsList(&w, "text"); err != nil {
		t.Fatalf("runSessionsList: %v", err)
	}
	if !strings.Contains(w.String(), "no sessions") {
		t.Errorf("text output for empty list = %q, want a 'no sessions' marker", w.String())
	}
}

func TestRunSessionsList_TextWithRows(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)

	a, _, err := session.CreateSession(filepath.Join(tmp, ".bcc"), "/tmp/a.md", "h1", time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := session.CreateSession(filepath.Join(tmp, ".bcc"), "/tmp/b.md", "h2", time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	var w bytes.Buffer
	if err := runSessionsList(&w, "text"); err != nil {
		t.Fatalf("runSessionsList: %v", err)
	}
	out := w.String()
	if !strings.Contains(out, a.Session().ID) || !strings.Contains(out, b.Session().ID) {
		t.Errorf("text output missing session ids: %q", out)
	}
	// Newer session is listed first.
	idxA := strings.Index(out, a.Session().ID)
	idxB := strings.Index(out, b.Session().ID)
	if idxB > idxA {
		t.Errorf("listing not ordered by UpdatedAt desc; got A then B: %q", out)
	}
	if !strings.Contains(out, "STATUS") {
		t.Errorf("text output missing header: %q", out)
	}
}

func TestRunSessionsList_JSON(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)

	_, _, err := session.CreateSession(filepath.Join(tmp, ".bcc"), "/tmp/spec.md", "h1", time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	var w bytes.Buffer
	if err := runSessionsList(&w, "json"); err != nil {
		t.Fatalf("runSessionsList: %v", err)
	}
	var got []session.Session
	if err := json.Unmarshal(w.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, w.String())
	}
	if len(got) != 1 {
		t.Fatalf("got %d sessions, want 1", len(got))
	}
}

func TestRunSessionsShow_TextHappyPath(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)

	store, _, err := session.CreateSession(filepath.Join(tmp, ".bcc"), "/tmp/spec.md", "deadbeef", time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	var w bytes.Buffer
	if err := runSessionsShow(&w, "text", store.Session().ID); err != nil {
		t.Fatalf("runSessionsShow: %v", err)
	}
	out := w.String()
	for _, want := range []string{store.Session().ID, "/tmp/spec.md", "deadbeef", "running"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q: %s", want, out)
		}
	}
}

func TestRunSessionsShow_JSONHappyPath(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)

	store, _, err := session.CreateSession(filepath.Join(tmp, ".bcc"), "/tmp/spec.md", "deadbeef", time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	var w bytes.Buffer
	if err := runSessionsShow(&w, "json", store.Session().ID); err != nil {
		t.Fatalf("runSessionsShow: %v", err)
	}
	var got session.Session
	if err := json.Unmarshal(w.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, w.String())
	}
	if got.ID != store.Session().ID {
		t.Errorf("json id = %q, want %q", got.ID, store.Session().ID)
	}
}

func TestRunSessionsShow_MissingReturnsError(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)

	prev := ExitCode
	t.Cleanup(func() { ExitCode = prev })

	var w bytes.Buffer
	err := runSessionsShow(&w, "text", "abcdef012345")
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want it to wrap fs.ErrNotExist", err)
	}
	if ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", ExitCode)
	}
}

func TestSessionsCommand_RegisteredOnRoot(t *testing.T) {
	for _, sub := range rootCmd.Commands() {
		if sub.Name() == "sessions" {
			return
		}
	}
	t.Fatal("rootCmd does not register `sessions`")
}
