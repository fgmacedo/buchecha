# Iteration scope

Deliver every task in the sub-DAG end to end and report progress over the MCP methods defined in the system message.

## Wire contract

You decide the order and how much to parallelize, as long as the rules below hold. The reviewer audits the phase-level diff against each task's acceptance criteria; the executor's pacing inside the phase does not matter to it.

1. For each task you advance, call `task_started(task_id)` before any tool_use that contributes to it, and `task_completed(task_id)` once the work the briefing asks for is on disk. Either call may share an assistant turn with other tool_use blocks.
2. Respect `depends_on`. If task B depends on task A, A must reach `done` before B's `task_started`. The handler rejects `task_started(B)` when any `depends_on` is not yet `done`.
3. Batch aggressively inside the phase. Independent `Read`/`Edit`/`Write` calls should share a single assistant turn. Defer expensive verification commands (build, lint, test, type-check, whatever the target project uses) to the end of each task or the end of the iteration, not after every edit.
4. If you cannot complete a task, do not call `task_completed` for it. Close with `iteration_finished(signal="blocked", summary)` and let the reviewer adjudicate.
5. After the last task in the sub-DAG is `done`, call `iteration_finished(signal="review", summary)` exactly once and exit. Do not skip it.

## Iteration

- iteration_id: p1-2
- phase_id: p1
- title: Phase one
- intent: Bootstrap the package layout and types.

## Scope

In:
- internal/foo/
- internal/foo/types.go

Out:
- internal/bar/

## Tasks

### Task t1: Add types

Intent: Define the new domain shape.

Acceptance:
- A1 (test): go test ./internal/foo/... is green
- A2 (diff): no import of internal/bar in foo


## Spec

Read the spec at: /tmp/spec.md (use the `Read` tool; if the path is a directory, treat it as a spec bundle and read the entries that describe the work). The spec is the source of truth for any acceptance detail this briefing did not pin.

## Instructions

Earlier phases delivered the spec parser. This phase wires the typed domain.

## Prior feedback

Attempt 1 left out the table-driven test for the parser. Required: add it.

