package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/director/dag"
)

func TestFormatExecutorCrash(t *testing.T) {
	cases := []struct {
		name        string
		result      ExecResult
		iterationID string
		wantSubs    []string
		wantNoSubs  []string
	}{
		{
			name:        "no debug, no tail",
			result:      ExecResult{ExitCode: 1},
			iterationID: "P7-01",
			wantSubs: []string{
				"director: executor (iteration P7-01) exited 1 with no terminal signal",
				"hint: rerun with --debug-logs",
			},
			wantNoSubs: []string{"agent ", "last stderr", "full output at"},
		},
		{
			name: "with agent and tail, no log path",
			result: ExecResult{
				ExitCode:   42,
				AgentID:    "bcc-executor-abc123",
				StderrTail: "auth: token expired",
			},
			iterationID: "P3-02",
			wantSubs: []string{
				"director: executor (iteration P3-02, agent bcc-executor-abc123) exited 42 with no terminal signal",
				"last stderr: auth: token expired",
				"hint: rerun with --debug-logs",
			},
			wantNoSubs: []string{"full output at"},
		},
		{
			name: "debug capture on, hint suppressed",
			result: ExecResult{
				ExitCode:      1,
				AgentID:       "bcc-executor-xyz",
				StderrLogPath: "/tmp/.bcc/sessions/abc/runs/P1-01/bcc-executor-xyz.stderr.log",
			},
			iterationID: "P1-01",
			wantSubs: []string{
				"agent bcc-executor-xyz",
				"full output at: /tmp/.bcc/sessions/abc/runs/P1-01/bcc-executor-xyz.stderr.log",
			},
			wantNoSubs: []string{"hint: rerun"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := formatExecutorCrash(tc.result, tc.iterationID)
			if err == nil {
				t.Fatal("formatExecutorCrash returned nil")
			}
			msg := err.Error()
			for _, sub := range tc.wantSubs {
				if !strings.Contains(msg, sub) {
					t.Errorf("missing %q in:\n%s", sub, msg)
				}
			}
			for _, sub := range tc.wantNoSubs {
				if strings.Contains(msg, sub) {
					t.Errorf("unexpected %q in:\n%s", sub, msg)
				}
			}
		})
	}
}

func TestTaskEventBridge_TranslatesTaskMethods(t *testing.T) {
	plan := &director.Plan{
		Goal: "bridge test",
		Phases: []director.Phase{{
			ID: "P1", Title: "p1", Intent: "p1",
			Tasks: []director.Task{
				{
					ID: "t1", Title: "task one", Intent: "intent",
					Acceptance: []director.AcceptanceItem{
						{ID: "a1", Description: "d", Evidence: director.EvidenceTest},
					},
					Status:      director.TaskPending,
					RetryBudget: 1,
				},
				{
					ID: "t2", Title: "task two", Intent: "intent",
					Acceptance: []director.AcceptanceItem{
						{ID: "a1", Description: "d", Evidence: director.EvidenceTest},
					},
					Status:      director.TaskPending,
					RetryBudget: 1,
				},
			},
		}},
	}
	state := dag.NewStateFromPlan(plan)
	registry := dag.NewAgentRegistry(nil)
	h := dag.NewHandler(state, registry)
	h.SetPlan(plan)

	exec, err := registry.Register(dag.RoleExecutor, dag.RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []dag.TaskID{"t1", "t2"},
	})
	if err != nil {
		t.Fatalf("register executor: %v", err)
	}
	rev, err := registry.Register(dag.RoleReviewer, dag.RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []dag.TaskID{"t1", "t2"},
	})
	if err != nil {
		t.Fatalf("register reviewer: %v", err)
	}

	events := make(chan Event, 16)
	bridge := &taskEventBridge{events: events, reg: registry}
	h.AttachObserver(bridge)

	ctx := context.Background()
	mustCall := func(role, method string, in map[string]any) {
		t.Helper()
		if _, err := h.HandleCall(ctx, role, method, in); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	mustCall(string(dag.RoleExecutor), dag.MethodTaskStarted, map[string]any{
		"agent_id": string(exec), "id": "t1",
	})
	mustCall(string(dag.RoleExecutor), dag.MethodTaskCompleted, map[string]any{
		"agent_id": string(exec), "id": "t1",
	})
	mustCall(string(dag.RoleReviewer), dag.MethodTaskApproved, map[string]any{
		"agent_id": string(rev), "id": "t1",
	})
	mustCall(string(dag.RoleReviewer), dag.MethodTaskNeedsFix, map[string]any{
		"agent_id": string(rev), "id": "t2", "feedback": "missing assertion",
	})
	// Non-task method must not produce an event.
	mustCall(string(dag.RoleExecutor), dag.MethodGetPendingTasks, map[string]any{
		"agent_id": string(exec),
	})

	close(events)
	var got []Event
	for ev := range events {
		got = append(got, ev)
	}
	if len(got) != 4 {
		t.Fatalf("events emitted = %d, want 4", len(got))
	}

	if e, ok := got[0].(TaskStarted); !ok || e.TaskID != "t1" || e.PhaseID != "P1" {
		t.Errorf("got[0] = %+v, want TaskStarted{P1,t1}", got[0])
	}
	if e, ok := got[1].(TaskCompleted); !ok || e.TaskID != "t1" || e.PhaseID != "P1" {
		t.Errorf("got[1] = %+v, want TaskCompleted{P1,t1}", got[1])
	}
	if e, ok := got[2].(TaskApproved); !ok || e.TaskID != "t1" || e.PhaseID != "P1" {
		t.Errorf("got[2] = %+v, want TaskApproved{P1,t1}", got[2])
	}
	if e, ok := got[3].(TaskNeedsFix); !ok || e.TaskID != "t2" || e.PhaseID != "P1" || e.Note != "missing assertion" {
		t.Errorf("got[3] = %+v, want TaskNeedsFix{P1,t2,note}", got[3])
	}
}

