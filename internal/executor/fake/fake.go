// Package fake is a deterministic loop.Executor implementation used in
// tests. It replays scripted Steps on consecutive Run calls and records
// the prompts it received.
//
// Not for production use. Lives under internal/executor so it sits next
// to the real adapter; importable from any test in the module.
package fake

import (
	"context"
	"fmt"
	"os"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// Compile-time check.
var _ loop.Executor = (*Executor)(nil)

// Step is one scripted iteration response.
type Step struct {
	// Events are pushed onto the events channel in order, one by one.
	// Real adapters translate native agent events into AgentEvents;
	// the fake skips the translation and lets tests script the result.
	Events []loop.AgentEvent

	// RawLog, when non-empty, is written verbatim to the file at
	// BCC_JSONL_PATH (set by the loop before each iteration). Mirrors
	// what a real adapter does when it persists the agent's raw event
	// stream for audit. ExecResult.LogPath is set to BCC_JSONL_PATH.
	RawLog string

	// ExitCode is the value returned in ExecResult.
	ExitCode int

	// Err is the error returned from Run. nil means success. ctx errors
	// can be simulated here to exercise the cancel path.
	Err error
}

// Executor replays Steps in order.
type Executor struct {
	steps   []Step
	called  int
	prompts []string
}

// New returns a fake Executor that will replay steps in order.
func New(steps ...Step) *Executor {
	return &Executor{steps: steps}
}

// Run pushes Step.Events on the events channel, optionally writes
// Step.RawLog to BCC_JSONL_PATH, and returns ExecResult, Step.Err. If
// called more times than there are steps, returns an error indicating
// the test exhausted the script (catches off-by-one in loop tests).
func (e *Executor) Run(ctx context.Context, prompt string, events chan<- loop.AgentEvent) (loop.ExecResult, error) {
	if e.called >= len(e.steps) {
		return loop.ExecResult{ExitCode: -1}, fmt.Errorf("fake: out of scripted steps (called %d, have %d)", e.called+1, len(e.steps))
	}
	step := e.steps[e.called]
	e.called++
	e.prompts = append(e.prompts, prompt)

	logPath := os.Getenv("BCC_JSONL_PATH")
	if step.RawLog != "" && logPath != "" {
		if err := os.WriteFile(logPath, []byte(step.RawLog), 0o644); err != nil {
			return loop.ExecResult{ExitCode: -1}, fmt.Errorf("fake: write raw log %s: %w", logPath, err)
		}
	}

	for _, ev := range step.Events {
		select {
		case events <- ev:
		case <-ctx.Done():
			return loop.ExecResult{ExitCode: -1, LogPath: logPath}, ctx.Err()
		}
	}

	return loop.ExecResult{ExitCode: step.ExitCode, LogPath: logPath}, step.Err
}

// CallCount returns how many times Run was called.
func (e *Executor) CallCount() int { return e.called }

// Prompts returns a copy of the prompts received, in call order. Useful
// for asserting that the loop builds the expected prompt per iteration.
func (e *Executor) Prompts() []string {
	out := make([]string, len(e.prompts))
	copy(out, e.prompts)
	return out
}
