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

	"github.com/fgmacedo/buchecha/internal/supervision"
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

// validPlanInput returns a plan body acceptable to plan_emit's
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

func TestHandlePlanEmit_AcceptsMinimalPlan(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	h := NewHandler(nil, registry)
	minimal := map[string]any{
		"goal":             "minimal goal",
		"success_criteria": []any{"it works"},
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
					},
				},
			},
		},
	}
	_, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
		"plan":     minimal,
	})
	if err != nil {
		t.Fatalf("plan emit (minimal): %v", err)
	}
	state := h.State()
	if state == nil {
		t.Fatal("state not initialized after minimal plan emit")
	}
	phase := state.Phase("P1")
	if phase == nil {
		t.Fatal("phase P1 missing after minimal plan emit")
	}
	if phase.Tasks["t1"].Status != supervision.TaskPending {
		t.Errorf("task t1 status = %q, want pending", phase.Tasks["t1"].Status)
	}
}

// capturingPlanPersister marshals the Plan exactly like the real Store
// does, so it surfaces TaskStatus.MarshalJSON failures the same way.
type capturingPlanPersister struct {
	bytes []byte
}

func (c *capturingPlanPersister) WritePlan(p *supervision.Plan) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	c.bytes = b
	return nil
}

func TestHandlePlanEmit_PersistsMinimalPlanWithDefaultedStatus(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	persister := &capturingPlanPersister{}
	h := NewHandlerWithOptions(nil, registry, HandlerOptions{PlanStore: persister})
	minimal := map[string]any{
		"goal":             "minimal goal",
		"success_criteria": []any{"it works"},
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
					},
				},
			},
		},
	}
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
		"plan":     minimal,
	}); err != nil {
		t.Fatalf("plan emit (minimal, with persister): %v", err)
	}
	if len(persister.bytes) == 0 {
		t.Fatal("persister did not capture marshaled plan")
	}
	var got supervision.Plan
	if err := json.Unmarshal(persister.bytes, &got); err != nil {
		t.Fatalf("unmarshal persisted plan: %v", err)
	}
	if len(got.Phases) != 1 || len(got.Phases[0].Tasks) != 1 {
		t.Fatalf("unexpected persisted plan shape: %+v", got)
	}
	if status := got.Phases[0].Tasks[0].Status; status != supervision.TaskPending {
		t.Errorf("persisted task status = %q, want pending", status)
	}
}

