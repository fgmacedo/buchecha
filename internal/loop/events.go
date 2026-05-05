package loop

import (
	"time"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// ExecResult is the result of one Executor.Run.
type ExecResult struct {
	// ExitCode is the agent's process exit code. 0 on success.
	ExitCode int
	// StderrTail is the last few KiB of the subprocess stderr, captured
	// internally by the adapter so callers can surface a diagnostic when
	// the agent exits non-zero. Empty on clean exits or when the adapter
	// chose not to capture.
	StderrTail string
	// AgentID is the per-spawn registry id assigned by the run boot. The
	// adapter does not populate this; the cli wrapper attaches it after
	// Run so the loop can name the agent in error messages without
	// reaching into the registry.
	AgentID string
	// StderrLogPath, when non-empty, is the absolute path the cli wrote
	// the subprocess stderr to (per-spawn capture file under
	// .bcc/sessions/<id>/runs/<iteration>/). Populated only when the
	// debug capture is enabled. The loop surfaces it in error messages
	// so users know where to look.
	StderrLogPath string
}

// Event is the union of loop-level events emitted on the loop's events
// channel. Implementations are tagged via the unexported isLoopEvent
// marker so consumers can switch over a closed set.
type Event interface{ isLoopEvent() }

// IterationStarted marks the beginning of one iteration. BaselineSHA is
// the HEAD SHA captured immediately before the executor runs; consumers
// (e.g., the TUI) treat the first iteration's value as the run-local
// baseline for counting commits made during the run.
type IterationStarted struct {
	Index       int
	MaxIter     int
	BaselineSHA string
	At          time.Time
}

func (IterationStarted) isLoopEvent() {}

// AgentEventReceived wraps a single AgentEvent forwarded from the
// executor onto the loop's events channel.
type AgentEventReceived struct {
	Event agentcontract.AgentEvent
}

func (AgentEventReceived) isLoopEvent() {}

// IterationFinished marks the end of one iteration with its outcome.
// Signal carries the value the agent emitted via the wire protocol's
// iteration_result event; bcc trusts it without parsing the spec.
type IterationFinished struct {
	Index        int
	Signal       agentcontract.Signal
	HEADAdvanced bool
	DurationMS   int64
	At           time.Time
}

func (IterationFinished) isLoopEvent() {}

// PhasePlanned is emitted once at the start of a Director-mode run
// after the Plan has been confirmed by the user. The Plan pointer is
// shared (read-only) with the consumer; the loop never mutates it.
type PhasePlanned struct {
	Plan *director.Plan
	At   time.Time
}

func (PhasePlanned) isLoopEvent() {}

// PhaseBriefed is emitted when the Director's Briefer returned a
// Briefing for (PhaseID, Iteration) and bcc has materialized the
// prompt to disk. The Executor is about to run.
//
// Iteration is the 1-based index of this brief→execute→review cycle
// within the phase. A phase may have multiple iterations when an
// earlier briefing covered only a subset of pending tasks, or when
// an escalation resumed the phase. It is not the executor retry
// counter; that lives on PhaseReviewed and DirectorEscalation.
//
// Capability fields surface the resolved per-role spawn parameters
// for the upcoming iteration: the model+effort each role will use
// after the Planner's phase-level assignments have been merged with
// the configured defaults. Empty Model/Effort means the corresponding
// flag is omitted from the spawn (the agent CLI uses its built-in
// default). BrieferSkipped is true when the Planner authored a
// PreparedBriefing and the Briefer agent was bypassed; ReviewSkipped
// is true when the Phase has SkipReview=true and the Reviewer agent
// will be bypassed.
type PhaseBriefed struct {
	PhaseID        string
	Iteration      int
	Briefing       *director.Briefing
	BrieferModel   string
	BrieferEffort  string
	ExecutorModel  string
	ExecutorEffort string
	ReviewerModel  string
	ReviewerEffort string
	BrieferSkipped bool
	ReviewSkipped  bool
	At             time.Time
}

func (PhaseBriefed) isLoopEvent() {}

// PhaseReviewed is emitted after the Director's Reviewer finished an
// audit. Outcome is the canonical wire value the Reviewer reported via
// bcc_review_finished ("approve", "revise", "escalate"); Reasoning
// carries the Reviewer's prose explanation. PhaseReviewed is now an
// informational summary derived from per-task DAG state plus the
// review outcome; the canonical per-task transitions are emitted as
// TaskApproved / TaskNeedsFix events.
type PhaseReviewed struct {
	PhaseID   string
	Attempt   int
	Outcome   string
	Reasoning string
	At        time.Time
}

func (PhaseReviewed) isLoopEvent() {}

// TaskStarted is emitted when an Executor or Planner reports a task
// transition to in_progress through bcc_task_started. The "planning"
// task uses the well-known PlanningTaskID.
type TaskStarted struct {
	PhaseID string
	TaskID  string
	At      time.Time
}

func (TaskStarted) isLoopEvent() {}

// TaskCompleted is emitted when the Executor (or Planner for the
// planning task) reports a task transition to done through
// bcc_task_completed.
type TaskCompleted struct {
	PhaseID string
	TaskID  string
	At      time.Time
}

func (TaskCompleted) isLoopEvent() {}

// TaskApproved is emitted when the Reviewer marks a task done through
// bcc_task_approved.
type TaskApproved struct {
	PhaseID string
	TaskID  string
	At      time.Time
}

func (TaskApproved) isLoopEvent() {}

// TaskNeedsFix is emitted when the Reviewer marks a task needs_fix
// through bcc_task_needs_fix; Note carries the Reviewer's per-task
// feedback when supplied.
type TaskNeedsFix struct {
	PhaseID string
	TaskID  string
	Note    string
	At      time.Time
}

func (TaskNeedsFix) isLoopEvent() {}

// DirectorEscalation is emitted when the decider returned Escalate. The
// loop pauses on the EscalationGate channel waiting for an
// EscalationReply (Resume / ForceApprove / Skip / Abort). Reasoning
// carries the Reviewer's explanation so the renderer can surface it to
// the user.
type DirectorEscalation struct {
	PhaseID   string
	Attempt   int
	Reasoning string
	At        time.Time
}

func (DirectorEscalation) isLoopEvent() {}

// LoopFinished marks the terminal state of the loop. Always the last
// event before the events channel is closed.
type LoopFinished struct {
	// Reason is a short human-readable cause (e.g., "spec done",
	// "max iterations", "blocked", "user cancelled", "fatal").
	Reason string

	// ExitCode mirrors the bash-compatible exit code in exitcodes.go.
	ExitCode int

	At time.Time
}

func (LoopFinished) isLoopEvent() {}
