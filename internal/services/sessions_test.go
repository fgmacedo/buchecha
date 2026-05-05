package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/director/dag"
)

// writeManifest writes an archived session manifest under
// baseDir/.bcc/sessions/<id>/manifest.json. The shape mirrors what
// director.CreateSession + director.Touch would produce in production.
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

// writeArchivedDAG writes a serialized dag.State under the session
// directory so SessionService.Snapshot can re-hydrate it.
func writeArchivedDAG(t *testing.T, baseDir, sessionID string, state *dag.State) {
	t.Helper()
	dir := filepath.Join(baseDir, "sessions", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := dag.SaveStateFile(state, filepath.Join(dir, "dag.json")); err != nil {
		t.Fatalf("save dag: %v", err)
	}
}

// trivialPlan returns the smallest valid plan the dag state can be
// constructed from: one phase, one task, in pending status. Real
// production plans are larger; tests do not care about the contents.
func trivialPlan() *director.Plan {
	return &director.Plan{
		Goal:     "x",
		SpecHash: "deadbeef",
		Phases: []director.Phase{{
			ID:     "P1",
			Title:  "phase",
			Intent: "intent",
			Tasks: []director.Task{{
				ID:         "T1",
				Title:      "task",
				Intent:     "intent",
				Acceptance: []director.AcceptanceItem{{ID: "A1", Description: "d", Evidence: "diff"}},
				Status:     director.TaskPending,
			}},
		}},
	}
}

func TestSessionService_List(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")

	archivedOnly := director.Session{
		ID:        "111111111111",
		SpecPath:  "/spec/a.md",
		SpecHash:  "h",
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Hour),
		Status:    director.SessionDone,
	}
	liveOnDisk := director.Session{
		ID:        "222222222222",
		SpecPath:  "/spec/b.md",
		SpecHash:  "h",
		CreatedAt: now.Add(-30 * time.Minute),
		UpdatedAt: now.Add(-20 * time.Minute),
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, archivedOnly)
	writeManifest(t, baseDir, liveOnDisk)

	store, err := director.OpenSession(baseDir, liveOnDisk.ID)
	if err != nil {
		t.Fatalf("open live store: %v", err)
	}
	deps := Deps{
		SessionsBaseDir: baseDir,
		SessionStore:    store,
	}
	svc := newSessionService(deps)

	got, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (got=%v)", len(got), got)
	}
	// Most recently updated session first (liveOnDisk).
	if got[0].ID != liveOnDisk.ID {
		t.Fatalf("got[0].ID = %q, want %q", got[0].ID, liveOnDisk.ID)
	}
	if got[1].ID != archivedOnly.ID {
		t.Fatalf("got[1].ID = %q, want %q", got[1].ID, archivedOnly.ID)
	}
	if got[1].FinishedAt.IsZero() {
		t.Fatal("archived session should carry FinishedAt")
	}
	if !got[0].FinishedAt.IsZero() {
		t.Fatal("running session should not carry FinishedAt")
	}
}

func TestSessionService_List_NoBaseDirReturnsInternal(t *testing.T) {
	t.Parallel()

	svc := newSessionService(Deps{})
	_, err := svc.List(context.Background())
	if err == nil {
		t.Fatal("expected error for empty SessionsBaseDir")
	}
	if !errors.Is(err, ErrInternal) {
		t.Fatalf("err = %v, want ErrInternal", err)
	}
}

func TestSessionService_Get(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	archived := director.Session{
		ID:        "abcdefabcdef",
		SpecPath:  "/spec/x.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionDone,
	}
	writeManifest(t, baseDir, archived)

	live := director.Session{
		ID:        "ffffffffffff",
		SpecPath:  "/spec/y.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, live)
	liveStore, err := director.OpenSession(baseDir, live.ID)
	if err != nil {
		t.Fatalf("open live: %v", err)
	}

	cases := []struct {
		name    string
		id      string
		deps    Deps
		wantErr error
		check   func(t *testing.T, m SessionMeta)
	}{
		{
			name: "live path",
			id:   live.ID,
			deps: Deps{SessionsBaseDir: baseDir, SessionStore: liveStore},
			check: func(t *testing.T, m SessionMeta) {
				if m.ID != live.ID {
					t.Fatalf("ID = %q", m.ID)
				}
				if m.Status != string(director.SessionRunning) {
					t.Fatalf("Status = %q", m.Status)
				}
			},
		},
		{
			name: "archived path",
			id:   archived.ID,
			deps: Deps{SessionsBaseDir: baseDir, SessionStore: liveStore},
			check: func(t *testing.T, m SessionMeta) {
				if m.ID != archived.ID {
					t.Fatalf("ID = %q", m.ID)
				}
				if m.Status != string(director.SessionDone) {
					t.Fatalf("Status = %q", m.Status)
				}
				if m.FinishedAt.IsZero() {
					t.Fatal("FinishedAt should be set on done session")
				}
			},
		},
		{
			name:    "unknown id returns session_not_found",
			id:      "000000000000",
			deps:    Deps{SessionsBaseDir: baseDir},
			wantErr: ErrSessionNotFound,
		},
		{
			name:    "empty id returns invalid_request",
			id:      "",
			deps:    Deps{SessionsBaseDir: baseDir},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "no base dir returns session_not_found",
			id:      "000000000000",
			deps:    Deps{},
			wantErr: ErrSessionNotFound,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc := newSessionService(tc.deps)
			got, err := svc.Get(context.Background(), tc.id)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			tc.check(t, got)
		})
	}
}

