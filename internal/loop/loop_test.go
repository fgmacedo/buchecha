package loop_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/config"
	"github.com/fgmacedo/buchecha/internal/executor/fake"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// fakeGit returns scripted SHAs and never errors.
type fakeGit struct {
	heads []string
	idx   int
}

func (f *fakeGit) HeadSHA(_ context.Context) (string, error) {
	if f.idx >= len(f.heads) {
		return "", fmt.Errorf("fakeGit: out of HeadSHA calls (idx=%d)", f.idx)
	}
	s := f.heads[f.idx]
	f.idx++
	return s, nil
}

func (f *fakeGit) CurrentBranch(_ context.Context) (string, error) { return "main", nil }
func (f *fakeGit) IsClean(_ context.Context) (bool, error)         { return true, nil }

// errGit always returns the configured error.
type errGit struct{ err error }

func (e *errGit) HeadSHA(_ context.Context) (string, error)       { return "", e.err }
func (e *errGit) CurrentBranch(_ context.Context) (string, error) { return "", e.err }
func (e *errGit) IsClean(_ context.Context) (bool, error)         { return false, e.err }

// fakeBriefing returns a fixed prompt regardless of input. Tests do not
// assert prompt content; only the loop's wire-protocol consumption.
type fakeBriefing struct{}

func (fakeBriefing) BuildPrompt(_ context.Context, _ loop.BriefingInput) (string, error) {
	return "stub prompt", nil
}

// errBriefing returns the configured error from BuildPrompt.
type errBriefing struct{ err error }

func (e *errBriefing) BuildPrompt(_ context.Context, _ loop.BriefingInput) (string, error) {
	return "", e.err
}

// signalEvent builds a single AgentEvent carrying a BccEventIterationResult.
func signalEvent(s agentcontract.Signal) loop.AgentEvent {
	return loop.AgentEvent{
		Kind: loop.KindBccEvent,
		At:   time.Now(),
		Bcc: &agentcontract.BccEvent{
			Kind:   agentcontract.BccEventIterationResult,
			Signal: s,
		},
	}
}

func newTestConfig() *config.Config {
	c := &config.Config{}
	config.ApplyDefaults(c)
	return c
}

// runWithEvents runs the loop with a buffered events channel and
// returns (exitCode, err, events). The channel is drained after Run
// returns so callers can assert the full event sequence.
func runWithEvents(t *testing.T, l *loop.Loop) (int, error, []loop.Event) {
	t.Helper()
	events := make(chan loop.Event, 1024)
	code, err := l.Run(context.Background(), events)
	var got []loop.Event
	for ev := range events {
		got = append(got, ev)
	}
	return code, err, got
}

func TestRun_ContinueThenDone(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(
		fake.Step{Events: []loop.AgentEvent{signalEvent(agentcontract.SignalContinue)}},
		fake.Step{Events: []loop.AgentEvent{signalEvent(agentcontract.SignalDone)}},
	)
	git := &fakeGit{heads: []string{"A", "B", "B", "C"}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{},
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want %d (loop.ExitDone)", code, loop.ExitDone)
	}
	if exec.CallCount() != 2 {
		t.Errorf("executor called %d times, want 2", exec.CallCount())
	}
}

func TestRun_BlockedStops(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{Events: []loop.AgentEvent{signalEvent(agentcontract.SignalBlocked)}})
	git := &fakeGit{heads: []string{"A", "B"}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{},
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitBlocked {
		t.Errorf("exit = %d, want %d", code, loop.ExitBlocked)
	}
}

func TestRun_ReviewStops(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{Events: []loop.AgentEvent{signalEvent(agentcontract.SignalReview)}})
	git := &fakeGit{heads: []string{"A", "B"}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{},
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitReview {
		t.Errorf("exit = %d, want %d (ExitReview)", code, loop.ExitReview)
	}
}

func TestRun_HEADStuck(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{Events: []loop.AgentEvent{signalEvent(agentcontract.SignalContinue)}})
	git := &fakeGit{heads: []string{"A", "A"}} // head did not advance
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{},
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitHEADStuck {
		t.Errorf("exit = %d, want %d", code, loop.ExitHEADStuck)
	}
}

func TestRun_NoIterationResultIsInvalid(t *testing.T) {
	cfg := newTestConfig()
	// Step has no events; agent emitted no iteration_result.
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{},
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitInvalid {
		t.Errorf("exit = %d, want %d (ExitInvalid)", code, loop.ExitInvalid)
	}
}

func TestRun_MaxIterationsReached(t *testing.T) {
	cfg := newTestConfig()
	cfg.Loop.MaxIterations = 2
	exec := fake.New(
		fake.Step{Events: []loop.AgentEvent{signalEvent(agentcontract.SignalContinue)}},
		fake.Step{Events: []loop.AgentEvent{signalEvent(agentcontract.SignalContinue)}},
	)
	git := &fakeGit{heads: []string{"A", "B", "B", "C"}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{},
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitMaxIterations {
		t.Errorf("exit = %d, want %d", code, loop.ExitMaxIterations)
	}
}

func TestRun_ExecutorErrorPropagates(t *testing.T) {
	cfg := newTestConfig()
	wantErr := errors.New("boom")
	exec := fake.New(fake.Step{Err: wantErr})
	git := &fakeGit{heads: []string{"A", "B"}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{},
	}
	code, err, _ := runWithEvents(t, l)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want loop.ExitInvalid", code)
	}
}

