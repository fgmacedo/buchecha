package loop_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/director/dag"
	"github.com/fgmacedo/buchecha/internal/director/fake"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// directorTestPlan returns a small but representative two-phase Plan
// with explicit RetryBudgets: phase-1 allows two retries, phase-2 one.
func directorTestPlan() *director.Plan {
	return &director.Plan{
		Goal: "Director loop coverage",
		SuccessCriteria: []string{
			"phases run sequentially",
			"per-task DAG state drives the loop",
		},
		Phases: []director.Phase{
			{
				ID:     "phase-1",
				Title:  "First",
				Intent: "do thing 1",
				Tasks: []director.Task{
					{
						ID:     "t1",
						Title:  "build",
						Intent: "compiles",
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
				Title:     "Second",
				Intent:    "do thing 2",
				DependsOn: []string{"phase-1"},
				Tasks: []director.Task{
					{
						ID:     "t1",
						Title:  "test",
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

type directorAdvancingGit struct {
	mu  sync.Mutex
	idx int
}

func (g *directorAdvancingGit) HeadSHA(_ context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.idx++
	if g.idx%2 == 1 {
		return "baseSHA", nil
	}
	return "headSHA", nil
}
func (g *directorAdvancingGit) CurrentBranch(_ context.Context) (string, error) {
	return "main", nil
}
func (g *directorAdvancingGit) IsClean(_ context.Context) (bool, error) { return true, nil }
func (g *directorAdvancingGit) Diff(_ context.Context, _, _ string) (string, error) {
	return "diff body\n", nil
}

type directorStuckGit struct{}

func (directorStuckGit) HeadSHA(_ context.Context) (string, error)       { return "stuckSHA", nil }
func (directorStuckGit) CurrentBranch(_ context.Context) (string, error) { return "main", nil }
func (directorStuckGit) IsClean(_ context.Context) (bool, error)         { return true, nil }
func (directorStuckGit) Diff(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

// directorFakeRuns counts executor invocations across NewExecutor calls
// for assertions. Each NewExecutor call returns a fresh directorFakeExec
// bound to one set of RegisterArgs.
type directorFakeRuns struct {
	mu sync.Mutex
	n  int
}

func (r *directorFakeRuns) inc() {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
}
func (r *directorFakeRuns) get() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

type directorFakeExec struct {
	signal  agentcontract.Signal
	handler *dag.Handler
	args    dag.RegisterArgs
	runs    *directorFakeRuns
}

func (e *directorFakeExec) Run(ctx context.Context, _ string, events chan<- agentcontract.AgentEvent) (loop.ExecResult, error) {
	if e.runs != nil {
		e.runs.inc()
	}
	if e.handler != nil {
		id, err := e.handler.Registry().Register(dag.RoleExecutor, e.args)
		if err != nil {
			return loop.ExecResult{ExitCode: -1}, err
		}
		// Mirror the production agent: report the iteration's signal
		// through the MCP handler so the loop driver picks it up via
		// IterationSignal(briefingID). Then deregister.
		if e.signal != agentcontract.SignalUnknown {
			signalStr := e.signal.String()
			_, _ = e.handler.HandleCall(ctx, string(dag.RoleExecutor), dag.MethodIterationFinished, map[string]any{
				"agent_id": string(id),
				"signal":   signalStr,
			})
		}
		e.handler.Registry().Deregister(id)
	}
	_ = time.Now
	_ = events
	return loop.ExecResult{ExitCode: 0}, nil
}

func directorNewExecutor(h *dag.Handler, signal agentcontract.Signal, runs *directorFakeRuns) func(string, dag.RegisterArgs) loop.Executor {
	return func(_ string, args dag.RegisterArgs) loop.Executor {
		return &directorFakeExec{signal: signal, handler: h, args: args, runs: runs}
	}
}

// briefingEmitter wires a fake.Briefer that emits a Briefing for the
// loop's iteration via handler.HandleCall, mirroring what the production
// claude adapter would do over MCP.
func briefingEmitter(t *testing.T, h *dag.Handler) director.Briefer {
	t.Helper()
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
					"instructions":     "fake briefing",
				},
			}
			if _, err := h.HandleCall(ctx, string(dag.RoleBriefer), dag.MethodBriefingEmit, input); err != nil {
				return nil, err
			}
			return &director.DirectorCallStats{}, nil
		},
	}
}

// approvingReviewer marks every sub-DAG task done and finalises with
// outcome=approve.
func approvingReviewer(t *testing.T, h *dag.Handler) director.Reviewer {
	t.Helper()
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
				"reasoning": "ok",
			})
			return &director.DirectorCallStats{}, err
		},
	}
}

// alwaysReviseReviewer marks every sub-DAG task needs_fix and finalises
// with outcome=revise.
func alwaysReviseReviewer(t *testing.T, h *dag.Handler) director.Reviewer {
	t.Helper()
	return &fake.Reviewer{
		ReviewFn: func(ctx context.Context, in director.ReviewerInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
			for _, tid := range in.SubDAG {
				if _, err := h.HandleCall(ctx, string(dag.RoleReviewer), dag.MethodTaskNeedsFix, map[string]any{
					"agent_id": in.AgentID,
					"id":       tid,
					"feedback": "still not right",
				}); err != nil {
					return nil, err
				}
			}
			_, err := h.HandleCall(ctx, string(dag.RoleReviewer), dag.MethodReviewFinished, map[string]any{
				"agent_id":  in.AgentID,
				"outcome":   "revise",
				"reasoning": "still not right",
			})
			return &director.DirectorCallStats{}, err
		},
	}
}

func directorTestSpec(t *testing.T, dir string) string {
	t.Helper()
	specPath := filepath.Join(dir, "spec.md")
	body := []byte("# spec\n\n## Implementation Plan\n\n## Execution Journal\n")
	if err := os.WriteFile(specPath, body, 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return specPath
}

func directorTestStore(t *testing.T, tmp, specPath string) *director.Store {
	t.Helper()
	store, _, err := director.CreateSession(filepath.Join(tmp, ".bcc"), specPath, "deadbeef", time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return store
}

// directorTestHandler returns a handler bound to a fresh state derived
// from plan, with no audit/persister wired (tests don't need them).
func directorTestHandler(plan *director.Plan) *dag.Handler {
	state := dag.NewStateFromPlan(plan)
	registry := dag.NewAgentRegistry(nil)
	h := dag.NewHandler(state, registry)
	return h
}

func runDirectorLoop(t *testing.T, l *loop.Loop) (int, error, []loop.Event) {
	t.Helper()
	events := make(chan loop.Event, 1024)
	code, err := l.Run(context.Background(), events)
	var got []loop.Event
	for ev := range events {
		got = append(got, ev)
	}
	return code, err, got
}

// TestDirectorIntegration_ApproveBothPhases drives the happy path: two
// phases, both approve on attempt 1, the loop exits with ExitDone.
func TestDirectorIntegration_ApproveBothPhases(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	plan := directorTestPlan()
	plan.SpecHash = "deadbeef"
	if err := store.WritePlan(plan); err != nil {
		t.Fatal(err)
	}

	h := directorTestHandler(plan)
	runsCounter := &directorFakeRuns{}
	cfg := newTestConfig()

	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      &directorAdvancingGit{},
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     briefingEmitter(t, h),
			Reviewer:    approvingReviewer(t, h),
			Store:       store,
			Handler:     h,
			NewExecutor: directorNewExecutor(h, agentcontract.SignalReview, runsCounter),
		},
	}

	code, err, evs := runDirectorLoop(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want ExitDone", code)
	}

	var briefed, reviewed []string
	for _, ev := range evs {
		switch e := ev.(type) {
		case loop.PhaseBriefed:
			briefed = append(briefed, e.PhaseID)
		case loop.PhaseReviewed:
			reviewed = append(reviewed, e.PhaseID)
			if e.Outcome != "approve" {
				t.Errorf("PhaseReviewed for %s outcome = %q, want approve", e.PhaseID, e.Outcome)
			}
		}
	}
	if want := []string{"phase-1", "phase-2"}; !equalStrings(briefed, want) {
		t.Errorf("briefed = %v, want %v", briefed, want)
	}
	if want := []string{"phase-1", "phase-2"}; !equalStrings(reviewed, want) {
		t.Errorf("reviewed = %v, want %v", reviewed, want)
	}
}

// TestDirectorIntegration_RetryBudgetExhausted: the reviewer always
// returns revise; the loop retries until the budget is spent and then
// escalates. With Escalation=nil, the loop aborts.
func TestDirectorIntegration_RetryBudgetExhausted(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	plan := directorTestPlan()
	plan.Phases[0].Tasks[0].RetryBudget = 1
	plan.SpecHash = "deadbeef"
	if err := store.WritePlan(plan); err != nil {
		t.Fatal(err)
	}

	h := directorTestHandler(plan)
	runsCounter := &directorFakeRuns{}
	cfg := newTestConfig()
	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      &directorAdvancingGit{},
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     briefingEmitter(t, h),
			Reviewer:    alwaysReviseReviewer(t, h),
			Store:       store,
			Handler:     h,
			NewExecutor: directorNewExecutor(h, agentcontract.SignalReview, runsCounter),
		},
	}
	code, err, evs := runDirectorLoop(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitInvalid {
		t.Errorf("exit = %d, want ExitInvalid", code)
	}
	var sawEscalation bool
	for _, ev := range evs {
		if e, ok := ev.(loop.DirectorEscalation); ok {
			sawEscalation = true
			if e.PhaseID != "phase-1" {
				t.Errorf("escalation phase = %q, want phase-1", e.PhaseID)
			}
		}
	}
	if !sawEscalation {
		t.Error("no DirectorEscalation event emitted")
	}
}

