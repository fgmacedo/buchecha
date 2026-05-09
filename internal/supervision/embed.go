package supervision

import (
	"embed"
	_ "embed"
)

//go:embed schemas/mcp/*.json
var mcpSchemaFS embed.FS

// MCPSchemaFS returns the embedded filesystem rooted at
// internal/supervision/schemas/mcp/, so adapters that consume the per-method
// schemas (notably the dag handler) can read them by their canonical
// path without duplicating the //go:embed directive.
func MCPSchemaFS() embed.FS { return mcpSchemaFS }

//go:embed prompts/plan.md
var planPromptMD string

//go:embed prompts/brief.md
var briefPromptMD string

//go:embed prompts/review.md
var reviewPromptMD string

//go:embed prompts/briefing.md
var briefingPromptMD string

//go:embed prompts/briefing_system.md
var briefingSystemMD string

//go:embed schemas/plan.schema.json
var planSchemaJSON []byte

//go:embed schemas/briefing.schema.json
var briefingSchemaJSON []byte

//go:embed schemas/verdict.schema.json
var verdictSchemaJSON []byte

// PlanPromptTemplate returns the raw text/template source for the
// Planner's system prompt. Composing it into a final string requires the
// agentcontract partials (see agentcontract.Partials).
func PlanPromptTemplate() string { return planPromptMD }

// BriefPromptTemplate returns the raw text/template source for the
// Briefer's system prompt.
func BriefPromptTemplate() string { return briefPromptMD }

// ReviewPromptTemplate returns the raw text/template source for the
// Reviewer's system prompt.
func ReviewPromptTemplate() string { return reviewPromptMD }

// PlanSchema returns the JSON Schema bytes for the Planner's output.
func PlanSchema() []byte { return planSchemaJSON }

// BriefingSchema returns the JSON Schema bytes for the Briefer's output.
func BriefingSchema() []byte { return briefingSchemaJSON }

// VerdictSchema returns the JSON Schema bytes for the Reviewer's output.
func VerdictSchema() []byte { return verdictSchemaJSON }
