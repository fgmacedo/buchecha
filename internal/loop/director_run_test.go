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
