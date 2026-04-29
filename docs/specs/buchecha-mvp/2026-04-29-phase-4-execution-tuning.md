---
title: "Phase 4: execution tuning (model, effort, MCP scope, planner)"
type: spec
status: draft
authors:
  - Fernando Macedo
reviewers: []
created: 2026-04-29
decision-date:
superseded-by:
supersedes:
review-by:
tags:
  - phase-4
  - mvp
  - executor
  - cost
---

# Phase 4: execution tuning

## Summary

Give `bcc` explicit, observable control over the executor knobs that dominate per-iteration cost and noise: model, thinking effort, MCP scope, and an opt-in pre-flight planner that downshifts model/effort for cheap phases. All knobs configurable in `.bcc.toml`, overridable per CLI invocation, and optionally per spec phase via inline metadata.

## Context and motivation

A typical iteration of `bcc run` against the claude executor spends well over 90% of its tokens on a 100k+ token `cache_read` repeated on every assistant turn. Most of that 100k is system prompt and tool schemas the agent inherits from the user's global Claude Code configuration: MCP servers (Atlassian, Slack, Gmail, Notion, etc.), skills, hooks, plugin lists, none of which a Go-coding spec needs.

Two further costs compound:

1. **Uniform model**: Opus runs every phase, including mechanical ones (panel wiring, CLI flag plumbing) where Sonnet or Haiku would suffice.
2. **Uniform effort**: thinking budget is whatever the binary defaults to, never tuned per task.

Phase 4 surfaces these as first-class knobs and adds a planner pass that picks model and effort per phase. Concretely, in a measured run on Phase 2 of this same project, dropping MCP inheritance and switching mechanical phases to Sonnet projects an order-of-magnitude cost reduction without changing observable behavior.

The spec keeps the principle that `bcc` is a thin orchestrator: every knob added here maps to an existing flag of the underlying agent, never invents agent-side behavior.

## Goals and non-goals

### Goals

- [ ] `executor.effort` (config) and `--effort` (CLI) drive the agent's thinking budget. Values: `low | medium | high | xhigh | max`. CLI wins over config; config wins over default.
- [ ] `--model` CLI flag with the same precedence as `--effort`. Configuration field already exists.
- [ ] `executor.mcp.mode` with values `isolated` (default) and `inherit`. `isolated` instructs the claude adapter to pass `--strict-mcp-config` plus any user-declared MCP config files. `inherit` passes nothing extra; the agent sees its global MCP state.
- [ ] `executor.mcp.config_files` (config) and `--mcp-config` (CLI, repeatable) declare paths merged into the agent invocation.
- [ ] `executor.bare` (config) and `--bare` (CLI) map to claude's `--bare` flag. No-op with a single warning log on adapters that lack an equivalent.
- [ ] `executor.max_budget_usd` (config) and `--max-budget-usd` (CLI) map to claude's `--max-budget-usd`.
- [ ] Per-phase override via inline HTML comment directly under the phase heading: `<!-- bcc: model=sonnet, effort=low, mcp_mode=isolated -->`. The loop reads it before each iteration and applies it for that invocation only.
- [ ] Optional pre-flight planner: when `executor.planner.enabled = true`, before each phase the loop runs one short executor call against a cheap model (`executor.planner.model`, default `claude-haiku-4-5-20251001`) with a fixed JSON schema. The planner returns `{model, effort, rationale}`. The result is logged and applied to the main invocation, unless overridden by phase metadata or CLI.
- [ ] `bcc init` wizard surfaces the new knobs with safe defaults.
- [ ] `bcc run` startup banner prints the resolved knobs (model, effort, mcp mode, bare, planner state) so the user sees what will run before any tokens are spent.
- [ ] Resolution log: each iteration's JSONL gets a synthetic `bcc_settings` event at the top with the resolved knobs and the precedence path that produced them, so the TUI and observers can render them.

### Non-goals

