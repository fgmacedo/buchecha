// Package markdown_bcc is the format adapter for the bcc-markdown spec
// convention: an Implementation Plan with [x]/[ ] checkboxes and an
// Execution Journal section, both inside one Markdown file.
//
// The package implements two ports from internal/loop:
//
//   - AgentBriefing: BuildPrompt renders the embedded contract.md
//     template with the active config and per-iteration BriefingInput.
//   - AgentEvents: ParseLine recognizes the bcc_event JSONL sentinels
//     emitted by the agent and normalizes them to loop.BccEvent.
//
// bcc never reads the user's spec file; the contract instructs the
// agent to read it. This package owns the prompt that goes out and the
// events that come back. Nothing else.
package markdown_bcc

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/fgmacedo/buchecha/internal/loop"
)

//go:embed contract.md
var contractTemplate string

var contractT = template.Must(template.New("contract").Parse(contractTemplate))

// Config carries the per-format options the adapter consumes when
// rendering the contract template. Sourced from [spec.markdown_bcc] in
// .bcc.toml; the cmd boundary translates Config values from the
// loaded *config.Config.
//
// All fields are template inputs to the agent's contract: bcc does not
// parse the spec, so localizing PlanHeading or JournalHeading only
// changes what the agent is told to write. The wire protocol stays
// canonical English regardless.
type Config struct {
	// PlanHeading is the H2 heading that introduces the plan in the
	// spec, including the "## " prefix. Defaults to "## Implementation
	// Plan" when empty.
	PlanHeading string

	// JournalHeading is the H2 heading that introduces the journal,
	// with the "## " prefix. Defaults to "## Execution Journal" when
	// empty.
	JournalHeading string

	// JournalStore is the [journal].store value: markdown_inspec, file,
	// or none. Drives which journal-writing fragment the contract
	// template renders.
	JournalStore string

	// JournalPath is the [journal.file].path value, used only when
	// JournalStore == "file".
	JournalPath string

	// MaxIterations is the loop's iteration cap; rendered into the
	// contract so the agent can self-check.
	MaxIterations int
}

// Reader is the bcc-markdown format adapter. Construct with New.
type Reader struct {
	cfg Config
}

// New returns a Reader configured from cfg. Empty fields fall back to
// the documented defaults.
func New(cfg Config) *Reader {
	if cfg.PlanHeading == "" {
		cfg.PlanHeading = "## Implementation Plan"
	}
	if cfg.JournalHeading == "" {
		cfg.JournalHeading = "## Execution Journal"
	}
	if cfg.JournalStore == "" {
		cfg.JournalStore = "markdown_inspec"
	}
	return &Reader{cfg: cfg}
}

// Compile-time checks that *Reader satisfies both ports.
var (
	_ loop.AgentBriefing = (*Reader)(nil)
	_ loop.AgentEvents   = (*Reader)(nil)
)

// templateData is the struct passed to contract.md template execution.
// Keeps the template fields stable even if loop.BriefingInput changes.
type templateData struct {
	SpecPath       string
	Iteration      int
	MaxIterations  int
	Mode           string
	Extra          string
	PlanHeading    string
	JournalHeading string
	JournalStore   string
	JournalPath    string
}

// BuildPrompt renders the embedded contract.md template with the
// active config and per-iteration BriefingInput.
func (r *Reader) BuildPrompt(_ context.Context, in loop.BriefingInput) (string, error) {
	mode := "loop"
	if in.Mode == loop.ModeSingleShot {
		mode = "single-shot"
	}
	data := templateData{
		SpecPath:       in.SpecPath,
		Iteration:      in.Iteration,
		MaxIterations:  r.cfg.MaxIterations,
		Mode:           mode,
		Extra:          in.Extra,
		PlanHeading:    r.cfg.PlanHeading,
		JournalHeading: r.cfg.JournalHeading,
		JournalStore:   r.cfg.JournalStore,
		JournalPath:    r.cfg.JournalPath,
	}
	var buf bytes.Buffer
	if err := contractT.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render bcc-markdown contract: %w", err)
	}
	return buf.String(), nil
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
func (r *Reader) ParseLine(line []byte) (loop.BccEvent, bool) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return loop.BccEvent{}, false
	}
	t, _ := raw["type"].(string)
	if t != "bcc_event" {
		return loop.BccEvent{}, false
	}
	var w wireEvent
	if err := json.Unmarshal(line, &w); err != nil {
		return loop.BccEvent{}, false
	}
	out := loop.BccEvent{
		ID:      w.ID,
		Summary: w.Summary,
		Raw:     raw,
	}
	switch w.Event {
	case "task_started":
		out.Kind = loop.BccEventTaskStarted
	case "task_completed":
		out.Kind = loop.BccEventTaskCompleted
	case "iteration_result":
		out.Kind = loop.BccEventIterationResult
		out.Signal = parseSignal(w.Value)
	case "progress_tick":
		out.Kind = loop.BccEventProgressTick
	default:
		return loop.BccEvent{}, false
	}
	return out, true
}

func parseSignal(v string) loop.Signal {
	switch v {
	case "continue":
		return loop.SignalContinue
	case "review":
		return loop.SignalReview
	case "done":
		return loop.SignalDone
	case "blocked":
		return loop.SignalBlocked
	default:
		return loop.SignalUnknown
	}
}
