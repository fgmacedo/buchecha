package tui

import (
	"strings"

	"charm.land/bubbles/v2/help"
)

// renderHelpOverlay produces the modal-style help screen toggled by `?`.
// The keymap is the single source of truth: the help.Model renders both
// the inline footer (ShortHelp) and this full overlay (FullHelp) from
// the same key.Binding set, so adding a binding to a keyMap lights it
// up in both places without further edits. Accepts any help.KeyMap so
// the same renderer serves both the dashboard's keyMap and the session
// menu's sessionKeyMap (P2.11).
func renderHelpOverlay(h help.Model, k help.KeyMap) string {
	var b strings.Builder
	b.WriteString(theme.title.Render("[ help ]"))
	b.WriteByte('\n')
	b.WriteString(theme.subtle.Render("  bcc dashboard keybindings"))
	b.WriteString("\n\n")
	full := h.FullHelpView(k.FullHelp())
	for _, line := range strings.Split(full, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(theme.subtle.Render("  press ? to return to the dashboard"))
	b.WriteByte('\n')
	return b.String()
}
