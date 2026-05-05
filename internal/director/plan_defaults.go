package director

// PlanDefaults carries the per-role model and effort the run falls
// back to when the Planner does not set an explicit Phase assignment.
// Each field is a RoleAssignment so callers can pass the same value
// shape they already expose elsewhere; an empty (zero) RoleAssignment
// means "no default known", and the corresponding role assignment on
// any Phase that omits it stays nil.
//
// PlanDefaults is consumed by FillPlanDefaults at bcc_plan_emit time.
// Filling at emit time (instead of resolving at briefer/executor
// runtime) keeps the persisted plan.json self-describing: every Phase
// shows the model and effort it actually ran with, not "(default)".
type PlanDefaults struct {
	Briefer  RoleAssignment
	Executor RoleAssignment
	Reviewer RoleAssignment
}

// FillPlanDefaults populates each Phase's BrieferAssignment,
// ExecutorAssignment, and ReviewerAssignment with the matching
// PlanDefaults entry when the Planner left it nil. Phases that the
// Planner explicitly assigned are left untouched. Empty default
// fields (Model == "" and Effort == "") are treated as "no default
// known" and skipped, leaving the assignment nil so the loop's own
// fallback applies.
//
// Mutates plan in place. Safe to call with a nil plan (no-op) or
// with a fully-assigned plan (no-op). Not safe for concurrent use;
// the caller serializes.
func FillPlanDefaults(plan *Plan, defaults PlanDefaults) {
	if plan == nil {
		return
	}
	for i := range plan.Phases {
		ph := &plan.Phases[i]
		if ph.BrieferAssignment == nil && !defaults.Briefer.isZero() {
			a := defaults.Briefer
			ph.BrieferAssignment = &a
		}
		if ph.ExecutorAssignment == nil && !defaults.Executor.isZero() {
			a := defaults.Executor
			ph.ExecutorAssignment = &a
		}
		if ph.ReviewerAssignment == nil && !defaults.Reviewer.isZero() {
			a := defaults.Reviewer
			ph.ReviewerAssignment = &a
		}
	}
}

func (a RoleAssignment) isZero() bool {
	return a.Model == "" && a.Effort == ""
}
