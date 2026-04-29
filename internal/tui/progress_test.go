package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/spec"
)

func samplePlan() spec.Plan {
	return spec.Plan{
		Phases: []spec.Phase{
			{
				Title: "P1: setup",
				Items: []spec.Item{
					{Text: "a", Checked: true},
					{Text: "b", Checked: true},
				},
			},
			{
				Title: "P2: build",
				Items: []spec.Item{
					{Text: "c", Checked: true},
					{Text: "d", Checked: false},
					{Text: "e", Checked: false},
				},
			},
			{
				Title: "P3: ship",
				Items: []spec.Item{
					{Text: "f", Checked: false},
				},
			},
		},
	}
}

func TestProgress_view_RendersCheckboxesAndCounts(t *testing.T) {
	p := progressPanel{currentPhaseIdx: -1}
	p.onSpecParsed(samplePlan())

	out := p.view()
	for _, w := range []string{"progress", "P1", "P2", "P3", "3/6 items"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in view\n%s", w, out)
		}
	}
	// Current phase marker on P2 (first phase with [ ]).
	if !strings.Contains(out, "►") {
		t.Errorf("missing current-phase marker:\n%s", out)
	}
}

func TestProgress_onIterationFinished_TracksRecentDurations(t *testing.T) {
	p := progressPanel{}
	for i := 0; i < 40; i++ {
		p.onIterationFinished(time.Duration(i+1) * time.Second)
	}
	if len(p.durations) != 32 {
		t.Errorf("durations not capped to 32, got %d", len(p.durations))
	}
}

func TestComputeETA_AveragesDurations(t *testing.T) {
	durations := []time.Duration{2 * time.Minute, 4 * time.Minute, 6 * time.Minute}
	got := computeETA(durations, 3)
	want := 4 * time.Minute * 3
	if got != want {
		t.Errorf("ETA = %v, want %v", got, want)
	}
}

func TestComputeETA_NoDurationsOrNoRemaining(t *testing.T) {
	if got := computeETA(nil, 5); got != 0 {
		t.Errorf("no durations should yield 0, got %v", got)
	}
	if got := computeETA([]time.Duration{time.Minute}, 0); got != 0 {
		t.Errorf("no remaining should yield 0, got %v", got)
	}
}

func TestRenderBar_ProportionalFill(t *testing.T) {
	out := renderBar(5, 10, 20)
	// 5/10 of 20 = 10 filled chars.
	if !strings.Contains(out, strings.Repeat("█", 10)) {
		t.Errorf("expected 10 filled chars in %q", out)
	}
	if !strings.Contains(out, strings.Repeat("░", 10)) {
		t.Errorf("expected 10 empty chars in %q", out)
	}
}

func TestRenderBar_ZeroTotal(t *testing.T) {
	out := renderBar(0, 0, 4)
	if !strings.Contains(out, strings.Repeat("░", 4)) {
		t.Errorf("zero total should render full empty bar, got %q", out)
	}
}

func TestRenderBar_NonZeroSmallProgressShowsAtLeastOne(t *testing.T) {
	out := renderBar(1, 100, 10) // 0.1 chars rounds to 0 in math but should show 1.
	if !strings.Contains(out, "█") {
		t.Errorf("any progress should yield at least one █, got %q", out)
	}
}

func TestPhaseLabel_PrefersPrefixWhenPresent(t *testing.T) {
	cases := []struct {
		idx   int
		title string
		want  string
	}{
		{0, "P2.5: panels", "P2.5"},
		{1, "P3: heuristics", "P3"},
		{2, "", "P3"},
		{3, "Setup phase", "Setup"},
	}
	for _, c := range cases {
		got := phaseLabel(c.idx, spec.Phase{Title: c.title})
		if got != c.want {
			t.Errorf("phaseLabel idx=%d title=%q = %q, want %q", c.idx, c.title, got, c.want)
		}
	}
}

func TestFirstUncheckedPhase(t *testing.T) {
	plan := samplePlan()
	if got := firstUncheckedPhase(plan); got != 1 {
		t.Errorf("firstUncheckedPhase = %d, want 1 (P2 has first [ ])", got)
	}
	allDone := spec.Plan{Phases: []spec.Phase{
		{Items: []spec.Item{{Checked: true}}},
		{Items: []spec.Item{{Checked: true}}},
	}}
	if got := firstUncheckedPhase(allDone); got != -1 {
		t.Errorf("all-checked plan should yield -1, got %d", got)
	}
}
