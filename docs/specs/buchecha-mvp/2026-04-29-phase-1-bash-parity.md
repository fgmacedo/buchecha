---
title: "Phase 1: bash parity"
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
  - phase-1
  - mvp
---

# Phase 1: bash parity

## Summary

Port `scripts/exec-spec.sh` (~280 lines of bash) to a Go CLI subcommand `bcc run <spec>`, preserving the journal contract, exit codes, and overall behavior. Add `bcc init` interactive wizard that writes `.bcc.toml`. Add config layer with `[env]` support (`.env` loading + inline vars). No TUI in this phase.

## Context and motivation

The current shell wrapper works but is opaque, bash-bound, and project-specific. Phase 1 establishes the Go foundation that subsequent phases (TUI dashboard, multi-agent, releases) build on. Functional parity with the bash script is the success criterion: anywhere `scripts/exec-spec.sh foo.md` works, `bcc run foo.md` (with appropriate `.bcc.toml`) must work identically.

## Goals and non-goals

### Goals

- [ ] `bcc run <spec>` end-to-end functional parity with `scripts/exec-spec.sh`: phase loop, single-shot mode, max-iterations cap, HEAD-advanced check, journal parsing, exit codes 0/1/2/3/4/5.
- [ ] `bcc init` interactive wizard generates `.bcc.toml` with sensible defaults.
- [ ] Config layer (`internal/config`) loads `.bcc.toml`, applies defaults by `project.language`, supports `[env]` with `.env` files and `[env.vars]` inline.
- [ ] Spec parser (`internal/spec`) extracts plan headings, counts `[x]`/`[ ]` items, parses latest journal `**Result**`. Pure functions, table-driven tests.
- [ ] Executor (`internal/executor`) runs the agent subprocess, streams JSONL events to a per-iteration file, captures exit code.
- [ ] Loop controller (`internal/loop`) implements the decision table with same semantics as the bash `case`.
- [ ] Localization: a `.bcc.toml` set to `language = "pt-BR"` makes `bcc run` work on `condo-fiscal` specs without rewriting them.

### Non-goals

- TUI dashboard (Phase 2).
- Multi-agent executor abstraction beyond a thin interface (Phase 3).
- goreleaser, GitHub Actions, Homebrew (Phase 3+).
- Plug-in system for custom heuristics (Phase 3+).
- PRD/spec scaffolding commands `bcc new prd|spec` (Phase 3+).

## Proposal

### Package layout

Layered following ports-and-adapters per [AGENTS.md](../../../AGENTS.md). Domain packages (`spec`, `config`, `loop`) have no adapter imports; adapter packages implement ports defined in `loop`.

```
buchecha/
├── main.go                       # cobra entry
├── cmd/                          # cobra commands; wires adapters into loop
│   ├── root.go
│   ├── run.go
│   ├── init.go
│   └── watch.go                  # stub until Phase 2
├── internal/
│   ├── config/                   # domain: typed Config, defaults, env merge
│   │   ├── config.go
│   │   ├── defaults.go
│   │   └── env.go
│   ├── spec/                     # domain: pure parsers, value objects
│   │   ├── spec.go               # Spec, Phase, Item, JournalEntry, Result
│   │   ├── plan.go               # ParsePlan, CountChecked, CountUnchecked
│   │   ├── journal.go              # ParseLatestResult
│   │   └── headings.go           # FindHeading, SectionBetween
│   ├── loop/                     # domain: orchestration + ports
│   │   ├── loop.go               # Loop.Run; decision table
│   │   ├── ports.go              # Executor, GitProbe, SpecReader interfaces
│   │   ├── prompt.go             # build prompts via text/template
│   │   └── exitcodes.go          # constants 0..5
│   ├── executor/                 # adapters
│   │   └── claude/
│   │       └── claude.go         # implements loop.Executor (JSONL stream)
│   ├── git/                      # adapters
│   │   └── cli/
│   │       └── cli.go            # implements loop.GitProbe via os/exec
│   ├── specreader/               # adapters
│   │   └── markdown/
│   │       └── markdown.go       # implements loop.SpecReader on disk
│   └── configloader/             # adapters
│       └── toml/
│           └── toml.go           # reads .bcc.toml into config.Config
└── testdata/
    ├── specs/
    │   ├── sample-en.md
    │   └── sample-pt-br.md
    └── jsonl/
        └── sample-events.jsonl
```

### Configuration: `.bcc.toml`

