package loop_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/config"
	"github.com/fgmacedo/buchecha/internal/executor/fake"
	"github.com/fgmacedo/buchecha/internal/loop"
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

// stepfulSpecReader returns the n-th content on the n-th Read call.
type stepfulSpecReader struct {
	contents []string
	idx      int
}

func (s *stepfulSpecReader) Read(_ string) (string, error) {
	if s.idx >= len(s.contents) {
		return "", fmt.Errorf("specreader: out of contents (idx=%d)", s.idx)
	}
	c := s.contents[s.idx]
	s.idx++
	return c, nil
}

// errSpecReader always returns the configured error.
type errSpecReader struct{ err error }

func (e *errSpecReader) Read(_ string) (string, error) { return "", e.err }

// errGit always returns the configured error.
type errGit struct{ err error }

func (e *errGit) HeadSHA(_ context.Context) (string, error)       { return "", e.err }
func (e *errGit) CurrentBranch(_ context.Context) (string, error) { return "", e.err }
func (e *errGit) IsClean(_ context.Context) (bool, error)         { return false, e.err }

// specWith builds a minimal English spec with the given checkbox states
// in a single phase, and a single journal entry with the given result
// value. Helper for table-driven tests.
func specWith(states []string, result string) string {
	var items []string
	for i, s := range states {
		items = append(items, fmt.Sprintf("1. %s Item %d", s, i+1))
	}
	return fmt.Sprintf(`# spec

## Implementation Plan

### P1: phase

%s

## Execution Journal

### entry

- **Result**: %s
`, strings.Join(items, "\n"), result)
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

func TestRun_OkThenDone(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(
		fake.Step{ExitCode: 0},
		fake.Step{ExitCode: 0},
	)
	git := &fakeGit{heads: []string{"A", "B", "B", "C"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]", "[ ]"}, "ok"),
		specWith([]string{"[x]", "[x]", "[x]"}, "done"),
	}}
	l := &loop.Loop{
		SpecPath:   "x.md",
		Config:     cfg,
		Executor:   exec,
		Git:        git,
		SpecReader: reader,
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
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]", "[ ]"}, "blocked"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader,
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitBlocked {
		t.Errorf("exit = %d, want %d (loop.ExitBlocked)", code, loop.ExitBlocked)
	}
}

func TestRun_DoneWithLeftovers(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]"}, "done"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader,
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDoneWithLeftovers {
		t.Errorf("exit = %d, want %d (loop.ExitDoneWithLeftovers)", code, loop.ExitDoneWithLeftovers)
	}
}

func TestRun_HEADStuck(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "A"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]"}, "ok"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader,
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitHEADStuck {
		t.Errorf("exit = %d, want %d (loop.ExitHEADStuck)", code, loop.ExitHEADStuck)
	}
}

func TestRun_UnknownResultIsInvalid(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]"}, "weird"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader,
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitInvalid {
		t.Errorf("exit = %d, want %d (loop.ExitInvalid)", code, loop.ExitInvalid)
	}
}

func TestRun_MaxIterationsReached(t *testing.T) {
	cfg := newTestConfig()
	cfg.Loop.MaxIterations = 2
	exec := fake.New(
		fake.Step{ExitCode: 0},
		fake.Step{ExitCode: 0},
	)
	git := &fakeGit{heads: []string{"A", "B", "B", "C"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]"}, "ok"),
		specWith([]string{"[x]", "[ ]"}, "partial"), // still has [ ]
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader,
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitMaxIterations {
		t.Errorf("exit = %d, want %d (loop.ExitMaxIterations)", code, loop.ExitMaxIterations)
	}
}

func TestRun_ExecutorErrorPropagates(t *testing.T) {
	cfg := newTestConfig()
	wantErr := errors.New("boom")
	exec := fake.New(fake.Step{Err: wantErr})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{} // never reached
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader,
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
	reader := &stepfulSpecReader{}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader,
	}
	code, err, _ := runWithEvents(t, l)
	if err == nil {
		t.Fatalf("expected error")
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want loop.ExitInvalid", code)
	}
}

func TestRun_SpecReaderErrorPropagates(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &errSpecReader{err: errors.New("read boom")}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader,
	}
	code, err, _ := runWithEvents(t, l)
	if err == nil {
		t.Fatalf("expected error")
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want loop.ExitInvalid", code)
	}
}

