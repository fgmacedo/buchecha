package dag

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/director"
)

func TestHandleCall_UnknownMethodRejected(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), "bcc_does_not_exist", nil); err == nil {
		t.Error("expected error for unknown method")
	}
}

func TestHandleCall_ConnectionRoleMismatchRejected(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, err := registry.Register(RolePlanner, RegisterArgs{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := NewHandler(nil, registry)
	_, err = h.HandleCall(context.Background(), string(RoleReviewer), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
	})
	if err == nil {
		t.Fatal("expected rejection on connection-name authz")
	}
}

func TestHandleCall_MissingAgentIDRejected(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	_, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{})
	if err == nil {
		t.Fatal("expected rejection when agent_id is missing")
	}
}

func TestHandleCall_UnregisteredAgentIDRejected(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	_, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": "planner-deadbeef",
	})
	if err == nil {
		t.Fatal("expected rejection for unregistered agent_id")
	}
}

func TestHandleCall_AgentRoleMismatchedToConnectionRejected(t *testing.T) {
	registry := NewAgentRegistry(nil)
	plannerID, err := registry.Register(RolePlanner, RegisterArgs{})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(nil, registry)
	_, err = h.HandleCall(context.Background(), string(RoleBriefer), MethodGetDAGSnapshot, map[string]any{
		"agent_id": string(plannerID),
	})
	if err == nil {
		t.Fatal("expected rejection when agent_id role differs from connection name")
	}
}

// validPlanInput returns a plan body acceptable to bcc_plan_emit's
// schema and ValidatePlan.
func validPlanInput(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"goal":             "demo",
		"success_criteria": []any{"works"},
		"spec_hash":        "abc",
		"planned_at":       "2026-05-02T12:00:00Z",
		"phases": []any{
			map[string]any{
				"id":     "P1",
				"title":  "First",
				"intent": "do work",
				"tasks": []any{
					map[string]any{
						"id":     "t1",
						"title":  "task one",
						"intent": "first task",
						"acceptance": []any{
							map[string]any{
								"id":          "a1",
								"description": "ok",
								"evidence":    "diff",
							},
						},
						"status": "pending",
					},
				},
			},
		},
	}
}

func TestHandlePlanEmit_AcceptsValidPlan(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	h := NewHandler(nil, registry)
	res, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
		"plan":     validPlanInput(t),
	})
	if err != nil {
		t.Fatalf("plan emit: %v", err)
	}
	if !strings.Contains(res, "true") {
		t.Errorf("result %q lacks ok=true", res)
	}
	if h.Plan() == nil {
		t.Fatal("plan not stored after successful emit")
	}
	if h.State() == nil {
		t.Fatal("state not initialized after successful emit")
	}
}

func TestHandlePlanEmit_RejectsCycle(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	h := NewHandler(nil, registry)
	plan := validPlanInput(t)
	plan["phases"] = []any{
		map[string]any{
			"id": "P1", "title": "a", "intent": "a", "depends_on": []any{"P2"},
			"tasks": plan["phases"].([]any)[0].(map[string]any)["tasks"],
		},
		map[string]any{
			"id": "P2", "title": "b", "intent": "b", "depends_on": []any{"P1"},
			"tasks": plan["phases"].([]any)[0].(map[string]any)["tasks"],
		},
	}
	_, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
		"plan":     plan,
	})
	if err == nil {
		t.Fatal("expected rejection on cyclic plan")
	}
}

func TestHandlePlanEmit_RejectsAssignmentOutsideRegistry(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	caps := &director.CapabilityRegistry{
		Models: []director.Capability{
			{Family: "claude", Model: "claude-opus-4-7", Tier: "frontier"},
		},
	}
	h := NewHandlerWithOptions(nil, registry, HandlerOptions{CapabilityRegistry: caps})
	plan := validPlanInput(t)
	plan["phases"].([]any)[0].(map[string]any)["executor_assignment"] = map[string]any{
		"model": "claude-mystery-9-9",
	}
	_, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
		"plan":     plan,
	})
	if err == nil {
		t.Fatal("expected rejection on unknown model assignment")
	}
	if !strings.Contains(err.Error(), "not in capability registry") {
		t.Fatalf("error %q missing capability rejection text", err)
	}
}