```toml
[project]
language = "en"                       # "en" | "pt-BR" (more languages later)

[executor]
agent = "claude"                      # "claude" | "codex" | "gemini" (Phase 3) | "custom"
binary = "claude"                     # path or PATH name
model = "claude-opus-4-7"
extra_args = ["--dangerously-skip-permissions", "--output-format", "stream-json", "--verbose"]

[specs]
dir = "docs/specs"
plan_heading = "## Implementation Plan"   # auto-defaulted from project.language
journal_heading = "## Execution Journal"

[loop]
mode = "phase"                        # "phase" | "single-shot"
max_iterations = 20

[loop.results]
ok = "ok"
partial = "partial"
done = "done"
blocked = "blocked"

[git]
branch_prefix = "feat"
require_commit_per_iteration = true

[env]
files = [".env", ".env.bcc"]          # loaded in order, later wins among files

[env.vars]
# CLAUDE_CONFIG_DIR = "~/.claude-pessoal"
# Add per-project env vars here. Tilde and ${VAR} are expanded.
```

`project.language = "pt-BR"` switches defaults for `specs.plan_heading`, `specs.journal_heading`, and `loop.results.*` to the pt-BR equivalents (`## Plano de implementação`, `## Diário de execução`, `ok` / `parcial` / `finalizado` / `bloqueado`).

### Env precedence (highest first)

1. `--env KEY=VALUE` flag on the CLI (repeatable).
1. Shell env at the moment `bcc` was invoked.
1. `[env.vars]` from `.bcc.toml`.
1. Files listed in `[env].files`, in declared order; later wins among files.

Tilde (`~`) and `${VAR}` are expanded after merge. Env values are never logged; only key names.

### CLI surface (Phase 1)

```bash
bcc run <spec> [flags]
  --single-shot                  # one agent call tries everything
  --max-iterations N             # default from .bcc.toml or 20
  --env KEY=VALUE                # repeatable; highest precedence
  --extra "<text>"               # injected as "Additional instructions" in the prompt
  --config <path>                # override .bcc.toml discovery

bcc init                         # interactive wizard
  --force                        # overwrite existing .bcc.toml
  --language <code>              # skip language prompt

bcc watch <spec>                 # Phase 2 stub: prints "not implemented yet"
```

### Exit codes (parity with bash)

| Code | Meaning |
|---|---|
| 0 | `done` declared and zero `[ ]` remain |
| 1 | `blocked` (human review needed) |
| 2 | unknown result, or invalid spec/config |
| 3 | HEAD did not advance during iteration |
| 4 | iteration cap reached without `done` |
| 5 | `done` declared with `[ ]` items still in plan |

### Init wizard flow

Linear prompt sequence using `bubbles/textinput` or plain `bufio` + `fmt`. To minimize Phase 1 deps, prefer plain stdin until the wizard's UX needs grow.

1. Detect existing `.bcc.toml`. If present and no `--force`, ask whether to overwrite (default no).
1. Project language: `[en/pt-BR]` (default `en`).
1. Agent: `[claude/codex/gemini/custom]` (default `claude`).
1. Agent binary path: probe `PATH`, suggest result.
1. Model name (only if relevant for the agent): defaults per agent.
1. Spec directory: default `docs/specs`. Verify exists or offer to create.
1. Loop mode: `[phase/single-shot]` (default `phase`).
1. Max iterations: default 20.
1. Branch prefix: default `feat`.
1. `.env` files to load: comma-separated (default `.env`).
1. Confirm summary, write `.bcc.toml`.

### Prompt construction

`internal/loop/prompt.go` builds the prompt the agent receives, mirroring `prompt_loop()` and `prompt_single_shot()` from the bash script. Templates use Go's `text/template`. The localized vocabulary comes from the loaded config.

## Implementation Plan

Each phase below is a checkbox group. The autonomous execution agent (when this spec is eventually run by `bcc` itself) marks `[x]` per item delivered.

### P1.1: config + spec parsers (no I/O on agent)

1. [x] `internal/config/config.go`: `Config` struct mirroring TOML schema; types and validation only (Load/Discover live in `configloader/toml/` per layout). Stdlib-only.
1. [x] `internal/config/defaults.go`: `applyDefaults(c *Config)` filling missing values per `project.language`.
1. [x] `internal/config/env.go`: `(c *Config) ApplyEnv(extraFlags []string) error` resolving precedence and exporting to `os.Setenv`. No values logged.
1. [x] `internal/configloader/toml/toml.go`: `Load(path) (*config.Config, error)`; `Discover(cwd) (*config.Config, error)` (walks up). Uses `BurntSushi/toml`; applies defaults after parse.
1. [x] `internal/spec/headings.go`, `plan.go`, `journal.go`: `ParsePlan`, `CountChecked`, `CountUnchecked`, `ParseLatestResult`. Pure, table-driven tests against `testdata/specs/`.
1. [x] `go test -race ./internal/config/... ./internal/configloader/... ./internal/spec/...` zero failures.