// TestMaxRetryBudget_TakesPerTaskMaximum pins the per-task aggregation
// rule: the iteration-level budget is the highest retry_budget across
// the sub-DAG. Returns 0 when no task in subDAG has a budget.
func TestMaxRetryBudget_TakesPerTaskMaximum(t *testing.T) {
	phase := &director.Phase{
		ID: "P1",
		Tasks: []director.Task{
			{ID: "t1", RetryBudget: 1},
			{ID: "t2", RetryBudget: 3},
			{ID: "t3", RetryBudget: 0},
		},
	}
	cases := []struct {
		name   string
		subDAG []string
		want   int
	}{
		{"single task budget", []string{"t1"}, 1},
		{"max across sub-DAG", []string{"t1", "t2"}, 3},
		{"unknown task ignored", []string{"t1", "missing"}, 1},
		{"all zero", []string{"t3"}, 0},
		{"empty sub-DAG", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := maxRetryBudget(phase, tc.subDAG); got != tc.want {
				t.Errorf("maxRetryBudget(%v) = %d, want %d", tc.subDAG, got, tc.want)
			}
		})
	}
}

// TestEffectiveRetryBudget_ConfigFloorWins guards the run-config floor:
// a Planner that emits retry_budget=1 on every task must not be able to
// shrink Config.Director.RetryBudget. Per-task values higher than the
// floor still win.
func TestEffectiveRetryBudget_ConfigFloorWins(t *testing.T) {
	phase := &director.Phase{
		ID: "P1",
		Tasks: []director.Task{
			{ID: "t1", RetryBudget: 1},
			{ID: "t2", RetryBudget: 4},
			{ID: "t3", RetryBudget: 0},
		},
	}
	cases := []struct {
		name        string
		subDAG      []string
		configFloor int
		want        int
	}{
		{"floor lifts a brittle plan", []string{"t1"}, 2, 2},
		{"per-task above floor wins", []string{"t2"}, 2, 4},
		{"floor lifts an all-zero plan", []string{"t3"}, 2, 2},
		{"no floor and no per-task", []string{"t3"}, 0, 0},
		{"floor lifts empty sub-DAG", nil, 3, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveRetryBudget(phase, tc.subDAG, tc.configFloor); got != tc.want {
				t.Errorf("effectiveRetryBudget(%v, floor=%d) = %d, want %d",
					tc.subDAG, tc.configFloor, got, tc.want)
			}
		})
	}
}
