package supervision

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListSessions_EmptyBaseReturnsNil(t *testing.T) {
	got, err := ListSessions(t.TempDir())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil slice, got %+v", got)
	}
}

func TestListSessions_OrdersByUpdatedAtDesc(t *testing.T) {
	base := t.TempDir()
	_, oldSess, err := CreateSession(base, "/tmp/spec.md", "h1", time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateSession old: %v", err)
	}
	store2, newSess, err := CreateSession(base, "/tmp/spec.md", "h1", time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateSession new: %v", err)
	}
	if err := store2.Touch(SessionDone, time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	got, err := ListSessions(base)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sessions, want 2", len(got))
	}
	if got[0].ID != newSess.ID {
		t.Fatalf("got[0].ID = %q, want %q (newer first)", got[0].ID, newSess.ID)
	}
	if got[1].ID != oldSess.ID {
		t.Fatalf("got[1].ID = %q, want %q", got[1].ID, oldSess.ID)
	}
}

func TestListSessions_SkipsCorruptManifests(t *testing.T) {
	base := t.TempDir()
	_, sess, err := CreateSession(base, "/tmp/spec.md", "h1", time.Now())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	corruptDir := SessionDirFor(base, "deadbeefdead")
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, manifestFile), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListSessions(base)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 1 || got[0].ID != sess.ID {
		t.Fatalf("expected only the well-formed session, got %+v", got)
	}
}

func TestFindSessionsForSpec_FiltersBySpecPath(t *testing.T) {
	base := t.TempDir()
	_, sessA, err := CreateSession(base, "/tmp/a.md", "ha", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, sessB, err := CreateSession(base, "/tmp/b.md", "hb", time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	gotA, err := FindSessionsForSpec(base, "/tmp/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(gotA) != 1 || gotA[0].ID != sessA.ID {
		t.Fatalf("FindSessionsForSpec a = %+v, want only %s", gotA, sessA.ID)
	}
	gotB, err := FindSessionsForSpec(base, "/tmp/b.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(gotB) != 1 || gotB[0].ID != sessB.ID {
		t.Fatalf("FindSessionsForSpec b = %+v, want only %s", gotB, sessB.ID)
	}
}

func TestResolveSession_ByID_ReturnsSession(t *testing.T) {
	base := t.TempDir()
	_, sess, err := CreateSession(base, "/tmp/spec.md", "h1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSession(base, sess.ID, "/tmp/spec.md")
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	if got.ID != sess.ID {
		t.Fatalf("got.ID = %q, want %q", got.ID, sess.ID)
	}
}

func TestResolveSession_ByID_SpecMismatch(t *testing.T) {
	base := t.TempDir()
	_, sess, err := CreateSession(base, "/tmp/spec.md", "h1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveSession(base, sess.ID, "/tmp/different.md")
	if !errors.Is(err, ErrSessionSpecMismatch) {
		t.Fatalf("err = %v, want ErrSessionSpecMismatch", err)
	}
}

func TestResolveSession_ByID_NotFound(t *testing.T) {
	base := t.TempDir()
	_, err := ResolveSession(base, "abcdef012345", "/tmp/spec.md")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want it to wrap fs.ErrNotExist", err)
	}
}

func TestResolveSession_BySpec_NoMatch(t *testing.T) {
	base := t.TempDir()
	_, err := ResolveSession(base, "", "/tmp/spec.md")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestResolveSession_BySpec_OneMatch(t *testing.T) {
	base := t.TempDir()
	_, sess, err := CreateSession(base, "/tmp/spec.md", "h1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSession(base, "", "/tmp/spec.md")
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	if got.ID != sess.ID {
		t.Fatalf("got.ID = %q, want %q", got.ID, sess.ID)
	}
}

func TestResolveSession_BySpec_AmbiguousListsCandidates(t *testing.T) {
	base := t.TempDir()
	_, a, err := CreateSession(base, "/tmp/spec.md", "h1", time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := CreateSession(base, "/tmp/spec.md", "h2", time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveSession(base, "", "/tmp/spec.md")
	if !errors.Is(err, ErrSessionAmbiguous) {
		t.Fatalf("err = %v, want ErrSessionAmbiguous", err)
	}
	if !strings.Contains(err.Error(), a.ID) || !strings.Contains(err.Error(), b.ID) {
		t.Fatalf("err message missing both candidate ids: %v", err)
	}
}

func TestOpenSession_RejectsBadID(t *testing.T) {
	base := t.TempDir()
	cases := []string{
		"",
		"NOT-HEX-AT-ALL",
		"abc",
	}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			if _, err := OpenSession(base, id); err == nil {
				t.Fatalf("OpenSession(%q) returned nil error", id)
			}
		})
	}
}

func TestOpenSession_RejectsManifestIDMismatch(t *testing.T) {
	base := t.TempDir()
	_, sess, err := CreateSession(base, "/tmp/spec.md", "h1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	otherDir := SessionDirFor(base, "0123456789ab")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(otherDir, manifestFile)
	body, err := os.ReadFile(filepath.Join(SessionDirFor(base, sess.ID), manifestFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = OpenSession(base, "0123456789ab")
	if err == nil || !strings.Contains(err.Error(), "manifest declares id") {
		t.Fatalf("err = %v, want id-mismatch error", err)
	}
}