### P1.2: executor adapter with JSONL streaming

1. [x] `internal/loop/ports.go`: define `Executor` interface (`Run(ctx, prompt, jsonlOut) (exitCode int, err error)`), plus `GitProbe`, `SpecReader`. All consumed by `Loop`.
1. [x] `internal/executor/claude/claude.go`: implements `loop.Executor` invoking `claude -p --output-format stream-json --verbose ...`, streaming stdout directly to `jsonlOut` (`cmd.Stdout = jsonlOut`), propagating exit code.
1. [x] Cancellation via `context.Context`. `cmd.Cancel` sends SIGINT; `cmd.WaitDelay` (default 5s) escalates to SIGKILL; a final `{"type":"interrupted"}` line is appended to `jsonlOut`. Run returns `ctx.Err()` so callers can `errors.Is` it.
1. [x] Fake executor under `internal/executor/fake/` for tests: replays a scripted sequence of `Step{JSONL, ExitCode, Err}`; records prompts received. Used by loop tests in P1.3.

### P1.3: loop controller

1. [x] `internal/loop/loop.go`: `Loop.Run(ctx) (exitCode int, err error)` implementing the bash decision table: per iteration, capture HEAD before; build prompt once; invoke executor; capture HEAD after; reload spec, parse plan + latest Result; pass to Decide; act. Depends only on the ports defined in `ports.go`. Uses `log/slog` for milestone logging.
1. [x] `internal/loop/exitcodes.go`: constants 0..5 with comments matching the bash contract.
1. [x] `internal/loop/prompt.go`: `BuildPromptLoop` and `BuildPromptSingleShot` via `text/template`, parameterized by spec path, guide path, full localized vocabulary (plan/journal headings, Result keyword, four Result values), and an optional `Extra` block.
1. [x] `internal/loop/decider.go`: pure `Decide(DeciderInput) Decision`. HEADAdvanced checked first; switch on Result; `done` with leftovers maps to ExitDoneWithLeftovers.
1. [x] Single-shot mode: max iterations forced to 1; uses the single-shot prompt template variant.
1. [x] Table-driven tests for the decider (8 cases); loop tests in external `loop_test` package using `executor/fake` + scripted `GitProbe` + scripted `SpecReader` (12 cases covering all decision branches, single-shot cap, pt-BR localization, port-error propagation, JSONL file emission).

### P1.4: cobra wiring + end-to-end

1. [ ] `cmd/run.go`: parse flags, load config via `configloader/toml`, apply env, instantiate `executor/claude` and `git/cli` and `specreader/markdown`, build `Loop`, invoke. Translate Go error to exit code.
1. [ ] `cmd/init.go`: linear stdin wizard, write `.bcc.toml`. `--force` and `--language` honored.
1. [ ] `cmd/watch.go`: print "not implemented yet (Phase 2)" and return exit 2.
1. [ ] End-to-end smoke test: a small spec in `testdata/specs/sample-en.md` driven by `executor/fake` that simulates two iterations (`partial`, `done`); verifies exit 0 and correct file outputs.

### P1.5: validation against `condo-fiscal`

1. [ ] Author manually creates `.bcc.toml` in `condo-fiscal` repo with `language = "pt-BR"`, points `[executor].binary` to `claude`, configures `[env.vars]` with `CLAUDE_CONFIG_DIR`.
1. [ ] Run `bcc run docs/specs/<a-finished-spec>.md --max-iterations 0` (dry-load) and confirm spec/plan/journal parsing matches what the bash awk produced.
1. [ ] Run `bcc run docs/specs/<a-real-pending-spec>.md` for one iteration with stub model (or smallest claude model) and confirm exit codes and journal parsing on real output.

## Autonomous execution

This spec follows the [Autonomous execution guide](../../guides/autonomous-execution.md) defaults. No customization needed.

### Done criteria

In addition to the guide defaults:

1. `gofmt -l ./...` empty output.
1. `go vet ./...` zero errors.
1. `go test ./...` zero failures.
1. `go build -o bcc .` succeeds.
1. `bcc --help` and `bcc run --help` show expected output.

### Stop criteria

