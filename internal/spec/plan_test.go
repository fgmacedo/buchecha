package spec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", p, err)
	}
	return string(b)
}

func TestParsePlan_English(t *testing.T) {
	plan, err := ParsePlan(loadFixture(t, "plan-en.md"), "## Implementation Plan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := plan.CountChecked(), 2; got != want {
		t.Errorf("CountChecked = %d, want %d", got, want)
	}
	if got, want := plan.CountUnchecked(), 3; got != want {
		t.Errorf("CountUnchecked = %d, want %d", got, want)
	}
	if got, want := len(plan.Phases), 2; got != want {
		t.Fatalf("len(Phases) = %d, want %d", got, want)
	}
	if got := plan.Phases[0].Title; got != "Phase 1: foo" {
		t.Errorf("Phases[0].Title = %q", got)
	}
	if got := len(plan.Phases[0].Items); got != 2 {
		t.Errorf("Phases[0].Items count = %d, want 2", got)
	}
	if !plan.Phases[0].Items[0].Checked {
		t.Errorf("Phases[0].Items[0] should be checked")
	}
	if plan.Phases[0].Items[1].Checked {
		t.Errorf("Phases[0].Items[1] should not be checked")
	}
	if got, want := plan.Phases[0].Items[0].Text, "Item one done"; got != want {
		t.Errorf("Phases[0].Items[0].Text = %q, want %q", got, want)
	}
}

func TestParsePlan_PortugueseLocalized(t *testing.T) {
	plan, err := ParsePlan(loadFixture(t, "plan-pt-br.md"), "## Plano de implementação")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := plan.CountChecked(), 2; got != want {
		t.Errorf("CountChecked = %d, want %d", got, want)
	}
	if got, want := plan.CountUnchecked(), 3; got != want {
		t.Errorf("CountUnchecked = %d, want %d", got, want)
	}
	if got, want := len(plan.Phases), 2; got != want {
		t.Fatalf("len(Phases) = %d, want %d", got, want)
	}
}

func TestParsePlan_HeadingNotFound(t *testing.T) {
	_, err := ParsePlan(loadFixture(t, "no-plan.md"), "## Implementation Plan")
	if !errors.Is(err, ErrPlanHeadingNotFound) {
		t.Errorf("err = %v, want ErrPlanHeadingNotFound", err)
	}
}

func TestParsePlan_EmptyPlan(t *testing.T) {
	plan, err := ParsePlan(loadFixture(t, "plan-empty.md"), "## Implementation Plan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := plan.CountChecked() + plan.CountUnchecked(); got != 0 {
		t.Errorf("expected zero items, got %d", got)
	}
}

func TestParsePlan_ImplicitPhase(t *testing.T) {
	plan, err := ParsePlan(loadFixture(t, "plan-implicit-phase.md"), "## Implementation Plan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := len(plan.Phases), 2; got != want {
		t.Fatalf("len(Phases) = %d, want %d", got, want)
	}
	if plan.Phases[0].Title != "" {
		t.Errorf("Phases[0].Title = %q, want empty (implicit phase)", plan.Phases[0].Title)
	}
	if plan.Phases[0].Line != 0 {
		t.Errorf("Phases[0].Line = %d, want 0 (implicit phase)", plan.Phases[0].Line)
	}
	if got := len(plan.Phases[0].Items); got != 2 {
		t.Errorf("implicit phase items = %d, want 2", got)
	}
}

func TestParsePlan_StopsAtNextH2(t *testing.T) {
	// Decoy item lives under "## Some other section" in plan-en.md and
	// MUST NOT be counted.
	plan, err := ParsePlan(loadFixture(t, "plan-en.md"), "## Implementation Plan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, ph := range plan.Phases {
		for _, it := range ph.Items {
			if it.Text == "Decoy item not in plan" {
				t.Fatal("decoy item leaked into plan parse")
			}
		}
	}
}
