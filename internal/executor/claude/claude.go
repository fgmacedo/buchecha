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
	"os"
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
	// the prompt positional argument. Reserve for ad-hoc additions.
	ExtraArgs []string

	// SkipPermissions, when true, adds --dangerously-skip-permissions to
	// the args so claude does not stall the loop with confirmation
	// prompts. This is the documented contract of bcc's autonomous mode.
	// When false (explicit user opt-out via .bcc.toml), the loop is
	// likely to hang on the first tool call; the user accepts that.
	SkipPermissions bool

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
// stdout is captured to the file at BCC_JSONL_PATH (set by the loop
// before each iteration); claude emits one stream-json event per line,
// terminated with newline. ExecResult.LogPath returns that path. The
// adapter does not parse events into AgentEvent in this iteration of
// the spec; the events channel is currently used only for cancellation
// (P2.2 will wire up parsing).
//
// On context cancellation the subprocess receives SIGINT first; if it
// fails to exit within CancelGrace it is killed via SIGKILL (handled by
// exec.Cmd via WaitDelay). After waiting, if the cancel path was taken,
// a terminator line {"type":"interrupted"} is appended to the log file
// so downstream parsers can detect abnormal end-of-stream.
//
// Returns (ExecResult{ExitCode}, nil) on natural completion (including
// non-zero exit from the agent itself). Returns (ExecResult, ctx.Err())
// when canceled. Returns (ExecResult{ExitCode: -1}, err) when the
// invocation itself failed (e.g., binary missing).
func (e *Executor) Run(ctx context.Context, prompt string, _ chan<- loop.AgentEvent) (loop.ExecResult, error) {
	logPath := os.Getenv("BCC_JSONL_PATH")
	var stdout io.Writer = io.Discard
	var logFile *os.File
	if logPath != "" {
		f, err := os.Create(logPath)
		if err != nil {
			return loop.ExecResult{ExitCode: -1}, fmt.Errorf("create log %s: %w", logPath, err)
		}
		defer f.Close()
		logFile = f
		stdout = f
	}

	// -p, --output-format stream-json, and --verbose are required for
	// the loop to function (line-by-line JSONL events). They are not
	// configurable.
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
	}
	// --dangerously-skip-permissions is the precondition for autonomous
	// mode; without it claude prompts on every tool use. Users who set
	// skip_permissions=false in .bcc.toml accept that the loop will
	// stall on the first prompt.
	if e.cfg.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if e.cfg.Model != "" {
		args = append(args, "--model", e.cfg.Model)
	}
	args = append(args, e.cfg.ExtraArgs...)
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, e.cfg.Binary, args...)
	cmd.Stdout = stdout
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
		if logFile != nil {
			_, _ = logFile.Write([]byte(`{"type":"interrupted"}` + "\n"))
		}
		exitCode := -1
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
		}
		return loop.ExecResult{ExitCode: exitCode, LogPath: logPath}, ctxErr
	}

	if runErr == nil {
		return loop.ExecResult{ExitCode: 0, LogPath: logPath}, nil
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		// Agent exited non-zero; that is a normal control-flow signal,
		// not a Run failure. Caller decides what to do.
		return loop.ExecResult{ExitCode: ee.ExitCode(), LogPath: logPath}, nil
	}
	return loop.ExecResult{ExitCode: -1, LogPath: logPath}, fmt.Errorf("run %s: %w", e.cfg.Binary, runErr)
}
