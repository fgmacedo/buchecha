---
title: "Rename director to supervision; split for package cohesion"
type: prd
status: done
authors:
  - Fernando Macedo
reviewers: []
created: 2026-05-08
decision-date: 2026-05-08
supersedes:
superseded-by:
review-by:
tags:
  - refactor
  - architecture
  - supervision
comments: true
---

# Rename director to supervision; split for package cohesion

## Summary

The Go package `internal/director/` accumulated 17 files mixing five distinct reasons to change: wire value objects (Plan, Phase, Task, Briefing), session entities and on-disk persistence (Session, Store), routing and capability tables (Capability, RoleMenus), briefing prompt rendering, and assorted helpers (journal, stats, ids). The name "director" never matched a type in the code. What the package actually holds is the supervision tier: three role ports (Planner, Briefer, Reviewer) that oversee the Executor, plus the artifacts those roles produce.

This work renames the package to `internal/supervision/`, splits it into cohesive subpackages (one reason to change each), isolates `EventService` from `internal/services/` into `internal/services/events/`, and removes a layer leak where `internal/director/dag/tools.go` imports `internal/mcp` (a protocol adapter) for a wire type. The result is a hexagonal core whose package boundaries match the architecture stated in CLAUDE.md.

## Goals

- The supervision tier lives under `internal/supervision/`, with one root package for value objects plus ports, and dedicated subpackages for menu, render, session, stats, journal, dag, claude adapter, and fake adapter.
- Each subpackage has a single reason to change. The package name reflects what the package owns, not where it sits in the build graph.
- `internal/supervision/dag/` no longer imports `internal/mcp`. The DAG advertises tools through a neutral `dag.ToolDescriptor` that the MCP adapter translates to `mcp.Tool` at the boundary.
- `EventService` (ring buffer, fan-out, sequence numbers, persistence, replay) lives in `internal/services/events/`. The `internal/services/` root keeps the CRUD readers (sessions, briefings, prompts), the application-level audit log, error codes, and the small file IO helpers.
- `dag.AuditLog` and `dag.AuditEntry` rename to `dag.MCPLog` and `dag.MCPLogEntry`. The file backing them is `mcp-log.jsonl`, a wire-level dispatch log; the rename eliminates name collision with the application-level `services.Audit`.
- All consumers (cli, tui, api, mcp adapter, executor adapter, loop, services) update their imports and type references in lockstep with each phase. No deprecation aliases or compatibility shims (CLAUDE.md authorizes direct breaking changes for solo work).
- CLAUDE.md is updated to describe the new topology and to drop "director" as a code-level term. The runtime behavior of `bcc run`, `bcc dev`, `bcc init`, and `bcc sessions` is unchanged.

## Non-goals

- Replacing the supervision pattern itself (Planner + Briefer + Reviewer roles): the contract stays.
- Adding new agent vendors or new MCP methods.
- Refactoring `internal/loop/`: it is already cohesive.
- Changing `internal/services/errors.go` or splitting it into a separate package: size does not justify the churn.
- Merging `services.Audit` and `dag.MCPLog`: they are different ledgers with different lifecycles and consumers.

## Domain model

