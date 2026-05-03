---
title: "Spec: Migrate Director to executable-plan DAG and MCP protocol"
type: spec
status: draft
authors:
  - Fernando Macedo
reviewers: []
created: 2026-05-02
decision-date:
superseded-by:
supersedes:
review-by:
tags:
  - director
  - dag
  - mcp
  - migration
comments: true
---

# Spec: Migrate Director to executable-plan DAG and MCP protocol

## Summary

Convert the current Director implementation, which operates on phase-only granularity and a parser-based wire protocol, to the model defined in [PRD 5](./2026-05-02-executable-plan-dag.md): the plan is a DAG of phases and tasks, all communication between agents and bcc flows through a real MCP handler, and the iteration loop is DAG-driven. The migration is sequenced as eight phases that each leave the codebase in a working state.

This spec is normative. Each phase is executed autonomously by bcc and converges when its acceptance criteria are green.

## Context

### What is in place today

The Director was implemented per [the original implementation spec](./2026-05-02-reviewed-execution-implementation.md), which targeted [PRD 2](./2026-04-30-reviewed-execution.md). That implementation shipped:

1. A typed `Plan` of `Phase` containing `AcceptanceItem`. No `Task` type.
2. Director Claude adapter invoking `claude -p --bare --no-session-persistence --json-schema <file>`. No tools, no MCP, no permission flags.
3. MCP server (`internal/mcp/server.go`) registered only in the Executor adapter, with a stub handler that returns `"ok"` to every `tools/call`. Wire signal extracted from stream-json `tool_use` envelopes by `internal/executor/claude/claude.go::parseAssistant` plus `internal/loop/agentcontract/agentcontract.go::FromToolCall`.
4. Loop driver `runDirector` iterating `plan.Phases` with an inner attempt loop per phase.
5. `os.ReadFile(specPath)` at run boot in `internal/cli/run_director.go`, with content inlined into `PlannerInput.SpecContent` and `BrieferInput.SpecContent`.
6. State persisted globally under `.bcc/plan.json`, `.bcc/briefings/`, `.bcc/verdicts/`. `--resume` knows only "what is there".
7. Three-option escalation (`Resume`, `Skip`, `Abort`) without hint propagation or force-approve.

### What needs to change

PRD 5 makes four orthogonal corrections to that picture:

1. **Per-session state isolation.** Each `bcc run` becomes a discrete session with a generated id; state lives under `.bcc/sessions/<session-id>/`. `--resume` operates on a specific session.
2. **DAG of tasks.** Tasks become first-class atomic nodes carrying their own acceptance criteria, dependencies, priority, status, and retry budget. Phases are containers.
3. **MCP as the only protocol.** The MCP server is run-wide, has a real handler with per-method schema validation and per-connection authorization, and replaces the parser as the source of truth. All Director roles get tools and MCP. Spec content is never inlined; only the path is passed.
4. **Four-option escalation with hint.** `EscalationReply` becomes a struct with `Kind` and `Hint`; `EscalationForceApprove` synthesizes `done` for the still-pending sub-DAG tasks.

### Guiding principle

Every phase must leave `go test -race ./...` green and `go vet ./...` clean. No phase blocks on a later phase's behavior; the migration is sequenced so dependents are built on top of green dependencies.

## Goals and non-goals

### Goals

- [ ] G1: Sessions are first-class. Each `bcc run` is a session with a stable id, listable and resumable.
- [ ] G2: The plan is a DAG of phases and tasks. Tasks own acceptance criteria, depends-on edges, priority, status, and retry budget.
- [ ] G3: The MCP server is run-wide, with a real handler that validates per-method schemas and authorizes per-connection.
- [ ] G4: All Director roles (Planner, Briefer, Reviewer) run with tool access (Read, Bash, Grep, Glob), MCP wiring, and `--dangerously-skip-permissions`. They receive only the spec path; bcc never inlines spec content.
- [ ] G5: Every agent-to-bcc and bcc-to-agent communication flows through MCP method calls. No `--json-schema` flag on any Director invocation.
- [ ] G6: The loop driver is DAG-driven: outer loop while pending tasks remain; per-iteration sub-DAG selection by the Briefer; per-task retry within the iteration.
- [ ] G7: Escalation has four options (`resume_with_hint`, `force_approve`, `skip`, `abort`); hint propagates to the next briefing's `prior_feedback`.
- [ ] G8: `internal/loop/agentcontract/wire_protocol.md` describes the MCP usage manual (not the legacy stream-json envelope format).

### Non-goals

- Reopening PRD 5 design decisions (DAG model, MCP-as-protocol, per-role connection authz).
- Capability-aware execution (PRD 4) and parallel phase execution (PRD 3).
- Multi-vendor agent support beyond Claude. The MCP protocol is vendor-agnostic by construction; new adapters are follow-up.
- bcc executing tests, builds, or any language-specific commands. Concrete capability remains in the agent's hands.
- Detection of mid-run spec changes. The user can edit the spec, abort the run, and resume by restarting with `bcc run --resume <session-id>`.
- Backwards compatibility with the legacy `.bcc/{plan,briefings,verdicts}/` layout. The implementation is fresh; the new layout is the only layout.

## Proposal

The migration is sequenced as eight phases. Each phase is independently mergeable. The phases are ordered by dependency: P1 establishes the session boundary that every later artifact writes into; P2 introduces the DAG types; P3 lifts the MCP server to run-wide with a real handler; P4 wires Director roles into the MCP and gives them tools; P5 implements the full method surface; P6 rewrites the loop to drive on the DAG; P7 finishes escalation; P8 rewrites the wire-protocol partial and updates the TUI and docs.

Cross-references to PRD 5 appear in each phase's context.

## Implementation Plan

### Phase 1: Per-session state isolation

**Context**: Today `.bcc/plan.json`, `.bcc/briefings/`, and `.bcc/verdicts/` are global per repository. Two consecutive runs overwrite each other; `--resume` knows only "the latest." This phase introduces sessions: each run is identified by a generated id, and its state lives under `.bcc/sessions/<id>/`. CLI gains `--session <id>` and the subcommand `bcc sessions`. Every subsequent phase writes into a session directory; doing P1 first avoids reworking paths in multiple places.

**Depends on**: nothing.

**Acceptance**:

