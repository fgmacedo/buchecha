// Package loop defines the iteration loop and the ports it consumes.
//
// Ports (interfaces) are declared here, in the consumer package, per Go
// convention and the hexagonal-light layout in CLAUDE.md. Adapters that
// implement them live in sibling packages: executor/<flavor>, git/<flavor>,
// specreader/<flavor>.
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

// SpecContent loads the raw markdown content of a spec by path. Errors
// are returned as-is from the underlying filesystem; the loop maps them
// to invalid-spec exit codes.
//
// SpecContent is the legacy content-shaped port: it returns bytes for a
// prompt template to interpolate. Format-aware introspection lives on
// SpecReader (below) and prompt construction lives on AgentBriefing.
// Once both are wired through every consumer, SpecContent retires.
type SpecContent interface {
	Read(path string) (string, error)
}

// Signal is the decision-relevant outcome of an iteration as observed
// by the active spec adapter. It is the format-neutral language the
// loop and the TUI consume; format-specific result vocabularies (e.g.,
// bcc-markdown's "ok"/"partial"/"done"/"blocked"/"review") are mapped
// to Signal at the adapter boundary.
type Signal int

const (
	// SignalUnknown is the zero value: the adapter could not determine a
	// signal, or no record exists yet.
	SignalUnknown Signal = iota
	// SignalContinue means the iteration produced normal progress and
	// the loop should run another iteration.
	SignalContinue
	// SignalReview means the iteration reached an observer-driven gate
	// and the loop should stop until a human edits the spec.
	SignalReview
	// SignalDone means every pending unit is complete and the loop
	// should terminate successfully.
	SignalDone
	// SignalBlocked means the iteration hit an unrecoverable failure
	// and the loop should stop with a non-zero exit.
	SignalBlocked
)

// String returns a stable lower-case label for the signal.
func (s Signal) String() string {
	switch s {
	case SignalContinue:
		return "continue"
	case SignalReview:
		return "review"
	case SignalDone:
		return "done"
	case SignalBlocked:
		return "blocked"
	default:
		return "unknown"
	}
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
// OpenSpec's for the openspec adapter, etc.) and stitches it into the
// prompt with format-specific framing.
//
// The loop calls BuildPrompt once per iteration and passes the result
// to the executor unchanged.
type AgentBriefing interface {
	BuildPrompt(ctx context.Context, in BriefingInput) (string, error)
}

// BccEventKind is the type tag for a bcc-protocol event emitted by the
// agent on stdout. The set is intentionally small; adapters that need
// richer signals carry them in BccEvent.Raw.
type BccEventKind int

const (
	// BccEventUnknown is the zero value; consumers should ignore.
	BccEventUnknown BccEventKind = iota
	// BccEventTaskStarted marks the agent beginning work on a unit
	// (phase, task, etc.); BccEvent.ID identifies the unit.
	BccEventTaskStarted
	// BccEventTaskCompleted marks the agent finishing a unit.
	BccEventTaskCompleted
	// BccEventIterationResult carries the iteration's overall signal;
	// BccEvent.Signal is populated.
	BccEventIterationResult
	// BccEventProgressTick is an opaque heartbeat; consumers may use
	// it to drive UI without otherwise reacting.
	BccEventProgressTick
)

// BccEvent is a normalized agent-emitted progress event. Adapters
// translate their wire format into this shape; the loop and the TUI
// consume it without knowing the source format.
type BccEvent struct {
	Kind    BccEventKind
	ID      string
	Signal  Signal // populated only for BccEventIterationResult
	Summary string
	// Raw preserves adapter-specific payload for round-tripping.
	Raw map[string]any
}

// AgentEvents inspects raw stdout lines from the executor and
// translates the adapter's bcc-protocol sentinels into BccEvents.
// Lines that are not bcc-protocol events return ok=false; the executor
// falls through to its existing handling.
type AgentEvents interface {
	ParseLine(line []byte) (BccEvent, bool)
}
