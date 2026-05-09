package director

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestStore returns a Store rooted at a fresh session under
// t.TempDir(). Every Store-facing test goes through this helper so the
// session lifecycle, not the legacy global directory, is what is
// exercised.
func newTestStore(t *testing.T) (*Store, *Session, string) {
	t.Helper()
	base := t.TempDir()
	store, sess, err := CreateSession(base, "/tmp/spec.md", "deadbeef", time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return store, sess, base
}

func TestStore_RunLogPath(t *testing.T) {
	s, _, _ := newTestStore(t)
	cases := []struct {
		name        string
		bucket      string
		agentID     string
		kind        string
		wantSuffix  string
		wantBucket  string
		expectError bool
	}{
		{name: "iteration", bucket: "P7-01", agentID: "bcc-executor-abc123", kind: "stderr.log", wantSuffix: "P7-01/bcc-executor-abc123.stderr.log", wantBucket: "P7-01"},
		{name: "planner empty bucket", bucket: "", agentID: "bcc-planner-xyz", kind: "stderr.log", wantSuffix: "_planner/bcc-planner-xyz.stderr.log", wantBucket: "_planner"},
		{name: "planner explicit", bucket: PlannerRunsBucket, agentID: "bcc-planner-xyz", kind: "stdout.jsonl", wantSuffix: "_planner/bcc-planner-xyz.stdout.jsonl", wantBucket: "_planner"},
		{name: "missing agent", bucket: "P1-01", agentID: "", kind: "stderr.log", expectError: true},
		{name: "missing kind", bucket: "P1-01", agentID: "bcc-x", kind: "", expectError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.RunLogPath(tc.bucket, tc.agentID, tc.kind)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("RunLogPath: %v", err)
			}
			if !strings.HasSuffix(got, tc.wantSuffix) {
				t.Errorf("path = %q, want suffix %q", got, tc.wantSuffix)
			}
			parent := filepath.Dir(got)
			if filepath.Base(parent) != tc.wantBucket {
				t.Errorf("bucket dir = %q, want %q", filepath.Base(parent), tc.wantBucket)
			}
			if info, err := os.Stat(parent); err != nil {
				t.Errorf("parent dir not created: %v", err)
			} else if !info.IsDir() {
				t.Errorf("parent is not a directory")
			}
		})
	}
}

func TestStore_PlanRoundTrip(t *testing.T) {
	s, _, _ := newTestStore(t)
	plan := samplePlan(t)

	if err := s.WritePlan(plan); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}

	got, err := s.ReadPlan()
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if got.Goal != plan.Goal {
		t.Fatalf("goal = %q, want %q", got.Goal, plan.Goal)
	}
	if !got.PlannedAt.Equal(plan.PlannedAt) {
		t.Fatalf("PlannedAt = %v, want %v", got.PlannedAt, plan.PlannedAt)
	}
	if len(got.Phases) != len(plan.Phases) {
		t.Fatalf("phase count = %d, want %d", len(got.Phases), len(plan.Phases))
	}
}

func TestStore_ReadPlan_Missing(t *testing.T) {
	s, _, _ := newTestStore(t)
	_, err := s.ReadPlan()
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want fs.ErrNotExist, got %v", err)
	}
}

