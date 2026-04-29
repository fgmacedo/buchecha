package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/spec"
)

// newTestModel builds a Model with a buffered events channel returned
// alongside so tests can push events into it.
func newTestModel(t *testing.T) (Model, chan loop.Event, *Gate, *bool) {
	t.Helper()
	events := make(chan loop.Event, 16)
	gate := NewGate()
	cancelled := false
	cancel := func() { cancelled = true }
	m := New(Options{
		Events:   events,
		Cancel:   cancel,
		Gate:     gate,
		SpecPath: "spec.md",
		Branch:   "feat/x",
		MaxIter:  5,
	})
	return m, events, gate, &cancelled
}

func TestUpdate_WindowSizeStored(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := got.(Model)
	if mm.width != 120 || mm.height != 40 {
		t.Errorf("width/height = (%d,%d), want (120,40)", mm.width, mm.height)
	}
}

func TestUpdate_QuitKeyCancelsAndQuits(t *testing.T) {
	m, _, _, cancelled := newTestModel(t)
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatalf("expected tea.Quit cmd, got nil")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("cmd msg = %v, want tea.Quit()", msg)
	}
	if !*cancelled {
		t.Errorf("loop ctx cancel not invoked on q")
	}
	mm := got.(Model)
	if !mm.cancelled {
		t.Errorf("Model.cancelled = false, want true")
	}
}

func TestUpdate_CtrlCQuitsAndCancels(t *testing.T) {
	m, _, _, cancelled := newTestModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatalf("expected tea.Quit cmd, got nil")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("cmd msg = %v, want tea.Quit()", msg)
	}
	if !*cancelled {
		t.Errorf("loop ctx cancel not invoked on Ctrl+C")
	}
}

func TestUpdate_SpaceTogglesPause(t *testing.T) {
	m, _, gate, cancelled := newTestModel(t)
	if gate.Paused() {
		t.Fatalf("gate should start unpaused")
	}
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd != nil {
		t.Errorf("space must not return a cmd; got %v", cmd)
	}
	if *cancelled {
		t.Errorf("space must not cancel the loop")
	}
	if !gate.Paused() {
		t.Errorf("gate not paused after first space")
	}
	mm := got.(Model)
	if !mm.paused {
		t.Errorf("Model.paused = false after space")
	}
	got2, _ := mm.Update(tea.KeyMsg{Type: tea.KeySpace})
	if gate.Paused() {
		t.Errorf("gate not resumed after second space")
	}
	mm2 := got2.(Model)
	if mm2.paused {
		t.Errorf("Model.paused = true after second space")
	}
}

func TestUpdate_IterationStartedTracksIndex(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, cmd := m.Update(eventMsg{ev: loop.IterationStarted{Index: 3, MaxIter: 5}})
	if cmd == nil {
		t.Fatalf("must schedule next read after handling event")
	}
	mm := got.(Model)
	if mm.header.iter != 3 {
		t.Errorf("header.iter = %d, want 3", mm.header.iter)
	}
}

func TestUpdate_IterationFinishedReleasesGate(t *testing.T) {
	m, _, gate, _ := newTestModel(t)
	if recv(gate) {
		t.Fatalf("gate should be empty before any IterationFinished")
	}
	_, cmd := m.Update(eventMsg{ev: loop.IterationFinished{Index: 1}})
	if cmd == nil {
		t.Fatalf("must schedule next read after handling event")
	}
	if !recv(gate) {
		t.Errorf("gate did not get a token after IterationFinished")
	}
}

func TestUpdate_IterationFinishedWhilePausedHoldsGate(t *testing.T) {
	m, _, gate, _ := newTestModel(t)
	gate.SetPaused(true)
	_, _ = m.Update(eventMsg{ev: loop.IterationFinished{Index: 1}})
	if recv(gate) {
		t.Errorf("gate must not release while paused")
	}
}

func TestUpdate_ChannelClosedQuits(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, cmd := m.Update(eventMsg{closed: true})
	if cmd == nil {
		t.Fatalf("expected tea.Quit cmd on channel close")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("cmd msg = %v, want tea.Quit()", msg)
	}
	mm := got.(Model)
	if !mm.finished {
		t.Errorf("Model.finished = false after channel close")
	}
}

// findReadEventCmd walks a possibly-batched tea.Cmd and synchronously
// invokes inner cmds until one returns an eventMsg. Init now returns a
// Batch (event read + spinner tick + git probe) so tests can no longer
// assume the top-level cmd directly produces an eventMsg.
func findReadEventCmd(t *testing.T, cmd tea.Cmd) eventMsg {
	t.Helper()
	if cmd == nil {
		t.Fatalf("nil cmd")
	}
	msg := cmd()
	switch m := msg.(type) {
	case eventMsg:
		return m
	case tea.BatchMsg:
		for _, c := range m {
			if c == nil {
				continue
			}
			if em, ok := c().(eventMsg); ok {
				return em
			}
		}
	}
	t.Fatalf("no eventMsg produced by cmd (got %T)", msg)
	return eventMsg{}
}

