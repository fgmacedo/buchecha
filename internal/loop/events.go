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
// Briefing for (PhaseID, Attempt) and bcc has materialized the prompt
// to disk. The Executor is about to run.
type PhaseBriefed struct {
	PhaseID  string
	Attempt  int
	Briefing *director.Briefing
	At       time.Time
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
