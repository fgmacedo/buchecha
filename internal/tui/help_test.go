package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
)

// TestRenderHelpOverlay_ListsAllKeybindings exercises the bubbles/v2/help
// FullHelp path: the overlay must surface every binding from keyMap so a
// new binding lights up in the modal without an extra edit.
func TestRenderHelpOverlay_ListsAllKeybindings(t *testing.T) {
	keys := defaultKeyMap()
	out := renderHelpOverlay(help.New(), keys)
	if !strings.Contains(out, "[ help ]") {
		t.Errorf("renderHelpOverlay missing title:\n%s", out)
	}
	for _, b := range []struct{ label, desc string }{
		{"q / Ctrl+C", "cancel the loop and quit"},
		{"space", "pause / resume between iterations"},
		{"w", "open the web dashboard in the browser"},
		{"?", "toggle this help overlay"},
	} {
		if !strings.Contains(out, b.label) {
			t.Errorf("renderHelpOverlay missing key %q:\n%s", b.label, out)
		}
		if !strings.Contains(out, b.desc) {
			t.Errorf("renderHelpOverlay missing description %q:\n%s", b.desc, out)
		}
	}
}

// TestRenderHelpOverlay_HidesDisabledWebuiBinding asserts the [w]
// binding stays out of the help overlay when the host did not wire a
// dashboard URL / launcher. Disabling the binding (key.SetEnabled
// false) is what New does in that case; the help renderer must honor
// it so the overlay does not advertise a no-op shortcut.
func TestRenderHelpOverlay_HidesDisabledWebuiBinding(t *testing.T) {
	keys := defaultKeyMap()
	keys.Webui.SetEnabled(false)
	out := renderHelpOverlay(help.New(), keys)
	if strings.Contains(out, "open the web dashboard in the browser") {
		t.Errorf("renderHelpOverlay listed disabled [w] binding:\n%s", out)
	}
}

// TestKeyMap_FullHelpListsEveryBinding asserts the key.Binding source of
// truth surfaces every binding the model handles, so the help renderer
// stays in sync with the handler automatically.
func TestKeyMap_FullHelpListsEveryBinding(t *testing.T) {
	keys := defaultKeyMap()
	groups := keys.FullHelp()
	flat := []string{}
	for _, g := range groups {
		for _, b := range g {
			flat = append(flat, b.Help().Key+" "+b.Help().Desc)
		}
	}
	for _, want := range []string{
		"q / Ctrl+C cancel the loop and quit",
		"space pause / resume between iterations",
		"? toggle this help overlay",
	} {
		found := false
		for _, got := range flat {
			if strings.Contains(got, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("FullHelp missing binding %q\nflat=%v", want, flat)
		}
	}
}

func TestUpdate_QuestionTogglesHelp(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, cmd := m.Update(keyPress("?"))
	if cmd != nil {
		t.Errorf("? must not return a cmd; got %v", cmd)
	}
	if !got.(Model).helpVisible {
		t.Errorf("helpVisible = false after first ?, want true")
	}
	got2, _ := got.(Model).Update(keyPress("?"))
	if got2.(Model).helpVisible {
		t.Errorf("helpVisible = true after second ?, want false")
	}
}

func TestView_HelpOverlayReplacesPanels(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, _ := m.Update(keyPress("?"))
	out := got.(Model).View().Content
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
	out := mm.(Model).View().Content
	if strings.Contains(out, "[ help ]") {
		t.Errorf("View showed help overlay by default:\n%s", out)
	}
}