func TestInit_StartsBridgePump(t *testing.T) {
	m, events, _, _ := newTestModel(t)
	cmd := m.Init()
	if cmd == nil {
		t.Fatalf("Init returned nil cmd; bridge not wired")
	}
	// Pump one event through; the bridge cmd should pick it up.
	want := loop.IterationStarted{Index: 1, MaxIter: 5}
	events <- want
	msg := findReadEventCmd(t, cmd)
	if msg.closed {
		t.Fatalf("eventMsg.closed = true, want false")
	}
	if got := msg.ev.(loop.IterationStarted); got.Index != 1 {
		t.Errorf("read event index = %d, want 1", got.Index)
	}
}

func TestInit_ChannelCloseSurfacesAsClosed(t *testing.T) {
	m, events, _, _ := newTestModel(t)
	close(events)
	cmd := m.Init()
	msg := findReadEventCmd(t, cmd)
	if !msg.closed {
		t.Errorf("eventMsg.closed = false after channel close, want true")
	}
}

func TestView_FivePanelTitlesPresent(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	out := mm.(Model).View()
	for _, name := range []string{"now", "health", "progress", "if you close now", "recent actions"} {
		if !strings.Contains(out, name) {
			t.Errorf("View missing panel title %q\n%s", name, out)
		}
	}
	for _, ctl := range []string{"[q]uit", "[space]pause", "[?]help"} {
		if !strings.Contains(out, ctl) {
			t.Errorf("View missing footer hint %q\n%s", ctl, out)
		}
	}
}

func TestView_PausedTagAppearsInHeader(t *testing.T) {
	m, _, gate, _ := newTestModel(t)
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !gate.Paused() {
		t.Fatalf("setup: gate not paused")
	}
	out := mm.(Model).View()
	if !strings.Contains(out, "[paused]") {
		t.Errorf("View missing [paused] tag while paused\n%s", out)
	}
}

func TestUpdate_AgentEventRoutesToPanels(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	at := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	got, _ := m.Update(eventMsg{ev: loop.AgentEventReceived{Event: loop.AgentEvent{
		Kind: loop.KindToolUse, At: at,
		Tool: &loop.ToolCallInfo{ID: "t1", Name: "Edit", Args: map[string]any{"file_path": "x.go"}},
	}}})
	mm := got.(Model)
	if mm.now.currentTool == nil || mm.now.currentTool.Name != "Edit" {
		t.Errorf("tool_use not routed to nowPanel: %+v", mm.now.currentTool)
	}
	if mm.health.totalTools != 1 {
		t.Errorf("tool_use not counted in healthPanel: got %d", mm.health.totalTools)
	}
	if len(mm.actions.entries) != 1 {
		t.Errorf("tool_use not appended to actionsPanel: got %d", len(mm.actions.entries))
	}
	if mm.header.lastEvent != at {
		t.Errorf("header heartbeat not updated: got %v, want %v", mm.header.lastEvent, at)
	}
}

func TestUpdate_SpinnerTickAdvancesAndReschedules(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, cmd := m.Update(spinnerTickMsg{})
	if cmd == nil {
		t.Fatalf("spinner tick must reschedule itself")
	}
	mm := got.(Model)
	if mm.now.spinnerFrame != 1 {
		t.Errorf("spinnerFrame = %d, want 1", mm.now.spinnerFrame)
	}
}

func TestUpdate_GitProbeMsgFeedsRiskPanel(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, _ := m.Update(gitProbeMsg{count: 4})
	mm := got.(Model)
	if !mm.risk.dirtyKnown || mm.risk.dirtyFileCount != 4 {
		t.Errorf("gitProbeMsg not folded into risk panel: %+v", mm.risk)
	}
}

func TestUpdate_SpecParsedMsgFeedsProgressAndRisk(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	plan := samplePlan()
	got, _ := m.Update(specParsedMsg{
		ok:          true,
		plan:        plan,
		latest:      spec.LatestResult{Raw: "ok", Result: spec.ResultOK},
		latestKnown: true,
	})
	mm := got.(Model)
	if mm.progress.currentPhaseIdx == -1 {
		t.Errorf("progress panel did not record current phase")
	}
	if mm.risk.totalItems != 6 {
		t.Errorf("risk panel did not record items: got %d", mm.risk.totalItems)
	}
}

// TestUpdate_ScheduledReadCmdReturnsEventMsg drives the loop end-to-end:
// Update receives an event, schedules the next read, and that read
// surfaces the pumped event correctly.
func TestUpdate_ScheduledReadCmdReturnsEventMsg(t *testing.T) {
	m, events, _, _ := newTestModel(t)
	_, cmd := m.Update(eventMsg{ev: loop.IterationStarted{Index: 1, MaxIter: 1}})
	if cmd == nil {
		t.Fatalf("no follow-up cmd")
	}

	want := loop.AgentEventReceived{Event: loop.AgentEvent{Kind: loop.KindToolUse}}
	events <- want
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		ev := msg.(eventMsg)
		if ev.closed {
			t.Errorf("eventMsg.closed = true, want false")
		}
	case <-time.After(time.Second):
		t.Fatalf("cmd did not return within 1s")
	}
}