func TestHandlePlanEmit_PersistsRoleAssignmentsWithProviderModelEffort(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	persister := &capturingPlanPersister{}
	cap := supervision.CapabilityRegistry{
		Models: []supervision.Capability{
			{Provider: "claude", Model: "claude-sonnet-4-6", Tier: "balanced", Efforts: []string{"low", "medium", "high"}},
		},
	}
	menu := supervision.RoleMenu{Options: []supervision.MenuOption{
		{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: []string{"medium"}},
	}}
	menus := supervision.RoleMenus{
		Briefer:  menu,
		Executor: menu,
		Reviewer: menu,
	}
	h := NewHandlerWithOptions(nil, registry, HandlerOptions{
		PlanStore:          persister,
		CapabilityRegistry: &cap,
		RoleMenus:          menus,
	})
	minimal := map[string]any{
		"goal":             "minimal goal",
		"success_criteria": []any{"it works"},
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
					},
				},
			},
		},
	}
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
		"plan":     minimal,
	}); err != nil {
		t.Fatalf("plan emit (with role assignments): %v", err)
	}
	if len(persister.bytes) == 0 {
		t.Fatal("persister did not capture marshaled plan")
	}
	var got supervision.Plan
	if err := json.Unmarshal(persister.bytes, &got); err != nil {
		t.Fatalf("unmarshal persisted plan: %v", err)
	}
	if len(got.Phases) != 1 {
		t.Fatalf("unexpected persisted plan shape: %+v", got)
	}
	phase := got.Phases[0]
	if phase.BrieferAssignment == nil {
		t.Errorf("phase.BrieferAssignment is nil")
	} else {
		if phase.BrieferAssignment.Provider != "claude" {
			t.Errorf("BrieferAssignment.Provider = %q, want claude", phase.BrieferAssignment.Provider)
		}
		if phase.BrieferAssignment.Model != "claude-sonnet-4-6" {
			t.Errorf("BrieferAssignment.Model = %q, want claude-sonnet-4-6", phase.BrieferAssignment.Model)
		}
		if phase.BrieferAssignment.Effort != "medium" {
			t.Errorf("BrieferAssignment.Effort = %q, want medium", phase.BrieferAssignment.Effort)
		}
	}
	if phase.ExecutorAssignment == nil {
		t.Errorf("phase.ExecutorAssignment is nil")
	} else {
		if phase.ExecutorAssignment.Provider != "claude" {
			t.Errorf("ExecutorAssignment.Provider = %q, want claude", phase.ExecutorAssignment.Provider)
		}
		if phase.ExecutorAssignment.Model != "claude-sonnet-4-6" {
			t.Errorf("ExecutorAssignment.Model = %q, want claude-sonnet-4-6", phase.ExecutorAssignment.Model)
		}
		if phase.ExecutorAssignment.Effort != "medium" {
			t.Errorf("ExecutorAssignment.Effort = %q, want medium", phase.ExecutorAssignment.Effort)
		}
	}
	if phase.ReviewerAssignment == nil {
		t.Errorf("phase.ReviewerAssignment is nil")
	} else {
		if phase.ReviewerAssignment.Provider != "claude" {
			t.Errorf("ReviewerAssignment.Provider = %q, want claude", phase.ReviewerAssignment.Provider)
		}
		if phase.ReviewerAssignment.Model != "claude-sonnet-4-6" {
			t.Errorf("ReviewerAssignment.Model = %q, want claude-sonnet-4-6", phase.ReviewerAssignment.Model)
		}
		if phase.ReviewerAssignment.Effort != "medium" {
			t.Errorf("ReviewerAssignment.Effort = %q, want medium", phase.ReviewerAssignment.Effort)
		}
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

func TestHandlePlanEmit_RejectsAssignmentOutsideMenu(t *testing.T) {
	registry := NewAgentRegistry(nil)
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	menus := supervision.RoleMenus{
		Executor: supervision.RoleMenu{Options: []supervision.MenuOption{
			{Provider: "claude", Model: "claude-opus-4-7", Efforts: []string{"high"}},
		}},
	}
	h := NewHandlerWithOptions(nil, registry, HandlerOptions{RoleMenus: menus})
	plan := validPlanInput(t)
	plan["phases"].([]any)[0].(map[string]any)["executor_assignment"] = map[string]any{
		"provider": "claude",
		"model":    "claude-mystery-9-9",
	}
	_, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
		"plan":     plan,
	})
	if err == nil {
		t.Fatal("expected rejection on assignment outside menu")
	}
	if !strings.Contains(err.Error(), "not in the role menu") {
		t.Fatalf("error %q missing menu rejection text", err)
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
	br := supervision.Briefing{
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
	err := h.RecordSyntheticBriefing(supervision.Briefing{
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
	err := h.RecordSyntheticBriefing(supervision.Briefing{PhaseID: "P1"})
	if err == nil || !strings.Contains(err.Error(), "empty iteration_id") {
		t.Fatalf("expected empty-iteration_id rejection, got %v", err)
	}
}

func TestRecordSyntheticApproval_MarksTasksDone(t *testing.T) {
	registry := NewAgentRegistry(nil)
	h := NewHandler(nil, registry)
	pid, _ := registry.Register(RolePlanner, RegisterArgs{})
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(pid),
		"plan":     validPlanInput(t),
	}); err != nil {
		t.Fatalf("plan emit: %v", err)
	}
	if err := h.RecordSyntheticBriefing(supervision.Briefing{
		IterationID:   "P1-1",
		PhaseID:       "P1",
		SubDAGTaskIDs: []string{"t1"},
		Instructions:  "x",
		SpecPath:      "/tmp/spec.md",
	}); err != nil {
		t.Fatalf("RecordSyntheticBriefing: %v", err)
	}
	if err := h.RecordSyntheticApproval("P1-1"); err != nil {
		t.Fatalf("RecordSyntheticApproval: %v", err)
	}
	if got := h.LastReviewOutcome("P1-1"); got != "approve" {
		t.Errorf("review outcome = %q, want approve", got)
	}
	state := h.State()
	if state == nil {
		t.Fatal("nil state after approval")
	}
	phase := state.Phase("P1")
	if phase == nil {
		t.Fatal("phase missing after approval")
	}
	if phase.Tasks["t1"].Status != supervision.TaskDone {
		t.Errorf("task t1 status = %q, want done", phase.Tasks["t1"].Status)
	}
}

func TestRecordSyntheticApproval_RejectsUnknownIteration(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	err := h.RecordSyntheticApproval("missing")
	if err == nil || !strings.Contains(err.Error(), "unknown iteration") {
		t.Fatalf("expected unknown-iteration rejection, got %v", err)
	}
}

func TestRecordSyntheticApproval_RequiresIterationID(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	err := h.RecordSyntheticApproval("")
	if err == nil || !strings.Contains(err.Error(), "empty iteration_id") {
		t.Fatalf("expected empty-iteration_id rejection, got %v", err)
	}
}

// emitSamplePlan installs samplePlan() into the handler via the
// plan_emit path so subsequent tests run against a real DAG state.
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
	plan := supervision.Plan{
		Goal:      "demo",
		SpecHash:  "abc",
		PlannedAt: time.Now(),
		Phases: []supervision.Phase{{ID: "P1", Title: "p", Intent: "p", Tasks: []supervision.Task{{
			ID: "t1", Title: "t", Intent: "t",
			Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "ok", Evidence: supervision.EvidenceDiff}},
			Status:     supervision.TaskPending,
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

func TestHandleBriefingEmit_CoercesStringifiedBriefing(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	bid, _ := h.Registry().Register(RoleBriefer, RegisterArgs{})
	stringified := `{"iteration_id":"P1-1","phase_id":"P1","sub_dag_task_ids":["t1"],"instructions":"do t1","spec_path":"/tmp/spec.md"}`
	res, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodBriefingEmit, map[string]any{
		"agent_id": string(bid),
		"briefing": stringified,
	})
	if err != nil {
		t.Fatalf("briefing emit (stringified): %v", err)
	}
	if !strings.Contains(res, "P1-1") {
		t.Errorf("result %q missing iteration_id echo", res)
	}
	if h.Briefing("P1-1") == nil {
		t.Error("briefing not stored after coercion")
	}
}

func TestHandleBriefingEmit_RejectsUnparseableString(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	bid, _ := h.Registry().Register(RoleBriefer, RegisterArgs{})
	_, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodBriefingEmit, map[string]any{
		"agent_id": string(bid),
		"briefing": "this is not JSON",
	})
	if err == nil {
		t.Fatal("expected rejection: string is not parseable JSON object")
	}
	if !strings.Contains(err.Error(), "schema validation") {
		t.Errorf("err = %v, want schema validation rejection", err)
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

func TestHandleTaskStarted_BrieferBriefingOnly(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	bid, _ := h.Registry().Register(RoleBriefer, RegisterArgs{PhaseID: "P1"})
	if _, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodTaskStarted, map[string]any{
		"agent_id": string(bid),
		"id":       BriefingTaskID,
	}); err != nil {
		t.Fatalf("briefer briefing: %v", err)
	}
	if _, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodTaskCompleted, map[string]any{
		"agent_id": string(bid),
		"id":       BriefingTaskID,
	}); err != nil {
		t.Fatalf("briefer briefing complete: %v", err)
	}
	if _, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodTaskStarted, map[string]any{
		"agent_id": string(bid),
		"id":       PlanningTaskID,
	}); err == nil {
		t.Fatal("briefer using planning id must be rejected")
	}
	if _, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodTaskStarted, map[string]any{
		"agent_id": string(bid),
		"id":       "t1",
	}); err == nil {
		t.Fatal("briefer using a real task id must be rejected")
	}
}