The `Director` term disappears from code identifiers and from CLAUDE.md as a Go-level concept. It survives only as informal English in comments where the cinematographic metaphor is still useful (the supervision tier plans, briefs, and reviews on the Executor's behalf).

The new package layout under `internal/supervision/`:

```
internal/supervision/                 (root: wire value objects + ports)
├── types.go                          Plan, Phase, Task, AcceptanceItem,
│                                     RoleAssignment, PreparedBriefing, Briefing
├── enums.go                          TaskStatus, EvidenceKind
├── briefing.go                       BriefingFor, PendingTaskIDs
├── diff.go                           PlanDiff, RenderPlanDiff
├── ids.go                            SpecHash, PhaseID, TaskID, ComputeSessionHash
├── spawn_id.go
├── ports.go                          Planner, Briefer, Reviewer (+ Inputs, SpawnStats)
├── embed.go                          plan/briefing/verdict JSON schemas
└── supervision.go                    package doc
├── menu/                             Capability, CapabilityRegistry,
│                                     RoleMenu, MenuOption, RoleMenus,
│                                     FillPlanFromMenus, ValidatePlanAgainstMenus
├── render/                           RenderBriefingSystem, RenderBriefingUser,
│                                     briefingView, selectTasks; embedded prompts
├── session/                          Session, SessionStatus, NewSessionID,
│                                     ListSessions, FindSessionsForSpec,
│                                     ResolveSession, Store (with all writers
│                                     and readers for the on-disk layout)
├── stats/                            StatsEntry, StatsLog
├── journal/                          JournalHeading, GatherJournalDelta,
│                                     JournalDeltaProvider, GitDiffer, GatherDiff
├── dag/                              dag.State, dag.Handler, dag.AgentRegistry,
│                                     dag.MCPLog, dag.MCPLogEntry,
│                                     dag.ToolDescriptor; embedded MCP schemas
├── claude/                           Adapter implementing Planner/Briefer/Reviewer
└── fake/                             Fake Planner/Briefer/Reviewer for tests
```

`DirectorCallStats` renames to `SpawnStats`. The renamed type's location is `internal/supervision/ports.go`; consumers in cli, tui, claude adapter, and stats log update in step.

`internal/services/` becomes:

```
internal/services/
├── services.go                       Services aggregator, embeds *events.EventService
├── sessions.go, briefings.go, prompts.go     read APIs
├── audit.go                          application-level Audit (audit.ndjson)
├── errors.go                         error code enum
├── io.go                             small fs helpers
└── events/                           EventService, SeqEvent, MarshalEvent,
                                       IsFinalEvent, ringSize, subscriber, New
```

The leak fix introduces `dag.ToolDescriptor`:

```go
type ToolDescriptor struct {
    Name        string
    Description string
    InputSchema map[string]any
}
```

`dag.Tools()` returns `[]dag.ToolDescriptor`. The MCP adapter (`internal/mcp/server.go`) keeps its existing `mcp.Tool` and gains a one-line conversion at the single call site in `internal/cli/mcp_boot.go`.

## Surfacement

User-facing surfaces do not change. `bcc run`, `bcc dev`, `bcc init`, `bcc sessions list`, and `bcc sessions show <id>` produce the same outputs and the same files on disk. The TUI panels render the same content. The HTTP API serves the same routes with the same payloads. The MCP wire methods keep their names and schemas. The `mcp-log.jsonl` file format is unchanged; only the Go-level type names backing it (`MCPLog`/`MCPLogEntry`) are renamed.

## Migration sequence

The work is structured so each phase ends with a green tree (`go test -race ./...`, `go vet ./...`, `gofmt -l .` empty). Each phase is one commit unless explicitly subdivided.

## P1: break the dag → mcp leak

One task; no rename of any package.

### T1.1: introduce dag.ToolDescriptor and remove the mcp import from dag

Edit `internal/director/dag/tools.go` to define and return `[]ToolDescriptor`. Edit `internal/mcp/server.go` to add a `ToolFromDescriptor(d dag.ToolDescriptor) Tool` helper (or accept the descriptor in the constructor). Edit `internal/cli/mcp_boot.go` (the single call site that wires `dag.Tools()` into the MCP server) to apply the conversion. Acceptance: `internal/director/dag/` imports only stdlib, `internal/director`, `internal/loop/agentcontract`, and `santhosh-tekuri/jsonschema`; `go test ./internal/director/dag/... ./internal/cli/... ./internal/mcp/...` passes.

## P2: rename director to supervision

Mechanical, one large commit. Depends on P1.

### T2.1: move directories and update package declarations

Run `git mv internal/director internal/supervision`. The subdirectories `dag/`, `claude/`, `fake/`, `prompts/`, `schemas/`, `testdata/` move with the parent. Replace `package director` with `package supervision` in every `.go` file under `internal/supervision/` (except `dag/`, `claude/`, `fake/`, which keep their own package names).

### T2.2: update import paths everywhere

Across the repo, replace every import of `github.com/fgmacedo/buchecha/internal/director` with `github.com/fgmacedo/buchecha/internal/supervision`. Same for the `dag`, `claude`, and `fake` subpackages. Use `goimports` or `gofmt` to settle the diff.

### T2.3: rename DirectorCallStats to SpawnStats

Rename the type in `internal/supervision/ports.go`. Update all consumers (the claude adapter, cli stats append, any tests).

Acceptance for P2: `go build ./...` succeeds; `go test -race ./...` passes; no remaining string `internal/director` in the codebase except inside `CLAUDE.md` (which is updated in P5).

## P3: split supervision root into cohesive subpackages

Three sub-commits, one per group, each independently green.

### T3.1: extract supervision/menu

Move `capability.go`, `capability_test.go`, `plan_defaults.go`, `plan_defaults_test.go` from `internal/supervision/` to `internal/supervision/menu/`. Update consumers in `internal/cli/` (where `buildCapabilityRegistry`, `buildRoleMenus`, `FillPlanFromMenus`, `ValidatePlanAgainstMenus` are called).

### T3.2: extract supervision/render

Move `render.go` and `render_test.go` to `internal/supervision/render/`. Move the embedded prompts (`briefingPromptMD`, `briefingSystemMD` and the underlying files in `prompts/briefing.md`, `prompts/briefing_system.md`) into `internal/supervision/render/prompts/` with a local `embed.go`. Update consumers in cli and loop.

### T3.3: extract supervision/session, supervision/stats, supervision/journal in one commit

Move:
- `session.go`, `session_test.go`, `sessions.go`, `sessions_test.go`, `store.go`, `store_test.go` to `internal/supervision/session/`.
- `stats.go`, `stats_test.go` to `internal/supervision/stats/`.
- `journal.go`, `journal_test.go` to `internal/supervision/journal/`.

Update consumers in cli (session creation, plan persistence), loop (journal delta, stats append), services (session lookups, briefing reads), and the claude adapter.

After P3, `internal/supervision/` root contains only the wire value objects, ports, ids, schema embeds, and the package doc.

## P4: split services events and rename dag audit

Two sub-commits.

### T4.1: extract services/events

Create `internal/services/events/`. Move `events.go` and `events_test.go`. Tipos exposed at the new path: `EventService`, `SeqEvent`, `MarshalEvent`, `IsFinalEvent`, `ringSize`, `subscriber`. Constructor: `events.New(deps)`. The aggregator `services.Services` embeds `*events.EventService`. Update consumers in `internal/api/sse.go` and `internal/api/handlers/events.go` (their imports and type references shift from `services.SeqEvent` to `events.SeqEvent`).

### T4.2: rename dag.AuditLog and dag.AuditEntry to dag.MCPLog and dag.MCPLogEntry

Rename in `internal/supervision/dag/audit.go` (and its test). Update consumers in `internal/cli/mcp_boot.go` and `internal/cli/run_director.go` (the only callers that bind the log to a session). The on-disk file `mcp-log.jsonl` is unchanged.

## P5: update CLAUDE.md and remaining text references

### T5.1: rewrite the architecture section

Replace every occurrence of `internal/director/...` with `internal/supervision/...` in CLAUDE.md. Update the "Layers" diagram, the "Rules" list, the DDD glossary entry, and the package responsibilities table to describe the new subpackages. Update the description of `internal/services/` to mention the `events/` subpackage. Drop "Director" as a Go-level term: it remains only as informal English where the cinematographic metaphor (plan, brief, review) is still useful.

### T5.2: ajustar comentários internos remanescentes

Update doc comments in `internal/api/server.go`, `internal/api/mcp_auth.go`, `internal/loop/agentcontract/agentcontract.go`, and `internal/loop/agentcontract/agentevent.go` to reference the new package names where they currently mention "director".

## Verification

Per phase: `go build ./...`, `go vet ./...`, `go test -race ./...`, `gofmt -l .` (empty).

End-to-end after P5:

1. `go install ./cmd/bcc` produces a working binary.
2. `bcc init` runs the wizard interactively and writes a valid `.bcc.toml`.
3. `bcc run testdata/specs/diag-dag.md --output text` against a clean repo: the loop completes; `.bcc/sessions/<id>/` contains `manifest.json`, `plan.json`, `dag.json`, `briefings/<iter>.prompt.md`, `spawns/<spawn>.md`, `mcp-log.jsonl`, `events.ndjson`, `audit.ndjson`, `stats.jsonl`.
4. `bcc dev` (replay-driven) launches and the WebUI renders the recorded events with monotonic `seq`, no regression in the SSE stream.
5. `bcc sessions list` and `bcc sessions show <id>` return the same shape and content as before the refactor.
6. `git grep -n 'internal/director' -- ':!CLAUDE.md.bak'` returns nothing under `internal/`, `cmd/`, or `docs/specs/director/` (the latter directory keeps its name as historical specs but its content references are updated).
7. `git grep -n 'DirectorCallStats'` returns nothing.

## Risks and notes

- P2 produces the largest line-diff but the lowest semantic risk: it is mechanical search-and-replace plus directory moves.
- P3.3 groups three extractions; if any of the three blows up, split it into three sub-commits.
- P4.1 is a hard breaking change to the `services` package surface for the api adapters. CLAUDE.md authorizes direct breaking changes (solo project, no external users), so no compatibility shim is added.
- `supervision/stats/` and `supervision/journal/` are small (one file each plus tests). This is acceptable: each owns a different reason to change and they share no coupling. If after a quarter of evolution they remain trivially small, fold them back into the root; do not preempt that decision now.
