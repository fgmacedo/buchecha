---
title: "Spec: WebUI observability redesign"
type: spec
status: draft
authors:
  - Fernando Macedo
reviewers: []
created: 2026-05-05
decision-date:
superseded-by:
supersedes:
review-by:
tags:
  - webui
  - observability
  - spawns
  - cost
  - prompts
comments: true
---

# Spec: WebUI observability redesign

## Goal

Make the embedded web dashboard a trustworthy observability instrument for an autonomous bcc run. A user watching the dashboard must be able to answer four questions at any moment without leaving the browser:

1. What is happening right now (which role, which phase, which task)?
2. What did this iteration cost, in tokens and USD, broken down by role?
3. Why did the agent do what it did (the exact prompt and briefing it received)?
4. What state is each phase and task in, and how did it get there?

The current dashboard ships V1 of the read-only roadmap from `docs/specs/webui/2026-05-04-embedded-web-dashboard.md`. It has all the data plumbing but the presentation does not yet support those four questions: the DAG renders as a flat dark canvas with no per-node inspection, the timeline is a generic event list, prompts are exposed only as global per-role templates, and cost is not surfaced anywhere even though `agent_event.result_summary` already carries it for the executor.

This spec adds the loop, adapter, service, and SPA changes that close those gaps as one coherent redesign. No regressions on the V1 read-only contract or on existing endpoints. No new mutating endpoints (those still ride V2).

## Context

The dashboard's V1 was scoped to "static read-only inspection". Real use surfaces five concrete deficits that the redesign addresses:

1. **Information architecture is flat.** Header / Sessions sidebar / Main / Timeline / Briefing-drawer-at-bottom each owns a slice of context, but none of them composes the others. Selecting a task in the DAG does not focus the timeline. Opening the briefing drawer hides part of the canvas. There is no single object the user can anchor on.
2. **Prompts cannot be inspected per spawn.** `PromptService.Get(role)` reads `<sessionDir>/prompts/<role>.md`, the global system prompt template. There is no way to see the resolved prompt the briefer of phase `P3` received on its second attempt, or the planner's prompt with the resolved spec hash. The briefing-drawer's Prompts tab can only show four templates, no per-spawn body.
3. **The DAG canvas reads as a black slab.** `--color-background: #0b0d10` covers the full ReactFlow viewport with no gradation, no surface hierarchy, no texture. Phase containers and task nodes are flat dark cards that disappear into the background. No click handler opens a detail view; the popover-on-hover dies as soon as the cursor leaves the node.
4. **The timeline does not use event types.** All twelve `loop.AllEventKinds` collapse into a single rendering. `agent_event.tool_use` and `agent_event.tool_result` are not paired. `agent_event.result_summary` is rendered as plain text, not as an iteration cost summary. Phase boundaries and iteration boundaries are not visually distinct. There is no grouping, no filtering, no search.
5. **Cost is invisible.** `agent_event.result_summary` already emits `total_cost_usd`, `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens` from the executor spawn. The TUI aggregates this into `internal/tui/health.go`. The dashboard does nothing with it. Director-role spawns (Planner, Briefer, Reviewer) compute the same metrics internally via `director.CallStats` but those metrics never reach `internal/loop/Event` or the API.

The redesign treats the spawn as a first-class wire concept. Every role invocation becomes a pair of events (`spawn_started`, `spawn_finished`) carrying the resolved prompt path and the resolved cost. The dashboard composes those events into:

- A header CostMeter that aggregates USD and tokens per session and per iteration.
- A unified RightPane that is a Timeline by default and an Inspector when a node is selected.
- An Inspector that resolves spawns by phase / task / iteration so the Prompts tab lists every spawn the role made and renders the exact body each one consumed.
- A DAGView with three surface levels (canvas, panel, card), gradation, and click-to-inspect.

Stack stays as in V1: React 19, TypeScript 5.7, Vite 6, Tailwind v4, wouter, xyflow + dagre, d3 for the Gantt, shiki for markdown, Geist Sans + Geist Mono + Instrument Serif self-hosted. No new heavy frontend dependencies. No new Go dependencies.

## Cross-cutting requirements

These apply to every task:

1. All code, comments, identifiers, prompts, and commit messages in English. Portuguese only in conversation.
2. The em-dash character (Unicode U+2014) is forbidden in prose anywhere in the repo. Use commas, periods, colons, or rephrase.
3. `gofmt -l .` produces no output before any commit.
4. `go vet ./...` reports zero issues before any commit.
5. `go test -race ./...` passes before any commit that touches concurrent code.
6. Commit messages use lowercase prefixes matching git log style: `loop:`, `director:`, `executor:`, `services:`, `api:`, `webui:`, `cli:`, `docs:`, `refac:`. One commit per task; one merged set per phase.
7. Working tree is clean between phases.
8. Each task's acceptance criteria is verifiable by `go test`, `make build`, `curl`, or explicit manual inspection of the dashboard. Criteria like "should look better" are forbidden.
9. The anti-drift contract documented in `CLAUDE.md` (`loop.AllEventKinds`, `MarshalJSONEvent` switch, `internal/api/schemas/event.schema.json` enum, sample table in `TestMarshalJSONEvent_AllKindsCovered`) is enforced for every new event kind.
10. The CI bundle gate of 600 KB gzipped for the SPA holds. New code-splits if needed; no new heavy vendor chunks.
11. No new top-level Go package outside `internal/`.
12. Layer boundaries from `CLAUDE.md` hold: `internal/loop/`, `internal/director/`, `internal/director/dag/`, `internal/config/` import no adapter; the API and MCP adapters consume only `internal/services/`.

## Phases

Eleven phases. Sequencing follows the dependency graph at the end of this section.

### [x] P1: Spawn events on the loop wire

**id**: `P1-spawn-events`
**intent**: Introduce `SpawnStarted` and `SpawnFinished` as new loop event kinds, with the JSON serialization, schema, and tests that every wire-level event in this codebase already has. No emission yet; the producers in adapters land in P2 and P3.
**scope_in**: `internal/loop/`, `internal/api/schemas/event.schema.json`.
**scope_out**: any change in `internal/director/claude/`, `internal/executor/claude/`, `internal/services/`, `internal/api/handlers/`, or the SPA.
**depends_on**: none.

#### [x] T1.1: `SpawnStarted` and `SpawnFinished` types

**acceptance_criteria**:
- `internal/loop/events.go` declares `SpawnStarted` and `SpawnFinished` with the `isLoopEvent()` marker. Fields:
  - `SpawnStarted{ SpawnID string; Role string; PhaseID string; TaskID string; IterationID string; Attempt int; Model string; Effort string; PromptPath string; At time.Time }`
  - `SpawnFinished{ SpawnID string; Role string; ExitCode int; DurationMS int64; Cost SpawnCost; At time.Time }`
- `SpawnCost` struct in the same file: `{ InputTokens, OutputTokens, CacheReadTokens, CacheCreateTokens int; USD float64 }`. Empty values mean "not reported by the adapter" (e.g. spawn aborted before the result summary line).
- `Role` accepts `"planner" | "briefer" | "executor" | "reviewer"` (string for forward compatibility; validation lives in adapters).
- `SpawnID` is opaque to the loop. Producers generate it; consumers use it as a correlation key.
- `PhaseID`, `TaskID`, `IterationID`, `Attempt` are all optional (`""` or `0` mean "not applicable", e.g. the Planner has no phase).
- Package compiles with `go build ./internal/loop/...`.

