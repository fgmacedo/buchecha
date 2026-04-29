package tui

import (
	"fmt"
	"strings"
)

// helpEntry pairs a keybinding with its description.
type helpEntry struct {
	key, desc string
}

// helpEntries lists every keybinding the TUI honours. Source of truth
// for the help overlay and (after the footer) the runtime hint line.
// Adding a new key means adding it here so the help screen stays in
// sync; tests assert that the keys appear in the rendered overlay.
var helpEntries = []helpEntry{
	{"q / Ctrl+C", "cancel the loop and quit"},
	{"space", "pause / resume between iterations"},
	{"?", "toggle this help overlay"},
}

// renderHelp builds the modal-style help screen. It is rendered in
// place of the panels when Model.helpVisible is true; pressing `?`
// again returns to the dashboard.
func renderHelp() string {
	var b strings.Builder
	b.WriteString(theme.title.Render("[ help ]"))
	b.WriteByte('\n')
	b.WriteString(theme.subtle.Render("  bcc dashboard keybindings"))
	b.WriteString("\n\n")

	width := 0
	for _, e := range helpEntries {
		if len(e.key) > width {
			width = len(e.key)
		}
	}
	for _, e := range helpEntries {
		fmt.Fprintf(&b, "  %s  %s\n",
			theme.keyHint.Render(padRight(e.key, width)),
			e.desc,
		)
	}
	b.WriteByte('\n')
	b.WriteString(theme.subtle.Render("  press ? to return to the dashboard"))
	b.WriteByte('\n')
	return b.String()
}

// padRight returns s padded with spaces on the right to width runes.
// Helper for help-overlay alignment; lipgloss could do this but a
// runtime no-op for the common case is simpler.
func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(r))
}
