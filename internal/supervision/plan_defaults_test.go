package supervision

import "testing"

func TestFillPlanFromMenus_NilPlanIsNoop(t *testing.T) {
	menus := RoleMenus{Executor: RoleMenu{Options: []MenuOption{
		{Provider: "claude", Model: "m", Efforts: []string{"low"}},
	}}}
	FillPlanFromMenus(nil, menus)
}

func TestFillPlanFromMenus_EmptyMenusLeaveAssignmentsNil(t *testing.T) {
	plan := &Plan{Phases: []Phase{
		{ID: "p1", Tasks: []Task{{ID: "t1"}}},
	}}
	FillPlanFromMenus(plan, RoleMenus{})
	ph := plan.Phases[0]
	if ph.BrieferAssignment != nil || ph.ExecutorAssignment != nil || ph.ReviewerAssignment != nil {
		t.Errorf("empty menus should leave assignments nil; got %+v", ph)
	}
}

func TestFillPlanFromMenus_FillsMissingAssignmentsAcrossRoles(t *testing.T) {
	plan := &Plan{Phases: []Phase{
		{ID: "p1", Tasks: []Task{{ID: "t1"}}},
		{ID: "p2", Tasks: []Task{{ID: "t2"}}},
	}}
	menus := RoleMenus{
		Briefer:  RoleMenu{Options: []MenuOption{{Provider: "claude", Model: "default-mid", Efforts: []string{"medium"}}}},
		Executor: RoleMenu{Options: []MenuOption{{Provider: "claude", Model: "default-fast", Efforts: []string{"low"}}}},
		Reviewer: RoleMenu{Options: []MenuOption{{Provider: "claude", Model: "default-mid", Efforts: []string{"medium"}}}},
	}
	FillPlanFromMenus(plan, menus)
	for _, ph := range plan.Phases {
		if ph.BrieferAssignment == nil || ph.BrieferAssignment.Model != "default-mid" || ph.BrieferAssignment.Provider != "claude" {
			t.Errorf("phase %s briefer = %+v, want claude/default-mid", ph.ID, ph.BrieferAssignment)
		}
		if ph.ExecutorAssignment == nil || ph.ExecutorAssignment.Model != "default-fast" || ph.ExecutorAssignment.Effort != "low" {
			t.Errorf("phase %s executor = %+v, want default-fast/low", ph.ID, ph.ExecutorAssignment)
		}
		if ph.ReviewerAssignment == nil || ph.ReviewerAssignment.Model != "default-mid" || ph.ReviewerAssignment.Effort != "medium" {
			t.Errorf("phase %s reviewer = %+v, want default-mid/medium", ph.ID, ph.ReviewerAssignment)
		}
	}
}

func TestFillPlanFromMenus_PreservesPlannerAssignments(t *testing.T) {
	planner := &RoleAssignment{Provider: "claude", Model: "frontier-explicit", Effort: "high"}
	plan := &Plan{Phases: []Phase{
		{
			ID: "p1", Tasks: []Task{{ID: "t1"}},
			ExecutorAssignment: planner,
		},
	}}
	menus := RoleMenus{
		Executor: RoleMenu{Options: []MenuOption{{Provider: "claude", Model: "default-fast", Efforts: []string{"low"}}}},
	}
	FillPlanFromMenus(plan, menus)
	got := plan.Phases[0].ExecutorAssignment
	if got != planner {
		t.Errorf("planner-set executor pointer was replaced; got %p, want %p", got, planner)
	}
	if got.Model != "frontier-explicit" || got.Effort != "high" {
		t.Errorf("planner-set executor was overwritten: %+v", got)
	}
}

func TestFillPlanFromMenus_DefaultsEmptyTaskStatusToPending(t *testing.T) {
	plan := &Plan{Phases: []Phase{
		{
			ID: "p1",
			Tasks: []Task{
				{ID: "t1"},
				{ID: "t2", Status: TaskDone},
				{ID: "t3"},
			},
		},
	}}
	FillPlanFromMenus(plan, RoleMenus{})
	tasks := plan.Phases[0].Tasks
	if tasks[0].Status != TaskPending {
		t.Errorf("t1 status = %q, want pending", tasks[0].Status)
	}
	if tasks[1].Status != TaskDone {
		t.Errorf("t2 status = %q, want preserved done", tasks[1].Status)
	}
	if tasks[2].Status != TaskPending {
		t.Errorf("t3 status = %q, want pending", tasks[2].Status)
	}
}

func TestValidatePlanAgainstMenus_AcceptsMatchingTriple(t *testing.T) {
	menus := RoleMenus{
		Executor: RoleMenu{Options: []MenuOption{
			{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: []string{"medium", "high"}},
		}},
	}
	plan := &Plan{Phases: []Phase{
		{
			ID: "p1", Tasks: []Task{{ID: "t1"}},
			ExecutorAssignment: &RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "high"},
		},
	}}
	if err := ValidatePlanAgainstMenus(plan, menus); err != nil {
		t.Errorf("expected accept, got %v", err)
	}
}

func TestValidatePlanAgainstMenus_RejectsModelOutsideMenu(t *testing.T) {
	menus := RoleMenus{
		Executor: RoleMenu{Options: []MenuOption{
			{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: []string{"medium"}},
		}},
	}
	plan := &Plan{Phases: []Phase{
		{
			ID: "p1", Tasks: []Task{{ID: "t1"}},
			ExecutorAssignment: &RoleAssignment{Provider: "claude", Model: "claude-opus-4-7", Effort: "high"},
		},
	}}
	if err := ValidatePlanAgainstMenus(plan, menus); err == nil {
		t.Errorf("expected reject for model outside menu")
	}
}

func TestValidatePlanAgainstMenus_RejectsEffortNotAllowed(t *testing.T) {
	menus := RoleMenus{
		Executor: RoleMenu{Options: []MenuOption{
			{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: []string{"medium"}},
		}},
	}
	plan := &Plan{Phases: []Phase{
		{
			ID: "p1", Tasks: []Task{{ID: "t1"}},
			ExecutorAssignment: &RoleAssignment{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "high"},
		},
	}}
	if err := ValidatePlanAgainstMenus(plan, menus); err == nil {
		t.Errorf("expected reject for effort outside option's allowed list")
	}
}
