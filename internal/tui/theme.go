package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// theme bundles the lipgloss styles the panels use. Phase 2.5 keeps
// the surface minimal: a panel-title style, an "ok" / "warn" / "err"
// trio for status glyphs, and a subdued style for secondary text. The
// full palette and `--no-color` toggle land in P2.7.
var theme = struct {
	title    lipgloss.Style
	ok       lipgloss.Style
	warn     lipgloss.Style
	err      lipgloss.Style
	subtle   lipgloss.Style
	bar      lipgloss.Style
	barEmpty lipgloss.Style
}{
	title:    lipgloss.NewStyle().Bold(true),
	ok:       lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
	warn:     lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
	err:      lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
	subtle:   lipgloss.NewStyle().Faint(true),
	bar:      lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
	barEmpty: lipgloss.NewStyle().Faint(true),
}

// panelTitle renders the bracketed heading every panel prints on its
// first line (e.g. "[ now ]"). Subsequent lines are the body. The
// substring matched by tui_test.TestView_FivePanelTitlesPresent is
// the inner name.
func panelTitle(name string) string {
	return theme.title.Render("[ " + name + " ]")
}

// formatDuration renders a short human-readable duration: 12s,
// 1m32s, 14m, 1h23m. Negative or zero d is "0s".
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) - m*60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

// trimEmpty returns "-" when s is empty so headers never collapse to
// "iter 3/5  " with a stray gap.
func trimEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// pluralize renders "<n> <singular>" when n == 1 and "<n> <plural>"
// otherwise. Used for short panel labels like "1 commit" / "12 commits".
func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
