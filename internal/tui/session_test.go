package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/spec"
)

// TestUpdate_LoopFinishedLatchesSessionMode covers P2.11.2 / P2.11.3:
// LoopFinished with a non-fatal reason latches sessionMode. The two
// fatal-shaped reasons ("user cancelled", "fatal") leave sessionMode
// off so the subsequent channel close fires tea.Quit and the program
// exits per the behaviour matrix in the spec.
func TestUpdate_LoopFinishedLatchesSessionMode(t *testing.T) {
	cases := []struct {
		reason   string
		wantMenu bool
	}{
		{"done", true},
		{"review", true},
		{"blocked", true},
		{"max_iterations", true},
		{"user cancelled", false},
		{"fatal", false},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			m, _, _, _ := newTestModel(t)
			// Latch the most recent iter result so the badge shows ok.
			m.lastIterResult = spec.ResultOK
			got, _ := m.Update(eventMsg{ev: loop.LoopFinished{Reason: tc.reason}})
			mm := got.(Model)
			if mm.sessionMode != tc.wantMenu {
				t.Errorf("sessionMode = %v, want %v (reason=%q)",
					mm.sessionMode, tc.wantMenu, tc.reason)
			}
		})
	}
}

// TestUpdate_ChannelCloseWhileSessionDoesNotQuit covers the second leg of
// P2.11.2: after sessionMode is latched, the events channel close (which
// follows LoopFinished in normal loop teardown) must not schedule
// tea.Quit; the program stays alive for the menu.
func TestUpdate_ChannelCloseWhileSessionDoesNotQuit(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	m.sessionMode = true
	got, cmd := m.Update(eventMsg{closed: true})
	if cmd != nil {
		t.Errorf("session mode must swallow channel close; got cmd %v", cmd)
	}
	mm := got.(Model)
	if !mm.finished {
		t.Errorf("finished should be set on channel close; got false")
	}
	if !mm.sessionMode {
		t.Errorf("sessionMode dropped after channel close; want sticky")
	}
}

// TestView_SessionMenuRenders exercises P2.11.3 + P2.11.7: in session
// mode the dashboard renders with the header's session badge and the
// menu line is appended below the footer.
func TestView_SessionMenuRenders(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	mm0, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mm0.(Model)
	m.lastIterResult = spec.ResultReview
	got, _ := m.Update(eventMsg{ev: loop.LoopFinished{Reason: "review"}})
	out := got.(Model).View().Content

	for _, want := range []string{
		"idle (review)",
		"r resume",
		"e edit",
		"q exit",
		"[ session: review ]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("session view missing %q\n%s", want, out)
		}
	}
	// Alive dot markers should not show next to the session badge.
	if strings.Contains(out, "  ●") {
		t.Errorf("alive dot still visible in session header; want session badge:\n%s", out)
	}
}

// TestUpdate_ResumeKeyEmitsRestartLoopMsg covers P2.11.5: pressing [r]
// in session mode produces a restartLoopMsg cmd. The msg is consumed in
// a separate Update call which dispatches the host's NewEvents factory.
func TestUpdate_ResumeKeyEmitsRestartLoopMsg(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	called := 0
	freshCh := make(chan loop.Event, 1)
	m.newEvents = func() <-chan loop.Event {
		called++
		return freshCh
	}
	m.sessionMode = true
	m.sessionReason = "review"

	_, cmd := m.Update(keyPress("r"))
	if cmd == nil {
		t.Fatalf("[r] in session must return a cmd")
	}
	msg := cmd()
	if _, ok := msg.(restartLoopMsg); !ok {
		t.Fatalf("[r] cmd produced %T, want restartLoopMsg", msg)
	}

	// Feed the restartLoopMsg back into Update; that path invokes the
	// factory and produces a rebindEventsMsg.
	_, cmd2 := m.Update(restartLoopMsg{})
	if cmd2 == nil {
		t.Fatalf("restartLoopMsg must return a cmd")
	}
	msg2 := cmd2()
	rb, ok := msg2.(rebindEventsMsg)
	if !ok {
		t.Fatalf("restartLoopMsg cmd produced %T, want rebindEventsMsg", msg2)
	}
	if called != 1 {
		t.Errorf("newEvents called %d times, want 1", called)
	}
	if rb.events != (<-chan loop.Event)(freshCh) {
		t.Errorf("rebindEventsMsg did not carry the freshly built channel")
	}
}

