package director

import "testing"

func TestFillPlanDefaults_NilPlanIsNoop(t *testing.T) {
	defaults := PlanDefaults{Executor: RoleAssignment{Model: "m", Effort: "low"}}
	FillPlanDefaults(nil, defaults)
}

func TestFillPlanDefaults_EmptyDefaultsLeaveAssignmentsNil(t *testing.T) {
	plan := &Plan{Phases: []Phase{
		{ID: "p1", Tasks: []Task{{ID: "t1"}}},
	}}
	FillPlanDefaults(plan, PlanDefaults{})
	ph := plan.Phases[0]
	if ph.BrieferAssignment != nil || ph.ExecutorAssignment != nil || ph.ReviewerAssignment != nil {
		t.Errorf("empty defaults should leave assignments nil; got %+v", ph)
	}
}

func TestFillPlanDefaults_FillsMissingAssignmentsAcrossRoles(t *testing.T) {
	plan := &Plan{Phases: []Phase{
		{ID: "p1", Tasks: []Task{{ID: "t1"}}},
		{ID: "p2", Tasks: []Task{{ID: "t2"}}},
	}}
	defaults := PlanDefaults{
		Briefer:  RoleAssignment{Model: "default-mid"},
		Executor: RoleAssignment{Model: "default-fast", Effort: "low"},
		Reviewer: RoleAssignment{Model: "default-mid", Effort: "medium"},
	}
	FillPlanDefaults(plan, defaults)
	for _, ph := range plan.Phases {
		if ph.BrieferAssignment == nil || ph.BrieferAssignment.Model != "default-mid" {
			t.Errorf("phase %s briefer = %+v, want default-mid", ph.ID, ph.BrieferAssignment)
		}
		if ph.ExecutorAssignment == nil || ph.ExecutorAssignment.Model != "default-fast" || ph.ExecutorAssignment.Effort != "low" {
			t.Errorf("phase %s executor = %+v, want default-fast/low", ph.ID, ph.ExecutorAssignment)
		}
		if ph.ReviewerAssignment == nil || ph.ReviewerAssignment.Model != "default-mid" || ph.ReviewerAssignment.Effort != "medium" {
			t.Errorf("phase %s reviewer = %+v, want default-mid/medium", ph.ID, ph.ReviewerAssignment)
		}
	}
}

func TestFillPlanDefaults_PreservesPlannerAssignments(t *testing.T) {
	planner := &RoleAssignment{Model: "frontier-explicit", Effort: "high"}
	plan := &Plan{Phases: []Phase{
		{
			ID: "p1", Tasks: []Task{{ID: "t1"}},
			ExecutorAssignment: planner,
		},
	}}
	defaults := PlanDefaults{
		Executor: RoleAssignment{Model: "default-fast", Effort: "low"},
	}
	FillPlanDefaults(plan, defaults)
	got := plan.Phases[0].ExecutorAssignment
	if got != planner {
		t.Errorf("planner-set executor pointer was replaced; got %p, want %p", got, planner)
	}
	if got.Model != "frontier-explicit" || got.Effort != "high" {
		t.Errorf("planner-set executor was overwritten: %+v", got)
	}
}

func TestFillPlanDefaults_OnlyFillsRolesWithKnownDefaults(t *testing.T) {
	plan := &Plan{Phases: []Phase{
		{ID: "p1", Tasks: []Task{{ID: "t1"}}},
	}}
	// Only executor has a default; briefer and reviewer should stay nil.
	defaults := PlanDefaults{
		Executor: RoleAssignment{Model: "default-fast"},
	}
	FillPlanDefaults(plan, defaults)
	ph := plan.Phases[0]
	if ph.ExecutorAssignment == nil || ph.ExecutorAssignment.Model != "default-fast" {
		t.Errorf("executor default not applied: %+v", ph.ExecutorAssignment)
	}
	if ph.BrieferAssignment != nil {
		t.Errorf("briefer should stay nil with empty default; got %+v", ph.BrieferAssignment)
	}
	if ph.ReviewerAssignment != nil {
		t.Errorf("reviewer should stay nil with empty default; got %+v", ph.ReviewerAssignment)
	}
}
