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
type AgentEvent struct {
	Kind AgentEventKind
	At   time.Time

	Init  *InitInfo          // KindInit
	Tool  *ToolCallInfo      // KindToolUse, KindToolResult
	Text  string             // KindThinking, KindAssistantText
	Usage *UsageInfo         // KindAssistantText only: per-message token usage
	Rate  *RateLimitInfo     // KindRateLimit
	Done  *ResultSummaryInfo // KindResultSummary
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
