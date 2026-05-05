package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// keyPress builds a tea.KeyPressMsg for the given key string. v2 replaces
// the v1 tea.KeyMsg{Type, Runes} struct with a Key/KeyPressMsg shape; this
// helper keeps test bodies readable.
func keyPress(k string) tea.KeyPressMsg {
	switch k {
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "space", " ":
		return tea.KeyPressMsg{Code: ' ', Text: " "}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	default:
		runes := []rune(k)
		if len(runes) == 1 {
			return tea.KeyPressMsg{Code: runes[0], Text: k}
		}
		return tea.KeyPressMsg{Text: k}
	}
}

// fakeGitProbe counts calls and returns canned values.
type fakeGitProbe struct {
	dirty    atomic.Int32
	commits  atomic.Int32
	dirtyN   int
	commitsN int
}

func (f *fakeGitProbe) DirtyFileCount(_ context.Context) (int, error) {
	f.dirty.Add(1)
	return f.dirtyN, nil
}

func (f *fakeGitProbe) CommitsSince(_ context.Context, _ string) (int, error) {
	f.commits.Add(1)
	return f.commitsN, nil
}

// newTestModel builds a Model backed by a test services instance,
// returning the raw events channel alongside so tests can push events into it.
func newTestModel(t *testing.T) (Model, chan loop.Event, *Gate, *bool) {
	t.Helper()
	ts := newTestSvc(t)
	gate := NewGate()
	cancelled := false
	cancel := func() { cancelled = true }
	m := New(Options{
		Services:  ts.Svc,
		SessionID: ts.SessionID,
		Cancel:    cancel,
		Gate:      gate,
		SpecPath:  "spec.md",
		Branch:    "feat/x",
		MaxIter:   5,
	})
	return m, ts.Events, gate, &cancelled
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
	got, cmd := m.Update(keyPress("q"))
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
	_, cmd := m.Update(keyPress("ctrl+c"))
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

func TestUpdate_MouseKeyTogglesCaptureAndViewMouseMode(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("default MouseMode = %v, want MouseModeCellMotion", m.View().MouseMode)
	}
	got, cmd := m.Update(keyPress("m"))
	if cmd != nil {
		t.Errorf("m key must not return a cmd; got %v", cmd)
	}
	mm := got.(Model)
	if !mm.mouseCaptureOff {
		t.Fatal("mouseCaptureOff = false after first m press")
	}
	if mm.View().MouseMode != tea.MouseModeNone {
		t.Errorf("View MouseMode = %v after toggle off, want MouseModeNone", mm.View().MouseMode)
	}
	got2, _ := mm.Update(keyPress("m"))
	mm2 := got2.(Model)
	if mm2.mouseCaptureOff {
		t.Fatal("mouseCaptureOff = true after second m press; want toggled back")
	}
	if mm2.View().MouseMode != tea.MouseModeCellMotion {
		t.Errorf("View MouseMode = %v after toggle on, want MouseModeCellMotion", mm2.View().MouseMode)
	}
}

func TestUpdate_SpinnerTickStopsWhenIdle(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	// No tool active and no planning pending: tick must not re-arm.
	got, cmd := m.Update(spinner.TickMsg{})
	if cmd != nil {
		t.Errorf("idle spinner tick must not re-arm; got cmd %v", cmd)
	}
	_ = got

	// With planning pending the tick continues to arm so the
	// "planning..." placeholder keeps animating.
	m.planningPending = true
	_, cmd2 := m.Update(spinner.TickMsg{})
	if cmd2 == nil {
		t.Error("spinner tick must re-arm while planningPending")
	}
}

func TestUpdate_ToolUseArmsSpinner(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	// Idle tick: no cmd.
	if _, cmd := m.Update(spinner.TickMsg{}); cmd != nil {
		t.Fatalf("precondition: idle tick must not arm; got %v", cmd)
	}
	ev := loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindToolUse,
		Tool: &agentcontract.ToolCallInfo{ID: "t1", Name: "Bash"},
	}}
	got, cmd := m.Update(eventMsg{ev: ev})
	if cmd == nil {
		t.Fatal("KindToolUse must arm the spinner; got nil cmd")
	}
	mm := got.(Model)
	if mm.now.currentTool == nil {
		t.Error("currentTool not set after KindToolUse")
	}
}

