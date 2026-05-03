package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/config"
	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/director/dag"
	"github.com/fgmacedo/buchecha/internal/director/fake"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// scriptedPlan returns a Plan with two phases that pass ValidatePlan,
// suitable as the FakePlanner output for runDirectorWith tests.
func scriptedPlan() *director.Plan {
	return &director.Plan{
		Goal: "Wire the Director MVP",
		SuccessCriteria: []string{
			"go test -race ./... is green",
			"bcc run --director boots end to end",
		},
		Phases: []director.Phase{
			{
				ID:        "phase-1",
				Title:     "First phase",
				Intent:    "Bootstrap things",
				DependsOn: nil,
				Tasks: []director.Task{
					{
						ID:     "t1",
						Title:  "build it",
						Intent: "ensure it compiles",
						Acceptance: []director.AcceptanceItem{
							{ID: "a1", Description: "compiles", Evidence: director.EvidenceBuild},
						},
						Status:      director.TaskPending,
						RetryBudget: 2,
					},
				},
			},
			{
				ID:        "phase-2",
				Title:     "Second phase",
				Intent:    "Round it out",
				DependsOn: []string{"phase-1"},
				Tasks: []director.Task{
					{
						ID:     "t1",
						Title:  "test it",
						Intent: "tests pass",
						Acceptance: []director.AcceptanceItem{
							{ID: "a2", Description: "tests pass", Evidence: director.EvidenceTest},
						},
						Status:      director.TaskPending,
						RetryBudget: 1,
					},
				},
			},
		},
	}
}

