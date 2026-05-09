package director

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// PlanDiff describes how a re-plan changes the canonical Plan: phases
// added, removed, modified in place, and unchanged. It is a pure
// function of two Plan snapshots, addressed by Phase.ID. The structure
// is the substrate for both the text renderer (RenderPlanDiff) and the
// JSON serialization driven by the same struct shape.
type PlanDiff struct {
	GoalChanged            bool                `json:"goal_changed"`
	OldGoal                string              `json:"old_goal,omitempty"`
	NewGoal                string              `json:"new_goal,omitempty"`
	SuccessCriteriaChanged bool                `json:"success_criteria_changed"`
	OldSuccessCriteria     []string            `json:"old_success_criteria,omitempty"`
	NewSuccessCriteria     []string            `json:"new_success_criteria,omitempty"`
	Added                  []Phase             `json:"added"`
	Removed                []Phase             `json:"removed"`
	Modified               []PhaseModification `json:"modified"`
	Unchanged              []string            `json:"unchanged"`
}

// PhaseModification records a phase that exists in both plans but
// changed in at least one field. Changes is a list of human-readable
// summaries (one per modified field) suitable for the renderer; Old
// and New carry the full Phase snapshots so a JSON consumer can
// produce its own visualization.
type PhaseModification struct {
	ID      string   `json:"id"`
	Old     Phase    `json:"old"`
	New     Phase    `json:"new"`
	Changes []string `json:"changes"`
}

// Empty reports whether the diff contains no changes at all (no
// added/removed/modified phases and identical goal + success criteria).
// Useful for short-circuit rendering: an empty diff means the planner
// produced exactly the same plan despite a spec hash change.
func (d *PlanDiff) Empty() bool {
	if d == nil {
		return true
	}
	return !d.GoalChanged &&
		!d.SuccessCriteriaChanged &&
		len(d.Added) == 0 &&
		len(d.Removed) == 0 &&
		len(d.Modified) == 0
}

// ComputePlanDiff returns the difference from old to new. Both inputs
// may be nil (treated as empty). Phases are matched by ID; same-ID
// phases with deep-equal field values land in Unchanged, otherwise in
// Modified with a per-field summary.
func ComputePlanDiff(old, newPlan *Plan) *PlanDiff {
	d := &PlanDiff{}
	oldPhases := indexPhases(old)
	newPhases := indexPhases(newPlan)

	if old != nil && newPlan != nil {
		if old.Goal != newPlan.Goal {
			d.GoalChanged = true
			d.OldGoal = old.Goal
			d.NewGoal = newPlan.Goal
		}
		if !equalStringSlices(old.SuccessCriteria, newPlan.SuccessCriteria) {
			d.SuccessCriteriaChanged = true
			d.OldSuccessCriteria = slices.Clone(old.SuccessCriteria)
			d.NewSuccessCriteria = slices.Clone(newPlan.SuccessCriteria)
		}
	}

	if newPlan != nil {
		for i := range newPlan.Phases {
			ph := newPlan.Phases[i]
			oldPh, ok := oldPhases[ph.ID]
			if !ok {
				d.Added = append(d.Added, ph)
				continue
			}
			changes := diffPhase(*oldPh, ph)
			if len(changes) == 0 {
				d.Unchanged = append(d.Unchanged, ph.ID)
				continue
			}
			d.Modified = append(d.Modified, PhaseModification{
				ID:      ph.ID,
				Old:     *oldPh,
				New:     ph,
				Changes: changes,
			})
		}
	}

	if old != nil {
		for i := range old.Phases {
			ph := old.Phases[i]
			if _, ok := newPhases[ph.ID]; !ok {
				d.Removed = append(d.Removed, ph)
			}
		}
	}

	sort.Strings(d.Unchanged)
	sort.Slice(d.Added, func(i, j int) bool { return d.Added[i].ID < d.Added[j].ID })
	sort.Slice(d.Removed, func(i, j int) bool { return d.Removed[i].ID < d.Removed[j].ID })
	sort.Slice(d.Modified, func(i, j int) bool { return d.Modified[i].ID < d.Modified[j].ID })
	return d
}

func indexPhases(p *Plan) map[string]*Phase {
	out := map[string]*Phase{}
	if p == nil {
		return out
	}
	for i := range p.Phases {
		out[p.Phases[i].ID] = &p.Phases[i]
	}
	return out
}

