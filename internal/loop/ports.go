// Package loop defines the iteration loop and the ports it consumes.
//
// Ports (interfaces) are declared here, in the consumer package, per Go
// convention and the hexagonal-light layout in CLAUDE.md. Adapters that
// implement them live in sibling packages: executor/<flavor>, git/<flavor>,
// specreader/<flavor>, journal/<flavor>.
//
// Loop itself is implemented in loop.go.
package loop

import (
	"context"
	"time"
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

// RenderProfile selects how SpecReader.Render produces a TUI-friendly
// representation of the spec. Adapters may treat this as a hint or
// ignore it.
type RenderProfile int

const (
	// RenderTerminal targets a terminal viewport with ANSI styling.
	RenderTerminal RenderProfile = iota
	// RenderPlain targets a no-color, ASCII-only output.
	RenderPlain
)

// SpecReader is the read-side introspection port for a spec format.
// The active spec-format adapter implements it; the loop and the TUI
// consume it for decisions and progress display.
//
// Methods may read the spec from disk; implementations should be
// re-entrant and tolerate concurrent calls from different goroutines.
type SpecReader interface {
	// LatestSignal returns the most recent decision-relevant signal
	// from the spec's progress record (the journal, a tasks.json
	// status, whatever the format uses to record per-iteration
	// outcome).
	LatestSignal(ctx context.Context, specPath string) (Signal, error)

	// WorkRemaining is true when at least one pending unit exists. The
	// format decides what "pending" means: unchecked checkbox, open
	// task, task with unmet dependencies, etc.
	WorkRemaining(ctx context.Context, specPath string) (bool, error)

	// Progress is an optional UI hint. Adapters with no notion of
	// progress return ok=false; consumers degrade gracefully.
	Progress(ctx context.Context, specPath string) (checked, total int, ok bool, err error)

	// NextWorkItem returns an opaque, format-defined identifier for
	// the unit the agent should focus on next (phase number for
	// bcc-markdown, task ID for Ralph or OpenSpec). Adapters with no
	// notion of "next item" return ok=false; the briefing falls back
	// to a generic "implement the next pending item" prompt.
	NextWorkItem(ctx context.Context, specPath string) (id string, ok bool, err error)

	// Render produces a TUI-friendly view of the spec for the optional
	// preview panel. Adapters without a renderer return ok=false; the
	// panel is hidden.
	Render(ctx context.Context, specPath string, profile RenderProfile) (text string, ok bool, err error)
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
// uses to render a per-iteration prompt.
type BriefingInput struct {
	// SpecPath is the absolute or cwd-relative path to the spec.
	SpecPath string

	// Iteration is the 1-based index of the iteration about to run.
	Iteration int

	// NextItemID is the SpecReader.NextWorkItem result for this
	// iteration; empty when the adapter returned ok=false.
	NextItemID string

	// Mode tells the briefing whether to render loop-mode framing or
	// single-shot framing.
	Mode Mode

	// JournalEnabled is false when [journal].store = "none". The
	// briefing should suppress journal-writing instructions in that
	// mode.
	JournalEnabled bool
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

// JournalEntry is a normalized journal record. The format adapter owns
// the on-disk shape (markdown block in the spec, NDJSON sidecar,
// SQLite row); JournalEntry is the in-memory carrier between adapter
// and consumer.
type JournalEntry struct {
	At      time.Time
	Phase   string
	Signal  Signal
	Summary string
	// Raw preserves adapter-specific payload for round-tripping.
	Raw map[string]any
}

// JournalReader exposes the most recent journal entry to the TUI's
// optional viewer. bcc does not write journal entries: under the
// bcc-markdown contract the agent owns the write side, instructed by
// the AgentBriefing prompt. bcc does not read the journal for control
// flow either; signal comes from the bcc_event wire protocol. This
// port is therefore read-only and exists solely for display.
//
// Implementations are owned by the journal-store adapter selected by
// [journal].store. The no-op "none" store returns ok=false from Latest
// so the TUI viewer hides the binding.
type JournalReader interface {
	Latest(ctx context.Context) (entry JournalEntry, ok bool, err error)
}
