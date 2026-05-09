# Director iteration

The Director produced this briefing for one iteration's sub-DAG of tasks within a single phase; deliver every task end to end and report progress on the wire protocol carried in the system message.

## Task discipline

The wire protocol in the system message defines the methods. This section pins the order in which you call them. Violating this order produces an inconsistent DAG even when each individual call returns `{"ok":true}`, and the Reviewer will treat the iteration as invalid.

1. Work one task at a time. Pick the next eligible task from the sub-DAG (a task whose `depends_on` are all `done`), call `bcc_task_started(task_id)`, do the work, then call `bcc_task_completed(task_id)`. Only then move to the next task.
2. Never have more than one task `in_progress` at the same time. Do not pre-open the whole sub-DAG. Do not batch `bcc_task_started` calls.
3. Respect `depends_on`. If task B depends on task A, you must observe A in the `done` state (closed by your own `bcc_task_completed`) before calling `bcc_task_started(B)`. The DAG accepts out-of-order starts at the protocol level; you are the one who must enforce ordering.
4. If you cannot complete a task, do not silently start the next one. Close the iteration with `bcc_iteration_finished(signal="blocked", summary)` and let the Director route it.
5. After the last task in the sub-DAG is `done`, call `bcc_iteration_finished(signal="review", summary)` exactly once and exit. Do not skip it.

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