func writeTestSpec(t *testing.T, dir string) string {
	t.Helper()
	specPath := filepath.Join(dir, "spec.md")
	body := []byte("# spec\n\n## Implementation Plan\n\n### Phase 1\n[ ] do thing\n")
	if err := os.WriteFile(specPath, body, 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return specPath
}

// mkSessionStore allocates a fresh session-backed Store rooted at
// tmp/.bcc/sessions/<id>/ for the supplied spec path. Tests use it
// instead of poking at directory paths so the production session
// lifecycle is what is exercised.
func mkSessionStore(t *testing.T, tmp, specPath string) *director.Store {
	t.Helper()
	hash := "deadbeef"
	if data, err := os.ReadFile(specPath); err == nil {
		hash = director.SpecHash(data)
	}
	store, _, err := director.CreateSession(filepath.Join(tmp, ".bcc"), specPath, hash, time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return store
}

// resetExitCode keeps tests independent: ExitCode is a package-level
// variable shared across the cli package, so any test that exercises
// runDirectorWith must restore it.
func resetExitCode(t *testing.T) {
	t.Helper()
	prev := ExitCode
	t.Cleanup(func() { ExitCode = prev })
}

// withOutputMode pins runOutput to the given mode for the test and
// restores the previous value on cleanup. Director-mode tests that go
// through dispatchEvents bypass the bubbletea TUI path; without this
// override the runDirectorWith branch hits the TUI host and fails on a
// missing /dev/tty.
func withOutputMode(t *testing.T, mode string) {
	t.Helper()
	prev := runOutput
	runOutput = mode
	t.Cleanup(func() { runOutput = prev })
}

// scriptedBriefer emits a briefing through the handler so the loop can
// observe it via Handler.Briefing(iterationID).
func scriptedBriefer(h *dag.Handler) *fake.Briefer {
	return &fake.Briefer{
		BriefFn: func(ctx context.Context, in director.BrieferInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
			ids := make([]any, len(in.SubDAGTaskIDs))
			for i, s := range in.SubDAGTaskIDs {
				ids[i] = s
			}
			input := map[string]any{
				"agent_id": in.AgentID,
				"briefing": map[string]any{
					"iteration_id":     in.IterationID,
					"phase_id":         in.PhaseID,
					"sub_dag_task_ids": ids,
					"spec_path":        in.SpecPath,
					"instructions":     "stub context",
				},
			}
			_, err := h.HandleCall(ctx, string(dag.RoleBriefer), dag.MethodBriefingEmit, input)
			return &director.DirectorCallStats{}, err
		},
	}
}

// approvingReviewer marks each sub-DAG task done and finalises with
// outcome=approve through the handler.
func approvingReviewer(h *dag.Handler) *fake.Reviewer {
	return &fake.Reviewer{
		ReviewFn: func(ctx context.Context, in director.ReviewerInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
			for _, tid := range in.SubDAG {
				if _, err := h.HandleCall(ctx, string(dag.RoleReviewer), dag.MethodTaskApproved, map[string]any{
					"agent_id": in.AgentID,
					"id":       tid,
				}); err != nil {
					return nil, err
				}
			}
			_, err := h.HandleCall(ctx, string(dag.RoleReviewer), dag.MethodReviewFinished, map[string]any{
				"agent_id":  in.AgentID,
				"outcome":   "approve",
				"reasoning": "looks good",
			})
			return &director.DirectorCallStats{}, err
		},
	}
}

// stringSliceToAny converts []string to []any so JSON Schema array
// validation accepts the slice when invoked in-process; the JSON-RPC
// layer would deliver []any natively.
func stringSliceToAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// newTestHandler returns a fresh dag.Handler suitable for cli tests.
// Plan-derived state is built lazily by the loop the first time it is
// requested.
func newTestHandler() *dag.Handler {
	return dag.NewHandler(nil, dag.NewAgentRegistry(nil))
}

// testRegisterFn returns a directorDeps.registerFn closure that
// registers Director agents against the supplied test handler's
// registry, mirroring what the production MCP boot does. Tests that
// need the planner / briefer / reviewer fakes to call MCP methods with
// a registered agent_id wire this in.
func testRegisterFn(h *dag.Handler) func(role dag.Role) (string, func(), error) {
	return func(role dag.Role) (string, func(), error) {
		id, err := h.Registry().Register(role, dag.RegisterArgs{})
		if err != nil {
			return "", func() {}, err
		}
		cleanup := func() { h.Registry().Deregister(id) }
		return string(id), cleanup, nil
	}
}

// recordingExecutor is a loop.Executor stand-in that captures Run
// arguments and never spawns a subprocess. When emitSignal is set and
// handler+args are wired, it registers an Executor agent on the run
// handler and calls bcc_iteration_finished — exactly what the
// production claude adapter would do once the briefing closes — so the
// loop driver picks the signal up via Handler.IterationSignal.
type recordingExecutor struct {
	mu               sync.Mutex
	systemPromptFile string
	runs             int
	emitSignal       agentcontract.Signal
	promptPaths      []string
	handler          *dag.Handler
	args             dag.RegisterArgs
}

func (r *recordingExecutor) Run(ctx context.Context, _ string, _ chan<- agentcontract.AgentEvent) (loop.ExecResult, error) {
	r.mu.Lock()
	r.runs++
	r.promptPaths = append(r.promptPaths, r.systemPromptFile)
	signal := r.emitSignal
	handler := r.handler
	args := r.args
	r.mu.Unlock()

	if signal != agentcontract.SignalUnknown && handler != nil {
		id, err := handler.Registry().Register(dag.RoleExecutor, args)
		if err == nil {
			_, _ = handler.HandleCall(ctx, string(dag.RoleExecutor), dag.MethodIterationFinished, map[string]any{
				"agent_id": string(id),
				"signal":   signal.String(),
			})
			handler.Registry().Deregister(id)
		}
	}
	return loop.ExecResult{ExitCode: 0}, nil
}

// recordingExecutorFactory returns a newExecutor closure plus a
// pointer to the shared recordingExecutor every Run call goes through.
// The factory captures the system_prompt_file path on each call. The
// handler reference lets the executor mirror a production agent: it
// registers, reports the iteration signal via MCP, and deregisters.
func recordingExecutorFactory(signal agentcontract.Signal, h *dag.Handler) (func(dag.RegisterArgs, func(string) (string, error)) loop.Executor, *recordingExecutor) {
	rec := &recordingExecutor{emitSignal: signal, handler: h}
	return func(args dag.RegisterArgs, renderSystem func(string) (string, error)) loop.Executor {
		path := ""
		if renderSystem != nil {
			p, err := renderSystem("test-executor-agent")
			if err != nil {
				return &failingExecutor{err: err}
			}
			path = p
		}
		rec.mu.Lock()
		rec.systemPromptFile = path
		rec.args = args
		rec.mu.Unlock()
		return rec
	}, rec
}

// stubGitProbe is a hand-rolled loop.GitProbe for run_director tests.
// HeadFn (when non-nil) returns scripted SHAs in call order; DiffFn
// (when non-nil) is invoked verbatim. heads cycles through the slice;
// when called more times than heads has entries, it pairs (base, head)
// in alternation so multi-phase runs do not run out of SHAs.
type stubGitProbe struct {
	mu      sync.Mutex
	heads   []string
	idx     int
	diff    string
	diffErr error
}

func (s *stubGitProbe) HeadSHA(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.heads) == 0 {
		return "", errors.New("stubGitProbe: no scripted heads")
	}
	out := s.heads[s.idx%len(s.heads)]
	s.idx++
	return out, nil
}

func (s *stubGitProbe) CurrentBranch(_ context.Context) (string, error) { return "main", nil }
func (s *stubGitProbe) IsClean(_ context.Context) (bool, error)         { return true, nil }
func (s *stubGitProbe) Diff(_ context.Context, _, _ string) (string, error) {
	return s.diff, s.diffErr
}

// newAdvancingGit returns a stubGitProbe whose HeadSHA alternates
// between two SHAs; every iteration sees a head that differs from its
// baseline, so the decider never trips HEAD-stuck.
func newAdvancingGit() *stubGitProbe {
	return &stubGitProbe{heads: []string{"baseSHA", "headSHA"}, diff: "diff body\n"}
}

// directorTestConfig returns a Config large enough that the loop runs
// the full plan without hitting the iteration cap.
func directorTestConfig() *config.Config {
	c := &config.Config{}
	c.Loop.MaxIterations = 50
	c.Director.RetryBudget = 2
	return c
}

// TestRunDirectorWith_HappyPath_TwoPhasesApprove drives runDirectorWith
// end to end with fake Director ports: planner returns a two-phase
// plan, briefer renders a Briefing per attempt, executor signals
// review, reviewer approves, decider advances, loop terminates with
// ExitDone. The test pins the persisted artifacts under the session dir.
func TestRunDirectorWith_HappyPath_TwoPhasesApprove(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	plannerCalls := 0
	planner := &fake.Planner{
		PlanFn: func(_ context.Context, in director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			plannerCalls++
			if in.SpecPath != specPath {
				t.Errorf("planner spec_path = %q, want %q", in.SpecPath, specPath)
			}
			if in.SpecHash == "" {
				t.Error("planner spec_hash empty")
			}
			return scriptedPlan(), &director.DirectorCallStats{}, nil
		},
	}

	h := newTestHandler()
	briefCalls := 0
	briefer := &fake.Briefer{
		BriefFn: func(ctx context.Context, in director.BrieferInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
			briefCalls++
			input := map[string]any{
				"agent_id": in.AgentID,
				"briefing": map[string]any{
					"iteration_id":     in.IterationID,
					"phase_id":         in.PhaseID,
					"sub_dag_task_ids": stringSliceToAny(in.SubDAGTaskIDs),
					"spec_path":        in.SpecPath,
					"instructions":     "context",
				},
			}
			_, err := h.HandleCall(ctx, string(dag.RoleBriefer), dag.MethodBriefingEmit, input)
			return &director.DirectorCallStats{}, err
		},
	}

	newExec, rec := recordingExecutorFactory(agentcontract.SignalReview, h)
	store := mkSessionStore(t, tmp, specPath)
	deps := directorDeps{
		handler:     h,
		planner:     planner,
		briefer:     briefer,
		reviewer:    approvingReviewer(h),
		store:       store,
		git:         newAdvancingGit(),
		newExecutor: newExec,
		now:         func() time.Time { return time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC) },
	}

	var stderr bytes.Buffer
	dio := directorIO{
		stdin:  strings.NewReader("p\n"),
		stderr: &stderr,
	}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ExitCode != loop.ExitDone {
		t.Errorf("ExitCode = %d, want ExitDone", ExitCode)
	}
	if plannerCalls != 1 {
		t.Errorf("planner called %d times, want 1", plannerCalls)
	}
	if briefCalls != 2 {
		t.Errorf("briefer called %d times, want 2 (one per phase)", briefCalls)
	}
	if rec.runs != 2 {
		t.Errorf("executor ran %d times, want 2", rec.runs)
	}

	planPath := filepath.Join(store.SessionDir(), "plan.json")
	data, readErr := os.ReadFile(planPath)
	if readErr != nil {
		t.Fatalf("read persisted plan: %v", readErr)
	}
	var got director.Plan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse persisted plan: %v", err)
	}
	if got.SpecHash == "" {
		t.Error("persisted plan has empty spec_hash")
	}

	if !strings.Contains(stderr.String(), "Director plan") {
		t.Errorf("stderr missing plan header: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "[P]roceed") {
		t.Errorf("stderr missing confirmation prompt: %q", stderr.String())
	}

	// Session lifecycle: a clean run lands in status=done.
	reopened, err := director.OpenSession(filepath.Join(tmp, ".bcc"), store.Session().ID)
	if err != nil {
		t.Fatalf("OpenSession after run: %v", err)
	}
	if reopened.Session().Status != director.SessionDone {
		t.Errorf("session status = %q, want %q", reopened.Session().Status, director.SessionDone)
	}
}

