// Package loop defines the iteration loop and the ports it consumes.
//
// Ports (interfaces) are declared here, in the consumer package, per Go
// convention and the hexagonal-light layout in AGENTS.md. Adapters that
// implement them live in sibling packages: executor/<flavor>, git/<flavor>,
// specreader/<flavor>.
//
// Loop itself is implemented in loop.go (Phase 1.3).
package loop

import (
	"context"
	"io"
)

// Executor runs the configured agent against a prompt and streams its
// JSONL output (one event per line) to jsonlOut.
//
// Cancellation: when ctx is canceled, the implementation must signal the
// subprocess (typically SIGINT), wait for it to exit (with a bounded
// grace period before forcing SIGKILL), and return promptly. The
// implementation should append a terminator event of the form
// {"type":"interrupted"} to jsonlOut so downstream consumers can detect
// abnormal end-of-stream.
//
// Return contract:
//
//   - exitCode is the agent's process exit code (0 on success).
//   - err is nil when the subprocess started, completed, and its stdout
//     was streamed without I/O errors. err is non-nil for invocation
//     failures (e.g., binary not found), context cancellation, or stream
//     write errors. When err wraps context.Canceled or
//     context.DeadlineExceeded, callers should treat the iteration as
//     interrupted and exitCode as advisory.
type Executor interface {
	Run(ctx context.Context, prompt string, jsonlOut io.Writer) (exitCode int, err error)
}

// GitProbe is the read-only view of the working tree the loop needs.
// All methods may shell out to the git binary, but never mutate state.
type GitProbe interface {
	HeadSHA(ctx context.Context) (string, error)
	CurrentBranch(ctx context.Context) (string, error)
	IsClean(ctx context.Context) (bool, error)
}

// SpecReader loads the markdown content of a spec by path. Errors are
// returned as-is from the underlying filesystem; the loop maps them to
// invalid-spec exit codes.
type SpecReader interface {
	Read(path string) (string, error)
}