// TestDirectorIntegration_HEADStuck covers the HEAD-stuck guard.
func TestDirectorIntegration_HEADStuck(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	plan := directorTestPlan()
	plan.SpecHash = "deadbeef"
	if err := store.WritePlan(plan); err != nil {
		t.Fatal(err)
	}

	h := directorTestHandler(plan)
	runsCounter := &directorFakeRuns{}
	cfg := newTestConfig()
	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      directorStuckGit{},
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     briefingEmitter(t, h),
			Reviewer:    approvingReviewer(t, h),
			Store:       store,
			Handler:     h,
			NewExecutor: directorNewExecutor(h, agentcontract.SignalReview, runsCounter),
		},
	}
	code, err, _ := runDirectorLoop(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitHEADStuck {
		t.Errorf("exit = %d, want ExitHEADStuck", code)
	}
}

// TestDirectorIntegration_BlockedSignal covers the executor explicitly
// signaling blocked: the loop terminates with ExitBlocked without
// invoking the Reviewer.
func TestDirectorIntegration_BlockedSignal(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	plan := directorTestPlan()
	plan.SpecHash = "deadbeef"
	if err := store.WritePlan(plan); err != nil {
		t.Fatal(err)
	}

	h := directorTestHandler(plan)
	reviewerCalls := 0
	reviewer := &fake.Reviewer{
		ReviewFn: func(_ context.Context, _ director.ReviewerInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
			reviewerCalls++
			return nil, errors.New("reviewer must not be called when executor blocks")
		},
	}
	runsCounter := &directorFakeRuns{}
	cfg := newTestConfig()
	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      &directorAdvancingGit{},
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     briefingEmitter(t, h),
			Reviewer:    reviewer,
			Store:       store,
			Handler:     h,
			NewExecutor: directorNewExecutor(h, agentcontract.SignalBlocked, runsCounter),
		},
	}
	code, err, _ := runDirectorLoop(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitBlocked {
		t.Errorf("exit = %d, want ExitBlocked", code)
	}
	if reviewerCalls != 0 {
		t.Errorf("reviewer called %d times on blocked signal, want 0", reviewerCalls)
	}
}

