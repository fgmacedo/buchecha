# buchecha

A Go CLI that runs a coding agent against a Markdown spec in a phase-by-phase loop, with a strict journal-based handoff contract and a live status TUI. Replaces ad-hoc shell wrappers (Ralph-style) with a single binary, structured event streams, and observable execution.

## Why it exists

The spec-driven autonomous-loop pattern works: write a Markdown spec with a numbered Implementation Plan, point an agent (Claude Code, Codex, Gemini) at it, let it implement one phase per iteration, commit, write a journal entry, exit; outer loop reads the journal and decides whether to continue, stop, or escalate. Most existing implementations are bash scripts: opaque while running, project-bound, hard to share. `buchecha` keeps the pattern's discipline (Plan with `[x]`/`[ ]`, strict `**Result**` parsing, no scope transfer in prose) and rebuilds it as a portable Go tool with a live dashboard.

It is not a general-purpose AI orchestrator. It is **one loop, one spec, one binary**, and it stays small.

## Stack

- **Go 1.24** managed via mise (`.mise.toml`).
- **cobra** for the CLI surface.
- **BurntSushi/toml** for `.bcc.toml` config.
- **joho/godotenv** for `.env` loading.
- **charmbracelet/bubbletea + lipgloss + bubbles** for the TUI dashboard (Phase 2).
- **fsnotify** for file watching (Phase 2).
- **stdlib only** for everything else: `os/exec`, `encoding/json`, `bufio`, `context`, `log/slog`, `text/template`.

No `viper`, no `zap`/`zerolog`, no ORM, no DI framework. Resist them.

## Architecture: hexagonal, Go-idiomatic

The shape is ports and adapters, with the Go convention that **interfaces are defined where they are consumed**, not where they are implemented. There is no separate `domain/` or `ports/` directory; the domain is the set of packages that know nothing about the outside world.

### Layers (dependencies point downward)

```
cmd/                          (cobra wiring, argv parsing, exit codes)
        ↓
internal/tui/                 (bubbletea Model/Update/View; Phase 2)
        ↓
internal/loop/                (orchestration, decision table, prompt build,
                              defines Executor / GitProbe / SpecReader ports)
        ↓
internal/spec/                (Spec/Phase/Item/JournalEntry value objects, parsers)
internal/config/              (Config types, defaults, env precedence)
        ↑
internal/executor/<adapter>/  (e.g. claude/) implements loop.Executor
internal/git/<adapter>/       (e.g. cli/) implements loop.GitProbe
internal/specreader/<adapter>/ (e.g. markdown/) implements loop.SpecReader
internal/configloader/<adapter>/ (e.g. toml/) reads .bcc.toml into config.Config
```

Rules:

1. `internal/spec` and `internal/config` import **no adapters** and depend only on stdlib. They are pure value objects + pure parsers.
1. `internal/loop` imports `spec` and `config`, and defines its own ports (`ports.go`) for what it consumes. It does not import any adapter.
1. `internal/executor/claude/` (and future siblings) import `loop` to satisfy `loop.Executor`. It knows about subprocess, JSONL, env. The domain does not.
1. `internal/git/cli/` shells out to `git` via `os/exec`. Anything else that needs git later goes through the same port.
1. `internal/tui/` imports `loop` types and `watcher` events. Never the reverse.
1. `cmd/` wires everything: load config → build adapters → build loop → run.

Sign of trouble: a feature that touches 4+ packages probably crossed a layer wrongly. Stop and revisit.

## Domain language (DDD, lightweight)

The domain is small. We use the parts of DDD that pay off and skip the rest.

- **Value objects** (immutable, equality by value, no identity): `Result` (`ok`, `partial`, `done`, `blocked`), `Phase`, `Item`, `CommitSHA`, `JSONLPath`, `IterationID`. Construct via constructors that validate; never mutate.
- **Entities** (identity, lifecycle): `Spec` (identified by file path), `Iteration` (identified by index within a spec run).
- **Aggregate root**: `Spec` is the root for `Phase` and `Item`. Modifications go through the root.
- **Domain services** (behavior that does not belong on a single entity): `LoopDecider` (pure function on `(LatestResult, HEADAdvanced, UncheckedCount) → Action`), `Heuristic` (loop-suspect detector).
- **Ports**: `Executor`, `GitProbe`, `SpecReader`, `ConfigLoader`. Interfaces in the consumer package.
- **Adapters**: concrete implementations of ports, each in its own package.
- **Ubiquitous language** (use these names everywhere: code, comments, docs, prompts): spec, plan, phase, item, iteration, result, journal entry, checkbox, executor, watcher.

We do **not** use: domain events bus, CQRS, ubiquitous language clinics, factory/repository ceremonies. Too much for the size of this domain.

## Design principles

### SOLID, applied here

