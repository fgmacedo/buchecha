// Package claude implements loop.Executor for Claude Code (claude CLI in
// print mode with stream-json output).
//
// This is the only adapter today; codex and gemini will be added in
// Phase 3 as sibling packages under internal/executor.
package claude

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// Compile-time check that *Executor satisfies loop.Executor.
var _ loop.Executor = (*Executor)(nil)

// Config configures the Claude executor.
type Config struct {
	// Binary is the path or PATH name of the claude binary.
	Binary string

	// Model is passed via --model. Empty omits the flag.
	Model string

	// ExtraArgs are appended to the command line after --model and before
	// the prompt positional argument. Useful for, e.g.,
	// "--dangerously-skip-permissions".
	ExtraArgs []string

	// Stderr, when non-nil, receives the subprocess stderr verbatim.
	// Default (nil) discards stderr; callers wanting it should pipe to a
	// log file or os.Stderr explicitly.
	Stderr io.Writer

	// CancelGrace is how long to wait after sending SIGINT before forcing
	// SIGKILL. Defaults to 5 seconds when zero.
	CancelGrace time.Duration
}

// Executor invokes Claude Code in print mode and streams its stream-json
// events to a writer.
type Executor struct {
	cfg Config
}

// New returns a Claude Executor with cfg.
func New(cfg Config) *Executor {
	if cfg.CancelGrace == 0 {
		cfg.CancelGrace = 5 * time.Second
	}
	return &Executor{cfg: cfg}
}

// Run invokes the binary with print mode and stream-json output.
//
// stdout is written verbatim to jsonlOut (claude emits one event per
// line, terminated with newline; we do not parse here).
//
// On context cancellation the subprocess receives SIGINT first; if it
// fails to exit within CancelGrace it is killed via SIGKILL (handled by
// exec.Cmd via WaitDelay). After waiting, if the cancel path was taken,
// a terminator line {"type":"interrupted"} is appended to jsonlOut so
// downstream parsers can detect abnormal end-of-stream.
//
// Returns (exitCode, nil) on natural completion (including non-zero exit
// from the agent itself). Returns (exitCode, ctx.Err()) when canceled.
// Returns (-1, err) when invocation itself failed (e.g., binary missing).
func (e *Executor) Run(ctx context.Context, prompt string, jsonlOut io.Writer) (int, error) {
	args := []string{"-p", "--output-format", "stream-json", "--verbose"}
	if e.cfg.Model != "" {
		args = append(args, "--model", e.cfg.Model)
	}
	args = append(args, e.cfg.ExtraArgs...)
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, e.cfg.Binary, args...)
	cmd.Stdout = jsonlOut
	if e.cfg.Stderr != nil {
		cmd.Stderr = e.cfg.Stderr
	}
	cmd.Cancel = func() error {
		// Graceful interrupt; exec.Cmd escalates to SIGKILL after WaitDelay.
		return cmd.Process.Signal(syscall.SIGINT)
	}
	cmd.WaitDelay = e.cfg.CancelGrace

	runErr := cmd.Run()

	if ctxErr := ctx.Err(); ctxErr != nil {
		// Best-effort terminator; ignore write failures (writer may be closed).
		_, _ = jsonlOut.Write([]byte(`{"type":"interrupted"}` + "\n"))
		exitCode := -1
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
		}
		return exitCode, ctxErr
	}

	if runErr == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		// Agent exited non-zero; that is a normal control-flow signal,
		// not a Run failure. Caller decides what to do.
		return ee.ExitCode(), nil
	}
	return -1, fmt.Errorf("run %s: %w", e.cfg.Binary, runErr)
}