// TestRunDirectorWith_AbortPath covers `[A]bort`: ExitInvalid, no
// loop run, and the persisted plan still exists for inspection.
func TestRunDirectorWith_AbortPath(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			return scriptedPlan(), &director.DirectorCallStats{}, nil
		},
	}
	briefer := &fake.Briefer{
		BriefFn: func(_ context.Context, _ director.BrieferInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
			t.Fatal("briefer should not be called when user aborts")
			return nil, nil
		},
	}
	store := mkSessionStore(t, tmp, specPath)
	h := newTestHandler()
	deps := directorDeps{
		handler:  h,
		planner:  planner,
		briefer:  briefer,
		reviewer: &fake.Reviewer{},
		store:    store,
		git:      newAdvancingGit(),
		newExecutor: func(dag.RegisterArgs, func(string) (string, error)) loop.Executor {
			return &recordingExecutor{}
		},
		now: time.Now,
	}
	dio := directorIO{stdin: strings.NewReader("a\n"), stderr: io.Discard}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio)
	if !errors.Is(err, errDirectorAborted) {
		t.Fatalf("err = %v, want errDirectorAborted", err)
	}
	if ExitCode != loop.ExitInvalid {
		t.Errorf("ExitCode = %d, want ExitInvalid", ExitCode)
	}
	if _, statErr := os.Stat(filepath.Join(store.SessionDir(), "plan.json")); statErr != nil {
		t.Errorf("plan.json missing after abort: %v", statErr)
	}

	// Session lifecycle: aborted runs end up in status=aborted so a
	// later `bcc sessions list` shows the failure clearly.
	reopened, err := director.OpenSession(filepath.Join(tmp, ".bcc"), store.Session().ID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if reopened.Session().Status != director.SessionAborted {
		t.Errorf("status = %q, want %q", reopened.Session().Status, director.SessionAborted)
	}
}

// TestRunDirectorWith_AutoProceedSkipsPrompt covers --auto-proceed:
// stdin is never read, the confirmation block is skipped, the loop
// runs the plan to ExitDone.
func TestRunDirectorWith_AutoProceedSkipsPrompt(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			return scriptedPlan(), &director.DirectorCallStats{}, nil
		},
	}
	h := newTestHandler()
	newExec, _ := recordingExecutorFactory(agentcontract.SignalReview, h)
	deps := directorDeps{
		handler:     h,
		planner:     planner,
		briefer:     scriptedBriefer(h),
		reviewer:    approvingReviewer(h),
		store:       mkSessionStore(t, tmp, specPath),
		git:         newAdvancingGit(),
		newExecutor: newExec,
		now:         time.Now,
	}
	// emptyReader returns EOF immediately; if the implementation reads
	// stdin under --auto-proceed before escalation it would either block
	// or treat EOF as abort.
	dio := directorIO{stdin: strings.NewReader(""), stderr: io.Discard, autoProceed: true}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ExitCode != loop.ExitDone {
		t.Errorf("ExitCode = %d, want ExitDone", ExitCode)
	}
}