// TestUpdate_RebindEventsMsgClearsSession asserts the rebind handler
// drops sessionMode, restarts the bridge pump on the new channel, and
// preserves the run-local baseline SHA (the run, not the iteration, is
// the baseline).
func TestUpdate_RebindEventsMsgClearsSession(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	m.sessionMode = true
	m.runBaselineSHA = "abc123"
	m.header.iter = 7
	m.lastIterResult = spec.ResultReview

	freshCh := make(chan loop.Event, 1)
	got, cmd := m.Update(rebindEventsMsg{events: freshCh})
	mm := got.(Model)
	if mm.sessionMode {
		t.Errorf("sessionMode = true after rebind, want false")
	}
	if mm.finished {
		t.Errorf("finished still set after rebind")
	}
	if mm.runBaselineSHA != "abc123" {
		t.Errorf("runBaselineSHA dropped on rebind; want abc123, got %q", mm.runBaselineSHA)
	}
	if mm.header.iter != 0 {
		t.Errorf("header.iter = %d, want 0 (reset per session)", mm.header.iter)
	}
	if mm.lastIterResult != spec.ResultUnknown {
		t.Errorf("lastIterResult kept across resume; want reset")
	}
	if cmd == nil {
		t.Fatalf("rebind must return a readEventCmd to restart the pump")
	}

	// The cmd is the new bridge: pumping an event onto freshCh must
	// surface as an eventMsg.
	want := loop.IterationStarted{Index: 1, MaxIter: 5}
	freshCh <- want
	em, ok := cmd().(eventMsg)
	if !ok {
		t.Fatalf("cmd produced wrong msg type")
	}
	if em.closed {
		t.Errorf("eventMsg.closed = true on fresh channel pump")
	}
	if got := em.ev.(loop.IterationStarted); got.Index != 1 {
		t.Errorf("pumped event = %+v, want IterationStarted{Index:1}", got)
	}
}

// TestUpdate_QuitKeyInSessionMode covers P2.11.8: [q] / Ctrl+C in
// session mode return tea.Quit immediately. No cancel call is required
// because the loop has already terminated.
func TestUpdate_QuitKeyInSessionMode(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		t.Run(k, func(t *testing.T) {
			m, _, _, _ := newTestModel(t)
			m.sessionMode = true
			_, cmd := m.Update(keyPress(k))
			if cmd == nil {
				t.Fatalf("session [%s] must return a cmd", k)
			}
			if msg := cmd(); msg != tea.Quit() {
				t.Errorf("session [%s] cmd = %v, want tea.Quit()", k, msg)
			}
		})
	}
}

// TestUpdate_SessionResumeEndToEnd is the integration test described in
// P2.11.10's last bullet: a fake loop terminates the first iteration
// with review; the user presses [r]; a second IterationStarted is
// observable on the new bridge pump.
//
// The test drives the pump by hand: feed events directly into Update
// (skipping the bridge cmd), so we exercise the state-machine paths
// without blocking on the readEventCmd read which is unsafe in unit
// tests (tea.Batch with one cmd unwraps to that cmd directly).
func TestUpdate_SessionResumeEndToEnd(t *testing.T) {
	m, _, _, _ := newTestModel(t)

	freshCh := make(chan loop.Event, 8)
	resumed := false
	m.newEvents = func() <-chan loop.Event {
		resumed = true
		// Replay the same scripted run on the new channel.
		freshCh <- loop.IterationStarted{Index: 1, MaxIter: 1}
		freshCh <- loop.LoopFinished{Reason: "review"}
		close(freshCh)
		return freshCh
	}

	// First run: drive the model through IterationStarted, LoopFinished,
	// channel close.
	mm := tea.Model(m)
	mm, _ = mm.Update(eventMsg{ev: loop.IterationStarted{Index: 1, MaxIter: 1}})
	mm, _ = mm.Update(eventMsg{ev: loop.LoopFinished{Reason: "review"}})
	mm, cmdAfterClose := mm.Update(eventMsg{closed: true})
	if !mm.(Model).sessionMode {
		t.Fatalf("first run did not reach sessionMode")
	}
	if cmdAfterClose != nil {
		t.Fatalf("session mode must swallow channel close; cmd != nil")
	}

	// Press [r]: produce restartLoopMsg.
	_, cmd := mm.Update(keyPress("r"))
	restart := cmd()
	if _, ok := restart.(restartLoopMsg); !ok {
		t.Fatalf("[r] produced %T, want restartLoopMsg", restart)
	}

	// Restart: factory fires, rebindEventsMsg follows.
	mm, cmd = mm.Update(restart)
	if !resumed {
		t.Fatalf("newEvents factory not invoked")
	}
	rebind := cmd()
	rb, ok := rebind.(rebindEventsMsg)
	if !ok {
		t.Fatalf("restartLoopMsg → %T, want rebindEventsMsg", rebind)
	}

	// Rebind: sessionMode flips off; the cmd is a fresh readEventCmd
	// that pulls from freshCh. The fake factory has already pushed a
	// full scripted run onto freshCh, so calling the cmd returns the
	// first event without blocking.
	mm, cmd = mm.Update(rb)
	if mm.(Model).sessionMode {
		t.Fatalf("rebind did not clear sessionMode")
	}
	em, ok := cmd().(eventMsg)
	if !ok {
		t.Fatalf("post-rebind pump produced %T, want eventMsg", cmd())
	}
	if em.closed {
		t.Fatalf("post-rebind pump returned closed; want IterationStarted")
	}
	if got := em.ev.(loop.IterationStarted); got.Index != 1 {
		t.Errorf("second run did not start at index 1; got %+v", got)
	}
}

