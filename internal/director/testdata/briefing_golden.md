# Director iteration

The Director produced this briefing for one iteration's sub-DAG of tasks within a single phase; deliver every task end to end and report progress on the wire protocol carried in the system message.

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