// TestRunDirectorWith_RejectsEmptyPlan covers the ValidatePlan gate:
// when the planner returns a plan with zero phases, runDirectorWith
// fails before persistence and never prompts the user.
func TestRunDirectorWith_RejectsEmptyPlan(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			return &director.Plan{Goal: "x", Phases: nil}, &director.DirectorCallStats{}, nil
		},
	}
	store := mkSessionStore(t, tmp, specPath)
	h := newTestHandler()
	deps := directorDeps{
		handler:  h,
		planner:  planner,
		briefer:  scriptedBriefer(h),
		reviewer: &fake.Reviewer{},
		store:    store,
		git:      newAdvancingGit(),
		newExecutor: func(dag.RegisterArgs, func(string) (string, error)) loop.Executor {
			return &recordingExecutor{}
		},
		now: time.Now,
	}
	dio := directorIO{stdin: strings.NewReader("p\n"), stderr: io.Discard}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio)
	if err == nil || !strings.Contains(err.Error(), "no phases") {
		t.Fatalf("err = %v, want no-phases error", err)
	}
	if ExitCode != loop.ExitInvalid {
		t.Errorf("ExitCode = %d, want ExitInvalid", ExitCode)
	}
	if _, statErr := os.Stat(filepath.Join(store.SessionDir(), "plan.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("plan persisted despite validation error: %v", statErr)
	}
}

// TestRunDirectorWith_PlannerSkipsHeadless drives the planner-skip
// path end to end on the JSON output: the planner's fake calls
// bcc_plan_skip via the handler and returns nil; runDirectorWith must
// exit cleanly with ExitDone, mark the session as done, never prompt
// for confirmation, and never persist a plan.
func TestRunDirectorWith_PlannerSkipsHeadless(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	h := newTestHandler()
	planner := &fake.Planner{
		PlanFn: func(ctx context.Context, in director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			if _, err := h.HandleCall(ctx, string(dag.RolePlanner), dag.MethodPlanSkip, map[string]any{
				"agent_id": in.AgentID,
				"reason":   "every acceptance bullet is checked off in the spec journal",
			}); err != nil {
				return nil, nil, err
			}
			return nil, &director.DirectorCallStats{}, nil
		},
	}
	store := mkSessionStore(t, tmp, specPath)
	deps := directorDeps{
		handler:  h,
		planner:  planner,
		briefer:  scriptedBriefer(h),
		reviewer: approvingReviewer(h),
		store:    store,
		git:      newAdvancingGit(),
		newExecutor: func(dag.RegisterArgs, func(string) (string, error)) loop.Executor {
			return &recordingExecutor{}
		},
		registerFn: testRegisterFn(h),
		now:        func() time.Time { return time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC) },
	}

	var stderr bytes.Buffer
	dio := directorIO{stdin: strings.NewReader(""), stderr: &stderr}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ExitCode != loop.ExitDone {
		t.Errorf("ExitCode = %d, want ExitDone", ExitCode)
	}
	if !strings.Contains(stderr.String(), "nothing to do") {
		t.Errorf("stderr missing nothing-to-do hint: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "[P]roceed") {
		t.Error("confirmation prompt rendered despite skip")
	}
	if _, statErr := os.Stat(filepath.Join(store.SessionDir(), "plan.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("plan persisted despite skip: %v", statErr)
	}
	reopened, err := director.OpenSession(filepath.Join(tmp, ".bcc"), store.Session().ID)
	if err != nil {
		t.Fatalf("OpenSession after skip: %v", err)
	}
	if reopened.Session().Status != director.SessionDone {
		t.Errorf("Session status = %q, want %q", reopened.Session().Status, director.SessionDone)
	}
}

// TestRunDirectorWith_PlannerSkipsThenAgentExitsNonZero reproduces the
// real-world failure where the planner subprocess called bcc_plan_skip
// successfully but then exited with a non-zero status. Handler state
// is authoritative: the run still surfaces the friendly nothing-to-do
// path with ExitDone instead of treating the exit code as fatal.
func TestRunDirectorWith_PlannerSkipsThenAgentExitsNonZero(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	h := newTestHandler()
	planner := &fake.Planner{
		PlanFn: func(ctx context.Context, in director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			if _, err := h.HandleCall(ctx, string(dag.RolePlanner), dag.MethodPlanSkip, map[string]any{
				"agent_id": in.AgentID,
				"reason":   "spec already complete; skipped",
			}); err != nil {
				return nil, nil, err
			}
			return nil, &director.DirectorCallStats{}, errors.New("director/claude: claude exited 1")
		},
	}
	store := mkSessionStore(t, tmp, specPath)
	deps := directorDeps{
		handler:  h,
		planner:  planner,
		briefer:  scriptedBriefer(h),
		reviewer: approvingReviewer(h),
		store:    store,
		git:      newAdvancingGit(),
		newExecutor: func(dag.RegisterArgs, func(string) (string, error)) loop.Executor {
			return &recordingExecutor{}
		},
		registerFn: testRegisterFn(h),
		now:        func() time.Time { return time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC) },
	}

	var stderr bytes.Buffer
	dio := directorIO{stdin: strings.NewReader(""), stderr: &stderr}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio); err != nil {
		t.Fatalf("err = %v, want nil; agent exit code must not override handler skip", err)
	}
	if ExitCode != loop.ExitDone {
		t.Errorf("ExitCode = %d, want ExitDone", ExitCode)
	}
	if !strings.Contains(stderr.String(), "nothing to do") {
		t.Errorf("stderr missing nothing-to-do hint: %q", stderr.String())
	}
}