// TestDirectorIntegration_SessionLifecycle_RunningAtStart pins the
// session manifest's initial state.
func TestDirectorIntegration_SessionLifecycle_RunningAtStart(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	if got := store.Session().Status; got != director.SessionRunning {
		t.Errorf("freshly created session status = %q, want %q", got, director.SessionRunning)
	}
}

// TestDirectorIntegration_SessionLifecycle_EscalationMarksPending
// covers the loop hook: when an iteration escalates, the loop calls
// Touch(SessionEscalatedPending) before blocking on the gate.
func TestDirectorIntegration_SessionLifecycle_EscalationMarksPending(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	plan := directorTestPlan()
	plan.Phases[0].Tasks[0].RetryBudget = 1
	plan.SpecHash = "deadbeef"
	if err := store.WritePlan(plan); err != nil {
		t.Fatal(err)
	}

	h := directorTestHandler(plan)
	runsCounter := &directorFakeRuns{}

	gate := make(chan loop.EscalationReply, 1)
	observed := make(chan director.SessionStatus, 1)
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		for {
			reopened, err := director.OpenSession(filepath.Join(tmp, ".bcc"), store.Session().ID)
			if err == nil && reopened.Session().Status == director.SessionEscalatedPending {
				observed <- reopened.Session().Status
				gate <- loop.EscalationReply{Kind: loop.EscalationAbort}
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	cfg := newTestConfig()
	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      &directorAdvancingGit{},
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     briefingEmitter(t, h),
			Reviewer:    alwaysReviseReviewer(t, h),
			Store:       store,
			Handler:     h,
			NewExecutor: directorNewExecutor(h, agentcontract.SignalReview, runsCounter),
			Escalation:  gate,
		},
	}

	code, err, _ := runDirectorLoop(t, l)
	<-probeDone
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitInvalid {
		t.Errorf("exit = %d, want ExitInvalid", code)
	}
	select {
	case got := <-observed:
		if got != director.SessionEscalatedPending {
			t.Fatalf("observed status = %q, want %q", got, director.SessionEscalatedPending)
		}
	default:
		t.Fatal("session status was never observed as escalated_pending")
	}
}