1. Success: P1.1 through P1.5 all `[x]`, criteria above all green, manual smoke on `condo-fiscal` confirms localization works.
1. Block: 3 consecutive validation failures after `git revert`. Or if the `claude` JSONL contract changes mid-development.
1. Human decision: any deviation from the journal contract semantics (e.g., new `Result` value needed). Requires guide update first.

## Risks and mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Claude `stream-json` event shape changes | Medium | High | Tolerant parser: pass through unknown event types; tests against captured fixtures |
| Subprocess buffering breaks JSONL streaming | Medium | Medium | Use `bufio.Scanner` with explicit buffer size; test with slow producer |
| Localization defaults drift from `condo-fiscal` strings | Low | High | Snapshot test loading the actual `condo-fiscal` spec headings |
| Cobra flag handling diverges from bash flag order | Low | Low | Document flag mapping in README; tests cover all flags |

## Execution Journal

### 2026-04-29 14:00, P1.3: loop controller

- **Result**: ok
- **Summary**: Built the loop orchestration layer end to end. `internal/loop/exitcodes.go` declares the bash-compatible 0..5 constants. `internal/loop/decider.go` is the pure decision function (HEADAdvanced first, then Result-based dispatch with `done`+leftovers → exit 5). `internal/loop/prompt.go` renders loop-mode and single-shot prompts via `text/template` with full localized vocabulary. `internal/loop/loop.go` ties it together: opens per-iteration JSONL files under `JSONLDir`, calls Executor → SpecReader → Git → Decide, logs milestones via `log/slog`. Single-shot mode forces max iterations to 1. 12 loop test cases cover every decision branch including pt-BR localization, port error propagation, single-shot cap, and JSONL file emission. Total: 7 packages green under `go test -race`.
- **Commits**: (forthcoming) config: add ResultKeyword to Specs (discovered during P1.3); loop: exit codes and pure decider (P1.3); loop: prompt templates with localization (P1.3); loop: orchestrator with port wiring (P1.3); docs: mark P1.3 [x] and journal entry
- **Decisions**: (1) Discovered work registered: added `ResultKeyword` field to `config.Specs` (toml tag `result_keyword`, defaults `Result` for en, `Resultado` for pt-BR). Missed in P1.1 because the journal parser already accepted the keyword as a parameter; the gap was on the config side. Tests in `internal/config/defaults_test.go` updated for both languages. (2) `loop_test.go` lives in the external `package loop_test` to avoid an import cycle (the fake executor imports `loop` to satisfy the `loop.Executor` interface; tests import both fake and loop). `decider_test.go` and `prompt_test.go` stay in `package loop` because they only use exported types from their own package and adding `loop.` prefixes everywhere is needless churn. (3) Loop owns the JSONL file lifecycle (Create/Close per iteration); the Executor only writes to the provided io.Writer. Filename pattern: `<slug>-iter<N>.jsonl` under `JSONLDir`. (4) Loop fields hold the wired ports (`Executor`, `Git`, `SpecReader`); `Run(ctx)` takes only the cancellation context. Construction is done at the cmd boundary; tests bypass cmd and build the Loop directly. Validates AGENTS.md's "wire adapters at cmd, ports in consumer" rule. (5) Decider is pure (no I/O, no time): trivially testable, easy to extend later for new heuristics (loop-suspect detector in Phase 2). (6) Stop reasons logged with structured kv pairs (`reason=done|blocked|head_stuck|...`) so `bcc watch` can render them later without parsing prose. (7) Loop returns `(ExitInvalid, ctx.Err())` on cancel and on any port error; cmd/run.go in P1.4 will translate this to os.Exit(2) and stderr.
- **Problems**: First pass had `package loop` for `loop_test.go` which created an import cycle with `executor/fake` (fake imports loop for the interface assertion). Fixed by switching to external `package loop_test`. Easier to spot once Go printed "import cycle not allowed in test"; the rule is not obvious for first-time hexagonal-in-Go layouts.
- **Next**: P1.4 (cobra wiring + end-to-end smoke)

### 2026-04-29 13:00, P1.2: executor adapter with JSONL streaming