func TestRun_GitErrorPropagates(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &errGit{err: errors.New("git boom")}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{},
	}
	code, err, _ := runWithEvents(t, l)
	if err == nil {
		t.Errorf("expected error from git probe")
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want loop.ExitInvalid", code)
	}
}

func TestRun_BriefingErrorPropagates(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A"}}
	briefingErr := errors.New("briefing boom")
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: &errBriefing{err: briefingErr},
	}
	code, err, _ := runWithEvents(t, l)
	if !errors.Is(err, briefingErr) {
		t.Errorf("err = %v, want %v", err, briefingErr)
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want ExitInvalid", code)
	}
}

func TestRun_SingleShotForcesOneIteration(t *testing.T) {
	cfg := newTestConfig()
	cfg.Loop.MaxIterations = 5 // ignored in single-shot
	exec := fake.New(fake.Step{Events: []loop.AgentEvent{signalEvent(agentcontract.SignalContinue)}})
	git := &fakeGit{heads: []string{"A", "B"}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{}, SingleShot: true,
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitMaxIterations {
		t.Errorf("exit = %d, want %d (cap reached after 1 iter)", code, loop.ExitMaxIterations)
	}
	if exec.CallCount() != 1 {
		t.Errorf("executor called %d times, want 1", exec.CallCount())
	}
}

func TestRun_NilPortsReturnInvalid(t *testing.T) {
	cfg := newTestConfig()
	cases := []struct {
		name string
		l    *loop.Loop
	}{
		{
			name: "no executor",
			l:    &loop.Loop{Config: cfg, Git: &fakeGit{}, Briefing: fakeBriefing{}},
		},
		{
			name: "no git",
			l:    &loop.Loop{Config: cfg, Executor: fake.New(), Briefing: fakeBriefing{}},
		},
		{
			name: "no briefing",
			l:    &loop.Loop{Config: cfg, Executor: fake.New(), Git: &fakeGit{}},
		},
		{
			name: "no config",
			l:    &loop.Loop{Executor: fake.New(), Git: &fakeGit{}, Briefing: fakeBriefing{}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err, _ := runWithEvents(t, tc.l)
			if err == nil {
				t.Errorf("expected error")
			}
			if code != loop.ExitInvalid {
				t.Errorf("code = %d, want loop.ExitInvalid", code)
			}
		})
	}
}

func TestRun_EmitsEventsInOrder(t *testing.T) {
	cfg := newTestConfig()
	cfg.Loop.MaxIterations = 1
	exec := fake.New(fake.Step{Events: []loop.AgentEvent{signalEvent(agentcontract.SignalDone)}})
	git := &fakeGit{heads: []string{"A", "B"}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{},
	}
	_, err, events := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(events) < 4 {
		t.Fatalf("events = %d, want at least 4 (started, agent, finished, loop_finished)", len(events))
	}
	if _, ok := events[0].(loop.IterationStarted); !ok {
		t.Errorf("events[0] = %T, want IterationStarted", events[0])
	}
	if _, ok := events[len(events)-1].(loop.LoopFinished); !ok {
		t.Errorf("last event = %T, want LoopFinished", events[len(events)-1])
	}
	// The final IterationFinished should carry SignalDone.
	var finished *loop.IterationFinished
	for i := range events {
		if f, ok := events[i].(loop.IterationFinished); ok {
			finished = &f
		}
	}
	if finished == nil {
		t.Fatalf("no IterationFinished in event stream")
	}
	if finished.Signal != agentcontract.SignalDone {
		t.Errorf("IterationFinished.Signal = %v, want SignalDone", finished.Signal)
	}
}

func TestRun_PauseGateBlocksIteration(t *testing.T) {
	cfg := newTestConfig()
	cfg.Loop.MaxIterations = 2
	exec := fake.New(
		fake.Step{Events: []loop.AgentEvent{signalEvent(agentcontract.SignalContinue)}},
		fake.Step{Events: []loop.AgentEvent{signalEvent(agentcontract.SignalDone)}},
	)
	git := &fakeGit{heads: []string{"A", "B", "B", "C"}}
	gate := make(chan struct{}, 1)
	gate <- struct{}{} // release the second iteration immediately
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{}, PauseGate: gate,
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want %d", code, loop.ExitDone)
	}
}

// TestRun_AgentEventForwardingPreservesShape ensures that the loop pumps
// AgentEvents to the events channel without dropping or reordering.
func TestRun_AgentEventForwardingPreservesShape(t *testing.T) {
	cfg := newTestConfig()
	cfg.Loop.MaxIterations = 1
	scripted := []loop.AgentEvent{
		{Kind: loop.KindAssistantText, Text: "first", At: time.Now()},
		signalEvent(agentcontract.SignalDone),
	}
	exec := fake.New(fake.Step{Events: scripted})
	git := &fakeGit{heads: []string{"A", "B"}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		Briefing: fakeBriefing{},
	}
	_, err, events := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var forwarded []loop.AgentEvent
	for _, ev := range events {
		if rec, ok := ev.(loop.AgentEventReceived); ok {
			forwarded = append(forwarded, rec.Event)
		}
	}
	if len(forwarded) != len(scripted) {
		t.Fatalf("forwarded %d events, want %d", len(forwarded), len(scripted))
	}
	if !reflect.DeepEqual(forwarded[0].Kind, scripted[0].Kind) ||
		forwarded[0].Text != scripted[0].Text {
		t.Errorf("first forwarded event mismatch")
	}
	if forwarded[1].Kind != loop.KindBccEvent {
		t.Errorf("second forwarded event Kind = %v, want KindBccEvent", forwarded[1].Kind)
	}
}