- Adapter coverage beyond claude. The `codex` and `gemini` adapters (Phase 3+) accept the new fields but log a single warning per run when a knob is unmapped. Full mapping is part of multi-agent phase.
- Mid-iteration model switching. The planner picks once per phase; the agent runs the chosen model end to end for that iteration.
- A custom budget/cost dashboard. `--max-budget-usd` is a hard ceiling enforced by the agent; surfacing live spend in the TUI is Phase 2/5 territory.
- A learning planner. The planner is stateless and prompt-driven; tuning is by editing the planner prompt template, not by gradient updates or memory.

## Proposal

### Resolution order (highest first)

For every per-invocation knob (`model`, `effort`, `mcp_mode`, `mcp_config_files`, `bare`, `max_budget_usd`):

1. CLI flag (e.g. `--effort xhigh`).
2. Phase metadata directive (`<!-- bcc: ... -->` immediately after the phase heading).
3. Planner output (when `executor.planner.enabled` and the field is in the planner schema).
4. `executor.*` in `.bcc.toml`.
5. Built-in defaults baked into `internal/config/defaults.go`.

A single `Resolve` function in `internal/loop` produces a `ResolvedSettings` per iteration, attaches the precedence path to each field, and emits the `bcc_settings` event before the executor runs.

### Config additions

`internal/config/config.go`:

```go
type Executor struct {
    Agent           string   `toml:"agent"`
    Binary          string   `toml:"binary"`
    Model           string   `toml:"model"`
    Effort          string   `toml:"effort"`            // "" | low | medium | high | xhigh | max
    Bare            *bool    `toml:"bare"`              // tristate; default false
    MaxBudgetUSD    float64  `toml:"max_budget_usd"`    // 0 = unset
    ExtraArgs       []string `toml:"extra_args"`
    SkipPermissions *bool    `toml:"skip_permissions"`

    MCP     ExecutorMCP     `toml:"mcp"`
    Planner ExecutorPlanner `toml:"planner"`
}

type ExecutorMCP struct {
    Mode        string   `toml:"mode"`         // "" -> defaults to "isolated"; "isolated" | "inherit"
    ConfigFiles []string `toml:"config_files"` // paths to JSON files; merged in declared order
}

type ExecutorPlanner struct {
    Enabled bool    `toml:"enabled"`
    Model   string  `toml:"model"`             // default "claude-haiku-4-5-20251001"
    Effort  string  `toml:"effort"`            // default "low"
    BudgetUSD float64 `toml:"budget_usd"`      // default 0.05
    PromptOverride string `toml:"prompt_override"` // optional path to a custom planner prompt template
}
```

The defaults applied in `internal/config/defaults.go`:

| Field | Default |
|---|---|
| `executor.effort` | `""` (no `--effort` passed; agent picks) |
| `executor.bare` | `false` |
| `executor.max_budget_usd` | `0` |
| `executor.mcp.mode` | `isolated` |
| `executor.mcp.config_files` | `[]` |
| `executor.planner.enabled` | `false` |
| `executor.planner.model` | `claude-haiku-4-5-20251001` |
| `executor.planner.effort` | `low` |
| `executor.planner.budget_usd` | `0.05` |

`mcp.mode = "isolated"` plus `mcp.config_files = []` means the adapter passes `--strict-mcp-config` with no `--mcp-config` files, which is what eliminates the global MCP overhead.

### CLI additions

`bcc run`:

```
--model <name>                 (overrides executor.model)
--effort <level>               (low|medium|high|xhigh|max)
--mcp-mode <mode>              (isolated|inherit)
--mcp-config <path>            (repeatable; appended after config files)
--bare                         (boolean)
--no-bare                      (boolean; counter to executor.bare=true)
--max-budget-usd <amount>      (float; 0 = unset)
--planner / --no-planner       (override executor.planner.enabled)
```

