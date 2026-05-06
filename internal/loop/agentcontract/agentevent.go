package agentcontract

import "time"

// AgentEventKind classifies a normalized agent event. Adapters translate
// their native formats (e.g., Claude Code's stream-json subtypes) to one
// of these kinds before sending on the AgentEvent channel.
type AgentEventKind string

const (
	KindInit          AgentEventKind = "init"
	KindThinking      AgentEventKind = "thinking"
	KindToolUse       AgentEventKind = "tool_use"
	KindToolResult    AgentEventKind = "tool_result"
	KindAssistantText AgentEventKind = "assistant_text"
	KindRateLimit     AgentEventKind = "rate_limit"
	KindResultSummary AgentEventKind = "result_summary"
)

// AgentEvent is a normalized event emitted by an Executor or Director
// adapter. Only the field(s) relevant to the Kind are populated; the
// rest are zero. The same envelope shape is used regardless of which
// agent role (executor, planner, briefer, reviewer) produced the event.
//
// Origin fields (AgentID, Role, PhaseID, IterationID, Attempt) identify
// which agent produced the event; adapters populate them before sending
// on the events channel so downstream consumers (TUI, SSE, persistence)
// can correlate stream-json output with the agent that emitted it. They
// are required to support concurrent agents in the future. The empty
// value remains acceptable for legacy callers and tests.
//
// TaskID is left empty by adapters and is populated by EventService at
// fan-out time, derived from the active-task index keyed by AgentID. See
// internal/services/events.go for the attribution mechanism.
type AgentEvent struct {
	Kind AgentEventKind
	At   time.Time

	// Origin: who emitted this event.
	AgentID     string
	Role        Role
	PhaseID     string // empty for planner
	IterationID string // empty for planner
	Attempt     int    // 0 for planner, >=1 otherwise
	TaskID      string // populated by EventService.fanout, not by adapters

	Init  *InitInfo          // KindInit
	Tool  *ToolCallInfo      // KindToolUse, KindToolResult
	Text  string             // KindThinking, KindAssistantText
	Usage *UsageInfo         // KindAssistantText only: per-message token usage
	Rate  *RateLimitInfo     // KindRateLimit
	Done  *ResultSummaryInfo // KindResultSummary
}

// WithOrigin returns a copy of ev with the origin fields (AgentID,
// Role, PhaseID, IterationID, Attempt) set. Adapters call this on each
// parsed AgentEvent before forwarding on the events channel so
// downstream consumers can correlate the event with the agent that
// produced it.
//
// TaskID is intentionally not set here; it is populated later by the
// EventService at fan-out time, derived from the active-task index
// keyed by AgentID.
func (ev AgentEvent) WithOrigin(agentID string, role Role, phaseID, iterationID string, attempt int) AgentEvent {
	ev.AgentID = agentID
	ev.Role = role
	ev.PhaseID = phaseID
	ev.IterationID = iterationID
	ev.Attempt = attempt
	return ev
}

// UsageInfo carries per-message token usage attached to assistant text
// events. The four fields sum to the total billable tokens for that
// message. Cache fields are zero when caching was not involved.
type UsageInfo struct {
	InputTokens              int64
	OutputTokens             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
}

// InitInfo carries data from a session-init event.
type InitInfo struct {
	SessionID string
	Model     string
	CWD       string
}

// ToolCallInfo is shared by tool_use and tool_result events. Args is the
// raw tool input on tool_use; IsError and Summary apply on tool_result.
type ToolCallInfo struct {
	ID      string
	Name    string
	Args    map[string]any
	IsError bool
	Summary string
}

// RateLimitInfo carries a rate-limit notification from the agent.
type RateLimitInfo struct {
	Status  string
	ResetAt time.Time
}

// ResultSummaryInfo carries the agent's per-iteration cost and usage
// totals (last event of an iteration in stream-json). The four token
// fields sum to the total billable token count for the iteration.
type ResultSummaryInfo struct {
	NumTurns                 int
	TotalCostUSD             float64
	InputTokens              int64
	OutputTokens             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
	DurationMS               int64
}
