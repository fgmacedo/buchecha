package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRenderHelp_ListsAllKeybindings(t *testing.T) {
	out := renderHelp()
	if !strings.Contains(out, "[ help ]") {
		t.Errorf("renderHelp missing title:\n%s", out)
	}
	for _, e := range helpEntries {
		if !strings.Contains(out, e.key) {
			t.Errorf("renderHelp missing key %q:\n%s", e.key, out)
		}
		if !strings.Contains(out, e.desc) {
			t.Errorf("renderHelp missing description %q:\n%s", e.desc, out)
		}
	}
}

func TestUpdate_QuestionTogglesHelp(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if cmd != nil {
		t.Errorf("? must not return a cmd; got %v", cmd)
	}
	if !got.(Model).helpVisible {
		t.Errorf("helpVisible = false after first ?, want true")
	}
	got2, _ := got.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if got2.(Model).helpVisible {
		t.Errorf("helpVisible = true after second ?, want false")
	}
}

func TestView_HelpOverlayReplacesPanels(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	out := got.(Model).View()
	if !strings.Contains(out, "[ help ]") {
		t.Errorf("View did not show help overlay:\n%s", out)
	}
	// While help is up, the dashboard panels must not render. The
	// title "[ now ]" is the canonical first-panel marker.
	if strings.Contains(out, "[ now ]") {
		t.Errorf("View showed dashboard panels while help is visible:\n%s", out)
	}
}

func TestView_HelpHiddenByDefault(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	out := mm.(Model).View()
	if strings.Contains(out, "[ help ]") {
		t.Errorf("View showed help overlay by default:\n%s", out)
	}
}