**context**: The SpawnID model mirrors how the existing `agent_id` is used in the MCP registry, but lives at the event layer. The loop does not register spawn ids; it only forwards them.
**depends_on**: none.

#### [x] T1.2: JSON serialization

**acceptance_criteria**:
- `internal/loop/eventjson.go` extends `MarshalJSONEvent` with cases for `SpawnStarted` and `SpawnFinished`. The wire payloads:
  - `spawn_started`: `{ type: "spawn_started", at, level, payload: { spawn_id, role, phase_id?, task_id?, iteration_id?, attempt?, model?, effort?, prompt_path } }`. Empty optional fields are omitted from the JSON.
  - `spawn_finished`: `{ type: "spawn_finished", at, level, payload: { spawn_id, role, exit_code, duration_ms, cost: { input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens, usd } } }`. The `cost` object is always present; zero values are emitted as `0`.
- `internal/loop/AllEventKinds` slice gains `"spawn_started"` and `"spawn_finished"` in alphabetical order.
- `level` defaults to `"info"` for both, override path the same as the other event kinds.
- `internal/loop/eventjson_test.go` adds two rows in `TestMarshalJSONEvent_AllKindsCovered` with golden JSON.

**context**: The anti-drift contract mandates that any new `Event` variant updates four places at once: the marshal switch, `AllEventKinds`, the schema enum, and the sample table. Two tests fail loudly when any drifts.
**depends_on**: T1.1.

#### [x] T1.3: Event schema

**acceptance_criteria**:
- `internal/api/schemas/event.schema.json` adds `"spawn_started"` and `"spawn_finished"` to the `type` enum.
- A `oneOf` branch is added for each new kind under `payload`, with the field set from T1.2 and types matching JSON Schema draft-07 (or whatever the existing schema declares).
- `cost` becomes a reusable `$defs/SpawnCost` referenced from the `spawn_finished` branch.
- The schema validates all golden samples emitted by T1.2 (round-trip test using `santhosh-tekuri/jsonschema`).
- `go test ./internal/api/...` passes.

**context**: The SPA fetches this schema at runtime to discriminate event kinds. Drift here breaks the timeline silently in production.
**depends_on**: T1.2.

#### [x] T1.4: Loop integration test

**acceptance_criteria**:
- `internal/loop/eventjson_test.go` adds a unit test that asserts `loop.AllEventKinds` matches the `type` enum in the embedded `event.schema.json` exactly (already covered by the existing drift test; extend to cover the two new kinds).
- `go test -race ./internal/loop/...` passes.

**context**: This is the canary that catches the schema/code drift when one place is updated without the other.
**depends_on**: T1.2, T1.3.

### [x] P2: Per-spawn prompt persistence

**id**: `P2-spawn-prompts`
**intent**: Every role spawn writes its resolved prompt to disk before the subprocess starts, under `.bcc/sessions/<id>/spawns/<spawn_id>.md`. The path is recorded in `SpawnStarted.PromptPath`. This makes "what prompt did this exact spawn receive" answerable from the SPA without reaching into the agent CLI.
**scope_in**: `internal/director/claude/claude.go`, `internal/executor/claude/claude.go`, `internal/director/session.go` (only if a tiny path helper is needed there).
**scope_out**: `internal/loop/`, `internal/services/`, `internal/api/`, the SPA.
**depends_on**: P1.

#### [x] T2.1: Spawn directory contract

