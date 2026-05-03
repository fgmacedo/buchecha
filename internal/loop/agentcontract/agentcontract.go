// Package agentcontract owns the format-agnostic surface of bcc's
// contract with any agent: the canonical Signal alphabet that closes
// every iteration, the normalized stream-telemetry envelope every
// executor / director adapter produces, and the cross-format markdown
// blocks that ship inside every spec adapter's prompt.
//
// Signal (continue / review / done / blocked) is the wire vocabulary
// the loop and the TUI consume regardless of which role (executor,
// planner, briefer, reviewer) emitted it. AgentEvent and friends
// (AgentEventKind, InitInfo, ToolCallInfo, UsageInfo, RateLimitInfo,
// ResultSummaryInfo) define the shape adapters use to forward
// streaming agent activity. Other format-neutral blocks
// (wire_protocol.md, absolute_restrictions.md, working_tree.md) ship
// from here so there is one canonical bcc-level statement of these
// rules, and every per-format adapter composes its prompt by including
// them via Go text/template partials.
//
// What is NOT here: per-format procedure, scope discipline, journal
// shape. Those live in the format adapter (e.g.,
// internal/format/markdown_bcc/contract.md), which composes the final
// prompt by extending Partials() with its own template. The MCP
// transport itself (HTTP server, mcp-config generation, name
// prefixing) lives in the executor adapter (internal/executor/<flavor>
// + internal/mcp); this package only fixes the abstract events. The
// per-vendor stream-json parser likewise lives under the relevant
// adapter (e.g. internal/executor/claude/streamjson). The MCP method
// dispatch table that translates bcc_* method calls into DAG mutations
// lives in internal/director/dag.
package agentcontract

import (
	_ "embed"
	"text/template"
)

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

// Partials returns a *template.Template containing the format-neutral
// markdown blocks every agent contract should compose. Format adapters
// extend this template with their own definitions and invoke the
// partials via:
//
//	{{template "wire_protocol" .}}
//	{{template "absolute_restrictions" .}}
//	{{template "working_tree" .}}
//
// Partials are body-only (no heading); the parent template provides
// the heading at whatever level fits.
func Partials() *template.Template {
	t := template.New("partials")
	template.Must(t.New("wire_protocol").Parse(wireProtocolMD))
	template.Must(t.New("absolute_restrictions").Parse(absoluteRestrictionsMD))
	template.Must(t.New("working_tree").Parse(workingTreeMD))
	return t
}