// TestRunDirectorWith_PlannerExitsNonZeroWithoutTerminalCall keeps the
// fatal path honest: when the agent crashed without ever calling
// bcc_plan_emit or bcc_plan_skip, the run still aborts with
// ExitInvalid and surfaces the underlying agent error.
func TestRunDirectorWith_PlannerExitsNonZeroWithoutTerminalCall(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	h := newTestHandler()
	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			return nil, &director.DirectorCallStats{}, errors.New("director/claude: claude exited 1")
		},
	}
	store := mkSessionStore(t, tmp, specPath)
	deps := directorDeps{
		handler:  h,
		planner:  planner,
		briefer:  scriptedBriefer(h),
		reviewer: approvingReviewer(h),
		store:    store,
		git:      newAdvancingGit(),
		newExecutor: func(dag.RegisterArgs, func(string) (string, error)) loop.Executor {
			return &recordingExecutor{}
		},
		registerFn: testRegisterFn(h),
		now:        time.Now,
	}
	dio := directorIO{stdin: strings.NewReader(""), stderr: io.Discard}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio)
	if err == nil || !strings.Contains(err.Error(), "claude exited 1") {
		t.Fatalf("err = %v, want wrapped agent exit error", err)
	}
	if ExitCode != loop.ExitInvalid {
		t.Errorf("ExitCode = %d, want ExitInvalid", ExitCode)
	}
}

// TestRunCmd_AutoProceedFlagDefaultsOff locks --auto-proceed off so a
// missing terminal in CI does not silently green-light a Director plan
// that no human reviewed.
func TestRunCmd_AutoProceedFlagDefaultsOff(t *testing.T) {
	flag := runCmd.Flags().Lookup("auto-proceed")
	if flag == nil {
		t.Fatal("runCmd has no --auto-proceed flag")
	}
	if got := flag.DefValue; got != "false" {
		t.Errorf("--auto-proceed default = %q, want false", got)
	}
}

// TestRunCmd_SessionFlagExistsDefaultsEmpty pins the --session flag
// surface so a future CLI refactor cannot silently drop it.
func TestRunCmd_SessionFlagExistsDefaultsEmpty(t *testing.T) {
	flag := runCmd.Flags().Lookup("session")
	if flag == nil {
		t.Fatal("runCmd has no --session flag")
	}
	if flag.DefValue != "" {
		t.Errorf("--session default = %q, want empty", flag.DefValue)
	}
}

// TestRunDirectorWith_ResumeMatchingHash_SkipsPlanner exercises the
// happy resume path: a plan was persisted in a previous session and
// the spec content is byte-identical, so the planner is never called
// and the user is not prompted; the loop runs the persisted plan to
// ExitDone.
func TestRunDirectorWith_ResumeMatchingHash_SkipsPlanner(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	persisted := scriptedPlan()
	specContent, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	persisted.SpecHash = director.SpecHash(specContent)
	store := mkSessionStore(t, tmp, specPath)
	if err := store.WritePlan(persisted); err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			t.Fatal("planner should not be called when spec hash matches")
			return nil, nil, nil
		},
	}
	h := newTestHandler()
	newExec, rec := recordingExecutorFactory(agentcontract.SignalReview, h)

	deps := directorDeps{
		handler:     h,
		planner:     planner,
		briefer:     scriptedBriefer(h),
		reviewer:    approvingReviewer(h),
		store:       store,
		git:         newAdvancingGit(),
		newExecutor: newExec,
		now:         func() time.Time { return time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC) },
	}

	var stderr bytes.Buffer
	dio := directorIO{
		stdin:  strings.NewReader(""), // never read on resume happy path
		stderr: &stderr,
		resume: true,
	}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ExitCode != loop.ExitDone {
		t.Errorf("ExitCode = %d, want ExitDone", ExitCode)
	}
	if rec.runs != 2 {
		t.Errorf("executor ran %d times, want 2", rec.runs)
	}
	out := stderr.String()
	if !strings.Contains(out, "spec hash unchanged") {
		t.Errorf("stderr missing resume marker: %q", out)
	}
	if strings.Contains(out, "[P]roceed") {
		t.Errorf("resume must skip the standard confirmation: %q", out)
	}
}

// TestRunDirectorWith_ResumeNoPlan_FallsThroughToFresh covers a user
// who passes --resume on a session with no persisted plan: there's no
// plan.json yet, so runDirectorWith plans + prompts as if --resume
// were not set.
func TestRunDirectorWith_ResumeNoPlan_FallsThroughToFresh(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	plannerCalls := 0
	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			plannerCalls++
			return scriptedPlan(), &director.DirectorCallStats{}, nil
		},
	}
	h := newTestHandler()
	newExec, _ := recordingExecutorFactory(agentcontract.SignalReview, h)
	deps := directorDeps{
		handler:     h,
		planner:     planner,
		briefer:     scriptedBriefer(h),
		reviewer:    approvingReviewer(h),
		store:       mkSessionStore(t, tmp, specPath),
		git:         newAdvancingGit(),
		newExecutor: newExec,
		now:         time.Now,
	}

	var stderr bytes.Buffer
	dio := directorIO{
		stdin:  strings.NewReader("p\n"),
		stderr: &stderr,
		resume: true,
	}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ExitCode != loop.ExitDone {
		t.Errorf("ExitCode = %d, want ExitDone", ExitCode)
	}
	if plannerCalls != 1 {
		t.Errorf("planner calls = %d, want 1 (fresh path)", plannerCalls)
	}
	out := stderr.String()
	if !strings.Contains(out, "no persisted plan") {
		t.Errorf("stderr missing fall-through marker: %q", out)
	}
	if !strings.Contains(out, "[P]roceed") {
		t.Errorf("fresh path should still prompt: %q", out)
	}
}