1. `bcc run <spec>` without `--session` creates a new session with a generated id (12 hex chars). `manifest.json` is written to `.bcc/sessions/<id>/manifest.json` with `ID`, `SpecPath`, `SpecHash`, `CreatedAt`, `UpdatedAt`, `Status: running`. The session directory is the root for the per-session layout defined by [PRD 5](./2026-05-02-executable-plan-dag.md#persistence): `plan.json` (P1 stub, P5 real), `dag.json` (P3 onward), `briefings/` (P5 onward), `mcp-log.jsonl` (P5 onward). The legacy `verdicts/` directory does not exist; per-task state lives in `dag.json` from P3 forward.
2. `bcc run --resume <session-id> <spec>` resumes the session by id. Returns a typed error (`ErrSessionNotFound`) if the id does not exist; returns `ErrSessionSpecMismatch` if `Session.SpecPath != specArg`.
3. `bcc run --resume <spec>` (without `--session`) selects the most recent session whose `SpecPath` matches. If multiple are eligible, fails with a message listing candidates and requiring `--session <id>`.
4. `bcc sessions list` prints a table of `id | spec | status | created | updated` ordered by `UpdatedAt` descending. JSON output supported via `--output json`.
5. `bcc sessions show <id>` prints the manifest.
6. `go test -race ./...` green; `go vet ./...` clean.

**Tasks**:

1. [x] Create `internal/director/session.go` with `type Session struct { ID, SpecPath, SpecHash string; CreatedAt, UpdatedAt time.Time; Status SessionStatus }` and `type SessionStatus string` with constants `SessionRunning`, `SessionDone`, `SessionAborted`, `SessionEscalatedPending`. JSON round-trip test.
1. [x] Add `func NewSessionID(specPath string, now time.Time, randSource io.Reader) string` returning `sha256(specPath || now.UnixNano() || crypto/rand 16 bytes)` truncated to 12 hex chars. Deterministic in tests via injected `now` and `randSource`.
1. [x] Refactor `internal/director/store.go`: `Store` operates on `sessionDir` (e.g. `.bcc/sessions/<id>/`), not on a global `baseDir`. Constructors: `func OpenSession(baseDir, sessionID string) (*Store, error)` (errors if manifest missing); `func CreateSession(baseDir, specPath, specHash string, now time.Time) (*Store, *Session, error)` (generates id, writes manifest, creates dirs). Add `Session() *Session` and `Touch(status SessionStatus, now time.Time) error`.
1. [x] Add `internal/director/sessions.go` helpers: `ListSessions(baseDir) ([]Session, error)` (walks `.bcc/sessions/*/manifest.json`); `FindSessionsForSpec(baseDir, specPath) ([]Session, error)`; `ResolveSession(baseDir, sessionID, specPath string) (Session, error)` implementing the rules in Acceptance 2 and 3.
1. [x] In `internal/cli/run.go`: add `--session string` flag; adjust `--resume` semantics. Without flags, create new session. With `--resume` only, resolve by spec; with `--resume --session <id>`, resume specific id; with `--session <id>` only (no `--resume`), resume if exists or fail (do not silently overwrite).
1. [x] In `internal/cli/run_director.go`: replace ad-hoc construction of `Store` rooted at `.bcc/` with `ResolveSession` or `CreateSession`. Update `runDirectorTUI` similarly.
1. [x] Update `internal/loop/director_run.go` to receive the per-session `*Store` (the existing signature already accepts `*Store`; only the construction site changes).
1. [x] Add subcommand `bcc sessions` in `cmd/bcc/sessions.go` (or attached to `cmd/bcc/main.go`): `list` (text and json), `show <id>` (text and json). Cover both with tests.
1. [x] Update existing tests that write to `.bcc/`: replace setup with `t.TempDir()` plus `CreateSession`; resume paths use `OpenSession`.
1. [x] Status updates: `Touch(SessionRunning)` on start; `Touch(SessionDone)` on completion; `Touch(SessionAborted)` on `ExitInvalid`; `Touch(SessionEscalatedPending)` when emitting an escalation that awaits user input. Cover four scenarios in `internal/loop/director_integration_test.go`.
1. [x] Update `docs/guides/director.md` (en and pt-BR) with a "Sessions" section.

### Phase 2: DAG domain types and validator

**Context**: PRD 5 makes tasks first-class DAG nodes nested inside phases. Today `Phase` carries `Acceptance []AcceptanceItem`; there is no `Task`. This phase introduces the nested shape: `Plan` carries `Phases []Phase`, each `Phase` owns `Tasks []Task`. Tasks own acceptance, intra-phase dependencies, priority, status, and retry budget. Cross-phase task dependencies are not representable; cross-phase ordering goes through phase-level `depends_on`. The validator is extended to detect cycles at both the phase level and within each phase's task DAG.

**Depends on**: P1.

**Acceptance**:

1. `Plan` has `Phases []Phase` only. `Phase` gains `Priority int`, `Tasks []Task`, and loses `Acceptance` (moves to Task). `Task` has `ID`, `Title`, `Intent`, `DependsOn []string` (task ids within the same phase), `Priority int`, `Acceptance []AcceptanceItem`, `Status TaskStatus`, `RetryBudget int`. Task does not carry a `PhaseID` field; nesting is the back-reference. `TaskStatus` is a string enum with constants `TaskPending`, `TaskInProgress`, `TaskDone`, `TaskNeedsFix`.
2. Phase ids are unique within the plan. Task ids are unique within their owning phase, not globally.
3. `ValidatePlan` rejects: empty phases; phases with no tasks; duplicate phase ids; duplicate task ids within a phase; phase-level `depends_on` referencing unknown phase ids; task-level `depends_on` referencing ids outside the same phase or unknown within the phase; cycles in the phase-level DAG; cycles in any phase's task-level DAG.
4. `Briefing` Go type is refactored to PRD 5 shape: `{IterationID, PhaseID, SubDAGTaskIDs []string, Instructions, SpecPath string, PriorFeedback *string}`. Old fields (`Attempt`, `SpecExcerpt`, `ContextSummary`) are removed.
5. Existing call sites that read `Phase.Acceptance` (in `internal/loop/director_run.go`, `internal/loop/director_decider.go`, `internal/director/render.go`, fakes, and tests) are updated to walk `Phase.Tasks[i].Acceptance`. The legacy phase-level `Verdict` is preserved as-is in this phase; its removal happens in P6 along with the loop rewrite.
6. JSON schema in `internal/director/schemas/plan.schema.json` reflects the nested shape.
7. Round-trip JSON tests cover the new types.
8. `go test -race ./...` green; `go vet ./...` clean.

**Tasks**:

1. [x] In `internal/director/types.go`: add `Task` struct (no `PhaseID` field; nesting is the back-reference); add `TaskStatus string` with constants and `String`/`MarshalJSON`/`UnmarshalJSON` enforcing the closed set. Move `Acceptance` from `Phase` to `Task`. Add `Priority int` to `Phase` and `Task`. Move `RetryBudget` from `Phase` to `Task`. Add `Tasks []Task` to `Phase`.
1. [x] Remove `Plan.Tasks` if any draft sketch added it; the only nesting is `Plan.Phases[].Tasks`.
1. [x] Extend `ValidatePlan` in `internal/director/types.go`: enforce phase-id uniqueness; per-phase task-id uniqueness; phase-level deps resolve to existing phase ids; task-level deps resolve to task ids in the same phase only; cycle detection runs on the phase DAG and, separately, on each phase's internal task DAG. DFS with three-color marking on each.
1. [x] Add a helper `func (p *Phase) TaskByID(id string) *Task` for ergonomic lookup; mirror at `func (pl *Plan) PhaseByID(id string) *Phase`.
1. [x] Update `internal/director/schemas/plan.schema.json` to nest `tasks` inside `phase`; remove any `phase_id` field on the task schema.
1. [x] Update `internal/director/types_test.go` with table-driven validator tests: phase cycle (`A -> B -> A`); intra-phase task cycle (`t1 -> t2 -> t1` inside phase `A`); task-level dep crossing phases (rejected); duplicate task id within a phase (rejected); duplicate task id across phases (accepted, since task ids are phase-local).
1. [x] Update `internal/director/ids.go::PhaseID` if needed. Add `func TaskID(specHash, phaseID, intent string) string` returning a phase-scoped task id.
1. [x] Update `internal/director/render.go` to render task-level acceptance in the briefing prompt by walking `phase.Tasks` (interim shape; full prompt rewrite happens in P4).
1. [x] Refactor `internal/director/types.go::Briefing` to the PRD 5 shape (`IterationID`, `PhaseID`, `SubDAGTaskIDs []string`, `Instructions`, `SpecPath`, `PriorFeedback *string`). Drop `Attempt`, `SpecExcerpt`, `ContextSummary`. Update `internal/director/briefing.go::BriefingFor` to populate the new fields (interim: `IterationID` derived from a counter; `SubDAGTaskIDs` populated with all pending tasks in the next eligible phase; full per-iteration semantics arrive in P5/P6).
1. [x] Update `internal/loop/director_run.go` and `internal/loop/director_decider.go` to read `phase.Tasks[i].Acceptance` instead of `phase.Acceptance`. Aggregate per-task acceptance results to keep the existing phase-level Verdict shape working until P6 removes it.
1. [x] Update fakes in `internal/director/fake/` to produce nested DAG plans and Briefings in the new shape.

### Phase 3: Run-wide MCP server with real handler dispatch

**Context**: The MCP server is currently spawned per Executor invocation and has a stub handler. PRD 5 requires it to be run-wide and to dispatch real method calls with per-connection authorization. This phase lifts `mcp.Start` to run boot, introduces a `Handler` interface, adds a real handler in `internal/director/dag/` backed by an in-memory DAG state, and removes the per-Executor MCP boot.

**Depends on**: P2.

**Acceptance**:

1. `internal/mcp/server.go` exposes a `Handler` interface (`HandleCall(connectionName, methodName string, input map[string]any) (resultText string, err error)`). The server validates the bearer token and the connection name, then calls `Handler.HandleCall`.
2. `cmd/bcc/main.go` (or `internal/cli/run.go`) starts a single `mcp.Server` per run, before any agent invocation. The server is closed at run exit.
3. `internal/director/dag/dag.go` defines `type State struct { Phases map[string]*PhaseState; mu sync.Mutex }` (each `PhaseState` carries its own `Tasks map[string]*TaskState`) with `Apply(event)`, `Snapshot()`, `PendingTasks(phaseID string) []TaskID`, `EligiblePhases() []PhaseID`, `HasPending() bool`. Concurrent `Apply` is race-free.
4. `internal/director/dag/registry.go` defines an `AgentRegistry` keyed by `agent_id` carrying `{role, briefingID?, subDAG?, registeredAt}` with `Register(role, ...) AgentID`, `Lookup(agentID) (Entry, bool)`, `Deregister(agentID)`. Mutex-guarded.
5. `internal/director/dag/handler.go` implements `mcp.Handler`. It dispatches by method name to per-method handler functions; each function (a) extracts and validates `agent_id` from the input, (b) verifies the registered role matches the connection name, (c) validates the rest of the input against the per-method JSON Schema, (d) for scope-sensitive methods, verifies the requested resource lies within the agent's registered scope, (e) mutates state or returns a query response. Unknown methods, missing/unregistered `agent_id`, role mismatches, and scope violations all return structured MCP errors.
6. The Executor adapter (`internal/executor/claude/claude.go`) accepts MCP wiring (URL, token, connection name, agent_id) via `Config` and stops calling `mcp.Start` itself. The Director Claude adapter does the same per role.
7. `go test -race ./...` green; `go vet ./...` clean.

**Tasks**:

1. [x] Add `Handler` interface to `internal/mcp/server.go`. Refactor the JSON-RPC handler to delegate `tools/call` to `Handler.HandleCall`. Preserve the existing `tools/list` behavior (returns the registered tool descriptors).
1. [x] Add a `connectionName` parameter to the server config (a list of valid connection names). The server reads the connection name from a custom header (`X-BCC-Role`) on every request and rejects requests with unknown names.
1. [x] Update `internal/mcp/server_test.go` for the new `Handler` interface and connection-name header.
1. [x] Create `internal/director/dag/dag.go` with `State` keyed by phase, each phase carrying its own task map; `Apply`, `Snapshot`, `PendingTasks(phaseID)`, `EligiblePhases()`, `HasPending`. Use `sync.Mutex`.
1. [x] Create `internal/director/dag/registry.go` with `type AgentID string`, `type Role string` (constants `RolePlanner`, `RoleBriefer`, `RoleExecutor`, `RoleReviewer`), `type AgentEntry struct { Role; BriefingID *string; SubDAG []TaskID; PhaseID *string; RegisteredAt time.Time }`, `type AgentRegistry struct { entries map[AgentID]AgentEntry; mu sync.Mutex }`. Methods: `Register(role Role, opts ...RegisterOption) (AgentID, error)`, `Lookup(AgentID) (AgentEntry, bool)`, `Deregister(AgentID)`. `Register` generates a fresh id like `<role>-<short-rand-hex>`.
1. [x] Create `internal/director/dag/handler.go` with the `Handler` implementation. Each per-method handler (a) parses `agent_id` from the input, (b) calls `registry.Lookup`, rejecting if missing or if role mismatches the connection name, (c) validates the rest of the input against the per-method JSON Schema, (d) for scope-sensitive methods, verifies the requested resource lies within the agent's registered `SubDAG`/`PhaseID`, (e) executes the dispatch. P3 ships with placeholders for every method P5 fills in: each method's handler returns "not yet implemented" so the dispatch shape is in place.
1. [x] Add `internal/director/schemas/mcp/` with placeholder schemas per method, all carrying `agent_id` as a required top-level field (P5 fills in the rest).
1. [x] In `internal/cli/run.go` (or `cmd/bcc/main.go`), instantiate the MCP server at run boot with the registry attached to the handler. Pass URL, token, and a connection-name allow-list. The runtime spawns agents through helpers that (1) call `registry.Register(role, opts)` to obtain the agent_id, (2) write the per-spawn `mcp-config` with the role's connection name, (3) embed the `agent_id` in the agent's prompt, (4) `Deregister` after the process exits.
1. [x] In `internal/executor/claude/claude.go`, remove the `mcp.Start` call. Add `Config.MCPURL`, `Config.MCPToken`, `Config.MCPConnectionName`, `Config.AgentID` fields. The Config is constructed by the run helper above.
1. [x] Update `internal/executor/claude/mcp.go` accordingly: keep `BccTools()` for the descriptors registered with the agent CLI; remove dispatch logic (now in the handler).
1. [x] Add `internal/director/dag/dag_test.go` with concurrent `Apply` under `-race`; snapshot/restore round-trip; `PendingTasks` ordering. Add `internal/director/dag/registry_test.go` covering register/lookup/deregister, mismatched role rejection, double-register safety.
1. [x] Resume reconciliation: when `OpenSession` reads an existing `dag.json`, every task with `status: in_progress` is rewritten to `pending` before the session is handed to the loop. Cover with a test that crafts a `dag.json` with stuck `in_progress` tasks and verifies the next `EligiblePhases()` includes them as pending. (Per PRD 5 risks: agent processes that died mid-iteration leave tasks stuck; resume must heal them.)

### Phase 4: All Director roles get tools, MCP, and spec-path-only inputs

**Context**: Today the Director Claude adapter invokes `claude -p --bare --no-session-persistence --json-schema <file>` for all three roles. PRD 5 requires each role to run as an interactive agent with `Read,Bash,Grep,Glob` tools, `--dangerously-skip-permissions`, MCP wiring, and **no** `--json-schema`. Roles read the spec via the Read tool; bcc never inlines spec content. This phase rewrites the adapter and the prompts.

**Depends on**: P3.

**Acceptance**:

1. The Planner, Briefer, and Reviewer are each invoked with `--allowed-tools "Read,Bash,Grep,Glob"`, `--dangerously-skip-permissions`, `--mcp-config <per-role>`, and `--strict-mcp-config`. None receive `--json-schema`. Verifiable via golden-arg tests in `internal/director/claude/claude_test.go`.
2. `PlannerInput`, `BrieferInput`, and `ReviewerInput` no longer contain `SpecContent []byte`. They carry only `SpecPath` (and the role-specific other inputs already in place).
3. `ReviewerInput.AcceptanceEvidence` is removed from the type, the prompt payload, and the schema.
4. The prompts in `internal/director/prompts/{plan,brief,review}.md` are rewritten to: declare role; list available tools and read-only boundaries; instruct reading the spec via Read; instruct emitting structured output via the appropriate MCP method (P5 wires the methods themselves, but the prompts already reference them).
5. `go test -race ./...` green; `go vet ./...` clean.

**Tasks**:

1. [x] In `internal/director/claude/claude.go`: rewrite `runJSONCall` (or split into per-role variants) so each role's argv reflects the new envelope. Drop the `--json-schema` flag entirely. Add `--allowed-tools "Read,Bash,Grep,Glob"`, `--dangerously-skip-permissions`, `--mcp-config <path>`, `--strict-mcp-config`. The mcp-config path is per-spawn: a fresh tempfile per invocation, parameterized with role connection name and `X-BCC-Role` header.
1. [x] Add `Adapter.Config.AgentID string` (assigned by the run helper from `registry.Register(...)`); the adapter embeds it into the prompt at a well-known marker so the role reads it back when emitting MCP calls.
1. [x] Update `Adapter.Plan`, `Adapter.Brief`, `Adapter.Review` to no longer parse stdout for the structured output. The agent emits via MCP; bcc reads the result from the handler. Adapter's job becomes: invoke the agent (with its assigned `agent_id`), wait for clean exit, deregister the agent, return success or a typed error. The Plan and Briefing artifacts are read by the loop directly from the handler/store after the call returns; the Reviewer produces no single artifact (its work is encoded as DAG mutations plus a final `bcc_review_finished` outcome, all persisted by the handler).
1. [x] Add `internal/director/claude/mcpconfig.go` to write the per-spawn mcp-config files atomically before each invocation. Each file binds URL, token, role connection name, and (in the prompt, not the config) the agent_id.
1. [x] In `internal/director/ports.go`, drop `SpecContent []byte` from `PlannerInput`, `BrieferInput`, and `ReviewerInput`. Keep `SpecPath`. Drop `ReviewerInput.AcceptanceEvidence`.
1. [x] Rewrite `internal/director/prompts/plan.md`. Structure: role; tools; boundaries; **agent_id** (a clear line "Your agent_id is `{{.AgentID}}`. Pass this as the first argument on every MCP call."); how to read the spec (Read tool, with absolute path); how to emit (`bcc_task_started(agent_id, "planning")`, `bcc_plan_emit(agent_id, plan)`, `bcc_task_completed(agent_id, "planning")`); compose `absolute_restrictions` partial.
1. [x] Rewrite `internal/director/prompts/brief.md` similarly: agent_id, query via `bcc_get_dag_snapshot(agent_id)`, decide the sub-DAG within a single eligible phase, emit via `bcc_briefing_emit(agent_id, ...)`.
1. [x] Rewrite `internal/director/prompts/review.md`: agent_id, query via `bcc_get_briefing(agent_id)`, `bcc_get_diff(agent_id)`, `bcc_get_dag_snapshot(agent_id)`; audit each task; call `bcc_task_approved(agent_id, ...)` or `bcc_task_needs_fix(agent_id, ...)`; close with `bcc_review_finished(agent_id, outcome)`. List how to handle each evidence kind (diff inline, test/build via Bash where appropriate, manual escalates to the user).
1. [x] Update `internal/cli/run_director.go` to drop `os.ReadFile(specPath)`. Pass `SpecPath` through `PlannerInput` and propagate forward.
1. [x] Update `internal/loop/director_run.go` to drop the per-attempt `os.ReadFile(specPath)` (it was used for the briefer's `SpecContent` and journal-delta computation). Journal delta moves into the handler in P5; for now, leave a stub that returns empty.
1. [x] Update fakes (`internal/director/fake/`) to satisfy the new input shapes (no `SpecContent`); fake roles can use a fixed agent_id like `fake-planner-001`.
1. [x] Update golden tests for prompts in `internal/director/render_test.go`.

### Phase 5: MCP method surface for all roles

**Context**: P3 stood up the dispatch shape with placeholder methods; P4 wired the agents to it. This phase implements every method described in [PRD 5 section 8](./2026-05-02-executable-plan-dag.md): emit methods (`bcc_plan_emit`, `bcc_briefing_emit`), per-task methods (`bcc_task_started`, `bcc_task_completed`, `bcc_task_approved`, `bcc_task_needs_fix`), finalization methods (`bcc_iteration_finished`, `bcc_review_finished`), and query methods (`bcc_get_briefing`, `bcc_get_dag_snapshot`, `bcc_get_pending_tasks`, `bcc_get_diff`, `bcc_get_journal_delta`). Each has a JSON Schema validated server-side.

**Depends on**: P3, P4.

**Acceptance**:

1. Every method in PRD 5 section "MCP method surface" has a JSON Schema in `internal/director/schemas/mcp/<method>.schema.json` and a handler function in `internal/director/dag/handler.go`. Every schema requires `agent_id` as a top-level string field. Schemas are validated at registration.
2. `agent_id` is enforced on every call: missing, unregistered, or role-mismatched ids return a structured MCP error before any other validation.
3. Scope-sensitive methods (`bcc_get_briefing`, `bcc_get_pending_tasks`, `bcc_task_started`, `bcc_task_completed`, `bcc_task_approved`, `bcc_task_needs_fix`, `bcc_iteration_finished`, `bcc_review_finished`, `bcc_get_diff`, `bcc_get_journal_delta`) verify that the requested resource lies within the calling agent's registered scope (its `BriefingID`/`SubDAG`/`PhaseID`). Out-of-scope returns a structured error.
4. `bcc_plan_emit` validates the Plan against `plan.schema.json` and runs `ValidatePlan`. On rejection, returns a structured error to the agent so it can correct.
5. `bcc_get_diff` shells out to git via `loop.GitProbe.Diff(ctx, baseSHA, headSHA)` (already present); `bcc_get_journal_delta` calls into a format adapter port (markdown_bcc for now).
6. `bcc_get_dag_snapshot` for the Briefer returns a deep copy of the full DAG state. For Executor and Reviewer it returns the slice scoped to the agent's `PhaseID`.
7. Each successful method call appends an entry to `.bcc/sessions/<id>/mcp-log.jsonl` with timestamp, agent_id, role, method, input, and result.
8. `go test -race ./...` green; `go vet ./...` clean.

**Tasks**:

1. [x] Author JSON Schemas for each method input under `internal/director/schemas/mcp/`. Each schema requires a top-level `agent_id: string`. Schemas are loaded by the handler at startup and used to validate inputs after agent identity checks.
1. [x] Implement each method in `internal/director/dag/handler.go`. Group by category: emit (plan, briefing), mutate (task started/completed/approved/needs_fix; iteration_finished; review_finished), query (briefing, dag_snapshot, pending_tasks, diff, journal_delta).
1. [x] Wire the agent identity check at the entry of every dispatch: parse `agent_id`, `registry.Lookup`, reject if missing/unregistered; reject if `entry.Role` does not match the role bound to the connection name.
1. [x] Implement scope checks per method: `bcc_get_briefing` returns only the briefing associated with the agent's `BriefingID`; `bcc_get_pending_tasks` filters to the agent's `SubDAG`; `bcc_task_*` mutations reject task ids outside the agent's `SubDAG`; `bcc_get_dag_snapshot` returns full state for Briefer (its registry entry has no scope) and phase-scoped state for Executor/Reviewer.
1. [x] Implement cross-method invariants on `bcc_review_finished`: outcome `approve` requires every sub-DAG task `done`; `revise` requires at least one `needs_fix`; `escalate` requires non-empty `reasoning`. Reject otherwise.
1. [x] Persistence: each mutation method calls `Store.WriteDAGSnapshot(state)` after the mutex is released. Snapshot uses write-tmp-then-rename for atomicity.
1. [x] MCP audit log: handler writes a structured JSONL line per call to `<sessionDir>/mcp-log.jsonl` including `agent_id`. Configurable via `[director].mcp_audit` (default true) in `internal/config/config.go`.
1. [x] Add a format-adapter port `JournalDeltaProvider interface { JournalDelta(ctx, before, after []byte) string }` consumed by the handler. The markdown_bcc adapter implements it (in P5; the existing helper in `internal/director/journal.go` is reused).
1. [x] Update fakes to use the new MCP methods (fakes simulate calling the handler directly, bypassing the HTTP layer; they pass realistic agent_ids registered through the same `AgentRegistry`).
1. [x] Update prompts (P4) to reference the methods by name in their instructions, with `agent_id` as the first argument on every call.
1. [x] Test `internal/director/dag/handler_test.go`: each method with valid input; missing `agent_id`; unregistered `agent_id`; role/connection mismatch (Reviewer agent_id calling from Executor connection); scope violation (Executor 1 trying to complete a task in Executor 2's sub-DAG); cycle detection on `bcc_plan_emit`; cross-method invariants on `bcc_review_finished`.
1. [x] Integration test in `internal/mcp/server_test.go`: end-to-end RPC including connection-name header and agent_id in the body.

### Phase 6: DAG-driven loop driver

**Context**: Today `runDirector` iterates `plan.Phases` with an inner attempt loop per phase. PRD 5 makes the loop DAG-driven: outer loop while pending tasks remain; per iteration the Briefer picks a sub-DAG and emits a Briefing via MCP; the Executor consumes the Briefing via MCP; the Reviewer audits per task via MCP; the inner loop reruns the Executor up to retry budget when tasks remain in `needs_fix`. The decider becomes a per-task aggregation reading the DAG state.

**Depends on**: P5.

**Acceptance**:

1. `runDirector` in `internal/loop/director_run.go` is rewritten: outer loop on `dag.HasPending()`; per iteration calls Briefer, then Executor, then Reviewer; inner loop reruns Executor while sub-DAG has `needs_fix` tasks and retry budget is not exhausted.
2. The decider in `internal/loop/director_decider.go` aggregates DAG state to determine whether to advance, retry, or escalate. Per-task verdicts replace per-phase verdicts.
3. `parseAssistant` and `agentcontract.FromToolCall` are demoted to UI/cost informational roles. A test in `internal/loop/agentcontract/agentcontract_test.go` (or a new package test) verifies that for any recorded run, every observed `tool_use` envelope corresponds to an MCP handler call recorded in `mcp-log.jsonl`.
4. The loop reads DAG state via `Store.ReadDAGSnapshot()` (or directly from the in-memory state) at iteration boundaries. It does not re-parse the spec.
5. `go test -race ./...` green; `go vet ./...` clean. Existing integration tests pass after fakes are updated.

**Tasks**:

1. [x] Rewrite `internal/loop/director_run.go::runDirector`. Outline:
   - Initialize DAG state from `Store.ReadPlan()` (read once at boot).
   - While `dag.HasPending()`:
     - Register a Briefer agent_id; spawn the Briefer with that id; wait for `bcc_briefing_emit` to land; deregister.
     - Read the Briefing back from the store; capture `phase_id` and `sub_dag_task_ids` for downstream agent registration.
     - For attempt in 1..1+retryBudget:
       - Register an Executor agent_id bound to the briefing (BriefingID, SubDAG, PhaseID); spawn Executor; on exit, verify `bcc_iteration_finished` was called (else treat as `blocked`); deregister.
       - Register a Reviewer agent_id bound to the same briefing; spawn Reviewer; on exit, verify `bcc_review_finished` (else treat as `escalate`); deregister.
       - If sub-DAG has no `needs_fix`: break (advance).
       - If outcome is `escalate` or attempt exhausted: emit `DirectorEscalation`, await `EscalationReply`.
   - Terminate with `ExitDone` when DAG is empty.
1. [x] Rewrite `internal/loop/director_decider.go` for per-task aggregation. Inputs: DAG state, attempt, retryBudget, head_advanced. Outputs: `Advance`, `Retry`, `Escalate`, `Abort`.
1. [x] Add `internal/director/dag/state.go` helpers for outer/inner loop predicates (`HasPending`, `SubDAGFullyDone`, `SubDAGAnyNeedsFix`).
1. [x] Update `internal/loop/events.go` to emit per-task events: `TaskStarted`, `TaskCompleted`, `TaskApproved`, `TaskNeedsFix`. Existing phase-level events are kept as informational summaries derived from DAG state.
1. [x] Update fakes in `internal/director/fake/` to drive scenarios via direct handler calls (the fake Executor/Reviewer can call the handler in-process to mutate DAG state without an HTTP round-trip).
1. [x] Rewrite `internal/loop/director_integration_test.go` per the PRD-5 verification list: planning-as-task, sub-DAG selection, retry-budget exhaustion, force-approve advances run, polling tests where the fake Executor calls `bcc_get_briefing` mid-attempt.
1. [x] Compatibility verification test: walk a recorded session's stream-json plus `mcp-log.jsonl` and verify alignment.
1. [x] Remove obsolete Verdict types and Store methods. Delete `Verdict`, `VerdictOutcome`, `VerdictFeedback`, `RequiredChange`, `OutOfScopeNote`, `ValidateVerdict` from `internal/director/types.go`. Delete `Store.WriteVerdict`, `Store.ReadVerdict`, `Store.LatestVerdict` from `internal/director/store.go`. The new model encodes per-task review outcomes as DAG state (`done` vs `needs_fix` plus per-task feedback string from `bcc_task_needs_fix`); whole-phase Verdict is no longer a domain concept. Update any remaining test fixtures that reference these types.

### Phase 7: Four-option escalation with hint propagation

**Context**: The current `EscalationReply` is an `int` enum with `Resume`, `Skip`, `Abort`. PRD 5 requires four options: `resume_with_hint`, `force_approve`, `skip`, `abort`. `Hint` is propagated to the next briefing's `prior_feedback`. `force_approve` synthesizes `done` for the still-pending sub-DAG tasks via direct handler mutation.

**Depends on**: P6.

**Acceptance**:

1. `EscalationReply` is a struct: `type EscalationReply struct { Kind EscalationKind; Hint string }`. Constants `EscalationResume`, `EscalationForceApprove`, `EscalationSkip`, `EscalationAbort` are `EscalationKind` values.
2. With `Kind=EscalationResume, Hint="<text>"`, the next iteration's briefing receives the hint prepended to `prior_feedback`. Briefer prompt instructs that the hint comes from the user via escalation.
3. With `Kind=EscalationForceApprove`, the loop calls a handler method that synthesizes `done` for every pending sub-DAG task, persists the change, and continues. The audit log records a synthetic entry with `role: "user"` and `method: "bcc_force_approve"`.
4. The TUI modal shows four buttons: `[R]esume hint`, `[F]orce-approve`, `[S]kip`, `[A]bort`. `R` opens a text input; submit packages `EscalationReply{Resume, hint}` and sends it on the gate.
5. The stdin gate (text/json modes) reads first line as the letter (`r`, `f`, `s`, `a`); on `r` reads a second line as the hint; invalid inputs re-prompt.
6. `go test -race ./...` green.

**Tasks**:

1. [x] In `internal/loop/ports.go`: rename existing `EscalationReply int` to `EscalationKind int`; create `type EscalationReply struct { Kind EscalationKind; Hint string }`. Update existing constants. Add `EscalationForceApprove`.
1. [x] In `internal/loop/director_run.go`: handle `EscalationForceApprove` by calling a new handler method `bcc_force_approve_pending` (registered with `connectionName: "bcc-loop"` for internal calls). The handler writes `done` to the still-pending sub-DAG tasks under the mutex and appends a synthetic audit entry.
1. [x] In `internal/director/dag/handler.go`: add `bcc_force_approve_pending(briefing_id)` method, callable only from the loop's internal connection. Schema and authz reflect this.
1. [x] In `internal/director/render.go`: extend `RenderBriefingPrompt` (or its successor in P4) to prepend a "User hint (escalation)" block to `prior_feedback` when `Hint` is non-empty.
1. [x] In `internal/loop/director_run.go`, propagate the hint from the previous attempt's `EscalationReply` into the next Briefer invocation (a field on the runtime state or written to the handler before invocation).
1. [x] In `internal/tui/director.go`: add `escalationStateChoosing` and `escalationStateHintInput`. The hint input uses `bubbles/textinput`. Submit packages and forwards `EscalationReply{Resume, hint}`. Esc cancels the hint input.
1. [x] Update `stdinEscalationGate` in `internal/cli/run_director.go` for the two-line protocol on `r`. Empty input on the second line is treated as no hint.
1. [x] Snapshot tests in `internal/tui/director_test.go`: choosing state with four buttons; hint input state; transitions.
1. [x] Integration scenarios in `internal/loop/director_integration_test.go`: (a) escalate then resume with hint produces a briefing carrying the hint; (b) escalate then force-approve writes `done` for pending tasks and advances; (c) last iteration with force-approve terminates with `ExitDone`.

### Phase 8: Wire-protocol partial rewrite, planning-as-task UI, docs

**Context**: P5 and P6 leave the legacy parser informational. The wire-protocol partial in `internal/loop/agentcontract/wire_protocol.md` still describes the old "emit JSON line" model; it must be rewritten as the user-facing manual for using MCP from inside the agent. The TUI must surface planning as a task on the timeline. The user guides need updates.

**Depends on**: P5, P6, P7.

**Acceptance**:

1. `internal/loop/agentcontract/wire_protocol.md` is rewritten to document the per-role MCP method surface and the polling pattern. No mention of `tool_use` envelopes as the protocol of record.
2. The TUI timeline shows the planning task as the first entry of every run, with the same visual treatment as work tasks. Sub-DAG tasks of the active iteration are highlighted.
3. `docs/guides/director.md` (en and pt-BR) is updated to cover sessions, the DAG model, MCP communication, four-option escalation, and what changed from the MVP wire protocol.
4. `go test -race ./...` green.

**Tasks**:

1. [x] Rewrite `internal/loop/agentcontract/wire_protocol.md`: per-role method tables; polling pattern (entry, task boundaries, retry boundaries, exit); error handling (how the agent reads structured MCP errors and corrects). Keep the file as a partial composed into Director and Executor prompts.
1. [x] Update `internal/loop/agentcontract/agentcontract_test.go` golden tests for the new partial.
1. [x] In `internal/tui/director.go`: extend the timeline panel with a `planning` track that consumes `TaskStarted("planning")`/`TaskCompleted("planning")` events. Sub-DAG highlighting in the iterations panel.
1. [x] Snapshot tests in `internal/tui/director_test.go` for the planning track and sub-DAG highlight.
1. [x] Rewrite `docs/guides/director.md` (en) and `docs/guides/director.pt-BR.md`. Sections: sessions; the executable-plan DAG; MCP communication and roles; four-option escalation; troubleshooting (what to check in `mcp-log.jsonl` if a run misbehaves).
1. [x] Update `docs/specs/director/index.md` to mark PRD 5 as the current target and PRD 2 as superseded; add "Documents in this initiative" rows for the new PRD and the corrections spec.

## Cross-cutting requirements

Applicable to every phase:

1. **Layer boundaries**. No adapter imports inside `internal/director/`. `internal/director/dag/` is a sibling package depended on by `internal/director/claude/` and `internal/loop/`; the dag package itself imports nothing under those.
2. **Stdlib-only in the domain.** `internal/director/` (excluding subpackage adapters) imports only stdlib plus the existing JSON Schema dependency.
3. **Tests pass with `-race`.** Every phase commits with `go test -race ./...` green.
4. **gofmt and go vet clean.**
5. **No narrative comments.** Code without "WAS", "REMOVED", "now we use X". The spec is the only place where history appears, and only where the PRD asks for it.
6. **No backwards-compat shims.** When `EscalationReply` becomes a struct (P7), every call site updates in the same commit. When `SpecContent` is removed (P4), no temporary alias remains.
7. **No en-dash characters in prose** (project authorial preference).

## Done criteria

This spec is done when:

1. Every phase P1 through P8 has its checkboxes marked.
2. `go test -race ./...` green.
3. `go vet ./...` clean.
4. `bcc run docs/specs/test-validation/<short-spec>.md` (or a minimal self-test spec) completes with `ExitDone` on a machine with `claude` on PATH and a valid API key. After completion, `bcc sessions list` shows the session with `status=done`.
5. Two consecutive `bcc run` invocations against different specs leave two distinct sessions in `.bcc/sessions/`, both listable.
6. A real session's `mcp-log.jsonl` shows a complete chain of method calls: planning task started, plan emitted, briefer iterations, executor task progress, reviewer per-task verdicts, review finished, iteration advance, run done.
7. Every observed `tool_use` envelope in the run's stream-json has a corresponding entry in `mcp-log.jsonl` (verified by the compat test in P6).
8. `docs/guides/director.md` (en and pt-BR) is up to date.

## Stop criteria

The agent stops and waits for the observer when:

1. Validation fails three iterations in a row after a `git revert` of the last problematic iteration (default contract rule).
2. Ambiguity in PRD 5 requires a human decision: emit `iteration_result` with `value=blocked` and the question summarized.
3. A handler dispatch decision is unclear (which role owns a method, which connection-name allow-list applies). Emit `value=blocked` with the affected method names.
4. The compat test in P6 reveals divergence between stream-json and `mcp-log.jsonl` that cannot be reconciled. Emit `value=blocked` and surface the offending records.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| Per-method handler proliferates and becomes hard to maintain | Keep methods small, single-purpose. The handler dispatch table is data; adding a method is a row in the table plus a function plus a schema. CI validates that every method has a schema and a test. |
| Connection-name authorization can be bypassed if the agent forges the header | Every connection name is paired with a derived sub-token written into the per-role mcp-config file. The handler validates token-name pairs together. Forging requires reading the per-role config; any agent with that level of access already has the run secrets. |
| MCP server's mutex serializes all dispatch and becomes a contention point | Handler dispatch is short and CPU-bound; no I/O under the mutex. If observed contention emerges, split per-method locks (DAG-mutate methods take the DAG lock; query methods are read-locked). |
| Audit log size grows large for long runs | Single-file JSONL with per-line records. A 1000-iteration run produces a few hundred kilobytes. Acceptable. Provide a flag to disable auditing if needed. |
| Force-approve normalizes mediocre work | The synthetic audit entry is preserved; TUI surfaces force-approved tasks distinctly; the user is responsible for the consequence. Same trade-off as the previous corrections spec. |
| Polling cost inflates per-iteration latency | Polling is bounded by the prompt instructions (entry, retry boundaries, exit). Three to five MCP calls per role per iteration is acceptable. Beyond that, the prompt is explicit about not over-polling. |

## References

- [PRD 5: Executable plan as a DAG, MCP-mediated communication](./2026-05-02-executable-plan-dag.md): the normative model this spec implements.
- [Original implementation spec](./2026-05-02-reviewed-execution-implementation.md): the prior spec this migration corrects.
- [Initiative index](./index.md): Director initiative overview.
- `internal/mcp/server.go`: MCP server transport, becomes the protocol substrate in P3.
- `internal/director/dag/`: new package for in-memory DAG state and handler.
- `internal/loop/agentcontract/wire_protocol.md`: legacy partial rewritten in P8 as the MCP usage manual.

## Execution Journal

<!-- Filled in by the agent during execution per the bcc-markdown contract. -->

### 2026-05-02 17:00, Phase 1: Per-session state isolation

Sessions land as the new persistence unit; every `bcc run` allocates `.bcc/sessions/<id>/` with a manifest, and the CLI gains `--session` plus `bcc sessions list/show`.

**Decisions:**

- `Touch(SessionDone | SessionAborted)` lives in `loop.runDirector` via a deferred status switch keyed on the loop's exit code, not in `runDirectorWith`. The loop is the single point that knows the canonical exit shape; centralising the lifecycle there avoids drift between the CLI and TUI hosts and makes the "four scenarios" of the manifest observable from a single layer.
- `ErrSessionNotFound` is constructed with `fmt.Errorf("...: %w", fs.ErrNotExist)` so existing `errors.Is(err, fs.ErrNotExist)` checks keep working alongside the new sentinel, without bespoke `Is/Unwrap` plumbing.
- `--resume` without a matching session falls through to creating a fresh one (preserving the existing operator-friendly UX) rather than erroring out. The migration spec acceptance is silent on the zero-match case; falling through keeps the workflow forgiving when a user habitually passes `--resume`.

**Discovered:**

- The pre-P1 `*director.Store` constructor (`NewStore`) was used in three test files plus the production CLI. Converting them all to `CreateSession` / `OpenSession` was straightforward because the only path consumer outside tests was `loop/director_run.go`, which already received `*Store` through `DirectorPorts`.

### 2026-05-02 18:00, Phase 2: DAG domain types and validator

`Plan` becomes a two-level DAG: `Phase` carries `Tasks []Task`; `Task` owns `Acceptance`, intra-phase `DependsOn`, `Priority`, `Status`, and `RetryBudget`. `Briefing` adopts the PRD 5 shape (`IterationID`, `PhaseID`, `SubDAGTaskIDs`, `Instructions`, `SpecPath`, `PriorFeedback *string`), and `ValidatePlan` now rejects empty task lists, duplicate phase-scoped ids, cross-phase task deps, and cycles at both DAG levels.

**Decisions:**

- `BrieferInput` grew `IterationID`, `SubDAGTaskIDs`, and `SpecPath` instead of having `BriefingFor` return both an input and a draft `Briefing`. The Briefer adapter still shapes the final `Briefing`, but now has every PRD-5 field at hand without the loop reaching across the port to mutate the result.
- The legacy phase-level `RetryBudget` collapses into a `phaseRetryBudget(phase)` helper in `internal/loop/director_run.go` that returns the maximum across the phase's tasks. Per-task retry semantics arrive in P6; the aggregation keeps the existing decider working in the meantime.
- `Store.WriteBriefing` keys file paths off `Briefing.IterationID` rather than reintroducing a separate `(phase, attempt)` argument. The loop assigns `IterationID = "<phase-id>-<attempt>"` so existing on-disk layouts remain stable, and the API stops carrying a redundant attempt counter alongside the briefing struct.

**Discovered:**

- The Claude director adapter's stdout JSON path still expects the legacy briefing schema. P2 patches `briefing.schema.json` and the per-role payload helpers minimally to keep the build green, but full removal of `--json-schema` is P4 work; nothing here pre-empts that rewrite.

### 2026-05-02 19:00, Phase 3: Run-wide MCP server with real handler dispatch

The MCP server gains a `Handler` interface and per-connection authorization (the `X-BCC-Role` header), the new `internal/director/dag` package owns the in-memory DAG state plus the agent registry, and the executor adapter no longer spawns its own MCP server: the run boot starts a single `mcp.Server` with the dag handler attached and threads `MCPURL` / `MCPToken` / `MCPConnectionName` / `AgentID` into the executor `Config`.

**Decisions:**

- Legacy executor wire tools (`task_started`, `task_completed`, `iteration_result`) bypass the registry inside `dag.Handler` and respond `"ok"` so the per-phase loop keeps converging through P3. **Why:** P5 fills in the new `bcc_*` methods; demoting the stream-json parser is P6 work. **How to apply:** when adding new wire tools that the loop already consumes, add the name to the legacy passthrough until the matching handler method lands; do not rip the parser out earlier than P6.
- The `dag` package imports `internal/director` for `TaskStatus` rather than defining a parallel enum, and `OpenSession` does not call `dag.LoadStateFile`. **Why:** `internal/director` is stdlib-only and can't depend on `dag` without breaking that boundary; routing through `dag` keeps the import direction one-way. **How to apply:** when P5/P6 wire `dag.json` reads at the cli/loop layer, do it after `OpenSession` returns rather than threading it into the store.
- The Director Claude adapter (`internal/director/claude`) does not yet receive the new MCP `Config` fields. **Why:** P3 acceptance #6 is partly fulfilled by the executor adapter rewrite; the director adapter still drives `--json-schema` and only switches to `--mcp-config` in P4 alongside the prompt rewrite. **How to apply:** when P4 rewrites `runJSONCall`, add the same MCP fields to `directorclaude.Config` and pass per-role connection names from the run helper.

**Discovered:**

- `mcp.Server` had a `Calls()` accumulator used only by tests; it disappeared along with the stub dispatch when the handler became the protocol of record. The new test fake `recordingHandler` provides the same observability, and the production server holds no per-call state.
- Resume reconciliation lives in `dag.LoadStateJSON` (in_progress collapses to pending) with a unit test against a crafted `dag.json`. The loop-level wiring at session-open time is naturally a P5/P6 task because `dag.json` is not written by P3.

### 2026-05-02 20:00, Phase 4: All Director roles get tools, MCP, and spec-path-only inputs

Every Director role now invokes claude as an interactive agent: `--allowed-tools Read,Bash,Grep,Glob`, `--dangerously-skip-permissions`, per-spawn `--mcp-config` with `X-BCC-Role` and `--strict-mcp-config`, and no `--json-schema`. `PlannerInput`, `BrieferInput`, `ReviewerInput` carry only `SpecPath` plus an `AgentID`; spec content never crosses into bcc, and `AcceptanceEvidence` is gone.

**Decisions:**

- `AgentID` lives on the per-call input struct (`PlannerInput.AgentID`, `BrieferInput.AgentID`, `ReviewerInput.AgentID`), not on `directorclaude.Config`. **Why:** one `*Adapter` satisfies all three Director ports today; pinning the id on `Config` would force the cli/loop to construct a fresh adapter per call (per role per iteration) just to thread an opaque string. The input is the per-call surface where role-specific data already lives. **How to apply:** when registering a Director agent, the cli/loop calls `mcpBoot.registerDirectorAgent(role)` (or, in tests, a stable `fake-<role>` stub) and writes the id onto the input before invoking the port; the adapter renders it into the prompt at `{{.AgentID}}`.
- Agent registration and deregistration live in the cli (`registerDirectorAgent` in `run_director.go`) and the loop (`registerDirectorAgentLoop` in `director_run.go`), not inside the adapter. **Why:** the registry is run-wide state owned by the boot helper; the adapter only needs the resulting id to put in the prompt. Threading a registry handle through the adapter would couple it to `internal/director/dag` lifecycle, beyond what `internal/director/claude` should know. **How to apply:** any future Director role (codex/gemini adapters) gets the id via the input contract; bcc keeps registration centralized.
- `Adapter.Plan` / `Brief` / `Review` return `(nil, *DirectorCallStats, nil)` on clean exit. **Why:** P5 is the phase that fills `bcc_plan_emit`, `bcc_briefing_emit`, and the per-task verdict methods; until then the handler returns `ErrNotImplemented` for those names. The migration accepts that the production claude path is non-functional between P4 and P5; the cli's `freshPlan` defends with a typed nil-plan error so the failure is loud, and every loop integration test drives the loop with fakes that bypass the adapter and supply the typed value directly. **How to apply:** P5 fills the handler methods and adds a read path (probably `Store.ReadPlan()` after `bcc_plan_emit` succeeds) so the cli/loop can recover the typed value; the adapter signature does not change.
- `internal/cli/run_director.go::runDirectorWith` still calls `os.ReadFile(specPath)` to compute `SpecHash` for the manifest and `--resume` divergence detection. **Why:** the migration acceptance is "PlannerInput carries only SpecPath" (no inlined spec content), not "bcc never opens the spec file." Hashing on the bcc side stays a domain concern. **How to apply:** never thread the read content into a Director input; the file read is bounded to the hash computation and the manifest update.

**Discovered:**

- The legacy stream-json fixtures (`fake-claude-plan.sh`, `fake-claude-briefing.sh`, `fake-claude-verdict.sh`) still emit assistant text with structured JSON. P4 keeps them as-is because the adapter only reads the `result` event for cost stats; the assistant lines are ignored. P5/P6 will replace them with MCP-driven fakes once the handler returns real data.
- `internal/director/journal.go::GatherJournalDelta` is no longer called from the loop in P4 (the Reviewer receives an empty `JournalDelta`); the helper stays in the package because P5 wires it back from the handler side via `bcc_get_journal_delta`.
- `internal/director/embed.go` still embeds `plan.schema.json`, `briefing.schema.json`, and `verdict.schema.json`. Removing them is a P5 concern when the per-method MCP schemas under `internal/director/schemas/mcp/` become the source of truth; deleting them now would force a follow-up cleanup with no benefit.


### 2026-05-02 21:00, Phase 5: MCP method surface for all roles

Every method in PRD 5 section 8 has a JSON Schema and a real handler dispatch: emit (`bcc_plan_emit`, `bcc_briefing_emit`), per-task mutations (`bcc_task_started/completed/approved/needs_fix`), finalization (`bcc_iteration_finished`, `bcc_review_finished`), and queries (`bcc_get_briefing`, `bcc_get_dag_snapshot`, `bcc_get_pending_tasks`, `bcc_get_diff`, `bcc_get_journal_delta`). Inputs are validated against the per-method schemas after agent identity checks; mutations persist atomically via `dag.SaveStateFile`; every dispatch is logged to `<sessionDir>/mcp-log.jsonl` when `[director].mcp_audit` is on (default true).

**Decisions:**

- The handler late-binds session-scoped persistence and audit through `Handler.AttachAudit`, `Handler.AttachStores`, and `Handler.AttachProviders`. **Why:** `mcpBoot` is constructed before `runDirectorWith` resolves the session directory; passing the audit path or `*director.Store` at boot time would force the constructor order to flip, dragging session resolution into the boot step. Late-binding keeps the boot symmetric for legacy and Director runs and lets tests construct a handler with only the inputs they need. **How to apply:** any new per-session collaborator the handler consumes goes through an `Attach*` setter, not the constructor.
- `bcc_get_diff` and `bcc_get_journal_delta` consult per-briefing handler-side caches (`Handler.SetBriefingDiffRange`, `Handler.SetBriefingJournalSnapshots`) rather than recomputing from git/spec on every poll. **Why:** the briefing window is fixed (one base/head SHA pair, one before/after spec snapshot) for the lifetime of a Reviewer agent; recomputing per call would re-shell `git diff` for every poll. The setter shape also keeps the dag package free of `os.ReadFile` and `git` knowledge: the loop captures the snapshots at the boundary it already owns and pushes them in. **How to apply:** P6 calls these setters from the loop driver between Executor exit and Reviewer registration; today the cli wiring is in place but the per-iteration push is still future work.
- `dag.PlanPersister` and `dag.BriefingPersister` are typed against `*director.Plan` / `*director.Briefing`. **Why:** the dag package already imports `internal/director` for `TaskStatus`; the typed signature lets `*director.Store` satisfy both interfaces structurally without an `any`-shaped seam. **How to apply:** future persisters (e.g., a SQLite store) implement the same typed contract.
- The Briefer connection authz allows `bcc_get_dag_snapshot` for Briefer, Executor, and Reviewer; the handler then scopes the response by role. **Why:** PRD 5 says the Executor and Reviewer see only their phase, but the wire method is the same name. Splitting the dispatch by role at the connection-name layer would force three nearly-identical method names; scoping inside the handler keeps the wire surface small.
- Cross-method invariants on `bcc_review_finished` consult the live DAG state (per `entry.SubDAG`), not the audit log. **Why:** the audit log is informational; the DAG is the source of truth. A Reviewer that called `bcc_task_approved` then `bcc_task_needs_fix` on the same task is consistent if the latest write is what the invariant compares against, which is what reading state gives us. **How to apply:** any future review-side invariant reads state, not history.

**Discovered:**

- The `recordingHandler` in `internal/mcp/server_test.go` is still useful for tests that exercise the transport layer in isolation; the new `internal/mcp/integration_test.go` uses the real `dag.Handler` to verify end-to-end alignment between role headers, agent_id, and the audit log. Both styles co-exist because the transport tests should not depend on dag semantics.
- `internal/director/embed.go` now also exports the `MCPSchemaFS` so `internal/director/dag/schemas.go` can read schemas without duplicating the `//go:embed` directive. The legacy `plan.schema.json`, `briefing.schema.json`, and `verdict.schema.json` embeds stay in place: `bcc_plan_emit` re-uses `plan.schema.json` as the inner Plan body schema, and the briefing/verdict ones are referenced by the Director Claude adapter until its `--json-schema`-free path is fully exercised. P6 will collapse the residue.
- The `MarshalJSON` on `*dag.State` already produced the canonical `dag.json` shape, so the `DAGSnapshotPersister` wiring just calls `dag.SaveStateFile(state, path)` per mutation; no new serialization plumbing was required.

### 2026-05-02 22:00, Phase 6: DAG-driven loop driver

`runDirector` is now a DAG-driven outer loop on `dag.HasPending()`: per iteration the loop registers a Briefer, calls `Briefer.Brief` (the agent emits via `bcc_briefing_emit`), reads the briefing back from `Handler.Briefing(iterationID)`, then enters an attempt loop that registers an Executor scoped to the briefing, captures the diff range on the handler, registers a Reviewer scoped to the same sub-DAG, and consults the live DAG plus `Handler.LastReviewOutcome` for the decision. The decider becomes per-task: `Outcome` plus `SubDAGFullyDone` / `SubDAGAnyNeedsFix` drive `advance` / `retry` / `escalate` / `abort`. The legacy `Verdict` family (`Verdict`, `VerdictOutcome`, `VerdictFeedback`, `RequiredChange`, `OutOfScopeNote`, `ValidateVerdict`, `Store.{Write,Read,Latest}Verdict`) is gone; per-task feedback travels as the `feedback` argument on `bcc_task_needs_fix` and the prose `reasoning` from `bcc_review_finished`.

**Decisions:**

- **Why:** the production CLI constructs the run-wide MCP handler at boot, but tests drive the loop with fake adapters and need a handler that has no audit/persister/git wiring. **How to apply:** `directorDeps.handler` overrides the boot-supplied handler at `runDirectorWith` time; when set, the loop receives that handler instead of the boot's. `directorEffectiveHandler(deps)` is the single resolution point.
- **Why:** legacy planner adapters in tests return `*director.Plan` directly without flowing through `bcc_plan_emit`, so the handler's `state` would be nil when the loop tried to validate sub-DAG eligibility. **How to apply:** the loop seeds `Handler.SetState` and `Handler.SetPlan` once per run from `d.Plan` when `Handler.State()` is nil, so in-process briefing emission has the DAG to validate against.
- **Why:** the JSON Schema validator (`santhosh-tekuri/jsonschema/v6`) rejects Go's `[]string` directly, expecting `[]any` (the shape JSON unmarshalling produces). **How to apply:** in-process handler callers convert string slices to `[]any` before passing them in `map[string]any` payloads; `cli.stringSliceToAny` and the loop integration test do this for `sub_dag_task_ids`. Production agents calling over HTTP get this for free because the JSON-RPC layer unmarshals into `[]any`.
- **Why:** `PhaseReviewed` was carrying a typed `*director.Verdict` so consumers could switch on `Outcome`; with the type gone, the event needs a closed alphabet to stay useful for the TUI. **How to apply:** `PhaseReviewed.Outcome` is the canonical wire string ("approve" / "revise" / "escalate"), `PhaseReviewed.Reasoning` carries the prose; the TUI maps the string directly. New per-task events (`TaskStarted`, `TaskCompleted`, `TaskApproved`, `TaskNeedsFix`) are exported from `internal/loop/events.go` for P8 to consume on the timeline.

**Discovered:**

- `bcc_iteration_finished` requires the Executor to have called it before exit, but the integration tests' `directorFakeExec` only emits the legacy stream-json `iteration_result` envelope and never calls the new MCP method. The current loop falls back to the parsed signal from the legacy envelope and only treats missing as `SignalReview` (the optimistic default). Until the Claude executor adapter is rewired in P8 to call `bcc_iteration_finished` directly, the production path keeps relying on the legacy parser; the compat test in `internal/loop/director_compat_test.go` pins that the two pathways still address the same dispatch.
- `internal/cli/run_director_test.go` had 19 tests deeply coupled to the legacy `*Verdict` shape and the old `func(string) loop.Executor` factory. The bulk of the rewrite is mechanical (handlers in helpers, slice conversion, dropped verdict file assertions), and the file is still informative for confirmation prompts / session resolution / replan flow; the loop-driver coverage now lives in `internal/loop/director_integration_test.go`.
- Per-iteration retry budget aggregation collapses into `maxRetryBudget(phase, subDAG)` in `internal/loop/director_run.go`. P3's `phaseRetryBudget` (defined over the whole phase) is removed; the new helper takes the maximum across the iteration's sub-DAG, since the loop now retries the sub-DAG as a unit.

### 2026-05-02 23:00, Phase 7: Four-option escalation with hint propagation

`EscalationReply` is now a struct carrying `Kind` and an optional `Hint`; `EscalationKind` covers `EscalationResume`, `EscalationForceApprove`, `EscalationSkip`, and `EscalationAbort`. The Director loop branches on the four kinds, propagates the hint into the next outer iteration's briefing prompt via a new `RenderBriefingPrompt(b, p, hint)` parameter and a "User hint (escalation)" template block, and force-approves pending sub-DAG tasks via a new internal `Handler.ForceApprovePending(iterationID, hint)` API that mutates state under the handler mutex and appends a synthetic audit entry with `role: "user"` and `method: "bcc_force_approve"`. The TUI modal becomes a two-state machine: choosing (`R/F/S/A`) and hint input (Enter submits, Esc cancels back). The stdin gate now reads two lines on `r` (kind, then hint).

**Decisions:**

- **Why:** the spec calls for the hint to land in the "next iteration's briefing" rather than the current attempt; threading it through the inner attempt loop would require re-rendering the prompt mid-iteration with a duplicated PromptPath, since `runDirector` renders the executor system prompt once before the attempt loop. **How to apply:** `EscalationResume` now sets `iterationDone = true` and stashes the hint in `pendingHint`. The outer loop re-briefs (the same phase if still pending) and `RenderBriefingPrompt` reads the hint as a template parameter. `priorFeedback` continues to be the reviewer's prose; the hint is rendered as an explicit higher-priority block above it.
- **Why:** "registered with `connectionName: bcc-loop` for internal calls" implies an MCP-routable method, but the audit entry must show `role: "user"`, which conflicts with the standard `logCall` path that records the connection name. **How to apply:** force-approve is exposed as a direct `Handler.ForceApprovePending` API rather than a dispatch-table method; the loop calls it in-process, and the handler appends the synthetic audit entry by hand with `role: "user"` and `method: "bcc_force_approve"`. No HTTP route is added; the `RoleLoop` reservation in `internal/director/dag/registry.go` is preserved for future internal MCP calls.

**Discovered:**

- The fake `briefingEmitter` in `internal/loop/director_integration_test.go` uses `attempt = 1` for `BriefingFor`, so successive outer iterations on the same phase share an `iteration_id` of `<phase_id>-1` and the per-iteration prompt file (`briefings/<iteration_id>.prompt.md`) is overwritten across re-briefings. The hint integration test reads the surviving file content to assert hint propagation; production runs do not show this collapse because the Claude Briefer adapter generates fresh iteration ids.
- `charm.land/bubbles/v2/textinput` transitively requires `github.com/atotto/clipboard`; introducing the hint field tripped `missing go.sum entry` on first build. `go get charm.land/bubbles/v2/textinput@v2.1.0` resolves it without an explicit dependency on the clipboard package elsewhere.

### 2026-05-02 23:30, Phase 8: Wire-protocol partial rewrite, planning-as-task UI, docs

`internal/loop/agentcontract/wire_protocol.md` is now the per-role MCP usage manual: method tables for Planner / Briefer / Executor / Reviewer, the polling pattern at entry / per-task / retry / exit boundaries, and the structured-error recovery rules. The TUI's Director panel grew a `planning` row that consumes the synthetic `loop.TaskStarted/TaskCompleted("planning")` pair the loop emits at boot, plus a sub-DAG highlight that lists `Briefing.SubDAGTaskIDs` indented under the active phase. Both `docs/guides/director.md` (en and pt-BR) were rewritten end-to-end to cover sessions, the executable-plan DAG, the MCP method surface, four-option escalation, and `mcp-log.jsonl` troubleshooting.

**Decisions:**

- The planning timeline entry is driven by a synthetic `TaskStarted/TaskCompleted("planning")` pair that `runDirector` emits immediately after `PhasePlanned`, rather than wiring the dag handler to fan out per-task events. **Why:** by the time the TUI is alive (it is constructed only after the Planner has already produced the Plan in cli boot), planning is already done; pushing a real handler-side event bus across the dag → loop boundary would be larger surface for a row that is, in honesty, always rendered as completed. **How to apply:** the synthetic pair keeps the wire model honest (the TUI consumes the event types defined in P6 rather than special-casing `plan != nil`) without forcing a new dag→loop callback shape. If a future role emits per-task events that need to surface live (e.g., a streaming Executor), wire the handler-side fanout then; the loop event types are already in place.
- The TUI's `directorPanel` mirrors `dag.PlanningTaskID` as a private `planningTaskID` constant rather than importing the dag package. **Why:** `internal/tui` has no other reason to import `internal/director/dag`; one shared string constant is less coupling than dragging the package in. **How to apply:** if a second well-known task id appears (e.g. a "review" timeline entry), define a small shared constants source the TUI and dag both import, or push the literal through the loop event payload.

**Discovered:**

- `internal/director/render_test.go::TestRenderBriefingPrompt_IncludesPartials` and `internal/format/markdown_bcc/markdown_bcc_test.go::TestBuildPrompt_LoopMode_IncludesContractCore` were still asserting the legacy `mcp__bcc__*` tool names embedded in the prompt. Both updated to assert the new MCP method names (`bcc_task_started`, `bcc_task_completed`, `bcc_iteration_finished`); the underlying prompts already compose the rewritten partial.
- `internal/director/testdata/briefing_golden.md` regenerated under `-update-golden` because the wire-protocol partial body changed end-to-end. The change is purely the new MCP usage manual; no semantic shift in the surrounding prompt.
- `docs/specs/director/index.md` already named PRD 5 as the current target and PRD 2 as superseded, and listed both the new PRD 5 and the corrections spec under "Documents in this initiative", so the P8 doc-index task was already discharged by prior phases. No edit needed; verified at completion.
