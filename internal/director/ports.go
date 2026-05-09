package director

import (
	"context"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// Planner produces a Plan from a spec snapshot. Adapters implement this
// against a concrete agent (Claude, future Codex/Gemini); the consumer
// (cmd/cli wiring) holds the porter and the loop receives only the
// resulting Plan.
//
// events, when non-nil, receives the agent's stream telemetry
// (thinking, tool_use, tool_result, assistant_text, result_summary,
// rate_limit). Adapters that cannot stream are free to ignore it.
// Adapters never close events; the caller owns it.
type Planner interface {
	Plan(ctx context.Context, in PlannerInput, events chan<- agentcontract.AgentEvent) (*Plan, *DirectorCallStats, error)
}

// Briefer spawns the Briefer agent for a single iteration. The agent
// emits its Briefing through the MCP method bcc_briefing_emit; the
// loop reads it back from the handler. Brief returns when the agent
// process has exited cleanly or with an error, plus telemetry for the
// cost panel.
//
// events behaves the same as on Planner.Plan.
type Briefer interface {
	Brief(ctx context.Context, in BrieferInput, events chan<- agentcontract.AgentEvent) (*DirectorCallStats, error)
}

// Reviewer spawns the Reviewer agent for one (phase, sub-DAG) pair. The
// agent reports per-task outcomes through bcc_task_approved and
// bcc_task_needs_fix, finalising with bcc_review_finished. Review
// returns when the agent process has exited; the loop reads the
// resulting DAG state and review outcome from the handler.
//
// events behaves the same as on Planner.Plan.
type Reviewer interface {
	Review(ctx context.Context, in ReviewerInput, events chan<- agentcontract.AgentEvent) (*DirectorCallStats, error)
}

// PlannerInput is the request payload for Planner.Plan. The Planner
// reads the spec via the Read tool using SpecPath; SpecHash is the
// sha256 of the current spec content, recorded on the emitted Plan so
// downstream resume can detect divergence. AgentID is the opaque id
// the run-wide registry assigned for this spawn; the adapter embeds it
// in the prompt so the agent passes it back on every MCP call.
//
// Registry is the merged set of models the Planner may pick from when
// it attributes per-phase model+effort to each role. Empty Registry
// means the Planner has nothing to choose between; it should still
// emit a Plan, leaving every assignment unset, and the loop falls back
// to configured defaults.
type PlannerInput struct {
	AgentID  string
	SpecPath string
	SpecHash string
	Registry CapabilityRegistry
	// Prompt is the free-form user directive supplied via `bcc run --prompt`.
	// When both Prompt and the spec are set, the Planner treats Prompt as a
	// lens over the spec; when only Prompt is set, it is the source of
	// truth for what to plan. Empty when the user supplied no --prompt.
	Prompt string
	// Assignment is the (provider, model, effort) triple the Planner
	// itself runs under, resolved by the loop from the user-configured
	// Planner menu (config.Roles.Planner.Options[0] after host filter).
	// Required: the adapter never invents a model.
	Assignment RoleAssignment
	// Menus is the per-role cardápio rendered into the Planner prompt
	// so the agent picks per-phase assignments only from declared,
	// host-available options. The Planner's own role is not in here
	// (it cannot reroute itself).
	Menus RoleMenus
}

// BrieferInput is the request payload for Briefer.Brief. IterationID
// is the loop-assigned id the Briefer echoes back into the emitted
// Briefing; it has the form "<phase_id>-<NN>" where NN is the 1-based
// iteration index within the phase. SubDAGTaskIDs lists the tasks
// within PhaseID this iteration targets, drawn from the phase's
// pending or needs_fix tasks. PriorFeedback, when non-empty, carries
// the user's escalation hint or the prior iteration's per-task
// feedback the Briefer prepends to the next iteration's instructions;
// its presence is also the signal that this is a follow-up iteration.
// The Briefer reads the spec itself via the Read tool using SpecPath;
// bcc never inlines the spec body. AgentID is the per-spawn registry
// id.
type BrieferInput struct {
	AgentID       string
	Plan          *Plan
	SpecPath      string
	IterationID   string
	PhaseID       string
	SubDAGTaskIDs []string
	PriorFeedback string
	// Assignment, when non-nil, overrides the Briefer adapter's
	// configured model and effort for this single call. The Planner
	// emits it on the Phase via briefer_assignment; the loop forwards
	// it here. Empty fields fall back to the configured defaults.
	Assignment *RoleAssignment
	// Attempt is the 1-based phase iteration counter for this brief.
	// Populated by the loop; adapters forward it into SpawnStarted.
	Attempt int
}

// ReviewerInput is the request payload for Reviewer.Review. The
// Reviewer reads the briefing, per-task ids, and the diff/journal
// snapshots through MCP queries (bcc_get_briefing, bcc_get_baseline,
// bcc_get_journal_delta) once the loop has captured them on the
// handler; bcc no longer pre-collects acceptance evidence on the wire.
// IterationID identifies the briefing the Reviewer audits; AgentID is
// the per-spawn registry id.
type ReviewerInput struct {
	AgentID     string
	IterationID string
	PhaseID     string
	SubDAG      []string
	// Assignment, when non-nil, overrides the Reviewer adapter's
	// configured model and effort for this single call. Same semantics
	// as BrieferInput.Assignment.
	Assignment *RoleAssignment
	// Attempt is the 1-based executor attempt number for this review.
	// Populated by the loop; adapters forward it into SpawnStarted.
	Attempt int
}

// DirectorCallStats reports the cost and shape of a single Director
// agent invocation. The TUI accumulates these per role for the cost
// panel; the loop uses them only for telemetry.
type DirectorCallStats struct {
	DurationMS   int64
	CostUSD      float64
	InputTokens  int64
	OutputTokens int64
}