// TestRunDirectorWith_ResumeHashMismatch_ReplansAndProceeds covers the
// re-plan flow: spec changed since last session, planner returns a new
// plan, the diff is rendered, the user proceeds via [P], and the new
// plan is persisted (overwriting the stale one) and run to ExitDone.
func TestRunDirectorWith_ResumeHashMismatch_ReplansAndProceeds(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	stale := scriptedPlan()
	stale.SpecHash = "00000000staleHash00000000"
	store := mkSessionStore(t, tmp, specPath)
	if err := store.WritePlan(stale); err != nil {
		t.Fatalf("seed stale plan: %v", err)
	}

	plannerCalls := 0
	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			plannerCalls++
			plan := scriptedPlan()
			plan.Phases[0].Title = "Renamed phase"
			plan.Phases = append(plan.Phases, director.Phase{
				ID:        "phase-3",
				Title:     "New phase",
				Intent:    "Added in re-plan",
				DependsOn: []string{"phase-2"},
				Tasks: []director.Task{
					{
						ID:     "t1",
						Title:  "still build",
						Intent: "still compiles",
						Acceptance: []director.AcceptanceItem{
							{ID: "a3", Description: "still compiles", Evidence: director.EvidenceBuild},
						},
						Status:      director.TaskPending,
						RetryBudget: 1,
					},
				},
			})
			return plan, &director.DirectorCallStats{}, nil
		},
	}
	h := newTestHandler()
	newExec, _ := recordingExecutorFactory(agentcontract.SignalReview, h)
	deps := directorDeps{
		handler:     h,
		planner:     planner,
		briefer:     scriptedBriefer(h),
		reviewer:    approvingReviewer(h),
		store:       store,
		git:         newAdvancingGit(),
		newExecutor: newExec,
		now:         time.Now,
	}

	var stderr bytes.Buffer
	dio := directorIO{
		stdin:  strings.NewReader("p\n"),
		stderr: &stderr,
		resume: true,
	}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ExitCode != loop.ExitDone {
		t.Errorf("ExitCode = %d, want ExitDone", ExitCode)
	}
	if plannerCalls != 1 {
		t.Errorf("planner calls = %d, want 1", plannerCalls)
	}

	// Persisted plan must reflect the new content (3 phases).
	got, err := store.ReadPlan()
	if err != nil {
		t.Fatalf("read persisted plan: %v", err)
	}
	if len(got.Phases) != 3 {
		t.Errorf("persisted phases = %d, want 3 (replanned)", len(got.Phases))
	}

	out := stderr.String()
	for _, want := range []string{
		"hash diverged",
		"plan diff",
		"+ phase-3",
		"~ phase-1",
		"[D]iff",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr missing %q: %s", want, out)
		}
	}
}

// TestRunDirectorWith_ResumeHashMismatch_AbortPath covers [A]bort at
// the re-plan diff prompt: the persisted plan must NOT be overwritten,
// the loop must NOT run, ExitCode = ExitInvalid, error is the typed
// re-plan abort sentinel.
func TestRunDirectorWith_ResumeHashMismatch_AbortPath(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	stale := scriptedPlan()
	stale.SpecHash = "00000000staleHash00000000"
	store := mkSessionStore(t, tmp, specPath)
	if err := store.WritePlan(stale); err != nil {
		t.Fatalf("seed stale plan: %v", err)
	}

	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			plan := scriptedPlan()
			plan.Phases[0].Title = "Different"
			return plan, &director.DirectorCallStats{}, nil
		},
	}
	briefer := &fake.Briefer{
		BriefFn: func(_ context.Context, _ director.BrieferInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
			t.Fatal("briefer should not be called after abort")
			return nil, nil
		},
	}
	h := newTestHandler()
	deps := directorDeps{
		handler:  h,
		planner:  planner,
		briefer:  briefer,
		reviewer: &fake.Reviewer{},
		store:    store,
		git:      newAdvancingGit(),
		newExecutor: func(dag.RegisterArgs, func(string) (string, error)) loop.Executor {
			return &recordingExecutor{}
		},
		now: time.Now,
	}
	dio := directorIO{stdin: strings.NewReader("a\n"), stderr: io.Discard, resume: true}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio)
	if !errors.Is(err, errDirectorRePlanAborted) {
		t.Fatalf("err = %v, want errDirectorRePlanAborted", err)
	}
	if ExitCode != loop.ExitInvalid {
		t.Errorf("ExitCode = %d, want ExitInvalid", ExitCode)
	}

	// The stale plan must still be on disk; the abort path must not
	// have overwritten it.
	got, err := store.ReadPlan()
	if err != nil {
		t.Fatalf("read persisted plan after abort: %v", err)
	}
	if got.SpecHash != "00000000staleHash00000000" {
		t.Errorf("plan was overwritten after abort: spec_hash = %q", got.SpecHash)
	}
}

// TestRunDirectorWith_ResumeHashMismatch_AutoProceed_PersistsAndRuns
// pins --resume + --auto-proceed under a hash mismatch: no prompt is
// issued, the new plan is persisted, the loop runs to ExitDone.
func TestRunDirectorWith_ResumeHashMismatch_AutoProceed_PersistsAndRuns(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	stale := scriptedPlan()
	stale.SpecHash = "00000000staleHash00000000"
	store := mkSessionStore(t, tmp, specPath)
	if err := store.WritePlan(stale); err != nil {
		t.Fatalf("seed stale plan: %v", err)
	}

	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			return scriptedPlan(), &director.DirectorCallStats{}, nil
		},
	}
	h := newTestHandler()
	newExec, _ := recordingExecutorFactory(agentcontract.SignalReview, h)
	deps := directorDeps{
		handler:     h,
		planner:     planner,
		briefer:     scriptedBriefer(h),
		reviewer:    approvingReviewer(h),
		store:       store,
		git:         newAdvancingGit(),
		newExecutor: newExec,
		now:         time.Now,
	}
	var stderr bytes.Buffer
	dio := directorIO{
		stdin:       strings.NewReader(""),
		stderr:      &stderr,
		resume:      true,
		autoProceed: true,
	}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ExitCode != loop.ExitDone {
		t.Errorf("ExitCode = %d, want ExitDone", ExitCode)
	}
	if !strings.Contains(stderr.String(), "auto-proceed; accepting replanned") {
		t.Errorf("stderr missing auto-proceed marker: %q", stderr.String())
	}
}

