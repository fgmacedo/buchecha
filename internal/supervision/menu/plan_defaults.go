package menu

import (
	"fmt"

	"github.com/fgmacedo/buchecha/internal/supervision"
)

// Type aliases re-export the supervision types under the menu namespace.
type (
	RoleMenu   = supervision.RoleMenu
	MenuOption = supervision.MenuOption
	RoleMenus  = supervision.RoleMenus
)

// FillPlanFromMenus stamps default RoleAssignments onto every Phase
// where the Planner left one nil, drawing the values from the
// corresponding role menu's first option (the user's most-preferred
// entry that survived availability filtering). Effort defaults to
// MenuOption.Efforts[0] when set; an empty Efforts slice yields an
// empty Effort, which the spawn-time validator rejects.
//
// Phases the Planner explicitly assigned are left untouched. Empty
// menus on a role skip the corresponding fill, leaving the assignment
// nil so callers can detect the missing default and surface an error.
//
// Also defaults each Task.Status to TaskPending when the Planner
// omitted it. The schema accepts plans without explicit task status,
// but TaskStatus.MarshalJSON rejects the zero value, so persisting a
// plan with empty statuses would fail.
//
// Mutates plan in place. Safe to call with a nil plan (no-op).
func FillPlanFromMenus(plan *supervision.Plan, menus RoleMenus) {
	if plan == nil {
		return
	}
	for i := range plan.Phases {
		ph := &plan.Phases[i]
		if ph.BrieferAssignment == nil {
			if a, ok := defaultAssignment(menus.Briefer); ok {
				ph.BrieferAssignment = &a
			}
		}
		if ph.ExecutorAssignment == nil {
			if a, ok := defaultAssignment(menus.Executor); ok {
				ph.ExecutorAssignment = &a
			}
		}
		if ph.ReviewerAssignment == nil {
			if a, ok := defaultAssignment(menus.Reviewer); ok {
				ph.ReviewerAssignment = &a
			}
		}
		for k := range ph.Tasks {
			t := &ph.Tasks[k]
			if t.Status == "" {
				t.Status = supervision.TaskPending
			}
		}
	}
}

func defaultAssignment(menu RoleMenu) (supervision.RoleAssignment, bool) {
	if len(menu.Options) == 0 {
		return supervision.RoleAssignment{}, false
	}
	opt := menu.Options[0]
	a := supervision.RoleAssignment{Provider: opt.Provider, Model: opt.Model}
	if len(opt.Efforts) > 0 {
		a.Effort = opt.Efforts[0]
	}
	return a, true
}

// ValidatePlanAgainstMenus checks every per-phase RoleAssignment in the
// plan against the corresponding role's menu. An assignment is valid
// when its (Provider, Model) matches some option in the menu and its
// Effort is one of that option's Efforts.
//
// Returns nil when the plan is fully consistent with the menus. Nil
// menus on a role with no assignments are accepted (no work to
// validate); a non-nil assignment whose role has an empty menu is
// rejected.
func ValidatePlanAgainstMenus(plan *supervision.Plan, menus RoleMenus) error {
	if plan == nil {
		return nil
	}
	for _, ph := range plan.Phases {
		if err := checkAssignmentAgainstMenu("briefer", ph.ID, ph.BrieferAssignment, menus.Briefer); err != nil {
			return err
		}
		if err := checkAssignmentAgainstMenu("executor", ph.ID, ph.ExecutorAssignment, menus.Executor); err != nil {
			return err
		}
		if err := checkAssignmentAgainstMenu("reviewer", ph.ID, ph.ReviewerAssignment, menus.Reviewer); err != nil {
			return err
		}
	}
	return nil
}

func checkAssignmentAgainstMenu(role, phaseID string, a *supervision.RoleAssignment, menu RoleMenu) error {
	if a == nil {
		return nil
	}
	if a.Provider == "" || a.Model == "" {
		return fmt.Errorf("director: phase %q %s_assignment missing provider or model", phaseID, role)
	}
	for _, opt := range menu.Options {
		if opt.Provider != a.Provider || opt.Model != a.Model {
			continue
		}
		if a.Effort == "" {
			return nil
		}
		for _, e := range opt.Efforts {
			if e == a.Effort {
				return nil
			}
		}
		return fmt.Errorf("director: phase %q %s_assignment effort %q not allowed for %s/%s (allowed: %v)",
			phaseID, role, a.Effort, a.Provider, a.Model, opt.Efforts)
	}
	return fmt.Errorf("director: phase %q %s_assignment %s/%s is not in the role menu",
		phaseID, role, a.Provider, a.Model)
}
