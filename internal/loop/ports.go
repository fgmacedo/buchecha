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

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/director/dag"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
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
	Run(ctx context.Context, prompt string, events chan<- agentcontract.AgentEvent) (ExecResult, error)
}

// GitProbe is the read-only view of the working tree the loop needs.
// All methods may shell out to the git binary, but never mutate state.
//
// Diff returns the unified diff between two SHAs (baseSHA..headSHA). The
// Director's review pipeline consumes it as the primary evidence the
// Reviewer judges. Callers that pass equal SHAs receive an empty string
// without a git invocation; this lets the loop skip review when an
// iteration produced no commits.
type GitProbe interface {
	HeadSHA(ctx context.Context) (string, error)
	CurrentBranch(ctx context.Context) (string, error)
	IsClean(ctx context.Context) (bool, error)
	Diff(ctx context.Context, baseSHA, headSHA string) (string, error)
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

// EscalationKind is the kind of resolution the user picks when the
// Director loop pauses on a DirectorEscalation.
type EscalationKind int

const (
	// EscalationResume retries the phase one more time despite the
	// retry budget being exhausted; the user takes responsibility for
	// the extra attempt. When packaged in an EscalationReply with a
	// non-empty Hint, the hint is prepended to the next briefing's
	// prior_feedback.
	EscalationResume EscalationKind = iota + 1

	// EscalationForceApprove synthesizes done for every still-pending
	// task in the active sub-DAG, advances past the iteration, and
	// continues the run. The handler records a synthetic audit entry.
	EscalationForceApprove

	// EscalationSkip advances past the unapproved phase to the next
	// pending phase. The phase remains without an approved verdict; the
	// run cannot end with ExitDone if any phase was skipped.
	EscalationSkip

	// EscalationAbort terminates the run with ExitInvalid.
	EscalationAbort
)

// EscalationReply is the user's resolution of a DirectorEscalation
// pause. Kind selects the next action; Hint carries an optional
// free-form note the user attached to a Resume reply, propagated into
// the next iteration's briefing as prior feedback.
type EscalationReply struct {
	Kind EscalationKind
	Hint string
}

// DirectorPorts groups the Director-mode dependencies the Loop needs.
// When non-nil on Loop, the loop takes the Director path: outer loop on
// dag.HasPending; per-iteration the Briefer emits a Briefing through
// the run-wide MCP handler, the Executor consumes it and reports per-
// task progress through MCP, the Reviewer audits and reports per-task
// outcomes through MCP. The decider aggregates the resulting per-task
// DAG state to choose advance/retry/escalate/abort. Plan must already
// be confirmed (validated and persisted) by the caller; the loop does
// not re-plan.
type DirectorPorts struct {
	// Plan is the confirmed Plan to execute. Required.
	Plan *director.Plan

	// Briefer spawns the Briefer agent for one iteration. Required.
	// The agent emits the Briefing through bcc_briefing_emit; the loop
	// reads it back from Handler.Briefing(iterationID).
	Briefer director.Briefer

	// Reviewer spawns the Reviewer agent for one iteration. Required.
	// The agent reports per-task outcomes through bcc_task_approved /
	// bcc_task_needs_fix and a final bcc_review_finished; the loop
	// reads the resulting DAG state and review outcome from Handler.
	Reviewer director.Reviewer

	// Store persists per-session artifacts under .bcc/sessions/<id>/.
	// Required.
	Store *director.Store

	// NewExecutor builds a fresh Executor for one (phase, attempt) with
	// systemPromptFile pointing at the rendered Briefing prompt on
	// disk and args carrying the per-iteration scope (BriefingID,
	// PhaseID, SubDAG) the Executor's MCP calls will be checked
	// against. Required.
	NewExecutor func(systemPromptFile string, args dag.RegisterArgs) Executor

	// Handler is the run-wide MCP handler. The loop reads briefings,
	// per-task statuses, and review outcomes through it; the Briefer
	// and Reviewer adapters mutate it. Required for the Director path;
	// unit tests that drive only the decider can leave it nil.
	Handler *dag.Handler

	// Escalation, when non-nil, supplies the user's reply when the
	// decider returns Escalate. The loop blocks on a receive from this
	// channel after emitting DirectorEscalation. nil means "abort on
	// any escalation"; useful for headless runs.
	Escalation <-chan EscalationReply
}