func TestSessionService_Snapshot_LivePath(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	live := director.Session{
		ID:        "abc123abc123",
		SpecPath:  "/spec/live.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, live)
	store, err := director.OpenSession(baseDir, live.ID)
	if err != nil {
		t.Fatalf("open live: %v", err)
	}

	state := dag.NewStateFromPlan(trivialPlan())
	registry := dag.NewAgentRegistry(nil)
	handler := dag.NewHandler(state, registry)

	svc := newSessionService(Deps{
		SessionsBaseDir: baseDir,
		SessionStore:    store,
		DAGHandler:      handler,
	})
	got, err := svc.Snapshot(context.Background(), live.ID)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.Session.ID != live.ID {
		t.Fatalf("Session.ID = %q", got.Session.ID)
	}
	if got.DAG == nil {
		t.Fatal("DAG nil; expected deep copy")
	}
	if got.DAG == state {
		t.Fatal("DAG returned the live State pointer; must be a deep copy")
	}
	// Mutating the returned snapshot must not bleed into live state.
	if err := got.DAG.SetTaskStatus("P1", "T1", director.TaskDone); err != nil {
		t.Fatalf("SetTaskStatus on snapshot: %v", err)
	}
	if state.Phase("P1").Tasks["T1"].Status != director.TaskPending {
		t.Fatal("live state mutated through snapshot")
	}
}

func TestSessionService_Snapshot_ArchivedPath(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	archived := director.Session{
		ID:        "deadbeef0001",
		SpecPath:  "/spec/arch.md",
		SpecHash:  "h",
		CreatedAt: now.Add(-1 * time.Hour),
		UpdatedAt: now.Add(-30 * time.Minute),
		Status:    director.SessionDone,
	}
	writeManifest(t, baseDir, archived)
	state := dag.NewStateFromPlan(trivialPlan())
	if err := state.SetTaskStatus("P1", "T1", director.TaskDone); err != nil {
		t.Fatalf("SetTaskStatus: %v", err)
	}
	writeArchivedDAG(t, baseDir, archived.ID, state)

	svc := newSessionService(Deps{SessionsBaseDir: baseDir})
	got, err := svc.Snapshot(context.Background(), archived.ID)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.Session.ID != archived.ID {
		t.Fatalf("Session.ID = %q", got.Session.ID)
	}
	if got.DAG == nil {
		t.Fatal("DAG nil; expected loaded archived state")
	}
	phase := got.DAG.Phase("P1")
	if phase == nil {
		t.Fatal("phase P1 missing")
	}
	if phase.Tasks["T1"].Status != director.TaskDone {
		t.Fatalf("task T1 status = %q, want done", phase.Tasks["T1"].Status)
	}
}

func TestSessionService_Snapshot_UnknownReturnsNotFound(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	svc := newSessionService(Deps{SessionsBaseDir: baseDir})
	_, err := svc.Snapshot(context.Background(), "000000000000")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

// TestSessionService_LiveAlias covers the SPA's bootstrap path: the
// dashboard hits /api/v1/sessions/live/{snapshot,...} before it has a
// real session id, and the service must resolve the alias to whichever
// session is bound as live.
func TestSessionService_LiveAlias(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	live := director.Session{
		ID:        "a1b2c3d4e5f6",
		SpecPath:  "/spec/live.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, live)
	store, err := director.OpenSession(baseDir, live.ID)
	if err != nil {
		t.Fatalf("open live: %v", err)
	}

	state := dag.NewStateFromPlan(trivialPlan())
	registry := dag.NewAgentRegistry(nil)
	handler := dag.NewHandler(state, registry)

	svc := newSessionService(Deps{
		SessionsBaseDir: baseDir,
		SessionStore:    store,
		DAGHandler:      handler,
	})

	t.Run("Get resolves to the bound live session", func(t *testing.T) {
		got, err := svc.Get(context.Background(), LiveSessionAlias)
		if err != nil {
			t.Fatalf("Get(live): %v", err)
		}
		if got.ID != live.ID {
			t.Errorf("Get(live).ID = %q, want %q", got.ID, live.ID)
		}
	})

	t.Run("Snapshot resolves to the bound live session", func(t *testing.T) {
		got, err := svc.Snapshot(context.Background(), LiveSessionAlias)
		if err != nil {
			t.Fatalf("Snapshot(live): %v", err)
		}
		if got.Session.ID != live.ID {
			t.Errorf("Snapshot(live).Session.ID = %q, want %q", got.Session.ID, live.ID)
		}
		if got.DAG == nil {
			t.Error("Snapshot(live).DAG nil; expected deep copy of live state")
		}
	})
}

// TestSessionService_LiveAliasWithoutLive verifies the alias surfaces
// ErrSessionNotFound when no SessionStore is bound, instead of silently
// returning a zero-value record.
func TestSessionService_LiveAliasWithoutLive(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	svc := newSessionService(Deps{SessionsBaseDir: baseDir})

	if _, err := svc.Get(context.Background(), LiveSessionAlias); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Get(live) without SessionStore: err = %v, want ErrSessionNotFound", err)
	}
	if _, err := svc.Snapshot(context.Background(), LiveSessionAlias); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Snapshot(live) without SessionStore: err = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionService_Snapshot_ArchivedWithoutDAGFile(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	archived := director.Session{
		ID:        "abcabcabc123",
		SpecPath:  "/spec/p.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionDone,
	}
	writeManifest(t, baseDir, archived)

	svc := newSessionService(Deps{SessionsBaseDir: baseDir})
	got, err := svc.Snapshot(context.Background(), archived.ID)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.Session.ID != archived.ID {
		t.Fatalf("Session.ID = %q", got.Session.ID)
	}
	if got.DAG != nil {
		t.Fatal("DAG should be nil when dag.json is absent")
	}
}