`bcc init` adds the planner step and the MCP mode step. It explains the cost trade-off briefly and offers `isolated` as the default.

### Per-phase metadata

A phase heading inside the spec's `## Implementation Plan` may be followed immediately by an HTML comment with the directive form:

```markdown
### Phase 2: TUI dashboard
<!-- bcc: model=claude-sonnet-4-6, effort=low -->

1. [ ] ...
```

Grammar:

```
directive    := "<!--" WS? "bcc:" WS? assignments WS? "-->"
assignments  := assignment ("," WS? assignment)*
assignment   := key "=" value
key          := /[a-z_]+/
value        := /[^,\s>]+/
```

Recognized keys: `model`, `effort`, `mcp_mode`, `bare`, `max_budget_usd`. Unknown keys produce a warning event but do not fail the iteration.

The directive applies only to the iteration that implements that phase. The heading the loop matches is the phase the agent is currently working on (resolved from the first phase with any `[ ]` item, identical to today's "next phase" rule).

The parser lives in `internal/spec/directive.go`, pure function, table-driven tests.

### Planner

The planner is a port in `internal/loop`:

```go
type Planner interface {
    Classify(ctx context.Context, in PlannerInput) (PlannerOutput, error)
}

type PlannerInput struct {
    SpecPath        string
    PhaseHeading    string
    PhaseBody       string  // markdown of the phase to implement
    PreviousResults []string // last N journal **Result** values, oldest first
    AvailableModels []string // resolved alias list, e.g. ["haiku", "sonnet", "opus"]
}

type PlannerOutput struct {
    Model     string  // value from AvailableModels
    Effort    string  // low|medium|high|xhigh|max
    Rationale string  // one short paragraph; logged, not applied
}
```

Default adapter `internal/executor/claudeplanner` reuses the existing `claude` binary in print mode with:

- `--bare`
- `--strict-mcp-config` and no MCP files
- `--model` from `executor.planner.model`
- `--effort` from `executor.planner.effort`
- `--max-budget-usd` from `executor.planner.budget_usd`
- `--output-format json`
- `--json-schema` set to a fixed schema matching `PlannerOutput`
- a system prompt template embedded in the binary (overridable via `executor.planner.prompt_override`)

The default planner prompt template lives at `internal/executor/claudeplanner/prompt.tmpl`. It is short (target under 500 tokens of payload) and asks the model to classify the phase as trivial, moderate, or complex, then map to model+effort using a built-in table. The mapping table:

| Class | Model | Effort |
|---|---|---|
| trivial | claude-haiku-4-5-20251001 | low |
| moderate | claude-sonnet-4-6 | medium |
| complex | claude-opus-4-7 | high |

The mapping table is data, not code, and lives in `internal/executor/claudeplanner/mapping.go` so the user can fork it without changing the prompt.

### Adapter changes

`internal/executor/claude/claude.go`:

1. `Config` gains `Effort string`, `Bare bool`, `MaxBudgetUSD float64`, `MCPStrict bool`, `MCPConfigFiles []string`.
2. `Run` builds the args list in this order: `-p`, `--output-format stream-json`, `--verbose`, conditional `--dangerously-skip-permissions`, conditional `--model`, conditional `--effort`, conditional `--bare`, conditional `--max-budget-usd`, conditional `--strict-mcp-config`, repeated `--mcp-config <file>`, then `cfg.ExtraArgs`, then the prompt.
3. `--exclude-dynamic-system-prompt-sections` is always passed when `Bare` is false: it is a free win for cache reuse and never hurts.
4. The adapter validates `Effort` against the known set at construction time; an invalid value is a config error caught at startup, not at iteration time.

Future codex/gemini adapters: their `Config` mirrors the same field names. When a field is unmapped (e.g. effort on an agent without a thinking-budget flag), the adapter logs `WARN executor: <field> not mapped on <agent>; ignored` once per `Run`.

### Loop changes

`internal/loop/loop.go`:

1. New stage between "decide next phase" and "invoke executor": **resolve settings**. Calls `Resolve(cfg, cli, spec, phaseHeading) ResolvedSettings`. Pure function, table-driven tests. Emits the `bcc_settings` synthetic event on the events channel.
2. New optional stage between resolve and invoke: **plan**. When `cfg.Executor.Planner.Enabled` and not `--no-planner`, call `Planner.Classify`. Treat `Classify` errors as soft: log, fall back to non-planner resolution, continue.
3. The executor adapter is constructed per iteration with the resolved settings (existing constructor change is small; the loop today builds the adapter once at startup).

### Event taxonomy additions

Add two `loop.Kind` values:

- `KindBccSettings`: payload `*ResolvedSettings`. Emitted before the executor starts.
- `KindPlannerDecision`: payload `*PlannerOutput`. Emitted only when the planner ran.

The TUI's "now" panel adds one line under the current phase: `model=opus effort=high mcp=isolated planner=on`.

### Startup banner

After the existing `bcc: WARNING: skip_permissions=...` block, `bcc run` prints:

```
bcc: resolved knobs:
  model=claude-opus-4-7 (cli)
  effort=high (planner: complex phase)
  mcp.mode=isolated (config)
  bare=false (default)
  max_budget_usd=2.00 (config)
  planner=on (config)
```

The parenthetical tag is the precedence source: `cli`, `phase`, `planner`, `config`, or `default`.

## Alternatives considered

### Alternative 1: model router as middleware

Wrap the executor in a router that observes turn output and switches model mid-iteration. **Discarded**: the agent CLIs do not expose mid-iteration switching, and emulating it via per-turn restarts breaks the journal contract (agent loses memory of its earlier reasoning). Phase-level granularity is the natural unit.

### Alternative 2: per-phase config in `.bcc.toml`

Instead of inline directives, a TOML table keyed by phase title:

```toml
[executor.phases."Phase 2: TUI dashboard"]
model = "claude-sonnet-4-6"
```

**Discarded**: doubles the config surface and decouples the override from the spec it applies to. Inline directives stay with the spec, survive renames within the same commit, and travel with the spec when shared.

### Alternative 3: skip the planner; require the user to annotate

Cheaper to implement, equally cheap at runtime if annotations are exhaustive. **Kept as a parallel path**: the planner is opt-in, never required. Inline directives work without it. The planner is the autopilot for users who do not want to annotate.

### Alternative 4: enable `--bare` by default

Maximally cheap, but breaks projects that depend on CLAUDE.md, hooks, or skills inside the loop. **Discarded as default**: bare is opt-in. Users who want maximum cost reduction set `executor.bare = true` explicitly.

## Implementation Plan

Each item is a deliverable with a checkbox. The order respects the layer rules in [AGENTS.md](../../../AGENTS.md): domain first, then ports, then adapters, then CLI wiring.

### P4.1: config schema extensions

1. [ ] Add `Effort`, `Bare`, `MaxBudgetUSD` to `config.Executor` with TOML tags.
2. [ ] Add `ExecutorMCP` struct and `Executor.MCP` field.
3. [ ] Add `ExecutorPlanner` struct and `Executor.Planner` field.
4. [ ] Update `internal/config/defaults.go` with the defaults table above.
5. [ ] Update `internal/config/defaults_test.go` with table rows for each new field, including the `mcp.mode = "isolated"` default.
6. [ ] Validate effort values in `config.ValidateExecutor()`. Unknown values return an `ErrInvalidEffort` with the offending input.

### P4.2: spec directive parser

1. [ ] Add `internal/spec/directive.go` with `ParsePhaseDirective(body string) (Directive, error)`.
2. [ ] Add table-driven tests covering: missing comment, malformed syntax, unknown keys (warning, not error), known keys, multiple comments (first wins), comment not adjacent to heading (ignored).
3. [ ] Wire `Spec.PhaseDirective(headingIndex int) (Directive, bool)` on the spec aggregate.

### P4.3: settings resolver

1. [ ] Add `internal/loop/settings.go` defining `ResolvedSettings` and `Resolve(cfg, cli, spec, phaseIdx) ResolvedSettings`.
2. [ ] Each field on `ResolvedSettings` carries an additional `Source` enum (`SourceCLI`, `SourcePhase`, `SourcePlanner`, `SourceConfig`, `SourceDefault`).
3. [ ] Table-driven tests cover the precedence matrix end to end (CLI > phase > planner > config > default) for each field.
4. [ ] Add `KindBccSettings` to `internal/loop/events.go` and a serializer in the JSON output backend.

### P4.4: claude adapter wiring

1. [ ] Extend `claude.Config` with new fields.
2. [ ] Update arg builder in `Run` per the Adapter changes section.
3. [ ] Validate effort in `New` (fail fast).
4. [ ] Adapter integration tests: capture the args slice via a fake `exec.Cmd` and assert the expected flags appear in the expected order, parameterized by config.
5. [ ] Update `internal/cli/run.go` to construct the claude adapter from `ResolvedSettings`.

### P4.5: planner port and adapter

1. [ ] Define `loop.Planner`, `loop.PlannerInput`, `loop.PlannerOutput`.
2. [ ] Implement `internal/executor/claudeplanner/` with a `Classifier` type satisfying `loop.Planner`.
3. [ ] Embed `prompt.tmpl` and `mapping.go` (the trivial/moderate/complex table).
4. [ ] Tests: golden test on the prompt template; fake claude binary writes a fixed JSON to stdout, adapter parses it; error path falls back to defaults without aborting.
5. [ ] Wire planner invocation into `loop.Loop.Run`. When enabled, call before `Resolve` so the planner result feeds resolution. Planner errors log and fall through.
6. [ ] Add `KindPlannerDecision` event.

### P4.6: CLI wiring

1. [ ] Add the new flags to `internal/cli/run.go` with cobra, including the `--bare`/`--no-bare` and `--planner`/`--no-planner` pairs (use `BoolVar` plus a sentinel struct to distinguish "unset" from "false"; pattern already used for SkipPermissions).
2. [ ] Add the resolved-knobs banner block.
3. [ ] Add planner-related flags (`--planner-model`, `--planner-effort`) only if explicit need surfaces in P4.5; otherwise rely on config alone for those subfields.

### P4.7: init wizard

1. [ ] Add the MCP mode question to `internal/cli/init.go` (default `isolated`).
2. [ ] Add the planner question (default `disabled`).
3. [ ] Update `internal/cli/init_test.go` golden output.

### P4.8: TUI surface

1. [ ] Header panel renders `model effort mcp planner` summary line.
2. [ ] Now panel renders the same plus precedence source per field on hover/expand (deferred if expand UX is not in Phase 2).
3. [ ] Add a panel test fixture exercising each combination.

### P4.9: docs

1. [ ] Update `docs/guides/autonomous-execution.md` with a "Per-phase tuning" section documenting the directive grammar.
2. [ ] Update `README.md` with a short "Cost knobs" subsection.
3. [ ] Update `docs/specs/buchecha-mvp/index.md` to add this spec to the table and check off the corresponding Phase 3+ bullet.

## Autonomous execution

### Done criteria

Default technical criteria from [Autonomous execution guide](../../guides/autonomous-execution.md). Plus:

1. `go test -race ./...` green, including new resolver and directive tests.
2. End-to-end smoke: `bcc run testdata/specs/sample-en.md --planner` against the fake executor produces the expected resolved-knobs banner and event sequence (`bcc_settings`, optional `planner_decision`, then the iteration events).
3. Manual smoke against the live `claude` binary, one phase, recording the actual cache_read delta with and without `--bare` plus `mcp.mode = isolated`. Logged in the journal.

### Stop criteria

1. Success: P4.1 through P4.9 all `[x]`. Stop and request review before announcing the new flags publicly.
2. Block: planner adapter cannot produce reliable JSON in three consecutive runs after prompt iteration. Treat as `Result: blocked` and revisit prompt design.
3. Human decision: if the precedence rule turns out to confuse users in the smoke run (planner overriding their config in surprising ways), pause for product call before shipping P4.5.

## Security and compliance

- The `mcp.config_files` paths and any planner prompt overrides are read from disk at startup. Errors are reported with the path; values are never echoed in event logs.
- `--max-budget-usd` is forwarded as-is to the agent. `bcc` does not enforce a duplicate ceiling.
- The planner's network calls are subject to the same auth as the main invocation; no separate credential is added.

## Observability

- Every iteration's JSONL begins with a `bcc_settings` event carrying the resolved knobs and source per field.
- When the planner runs, a `planner_decision` event follows immediately, with the decision and rationale.
- The startup banner mirrors the same data in plain text on stderr, for users who do not parse JSONL.
- Cost data per iteration already comes through `KindResultSummary` (`TotalCostUSD`); after this phase it is correlatable with the resolved knobs by `iteration_id`.

## Risks and mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Planner picks wrong model and the phase fails | Medium | Medium | Inline directive overrides planner; `--no-planner` disables it; Result `partial` triggers a re-plan with model bumped one tier. |
| Effort flag values diverge across agent versions | Low | Low | Validate at startup against a known list embedded in the adapter; bump the list when claude releases new tiers. |
| MCP isolation breaks a project that depends on a global MCP | Medium | Low | Default `isolated` is a flag; users who need globals set `mcp.mode = "inherit"` and the warning explains the cost trade-off. |
| `--bare` removes CLAUDE.md context the agent needs to follow project conventions | Medium | Medium | Bare is opt-in only; the banner makes it visible; doc explains explicitly that bare mode requires the spec to carry all required context. |
| Per-phase directive ambiguity (multiple comments, comments far from heading) | Low | Low | Parser rules are strict and tested; first-comment-wins, must be adjacent. Unknown keys emit a warning event but never fail. |

## References

- Phase 1 spec: [2026-04-29-phase-1-bash-parity.md](./2026-04-29-phase-1-bash-parity.md)
- Phase 2 spec: [2026-04-29-phase-2-tui-dashboard.md](./2026-04-29-phase-2-tui-dashboard.md)
- Phase 3 spec: [2026-04-29-phase-3-steering.md](./2026-04-29-phase-3-steering.md)
- Claude Code CLI: `--effort`, `--mcp-config`, `--strict-mcp-config`, `--bare`, `--max-budget-usd`, `--json-schema`, `--exclude-dynamic-system-prompt-sections`.
- Autonomous execution guide: [docs/guides/autonomous-execution.md](../../guides/autonomous-execution.md)

## Open questions

- [ ] Should the planner output also carry `expected_turns`, used by the loop to size `max_iterations` adaptively? Leaning yes, deferred until a post-P4.5 measurement shows whether the estimate is reliable enough to act on.
- [ ] Should `executor.planner.enabled = true` be the default once it has measured well? Leaning no for the first release; opt-in keeps the trust gate explicit.
- [ ] Should phase directives accept `bare = true`? Risk: enabling bare per phase strips CLAUDE.md context mid-spec, which is rarely what the user wants. Tentative answer: no, `bare` is a global-only knob; document the rationale.
- [ ] Where does the planner prompt template live for projects that want to fork it without forking the binary? Current proposal: `executor.planner.prompt_override` path; alternative is `docs/templates/planner.tmpl` autoloaded when present.

## Execution Journal

Most recent entries on top. Contract in [Autonomous execution guide](../../guides/autonomous-execution.md#execution-journal).

(empty until first execution)