// TestDirectorIntegration_EscalateResumeWithHint covers acceptance (a)
// of P7: when the user picks Resume with a hint, the next outer
// iteration's briefing prompt contains the hint block. The reviewer
// always revises until the user resumes; on the second outer iter the
// reviewer approves so the run finishes deterministically.
func TestDirectorIntegration_EscalateResumeWithHint(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	plan := &director.Plan{
		Goal: "hint propagation",
		Phases: []director.Phase{{
			ID: "phase-1", Title: "First", Intent: "do thing 1",
			Tasks: []director.Task{{
				ID: "t1", Title: "build", Intent: "compiles",
				Acceptance: []director.AcceptanceItem{
					{ID: "a1", Description: "compiles", Evidence: director.EvidenceBuild},
				},
				Status:      director.TaskPending,
				RetryBudget: 0,
			}},
		}},
		SpecHash: "deadbeef",
	}
	if err := store.WritePlan(plan); err != nil {
		t.Fatal(err)
	}

	h := directorTestHandler(plan)
	gate := make(chan loop.EscalationReply, 1)
	gate <- loop.EscalationReply{Kind: loop.EscalationResume, Hint: "tighten the diff"}

	outerIter := 0
	reviewer := &fake.Reviewer{
		ReviewFn: func(ctx context.Context, in director.ReviewerInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
			outerIter++
			if outerIter < 2 {
				for _, tid := range in.SubDAG {
					if _, err := h.HandleCall(ctx, string(dag.RoleReviewer), dag.MethodTaskNeedsFix, map[string]any{
						"agent_id": in.AgentID, "id": tid, "feedback": "no",
					}); err != nil {
						return nil, err
					}
				}
				_, err := h.HandleCall(ctx, string(dag.RoleReviewer), dag.MethodReviewFinished, map[string]any{
					"agent_id": in.AgentID, "outcome": "revise", "reasoning": "no",
				})
				return &director.DirectorCallStats{}, err
			}
			for _, tid := range in.SubDAG {
				if _, err := h.HandleCall(ctx, string(dag.RoleReviewer), dag.MethodTaskApproved, map[string]any{
					"agent_id": in.AgentID, "id": tid,
				}); err != nil {
					return nil, err
				}
			}
			_, err := h.HandleCall(ctx, string(dag.RoleReviewer), dag.MethodReviewFinished, map[string]any{
				"agent_id": in.AgentID, "outcome": "approve", "reasoning": "ok",
			})
			return &director.DirectorCallStats{}, err
		},
	}

	cfg := newTestConfig()
	cfg.Director.RetryBudget = 0
	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      &directorAdvancingGit{},
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     briefingEmitter(t, h),
			Reviewer:    reviewer,
			Store:       store,
			Handler:     h,
			NewExecutor: directorNewExecutor(h, agentcontract.SignalReview, &directorFakeRuns{}),
			Escalation:  gate,
		},
	}
	code, err, _ := runDirectorLoop(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want ExitDone", code)
	}

	briefingsDir := filepath.Join(store.SessionDir(), "briefings")
	entries, err := os.ReadDir(briefingsDir)
	if err != nil {
		t.Fatalf("read briefings dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no briefing prompts written")
	}
	hintSeen := false
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join(briefingsDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.Contains(string(body), "tighten the diff") &&
			strings.Contains(string(body), "User hint (escalation)") {
			hintSeen = true
		}
	}
	if !hintSeen {
		t.Errorf("no briefing prompt carried the user hint block")
	}
}