- **SRP**: each package changes for a single reason. `executor/claude` changes when Claude's CLI changes. `loop` changes when iteration semantics change. `spec` changes when the journal format changes. Mixed concerns are a smell.
- **OCP**: adding an agent is a new package under `executor/`, not edits to existing ones. Adding a heuristic is a new file in `loop/heuristics/`, not edits to the decider.
- **LSP**: any `Executor` implementation must honor the contract (cancellable via context, exit code propagated, JSONL written line-by-line). Tests use a fake executor that proves the contract.
- **ISP**: small interfaces. `Executor` has one method (`Run`). `GitProbe` has the few methods loop actually calls (`HeadSHA`, `IsClean`). No god interfaces.
- **DIP**: `loop` does not import `executor/claude`. It depends on the `loop.Executor` port. Adapters wire up at the cmd boundary.

### GRASP, where it helps

- **Information Expert**: `Spec` knows how to count its own checkboxes; `JournalEntry` knows its own format. The loop asks the spec, never reaches inside.
- **Low Coupling / High Cohesion**: small packages with their own vocabulary. No package called `util` or `helpers`. If a function does not belong somewhere, it does not belong.
- **Pure Fabrication**: `LoopDecider` is a fabrication, not a real-world concept. It exists to isolate the decision rules from I/O so they are trivially testable.
- **Indirection**: ports are the indirection between the loop and the outside world. Replacing `claude` with `codex` is a new adapter, not a loop edit.
- **Protected Variations**: agent CLI quirks live behind `Executor`. TOML quirks live behind `ConfigLoader`. Git quirks live behind `GitProbe`. Variations on each side are absorbed at the adapter.

### Orthogonality (Pragmatic Programmer)

A change in one dimension must not cascade into others.

- Replace `bubbletea` with another TUI: touches `internal/tui/` only.
- Add `codex` agent: new package `internal/executor/codex/`. No edit anywhere else.
- Switch from TOML to YAML for config: new adapter under `internal/configloader/`. The `Config` type does not move.
- Change spec format (e.g., add a new journal field): `internal/spec/` and `docs/guides/autonomous-execution.md` only.

Red flag: a feature requires editing four or more packages. Stop, revisit cohesion.

## Code conventions

### Formatting and tooling

- `gofmt`/`go fmt ./...` is law. CI fails on diff.
- `go vet ./...` zero issues.
- `golangci-lint` (Phase 3 onward) with a curated config. Until then, vet + gofmt are enough.
- Line length: idiomatic Go, no hard cap. Break lines where it improves reading, not by column.

### Naming

- Package names: lowercase, short, singular noun (`spec`, not `specs`; `loop`, not `looping`).
- Exported types: `CamelCase`. Unexported: `camelCase`.
- Receivers: short, consistent across methods of the same type (`s *Spec`, `l *Loop`).
- Test functions: `TestThing_Behavior`. Subtests: `t.Run("descriptive case", ...)`.
- File names: lowercase with underscore for clarity (`loop_test.go`, `parse_journal.go`).

### Errors

- Wrap with context: `fmt.Errorf("parse journal at %s: %w", path, err)`. Always `%w`.
- Sentinel errors as package-level vars: `var ErrUnknownResult = errors.New("unknown journal result")`.
- Compare with `errors.Is/As`, never string match.
- Domain errors carry enough context to diagnose without the stack trace.

### Context

- Any function that performs I/O or could block takes `ctx context.Context` as first parameter.
- Pass it down. Never store on a struct. Never use `context.Background()` deep in the call tree.

### Concurrency

- Channels and `select` for coordination.
- `sync.Mutex` only for shared state where channels would be baroque (rare in this codebase).
- All goroutines must have a clear lifecycle: who starts them, who waits for them, how they get canceled. No fire-and-forget.

### Logging

- `log/slog` from stdlib. No external logger.
- Structured: `slog.Info("loop iter complete", "iter", n, "result", res)`. Never `fmt.Sprintf` into messages.
- Levels: `Debug` for tracing, `Info` for milestones, `Warn` for recoverable issues, `Error` for aborts. Default level is `Info`.
- **Never log values of env vars or anything resembling a credential.** Names only. Adapters that handle env must enforce this.

### Comments

- Default: do not comment.
- Document the **why-not-obvious**: invariants, workarounds for external bugs, constraints from upstream tools. The "what" is the code.
- Public API needs a doc comment starting with the symbol name (`// Spec represents ...`). It is what `go doc` shows.
- Never `// TODO` without a concrete next action. Never `// removed X` or commit-message-shaped comments.

### Idiomatic Go gotchas to avoid

- Returning interface types from constructors: don't. Return concrete; let consumers narrow to interfaces.
- Empty interface `any` outside of generics or JSON edges: don't. Use a typed sum or explicit union.
- "Functional options" for configs with three fields: overkill. A struct literal is fine.
- Reflection: only as a last resort; tests cover it explicitly.
- `init()` for anything observable: avoid. Wire from `main` / `cmd/`.

## Testing model

We work in **TDD where it pays** and **retroactive coverage where it does not**. The areas where TDD pays in this codebase:

