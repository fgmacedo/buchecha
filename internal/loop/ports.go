// Package loop defines the iteration loop and the ports it consumes.
//
// Ports (interfaces) are declared here, in the consumer package, per Go
// convention and the hexagonal-light layout in CLAUDE.md. Adapters that
// implement them live in sibling packages: executor/<flavor>, git/<flavor>,
// format/<flavor>.
//
// Wire-protocol types (Signal, BccEvent, ParseLine) and the
// format-neutral markdown blocks every adapter composes (wire
// protocol, absolute restrictions, working tree invariants) live in
// internal/loop/agentcontract/. They are bcc-level, not format-level,
// so they have one canonical home.
//
// Loop itself is implemented in loop.go.
package loop

import (
	"context"
)

// Executor runs the configured agent against a prompt and emits a stream
// of normalized AgentEvents on the events channel. Adapters translate
// their native formats to AgentEvent before sending.
//
// The events channel is owned by the caller (the loop). The adapter
// only sends; it must not close the channel. The caller closes it after
// Run returns.
//
// Cancellation: when ctx is canceled, the implementation must signal
// the subprocess (typically SIGINT), wait for it to exit (with a
// bounded grace period before forcing SIGKILL), drain its parser, and
// return promptly with ctx.Err().
//
// Return contract:
//
//   - ExecResult.ExitCode is the agent's process exit code (0 on success).
//   - err is nil when the subprocess started, completed, and the parser
//     finished without I/O errors. err is non-nil for invocation failures
//     (binary not found), context cancellation, or stream-write errors.
//     When err wraps context.Canceled or context.DeadlineExceeded,
//     callers should treat the iteration as interrupted and ExitCode as
//     advisory.
type Executor interface {
	Run(ctx context.Context, prompt string, events chan<- AgentEvent) (ExecResult, error)
}

// GitProbe is the read-only view of the working tree the loop needs.
// All methods may shell out to the git binary, but never mutate state.
type GitProbe interface {
	HeadSHA(ctx context.Context) (string, error)
	CurrentBranch(ctx context.Context) (string, error)
	IsClean(ctx context.Context) (bool, error)
}

// SpecContent loads the raw markdown content of a spec by path.
//
// SpecContent is the legacy content-shaped port: it returns bytes for
// the legacy prompt template (internal/loop/prompt.go) to interpolate.
// The migration to the AgentBriefing port retires both this type and
// the legacy template; SpecContent stays only as long as Loop still
// uses the legacy path. Do not introduce new consumers.
type SpecContent interface {
	Read(path string) (string, error)
}

// Mode is the loop execution mode the briefing is being built for.
type Mode int

const (
	// ModeLoop is phase-by-phase iteration: one prompt per pending
	// phase, until done or blocked.
	ModeLoop Mode = iota
	// ModeSingleShot is a one-shot run: one prompt, the agent tries
	// the entire spec end to end.
	ModeSingleShot
)

// BriefingInput carries the orchestration context AgentBriefing.BuildPrompt
// uses to render a per-iteration prompt. Per-format options (heading
// text, journal store, etc.) live on the adapter's Config and are not
// passed per-iteration.
type BriefingInput struct {
	// SpecPath is the absolute or cwd-relative path to the spec.
	SpecPath string

	// Iteration is the 1-based index of the iteration about to run.
	Iteration int

	// Mode tells the briefing whether to render loop-mode framing or
	// single-shot framing.
	Mode Mode

	// Extra is user-provided extra instructions appended to the prompt
	// (sourced from --extra). Empty when not set.
	Extra string
}

// AgentBriefing builds the per-iteration prompt for the active spec
// format. The adapter owns the embedded operating contract for its
// format (bcc-markdown's contract for the markdown_bcc adapter,
// OpenSpec's for the openspec adapter, etc.) and composes the final
// prompt by extending agentcontract.Partials() with its own template.
//
// The loop calls BuildPrompt once per iteration and passes the result
// to the executor unchanged.
type AgentBriefing interface {
	BuildPrompt(ctx context.Context, in BriefingInput) (string, error)
}