func diffPhase(a, b Phase) []string {
	var out []string
	if a.Title != b.Title {
		out = append(out, fmt.Sprintf("title: %q → %q", a.Title, b.Title))
	}
	if a.Intent != b.Intent {
		out = append(out, "intent changed")
	}
	if !equalStringSlices(a.DependsOn, b.DependsOn) {
		out = append(out, fmt.Sprintf("depends_on: [%s] → [%s]",
			strings.Join(a.DependsOn, ","), strings.Join(b.DependsOn, ",")))
	}
	if a.Parallelizable != b.Parallelizable {
		out = append(out, fmt.Sprintf("parallelizable: %t → %t", a.Parallelizable, b.Parallelizable))
	}
	if !equalStringSlices(a.ScopeIn, b.ScopeIn) {
		out = append(out, "scope_in changed")
	}
	if !equalStringSlices(a.ScopeOut, b.ScopeOut) {
		out = append(out, "scope_out changed")
	}
	if !reflect.DeepEqual(a.Tasks, b.Tasks) {
		out = append(out, fmt.Sprintf("tasks: %d → %d", len(a.Tasks), len(b.Tasks)))
	}
	if !reflect.DeepEqual(a.BrieferAssignment, b.BrieferAssignment) {
		out = append(out, "briefer_assignment changed")
	}
	if !reflect.DeepEqual(a.ExecutorAssignment, b.ExecutorAssignment) {
		out = append(out, "executor_assignment changed")
	}
	if !reflect.DeepEqual(a.ReviewerAssignment, b.ReviewerAssignment) {
		out = append(out, "reviewer_assignment changed")
	}
	if !reflect.DeepEqual(a.PreparedBriefing, b.PreparedBriefing) {
		out = append(out, "prepared_briefing changed")
	}
	if a.SkipReview != b.SkipReview {
		out = append(out, fmt.Sprintf("skip_review: %t → %t", a.SkipReview, b.SkipReview))
	}
	return out
}

func equalStringSlices(a, b []string) bool {
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

// RenderPlanDiff prints the diff in plain text suitable for the
// re-plan confirmation prompt. The output is ASCII-only and never
// colorizes; callers pick the writer (typically stderr).
func RenderPlanDiff(d *PlanDiff, w io.Writer) {
	if d == nil {
		fmt.Fprintln(w, "bcc: plan diff: <nil>")
		return
	}
	fmt.Fprintln(w, "bcc: Director plan diff (old → new)")
	if d.GoalChanged {
		fmt.Fprintln(w, "  goal:")
		fmt.Fprintf(w, "    - %s\n", d.OldGoal)
		fmt.Fprintf(w, "    + %s\n", d.NewGoal)
	}
	if d.SuccessCriteriaChanged {
		fmt.Fprintln(w, "  success_criteria changed:")
		for _, c := range d.OldSuccessCriteria {
			fmt.Fprintf(w, "    - %s\n", c)
		}
		for _, c := range d.NewSuccessCriteria {
			fmt.Fprintf(w, "    + %s\n", c)
		}
	}
	if len(d.Added) > 0 {
		fmt.Fprintf(w, "  added (%d):\n", len(d.Added))
		for _, ph := range d.Added {
			fmt.Fprintf(w, "    + %s  %s\n", ph.ID, ph.Title)
		}
	}
	if len(d.Removed) > 0 {
		fmt.Fprintf(w, "  removed (%d):\n", len(d.Removed))
		for _, ph := range d.Removed {
			fmt.Fprintf(w, "    - %s  %s\n", ph.ID, ph.Title)
		}
	}
	if len(d.Modified) > 0 {
		fmt.Fprintf(w, "  modified (%d):\n", len(d.Modified))
		for _, m := range d.Modified {
			fmt.Fprintf(w, "    ~ %s  %s\n", m.ID, m.New.Title)
			for _, c := range m.Changes {
				fmt.Fprintf(w, "        %s\n", c)
			}
		}
	}
	if len(d.Unchanged) > 0 {
		fmt.Fprintf(w, "  unchanged (%d): %s\n", len(d.Unchanged), strings.Join(d.Unchanged, ","))
	}
	if d.Empty() {
		fmt.Fprintln(w, "  (no changes)")
	}
}

// MarshalPlanDiffJSON returns the JSON encoding of d. Stable field
// ordering comes from the struct tags; consumers parse it into the
// same shape on the other side.
func MarshalPlanDiffJSON(d *PlanDiff) ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}
