// Package markdown_bcc is the format adapter for the bcc-markdown spec
// convention: an Implementation Plan with [x]/[ ] checkboxes and an
// Execution Journal section, both inside one Markdown file.
//
// The package implements loop.AgentBriefing by embedding contract.md
// (the format-specific contract) and composing it with the shared
// blocks from internal/loop/agentcontract (wire protocol, absolute
// restrictions, working tree invariants).
//
// bcc never reads the user's spec file; the contract instructs the
// agent to read it. This package owns the prompt that goes out.
// Wire-event parsing (the inbound side) lives in agentcontract because
// the wire protocol is canonical English regardless of format and has
// no per-format variation.
package markdown_bcc

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"text/template"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

//go:embed contract.md
var contractTemplate string

// contractT is the parsed contract template, composed with the shared
// agentcontract partials. Built once at init time.
var contractT = func() *template.Template {
	t := agentcontract.Partials()
	return template.Must(t.New("contract").Parse(contractTemplate))
}()

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

// Compile-time check that *Reader satisfies AgentBriefing.
var _ loop.AgentBriefing = (*Reader)(nil)

// templateData is the struct passed to the contract template at
// execution time. Keeps the template's field surface stable even if
// loop.BriefingInput changes.
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

// BuildPrompt renders the embedded contract.md template (composed with
// the shared agentcontract partials) for one agent invocation.
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
	if err := contractT.ExecuteTemplate(&buf, "contract", data); err != nil {
		return "", fmt.Errorf("render bcc-markdown contract: %w", err)
	}
	return buf.String(), nil
}
