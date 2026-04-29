package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestComputeLayout_FullWidthAndNowHealthSplit(t *testing.T) {
	cases := []struct {
		width  int
		wantNW int
	}{
		{80, 48},   // 60% of 80 = 48
		{120, 72},  // 60% of 120 = 72
		{200, 120}, // 60% of 200 = 120
	}
	for _, c := range cases {
		got := computeLayout(c.width)
		if got.headerW != c.width {
			t.Errorf("width=%d: headerW=%d, want %d", c.width, got.headerW, c.width)
		}
		if got.progressW != c.width || got.riskW != c.width || got.actionsW != c.width {
			t.Errorf("width=%d: full-width panels mismatch: %+v", c.width, got)
		}
		if got.nowW != c.wantNW {
			t.Errorf("width=%d: nowW=%d, want %d", c.width, got.nowW, c.wantNW)
		}
		if got.nowW+got.healthW != c.width {
			t.Errorf("width=%d: nowW+healthW=%d, want %d (sum must equal terminal width)",
				c.width, got.nowW+got.healthW, c.width)
		}
	}
}

func TestComputeLayout_ZeroOrNegativeYieldsZero(t *testing.T) {
	if got := computeLayout(0); got != (layout{}) {
		t.Errorf("computeLayout(0) = %+v, want zero", got)
	}
	if got := computeLayout(-5); got != (layout{}) {
		t.Errorf("computeLayout(-5) = %+v, want zero", got)
	}
}

func TestUpdate_WindowSizeMsgRecomputesLayoutCache(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := got.(Model)
	if mm.layout.headerW != 120 {
		t.Errorf("layout.headerW = %d, want 120", mm.layout.headerW)
	}
	if mm.layout.nowW+mm.layout.healthW != 120 {
		t.Errorf("layout split does not sum to width 120: %+v", mm.layout)
	}
}

// TestView_NoLineExceedsWidth renders the dashboard at the three review
// widths (80, 120, 200) and asserts every emitted line fits within the
// terminal width per P2.8 sub-item 7 (a).
func TestView_NoLineExceedsWidth(t *testing.T) {
	for _, w := range []int{80, 120, 200} {
		m, _, _, _ := newTestModel(t)
		mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 40})
		out := mm.(Model).View()
		for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("width=%d line %d exceeds terminal: lipgloss.Width=%d\n%q",
					w, i, got, line)
			}
		}
	}
}

// TestView_NowAndHealthShareTopBand asserts the now+health row really
// is a single horizontally-joined block: the first line of the joined
// region contains both box top-borders side by side, top-aligned per
// P2.8 sub-item 7 (b).
func TestView_NowAndHealthShareTopBand(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	out := mm.(Model).View()
	lines := strings.Split(out, "\n")

	// Find the top-border line that carries the "now" title; that same
	// line must also contain the "health" title because the two boxes
	// are joined horizontally on the same vertical band.
	found := false
	for _, line := range lines {
		if strings.Contains(line, " now ") && strings.Contains(line, " health ") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no single line contains both 'now' and 'health' top-border titles\n%s", out)
	}
}

// TestView_HeaderTitleContainsBranchAndIter checks P2.8 sub-item 7 (c):
// the header box's top border embeds branch and iter n/N.
func TestView_HeaderTitleContainsBranchAndIter(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	m.layout = computeLayout(120)
	m.header.branch = "feat/x"
	m.header.iter = 3
	m.header.maxIter = 5
	out := m.View()
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		t.Fatal("View() returned empty output")
	}
	top := lines[0]
	if !strings.Contains(top, "feat/x") {
		t.Errorf("header top border missing branch 'feat/x':\n%s", top)
	}
	if !strings.Contains(top, "iter 3/5") {
		t.Errorf("header top border missing iter 3/5:\n%s", top)
	}
}

// TestView_BelowThresholdFallsBackToPlain checks P2.8 sub-item 7 (d):
// at widths under boxThreshold (40), the box wrapper degrades to plain
// "[ name ]" lines and no rendered line exceeds the width.
func TestView_BelowThresholdFallsBackToPlain(t *testing.T) {
	const w = 30
	m, _, _, _ := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
	out := mm.(Model).View()
	// Plain fallback: no rounded-border characters anywhere.
	for _, glyph := range []string{"╭", "╮", "╰", "╯", "│"} {
		if strings.Contains(out, glyph) {
			t.Errorf("plain fallback should not contain border glyph %q\n%s", glyph, out)
		}
	}
	// Each panel's name appears in a "[ name ]" header line.
	for _, name := range []string{"now", "health", "progress", "if you close now", "recent actions"} {
		if !strings.Contains(out, "[ "+name+" ]") {
			t.Errorf("plain fallback missing %q header:\n%s", "[ "+name+" ]", out)
		}
	}
	// Width contract still holds.
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if got := lipgloss.Width(line); got > w {
			t.Errorf("plain fallback line %d exceeds width %d (got %d):\n%q",
				i, w, got, line)
		}
	}
}
