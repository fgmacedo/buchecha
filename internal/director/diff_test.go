package director

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func makePhase(id, title string) Phase {
	return Phase{
		ID:     id,
		Title:  title,
		Intent: "do " + id,
		Tasks: []Task{
			{
				ID:     id + "-t1",
				Title:  id + " task",
				Intent: "build the thing",
				Acceptance: []AcceptanceItem{
					{ID: id + "-a1", Description: "compiles", Evidence: EvidenceBuild},
				},
				Status:      TaskPending,
				RetryBudget: 1,
			},
		},
	}
}

func TestComputePlanDiff_NilOld_AllAdded(t *testing.T) {
	newPlan := &Plan{
		Goal:   "x",
		Phases: []Phase{makePhase("p1", "First"), makePhase("p2", "Second")},
	}
	d := ComputePlanDiff(nil, newPlan)
	if len(d.Added) != 2 {
		t.Fatalf("Added = %d, want 2", len(d.Added))
	}
	if len(d.Removed) != 0 || len(d.Modified) != 0 || len(d.Unchanged) != 0 {
		t.Errorf("only Added expected, got removed=%d modified=%d unchanged=%d",
			len(d.Removed), len(d.Modified), len(d.Unchanged))
	}
	if d.Empty() {
		t.Error("Empty() = true, want false")
	}
}

func TestComputePlanDiff_NilNew_AllRemoved(t *testing.T) {
	old := &Plan{
		Goal:   "x",
		Phases: []Phase{makePhase("p1", "First")},
	}
	d := ComputePlanDiff(old, nil)
	if len(d.Removed) != 1 {
		t.Fatalf("Removed = %d, want 1", len(d.Removed))
	}
	if len(d.Added) != 0 || len(d.Modified) != 0 {
		t.Errorf("only Removed expected: %+v", d)
	}
}

func TestComputePlanDiff_BothNil_Empty(t *testing.T) {
	d := ComputePlanDiff(nil, nil)
	if !d.Empty() {
		t.Errorf("Empty() = false, want true: %+v", d)
	}
}

func TestComputePlanDiff_IdenticalPlans_AllUnchanged(t *testing.T) {
	plan := &Plan{
		Goal:            "x",
		SuccessCriteria: []string{"a"},
		Phases:          []Phase{makePhase("p1", "First")},
	}
	other := &Plan{
		Goal:            "x",
		SuccessCriteria: []string{"a"},
		Phases:          []Phase{makePhase("p1", "First")},
	}
	d := ComputePlanDiff(plan, other)
	if !d.Empty() {
		t.Errorf("Empty() = false on identical plans: %+v", d)
	}
	if len(d.Unchanged) != 1 || d.Unchanged[0] != "p1" {
		t.Errorf("Unchanged = %v, want [p1]", d.Unchanged)
	}
}

func TestComputePlanDiff_GoalAndCriteriaChanges(t *testing.T) {
	old := &Plan{Goal: "old", SuccessCriteria: []string{"a"}, Phases: []Phase{makePhase("p1", "T")}}
	newPlan := &Plan{Goal: "new", SuccessCriteria: []string{"a", "b"}, Phases: []Phase{makePhase("p1", "T")}}
	d := ComputePlanDiff(old, newPlan)
	if !d.GoalChanged || d.OldGoal != "old" || d.NewGoal != "new" {
		t.Errorf("GoalChanged misreported: %+v", d)
	}
	if !d.SuccessCriteriaChanged {
		t.Error("SuccessCriteriaChanged = false, want true")
	}
}

func TestComputePlanDiff_ModifiedPhase_TitleAndDeps(t *testing.T) {
	old := &Plan{
		Goal: "x",
		Phases: []Phase{
			makePhase("p1", "Old title"),
			makePhase("p2", "Stable"),
		},
	}
	newPlan := &Plan{
		Goal: "x",
		Phases: []Phase{
			func() Phase {
				ph := makePhase("p1", "New title")
				ph.DependsOn = []string{"p2"}
				ph.Priority = 5
				return ph
			}(),
			makePhase("p2", "Stable"),
		},
	}
	d := ComputePlanDiff(old, newPlan)
	if len(d.Modified) != 1 || d.Modified[0].ID != "p1" {
		t.Fatalf("Modified = %+v, want one entry for p1", d.Modified)
	}
	mod := d.Modified[0]
	joined := strings.Join(mod.Changes, "|")
	for _, want := range []string{"title", "depends_on", "priority"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Changes missing %q: %v", want, mod.Changes)
		}
	}
	if len(d.Unchanged) != 1 || d.Unchanged[0] != "p2" {
		t.Errorf("Unchanged = %v, want [p2]", d.Unchanged)
	}
}

func TestComputePlanDiff_AddedAndRemoved(t *testing.T) {
	old := &Plan{Goal: "x", Phases: []Phase{makePhase("p1", "Stay"), makePhase("p2", "Drop")}}
	newPlan := &Plan{Goal: "x", Phases: []Phase{makePhase("p1", "Stay"), makePhase("p3", "Brand new")}}
	d := ComputePlanDiff(old, newPlan)
	if len(d.Added) != 1 || d.Added[0].ID != "p3" {
		t.Errorf("Added = %v, want [p3]", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].ID != "p2" {
		t.Errorf("Removed = %v, want [p2]", d.Removed)
	}
	if len(d.Unchanged) != 1 {
		t.Errorf("Unchanged = %v, want [p1]", d.Unchanged)
	}
}

func TestRenderPlanDiff_EmptyMessage(t *testing.T) {
	d := ComputePlanDiff(&Plan{Goal: "x"}, &Plan{Goal: "x"})
	var b bytes.Buffer
	RenderPlanDiff(d, &b)
	if !strings.Contains(b.String(), "(no changes)") {
		t.Errorf("renderer missing (no changes) marker: %q", b.String())
	}
}

func TestRenderPlanDiff_AddRemoveModify(t *testing.T) {
	old := &Plan{
		Goal:   "old",
		Phases: []Phase{makePhase("p1", "First"), makePhase("p2", "Second")},
	}
	newPlan := &Plan{
		Goal: "new",
		Phases: []Phase{
			func() Phase { ph := makePhase("p1", "First v2"); return ph }(),
			makePhase("p3", "Third"),
		},
	}
	d := ComputePlanDiff(old, newPlan)
	var b bytes.Buffer
	RenderPlanDiff(d, &b)
	out := b.String()
	for _, want := range []string{
		"plan diff",
		"goal:",
		"+ p3",
		"- p2",
		"~ p1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderer output missing %q: %s", want, out)
		}
	}
}

func TestMarshalPlanDiffJSON_RoundTrip(t *testing.T) {
	old := &Plan{Goal: "x", Phases: []Phase{makePhase("p1", "A")}}
	newPlan := &Plan{Goal: "x", Phases: []Phase{makePhase("p1", "B")}}
	d := ComputePlanDiff(old, newPlan)
	data, err := MarshalPlanDiffJSON(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PlanDiff
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Modified) != 1 || got.Modified[0].ID != "p1" {
		t.Errorf("round-trip lost modification: %+v", got)
	}
}