- Spec parsers (`internal/spec/`): given a markdown fixture, produce expected structure.
- Loop decider (`internal/loop/`): given inputs, produce expected action and exit code.
- Config loader (`internal/config/`): given TOML + env, produce expected resolved Config.

For adapters (subprocess, git, file watching), retroactive integration tests with fakes are fine.

### Layout

```
internal/<package>/
├── <file>.go
├── <file>_test.go              # same-package unit tests
└── testdata/
    └── <fixture-name>.md
```

Project-level testdata at `testdata/` for end-to-end fixtures (sample specs, sample JSONL streams).

### Style

- **Table-driven** is the default. Each row is a named case with inputs and expected output. `t.Run(tt.name, ...)`.
- Use **fakes**, not mocks. A fake `Executor` reads a scripted JSONL fixture and replays it. Mocking frameworks add ceremony without payoff at this scale.
- `go test -race ./...` always passes. Race conditions in TUI/watcher code are the #1 risk; the race detector catches most.
- No flaky tests. If a test depends on time, inject a `Clock` interface; if on filesystem, use `t.TempDir()`.
- Coverage is not a target. We do not chase percentages. We cover what would hurt us if it broke.

### Fixtures

- Spec fixtures in `internal/spec/testdata/`: tiny, hand-authored, one-purpose-per-file.
- JSONL fixtures in `internal/executor/claude/testdata/`: captured from real runs and trimmed; never include credentials or proprietary content.
- End-to-end fixtures in `testdata/specs/`: sample specs in English and pt-BR to validate localization.

### Self-hosting test loop (Phase 3+)

Once `bcc` is stable, every spec we run on the `bcc` repo itself is the strongest possible test: it exercises the full loop on real work. CI will have a smoke job that runs a tiny spec end-to-end against a stub executor.

## Tooling and commands

```bash
# setup
mise install                                  # installs Go from .mise.toml
go mod download

# develop
go build -o bcc .                             # local binary
go test ./...                                 # all tests
go test -race ./...                           # with race detector
go vet ./...                                  # static checks
gofmt -l .                                    # show formatting drift
gofmt -w .                                    # apply formatting

# run
./bcc --help
./bcc init
./bcc run docs/specs/<spec>.md
./bcc watch docs/specs/<spec>.md              # Phase 2

# release (Phase 3+)
goreleaser release --snapshot --clean         # local snapshot
git tag -a v0.1.0 -m '...' && goreleaser release
```

## Security and safety

- **Never log env-var values.** Adapters must enforce this. Names only, in any output.
- **Never write to user `.env` files** from `bcc`. Reading is fine where the user opted in via `[env].files`.
- **Subprocess args**: pass as a slice, never as a shell string. Avoid `bash -c`; use the agent binary directly.
- **`--dangerously-skip-permissions`** on Claude is the user's choice in their config, not our default. The wizard suggests it but does not force.
- **No telemetry.** No phone-home. No update check. The user runs the binary; the binary does its job and exits.
- **Versioned dependencies** in `go.mod`. We pin `cobra`, `bubbletea`, etc. to known-good versions; we audit upgrades.

## Language

- **All code, comments, docs, commit messages, and prompts in this repo are in English.**
- Localization is a runtime feature exposed through `.bcc.toml` (`project.language`). Specs in any language work; the keywords used to parse them (plan heading, journal heading, result values) come from the user's config.
- The default vocabulary embedded in the binary covers `en` and `pt-BR`. More languages added as PRs adding a row to the defaults table.
- **Never use the en-dash character (`—`) in prose.** Use commas, periods, or rephrase. Authorial preference, enforced.

## For the assistant (Claude Code, agents in autonomous execution)

- Before touching any package, scan the existing tests to understand the contract.
- Respect layer boundaries: never import an adapter from `internal/loop/` or `internal/spec/` or `internal/config/`. Wire adapters at `cmd/`.
- Never put a god `util` or `helpers` package. If a helper is small and obvious, inline it; if it is reused, it has a real home.
- Working tree clean between milestones. Use `git reset` (non-destructive) before `git add <specific paths>` before `git status -s` to confirm. Never `git add -A` blindly.
- Tests must pass on `go test -race ./...` before any commit that touches concurrent code.
- TODOs require a concrete next action. No `// TODO: improve this`.
- Commit messages: imperative mood, lowercase prefix matching `git log` style (`spec:`, `loop:`, `executor:`, `tui:`, `cmd:`, `docs:`, `refac:`). One commit per milestone.
- The `docs/specs/buchecha-mvp/index.md` is the live status tracker for the MVP. When a phase advances, update its checkbox in the same commit, and add a journal entry in the spec following the [Autonomous execution guide](docs/guides/autonomous-execution.md).
- When in doubt about whether a piece of code belongs on the domain side or the adapter side, ask: would replacing the agent (Claude → Codex) require touching this code? If yes, it is in the wrong place; move it to an adapter.

## Open knowledge

The state of the project is in `docs/specs/buchecha-mvp/index.md`. Read it first.