func TestUpdate_SpaceTogglesPause(t *testing.T) {
	m, _, gate, cancelled := newTestModel(t)
	if gate.Paused() {
		t.Fatalf("gate should start unpaused")
	}
	got, cmd := m.Update(keyPress("space"))
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
	got2, _ := mm.Update(keyPress("space"))
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

// drainBatchChildren expands a tea.Cmd into its child cmds. Handles both
// tea.Batch (single batch wrapping multiple cmds) and tea.sequenceMsg
// shapes; if the cmd is a single non-batch cmd, its msg is irrelevant
// here so the caller treats the children list as empty.
func drainBatchChildren(t *testing.T, cmd tea.Cmd) []tea.Cmd {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		return batch
	}
	return nil
}

func TestUpdate_IterationStartedFiresImmediateGitProbe(t *testing.T) {
	probe := &fakeGitProbe{dirtyN: 1, commitsN: 0}
	ts := newTestSvc(t)
	gate := NewGate()
	m := New(Options{
		Services:  ts.Svc,
		SessionID: ts.SessionID,
		Cancel:    func() {},
		Gate:      gate,
		SpecPath:  "spec.md",
		Branch:    "feat/x",
		MaxIter:   5,
		GitProbe:  probe,
	})

	// Close the events channel so the subscriber channel also closes quickly.
	ts.Close()

	_, cmd := m.Update(eventMsg{ev: loop.IterationStarted{
		Index: 1, MaxIter: 5, BaselineSHA: "abc123",
	}})
	if cmd == nil {
		t.Fatalf("expected cmd from IterationStarted handler")
	}
	children := drainBatchChildren(t, cmd)

	// The batch must contain at least one cmd that produces a gitProbeMsg
	// without waiting for a tick. Run each child once with a short timeout
	// guard; we cannot synchronously drive tea.Tick, but the immediate cmd
	// returns instantly.
	sawProbeMsg := false
	for _, c := range children {
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func(cm tea.Cmd) { done <- cm() }(c)
		select {
		case msg := <-done:
			if _, ok := msg.(gitProbeMsg); ok {
				sawProbeMsg = true
			}
		case <-time.After(50 * time.Millisecond):
			// tea.Tick blocks for gitProbeInterval; ignore.
		}
	}
	if !sawProbeMsg {
		t.Errorf("expected an immediate gitProbeMsg in the batch; got none")
	}
	if probe.dirty.Load() < 1 {
		t.Errorf("DirtyFileCount not called; got %d", probe.dirty.Load())
	}
	if probe.commits.Load() < 1 {
		t.Errorf("CommitsSince not called for run baseline; got %d", probe.commits.Load())
	}
}