// TestPromptDirectorRePlanConfirmation_Variants pins the [D]iff /
// [P]roceed / [A]bort answers and the EOF-as-abort fallback.
func TestPromptDirectorRePlanConfirmation_Variants(t *testing.T) {
	old := &director.Plan{Goal: "x", Phases: []director.Phase{{ID: "p1", Title: "T"}}}
	newPlan := &director.Plan{Goal: "y", Phases: []director.Phase{{ID: "p1", Title: "T"}}}
	diff := director.ComputePlanDiff(old, newPlan)

	cases := []struct {
		name  string
		input string
		want  confirmChoice
	}{
		{name: "p", input: "p\n", want: confirmProceed},
		{name: "uppercase A", input: "A\n", want: confirmAbort},
		{name: "diff then proceed", input: "d\np\n", want: confirmProceed},
		{name: "eof aborts", input: "", want: confirmAbort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w bytes.Buffer
			got, err := promptDirectorRePlanConfirmation(strings.NewReader(tc.input), &w, diff)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("choice = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRunCmd_ResumeFlagDefaultsOff locks --resume off so a missing
// flag never silently reuses a stale plan.
func TestRunCmd_ResumeFlagDefaultsOff(t *testing.T) {
	flag := runCmd.Flags().Lookup("resume")
	if flag == nil {
		t.Fatal("runCmd has no --resume flag")
	}
	if got := flag.DefValue; got != "false" {
		t.Errorf("--resume default = %q, want false", got)
	}
}

// TestPromptDirectorConfirmation_Variants pins the accepted answers and
// the EOF-as-abort fallback. Whitespace and case are ignored; anything
// else loops until a recognized answer.
func TestPromptDirectorConfirmation_Variants(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		want   confirmChoice
		errSub string
	}{
		{name: "lowercase p", input: "p\n", want: confirmProceed},
		{name: "uppercase P", input: "P\n", want: confirmProceed},
		{name: "yes alias", input: "yes\n", want: confirmProceed},
		{name: "lowercase a", input: "a\n", want: confirmAbort},
		{name: "uppercase A", input: "A\n", want: confirmAbort},
		{name: "no alias", input: "no\n", want: confirmAbort},
		{name: "loops then proceed", input: "what?\np\n", want: confirmProceed},
		{name: "eof aborts", input: "", want: confirmAbort},
		{name: "trims whitespace", input: "  p  \n", want: confirmProceed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w bytes.Buffer
			got, err := promptDirectorConfirmation(strings.NewReader(tc.input), &w)
			if tc.errSub != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errSub) {
					t.Fatalf("err = %v, want substring %q", err, tc.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("choice = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRunDirectorWith_AutoResolvesSession_OnFreshRun pins the new
// session-resolution behavior under the no-flag default: a run with
// no --session and no --resume creates a fresh session under
// .bcc/sessions/<id>/ and writes its manifest.
func TestRunDirectorWith_AutoResolvesSession_OnFreshRun(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			return scriptedPlan(), &director.DirectorCallStats{}, nil
		},
	}
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	h := newTestHandler()
	newExec, _ := recordingExecutorFactory(agentcontract.SignalReview, h)
	deps := directorDeps{
		handler:     h,
		planner:     planner,
		briefer:     scriptedBriefer(h),
		reviewer:    approvingReviewer(h),
		baseDir:     filepath.Join(tmp, ".bcc"),
		git:         newAdvancingGit(),
		newExecutor: newExec,
		now:         func() time.Time { return now },
	}
	dio := directorIO{stdin: strings.NewReader("p\n"), stderr: io.Discard}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ExitCode != loop.ExitDone {
		t.Errorf("ExitCode = %d, want ExitDone", ExitCode)
	}

	sessions, err := director.ListSessions(filepath.Join(tmp, ".bcc"))
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].SpecPath != specPath {
		t.Errorf("session.SpecPath = %q, want %q", sessions[0].SpecPath, specPath)
	}
	if sessions[0].Status != director.SessionDone {
		t.Errorf("session.Status = %q, want %q", sessions[0].Status, director.SessionDone)
	}
}

// TestRunDirectorWith_SessionFlag_OpensSpecificSession exercises
// `--session <id>` without `--resume`: the named session is opened and
// reused; no fresh session is created.
func TestRunDirectorWith_SessionFlag_OpensSpecificSession(t *testing.T) {
	resetExitCode(t)
	withOutputMode(t, OutputJSON)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)
	seed := mkSessionStore(t, tmp, specPath)
	persisted := scriptedPlan()
	specContent, _ := os.ReadFile(specPath)
	persisted.SpecHash = director.SpecHash(specContent)
	if err := seed.WritePlan(persisted); err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			t.Fatal("planner should not run when resuming a matching session")
			return nil, nil, nil
		},
	}
	h := newTestHandler()
	newExec, _ := recordingExecutorFactory(agentcontract.SignalReview, h)
	deps := directorDeps{
		handler:     h,
		planner:     planner,
		briefer:     scriptedBriefer(h),
		reviewer:    approvingReviewer(h),
		baseDir:     filepath.Join(tmp, ".bcc"),
		git:         newAdvancingGit(),
		newExecutor: newExec,
		now:         func() time.Time { return time.Date(2026, 5, 2, 13, 0, 0, 0, time.UTC) },
	}
	dio := directorIO{
		stdin:   strings.NewReader(""),
		stderr:  io.Discard,
		session: seed.Session().ID,
		resume:  true,
	}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ExitCode != loop.ExitDone {
		t.Errorf("ExitCode = %d, want ExitDone", ExitCode)
	}

	sessions, err := director.ListSessions(filepath.Join(tmp, ".bcc"))
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 (no fresh session)", len(sessions))
	}
	if sessions[0].ID != seed.Session().ID {
		t.Errorf("running session = %q, want %q", sessions[0].ID, seed.Session().ID)
	}
}

// TestRunDirectorWith_SessionFlag_RejectsUnknownID covers the
// `--session <id>` failure path: the user typed an id that has no
// manifest. The runner returns ErrSessionNotFound and never plans.
func TestRunDirectorWith_SessionFlag_RejectsUnknownID(t *testing.T) {
	resetExitCode(t)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)

	planner := &fake.Planner{
		PlanFn: func(_ context.Context, _ director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
			t.Fatal("planner should not run when session is not found")
			return nil, nil, nil
		},
	}
	h := newTestHandler()
	deps := directorDeps{
		handler:  h,
		planner:  planner,
		briefer:  scriptedBriefer(h),
		reviewer: &fake.Reviewer{},
		baseDir:  filepath.Join(tmp, ".bcc"),
		git:      newAdvancingGit(),
		newExecutor: func(dag.RegisterArgs, func(string) (string, error)) loop.Executor {
			return &recordingExecutor{}
		},
		now: time.Now,
	}
	dio := directorIO{
		stdin:   strings.NewReader(""),
		stderr:  io.Discard,
		session: "abcdef012345",
	}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runDirectorWith(ctx, cancel, specPath, cfg, deps, dio)
	if !errors.Is(err, director.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
	if ExitCode != loop.ExitInvalid {
		t.Errorf("ExitCode = %d, want ExitInvalid", ExitCode)
	}
}

// TestRunDirectorWith_SessionFlag_RejectsSpecMismatch covers the
// SpecPath guard: the named session was created against a different
// spec, so resuming with the new spec is refused.
func TestRunDirectorWith_SessionFlag_RejectsSpecMismatch(t *testing.T) {
	resetExitCode(t)

	tmp := t.TempDir()
	originalSpec := writeTestSpec(t, tmp)
	seed := mkSessionStore(t, tmp, originalSpec)

	otherSpec := filepath.Join(tmp, "other.md")
	if err := os.WriteFile(otherSpec, []byte("# other\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newTestHandler()
	deps := directorDeps{
		handler:  h,
		planner:  &fake.Planner{},
		briefer:  scriptedBriefer(h),
		reviewer: &fake.Reviewer{},
		baseDir:  filepath.Join(tmp, ".bcc"),
		git:      newAdvancingGit(),
		newExecutor: func(dag.RegisterArgs, func(string) (string, error)) loop.Executor {
			return &recordingExecutor{}
		},
		now: time.Now,
	}
	dio := directorIO{
		stdin:   strings.NewReader(""),
		stderr:  io.Discard,
		session: seed.Session().ID,
	}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runDirectorWith(ctx, cancel, otherSpec, cfg, deps, dio)
	if !errors.Is(err, director.ErrSessionSpecMismatch) {
		t.Fatalf("err = %v, want ErrSessionSpecMismatch", err)
	}
	if ExitCode != loop.ExitInvalid {
		t.Errorf("ExitCode = %d, want ExitInvalid", ExitCode)
	}
}

// TestRunDirectorWith_ResumeAmbiguous_ListsCandidates covers the
// `--resume` (no `--session`) path when more than one session targets
// the same spec: the runner refuses to pick and surfaces the candidate
// ids verbatim so the user can disambiguate with --session.
func TestRunDirectorWith_ResumeAmbiguous_ListsCandidates(t *testing.T) {
	resetExitCode(t)

	tmp := t.TempDir()
	specPath := writeTestSpec(t, tmp)
	a, _, err := director.CreateSession(filepath.Join(tmp, ".bcc"), specPath, "h1", time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := director.CreateSession(filepath.Join(tmp, ".bcc"), specPath, "h2", time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	h := newTestHandler()
	deps := directorDeps{
		handler:  h,
		planner:  &fake.Planner{},
		briefer:  scriptedBriefer(h),
		reviewer: &fake.Reviewer{},
		baseDir:  filepath.Join(tmp, ".bcc"),
		git:      newAdvancingGit(),
		newExecutor: func(dag.RegisterArgs, func(string) (string, error)) loop.Executor {
			return &recordingExecutor{}
		},
		now: time.Now,
	}
	dio := directorIO{
		stdin:  strings.NewReader(""),
		stderr: io.Discard,
		resume: true,
	}

	cfg := directorTestConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = runDirectorWith(ctx, cancel, specPath, cfg, deps, dio)
	if !errors.Is(err, director.ErrSessionAmbiguous) {
		t.Fatalf("err = %v, want ErrSessionAmbiguous", err)
	}
	if !strings.Contains(err.Error(), a.Session().ID) || !strings.Contains(err.Error(), b.Session().ID) {
		t.Errorf("error message missing both candidate ids: %v", err)
	}
}