// TestSessionStatus_LabelsResultsAndReasons covers the helper that maps
// the loop's terminal Reason / spec.Result onto the badge label.
func TestSessionStatus_LabelsResultsAndReasons(t *testing.T) {
	cases := []struct {
		reason string
		res    spec.Result
		want   string
	}{
		{"review", spec.ResultReview, "review"},
		{"done", spec.ResultDone, "done"},
		{"blocked", spec.ResultBlocked, "blocked"},
		{"max_iterations", spec.ResultUnknown, "max iterations"},
		{"head_stuck", spec.ResultUnknown, "head stuck"},
		{"done_with_leftovers", spec.ResultUnknown, "done with leftovers"},
		{"review", spec.ResultUnknown, "review"},
		{"", spec.ResultUnknown, "idle"},
	}
	for _, tc := range cases {
		got := sessionStatus(tc.reason, tc.res)
		if got != tc.want {
			t.Errorf("sessionStatus(%q,%v) = %q, want %q",
				tc.reason, tc.res, got, tc.want)
		}
	}
}

// TestSessionKeyMap_FullHelpListsResumeEditQuit guards the binding set:
// resume/edit/quit must surface in the FullHelp output so the `?`
// overlay in session mode advertises every binding the model handles.
func TestSessionKeyMap_FullHelpListsResumeEditQuit(t *testing.T) {
	keys := defaultSessionKeyMap()
	groups := keys.FullHelp()
	flat := []string{}
	for _, g := range groups {
		for _, b := range g {
			flat = append(flat, b.Help().Key+" "+b.Help().Desc)
		}
	}
	for _, want := range []string{
		"r resume the loop",
		"e edit spec in $EDITOR",
		"q / Ctrl+C exit",
	} {
		found := false
		for _, got := range flat {
			if strings.Contains(got, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("session FullHelp missing binding %q\nflat=%v", want, flat)
		}
	}
}

// TestUpdate_QuestionTogglesHelpInSessionMode asserts the `?` overlay
// still works once sessionMode is latched. The handler in session mode
// must keep this binding active so the user can review keybindings
// without leaving the menu.
func TestUpdate_QuestionTogglesHelpInSessionMode(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	m.sessionMode = true
	got, _ := m.Update(keyPress("?"))
	if !got.(Model).helpVisible {
		t.Errorf("? in session did not toggle helpVisible")
	}
}

// TestUpdate_EditKeyWithoutProgramSetsHint asserts the [e] handler
// degrades gracefully when no tea.Program reference is wired (test-only
// path; runtime always wires one via SetProgram). The session menu
// surfaces the hint instead of attempting to suspend the terminal.
func TestUpdate_EditKeyWithoutProgramSetsHint(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	m.sessionMode = true
	got, cmd := m.Update(keyPress("e"))
	if cmd != nil {
		t.Errorf("[e] without program must not return a cmd")
	}
	mm := got.(Model)
	if !strings.Contains(mm.sessionExitMsg, "tea.Program reference missing") {
		t.Errorf("sessionExitMsg = %q, want hint about missing program", mm.sessionExitMsg)
	}
}
