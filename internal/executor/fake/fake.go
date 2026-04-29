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
	"io"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// Compile-time check.
var _ loop.Executor = (*Executor)(nil)

// Step is one scripted iteration response.
type Step struct {
	// JSONL is the raw text written to jsonlOut on this Run. Should end
	// with a newline if the consumer expects line-by-line semantics.
	JSONL string

	// ExitCode is the value returned from Run.
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

// Run writes Step.JSONL to jsonlOut and returns Step.ExitCode, Step.Err.
// If called more times than there are steps, returns an error indicating
// the test exhausted the script (catches off-by-one in loop tests).
func (e *Executor) Run(ctx context.Context, prompt string, jsonlOut io.Writer) (int, error) {
	if e.called >= len(e.steps) {
		return -1, fmt.Errorf("fake: out of scripted steps (called %d, have %d)", e.called+1, len(e.steps))
	}
	step := e.steps[e.called]
	e.called++
	e.prompts = append(e.prompts, prompt)

	if step.JSONL != "" {
		if _, err := io.WriteString(jsonlOut, step.JSONL); err != nil {
			return -1, fmt.Errorf("fake: write jsonl: %w", err)
		}
	}
	return step.ExitCode, step.Err
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
