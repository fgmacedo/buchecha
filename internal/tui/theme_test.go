package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestDisableColor asserts that DisableColor flips the lipgloss default
// renderer into the Ascii profile so subsequent renders contain no
// ANSI escape codes. The test restores the prior profile afterwards so
// it does not leak into the rest of the package.
func TestDisableColor_StripsANSI(t *testing.T) {
	prev := lipgloss.DefaultRenderer().ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	lipgloss.SetColorProfile(termenv.TrueColor)
	colored := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("err")
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("setup: colored output missing ANSI escape: %q", colored)
	}

	DisableColor()
	plain := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("err")
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("DisableColor: output still contains ANSI escapes: %q", plain)
	}
	if plain != "err" {
		t.Errorf("DisableColor: rendered text = %q, want %q", plain, "err")
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