func TestHandleTaskStarted_ReviewerReviewingOnly(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	rid, _ := h.Registry().Register(RoleReviewer, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodTaskStarted, map[string]any{
		"agent_id": string(rid),
		"id":       ReviewingTaskID,
	}); err != nil {
		t.Fatalf("reviewer reviewing: %v", err)
	}
	if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodTaskCompleted, map[string]any{
		"agent_id": string(rid),
		"id":       ReviewingTaskID,
	}); err != nil {
		t.Fatalf("reviewer reviewing complete: %v", err)
	}
	if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodTaskStarted, map[string]any{
		"agent_id": string(rid),
		"id":       BriefingTaskID,
	}); err == nil {
		t.Fatal("reviewer using briefing id must be rejected")
	}
	if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodTaskStarted, map[string]any{
		"agent_id": string(rid),
		"id":       "t1",
	}); err == nil {
		t.Fatal("reviewer using a real task id must be rejected")
	}
}

// emitPlanWithDeps installs a single-phase plan where t2 depends on
// t1 and t3 depends on t1+t2. Used by the depends_on enforcement
// tests below; the sample plan from emitSamplePlan has no edges.
func emitPlanWithDeps(t *testing.T, h *Handler) {
	t.Helper()
	registry := h.Registry()
	id, _ := registry.Register(RolePlanner, RegisterArgs{})
	plan := validPlanInput(t)
	plan["phases"] = []any{
		map[string]any{
			"id": "P1", "title": "p1", "intent": "p1",
			"tasks": []any{
				map[string]any{
					"id": "t1", "title": "task one", "intent": "intent",
					"acceptance": []any{map[string]any{
						"id": "a1", "description": "d", "evidence": "diff",
					}},
					"status": "pending",
				},
				map[string]any{
					"id": "t2", "title": "task two", "intent": "intent",
					"depends_on": []any{"t1"},
					"acceptance": []any{map[string]any{
						"id": "a1", "description": "d", "evidence": "diff",
					}},
					"status": "pending",
				},
				map[string]any{
					"id": "t3", "title": "task three", "intent": "intent",
					"depends_on": []any{"t1", "t2"},
					"acceptance": []any{map[string]any{
						"id": "a1", "description": "d", "evidence": "diff",
					}},
					"status": "pending",
				},
			},
		},
	}
	if _, err := h.HandleCall(context.Background(), string(RolePlanner), MethodPlanEmit, map[string]any{
		"agent_id": string(id),
		"plan":     plan,
	}); err != nil {
		t.Fatalf("emit plan with deps: %v", err)
	}
	registry.Deregister(id)
}

