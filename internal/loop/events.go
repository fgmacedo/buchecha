package loop

import (
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/spec"
)

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
	// KindBccEvent carries a parsed bcc_event sentinel from the agent's
	// stdout (canonical wire protocol; see internal/loop/agentcontract).
	KindBccEvent AgentEventKind = "bcc_event"
)

// AgentEvent is a normalized event emitted by an Executor adapter. Only
// the field(s) relevant to the Kind are populated; the rest are zero.
type AgentEvent struct {
	Kind AgentEventKind
	At   time.Time

	Init  *InitInfo               // KindInit
	Tool  *ToolCallInfo           // KindToolUse, KindToolResult
	Text  string                  // KindThinking, KindAssistantText
	Usage *UsageInfo              // KindAssistantText only: per-message token usage
	Rate  *RateLimitInfo          // KindRateLimit
	Done  *ResultSummaryInfo      // KindResultSummary
	Bcc   *agentcontract.BccEvent // KindBccEvent
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
	Event AgentEvent
}

func (AgentEventReceived) isLoopEvent() {}

// IterationFinished marks the end of one iteration with its outcome.
type IterationFinished struct {
	Index        int
	Result       spec.Result
	HEADAdvanced bool
	NewlyChecked int
	DurationMS   int64
	At           time.Time
}

func (IterationFinished) isLoopEvent() {}

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