func TestRun_SingleShotCapsAtOne(t *testing.T) {
	cfg := newTestConfig()
	cfg.Loop.MaxIterations = 99 // would normally allow many iterations
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{
		// even though plan still has [ ], single-shot should cap at 1
		specWith([]string{"[x]", "[ ]"}, "ok"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader, SingleShot: true,
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitMaxIterations {
		t.Errorf("exit = %d, want %d", code, loop.ExitMaxIterations)
	}
	if exec.CallCount() != 1 {
		t.Errorf("executor called %d times, want 1 (single-shot)", exec.CallCount())
	}
}

func TestRun_PortugueseLocalized(t *testing.T) {
	cfg := &config.Config{Project: config.Project{Language: "pt-BR"}}
	config.ApplyDefaults(cfg)

	plan := strings.Join([]string{
		"1. [x] Item um",
		"1. [x] Item dois",
	}, "\n")
	specPt := fmt.Sprintf(`# spec

## Plano de implementação

### F1

%s

## Diário de execução

- **Resultado**: finalizado
`, plan)

	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{specPt}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader,
	}
	code, err, _ := runWithEvents(t, l)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want %d (loop.ExitDone) for pt-BR done with no leftovers", code, loop.ExitDone)
	}
}

func TestRun_RejectsZeroMaxIterations(t *testing.T) {
	cfg := newTestConfig()
	cfg.Loop.MaxIterations = 0 // explicit override after defaults
	l := &loop.Loop{
		SpecPath:   "x.md",
		Config:     cfg,
		Executor:   fake.New(),
		Git:        &fakeGit{},
		SpecReader: &stepfulSpecReader{},
	}
	code, err, _ := runWithEvents(t, l)
	if err == nil {
		t.Errorf("expected error for max_iterations <= 0")
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want loop.ExitInvalid", code)
	}
}

func TestRun_NilPortsRejected(t *testing.T) {
	cfg := newTestConfig()
	l := &loop.Loop{SpecPath: "x.md", Config: cfg}
	code, err, _ := runWithEvents(t, l)
	if err == nil {
		t.Errorf("expected error for nil ports")
	}
	if code != loop.ExitInvalid {
		t.Errorf("code = %d, want loop.ExitInvalid", code)
	}
}

// TestRun_EventSequence asserts the kinds of Events emitted by the
// loop for a known fake transcript, in order.
func TestRun_EventSequence(t *testing.T) {
	tests := []struct {
		name      string
		steps     []fake.Step
		heads     []string
		contents  []string
		wantTypes []string
	}{
		{
			name: "single iteration done",
			steps: []fake.Step{
				{
					Events: []loop.AgentEvent{
						{Kind: loop.KindInit, Init: &loop.InitInfo{SessionID: "s1"}},
						{Kind: loop.KindToolUse, Tool: &loop.ToolCallInfo{Name: "Read"}},
						{Kind: loop.KindResultSummary, Done: &loop.ResultSummaryInfo{NumTurns: 1}},
					},
					ExitCode: 0,
				},
			},
			heads: []string{"A", "B"},
			contents: []string{
				specWith([]string{"[x]", "[x]"}, "done"),
			},
			wantTypes: []string{
				"IterationStarted",
				"AgentEventReceived",
				"AgentEventReceived",
				"AgentEventReceived",
				"IterationFinished",
				"LoopFinished",
			},
		},
		{
			name: "two iterations to done",
			steps: []fake.Step{
				{Events: []loop.AgentEvent{{Kind: loop.KindToolUse}}, ExitCode: 0},
				{Events: []loop.AgentEvent{{Kind: loop.KindResultSummary}}, ExitCode: 0},
			},
			heads: []string{"A", "B", "B", "C"},
			contents: []string{
				specWith([]string{"[x]", "[ ]", "[ ]"}, "ok"),
				specWith([]string{"[x]", "[x]", "[x]"}, "done"),
			},
			wantTypes: []string{
				"IterationStarted",
				"AgentEventReceived",
				"IterationFinished",
				"IterationStarted",
				"AgentEventReceived",
				"IterationFinished",
				"LoopFinished",
			},
		},
		{
			name: "no agent events still emits boundaries and finish",
			steps: []fake.Step{
				{ExitCode: 0},
			},
			heads: []string{"A", "B"},
			contents: []string{
				specWith([]string{"[x]", "[x]"}, "done"),
			},
			wantTypes: []string{
				"IterationStarted",
				"IterationFinished",
				"LoopFinished",
			},
		},
		{
			name: "blocked terminates after one iteration",
			steps: []fake.Step{
				{Events: []loop.AgentEvent{{Kind: loop.KindAssistantText, Text: "stuck"}}, ExitCode: 0},
			},
			heads: []string{"A", "B"},
			contents: []string{
				specWith([]string{"[x]", "[ ]", "[ ]"}, "blocked"),
			},
			wantTypes: []string{
				"IterationStarted",
				"AgentEventReceived",
				"IterationFinished",
				"LoopFinished",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newTestConfig()
			exec := fake.New(tt.steps...)
			git := &fakeGit{heads: tt.heads}
			reader := &stepfulSpecReader{contents: tt.contents}
			l := &loop.Loop{
				SpecPath:   "x.md",
				Config:     cfg,
				Executor:   exec,
				Git:        git,
				SpecReader: reader,
			}
			_, _, got := runWithEvents(t, l)
			gotTypes := make([]string, len(got))
			for i, ev := range got {
				gotTypes[i] = eventTypeName(ev)
			}
			if !reflect.DeepEqual(gotTypes, tt.wantTypes) {
				t.Errorf("event sequence mismatch:\n got: %v\nwant: %v", gotTypes, tt.wantTypes)
			}
		})
	}
}

