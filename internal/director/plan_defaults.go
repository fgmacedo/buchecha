package director

import "fmt"

// RoleMenu is the per-role list of (provider, model, efforts) triples
// the Planner is allowed to attribute on a phase. It mirrors
// config.RolePolicy at the wire layer; the loop converts the config
// shape into this one before binding it to the run-wide handler so the
// director package stays independent of the config package.
type RoleMenu struct {
	Options []MenuOption
}

// MenuOption is one entry in a role's menu.
//
// Tier and Summary are non-authoritative metadata pulled from the
// curated registry (config.KnownModelByName) at wiring time so the
// Planner prompt can render hints alongside the user's options. They
// are empty when the user declared a model bcc has no curated metadata
// for; the prompt rendering omits the corresponding line.
type MenuOption struct {
	Provider string
	Model    string
	Efforts  []string
	Note     string
	Tier     string
	Summary  string
}

// RoleMenus carries a menu per role. Empty menus on a role mean "no
// allowed options"; in practice the loop fills defaults before binding
// so this is non-empty for every role at runtime.
type RoleMenus struct {
	Planner  RoleMenu
	Briefer  RoleMenu
	Executor RoleMenu
	Reviewer RoleMenu
}

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
func FillPlanFromMenus(plan *Plan, menus RoleMenus) {
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
				t.Status = TaskPending
			}
		}
	}
}

func defaultAssignment(menu RoleMenu) (RoleAssignment, bool) {
	if len(menu.Options) == 0 {
		return RoleAssignment{}, false
	}
	opt := menu.Options[0]
	a := RoleAssignment{Provider: opt.Provider, Model: opt.Model}
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
func ValidatePlanAgainstMenus(plan *Plan, menus RoleMenus) error {
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

func checkAssignmentAgainstMenu(role, phaseID string, a *RoleAssignment, menu RoleMenu) error {
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
