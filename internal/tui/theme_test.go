package tui

import (
	"bytes"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// TestNoColorProfile_StripsANSI exercises the v2 colour-profile contract.
// In v2 lipgloss styles always emit ANSI escapes; the program's renderer
// (configured via tea.WithColorProfile) down-converts them at write time.
// Here we exercise the same conversion path via colorprofile.Writer with
// the NoTTY profile, which is exactly what cli/run wires up when the
// user passes --no-color: the rendered styled string flows through the
// writer and reaches the terminal as plain text.
func TestNoColorProfile_StripsANSI(t *testing.T) {
	colored := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("err")
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("setup: lipgloss.Render did not emit ANSI: %q", colored)
	}

	var buf bytes.Buffer
	w := &colorprofile.Writer{Forward: &buf, Profile: colorprofile.NoTTY}
	if _, err := w.WriteString(colored); err != nil {
		t.Fatalf("colorprofile writer: %v", err)
	}
	plain := buf.String()
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("NoTTY profile did not strip ANSI escapes: %q", plain)
	}
	if plain != "err" {
		t.Errorf("plain text = %q, want %q", plain, "err")
	}
}

func TestPluralize(t *testing.T) {
	cases := []struct {
		n        int
		singular string
		plural   string
		want     string
	}{
		{0, "commit", "commits", "0 commits"},
		{1, "commit", "commits", "1 commit"},
		{2, "commit", "commits", "2 commits"},
	}
	for _, c := range cases {
		if got := pluralize(c.n, c.singular, c.plural); got != c.want {
			t.Errorf("pluralize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