func TestStore_ReadPlan_Corrupted(t *testing.T) {
	s, _, _ := newTestStore(t)
	if err := os.WriteFile(filepath.Join(s.SessionDir(), "plan.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.ReadPlan()
	if err == nil {
		t.Fatal("expected error on corrupted plan.json")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("corrupted plan should not surface as ErrNotExist: %v", err)
	}
}

func TestStore_BriefingRoundTrip(t *testing.T) {
	s, _, _ := newTestStore(t)
	b := &Briefing{
		IterationID:   "abc123-1",
		PhaseID:       "abc123",
		SubDAGTaskIDs: []string{"t1", "t2"},
		Instructions:  "first attempt",
		SpecPath:      "/tmp/spec.md",
	}
	if err := s.WriteBriefing(b); err != nil {
		t.Fatalf("WriteBriefing: %v", err)
	}

	got, err := s.ReadBriefing("abc123-1")
	if err != nil {
		t.Fatalf("ReadBriefing: %v", err)
	}
	if got.Instructions != b.Instructions {
		t.Fatalf("Instructions = %q, want %q", got.Instructions, b.Instructions)
	}
	if got.SpecPath != b.SpecPath {
		t.Fatalf("SpecPath = %q, want %q", got.SpecPath, b.SpecPath)
	}
	if len(got.SubDAGTaskIDs) != 2 || got.SubDAGTaskIDs[0] != "t1" {
		t.Fatalf("SubDAGTaskIDs = %v, want [t1 t2]", got.SubDAGTaskIDs)
	}

	wantPath := filepath.Join(s.SessionDir(), "briefings", "abc123-1.json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected briefing at %s: %v", wantPath, err)
	}
}

func TestStore_ReadBriefing_Missing(t *testing.T) {
	s, _, _ := newTestStore(t)
	_, err := s.ReadBriefing("abc123-1")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}

func TestStore_WriteBriefing_RejectsBadInput(t *testing.T) {
	s, _, _ := newTestStore(t)
	if err := s.WriteBriefing(nil); err == nil {
		t.Fatal("expected error on nil briefing")
	}
	if err := s.WriteBriefing(&Briefing{IterationID: "p-1"}); err == nil {
		t.Fatal("expected error on empty phase_id")
	}
	if err := s.WriteBriefing(&Briefing{PhaseID: "x"}); err == nil {
		t.Fatal("expected error on empty iteration_id")
	}
}

func TestStore_SessionDir(t *testing.T) {
	s, sess, base := newTestStore(t)
	want := filepath.Join(base, "sessions", sess.ID)
	if s.SessionDir() != want {
		t.Fatalf("SessionDir() = %q, want %q", s.SessionDir(), want)
	}
}

// CreateSession should refuse blank inputs so a half-initialised
// manifest never reaches disk.
func TestCreateSession_RejectsEmptyInputs(t *testing.T) {
	base := t.TempDir()
	if _, _, err := CreateSession(base, "", "h1", time.Now()); err == nil {
		t.Fatal("expected error for empty spec_path")
	}
	if _, _, err := CreateSession(base, "/tmp/x", "", time.Now()); err == nil {
		t.Fatal("expected error for empty spec_hash")
	}
}

func TestCreateSession_WritesManifest(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	store, sess, err := CreateSession(base, "/tmp/spec.md", "h1", now)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	manifest := filepath.Join(store.SessionDir(), "manifest.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if sess.ID != store.session.ID {
		t.Fatalf("returned session id %q does not match store %q", sess.ID, store.session.ID)
	}
	if sess.Status != SessionRunning {
		t.Fatalf("Status = %q, want %q", sess.Status, SessionRunning)
	}
	if !sess.CreatedAt.Equal(now) || !sess.UpdatedAt.Equal(now) {
		t.Fatalf("timestamps = %v / %v, want %v", sess.CreatedAt, sess.UpdatedAt, now)
	}
}

func TestOpenSession_HappyPath(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	_, sess, err := CreateSession(base, "/tmp/spec.md", "h1", now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenSession(base, sess.ID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if got.Session().ID != sess.ID {
		t.Fatalf("opened id %q, want %q", got.Session().ID, sess.ID)
	}
}

func TestOpenSession_MissingReturnsErrSessionNotFound(t *testing.T) {
	base := t.TempDir()
	_, err := OpenSession(base, "abcdef012345")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want it to wrap fs.ErrNotExist", err)
	}
}

func TestStore_Touch_UpdatesStatusAndTimestamp(t *testing.T) {
	base := t.TempDir()
	created := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	store, _, err := CreateSession(base, "/tmp/spec.md", "h1", created)
	if err != nil {
		t.Fatal(err)
	}
	updated := time.Date(2026, 5, 2, 13, 0, 0, 0, time.UTC)
	if err := store.Touch(SessionDone, updated); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	reopened, err := OpenSession(base, store.Session().ID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if reopened.Session().Status != SessionDone {
		t.Fatalf("Status = %q, want %q", reopened.Session().Status, SessionDone)
	}
	if !reopened.Session().UpdatedAt.Equal(updated) {
		t.Fatalf("UpdatedAt = %v, want %v", reopened.Session().UpdatedAt, updated)
	}
	if !reopened.Session().CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt = %v, want %v (CreatedAt should not move)",
			reopened.Session().CreatedAt, created)
	}
}

func TestStore_Touch_RejectsInvalidStatus(t *testing.T) {
	store, _, _ := newTestStore(t)
	if err := store.Touch("nope", time.Now()); err == nil ||
		!strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("err = %v, want invalid-status error", err)
	}
}

// Ensures the store creates the parent directory the first time a
// briefing is written, even when nothing has been persisted yet.
func TestStore_WriteBriefing_CreatesDirs(t *testing.T) {
	s, _, _ := newTestStore(t)
	b := &Briefing{IterationID: "x-1", PhaseID: "x", Instructions: "y"}
	if err := s.WriteBriefing(b); err != nil {
		t.Fatalf("WriteBriefing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.SessionDir(), "briefings", "x-1.json")); err != nil {
		t.Fatalf("expected file: %v", err)
	}
}

func TestStore_SetIteration_PersistsAndRoundTrips(t *testing.T) {
	base := t.TempDir()
	created := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	store, _, err := CreateSession(base, "/tmp/spec.md", "h1", created)
	if err != nil {
		t.Fatal(err)
	}

	fixedTime := time.Date(2026, 5, 2, 13, 0, 0, 0, time.UTC)
	if err := store.SetIteration(7, 20, fixedTime); err != nil {
		t.Fatalf("SetIteration: %v", err)
	}

	reopened, err := OpenSession(base, store.Session().ID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if reopened.Session().IterationIndex != 7 {
		t.Fatalf("IterationIndex = %d, want 7", reopened.Session().IterationIndex)
	}
	if reopened.Session().MaxIter != 20 {
		t.Fatalf("MaxIter = %d, want 20", reopened.Session().MaxIter)
	}
	if !reopened.Session().UpdatedAt.Equal(fixedTime) {
		t.Fatalf("UpdatedAt = %v, want %v", reopened.Session().UpdatedAt, fixedTime)
	}
}

func TestStore_SetIteration_Idempotent(t *testing.T) {
	base := t.TempDir()
	store, _, err := CreateSession(base, "/tmp/spec.md", "h1", time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	fixedTime := time.Date(2026, 5, 2, 13, 0, 0, 0, time.UTC)
	if err := store.SetIteration(5, 10, fixedTime); err != nil {
		t.Fatalf("SetIteration (first): %v", err)
	}
	if err := store.SetIteration(5, 10, fixedTime); err != nil {
		t.Fatalf("SetIteration (second, same values): %v", err)
	}

	reopened, err := OpenSession(base, store.Session().ID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if reopened.Session().IterationIndex != 5 || reopened.Session().MaxIter != 10 {
		t.Fatalf("values mismatch after idempotent call")
	}
}
