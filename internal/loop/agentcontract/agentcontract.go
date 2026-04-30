// Package agentcontract owns the format-agnostic surface of bcc's
// contract with any agent: the wire protocol (JSON Lines on stdout)
// and the cross-format markdown blocks that ship inside every spec
// adapter's prompt.
//
// Wire protocol code (Signal, BccEvent, ParseLine) and wire protocol
// documentation (wire_protocol.md) live together so they evolve in
// lockstep. Other format-neutral blocks (absolute_restrictions.md,
// working_tree.md) ship from here for the same reason: there is one
// canonical bcc-level statement of these rules, and every per-format
// adapter composes its prompt by including them via Go text/template
// partials.
//
// What is NOT here: per-format procedure, scope discipline, journal
// shape. Those live in the format adapter (e.g.,
// internal/format/markdown_bcc/contract.md), which composes the final
// prompt by extending Partials() with its own template.
package agentcontract

import (
	_ "embed"
	"encoding/json"
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

// BccEventKind tags the subset of bcc-protocol events the agent emits
// over stdout. Every kind has a defined wire shape (see ParseLine and
// wire_protocol.md).
type BccEventKind int

const (
	// BccEventUnknown is the zero value; consumers should ignore.
	BccEventUnknown BccEventKind = iota
	// BccEventTaskStarted marks the agent beginning work on a unit
	// (phase, task, etc.). BccEvent.ID identifies the unit.
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

// BccEvent is a normalized agent-emitted progress event. The loop and
// the TUI consume it without knowing the source spec format; the wire
// protocol is canonical, see ParseLine.
type BccEvent struct {
	Kind    BccEventKind
	ID      string
	Signal  Signal // populated only for BccEventIterationResult
	Summary string
	// Raw preserves the original JSON object for diagnostics or
	// future extensions.
	Raw map[string]any
}

// wireEvent is the on-the-wire JSON shape the agent emits.
type wireEvent struct {
	Type    string `json:"type"`
	Event   string `json:"event"`
	ID      string `json:"id,omitempty"`
	Value   string `json:"value,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// ParseLine inspects a single JSONL line from the executor's stdout.
// Returns ok=true when the line is a recognized bcc_event sentinel;
// ok=false for anything else (the executor falls through to its
// existing handling).
//
// The wire protocol is format-agnostic: every spec format emits the
// same JSON shape, so this function is canonical and shared across
// adapters.
func ParseLine(line []byte) (BccEvent, bool) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return BccEvent{}, false
	}
	t, _ := raw["type"].(string)
	if t != "bcc_event" {
		return BccEvent{}, false
	}
	var w wireEvent
	if err := json.Unmarshal(line, &w); err != nil {
		return BccEvent{}, false
	}
	out := BccEvent{
		ID:      w.ID,
		Summary: w.Summary,
		Raw:     raw,
	}
	switch w.Event {
	case "task_started":
		out.Kind = BccEventTaskStarted
	case "task_completed":
		out.Kind = BccEventTaskCompleted
	case "iteration_result":
		out.Kind = BccEventIterationResult
		out.Signal = parseSignal(w.Value)
	case "progress_tick":
		out.Kind = BccEventProgressTick
	default:
		return BccEvent{}, false
	}
	return out, true
}

func parseSignal(v string) Signal {
	switch v {
	case "continue":
		return SignalContinue
	case "review":
		return SignalReview
	case "done":
		return SignalDone
	case "blocked":
		return SignalBlocked
	default:
		return SignalUnknown
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