func TestRecordSyntheticBriefing_ValidatesAndStores(t *testing.T) {
	registry := NewAgentRegistry(nil)
	h := NewHandler(nil, registry)
	pid, _ := registry.Register(RolePlanner, RegisterArgs{})
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(pid),
		"plan":     validPlanInput(t),
	}); err != nil {
		t.Fatalf("plan emit: %v", err)
	}
	br := director.Briefing{
		IterationID:   "P1-1",
		PhaseID:       "P1",
		SubDAGTaskIDs: []string{"t1"},
		Instructions:  "ship it",
		SpecPath:      "/tmp/spec.md",
	}
	if err := h.RecordSyntheticBriefing(br); err != nil {
		t.Fatalf("RecordSyntheticBriefing: %v", err)
	}
	got := h.Briefing("P1-1")
	if got == nil {
		t.Fatal("synthetic briefing not stored")
	}
	if got.Instructions != "ship it" || got.PhaseID != "P1" {
		t.Fatalf("unexpected briefing stored: %+v", got)
	}
}

func TestRecordSyntheticBriefing_RejectsUnknownTask(t *testing.T) {
	registry := NewAgentRegistry(nil)
	h := NewHandler(nil, registry)
	pid, _ := registry.Register(RolePlanner, RegisterArgs{})
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(pid),
		"plan":     validPlanInput(t),
	}); err != nil {
		t.Fatalf("plan emit: %v", err)
	}
	err := h.RecordSyntheticBriefing(director.Briefing{
		IterationID:   "P1-1",
		PhaseID:       "P1",
		SubDAGTaskIDs: []string{"ghost"},
		Instructions:  "x",
		SpecPath:      "/tmp/spec.md",
	})
	if err == nil || !strings.Contains(err.Error(), "not in phase") {
		t.Fatalf("expected unknown-task rejection, got %v", err)
	}
}

func TestRecordSyntheticBriefing_RequiresIterationID(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	err := h.RecordSyntheticBriefing(director.Briefing{PhaseID: "P1"})
	if err == nil || !strings.Contains(err.Error(), "empty iteration_id") {
		t.Fatalf("expected empty-iteration_id rejection, got %v", err)
	}
}

// emitSamplePlan installs samplePlan() into the handler via the
// bcc_plan_emit path so subsequent tests run against a real DAG state.
func emitSamplePlan(t *testing.T, h *Handler) {
	t.Helper()
	registry := h.Registry()
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	plan := validPlanInput(t)
	// Replace with two-phase sample.
	plan["phases"] = []any{
		map[string]any{
			"id": "P1", "title": "p1", "intent": "p1",
			"tasks": []any{
				map[string]any{
					"id": "t1", "title": "task one", "intent": "intent",
					"acceptance": []any{map[string]any{
						"id": "a1", "description": "d", "evidence": "diff",
					}},
					"status":       "pending",
					"retry_budget": 1,
				},
				map[string]any{
					"id": "t2", "title": "task two", "intent": "intent",
					"acceptance": []any{map[string]any{
						"id": "a1", "description": "d", "evidence": "diff",
					}},
					"status":       "pending",
					"retry_budget": 1,
				},
			},
		},
		map[string]any{
			"id": "P2", "title": "p2", "intent": "p2", "depends_on": []any{"P1"},
			"tasks": []any{
				map[string]any{
					"id": "t1", "title": "task one", "intent": "intent",
					"acceptance": []any{map[string]any{
						"id": "a1", "description": "d", "evidence": "diff",
					}},
					"status":       "pending",
					"retry_budget": 1,
				},
			},
		},
	}
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
		"plan":     plan,
	}); err != nil {
		t.Fatalf("emit sample plan: %v", err)
	}
	registry.Deregister(id)
}

func TestHandlePlanSkip_RecordsReason(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	h := NewHandler(nil, registry)
	res, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanSkip, map[string]any{
		"agent_id": string(id),
		"reason":   "every acceptance bullet is checked off in the spec journal",
	})
	if err != nil {
		t.Fatalf("plan skip: %v", err)
	}
	if !strings.Contains(res, "true") {
		t.Errorf("result %q lacks ok=true", res)
	}
	if !h.PlanSkipped() {
		t.Fatal("PlanSkipped() = false after handlePlanSkip")
	}
	if got := h.PlanSkipReason(); got != "every acceptance bullet is checked off in the spec journal" {
		t.Errorf("PlanSkipReason() = %q, want the recorded reason", got)
	}
	if h.Plan() != nil {
		t.Error("Plan() should remain nil after skip")
	}
}

func TestHandlePlanSkip_RejectsAfterPlanEmitted(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	h := NewHandler(nil, registry)
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
		"plan":     validPlanInput(t),
	}); err != nil {
		t.Fatalf("plan emit: %v", err)
	}
	_, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanSkip, map[string]any{
		"agent_id": string(id),
		"reason":   "redundant skip after emit",
	})
	if err == nil {
		t.Fatal("expected rejection when plan was already emitted")
	}
}

