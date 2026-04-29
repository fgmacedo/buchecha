package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// theme bundles the lipgloss styles the panels share. Style construction
// reads the lipgloss default renderer's color profile lazily, so calling
// DisableColor before any panel renders produces an Ascii (no-color)
// output without changing any panel code. Panels reference the styles
// by name (theme.title, theme.ok, ...).
//
// Color choice rationale: ANSI-16 indexed colors keep the output legible
// across nearly every terminal palette (light or dark, default or
// custom). Bright variants are reserved for transient alerts; the
// stable status glyphs use the muted base colors.
var theme = struct {
	title    lipgloss.Style // bold panel heading
	ok       lipgloss.Style // healthy state, ✓ glyphs, "alive" dot
	warn     lipgloss.Style // caution: paused tag, ⚠ glyphs, journal-missing
	err      lipgloss.Style // failure: error count, rate-limit, loop-suspect
	subtle   lipgloss.Style // secondary text (timestamps, paths, hints)
	bar      lipgloss.Style // filled cells in the progress bar
	barEmpty lipgloss.Style // empty cells in the progress bar
	keyHint  lipgloss.Style // help-overlay keyboard glyph (e.g. "[q]")
}{
	title:    lipgloss.NewStyle().Bold(true),
	ok:       lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
	warn:     lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
	err:      lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
	subtle:   lipgloss.NewStyle().Faint(true),
	bar:      lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
	barEmpty: lipgloss.NewStyle().Faint(true),
	keyHint:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")),
}

// DisableColor forces the lipgloss default renderer into the Ascii
// profile so every theme.* style renders as plain text. Wired to
// `--output tui --no-color` in cmd/run; safe to call before
// tea.NewProgram or after (subsequent renders pick up the new profile).
//
// Also affects any non-TUI render path that happens to use lipgloss
// styles. The text and json backends do not use lipgloss today, so the
// flag is effectively a TUI-only switch in practice.
func DisableColor() {
	lipgloss.SetColorProfile(termenv.Ascii)
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