func TestHandleTaskStarted_RejectsUnmetDependsOn(t *testing.T) {
	cases := []struct {
		name      string
		preDone   []string // tasks the executor closes before the probe
		probe     string   // task_started target
		wantOK    bool
		wantInErr string // substring expected in error when !wantOK
	}{
		{
			name:   "no deps",
			probe:  "t1",
			wantOK: true,
		},
		{
			name:      "single dep unmet",
			probe:     "t2",
			wantOK:    false,
			wantInErr: "unmet depends_on: [t1]",
		},
		{
			name:    "single dep met",
			preDone: []string{"t1"},
			probe:   "t2",
			wantOK:  true,
		},
		{
			name:      "multi deps both unmet",
			probe:     "t3",
			wantOK:    false,
			wantInErr: "unmet depends_on: [t1 t2]",
		},
		{
			name:      "multi deps partial",
			preDone:   []string{"t1"},
			probe:     "t3",
			wantOK:    false,
			wantInErr: "unmet depends_on: [t2]",
		},
		{
			name:    "multi deps all met",
			preDone: []string{"t1", "t2"},
			probe:   "t3",
			wantOK:  true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(nil, NewAgentRegistry(nil))
			emitPlanWithDeps(t, h)
			exec, _ := h.Registry().Register(RoleExecutor, RegisterArgs{
				BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1", "t2", "t3"},
			})
			for _, tid := range tt.preDone {
				if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskStarted, map[string]any{
					"agent_id": string(exec),
					"id":       tid,
				}); err != nil {
					t.Fatalf("preDone task_started %q: %v", tid, err)
				}
				if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskCompleted, map[string]any{
					"agent_id": string(exec),
					"id":       tid,
				}); err != nil {
					t.Fatalf("preDone task_completed %q: %v", tid, err)
				}
			}
			_, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskStarted, map[string]any{
				"agent_id": string(exec),
				"id":       tt.probe,
			})
			if tt.wantOK {
				if err != nil {
					t.Fatalf("task_started %q: unexpected error %v", tt.probe, err)
				}
				if got := h.State().Phase("P1").Tasks[tt.probe].Status; got != supervision.TaskInProgress {
					t.Errorf("task %q status = %q, want in_progress", tt.probe, got)
				}
				return
			}
			if err == nil {
				t.Fatalf("task_started %q: expected rejection, got ok", tt.probe)
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("error %q lacks %q", err.Error(), tt.wantInErr)
			}
			// Rejection must leave status untouched.
			if got := h.State().Phase("P1").Tasks[tt.probe].Status; got != supervision.TaskPending {
				t.Errorf("task %q status = %q, want pending after rejection", tt.probe, got)
			}
		})
	}
}