func TestHandlePlanEmit_RejectsAfterPlanSkipped(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	h := NewHandler(nil, registry)
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanSkip, map[string]any{
		"agent_id": string(id),
		"reason":   "nothing to do",
	}); err != nil {
		t.Fatalf("plan skip: %v", err)
	}
	_, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
		"plan":     validPlanInput(t),
	})
	if err == nil {
		t.Fatal("expected rejection when plan was already skipped")
	}
}

func TestHandlePlanSkip_RejectsNonPlanner(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RoleBriefer, RegisterArgs{})
	h := NewHandler(nil, registry)
	_, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodPlanSkip, map[string]any{
		"agent_id": string(id),
		"reason":   "briefer cannot skip",
	})
	if err == nil {
		t.Fatal("expected briefer to be rejected from plan-skip")
	}
}

func TestHandlePlanSkip_SchemaRejectsEmptyReason(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	h := NewHandler(nil, registry)
	_, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanSkip, map[string]any{
		"agent_id": string(id),
		"reason":   "",
	})
	if err == nil {
		t.Fatal("expected schema rejection for empty reason")
	}
}

func TestSetPlan_ClearsSkipState(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	h := NewHandler(nil, registry)
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanSkip, map[string]any{
		"agent_id": string(id),
		"reason":   "skip first",
	}); err != nil {
		t.Fatalf("plan skip: %v", err)
	}
	plan := director.Plan{
		Goal:      "demo",
		SpecHash:  "abc",
		PlannedAt: time.Now(),
		Phases: []director.Phase{{ID: "P1", Title: "p", Intent: "p", Tasks: []director.Task{{
			ID: "t1", Title: "t", Intent: "t",
			Acceptance: []director.AcceptanceItem{{ID: "a1", Description: "ok", Evidence: director.EvidenceDiff}},
			Status:     director.TaskPending,
		}}}},
	}
	h.SetPlan(&plan)
	if h.PlanSkipped() {
		t.Error("SetPlan should clear PlanSkipped")
	}
	if h.PlanSkipReason() != "" {
		t.Error("SetPlan should clear PlanSkipReason")
	}
}

func TestHandleBriefingEmit_AcceptsAndStores(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	registry := h.Registry()
	bid, _ := registry.Register(RoleBriefer, RegisterArgs{})
	res, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodBriefingEmit, map[string]any{
		"agent_id": string(bid),
		"briefing": map[string]any{
			"iteration_id":     "P1-1",
			"phase_id":         "P1",
			"sub_dag_task_ids": []any{"t1"},
			"instructions":     "do t1",
			"spec_path":        "/tmp/spec.md",
		},
	})
	if err != nil {
		t.Fatalf("briefing emit: %v", err)
	}
	if !strings.Contains(res, "P1-1") {
		t.Errorf("result %q missing iteration_id echo", res)
	}
	if h.Briefing("P1-1") == nil {
		t.Error("briefing not stored")
	}
}

func TestHandleBriefingEmit_RejectsIneligiblePhase(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	bid, _ := h.Registry().Register(RoleBriefer, RegisterArgs{})
	_, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodBriefingEmit, map[string]any{
		"agent_id": string(bid),
		"briefing": map[string]any{
			"iteration_id":     "P2-1",
			"phase_id":         "P2",
			"sub_dag_task_ids": []any{"t1"},
			"instructions":     "blocked",
			"spec_path":        "/tmp/spec.md",
		},
	})
	if err == nil {
		t.Fatal("expected rejection: P2 depends on P1 which is not done")
	}
}

func TestHandleBriefingEmit_RejectsCrossPhaseTask(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	bid, _ := h.Registry().Register(RoleBriefer, RegisterArgs{})
	_, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodBriefingEmit, map[string]any{
		"agent_id": string(bid),
		"briefing": map[string]any{
			"iteration_id":     "x",
			"phase_id":         "P1",
			"sub_dag_task_ids": []any{"not-in-phase"},
			"instructions":     "x",
			"spec_path":        "/tmp/spec.md",
		},
	})
	if err == nil {
		t.Fatal("expected rejection on unknown task id")
	}
}

func TestHandleTaskStarted_PlannerPlanningOnly(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	pid, _ := h.Registry().Register(RolePlanner, RegisterArgs{})
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodTaskStarted, map[string]any{
		"agent_id": string(pid),
		"id":       PlanningTaskID,
	}); err != nil {
		t.Fatalf("planner planning: %v", err)
	}
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodTaskStarted, map[string]any{
		"agent_id": string(pid),
		"id":       "t1",
	}); err == nil {
		t.Fatal("planner using non-planning id must be rejected")
	}
}