- **Result**: ok
- **Summary**: Defined the three loop ports (`Executor`, `GitProbe`, `SpecReader`) in `internal/loop/ports.go`. Implemented `internal/executor/claude` invoking `claude -p --output-format stream-json --verbose [...] <prompt>`, streaming stdout directly to the writer, with graceful cancellation (SIGINT via `cmd.Cancel`, SIGKILL escalation via `cmd.WaitDelay`) and an `{"type":"interrupted"}` terminator on cancel. Implemented `internal/executor/fake` as a scripted Executor for loop tests. Tests cover: stream capture, non-zero exit propagation, missing binary error, context-deadline cancel + terminator emission, arg ordering, `--model` omission when empty, and stderr wiring.
- **Commits**: 9c8519e loop: define Executor/GitProbe/SpecReader ports (P1.2); 976fd5d executor/claude: subprocess streaming with graceful cancel (P1.2); 0095c59 executor/fake: scripted Executor for loop tests (P1.2); b2b9f00 docs: mark P1.2 [x] and add journal entry
- **Decisions**: (1) Streaming via `cmd.Stdout = jsonlOut` instead of the `io.MultiWriter` mentioned in the spec text. The MultiWriter pattern only matters when there are two sinks; for P1.2 there is only one (the JSONL file), so direct assignment is simpler and avoids a goroutine. If P2 needs a live tee for the dashboard, we can add MultiWriter at the call site without touching the executor. (2) Ports live in `internal/loop/ports.go` (the consumer), per AGENTS.md's "interfaces in the consumer" rule. The adapter `internal/executor/claude` imports `internal/loop` to satisfy the contract; `internal/loop` does not import any adapter. Compile-time `var _ loop.Executor = (*Executor)(nil)` checks the contract on every build. (3) Graceful cancel uses Go 1.20+ `cmd.Cancel`/`cmd.WaitDelay` (we are on 1.24). SIGINT first, SIGKILL after 5s by default; configurable via `Config.CancelGrace`. We do NOT set up process groups; if claude spawns helpers, they may briefly leak after cancel. Acceptable for Phase 1; revisit if it becomes a problem. (4) On cancel, Run returns `(exitCode, ctx.Err())` so callers can `errors.Is(err, context.Canceled)`. On natural non-zero exit, returns `(exitCode, nil)`: a non-zero exit from the agent is a normal control signal, not a Run failure. Only invocation failures (binary missing, etc.) return `(-1, error)`. (5) Test fixtures are bash scripts under `internal/executor/claude/testdata/`, marked executable in git. Linux/macOS only; Windows would need a Go-built fake (out of scope for MVP).
- **Problems**: none.
- **Next**: P1.3 (loop controller and decider)

### 2026-04-29 12:00, P1.1: config + spec parsers

- **Result**: ok
- **Summary**: Implemented `internal/spec` (types, plan parser, journal parser, fixtures, table-driven tests covering en + pt-BR + edge cases) and `internal/config` (types with TOML tags, defaults per `project.language`, env merging with documented precedence and stdlib-only .env loader). Adapter `internal/configloader/toml` wraps BurntSushi/toml with `Load` and `Discover` (walks up). All P1.1 sub-items `[x]`. `gofmt`, `go vet`, `go test -race ./...`, `go build` all green. `bcc --help` builds and runs.
- **Commits**: 50c19bd spec: types and plan/journal parsers (P1.1); 68c664f config: types, defaults, and env precedence (P1.1); 7898758 configloader/toml: Load and Discover (P1.1); ca3859d docs: mark P1.1 [x], record journal entry, clean lingering "diary" refs
- **Decisions**: (1) Adjusted P1.1 plan to make `internal/configloader/toml/` a separate sub-item (was implicit in original P1.1.1, conflicted with the package layout). Hexagonal layout enforced: `internal/config` is stdlib-only, `internal/configloader/toml` is the only package importing `BurntSushi/toml`. (2) Did NOT add `joho/godotenv` even though the spec mentions it; the .env subset we need (KEY=VALUE, comments, optional quotes, `${VAR}` expansion via `os.ExpandEnv`) is small enough that ~30 lines of stdlib parser in `internal/config/env.go` suffices. If we ever need `export` keyword, multi-line values, or command substitution, swap in godotenv there. (3) `Result` is an `int` enum, not a string, with `ResultUnknown` as zero value to make uninitialized states explicit. `ResultVocab` decouples the typed enum from localized surface strings. (4) Plan parser models an "implicit phase" (Title="", Line=0) for items appearing before any H3 inside the plan section, so callers do not need a special-case path. (5) Journal parser stops scanning when it finds any `- **<keyword>**: ...` matching the configured Result keyword; first match wins, matching the bash awk's behavior of taking the latest entry (top of section).
- **Problems**: cobra command long descriptions and README still mentioned "diary" after the prior rename; caught at the end of the iteration (visible in `bcc --help`) and fixed in the same commit batch.
- **Next**: P1.2 (executor adapter with JSONL streaming)