func TestHandleTaskStarted_ParallelInProgressAllowed(t *testing.T) {
	// With phase-level parallelism authorized, independent tasks may
	// be in_progress simultaneously. The handler only enforces the
	// depends_on edges, not "one task at a time."
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitPlanWithDeps(t, h)
	exec, _ := h.Registry().Register(RoleExecutor, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1", "t2", "t3"},
	})
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskStarted, map[string]any{
		"agent_id": string(exec),
		"id":       "t1",
	}); err != nil {
		t.Fatalf("task_started t1: %v", err)
	}
	// t2 depends on t1, so it stays blocked while t1 is in_progress.
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskStarted, map[string]any{
		"agent_id": string(exec),
		"id":       "t2",
	}); err == nil {
		t.Fatal("task_started t2 must be rejected while t1 is in_progress")
	}
	// Close t1, then t2 unblocks even though t3 is still untouched.
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskCompleted, map[string]any{
		"agent_id": string(exec),
		"id":       "t1",
	}); err != nil {
		t.Fatalf("task_completed t1: %v", err)
	}
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskStarted, map[string]any{
		"agent_id": string(exec),
		"id":       "t2",
	}); err != nil {
		t.Fatalf("task_started t2 after t1 done: %v", err)
	}
	// t2 in_progress, t1 done. No other constraint; this state is legal.
	if got := h.State().Phase("P1").Tasks["t2"].Status; got != supervision.TaskInProgress {
		t.Errorf("t2 status = %q, want in_progress", got)
	}
}

func TestIsPseudoTaskID(t *testing.T) {
	if !IsPseudoTaskID(PlanningTaskID) {
		t.Errorf("PlanningTaskID should be pseudo")
	}
	if !IsPseudoTaskID(BriefingTaskID) {
		t.Errorf("BriefingTaskID should be pseudo")
	}
	if !IsPseudoTaskID(ReviewingTaskID) {
		t.Errorf("ReviewingTaskID should be pseudo")
	}
	if IsPseudoTaskID("t1") {
		t.Errorf("real task id must not be classified as pseudo")
	}
	if IsPseudoTaskID("") {
		t.Errorf("empty id must not be classified as pseudo")
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
	if h.State().Phase("P1").Tasks["t1"].Status != supervision.TaskDone {
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

// fakeJournal backs the journal port tests.
type fakeJournal struct{}

func (fakeJournal) JournalDelta(before, after []byte) string {
	return string(after) + "-" + string(before)
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
	audit := NewMCPLog(logPath)
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
	if !strings.Contains(body, "plan_emit") {
		t.Errorf("audit body missing method: %s", body)
	}
	if !strings.Contains(body, "\"error\":") {
		t.Errorf("audit body missing error: %s", body)
	}
}

// schemaRejectionMissingTask covers schema validation: task_started
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
	audit := NewMCPLog(logPath)
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
	if statuses["t1"] != supervision.TaskDone || statuses["t2"] != supervision.TaskDone {
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

type capturingObserver struct {
	calls []observerCall
}

type observerCall struct {
	method  string
	agentID string
	role    Role
}

func (c *capturingObserver) OnCall(method, agentID string, role Role, _ map[string]any) {
	c.calls = append(c.calls, observerCall{method: method, agentID: agentID, role: role})
}

func TestHandler_AttachObserver(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	exec, _ := h.Registry().Register(RoleExecutor, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})

	obs := &capturingObserver{}
	h.AttachObserver(obs)

	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskStarted, map[string]any{
		"agent_id": string(exec),
		"id":       "t1",
	}); err != nil {
		t.Fatalf("started: %v", err)
	}
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskCompleted, map[string]any{
		"agent_id": string(exec),
		"id":       "t1",
	}); err != nil {
		t.Fatalf("completed: %v", err)
	}
	// Out-of-scope id: handler rejects, observer must not fire.
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskStarted, map[string]any{
		"agent_id": string(exec),
		"id":       "t2",
	}); err == nil {
		t.Fatal("expected rejection on out-of-scope id")
	}
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodGetPendingTasks, map[string]any{
		"agent_id": string(exec),
	}); err != nil {
		t.Fatalf("pending: %v", err)
	}

	if len(obs.calls) != 3 {
		t.Fatalf("observer calls = %d, want 3 (rejection must not fire)", len(obs.calls))
	}
	want := []observerCall{
		{method: MethodTaskStarted, agentID: string(exec), role: RoleExecutor},
		{method: MethodTaskCompleted, agentID: string(exec), role: RoleExecutor},
		{method: MethodGetPendingTasks, agentID: string(exec), role: RoleExecutor},
	}
	for i, c := range obs.calls {
		if c != want[i] {
			t.Errorf("call[%d] = %+v, want %+v", i, c, want[i])
		}
	}

	// Detach: subsequent calls do not extend the slice.
	h.AttachObserver(nil)
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodGetPendingTasks, map[string]any{
		"agent_id": string(exec),
	}); err != nil {
		t.Fatalf("post-detach call: %v", err)
	}
	if len(obs.calls) != 3 {
		t.Errorf("observer fired after detach: got %d calls", len(obs.calls))
	}
}