func TestHandleTaskCompleted_ExecutorScopeEnforced(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	exec1, _ := h.Registry().Register(RoleExecutor, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskCompleted, map[string]any{
		"agent_id": string(exec1),
		"id":       "t2",
	}); err == nil {
		t.Fatal("executor must not complete tasks outside its sub-DAG")
	}
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskCompleted, map[string]any{
		"agent_id": string(exec1),
		"id":       "t1",
	}); err != nil {
		t.Fatalf("in-scope completion: %v", err)
	}
	if h.State().Phase("P1").Tasks["t1"].Status != director.TaskDone {
		t.Error("t1 not marked done")
	}
}

func TestHandleGetPendingTasks_FiltersToSubDAG(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	exec, _ := h.Registry().Register(RoleExecutor, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1", "t2"},
	})
	res, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodGetPendingTasks, map[string]any{
		"agent_id": string(exec),
	})
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	var got struct {
		Pending []string `json:"pending"`
	}
	if err := json.Unmarshal([]byte(res), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Pending) != 2 {
		t.Errorf("pending = %v, want 2 entries", got.Pending)
	}
}

func TestHandleGetBriefing_ReturnsAgentsBriefing(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	bid, _ := h.Registry().Register(RoleBriefer, RegisterArgs{})
	if _, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodBriefingEmit, map[string]any{
		"agent_id": string(bid),
		"briefing": map[string]any{
			"iteration_id":     "P1-1",
			"phase_id":         "P1",
			"sub_dag_task_ids": []any{"t1"},
			"instructions":     "x",
			"spec_path":        "/tmp",
		},
	}); err != nil {
		t.Fatalf("briefing emit: %v", err)
	}
	exec, _ := h.Registry().Register(RoleExecutor, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	res, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodGetBriefing, map[string]any{
		"agent_id": string(exec),
	})
	if err != nil {
		t.Fatalf("get briefing: %v", err)
	}
	if !strings.Contains(res, "P1-1") {
		t.Errorf("get briefing missing id: %s", res)
	}
}

func TestHandleReviewFinished_ApproveRequiresAllDone(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	rev, _ := h.Registry().Register(RoleReviewer, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	_, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodReviewFinished, map[string]any{
		"agent_id": string(rev),
		"outcome":  "approve",
	})
	if err == nil {
		t.Fatal("approve must require every sub-DAG task done")
	}
	if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodTaskApproved, map[string]any{
		"agent_id": string(rev),
		"id":       "t1",
	}); err != nil {
		t.Fatalf("approve task: %v", err)
	}
	if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodReviewFinished, map[string]any{
		"agent_id": string(rev),
		"outcome":  "approve",
	}); err != nil {
		t.Fatalf("approve after all-done: %v", err)
	}
}

func TestHandleReviewFinished_ReviseRequiresAnyNeedsFix(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	rev, _ := h.Registry().Register(RoleReviewer, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	_, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodReviewFinished, map[string]any{
		"agent_id": string(rev),
		"outcome":  "revise",
	})
	if err == nil {
		t.Fatal("revise must require at least one needs_fix")
	}
}

func TestHandleReviewFinished_EscalateRequiresReasoning(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	rev, _ := h.Registry().Register(RoleReviewer, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	_, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodReviewFinished, map[string]any{
		"agent_id":  string(rev),
		"outcome":   "escalate",
		"reasoning": "",
	})
	if err == nil {
		t.Fatal("escalate must require non-empty reasoning")
	}
}

// fakeGit and fakeJournal back the diff/journal port tests.
type fakeGit struct {
	diff string
	err  error
}

func (f *fakeGit) Diff(_ context.Context, _, _ string) (string, error) {
	return f.diff, f.err
}

type fakeJournal struct{}

func (fakeJournal) JournalDelta(before, after []byte) string {
	return string(after) + "-" + string(before)
}

func TestHandleGetDiff_UsesGitProvider(t *testing.T) {
	registry := NewAgentRegistry(nil)
	h := NewHandlerWithOptions(nil, registry, HandlerOptions{Git: &fakeGit{diff: "DIFF"}})
	emitSamplePlan(t, h)
	h.SetBriefingDiffRange("P1-1", "BASE", "HEAD")
	rev, _ := registry.Register(RoleReviewer, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	res, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodGetDiff, map[string]any{
		"agent_id": string(rev),
	})
	if err != nil {
		t.Fatalf("get_diff: %v", err)
	}
	if !strings.Contains(res, "DIFF") {
		t.Errorf("expected DIFF in result, got %s", res)
	}
}

