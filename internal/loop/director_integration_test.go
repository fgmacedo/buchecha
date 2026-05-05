package loop_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func directorNewExecutor(h *dag.Handler, signal agentcontract.Signal, runs *directorFakeRuns) func(dag.RegisterArgs, func(string) (string, error), *director.RoleAssignment) loop.Executor {
	return func(args dag.RegisterArgs, renderSystem func(string) (string, error), _ *director.RoleAssignment) loop.Executor {
		if renderSystem != nil {
			if _, err := renderSystem("integration-executor-agent"); err != nil {
				return &failingDirectorExec{err: err}
			}
		}
		return &directorFakeExec{signal: signal, handler: h, args: args, runs: runs}
	}
}

// failingDirectorExec mirrors the production failingExecutor: when the
// integration test's renderSystem callback fails, the factory returns a
// loop.Executor whose Run surfaces the error, letting the loop driver
// abort with the expected fatal exit code.
type failingDirectorExec struct{ err error }

func (e *failingDirectorExec) Run(_ context.Context, _ string, _ chan<- agentcontract.AgentEvent) (loop.ExecResult, error) {
	return loop.ExecResult{ExitCode: -1}, e.err
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

// TestDirectorIntegration_NoCommitGoesToReviewer verifies that an
// iteration where HEAD does not advance is no longer aborted by the
// loop. The Reviewer audits the empty diff and decides; an approving
// reviewer with all sub-DAG tasks marked done lets the run complete
// successfully (the no-op iteration is treated as legitimate).
func TestDirectorIntegration_NoCommitGoesToReviewer(t *testing.T) {
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
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want ExitDone (reviewer approved no-op iteration)", code)
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

// countingBriefer wraps a real Briefer and increments a counter on
// every call, so tests can assert the loop did or did not invoke it.
type countingBriefer struct {
	calls int
	mu    sync.Mutex
	inner director.Briefer
}

func (b *countingBriefer) Brief(ctx context.Context, in director.BrieferInput, events chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	return b.inner.Brief(ctx, in, events)
}

func (b *countingBriefer) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// countingReviewer wraps a real Reviewer and increments a counter on
// every call so tests can assert the loop did or did not invoke it.
type countingReviewer struct {
	calls int
	mu    sync.Mutex
	inner director.Reviewer
}

func (r *countingReviewer) Review(ctx context.Context, in director.ReviewerInput, events chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return r.inner.Review(ctx, in, events)
}

func (r *countingReviewer) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestDirectorIntegration_SkipReviewBypassesReviewer pins the Planner-
// driven SkipReview path: when Phase.SkipReview is true the loop must
// not spawn the Reviewer agent and the iteration must advance with a
// synthetic approval. The reviewer wraps the real approver only to
// count calls; a non-zero count fails the test.
func TestDirectorIntegration_SkipReviewBypassesReviewer(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	plan := directorTestPlan()
	plan.SpecHash = "deadbeef"
	plan.Phases[0].SkipReview = true
	plan.Phases[1].SkipReview = true
	if err := store.WritePlan(plan); err != nil {
		t.Fatal(err)
	}

	h := directorTestHandler(plan)
	runsCounter := &directorFakeRuns{}
	cr := &countingReviewer{inner: approvingReviewer(t, h)}
	cfg := newTestConfig()

	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      &directorAdvancingGit{},
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     briefingEmitter(t, h),
			Reviewer:    cr,
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
	if cr.callCount() != 0 {
		t.Errorf("Reviewer was called %d times; expected 0 with SkipReview on every phase", cr.callCount())
	}
	for _, ev := range evs {
		if rev, ok := ev.(loop.PhaseReviewed); ok {
			if rev.Outcome != "approve" {
				t.Errorf("phase %s: synthetic outcome = %q, want approve", rev.PhaseID, rev.Outcome)
			}
		}
	}
}

// TestDirectorIntegration_PreparedBriefingSkipsBriefer pins the
// Planner-prepared briefing path: when Phase.PreparedBriefing is set,
// the loop must not spawn the Briefer agent for that phase. The
// Briefer in this test wraps the real emitter only to count calls; if
// it were called the count would be > 0.
func TestDirectorIntegration_PreparedBriefingSkipsBriefer(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	plan := directorTestPlan()
	plan.SpecHash = "deadbeef"
	plan.Phases[0].PreparedBriefing = &director.PreparedBriefing{
		SubDAGTaskIDs: []string{"t1"},
		Instructions:  "ship it",
	}
	plan.Phases[1].PreparedBriefing = &director.PreparedBriefing{
		SubDAGTaskIDs: []string{"t1"},
		Instructions:  "ship it again",
	}
	if err := store.WritePlan(plan); err != nil {
		t.Fatal(err)
	}

	h := directorTestHandler(plan)
	runsCounter := &directorFakeRuns{}
	cb := &countingBriefer{inner: briefingEmitter(t, h)}
	cfg := newTestConfig()

	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      &directorAdvancingGit{},
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     cb,
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
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want ExitDone", code)
	}
	if cb.callCount() != 0 {
		t.Errorf("Briefer was called %d times; expected 0 with PreparedBriefing on every phase", cb.callCount())
	}
	// And the synthetic briefings must be present on the handler so the
	// Executor + Reviewer could read them.
	if got := h.Briefing("phase-1-01"); got == nil || got.Instructions != "ship it" {
		t.Errorf("phase-1 synthetic briefing not stored: %+v", got)
	}
	if got := h.Briefing("phase-2-01"); got == nil || got.Instructions != "ship it again" {
		t.Errorf("phase-2 synthetic briefing not stored: %+v", got)
	}
}

// TestDirectorIntegration_StatsLogPersistsRoles pins the
// per-spawn telemetry contract: when DirectorPorts.Stats is wired,
// every Briefer / Reviewer call (and Executor result summary, when
// the agent emits one) lands as a StatsEntry in the stats log,
// tagged with phase_id and iteration_id. The Planner is recorded by
// the cli boot, not the loop, so it is not asserted here.
func TestDirectorIntegration_StatsLogPersistsRoles(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	plan := directorTestPlan()
	plan.SpecHash = "deadbeef"
	if err := store.WritePlan(plan); err != nil {
		t.Fatal(err)
	}

	h := directorTestHandler(plan)
	statsPath := filepath.Join(t.TempDir(), "stats.jsonl")
	statsLog := director.NewStatsLog(statsPath)

	statsBriefer := &fake.Briefer{
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
			return &director.DirectorCallStats{
				DurationMS: 800, CostUSD: 0.012,
				InputTokens: 900, OutputTokens: 400,
			}, nil
		},
	}
	statsReviewer := &fake.Reviewer{
		ReviewFn: func(ctx context.Context, in director.ReviewerInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
			for _, tid := range in.SubDAG {
				if _, err := h.HandleCall(ctx, string(dag.RoleReviewer), dag.MethodTaskApproved, map[string]any{
					"agent_id": in.AgentID, "id": tid,
				}); err != nil {
					return nil, err
				}
			}
			if _, err := h.HandleCall(ctx, string(dag.RoleReviewer), dag.MethodReviewFinished, map[string]any{
				"agent_id": in.AgentID, "outcome": "approve", "reasoning": "ok",
			}); err != nil {
				return nil, err
			}
			return &director.DirectorCallStats{
				DurationMS: 1200, CostUSD: 0.022,
				InputTokens: 1400, OutputTokens: 700,
			}, nil
		},
	}

	cfg := newTestConfig()
	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      &directorAdvancingGit{},
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     statsBriefer,
			Reviewer:    statsReviewer,
			Store:       store,
			Handler:     h,
			NewExecutor: directorNewExecutor(h, agentcontract.SignalReview, &directorFakeRuns{}),
			Stats:       statsLog,
		},
	}
	code, err, _ := runDirectorLoop(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want ExitDone", code)
	}
	if err := statsLog.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	body, err := os.ReadFile(statsPath)
	if err != nil {
		t.Fatalf("read stats.jsonl: %v", err)
	}
	roleCounts := map[string]int{}
	taggedIters := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
		if line == "" {
			continue
		}
		var e director.StatsEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unmarshal stats line %q: %v", line, err)
		}
		roleCounts[e.Role]++
		if e.IterationID != "" {
			taggedIters[e.Role] = e.IterationID
		}
	}
	if roleCounts[string(dag.RoleBriefer)] != 2 {
		t.Errorf("briefer entries = %d, want 2 (one per phase)", roleCounts[string(dag.RoleBriefer)])
	}
	if roleCounts[string(dag.RoleReviewer)] != 2 {
		t.Errorf("reviewer entries = %d, want 2 (one per phase)", roleCounts[string(dag.RoleReviewer)])
	}
	if taggedIters[string(dag.RoleBriefer)] == "" {
		t.Error("briefer entries missing iteration_id tag")
	}
	if taggedIters[string(dag.RoleReviewer)] == "" {
		t.Error("reviewer entries missing iteration_id tag")
	}
}