func TestHandler_ResetReviewOutcome(t *testing.T) {
	t.Run("resets review outcome and reason", func(t *testing.T) {
		h := NewHandler(nil, NewAgentRegistry(nil))
		emitSamplePlan(t, h)
		registry := h.Registry()

		// Emit a briefing
		bid, _ := registry.Register(RoleBriefer, RegisterArgs{})
		if _, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodBriefingEmit, map[string]any{
			"agent_id": string(bid),
			"briefing": map[string]any{
				"iteration_id":     "P1-1",
				"phase_id":         "P1",
				"sub_dag_task_ids": []any{"t1"},
				"instructions":     "x",
				"spec_path":        "/tmp/spec.md",
			},
		}); err != nil {
			t.Fatalf("briefing emit: %v", err)
		}
		registry.Deregister(bid)

		// Register a Reviewer and drive reviewOutcome to revise
		rev, _ := registry.Register(RoleReviewer, RegisterArgs{
			BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
		})
		if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodTaskNeedsFix, map[string]any{
			"agent_id": string(rev),
			"id":       "t1",
			"feedback": "fix this",
		}); err != nil {
			t.Fatalf("task needs fix: %v", err)
		}
		if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodReviewFinished, map[string]any{
			"agent_id":  string(rev),
			"outcome":   "revise",
			"reasoning": "test reason",
		}); err != nil {
			t.Fatalf("review finished: %v", err)
		}

		// Verify reviewOutcome is set to revise
		if got := h.LastReviewOutcome("P1-1"); got != "revise" {
			t.Errorf("before reset: LastReviewOutcome = %q, want revise", got)
		}
		if got := h.LastReviewReasoning("P1-1"); got != "test reason" {
			t.Errorf("before reset: LastReviewReasoning = %q, want 'test reason'", got)
		}

		// Call ResetReviewOutcome
		h.ResetReviewOutcome("P1-1")

		// Verify both are now empty
		if got := h.LastReviewOutcome("P1-1"); got != "" {
			t.Errorf("after reset: LastReviewOutcome = %q, want empty", got)
		}
		if got := h.LastReviewReasoning("P1-1"); got != "" {
			t.Errorf("after reset: LastReviewReasoning = %q, want empty", got)
		}
	})

	t.Run("noop on unknown iteration", func(t *testing.T) {
		h := NewHandler(nil, NewAgentRegistry(nil))
		emitSamplePlan(t, h)
		registry := h.Registry()

		// Create a known iteration
		bid, _ := registry.Register(RoleBriefer, RegisterArgs{})
		if _, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodBriefingEmit, map[string]any{
			"agent_id": string(bid),
			"briefing": map[string]any{
				"iteration_id":     "P1-1",
				"phase_id":         "P1",
				"sub_dag_task_ids": []any{"t1"},
				"instructions":     "x",
				"spec_path":        "/tmp/spec.md",
			},
		}); err != nil {
			t.Fatalf("briefing emit: %v", err)
		}
		registry.Deregister(bid)

		// Call ResetReviewOutcome on unknown iteration
		// This should not panic or insert into the map
		h.ResetReviewOutcome("unknown-iter")

		// Verify the known iteration is untouched
		rev, _ := registry.Register(RoleReviewer, RegisterArgs{
			BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
		})
		if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodTaskNeedsFix, map[string]any{
			"agent_id": string(rev),
			"id":       "t1",
			"feedback": "fix",
		}); err != nil {
			t.Fatalf("task needs fix: %v", err)
		}
		if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodReviewFinished, map[string]any{
			"agent_id":  string(rev),
			"outcome":   "revise",
			"reasoning": "test",
		}); err != nil {
			t.Fatalf("review finished: %v", err)
		}

		// The known iteration should still have revise
		if got := h.LastReviewOutcome("P1-1"); got != "revise" {
			t.Errorf("known iteration: LastReviewOutcome = %q, want revise", got)
		}
	})
}

// fakeHead implements HeadProvider for get_baseline tests.
type fakeHead struct {
	sha string
	err error
}

func (f *fakeHead) HeadSHA(_ context.Context) (string, error) {
	return f.sha, f.err
}