func TestRun_LoopFinishedCarriesExitCode(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[x]"}, "done"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader,
	}
	code, _, got := runWithEvents(t, l)
	if len(got) == 0 {
		t.Fatalf("no events received")
	}
	last, ok := got[len(got)-1].(loop.LoopFinished)
	if !ok {
		t.Fatalf("last event = %T, want LoopFinished", got[len(got)-1])
	}
	if last.ExitCode != code {
		t.Errorf("LoopFinished.ExitCode = %d, run code = %d (must match)", last.ExitCode, code)
	}
	if last.ExitCode != loop.ExitDone {
		t.Errorf("LoopFinished.ExitCode = %d, want %d", last.ExitCode, loop.ExitDone)
	}
	if last.Reason != "done" {
		t.Errorf("LoopFinished.Reason = %q, want %q", last.Reason, "done")
	}
}

// TestRun_PauseGateBlocksBetweenIterations asserts the contract the
// TUI relies on: when PauseGate is set, the loop blocks before each
// iteration after the first until a token is posted. The first
// iteration is never gated (it would deadlock on first run).
func TestRun_PauseGateBlocksBetweenIterations(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(
		fake.Step{ExitCode: 0},
		fake.Step{ExitCode: 0},
	)
	git := &fakeGit{heads: []string{"A", "B", "B", "C"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]", "[ ]"}, "ok"),
		specWith([]string{"[x]", "[x]", "[x]"}, "done"),
	}}
	gate := make(chan struct{}, 1)
	l := &loop.Loop{
		SpecPath:   "x.md",
		Config:     cfg,
		Executor:   exec,
		Git:        git,
		SpecReader: reader,
		PauseGate:  gate,
	}
	events := make(chan loop.Event, 1024)
	doneCh := make(chan int, 1)
	go func() {
		code, _ := l.Run(context.Background(), events)
		doneCh <- code
	}()

	// Wait until the first IterationFinished arrives, then confirm the
	// loop is parked on the gate (no IterationStarted for iter 2 yet).
	sawFinish := false
	deadline := time.After(2 * time.Second)
	for !sawFinish {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("events closed before iter 1 finished")
			}
			if _, ok := ev.(loop.IterationFinished); ok {
				sawFinish = true
			}
		case <-deadline:
			t.Fatalf("timeout waiting for IterationFinished")
		}
	}
	// Drain a short window to confirm iter 2 has not started.
	select {
	case ev := <-events:
		if _, ok := ev.(loop.IterationStarted); ok {
			t.Fatalf("loop advanced to iter 2 without gate token")
		}
	case <-time.After(50 * time.Millisecond):
	}

	// Release the gate and confirm the loop terminates with done.
	gate <- struct{}{}
	for ev := range events {
		_ = ev
	}
	if got := <-doneCh; got != loop.ExitDone {
		t.Errorf("exit = %d, want %d (ExitDone)", got, loop.ExitDone)
	}
	if exec.CallCount() != 2 {
		t.Errorf("executor called %d times, want 2", exec.CallCount())
	}
}

func TestRun_IterationFinishedCarriesResult(t *testing.T) {
	cfg := newTestConfig()
	exec := fake.New(fake.Step{ExitCode: 0})
	git := &fakeGit{heads: []string{"A", "B"}}
	reader := &stepfulSpecReader{contents: []string{
		specWith([]string{"[x]", "[ ]"}, "partial"),
	}}
	l := &loop.Loop{
		SpecPath: "x.md", Config: cfg, Executor: exec, Git: git,
		SpecReader: reader,
	}
	_, _, got := runWithEvents(t, l)
	var fin *loop.IterationFinished
	for i := range got {
		if f, ok := got[i].(loop.IterationFinished); ok {
			fin = &f
			break
		}
	}
	if fin == nil {
		t.Fatalf("no IterationFinished event")
	}
	if fin.Index != 1 {
		t.Errorf("Index = %d, want 1", fin.Index)
	}
	if !fin.HEADAdvanced {
		t.Errorf("HEADAdvanced = false, want true")
	}
	if fin.Result.String() != "partial" {
		t.Errorf("Result = %q, want partial", fin.Result.String())
	}
}

func eventTypeName(e loop.Event) string {
	switch e.(type) {
	case loop.IterationStarted:
		return "IterationStarted"
	case loop.AgentEventReceived:
		return "AgentEventReceived"
	case loop.IterationFinished:
		return "IterationFinished"
	case loop.LoopFinished:
		return "LoopFinished"
	default:
		return fmt.Sprintf("Unknown(%T)", e)
	}
}