func TestUpdate_IterationStartedWithoutBaselineDoesNotProbe(t *testing.T) {
	probe := &fakeGitProbe{}
	ts := newTestSvc(t)
	gate := NewGate()
	m := New(Options{
		Services:  ts.Svc,
		SessionID: ts.SessionID,
		Cancel:    func() {},
		Gate:      gate,
		SpecPath:  "spec.md",
		Branch:    "feat/x",
		MaxIter:   5,
		GitProbe:  probe,
	})
	ts.Close()

	_, cmd := m.Update(eventMsg{ev: loop.IterationStarted{
		Index: 1, MaxIter: 5, BaselineSHA: "",
	}})
	children := drainBatchChildren(t, cmd)

	for _, c := range children {
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func(cm tea.Cmd) { done <- cm() }(c)
		select {
		case msg := <-done:
			if _, ok := msg.(gitProbeMsg); ok {
				t.Errorf("no probe should fire when BaselineSHA is empty; got %T", msg)
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
	if probe.dirty.Load() != 0 || probe.commits.Load() != 0 {
		t.Errorf("git probe must not be called without a baseline; dirty=%d commits=%d",
			probe.dirty.Load(), probe.commits.Load())
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
	out := mm.(Model).View().Content
	for _, name := range []string{"now", "health", "progress", "if you close now", "recent actions"} {
		if !strings.Contains(out, name) {
			t.Errorf("View missing panel title %q\n%s", name, out)
		}
	}
	// The footer is now rendered by bubbles/v2/help; the keymap exposes
	// the same bindings under their human labels. At narrow widths the
	// help.Model truncates the tail with an ellipsis, so we only assert
	// the first two bindings appear; the ? overlay test exercises the
	// full keymap path.
	for _, ctl := range []string{"q / Ctrl+C", "space"} {
		if !strings.Contains(out, ctl) {
			t.Errorf("View missing footer hint %q\n%s", ctl, out)
		}
	}
}

func TestView_PausedTagAppearsInHeader(t *testing.T) {
	m, _, gate, _ := newTestModel(t)
	mm, _ := m.Update(keyPress("space"))
	if !gate.Paused() {
		t.Fatalf("setup: gate not paused")
	}
	out := mm.(Model).View().Content
	if !strings.Contains(out, "[paused]") {
		t.Errorf("View missing [paused] tag while paused\n%s", out)
	}
}

func TestUpdate_AgentEventRoutesToPanels(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	at := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	got, _ := m.Update(eventMsg{ev: loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindToolUse, At: at,
		Tool: &agentcontract.ToolCallInfo{ID: "t1", Name: "Edit", Args: map[string]any{"file_path": "x.go"}},
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

// TestUpdate_SpinnerTickReschedulesWhenActive verifies the spinner
// keeps animating while there is something to show: an in-flight tool
// call is one of the conditions; the tick re-arms only then. Idle ticks
// are covered by TestUpdate_SpinnerTickStopsWhenIdle.
func TestUpdate_SpinnerTickReschedulesWhenActive(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	m.now.currentTool = &agentcontract.ToolCallInfo{ID: "t1", Name: "Bash"}
	tick := m.now.spinner.Tick().(spinner.TickMsg)
	_, cmd := m.Update(tick)
	if cmd == nil {
		t.Fatalf("spinner.TickMsg must reschedule itself while a tool is active")
	}
}

func TestUpdate_GitProbeMsgFeedsRiskPanel(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, _ := m.Update(gitProbeMsg{
		dirtyCount: 4, dirtyKnown: true,
		commitCount: 12, commitsKnown: true,
	})
	mm := got.(Model)
	if !mm.risk.dirtyKnown || mm.risk.dirtyFileCount != 4 {
		t.Errorf("gitProbeMsg dirty leg not folded into risk panel: %+v", mm.risk)
	}
	if !mm.risk.commitsKnown || mm.risk.runCommitCount != 12 {
		t.Errorf("gitProbeMsg commits leg not folded into risk panel: %+v", mm.risk)
	}
}

func TestUpdate_FirstIterationStartedCapturesBaselineSHA(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, _ := m.Update(eventMsg{ev: loop.IterationStarted{
		Index: 1, MaxIter: 5, BaselineSHA: "abc123",
	}})
	mm := got.(Model)
	if mm.runBaselineSHA != "abc123" {
		t.Errorf("runBaselineSHA = %q, want abc123", mm.runBaselineSHA)
	}
	got2, _ := mm.Update(eventMsg{ev: loop.IterationStarted{
		Index: 2, MaxIter: 5, BaselineSHA: "def456",
	}})
	if got2.(Model).runBaselineSHA != "abc123" {
		t.Errorf("runBaselineSHA changed on iter 2; want stable at abc123")
	}
}

func TestUpdate_IterationFinishedFeedsRisk(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	got, _ := m.Update(eventMsg{ev: loop.IterationFinished{
		Index: 1, Signal: agentcontract.SignalContinue, At: time.Now(),
	}})
	mm := got.(Model)
	if !mm.risk.signalKnown {
		t.Errorf("risk panel did not record signal")
	}
	if mm.risk.lastSignal != agentcontract.SignalContinue {
		t.Errorf("risk panel signal = %v, want SignalContinue", mm.risk.lastSignal)
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

	want := loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindToolUse}}
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