func TestHandleGetBaseline_HappyPath(t *testing.T) {
	registry := NewAgentRegistry(nil)
	h := NewHandlerWithOptions(nil, registry, HandlerOptions{Head: &fakeHead{sha: "def456"}})
	emitSamplePlan(t, h)
	h.SetPhaseBaseline("P1", "abc123")
	rev, _ := registry.Register(RoleReviewer, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	res, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodGetBaseline, map[string]any{
		"agent_id": string(rev),
	})
	if err != nil {
		t.Fatalf("get_baseline: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got := out["phase_id"]; got != "P1" {
		t.Errorf("phase_id = %v, want P1", got)
	}
	if got := out["phase_baseline_sha"]; got != "abc123" {
		t.Errorf("phase_baseline_sha = %v, want abc123", got)
	}
	if got := out["current_head_sha"]; got != "def456" {
		t.Errorf("current_head_sha = %v, want def456", got)
	}
}

// TestHandleGetBaseline_StableAcrossAttempts is a regression test for
// session f5be4441dbc7: two Reviewer agents successive in the same phase
// with a single SetPhaseBaseline call; phase_baseline_sha must be
// identical across both reads even when fakeHead advances current_head_sha.
func TestHandleGetBaseline_StableAcrossAttempts(t *testing.T) {
	head := &fakeHead{sha: "head-v1"}
	registry := NewAgentRegistry(nil)
	h := NewHandlerWithOptions(nil, registry, HandlerOptions{Head: head})
	emitSamplePlan(t, h)

	// Baseline recorded once before the first attempt.
	h.SetPhaseBaseline("P1", "abc123")

	// First Reviewer attempt.
	rev1, _ := registry.Register(RoleReviewer, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	res1, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodGetBaseline, map[string]any{
		"agent_id": string(rev1),
	})
	if err != nil {
		t.Fatalf("attempt 1 get_baseline: %v", err)
	}
	registry.Deregister(rev1)

	// HEAD advances between attempts.
	head.sha = "head-v2"

	// Second Reviewer attempt on the same phase, fresh agent registration.
	rev2, _ := registry.Register(RoleReviewer, RegisterArgs{
		BriefingID: "P1-2", PhaseID: "P1", SubDAG: []string{"t1"},
	})
	res2, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodGetBaseline, map[string]any{
		"agent_id": string(rev2),
	})
	if err != nil {
		t.Fatalf("attempt 2 get_baseline: %v", err)
	}

	var out1, out2 map[string]any
	if err := json.Unmarshal([]byte(res1), &out1); err != nil {
		t.Fatalf("unmarshal attempt 1: %v", err)
	}
	if err := json.Unmarshal([]byte(res2), &out2); err != nil {
		t.Fatalf("unmarshal attempt 2: %v", err)
	}
	if out1["phase_baseline_sha"] != out2["phase_baseline_sha"] {
		t.Errorf("phase_baseline_sha changed across attempts: %v != %v",
			out1["phase_baseline_sha"], out2["phase_baseline_sha"])
	}
	if out1["phase_baseline_sha"] != "abc123" {
		t.Errorf("phase_baseline_sha = %v, want abc123", out1["phase_baseline_sha"])
	}
	if out1["current_head_sha"] == out2["current_head_sha"] {
		t.Errorf("current_head_sha should differ across attempts (head advanced), got same: %v", out1["current_head_sha"])
	}
}

