# Director iteration

The Director produced this briefing for one iteration's sub-DAG of tasks within a single phase; deliver every task end to end and report progress on the wire protocol carried in the system message.

## Iteration

- iteration_id: {{.IterationID}}
- phase_id: {{.PhaseID}}
- title: {{.Title}}
- intent: {{.Intent}}

## Scope

In:
{{- if .ScopeIn}}
{{range .ScopeIn}}- {{.}}
{{end}}
{{- else}}
- (no in-scope paths declared)
{{end}}
Out:
{{- if .ScopeOut}}
{{range .ScopeOut}}- {{.}}
{{end}}
{{- else}}
- (no out-of-scope paths declared)
{{end}}
## Tasks

{{if .Tasks -}}
{{range .Tasks}}### Task {{.ID}}: {{.Title}}

Intent: {{.Intent}}

Acceptance:
{{range .Acceptance}}- {{.ID}} ({{.Evidence}}): {{.Description}}
{{end}}
{{end}}
{{- else -}}
- (no tasks selected)
{{end}}
## Spec

Read the spec at: {{.SpecPath}} (use the `Read` tool; if the path is a directory, treat it as a spec bundle and read the entries that describe the work). The spec is the source of truth for any acceptance detail this briefing did not pin.
{{if .Instructions}}
## Instructions

{{.Instructions}}
{{end}}{{if .Hint}}
## User hint (escalation)

The user resumed this iteration via escalation and attached this hint. Treat it as guidance with higher priority than reviewer feedback.

{{.Hint}}
{{end}}{{if .PriorFeedback}}
## Prior feedback

{{.PriorFeedback}}
{{end}}