func TestHandleGetJournalDelta_UsesProvider(t *testing.T) {
	registry := NewAgentRegistry(nil)
	h := NewHandlerWithOptions(nil, registry, HandlerOptions{Journal: fakeJournal{}})
	emitSamplePlan(t, h)
	h.SetBriefingJournalSnapshots("P1-1", []byte("B"), []byte("A"))
	rev, _ := registry.Register(RoleReviewer, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	res, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodGetJournalDelta, map[string]any{
		"agent_id": string(rev),
	})
	if err != nil {
		t.Fatalf("journal: %v", err)
	}
	if !strings.Contains(res, "A-B") {
		t.Errorf("provider result not surfaced: %s", res)
	}
}

func TestHandleCall_AuditLogRecordsSuccessAndFailure(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "mcp-log.jsonl")
	audit := NewAuditLog(logPath)
	defer audit.Close()
	registry := NewAgentRegistry(nil)
	h := NewHandlerWithOptions(nil, registry, HandlerOptions{Audit: audit})
	pid, _ := registry.Register(RolePlanner, RegisterArgs{})
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(pid),
	}); err == nil {
		t.Fatal("expected schema error: missing plan")
	}
	if err := audit.Close(); err != nil {
		t.Fatalf("audit close: %v", err)
	}
	body, err := readFile(t, logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "bcc_plan_emit") {
		t.Errorf("audit body missing method: %s", body)
	}
	if !strings.Contains(body, "\"error\":") {
		t.Errorf("audit body missing error: %s", body)
	}
}

// schemaRejectionMissingTask covers schema validation: bcc_task_started
// requires id; without it, the call is rejected before any state mutates.
func TestHandleCall_SchemaRejectsMissingRequiredField(t *testing.T) {
	registry := NewAgentRegistry(nil)
	h := NewHandler(nil, registry)
	emitSamplePlan(t, h)
	exec, _ := registry.Register(RoleExecutor, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	_, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskStarted, map[string]any{
		"agent_id": string(exec),
	})
	if err == nil {
		t.Fatal("expected schema rejection for missing id")
	}
	if !strings.Contains(err.Error(), "schema validation") {
		t.Errorf("error %v should mention schema validation", err)
	}
}

func TestForceApprovePending_MarksPendingDoneAndAudits(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "mcp-log.jsonl")
	audit := NewAuditLog(logPath)
	defer audit.Close()
	registry := NewAgentRegistry(nil)
	h := NewHandlerWithOptions(nil, registry, HandlerOptions{Audit: audit})
	emitSamplePlan(t, h)
	bid, _ := registry.Register(RoleBriefer, RegisterArgs{})
	if _, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodBriefingEmit, map[string]any{
		"agent_id": string(bid),
		"briefing": map[string]any{
			"iteration_id":     "P1-1",
			"phase_id":         "P1",
			"sub_dag_task_ids": []any{"t1", "t2"},
			"instructions":     "x",
			"spec_path":        "/tmp/spec.md",
		},
	}); err != nil {
		t.Fatalf("briefing emit: %v", err)
	}
	registry.Deregister(bid)

	exec, _ := registry.Register(RoleExecutor, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskCompleted, map[string]any{
		"agent_id": string(exec), "id": "t1",
	}); err != nil {
		t.Fatalf("complete t1: %v", err)
	}
	registry.Deregister(exec)

	if err := h.ForceApprovePending("P1-1", "trust me"); err != nil {
		t.Fatalf("ForceApprovePending: %v", err)
	}
	state := h.State()
	statuses := state.SubDAGStatuses("P1", []TaskID{"t1", "t2"})
	if statuses["t1"] != director.TaskDone || statuses["t2"] != director.TaskDone {
		t.Errorf("statuses = %v, want both done", statuses)
	}
	if err := audit.Close(); err != nil {
		t.Fatalf("audit close: %v", err)
	}
	body, err := readFile(t, logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "bcc_force_approve") {
		t.Errorf("audit body missing force_approve method: %s", body)
	}
	if !strings.Contains(body, "\"role\":\"user\"") {
		t.Errorf("audit body missing user role: %s", body)
	}
	if !strings.Contains(body, "trust me") {
		t.Errorf("audit body missing hint: %s", body)
	}
}

func TestForceApprovePending_RejectsUnknownIteration(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	if err := h.ForceApprovePending("nope", ""); err == nil {
		t.Fatal("expected error for unknown iteration_id")
	}
}

// readFile is a small helper that returns the file contents as a string,
// failing the test on I/O error.
func readFile(t *testing.T, path string) (string, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", errors.New("empty file")
	}
	return string(data), nil
}
