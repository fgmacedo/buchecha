// Package agentcontract owns the format-agnostic surface of bcc's
// contract with any agent: the canonical Signal alphabet that closes
// every iteration, the normalized stream-telemetry envelope every
// executor / director adapter produces, and the cross-role markdown
// blocks that ship inside every prompt bcc renders.
//
// Signal (continue / review / done / blocked) is the wire vocabulary
// the loop and the TUI consume regardless of which role (executor,
// planner, briefer, reviewer) emitted it. AgentEvent and friends
// (AgentEventKind, InitInfo, ToolCallInfo, UsageInfo, RateLimitInfo,
// ResultSummaryInfo) define the shape adapters use to forward
// streaming agent activity. The format-neutral markdown blocks
// (wire_protocol.md, absolute_restrictions.md, working_tree.md) ship
// from here so there is one canonical bcc-level statement of these
// rules; every per-role prompt under internal/director/prompts/
// composes its template by extending Partials().
//
// What is NOT here: the MCP transport itself (HTTP server, mcp-config
// generation, name prefixing), which lives under internal/mcp and the
// executor adapter; the per-vendor stream-json parser, which lives
// under the relevant adapter (e.g. internal/executor/claude/streamjson);
// the MCP method dispatch table that translates bcc_* method calls into
// DAG mutations, which lives in internal/director/dag.
package agentcontract

import (
	_ "embed"
	"text/template"
)

// MCPServerName is the connection name registered for bcc's run-wide
// MCP server in the per-spawn mcp-config. Claude's MCP transport
// prefixes every tool name with `mcp__<connection>__` on the agent
// side, so any tool_use whose tool.name starts with MCPToolNamePrefix
// came from a call to a bcc MCP method. Single source of truth for
// both wire emission (executor/director adapters that name the
// connection) and observability filters (UI, logs) that recognise bcc
// protocol traffic without hard-coding the literal.
const MCPServerName = "bcc"

// MCPToolNamePrefix is the wire prefix every bcc MCP tool call carries
// in the agent's tool_use stream (e.g. `mcp__bcc__bcc_task_started`).
// Mirrored in the SPA at internal/webui/web/src/lib/mcp.ts; both must
// stay in lockstep with this constant.
const MCPToolNamePrefix = "mcp__" + MCPServerName + "__"

// Signal is the decision-relevant outcome of an iteration as reported
// by the agent over the wire protocol. The values are canonical
// English; format adapters localize human-facing artifacts (journal
// text, commits) but never the wire.
type Signal int

const (
	// SignalUnknown is the zero value: no iteration_result observed,
	// or the value did not match any known signal.
	SignalUnknown Signal = iota
	// SignalContinue: the iteration produced normal progress; the
	// loop should run another iteration.
	SignalContinue
	// SignalReview: an observer-driven gate was reached; the loop
	// should stop and wait for the user to edit and re-trigger.
	SignalReview
	// SignalDone: every pending work unit is complete; the loop
	// should terminate successfully.
	SignalDone
	// SignalBlocked: unrecoverable failure; the loop should stop
	// with a non-zero exit code.
	SignalBlocked
)

// String returns a stable lower-case label for the signal, matching
// the wire protocol value.
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

//go:embed wire_protocol.md
var wireProtocolMD string

//go:embed absolute_restrictions.md
var absoluteRestrictionsMD string

//go:embed working_tree.md
var workingTreeMD string

//go:embed what_bcc_is.md
var whatBccIsMD string

// Partials returns a *template.Template containing the format-neutral
// markdown blocks every agent contract should compose. Format adapters
// extend this template with their own definitions and invoke the
// partials via:
//
//	{{template "wire_protocol" .}}
//	{{template "absolute_restrictions" .}}
//	{{template "working_tree" .}}
//	{{template "what_bcc_is" .}}
//
// Partials are body-only (no heading) except `what_bcc_is`, which
// includes its own `## What bcc is` heading because every per-role
// prompt opens with it. The view passed when invoking `what_bcc_is`
// must expose a `.Role` field set to one of "planner", "briefer",
// "executor", "reviewer"; the partial highlights the matching bullet
// with "(you)".
func Partials() *template.Template {
	t := template.New("partials")
	template.Must(t.New("wire_protocol").Parse(wireProtocolMD))
	template.Must(t.New("absolute_restrictions").Parse(absoluteRestrictionsMD))
	template.Must(t.New("working_tree").Parse(workingTreeMD))
	template.Must(t.New("what_bcc_is").Parse(whatBccIsMD))
	return t
}