func TestHandleGetBaseline_ErrBaselineNotSet(t *testing.T) {
	registry := NewAgentRegistry(nil)
	h := NewHandlerWithOptions(nil, registry, HandlerOptions{Head: &fakeHead{sha: "any"}})
	emitSamplePlan(t, h)
	// Register against P2 which has no baseline recorded.
	rev, _ := registry.Register(RoleReviewer, RegisterArgs{
		BriefingID: "P2-1", PhaseID: "P2", SubDAG: []string{"t1"},
	})
	_, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodGetBaseline, map[string]any{
		"agent_id": string(rev),
	})
	if err == nil {
		t.Fatal("expected error when baseline not set, got nil")
	}
	if !strings.Contains(err.Error(), "baseline") {
		t.Errorf("error should mention 'baseline', got: %v", err)
	}
	if !strings.Contains(err.Error(), "P2") {
		t.Errorf("error should mention phase 'P2', got: %v", err)
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

// emitBriefingP1 attaches a Briefing covering P1.t1 + P1.t2 under
// iteration "P1-1" so reviewer-scoped tests can audit a real briefing
// state. Returns the registered reviewer's agent id.
func emitBriefingP1(t *testing.T, h *Handler) AgentID {
	t.Helper()
	registry := h.Registry()
	bid, _ := registry.Register(RoleBriefer, RegisterArgs{})
	if _, err := h.HandleCall(context.Background(), string(RoleBriefer), MethodBriefingEmit, map[string]any{
		"agent_id": string(bid),
		"briefing": map[string]any{
			"iteration_id":     "P1-1",
			"phase_id":         "P1",
			"sub_dag_task_ids": []any{"t1", "t2"},
			"instructions":     "do t1 and t2",
			"spec_path":        "/tmp/spec.md",
		},
	}); err != nil {
		t.Fatalf("emit briefing: %v", err)
	}
	registry.Deregister(bid)
	rev, _ := registry.Register(RoleReviewer, RegisterArgs{
		BriefingID: "P1-1", PhaseID: "P1", SubDAG: []string{"t1", "t2"},
	})
	return rev
}

// markStarted advances a task to in_progress so a subsequent
// task_approved or task_needs_fix transition is legal.
func markStarted(t *testing.T, h *Handler, phaseID, taskID string) {
	t.Helper()
	exec, _ := h.Registry().Register(RoleExecutor, RegisterArgs{
		BriefingID: "P1-1", PhaseID: phaseID, SubDAG: []string{taskID},
	})
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskStarted, map[string]any{
		"agent_id": string(exec),
		"id":       taskID,
	}); err != nil {
		t.Fatalf("task_started %s: %v", taskID, err)
	}
	if _, err := h.HandleCall(context.Background(), string(RoleExecutor), MethodTaskCompleted, map[string]any{
		"agent_id": string(exec),
		"id":       taskID,
	}); err != nil {
		t.Fatalf("task_completed %s: %v", taskID, err)
	}
	h.Registry().Deregister(exec)
}

func TestHandleTaskNeedsFix_PersistsFeedback(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	rev := emitBriefingP1(t, h)
	markStarted(t, h, "P1", "t1")

	if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodTaskNeedsFix, map[string]any{
		"agent_id": string(rev),
		"id":       "t1",
		"feedback": "missing edge case",
	}); err != nil {
		t.Fatalf("task_needs_fix: %v", err)
	}

	got := h.NeedsFixFeedback("P1-1")
	if got["t1"] != "missing edge case" {
		t.Errorf("NeedsFixFeedback[t1] = %q, want %q", got["t1"], "missing edge case")
	}
}

func TestHandleTaskApproved_ClearsFeedback(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	rev := emitBriefingP1(t, h)
	markStarted(t, h, "P1", "t1")
	if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodTaskNeedsFix, map[string]any{
		"agent_id": string(rev),
		"id":       "t1",
		"feedback": "fix it",
	}); err != nil {
		t.Fatalf("task_needs_fix: %v", err)
	}
	// On the next executor pass, the task transitions back to
	// in_progress before the reviewer approves it.
	markStarted(t, h, "P1", "t1")
	if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodTaskApproved, map[string]any{
		"agent_id": string(rev),
		"id":       "t1",
	}); err != nil {
		t.Fatalf("task_approved: %v", err)
	}
	got := h.NeedsFixFeedback("P1-1")
	if _, ok := got["t1"]; ok {
		t.Errorf("NeedsFixFeedback[t1] still present after approval: %v", got)
	}
}

func TestResetReviewOutcome_ClearsAllFeedback(t *testing.T) {
	h := NewHandler(nil, NewAgentRegistry(nil))
	emitSamplePlan(t, h)
	rev := emitBriefingP1(t, h)
	markStarted(t, h, "P1", "t1")
	markStarted(t, h, "P1", "t2")
	for _, id := range []string{"t1", "t2"} {
		if _, err := h.HandleCall(context.Background(), string(RoleReviewer), MethodTaskNeedsFix, map[string]any{
			"agent_id": string(rev),
			"id":       id,
			"feedback": "fix " + id,
		}); err != nil {
			t.Fatalf("task_needs_fix %s: %v", id, err)
		}
	}
	if got := h.NeedsFixFeedback("P1-1"); len(got) != 2 {
		t.Fatalf("expected 2 feedback entries before reset, got %v", got)
	}
	h.ResetReviewOutcome("P1-1")
	if got := h.NeedsFixFeedback("P1-1"); got != nil {
		t.Errorf("ResetReviewOutcome did not clear feedback: %v", got)
	}
}