**acceptance_criteria**:
- `internal/director/session.go` (or the closest existing helper) exposes `SpawnsDir() string` returning `<sessionDir>/spawns`. Created lazily by the first spawn that writes there.
- Spawn ID format: ULID lowercase (use the existing ULID helper if present in `internal/director/`, otherwise a small inline implementation). Stable lexicographic order matches creation order.
- File path: `<sessionDir>/spawns/<spawn_id>.md`. The body is the exact bytes the agent process receives on stdin (or via `--system-prompt-file`, depending on the adapter's mechanism).
- Tests: a unit test creates a fake session and writes two prompts, asserts files exist with expected bytes.

**context**: Centralizing the path under the session keeps `bcc sessions show <id>` discoverable.
**depends_on**: P1.

#### [x] T2.2: Director adapter writes prompts and emits `spawn_started`

**acceptance_criteria**:
- `internal/director/claude/claude.go` `runRole` (around line 317) generates a SpawnID, writes the resolved prompt bytes to `<spawnsDir>/<spawn_id>.md` before the agent process starts, and emits `loop.SpawnStarted` on the events channel with the populated fields.
- `Role` is `"planner"`, `"briefer"`, or `"reviewer"`. `IterationID`, `PhaseID`, `Attempt` are populated from the call site (the Briefer call carries phase + attempt; the Reviewer too; the Planner has none).
- `Model` and `Effort` reflect the resolved per-spawn capability values (the same fields already passed to the agent CLI).
- The events channel is the existing per-run `chan loop.Event` already accessible to the adapter via `Deps`.
- Tests: a fake stdout/stdin pipe in `claude_test.go` asserts (1) the file exists after the spawn starts, (2) `SpawnStarted` is emitted with the matching path, (3) the SpawnID matches the file basename.

**context**: The Director adapter already gets a `stderrFactory` per spawn. Adding a prompt write at the same hook is mechanically similar.
**depends_on**: T2.1.

#### [x] T2.3: Executor adapter writes prompts and emits `spawn_started`

**acceptance_criteria**:
- `internal/executor/claude/claude.go` `Run` writes the resolved system+user prompt bytes to `<spawnsDir>/<spawn_id>.md` before the subprocess starts, and emits `loop.SpawnStarted` with `Role: "executor"`, `IterationID`, `PhaseID`, `Attempt` populated from the briefing context.
- The same SpawnID is recorded on `ExecResult` as `SpawnID string` (new field), so the loop can correlate the matching `SpawnFinished` (P3) without parsing events.
- Tests: a unit test using the existing testdata fixture asserts the file exists, `SpawnStarted` is emitted, and `ExecResult.SpawnID` matches.

**context**: Executor spawns are the heaviest cost contributor; making their prompts inspectable is the highest-value half of P2.
**depends_on**: T2.1.

### [x] P3: Per-spawn cost emission

**id**: `P3-spawn-cost`
**intent**: Every role spawn emits `SpawnFinished` with the cost extracted from the agent's `result_summary` (or zero values if the agent exited before reporting one). For the Executor adapter, the data already flows through the stream-json parser and surfaces in `agent_event.result_summary`; this phase wires the same parse output into a typed `SpawnFinished`. For Director adapters, the parser is reused.
**scope_in**: `internal/director/claude/claude.go`, `internal/executor/claude/claude.go`, `internal/executor/claude/streamjson/`.
**scope_out**: `internal/services/`, `internal/api/`, the SPA, `internal/tui/` (TUI keeps consuming `agent_event.result_summary` for now; SPA aggregator uses the new events).
**depends_on**: P1, P2.

#### [x] T3.1: Stream-json result extraction helper

**acceptance_criteria**:
- `internal/executor/claude/streamjson/` (existing package) exports a typed helper `LastResultSummary(events []AgentEvent) (Cost, bool)` returning the cost values plus a `found` flag. The helper does not modify the parser; it scans the parsed events.
- `Cost` struct mirrors `loop.SpawnCost` field-by-field (avoid a circular import: define `Cost` here, convert at the call site).
- Tests: a fixture stream that includes a `result_summary` line returns the matching values; a stream without one returns `(_, false)`.

**context**: Pulling the helper into the streamjson package keeps both adapters using the same extraction logic.
**depends_on**: P2.

#### [x] T3.2: Director adapter emits `spawn_finished`

**acceptance_criteria**:
- `internal/director/claude/claude.go` `runRole` emits `loop.SpawnFinished` after `cmd.Wait` returns, with `SpawnID` matching the `SpawnStarted` from T2.2, `Role`, `ExitCode` from the process, `DurationMS` measured around the spawn, and `Cost` populated from `streamjson.LastResultSummary` over the parsed agent events the adapter already reads.
- If the spawn exits non-zero before reporting a result, `SpawnFinished` still emits with `Cost{}` (zero) and the actual `ExitCode`.
- Tests: a fixture stdout with a result_summary line produces a `SpawnFinished` carrying the expected USD; a fixture without one produces zero.

**context**: Director spawns are short and currently invisible in cost dashboards; this closes the gap.
**depends_on**: T2.2, T3.1.

#### [x] T3.3: Executor adapter emits `spawn_finished`

**acceptance_criteria**:
- `internal/executor/claude/claude.go` `Run` emits `loop.SpawnFinished` after the subprocess exits, with `SpawnID` matching `ExecResult.SpawnID` and the same fields as T3.2.
- The existing `agent_event.result_summary` event continues to be emitted for backward compatibility with the TUI.
- Tests: extend `claude_test.go` to assert both events fire on a happy-path spawn and that costs match across them.

**context**: For one transition window the executor will emit both `agent_event.result_summary` (for the TUI) and `spawn_finished` (for the SPA). The TUI migration to `spawn_finished` is deferred to a later spec.
**depends_on**: T2.3, T3.1.

### [x] P4: Per-spawn prompt service and HTTP endpoint

**id**: `P4-prompt-service`
**intent**: Add `PromptService.GetSpawn(sessionID, spawnID)` and the matching `GET /api/v1/sessions/{id}/spawns/{spawnId}/prompt` endpoint. The dashboard uses this to render the body of any spawn the user clicks.
**scope_in**: `internal/services/prompts.go`, `internal/services/prompts_test.go`, `internal/api/handlers/`, `internal/api/openapi.json`.
**scope_out**: SPA changes; existing `GET /prompts/{role}` stays as-is.
**depends_on**: P2.

#### [x] T4.1: `PromptService.GetSpawn`

**acceptance_criteria**:
- `internal/services/prompts.go` adds `GetSpawn(ctx, sessionID, spawnID string) (Prompt, error)`.
- `Prompt` struct gains `SpawnID string` field, populated by `GetSpawn`. `Role` is left as-is when the service cannot resolve it from the path alone (V1 leaves `Role` empty for spawn-fetched prompts; the SPA already knows the role from the originating event).
- File path resolution: `<sessionDir>/spawns/<spawn_id>.md`. Missing file returns `ErrRoleNotFound` (reuse the closest existing canonical error; no new error code added in V1).
- SpawnID is validated against `^[0-9a-z]{16,32}$` (ULID-shaped) before any filesystem call; non-matching ids return `ErrInvalidRequest`.
- Tests: happy path, unknown session, unknown spawn id, malformed spawn id.

**context**: The validator gates filesystem access against path traversal; the rest is straightforward.
**depends_on**: P2.

#### [x] T4.2: `GET /api/v1/sessions/{id}/spawns/{spawnId}/prompt`

**acceptance_criteria**:
- `internal/api/handlers/prompts.go` (or extend the existing prompt handler) registers the new route.
- Response: `Content-Type: text/markdown; charset=utf-8`, body is the raw markdown.
- Errors map per the standard envelope: `404 not_found` for unknown session, `404 not_found` for unknown spawn, `400 invalid_request` for malformed spawn id.
- OpenAPI document regenerated (`make api-openapi`); the new route appears in `internal/api/openapi.json`.
- Integration test: end-to-end through `httptest.NewServer` covers happy path and the two miss cases.

**context**: Mirrors the shape of the existing `GET /sessions/{id}/prompts/{role}` handler.
**depends_on**: T4.1.

### [x] P5: Visual foundation

**id**: `P5-visual-foundation`
**intent**: Refresh the design tokens to give the dashboard surface depth and clear hierarchy. Three surface levels (canvas, panel, card), an elevated state for hover/selection, intentional use of Instrument Serif italic for editorial accents, and accent colors reserved for signal (cost, escalation). No layout or component changes yet; subsequent phases consume these tokens.
**scope_in**: `internal/webui/web/src/styles/tokens.css`, `internal/webui/web/src/styles/index.css`.
**scope_out**: any `.tsx` change.
**depends_on**: none.

#### [x] T5.1: Surface and elevation tokens

**acceptance_criteria**:
- `tokens.css` declares: `--surface-canvas: #08090b`, `--surface-panel: #101316`, `--surface-card: #16191d`, `--surface-elevated: #1d2126`, `--surface-overlay: #0d0f12cc` (translucent for popovers).
- Border tokens: `--border-subtle: #1c1f24`, `--border-default: #262a30` (the current value), `--border-strong: #353a42`.
- The legacy `--color-background` and `--color-muted` are kept as aliases of `--surface-canvas` and `--surface-panel` so existing components keep rendering unchanged until they are migrated.
- The existing status palette (`--status-pending` through `--status-error`) is unchanged; one new token `--accent-warn: #f5b840` reserved for cost spikes and escalation banners.
- Visual smoke test: `pnpm dev` renders without console errors; existing pages look identical (because legacy aliases are in place).

**context**: Tokens land first so subsequent components consume them. Aliases avoid a big-bang rename.
**depends_on**: none.

#### [x] T5.2: Typography refinement

**acceptance_criteria**:
- `tokens.css` adds `--font-display: "Instrument Serif", ui-serif, Georgia, serif;` and a `--font-numeric: "Geist Mono", ...` alias for explicit numeric runs.
- A new utility class `.font-display` is added to the Tailwind layer (`@layer utilities`) bound to `var(--font-display)`. Same for `.font-numeric`.
- The Instrument Serif italic face is preloaded via `<link rel="preload">` in `index.html` to avoid FOIT on the first cost number render.
- Visual smoke test: a temporary `<h1 className="font-display italic">` in `app.tsx` renders with the serif italic face; remove the smoke test before commit.

**context**: Instrument Serif italic at 28px and above gives the dashboard an editorial cadence without leaving the technical aesthetic.
**depends_on**: T5.1.

#### [x] T5.3: Canvas texture utility

**acceptance_criteria**:
- A reusable CSS-only background pattern is added under `styles/index.css`: a radial gradient between `--surface-canvas` and `transparent` plus a low-opacity SVG noise data-URL, exposed as a utility class `.bg-canvas-textured`.
- No external assets; the SVG is inlined as a data URI. Total CSS cost stays under 1 KB.
- Visual smoke test: the class applied to a full-screen div produces a perceptible but subtle gradation (not a uniform black).

**context**: The DAG canvas in P7 consumes this; isolating the pattern here keeps it reusable for the ActivityView background later.
**depends_on**: T5.1.

### [x] P6: CostMeter

**id**: `P6-cost-meter`
**intent**: Surface session-wide cost in USD and tokens at all times in the header, with a popover that breaks down the total per role and per iteration. The aggregator consumes the new `spawn_finished` events.
**scope_in**: `internal/webui/web/src/hooks/use-cost-aggregator.ts` (new), `internal/webui/web/src/components/cost-meter/` (new), `internal/webui/web/src/components/header/`.
**scope_out**: anything outside header and the new aggregator.
**depends_on**: P3, P5.

#### [x] T6.1: Cost aggregator hook

**acceptance_criteria**:
- `hooks/use-cost-aggregator.ts` exports `useCostAggregator(events: SeqEvent[]): CostAgg`.
- `CostAgg`: `{ totalUSD: number; totalTokens: { input, output, cacheRead, cacheCreate }; perRole: Record<Role, { usd, tokens }>; perIteration: Array<{ iterationIndex, usd, tokens }> }`.
- The hook is memoized on the events array length (not deep), recomputes incrementally as new events arrive.
- It only reads `spawn_finished` events; everything else is ignored.
- Unit test in `hooks/use-cost-aggregator.test.ts` (Vitest) covers: empty input, single role, multi-role, multiple iterations.

**context**: The hook stays a pure derivation; the events array is the single source of truth.
**depends_on**: P3.

#### [x] T6.2: CostMeter component

**acceptance_criteria**:
- `components/cost-meter/index.tsx` exports `<CostMeter agg={CostAgg} />`.
- Default state: a horizontal pill with the USD total in `font-display italic` (Instrument Serif), tokens (input/output) in `font-numeric` next to it, and a 24px wide sparkline of USD per iteration (inline SVG, no library).
- Hover/click opens a popover (`<Popover>` from a tiny existing primitive or a 30-line custom one) listing per-role rows with role name, USD, tokens, percentage of session total. A divider then lists per-iteration rows.
- Tooltip on the sparkline shows the iteration index and USD on hover.
- Storybook is not required; the component renders correctly in `app.tsx` against fixture events for visual review.
- Unit test asserts the rendered USD matches the aggregator's `totalUSD` to two decimals.

**context**: Restraint matters; the pill must read at a glance and only escalate to the popover on intent.
**depends_on**: T6.1, T5.2.

#### [x] T6.3: Integrate into Header

**acceptance_criteria**:
- `components/header/index.tsx` consumes `useCostAggregator(events)` and renders `<CostMeter agg={...} />` aligned to the right of the view toggle.
- Layout: status badge | iter index | spec path | view toggle | CostMeter.
- The header height does not grow; the CostMeter fits within the existing 48px row.
- Visual review: the pill renders with the correct number on a session that has at least one `spawn_finished` event in the events ring buffer.

**context**: The header is the always-visible surface; cost lives there because the user needs to see it without clicking anywhere.
**depends_on**: T6.2.

### [ ] P7: Right pane unified surface

**id**: `P7-right-pane`
**intent**: Replace the current split between `TimelinePanel` and the bottom `BriefingPanel` with a single right-side pane that shows the Timeline by default and switches to the Inspector when a node is selected. The Inspector shell is a placeholder here; phase P9 fills the tabs.
**scope_in**: `internal/webui/web/src/app.tsx`, `internal/webui/web/src/components/right-pane/` (new), `internal/webui/web/src/components/timeline-panel/` (renderers refactored under right-pane), `internal/webui/web/src/hooks/use-selection.ts` (new), `internal/webui/web/src/lib/event-grouping.ts` (new).
**scope_out**: DAG and ActivityView changes (P8, P10).
**depends_on**: P1, P5.

#### [ ] T7.1: Selection state

**acceptance_criteria**:
- `hooks/use-selection.ts` exports `useSelection()` returning `{ selection: Selection | null; select: (s: Selection | null) => void }`.
- `Selection` discriminated union: `{ kind: "phase"; phaseId } | { kind: "task"; phaseId; taskId } | { kind: "iteration"; iterationId } | { kind: "spawn"; spawnId; role; phaseId? }`.
- The hook is backed by a small Zustand store or equivalent (a hand-rolled context hook is fine; no new dependency).
- Selection survives view switches (DAG ↔ Activity) but resets on session change.
- Unit test asserts state transitions.

**context**: Selection is shared across DAGView, ActivityView, and the Inspector. Co-located in a hook for test friendliness.
**depends_on**: none.

#### [ ] T7.2: RightPane container

**acceptance_criteria**:
- `components/right-pane/index.tsx` exports `<RightPane events={SeqEvent[]} snapshot={Snapshot} />`.
- Internally consumes `useSelection`. When `selection === null`, renders `<TimelineMode events={events} />`. Otherwise renders `<InspectorMode selection={selection} events={events} snapshot={snapshot} onClose={() => select(null)} />`.
- Transition between the two modes is a 150ms cross-fade (Tailwind `transition-opacity`).
- The container occupies the full height of the right column from `app.tsx`.
- Visual review: the pane renders Timeline by default; calling `select({ kind: "phase", phaseId: "P1" })` from the React DevTools console swaps to the Inspector shell.

**context**: Wrapping behind a single component keeps `app.tsx` clean and the swap deterministic.
**depends_on**: T7.1.

#### [ ] T7.3: Typed timeline renderers

**acceptance_criteria**:
- `components/right-pane/timeline/` contains one renderer per event kind family:
  - `iter-divider.tsx` for `iter_started`, `iter_finished`, `loop_finished`.
  - `phase-card.tsx` for `phase_planned`, `phase_briefed`, `phase_reviewed`, `director_escalation`.
  - `task-line.tsx` for `task_started`, `task_completed`, `task_approved`, `task_needs_fix`.
  - `agent-block.tsx` for `agent_event` with internal discrimination on `event.payload.kind` (init, thinking, assistant_text, tool_use, tool_result, rate_limit, result_summary). `tool_use` and `tool_result` are paired (rendered as one collapsible block).
  - `spawn-marker.tsx` for `spawn_started` and `spawn_finished`. Click selects `{ kind: "spawn", spawnId, role, phaseId }`.
- Each renderer is a pure function of one `SeqEvent`; no IO.
- Visual: `iter-divider` is a horizontal rule with index and duration; `phase-card` is a `--surface-card` block with phase id mono and outcome pill; `task-line` is a single dense line; `agent-block` collapses long text to three lines with an expand chevron; `spawn-marker` is a small pill with role + cost USD.
- Unit tests: a fixture event of each kind renders without error; the `tool_use`/`tool_result` pairing logic is covered by a 4-event sample.

**context**: Each renderer owning its visual makes adding new event kinds in the future a single-file change.
**depends_on**: T7.2, T1.2.

#### [ ] T7.4: Iteration grouping and filters

**acceptance_criteria**:
- `lib/event-grouping.ts` exports `groupByIteration(events: SeqEvent[]): IterationGroup[]` where `IterationGroup` is `{ iterationIndex; iterationId; from: Date; to: Date | null; events: SeqEvent[]; summary: { tasksDone, tasksNeedsFix, usd, durationMS } }`.
- The function is incremental: O(n) with no allocation per event when extending the last group.
- Timeline mode renders one collapsible section per group with a sticky header showing the summary in compact form.
- Filter controls: a top bar with three multi-selects (Kind, Role, Phase) and a level toggle (info / warn / error). Filters persist in `localStorage` per session.
- Search box filters renders to events whose payload JSON contains the substring.
- Unit tests cover grouping with an open final iteration and filter combinations.

**context**: Grouping is the highest-leverage UX win; it turns 800 events per iteration into a scannable summary.
**depends_on**: T7.3.

#### [ ] T7.5: Wire RightPane into AppShell

**acceptance_criteria**:
- `app.tsx` removes `<TimelinePanel>` and `<BriefingPanel>` mount points; the right column renders `<RightPane events={events} snapshot={snapshot} />`.
- The bottom drawer is gone; the layout grid drops the third row.
- The `BriefingPanel` component is deleted (its `PromptsTab` migrates to P9; there is no V1 user that loses access because the redesign ships in one merge).
- Visual review: archived session URL `/archived/<id>` renders the new pane with all events grouped by iteration.

**context**: This is the user-visible flip. After this task, the Inspector is a shell; P9 fills it.
**depends_on**: T7.4.

### [ ] P8: DAGView refactor with selection

**id**: `P8-dag-view`
**intent**: Rebuild the phase and task nodes to be readable, inspectable, and visually layered. Click a phase or task to focus it in the Inspector via the selection hook.
**scope_in**: `internal/webui/web/src/components/dag-view/`.
**scope_out**: ActivityView, RightPane internals.
**depends_on**: P5, P7.

#### [ ] T8.1: Phase node redesign

**acceptance_criteria**:
- `components/dag-view/phase-node.tsx`:
  - Header row: phase id in `font-mono font-bold`, `depends_on` chips, status pill (aggregated from task statuses: any error → error; any needs_fix → needs_fix; any in_progress → running; all done → done; else pending).
  - Body: a 4xN grid of task chips (one cell per task, colored by status), grouped by row of 4. Hover a chip → tooltip with task id and status.
  - Footer: `tasks done/total`, attempt counter, total USD spent in this phase (consume the cost aggregator filtered by phase id).
  - Background: `--surface-card`; selected/hovered: `--surface-elevated`.
  - Click anywhere on the header invokes `select({ kind: "phase", phaseId })`.
- The component is a pure function of `{ phase, tasks, costAgg, selected }`; no fetches.
- Visual review against a fixture session: phases read at-a-glance.

**context**: The phase node is the entry point most users interact with; investing in its hierarchy pays back.
**depends_on**: P5, T7.1.

#### [ ] T8.2: Task node redesign

**acceptance_criteria**:
- `components/dag-view/task-node.tsx`:
  - Compact card with task id mono, status pill, retry budget rendered as small dots (filled = remaining, hollow = used), attempt indicator if attempt > 1.
  - Hover: tooltip shows `depends_on` and the started/ended timestamps derived from `task_started` / `task_completed` events the parent passes down.
  - Click: `select({ kind: "task", phaseId, taskId })`. Selected state outlines the card with `--accent-warn` when the task is currently in `needs_fix`, `--status-running` otherwise.
- Visual review: a phase with eight tasks renders cleanly.

**context**: Status pill at the same place across nodes is non-negotiable for quick scanning.
**depends_on**: T8.1.

#### [ ] T8.3: Canvas background and minimap

**acceptance_criteria**:
- `components/dag-view/index.tsx` applies `bg-canvas-textured` from T5.3 to the ReactFlow viewport wrapper.
- The xyflow `<MiniMap>` is enabled with custom node colors keyed off status; it sits in the bottom-right and uses `--surface-overlay` background.
- Zoom controls move to the bottom-left and use `--surface-card` background.
- Visual review: the canvas no longer reads as a flat slab; the minimap reflects status colors.

**context**: The textured background plus the colored minimap together fix the "black box" perception.
**depends_on**: T5.3.

#### [ ] T8.4: DAG selection round-trip

**acceptance_criteria**:
- Clicking a phase or task in the DAG transitions the RightPane from Timeline mode to Inspector mode (the shell from P7).
- Pressing `Escape` clears the selection and returns to Timeline.
- An end-to-end Vitest test using `@testing-library/react` mounts `app.tsx` against a fixture, simulates a click, and asserts the Inspector shell renders.

**context**: This closes the loop from canvas to detail.
**depends_on**: T8.1, T8.2, T7.5.

### [ ] P9: Inspector tabs

**id**: `P9-inspector`
**intent**: Fill the Inspector with four tabs: Overview, Briefing, Prompts, Events. The Prompts tab consumes the per-spawn endpoint from P4; the Briefing tab reuses the existing endpoint with attempt selection; the Events tab reuses the timeline renderers with a phase/task filter; the Overview tab synthesizes metadata.
**scope_in**: `internal/webui/web/src/components/right-pane/inspector/`.
**scope_out**: any change outside the Inspector.
**depends_on**: P4, P7, P8.

#### [ ] T9.1: Overview tab

**acceptance_criteria**:
- `inspector/overview-tab.tsx` renders the selected node's metadata:
  - For `kind: "phase"`: phase id, depends_on, status pill, task count by status, total USD in phase, attempt history (one row per attempt with outcome and duration derived from `phase_briefed` + `phase_reviewed` events).
  - For `kind: "task"`: task id, status, retry budget remaining, depends_on, start/end timestamps derived from events, USD attributable to the iteration the task ran in.
  - For `kind: "spawn"`: spawn id, role, model, effort, prompt path, exit code, duration, cost.
- All values are computed from `events` and `snapshot`; no new API calls.

**context**: Metadata first; the deeper tabs follow.
**depends_on**: P7, P8.

#### [ ] T9.2: Briefing tab

**acceptance_criteria**:
- `inspector/briefing-tab.tsx` shows the briefing markdown for the selected phase.
- An attempt selector at the top (`1, 2, 3, ...`) drives `GET /sessions/{id}/briefings/{phase}/{attempt}`; the markdown body renders with shiki for syntax highlighting (already configured for V1).
- Loading state is a single skeleton row; error state shows the canonical envelope's `message` field.
- Disabled when the selection is not a phase or task (gray out the tab label).
- Visual review against an archived session: the briefing for `P3 attempt 1` and `P3 attempt 2` both load on demand.

**context**: Reuses the existing endpoint and the same shiki setup; only the host changes.
**depends_on**: T9.1.

#### [ ] T9.3: Prompts tab

**acceptance_criteria**:
- `inspector/prompts-tab.tsx` lists every spawn associated with the current selection:
  - For a task selection, all spawns whose `task_id` matches.
  - For a phase selection, all spawns whose `phase_id` matches.
  - For a spawn selection, the single matching spawn.
- The list is built from `spawn_started` events filtered by selection. Each row shows: timestamp, role pill, model, effort, attempt, USD (from the matching `spawn_finished`).
- Clicking a row fetches `GET /api/v1/sessions/{id}/spawns/{spawnId}/prompt` and renders the markdown body in a right-side pane within the tab.
- The currently rendered spawn id is reflected in the URL hash (`#spawn=<id>`) so a deep-link to a specific prompt works.
- A "Copy" button copies the rendered markdown to the clipboard.
- Visual review: a phase with three attempts shows nine spawns (three briefer + three executor + three reviewer); clicking any one resolves the body within 200ms locally.

**context**: This is the headline feature of the redesign. The list-then-body interaction makes prompt diffing across attempts trivial.
**depends_on**: P4, T9.1.

#### [ ] T9.4: Events tab

**acceptance_criteria**:
- `inspector/events-tab.tsx` reuses the timeline renderers from P7 but pre-filters the event stream to those matching the selection (`phase_id`, `task_id`, or `spawn_id`).
- The tab inherits the same filter controls and grouping as the Timeline mode but with the scope locked.
- Visual review: selecting `T3.1` shows only events that mention `T3.1` plus the iteration boundaries that contain them.

**context**: Sharing renderers with the Timeline keeps the visual contract stable.
**depends_on**: T7.3, T9.1.

#### [ ] T9.5: Tab strip and keyboard

**acceptance_criteria**:
- `inspector/index.tsx` renders the tab strip (Overview / Briefing / Prompts / Events) with the selected tab persisted in `localStorage` keyed by selection kind.
- Keyboard: `1`/`2`/`3`/`4` switch tabs; `Escape` closes the inspector and returns to Timeline mode (already wired in P8.4).
- Tab labels show a small badge with the count for tabs that aggregate (Prompts shows the spawn count, Events shows the event count, Briefing shows the attempt count).

**context**: Keyboard fluency is what makes a power user instrument useful.
**depends_on**: T9.1, T9.2, T9.3, T9.4.

### [ ] P10: ActivityView and chrome polish

**id**: `P10-activity-and-chrome`
**intent**: Refresh the ActivityView (Gantt) to use the new tokens, integrate selection round-trip, surface costs, and close the visual loop with a refined Sessions sidebar and Header.
**scope_in**: `internal/webui/web/src/components/activity-view/`, `internal/webui/web/src/components/sessions-sidebar/`, `internal/webui/web/src/components/header/`.
**scope_out**: anything outside these three component trees.
**depends_on**: P5, P6, P7, P8.

#### [ ] T10.1: ActivityView visual refresh

**acceptance_criteria**:
- The Gantt background uses `bg-canvas-textured`. Phase rows use `--surface-panel`; task bars use status colors; iteration boundaries are vertical dashed lines labeled with the iteration index in `font-display italic`.
- Hovering a bar shows a tooltip with task id, status, started/ended timestamps, USD attributable to the iteration.
- Clicking a bar invokes `select({ kind: "task", phaseId, taskId })`; the selection round-trip works the same as in the DAG.
- Visual review: a 4-iteration session reads as a clear cadence with cost annotations.

**context**: The Gantt was already structurally sound; this task brings it visually in line with the redesign.
**depends_on**: P5, P8.

#### [ ] T10.2: Sessions sidebar refinement

**acceptance_criteria**:
- Each row shows: status pill, session id (mono short, last 8 chars), spec filename (truncated middle-ellipsis), USD compact (`$1.23`), duration compact (`12m`), live/archived icon.
- Hovering reveals a slide-in tooltip with the full path and started_at timestamp.
- Active session row uses `--surface-elevated` with a 2px left accent bar in `--status-running`.

**context**: The sidebar is the navigation primitive; making it dense and scannable matters more than making it wide.
**depends_on**: P5, P6.

#### [ ] T10.3: Header layout finalization

**acceptance_criteria**:
- Final header layout from left to right: session identity (id mono short + spec filename) | status pill + iter `X / Y` | view toggle (DAG ⇄ Activity) | CostMeter.
- Header height is 48px; nothing else fits.
- Responsive: below 1024px, the spec filename collapses to a tooltip on the session id; the CostMeter collapses to its USD pill only.

**context**: Closing the chrome polish before P11 verification.
**depends_on**: P6, T10.2.

### [ ] P11: Documentation and end-to-end verification

**id**: `P11-docs-verify`
**intent**: Document the new event kinds and the per-spawn prompt mechanism, regenerate the OpenAPI document, and run the full end-to-end verification.
**scope_in**: `CLAUDE.md`, `docs/specs/api/2026-05-04-http-api.md`, `docs/specs/webui/2026-05-04-embedded-web-dashboard.md` (cross-reference), `internal/api/openapi.json`, `README.md`.
**scope_out**: code changes outside these files.
**depends_on**: P1, P2, P3, P4, P9, P10.

#### [ ] T11.1: Update CLAUDE.md

**acceptance_criteria**:
- The "Anti-drift contract" callout in `CLAUDE.md` lists `spawn_started` and `spawn_finished` as part of `loop.AllEventKinds`.
- The "Sessions" subsection mentions `<sessionDir>/spawns/<spawn_id>.md` as a per-run artifact path alongside `briefings/`.

**context**: Onboarding readers need to know the spawn artifacts exist.
**depends_on**: P1, P2.

#### [ ] T11.2: Update API and webui PRDs

**acceptance_criteria**:
- `docs/specs/api/2026-05-04-http-api.md` adds `GET /api/v1/sessions/{id}/spawns/{spawnId}/prompt` to the V1 endpoint table with a one-line description.
- `docs/specs/webui/2026-05-04-embedded-web-dashboard.md` updates the V1 component list to mention the RightPane (Timeline + Inspector) and the CostMeter, with a forward link to this spec.

**context**: PRDs are normative; they need to reflect the implementation.
**depends_on**: P4, P9.

#### [ ] T11.3: Regenerate OpenAPI

**acceptance_criteria**:
- `make api-openapi` regenerates `internal/api/openapi.json` and the new `/spawns/{spawnId}/prompt` route appears.
- The regenerated client `internal/webui/web/src/lib/api-client.ts` compiles and the SPA build is green.

**context**: Mechanical step; included in the journal because it must run after P4.
**depends_on**: P4.

#### [ ] T11.4: End-to-end verification

**acceptance_criteria**:
1. `go test -race ./...` passes.
2. `gofmt -l .` produces no output; `go vet ./...` reports zero issues.
3. `make build` from a clean checkout produces a binary with the new SPA bundle and the regenerated `openapi.json`.
4. `bcc run --webui docs/specs/<spec>.md` boots, opens the dashboard, the header CostMeter shows `$0.00` initially, the DAG renders with three surface levels, the canvas reads as gradient-textured (not flat black).
5. After the first spawn completes, the CostMeter pill updates with the right USD and the `spawn_finished` event appears in the Timeline as a `spawn-marker`.
6. Clicking a phase opens the Inspector; the Prompts tab lists at least the briefer, executor, and reviewer spawns for that phase. Clicking each one renders the exact prompt body that was sent to the agent (verified by diffing against the file at `<sessionDir>/spawns/<spawn_id>.md`).
7. Clicking a task in the Gantt focuses the Inspector on the task with timestamps populated.
8. Reload on `/archived/<id>` reproduces the full UX from replay without losing ordering.
9. SPA bundle gate (≤ 600 KB gzipped) holds.
10. CI bundle gate (`pnpm build` exit 0) holds.

**context**: The verification list is the production-readiness gate.
**depends_on**: every previous phase.

## Phase dependency graph

```
P1 ─┬─→ P2 ─┬─→ P3 ──┐
    │       │        ▼
    │       └────────P4 ──→ P9 ──┐
    │                            │
    │                P7 ──→ P8 ──┤
    │                ▲           │
    │                │           ▼
    └─→ P5 ──→ P6 ──┘            P10 ──→ P11
```

P1 (events on the wire) unblocks P2 (prompt persistence) and P5 (visual foundation) in parallel. P3 (cost emission) needs both P1 (events declared) and P2 (spawn id available). P4 (prompt service and endpoint) needs P2. P6 (CostMeter) needs P3 (events emitted) and P5 (tokens). P7 (RightPane shell) needs P1 (typed renderers) and P5 (tokens). P8 (DAGView) needs P5 and the selection hook from P7. P9 (Inspector tabs) needs P4 (prompt endpoint), P7 (right pane), and P8 (selection round-trip). P10 (chrome polish) needs P5, P6, P7, P8. P11 closes the milestone.

## End-to-end verification

After P11, the following must hold without manual fix-up:

1. `make build` from a clean checkout produces a binary that contains the new SPA bundle, the regenerated `openapi.json`, and serves both at the expected paths under the API listener.
2. `bcc run --webui docs/specs/<spec>.md` boots, opens the dashboard, and within the first spawn the CostMeter, Timeline `spawn-marker`, and Inspector Prompts tab all reflect the same data.
3. `curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:<port>/api/v1/sessions/<id>/spawns/<spawnId>/prompt` returns the exact bytes of `<sessionDir>/spawns/<spawn_id>.md`.
4. `curl http://127.0.0.1:<port>/api/v1/openapi.json` describes the new route and the two new event kinds.
5. `bcc run docs/specs/<spec>.md` (no `--webui`, no `--api`) boots and runs to completion the same as before this milestone (TUI behavior unchanged).
6. The TUI continues to render `agent_event.result_summary` for executor cost; `spawn_finished` does not break the TUI even though the TUI does not yet consume it.
7. `go test -race ./...` passes.
8. `gofmt -l .` produces no output, `go vet ./...` reports zero issues.
9. CI bundle gate (≤ 600 KB gzipped) holds.

## Open items deferred to a later spec

- Light theme.
- TUI migration to consume `spawn_finished` instead of `agent_event.result_summary`.
- V2 mutating endpoints from the Inspector (approve / reject / escalate / abort).
- Editing prompts from the dashboard (spawns are read-only by design in this spec).
- Streaming the prompt body while the spawn is still running (today the full body is persisted before the subprocess starts; if a future adapter resolves prompt content lazily, this changes).
- Per-token-bucket cost projection (forecast remaining cost from current burn rate).
- Localization of the dashboard.
- Comparing prompts across spawns side by side (diff view).

## References

- `docs/specs/api/2026-05-04-http-api.md`
- `docs/specs/webui/2026-05-04-embedded-web-dashboard.md`
- `docs/specs/api-webui/2026-05-04-implementation.md`
- `docs/specs/director/2026-05-02-executable-plan-dag.md`
- `docs/specs/director/2026-05-03-capability-aware-execution.md`
- `internal/loop/events.go`
- `internal/loop/eventjson.go`
- `internal/api/schemas/event.schema.json`
- `internal/director/claude/claude.go`
- `internal/executor/claude/claude.go`
- `internal/executor/claude/streamjson/`
- `internal/services/prompts.go`
- `internal/api/handlers/`
- `internal/webui/web/src/`
- `CLAUDE.md`

## Execution Journal

### 2026-05-05 19:10:00 , P8-dag-view

- phase-node.tsx rebuilt with header (phase id mono bold, depends_on chips, aggregated status pill), 4xN task chip grid body, and footer (done/total, attempt, USD); click anywhere on the header invokes select({ kind: "phase", phaseId }) via useSelection context (T8.1)
- aggregatePhaseStatus exported from phase-node.tsx; implements error > needs_fix > running > done > pending priority; covered by 15 table-driven Vitest tests (T8.1)
- layout.ts switched from dagre-based task positioning to a 4-column grid layout; PHASE_FOOTER_H constant added; buildLayout accepts perPhaseCostUSD, perPhaseAttempt, perTaskTimestamps maps; phase nodes now carry full DAGPhase + tasks + costUSD + attempt in data (T8.1)
- task-node.tsx redesigned as compact chip card: id mono bold, status pill, retry-budget dots (filled circles, capped at 8); hover tooltip shows depends_on list and started/ended timestamps; click invokes select({ kind: "task", phaseId, taskId }); selected outline uses --accent-warn for needs_fix, --status-running otherwise (T8.2)
- dag-view/index.tsx updated: bg-canvas-textured applied to ReactFlow viewport wrapper; MiniMap enabled bottom-right with status-keyed node colors and --surface-overlay background; Controls moved to bottom-left with --surface-card background; events prop added; per-phase cost, attempt, and per-task timestamps derived from event stream (T8.3)
- EscapeHandler exported from app.tsx; clears selection (select(null)) on window keydown Escape; mounted inside SelectionProvider in AppShell (T8.4)
- E2E Vitest tests in dag-view/__tests__/dag-selection.test.tsx: SimulatedPhaseNode click transitions RightPane to Inspector, Escape keydown clears selection and returns to Timeline, App smoke mount with DAG fixture (T8.4)
- **Decisions**: xyflow culls all nodes in zero-dimension containers (happy-dom), so E2E tests drive selection through useSelection context rather than xyflow DOM events; behavior is identical to a real click since PhaseNodeComponent.onClick calls the same select() function. Per-phase cost aggregated in DAGView from spawn_finished events filtered by phase_id field; hook not imported inside phase-node per spec. pnpm build passes; bundle 188 kB gzipped (limit 600 kB).

### 2026-05-05 20:00:00 , P7-right-pane

- Selection hook added to internal/webui/web/src/hooks/use-selection.ts: Selection discriminated union (phase, task, iteration, spawn), hand-rolled context + useReducer, SelectionProvider resets selection on session id change, tests cover all union variants and reset (T7.1)
- RightPane container added to internal/webui/web/src/components/right-pane/index.tsx: renders TimelineMode when selection is null, InspectorMode placeholder otherwise; 150ms cross-fade via transition-opacity; InspectorMode shows selection kind and primary id plus X close button (T7.2)
- Five typed renderers added under internal/webui/web/src/components/right-pane/timeline/: iter-divider (iter_started/finished/loop_finished), phase-card (phase lifecycle + director_escalation), task-line (task lifecycle with feedback snippet), agent-block (agent_event with internal kind discrimination and tool_use/tool_result pairing via pairedResult prop), spawn-marker (spawn_started/finished with click-to-select) (T7.3)
- lib/event-grouping.ts added with O(n) groupByIteration returning IterationGroup[] (iterationIndex, iterationId, from/to, events, summary); applyFilters for kind/role/phase/level/search; loadFilters/saveFilters persisting to localStorage under bcc.timeline.filters.<sessionId>; TimelineMode replaced with full implementation: collapsible groups, sticky compact headers, filter toolbar (T7.4)
- app.tsx updated: mounts RightPane in right column, SelectionProvider wraps AppShell, bottom drawer row removed from layout grid; components/timeline-panel/ and components/briefing-panel/ deleted; RTL test asserts drawer absent, right pane renders, inspector appears on selection dispatch (T7.5)
- **Decisions**: Both timeline and inspector modes remain mounted simultaneously with opacity/pointer-events toggle to avoid remount cost on selection change; tool_use/tool_result pairing done in TimelineMode via pairedMap (tool_use_id keyed) before rendering; groups reversed so newest iteration appears at top; open final iteration (to: null) expands by default

### 2026-05-05 19:00:00 , P6-cost-meter

- useCostAggregator hook added to internal/webui/web/src/hooks/use-cost-aggregator.ts; computes totalUSD, totalTokens (input/output/cache-read/cache-create), perRole, perIteration aggregates from spawn_finished events; memoized on events array length (T6.1)
- CostMeter component added to internal/webui/web/src/components/cost-meter/index.tsx with inline SVG sparkline chart for USD per iteration; popover breaks down cost per role and per iteration with percentage of session total (T6.2)
- CostMeter integrated into header via internal/webui/web/src/components/header/index.tsx; pill displays USD in font-display italic with sparkline; layout: status badge | iter index | spec path | view toggle | CostMeter fits within 48px row (T6.3)
- **Decisions**: Sparkline hover tooltip shows iteration index and USD; popover on click uses click-outside detection to close; memoization on events.length trades memory for re-render cost in event-heavy runs

### 2026-05-05 17:30:00 , P5-visual-foundation

- Surface hierarchy tokens added to internal/webui/web/src/styles/tokens.css: canvas (#08090b), panel (#101316), card (#16191d), elevated (#1d2126), overlay (#0d0f12cc); legacy aliases (color-background, color-muted) preserved for backward compatibility (T5.1)
- Border tokens added: subtle (#1c1f24), default (#262a30), strong (#353a42); accent warning color (#f5b840) reserved for cost signals (T5.1)
- Typography tokens and utility classes added: --font-display (Instrument Serif), --font-numeric (Geist Mono); .font-display and .font-numeric Tailwind utilities registered; Instrument Serif italic preloaded via index.html link (T5.2)
- Canvas texture utility class bg-canvas-textured added to internal/webui/web/src/styles/index.css; radial gradient from surface-canvas with inline SVG noise data-URI (T5.3)
- **Decisions**: All tokens use CSS custom properties for cascading override; legacy aliases prevent big-bang component rename; utility classes scoped to @layer to avoid specificity inversion

### 2026-05-05 16:00:00 , P4-prompt-service

- PromptService.GetSpawn method added to internal/services/prompts.go; reads spawn prompt from .bcc/sessions/<id>/spawns/<spawn_id>.md; validates spawnID against ULID shape (^[0-9a-z]{16,32}$) before filesystem access to prevent path traversal (T4.1)
- GET /api/v1/sessions/{id}/spawns/{spawnId}/prompt endpoint registered in internal/api/handlers/prompts.go; returns text/markdown with exact bytes the spawn received; malformed spawn IDs return 400 invalid_request, missing spawns return 404 role_not_found (T4.2)
- Prompt type gains SpawnID field populated only by GetSpawn; Role left empty for spawn-fetched prompts since SPA derives role from originating event (T4.1)
- **Decisions**: Reused role_not_found error code for missing spawns to minimize error taxonomy; spawn ID regex enforced at service layer before any I/O

### 2026-05-05 14:00:00 , P3-spawn-cost

- streamjson.LastResultSummary helper added with table-driven tests covering empty, no-result, single, multiple, nil-Done, and parsed-fixture cases (T3.1)
- Director claude adapter accumulates parsed events and emits loop.SpawnFinished after cmd.Wait with SpawnID matching SpawnStarted, Cost extracted via LastResultSummary, exit code from cmd.ProcessState (T3.2)
- Executor claude adapter inlines the streamjson scan loop to retain parsed events and emits loop.SpawnFinished before any return path so observers always see the closing event paired with SpawnStarted (T3.3)
- agent_event.result_summary continues to be forwarded on the agent events channel for TUI backward compatibility
- New fixtures fake-claude-fail.sh and fake-claude-no-result.sh cover the non-zero-exit and zero-cost branches
- Decision: chose inline scan loop in executor over reusing streamjson.Stream so parsedEvents accumulation lives next to forwarding without changing Stream's public signature

### 2026-05-05 13:00:00 , P2-spawn-prompts

- NewSpawnID (26-char lowercase ULID via crypto/rand) and ValidSpawnID added to internal/director/spawn_id.go
- SpawnsDir() added to *Store returning <sessionDir>/spawns; directory is lazy (caller uses os.MkdirAll)
- Attempt int added to BrieferInput, ReviewerInput, and dag.RegisterArgs; threaded through director_run loop
- SpawnID string added to loop.ExecResult
- Director claude adapter: SessionStore and Events added to Config; runRole writes prompt to <spawnsDir>/<spawnID>.md (0o600) and emits loop.SpawnStarted before cmd.Start; SetSessionStore/SetEvents setters added for post-construction wiring
- Executor claude adapter: same pattern — SessionStore, Events, PhaseID, IterationID, Attempt added to Config; Run writes prompt file, emits SpawnStarted, returns SpawnID on ExecResult
- CLI wiring: bindDirectorAdapterSession and bindExecutorSpawnContext called in runDirectorWith after session resolution; makeNewExecutor accepts store and loopEvents parameters
- **Decisions**: Prompt file contains the user-facing prompt (positional arg or stdin briefing); for executor with SystemPromptFile set the system content is prepended so the file captures the full context. SpawnStarted emission is non-blocking (select with default) to avoid stalling the subprocess launch.

### 2026-05-05 12:15:00 , P1-spawn-events

- SpawnCost, SpawnStarted, SpawnFinished types declared in internal/loop/events.go with isLoopEvent() markers
- JSON serialization added to MarshalJSONEvent; spawn_started omits empty optional fields (phase_id, task_id, iteration_id, attempt, model, effort, prompt_path), spawn_finished always includes cost object with zero values rendered
- Both kinds appended to AllEventKinds in alphabetical order (spawn_finished, spawn_started)
- Golden JSON test cases added: SpawnStartedFull (all optional fields populated), SpawnStartedMinimal (only spawn_id and role), SpawnFinished (with cache tokens), SpawnFinishedZeroCost (on non-zero exit)
- event.schema.json updated with both kinds in type enum
- TestEventSchemaEnumMatchesLoopAllEventKinds passes; test coverage locked via TestMarshalJSONEvent_AllKindsCovered
- **Decisions**: Wire payload for spawn_started follows existing pattern of omitting empty optional fields (matching iter_started); spawn_finished cost object always present to enable reliable aggregation on the SPA side. No producers wired yet; they land in P2 and P3.
