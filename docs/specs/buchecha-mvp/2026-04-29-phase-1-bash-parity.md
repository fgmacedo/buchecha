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
area:
team:
tags:
  - phase-1
  - mvp
comments: true
---

# Phase 1: bash parity

## Summary

Port `scripts/exec-spec.sh` (~280 lines of bash) to a Go CLI subcommand `bcc run <spec>`, preserving the diary contract, exit codes, and overall behavior. Add `bcc init` interactive wizard that writes `.bcc.toml`. Add config layer with `[env]` support (`.env` loading + inline vars). No TUI in this phase.

## Context and motivation

The current shell wrapper works but is opaque, bash-bound, and project-specific. Phase 1 establishes the Go foundation that subsequent phases (TUI dashboard, multi-agent, releases) build on. Functional parity with the bash script is the success criterion: anywhere `scripts/exec-spec.sh foo.md` works, `bcc run foo.md` (with appropriate `.bcc.toml`) must work identically.

## Goals and non-goals

### Goals

- [ ] `bcc run <spec>` end-to-end functional parity with `scripts/exec-spec.sh`: phase loop, single-shot mode, max-iterations cap, HEAD-advanced check, diary parsing, exit codes 0/1/2/3/4/5.
- [ ] `bcc init` interactive wizard generates `.bcc.toml` with sensible defaults.
- [ ] Config layer (`internal/config`) loads `.bcc.toml`, applies defaults by `project.language`, supports `[env]` with `.env` files and `[env.vars]` inline.
- [ ] Spec parser (`internal/spec`) extracts plan headings, counts `[x]`/`[ ]` items, parses latest diary `**Result**`. Pure functions, table-driven tests.
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
│   │   ├── spec.go               # Spec, Phase, Item, DiaryEntry, Result
│   │   ├── plan.go               # ParsePlan, CountChecked, CountUnchecked
│   │   ├── diary.go              # ParseLatestResult
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
diary_heading = "## Execution Log"

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

`project.language = "pt-BR"` switches defaults for `specs.plan_heading`, `specs.diary_heading`, and `loop.results.*` to the pt-BR equivalents (`## Plano de implementação`, `## Diário de execução`, `ok` / `parcial` / `finalizado` / `bloqueado`).

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

1. [ ] `internal/config/config.go`: `Config` struct mirroring TOML schema; `Load(path) (*Config, error)`; `Discover(cwd) (*Config, error)` (walks up).
1. [ ] `internal/config/defaults.go`: `applyDefaults(c *Config)` filling missing values per `project.language`.
1. [ ] `internal/config/env.go`: `(c *Config) ApplyEnv(extraFlags []string) error` resolving precedence and exporting to `os.Setenv`. No values logged.
1. [ ] `internal/spec/headings.go`, `plan.go`, `diary.go`: `ParsePlan`, `CountChecked`, `CountUnchecked`, `ParseLatestResult`. Pure, table-driven tests against `testdata/specs/`.
1. [ ] `go test ./internal/config/... ./internal/spec/...` zero failures.

### P1.2: executor adapter with JSONL streaming

1. [ ] `internal/loop/ports.go`: define `Executor` interface (`Run(ctx, prompt, jsonlPath) (exitCode int, err error)`), plus `GitProbe`, `SpecReader`. All consumed by `Loop`.
1. [ ] `internal/executor/claude/claude.go`: implements `loop.Executor` invoking `claude -p --output-format stream-json --verbose ...`, streaming stdout to the JSONL file via `io.MultiWriter`, propagating exit code.
1. [ ] Cancellation via `context.Context` (Ctrl+C in foreground). On cancel, send SIGINT to the subprocess, drain stdout, write a final `{"type":"interrupted"}` line.
1. [ ] Fake executor under `internal/executor/fake/` for tests: replays a scripted JSONL fixture. Used by loop tests in P1.3.

### P1.3: loop controller

1. [ ] `internal/loop/loop.go`: `Loop.Run(ctx, spec, cfg) (exitCode int, err error)` implementing the bash decision table: per iteration, capture HEAD before; build prompt; invoke executor; capture HEAD after; parse latest `Result`; switch on value. Depends only on the ports defined in `ports.go`.
1. [ ] `internal/loop/exitcodes.go`: constants 0..5 with comments matching the table.
1. [ ] `internal/loop/prompt.go`: build prompts via `text/template`, parameterized by spec path, guide path, vocabulary, optional `--extra`.
1. [ ] `internal/loop/decider.go`: pure `Decider` function `(LatestResult, HEADAdvanced, UncheckedCount) → (Action, ExitCode)`. Trivially testable.
1. [ ] Single-shot mode: same logic, max-iterations forced to 1, prompt template variant.
1. [ ] Table-driven tests for the decider; loop tests using `executor/fake` and a temporary git repo.

### P1.4: cobra wiring + end-to-end

1. [ ] `cmd/run.go`: parse flags, load config via `configloader/toml`, apply env, instantiate `executor/claude` and `git/cli` and `specreader/markdown`, build `Loop`, invoke. Translate Go error to exit code.
1. [ ] `cmd/init.go`: linear stdin wizard, write `.bcc.toml`. `--force` and `--language` honored.
1. [ ] `cmd/watch.go`: print "not implemented yet (Phase 2)" and return exit 2.
1. [ ] End-to-end smoke test: a small spec in `testdata/specs/sample-en.md` driven by `executor/fake` that simulates two iterations (`partial`, `done`); verifies exit 0 and correct file outputs.

### P1.5: validation against `condo-fiscal`

1. [ ] Author manually creates `.bcc.toml` in `condo-fiscal` repo with `language = "pt-BR"`, points `[executor].binary` to `claude`, configures `[env.vars]` with `CLAUDE_CONFIG_DIR`.
1. [ ] Run `bcc run docs/specs/<a-finished-spec>.md --max-iterations 0` (dry-load) and confirm spec/plan/diary parsing matches what the bash awk produced.
1. [ ] Run `bcc run docs/specs/<a-real-pending-spec>.md` for one iteration with stub model (or smallest claude model) and confirm exit codes and diary parsing on real output.

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
1. Human decision: any deviation from the diary contract semantics (e.g., new `Result` value needed). Requires guide update first.

## Risks and mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Claude `stream-json` event shape changes | Medium | High | Tolerant parser: pass through unknown event types; tests against captured fixtures |
| Subprocess buffering breaks JSONL streaming | Medium | Medium | Use `bufio.Scanner` with explicit buffer size; test with slow producer |
| Localization defaults drift from `condo-fiscal` strings | Low | High | Snapshot test loading the actual `condo-fiscal` spec headings |
| Cobra flag handling diverges from bash flag order | Low | Low | Document flag mapping in README; tests cover all flags |

## Execution Log

(empty until Phase 1 is run)
