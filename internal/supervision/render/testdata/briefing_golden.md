# Iteration scope

Deliver every task in the sub-DAG end to end and report progress over the MCP methods defined in the system message.

## Task discipline

This section pins the order in which to call the wire methods. Violating it produces an inconsistent DAG even when each call returns `{"ok":true}`, and the iteration is treated as invalid.

1. Work one task at a time. Pick the next eligible task from the sub-DAG (a task whose `depends_on` are all `done`), call `task_started(task_id)`, do the work, then call `task_completed(task_id)`. Only then move to the next task.
2. Never have more than one task `in_progress` at the same time. Do not pre-open the whole sub-DAG. Do not batch `task_started` calls.
3. Respect `depends_on`. If task B depends on task A, observe A in the `done` state (closed by your own `task_completed`) before calling `task_started(B)`. The DAG accepts out-of-order starts at the protocol level; you enforce ordering.
4. If you cannot complete a task, do not silently start the next one. Close the iteration with `iteration_finished(signal="blocked", summary)`.
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