// TestDirectorIntegration_EscalateForceApprove covers acceptance (b)
// and (c) of P7: force-approve marks every still-pending sub-DAG task
// as done, the loop advances, and on the last iteration the run
// terminates with ExitDone.
func TestDirectorIntegration_EscalateForceApprove(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	plan := &director.Plan{
		Goal: "force approve last iter",
		Phases: []director.Phase{{
			ID: "phase-1", Title: "First", Intent: "do thing 1",
			Tasks: []director.Task{{
				ID: "t1", Title: "build", Intent: "compiles",
				Acceptance: []director.AcceptanceItem{
					{ID: "a1", Description: "compiles", Evidence: director.EvidenceBuild},
				},
				Status:      director.TaskPending,
				RetryBudget: 0,
			}},
		}},
		SpecHash: "deadbeef",
	}
	if err := store.WritePlan(plan); err != nil {
		t.Fatal(err)
	}

	h := directorTestHandler(plan)
	gate := make(chan loop.EscalationReply, 1)
	gate <- loop.EscalationReply{Kind: loop.EscalationForceApprove}

	cfg := newTestConfig()
	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      &directorAdvancingGit{},
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     briefingEmitter(t, h),
			Reviewer:    alwaysReviseReviewer(t, h),
			Store:       store,
			Handler:     h,
			NewExecutor: directorNewExecutor(h, agentcontract.SignalReview, &directorFakeRuns{}),
			Escalation:  gate,
		},
	}
	code, err, _ := runDirectorLoop(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want ExitDone", code)
	}
	state := h.State()
	for _, tid := range []dag.TaskID{"t1"} {
		stats := state.SubDAGStatuses("phase-1", []dag.TaskID{tid})
		if got := stats[tid]; got != director.TaskDone {
			t.Errorf("task %s status = %q, want done", tid, got)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