// TestDirectorIntegration_PhaseSubDAGSequence pins the iteration_id
// uniqueness contract: when a phase has multiple pending tasks and the
// Briefer picks them across successive iterations (subset-then-rest),
// each iteration must get a distinct iteration_id (suffix -01, -02,
// ...) and the handler must retain the per-iteration briefing record
// without overwriting the previous one. Regression test for the
// pre-fix bug where every iteration of the same phase reused
// "<phase>-1" and the second briefing silently overwrote the first.
func TestDirectorIntegration_PhaseSubDAGSequence(t *testing.T) {
	tmp := t.TempDir()
	specPath := directorTestSpec(t, tmp)
	store := directorTestStore(t, tmp, specPath)
	plan := &director.Plan{
		Goal:     "subset coverage",
		SpecHash: "deadbeef",
		Phases: []director.Phase{{
			ID: "phase-1", Title: "First", Intent: "do thing",
			Tasks: []director.Task{
				{ID: "t1", Title: "a", Intent: "a", Status: director.TaskPending,
					Acceptance:  []director.AcceptanceItem{{ID: "a1", Description: "a", Evidence: director.EvidenceBuild}},
					RetryBudget: 2},
				{ID: "t2", Title: "b", Intent: "b", Status: director.TaskPending,
					Acceptance:  []director.AcceptanceItem{{ID: "a2", Description: "b", Evidence: director.EvidenceBuild}},
					RetryBudget: 2},
				{ID: "t3", Title: "c", Intent: "c", Status: director.TaskPending,
					Acceptance:  []director.AcceptanceItem{{ID: "a3", Description: "c", Evidence: director.EvidenceBuild}},
					RetryBudget: 2},
			},
		}},
	}
	if err := store.WritePlan(plan); err != nil {
		t.Fatal(err)
	}

	h := directorTestHandler(plan)

	// Subset briefer: first call picks {t1}, subsequent calls pick the
	// remaining pending tasks. Each call writes a Briefing through the
	// handler, exactly mirroring the production wire.
	var brieferCalls int
	subsetBriefer := &fake.Briefer{
		BriefFn: func(ctx context.Context, in director.BrieferInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
			brieferCalls++
			var picked []string
			if brieferCalls == 1 {
				picked = []string{"t1"}
			} else {
				picked = append(picked, in.SubDAGTaskIDs...)
			}
			ids := make([]any, len(picked))
			for i, s := range picked {
				ids[i] = s
			}
			input := map[string]any{
				"agent_id": in.AgentID,
				"briefing": map[string]any{
					"iteration_id":     in.IterationID,
					"phase_id":         in.PhaseID,
					"sub_dag_task_ids": ids,
					"spec_path":        in.SpecPath,
					"instructions":     fmt.Sprintf("call %d covers %v", brieferCalls, picked),
				},
			}
			if _, err := h.HandleCall(ctx, string(dag.RoleBriefer), dag.MethodBriefingEmit, input); err != nil {
				return nil, err
			}
			return &director.DirectorCallStats{}, nil
		},
	}

	cfg := newTestConfig()
	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      &directorAdvancingGit{},
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     subsetBriefer,
			Reviewer:    approvingReviewer(t, h),
			Store:       store,
			Handler:     h,
			NewExecutor: directorNewExecutor(h, agentcontract.SignalReview, &directorFakeRuns{}),
		},
	}

	code, err, evs := runDirectorLoop(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want ExitDone", code)
	}
	if brieferCalls < 2 {
		t.Fatalf("Briefer was called %d times; expected at least 2 (subset then rest)", brieferCalls)
	}

	var briefedIDs []string
	for _, ev := range evs {
		if e, ok := ev.(loop.PhaseBriefed); ok && e.Briefing != nil {
			briefedIDs = append(briefedIDs, e.Briefing.IterationID)
		}
	}
	if len(briefedIDs) < 2 {
		t.Fatalf("expected ≥2 PhaseBriefed events; got %d (%v)", len(briefedIDs), briefedIDs)
	}
	if briefedIDs[0] != "phase-1-01" || briefedIDs[1] != "phase-1-02" {
		t.Errorf("PhaseBriefed iteration_ids = %v, want [phase-1-01 phase-1-02 ...]", briefedIDs)
	}

	// The handler must retain both briefings; the second emit must not
	// have overwritten the first.
	first := h.Briefing("phase-1-01")
	second := h.Briefing("phase-1-02")
	if first == nil || second == nil {
		t.Fatalf("handler dropped a briefing: first=%+v second=%+v", first, second)
	}
	if first.Instructions == second.Instructions {
		t.Errorf("first and second briefings carry identical instructions; they should differ:\n first=%q\nsecond=%q",
			first.Instructions, second.Instructions)
	}
	if len(first.SubDAGTaskIDs) == 0 || first.SubDAGTaskIDs[0] != "t1" {
		t.Errorf("first iteration sub_dag = %v, want [t1]", first.SubDAGTaskIDs)
	}
}
