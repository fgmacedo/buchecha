package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/supervision/dag"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

// TestNew_WiresAllServices is the aggregator-level smoke test: every
// sub-service is non-nil, an empty Deps still produces a usable
// Services pointer (callers branch on per-method errors), and the
// production-shaped Deps wires every handle without I/O at construction.
func TestNew_WiresAllServices(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		deps Deps
	}{
		{
			name: "empty deps",
			deps: Deps{},
		},
		{
			name: "production-shaped deps",
			deps: productionDeps(t),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := New(tc.deps)
			if svc.Sessions == nil {
				t.Fatal("Sessions nil")
			}
			if svc.Events == nil {
				t.Fatal("Events nil")
			}
			if svc.Briefings == nil {
				t.Fatal("Briefings nil")
			}
			if svc.Prompts == nil {
				t.Fatal("Prompts nil")
			}
			if svc.Audit == nil {
				t.Fatal("Audit nil")
			}
		})
	}
}

// TestNew_AuditWiredFromDeps verifies that Audit picks up the path
// from Deps so the aggregator does not require a separate setter.
func TestNew_AuditWiredFromDeps(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "audit.ndjson")
	svc := New(Deps{AuditPath: path})
	if svc.Audit.Path() != path {
		t.Fatalf("Audit.Path = %q, want %q", svc.Audit.Path(), path)
	}
}

// TestNew_ServicesShareDeps verifies that the SessionStore handle is
// observable across services so consumers can rely on a coherent view.
// SessionService and BriefingService both read SessionStore.SessionDir,
// so a write through one must be visible to the other.
func TestNew_ServicesShareDeps(t *testing.T) {
	t.Parallel()
	deps := productionDeps(t)
	svc := New(deps)

	live := deps.SessionStore.Session()
	ctx := context.Background()
	got, err := svc.Sessions.Get(ctx, live.ID)
	if err != nil {
		t.Fatalf("Sessions.Get: %v", err)
	}
	if got.ID != live.ID {
		t.Fatalf("Sessions.Get ID = %q, want %q", got.ID, live.ID)
	}
}

// productionDeps assembles a Deps in the same shape the cli boot
// produces: a freshly created session, an in-memory dag handler, an
// open loop events channel, and an audit path under the session dir.
// Tests use this to exercise paths that depend on multiple Deps
// fields.
func productionDeps(t *testing.T) Deps {
	t.Helper()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := session.Session{
		ID:        "ffeeddccbbaa",
		SpecPath:  "/spec/agg.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    session.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	store, err := session.OpenSession(baseDir, sess.ID)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	state := dag.NewStateFromPlan(trivialPlan())
	registry := dag.NewAgentRegistry(nil)
	handler := dag.NewHandler(state, registry)
	loopEvents := make(chan loop.Event, 4)
	t.Cleanup(func() { close(loopEvents) })
	return Deps{
		LoopEvents:      loopEvents,
		DAGHandler:      handler,
		SessionStore:    store,
		SessionsBaseDir: baseDir,
		AuditPath:       filepath.Join(store.SessionDir(), "audit.ndjson"),
	}
}
