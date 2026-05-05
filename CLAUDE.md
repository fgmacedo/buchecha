# buchecha

A Go CLI that runs a Director-driven coding pipeline against a Markdown spec: a Planner produces a typed DAG of phases and tasks, a Briefer picks the next sub-DAG, an Executor edits the working tree, and a Reviewer audits per-task outcomes. All four roles communicate exclusively through an in-process MCP server. bcc owns the loop, per-session state, and the live TUI.

## Why it exists

Driving a single agent through a long Markdown spec is unreliable: the agent loses focus, declares premature `done`, drifts on scope. bcc replaces the single-agent shell wrapper pattern with an orchestrated pipeline: separate cognitive roles for planning, briefing, executing, and reviewing, each with its own context, all coordinated by bcc through MCP. The discipline that made the single-loop pattern work (typed plan, explicit acceptance criteria, no scope transfer in prose, clean working tree per iteration) is preserved; the supervision tax that one agent could not pay for itself moves into the Director.

It is not a general-purpose multi-agent framework. It is **one orchestrator, one spec per session, one binary**, with a fixed set of roles and a small wire surface. It stays small.

## Stack

- **Go 1.24** managed via mise (`.mise.toml`).
- **cobra** for the CLI surface.
- **BurntSushi/toml** for `.bcc.toml` config.
- **joho/godotenv** for `.env` loading.
- **charmbracelet/bubbletea + lipgloss + bubbles** for the TUI dashboard.
- **santhosh-tekuri/jsonschema** for per-method MCP input validation.
- **github.com/go-chi/chi/v5** for HTTP API routing.
- **github.com/danielgtaylor/huma/v2** for OpenAPI generation and validation.
- **stdlib only** for everything else: `os/exec`, `encoding/json`, `bufio`, `context`, `log/slog`, `text/template`, `net/http` (for the MCP handler).

No `viper`, no `zap`/`zerolog`, no ORM, no DI framework. Resist them.

## Architecture: hexagonal, Go-idiomatic

The shape is ports and adapters, with the Go convention that **interfaces are defined where they are consumed**, not where they are implemented. There is no separate `domain/` or `ports/` directory; the domain is the set of packages that know nothing about the outside world.

### Layers (dependencies point downward)

```
cmd/                             (cobra wiring, argv parsing, exit codes)
        ↓
internal/cli/                    (run / init / sessions subcommands; MCP boot;
                                  session resolution; render dispatch)
        ↓
internal/tui/                    (bubbletea Model/Update/View)
internal/api/                    (HTTP API server, chi router, handlers)
internal/webui/                  (embedded SPA bundle http.Handler)
        ↓
internal/services/               (application services layer: SessionService,
                                  EventService, BriefingService, PromptService,
                                  read APIs over the core, audit log, events
                                  fan-out with seq numbers, ring buffer, closed
                                  error enum)
        ↓
internal/loop/                   (DAG-driven loop driver, decider, exit codes;
                                  defines Executor / GitProbe ports)
internal/loop/agentcontract/     (shared markdown blocks composed into prompts:
                                  wire_protocol.md, absolute_restrictions.md,
                                  working_tree.md; Signal / AgentEvent types)
internal/director/               (Plan / Phase / Task / Briefing types;
                                  Planner / Briefer / Reviewer ports;
                                  embedded prompts; Session / Store; journal
                                  delta helper)
internal/director/dag/           (in-memory DAG state, agent registry,
                                  per-method MCP handler dispatch, audit log)
internal/director/schemas/mcp/   (per-method JSON schemas embedded via go:embed)
internal/config/                 (Config types, defaults, env precedence)
internal/mcp/                    (stdlib HTTP MCP server with per-connection
                                  auth; mounted at /mcp/* on the API listener)
        ↑
internal/director/claude/        (claude adapter for Planner/Briefer/Reviewer)
internal/executor/<adapter>/     (e.g. claude/) implements loop.Executor
internal/git/<adapter>/          (e.g. cli/) implements loop.GitProbe
internal/configloader/<adapter>/ (e.g. toml/) reads .bcc.toml into config.Config
```

The application services layer (`internal/services/`) is the only caller of the core from above. It owns business operations: read APIs over core state, the audit log, and the events fan-out system with monotonic sequence numbers and a ring buffer of recent events. Protocol adapters (TUI, API, MCP) consume services and are peers at the protocol layer; they expose a behavioral surface (TUI bubbletea program, HTTP API router, MCP handler) while presentation adapters (WebUI SPA) consume the protocol layer but own no business logic themselves.

Rules:

1. `internal/loop/agentcontract`, `internal/director` (top-level), and `internal/config` import **no adapters** and depend only on stdlib (plus the embedded JSON schema lib in `director/dag`). They are pure value objects + pure parsers.
1. `internal/loop` imports `agentcontract`, `config`, `director`, and `director/dag`. It defines its own ports (`ports.go`) for what it consumes from outside (Executor, GitProbe). It does not import any executor or git adapter.
1. `internal/director/dag` owns the in-memory DAG state, the agent registry, and the MCP handler. It imports `internal/director` for typed value objects (Plan, Phase, Task, TaskStatus, Briefing) and stdlib only.
1. `internal/services/` owns business operations and is the sole caller of the core. It exports read-only service methods plus the audit log and error enum. Protocol adapters route every read and (V2+) every mutation through internal/services; they import the core only for typed value objects.
1. `internal/director/claude/` (and future siblings) implement the Director ports (Planner, Briefer, Reviewer). They know about the agent CLI, stream-json, and per-spawn `mcp-config` files.
1. `internal/executor/claude/` (and future siblings) implements `loop.Executor`. It knows about subprocess, stream-json, env, and the MCP wiring (URL, token, role connection name, agent_id) bcc passes via Config. The loop does not.
1. `internal/api/` and `internal/mcp/` are protocol adapters that consume `internal/services/`. The API uses chi router and huma for OpenAPI; the MCP is a stdlib-only HTTP handler with per-connection authorization mounted at `/mcp/*`. Both depend on services, not on the core directly.
1. `internal/webui/` is a presentation adapter that exposes an http.Handler serving the embedded SPA bundle. It imports stdlib only and has no knowledge of services, core, or other adapters.
1. `internal/tui/` imports `services`, `loop` types, `director` types, and `agentcontract` types. Never the reverse.
1. `internal/git/cli/` shells out to `git` via `os/exec`. Anything else that needs git later goes through the same port.
1. `internal/cli/` wires everything: load config → build services → build adapters → build loop → run.

Sign of trouble: a feature that touches 4+ packages probably crossed a layer wrongly. Stop and revisit.

bcc never inlines the user's spec content into prompts. Each role receives only the spec path; it reads the file via the Read tool. The DAG state, briefings, per-task outcomes, and review verdicts all flow through MCP method calls served by the run-wide handler.

### Framework and user-space boundary

`internal/` is framework space. `docs/` is user space. bcc reads from `internal/` at runtime (compiled-in resources, including embedded prompts and JSON schemas); it never reads from `docs/`.

The boundary cuts both ways:

1. The framework cannot rely on user-space files. Project docs, `CLAUDE.md`, `AGENTS.md`, custom skills are the user's. A user running `bcc` outside this repo may have none of them, or may have ones that conflict with bcc's contract. Code that reads user-space paths from inside the framework is a leak.
1. The framework prompts are assertive and self-contained. Per-role prompts live at `internal/director/prompts/{plan,brief,review}.md` and the executor prompt is generated from the rendered Briefing on disk. All prompts compose the shared markdown blocks from `internal/loop/agentcontract/` (`wire_protocol.md`, `absolute_restrictions.md`, `working_tree.md`).

## Domain language (DDD, lightweight)

The domain is small. We use the parts of DDD that pay off and skip the rest.

- **Value objects** (immutable, equality by value, no identity): `Plan`, `Phase`, `Task`, `TaskStatus` (`pending` / `in_progress` / `done` / `needs_fix`), `Briefing`, `Signal` (the iteration outcome string the Executor reports via `bcc_iteration_finished`), `AgentEvent`, `CommitSHA`, `IterationID`, `AgentID`, `Role`. Wire-protocol partials live in `internal/loop/agentcontract/`; Director value objects live in `internal/director/`.
- **Entities** (identity, lifecycle): `Session` (a `bcc run` instance, identified by id), `Iteration` (per phase + attempt within a session).
- **Domain services** (behavior that does not belong on a single entity): `DirectorDecide` (per-task aggregation: advance / retry / escalate / abort), `ValidatePlan` (DAG cycle detection at phase and task levels).
- **Ports**: `Executor`, `GitProbe`, `Planner`, `Briefer`, `Reviewer`, `JournalDeltaProvider`, `GitDiffProvider`. Interfaces in the consumer package.
- **Adapters**: concrete implementations of ports, each in its own package.
- **Ubiquitous language** (use these names everywhere: code, comments, docs, prompts): spec, plan, phase, task, iteration, briefing, sub-DAG, session, agent_id, role, MCP method, contract, executor, planner, briefer, reviewer, verdict outcome (`approve` / `revise` / `escalate`).

We do **not** use: domain events bus, CQRS, ubiquitous language clinics, factory/repository ceremonies. Too much for the size of this domain.

## Design principles

### SOLID, applied here

- **SRP**: each package changes for a single reason. `executor/claude` changes when Claude's CLI changes. `director/dag` changes when MCP method semantics change. `director/claude` changes when the per-role agent invocation changes. Mixed concerns are a smell.
- **OCP**: adding a new agent vendor is a new package under `executor/` and `director/<vendor>/`, not edits to existing ones. Adding a new MCP method is a new schema + a new dispatch entry, not surgery on the dispatch table.
- **LSP**: any `Executor` or Director-role implementation must honor the contract (cancellable via context, exit code propagated, agent events streamed line-by-line, MCP calls obey scope authz). Tests use fakes that prove the contract.
- **ISP**: small interfaces. `Executor` has one method (`Run`). `Planner`/`Briefer`/`Reviewer` have one method each. `GitProbe` has the few methods the loop actually calls. No god interfaces.
- **DIP**: `loop` does not import `executor/claude` or `director/claude`. It depends on the ports. Adapters wire up at the cli boundary.

### Orthogonality (Pragmatic Programmer)

A change in one dimension must not cascade into others.

- Replace `bubbletea` with another TUI: touches `internal/tui/` only.
- Add a `codex` agent: new packages `internal/executor/codex/` and `internal/director/codex/`. No edit to `loop` or `director` (top-level).
- Switch from TOML to YAML for config: new adapter under `internal/configloader/`. The `Config` type does not move.
- Add an MCP method: new JSON schema under `internal/director/schemas/mcp/`, new handler function in `internal/director/dag/handler.go`, new prompt instruction. No loop edit.

Red flag: a feature requires editing four or more packages. Stop, revisit cohesion.

## Code conventions

### Formatting and tooling

- `gofmt`/`go fmt ./...` is law. CI fails on diff.
- `go vet ./...` zero issues.
- Line length: idiomatic Go, no hard cap. Break lines where it improves reading, not by column.

### Naming

- Package names: lowercase, short, singular noun (`director`, not `directors`; `loop`, not `looping`).
- Exported types: `CamelCase`. Unexported: `camelCase`.
- Receivers: short, consistent across methods of the same type (`s *Session`, `l *Loop`, `h *Handler`).
- Test functions: `TestThing_Behavior`. Subtests: `t.Run("descriptive case", ...)`.
- File names: lowercase with underscore for clarity (`director_run.go`, `parse_journal.go`).

### Errors

- Wrap with context: `fmt.Errorf("director: register briefer: %w", err)`. Always `%w`.
- Sentinel errors as package-level vars: `var ErrSessionNotFound = errors.New("session not found")`.
- Compare with `errors.Is/As`, never string match.
- Domain errors carry enough context to diagnose without the stack trace.

### Context

- Any function that performs I/O or could block takes `ctx context.Context` as first parameter.
- Pass it down. Never store on a struct. Never use `context.Background()` deep in the call tree.

### Concurrency

- Channels and `select` for coordination.
- `sync.Mutex` only for shared state where channels would be baroque (the DAG state and registry use a mutex; that is the right tool).
- All goroutines must have a clear lifecycle: who starts them, who waits for them, how they get canceled. No fire-and-forget.

### Logging

- `log/slog` from stdlib. No external logger.
- Structured: `slog.Info("director iter finished", "phase", phaseID, "attempt", n, "signal", sig)`. Never `fmt.Sprintf` into messages.
- Levels: `Debug` for tracing, `Info` for milestones, `Warn` for recoverable issues, `Error` for aborts. Default level is `Info`.
- **Never log values of env vars or anything resembling a credential.** Names only. Adapters that handle env must enforce this.

### Comments

- Default: do not comment.
- Document the **why-not-obvious**: invariants, workarounds for external bugs, constraints from upstream tools. The "what" is the code.
- Public API needs a doc comment starting with the symbol name (`// Plan represents ...`). It is what `go doc` shows.
- Never `// TODO` without a concrete next action. Never `// removed X` or commit-message-shaped comments.

### Idiomatic Go gotchas to avoid

- Returning interface types from constructors: don't. Return concrete; let consumers narrow to interfaces.
- Empty interface `any` outside of generics or JSON edges: don't. Use a typed sum or explicit union.
- "Functional options" for configs with three fields: overkill. A struct literal is fine.
- Reflection: only as a last resort; tests cover it explicitly.
- `init()` for anything observable: avoid. Wire from `main` / `cmd/`.

## Testing model

We work in **TDD where it pays** and **retroactive coverage where it does not**. The areas where TDD pays in this codebase:

- DAG validator (`internal/director`): cycle detection, scope checks, schema enforcement.
- MCP handler dispatch (`internal/director/dag`): per-method handlers under valid input, missing/unregistered `agent_id`, role mismatches, scope violations, cross-method invariants.
- Director decider (`internal/loop`): per-task aggregation produces expected action and exit code.
- Config loader (`internal/config`): given TOML + env, produce expected resolved Config.

For adapters (subprocess, git, MCP transport), retroactive integration tests with fakes are fine.

### Layout

```
internal/<package>/
├── <file>.go
├── <file>_test.go              # same-package unit tests
└── testdata/
    └── <fixture-name>.md
```

Project-level testdata at `testdata/` for end-to-end fixtures (sample specs, sample stream-json streams).

### Style

- **Table-driven** is the default. Each row is a named case with inputs and expected output. `t.Run(tt.name, ...)`.
- Use **fakes**, not mocks. Fakes for Director roles call into the handler in-process to mutate DAG state without going through HTTP.
- `go test -race ./...` always passes. Race conditions in TUI/handler code are the #1 risk; the race detector catches most.
- No flaky tests. If a test depends on time, inject a `Clock` interface; if on filesystem, use `t.TempDir()`.
- Coverage is not a target. We do not chase percentages. We cover what would hurt us if it broke.
- **Anti-drift contract: `loop.AllEventKinds` and `internal/api/schemas/event.schema.json`.** The wire-level enum the SPA fetches at runtime mirrors the canonical `loop.AllEventKinds` slice. Adding an `Event` variant (including `spawn_started` and `spawn_finished`) requires updating the `MarshalJSONEvent` switch, `AllEventKinds`, the schema enum, and the sample table in `TestMarshalJSONEvent_AllKindsCovered`. Two tests fail loudly if any of these drift. See [`docs/how-to/event-stream-troubleshooting.md`](docs/how-to/event-stream-troubleshooting.md).

### Fixtures

- DAG state fixtures inline in tests; round-trip through `dag.SaveStateFile` / `dag.LoadStateFile`.
- Stream-json fixtures in `internal/executor/claude/testdata/`: captured from real runs and trimmed; never include credentials or proprietary content. The fixture covers wire-protocol `tool_use` envelopes (`mcp__bcc__bcc_*`) alongside ordinary built-in tools so the parser path is exercised end to end.
- End-to-end fixtures in `testdata/specs/`: sample specs in English and pt-BR to validate localization.

## Tooling and commands

```bash
# setup
mise install                                  # installs Go from .mise.toml
go mod download

# develop
go build -o bcc ./cmd/bcc                     # local binary
go install ./cmd/bcc                          # install to $GOBIN
go test ./...                                 # all tests
go test -race ./...                           # with race detector
go vet ./...                                  # static checks
gofmt -l .                                    # show formatting drift
gofmt -w .                                    # apply formatting

# run
./bcc --help
./bcc init
./bcc run docs/specs/<spec>.md                # opens live TUI by default; --output text|json for headless
./bcc sessions list                           # list past runs under .bcc/sessions/
./bcc sessions show <id>                      # inspect one session's manifest
                                              # session layout: manifest.json, plan.json,
                                              # dag.json, briefings/<iteration_id>.prompt.md,
                                              # spawns/<spawn_id>.md, mcp-log.jsonl

# release
goreleaser release --snapshot --clean         # local snapshot
git tag -a v0.1.0 -m '...' && goreleaser release
```

## Security and safety

- **Never log env-var values.** Adapters must enforce this. Names only, in any output.
- **Never write to user `.env` files** from `bcc`. Reading is fine where the user opted in via `[env].files`.
- **Subprocess args**: pass as a slice, never as a shell string. Avoid `bash -c`; use the agent binary directly.
- **MCP authz**: per-connection role allow-list plus per-call `agent_id` validation against the registry. The handler rejects out-of-scope task mutations.
- **No telemetry.** No phone-home. No update check. The user runs the binary; the binary does its job and exits.
- **Versioned dependencies** in `go.mod`. We pin `cobra`, `bubbletea`, etc. to known-good versions; we audit upgrades.

## Autonomy and the permission contract

`bcc run` invokes each role agent in non-interactive print mode. To complete an iteration end to end, the Executor must run reads, writes, edits, and shell commands without prompting the human for each one. Without that, the loop stalls. The Director roles (Planner, Briefer, Reviewer) run with `Read,Bash,Grep,Glob` and the same permission discipline so they can read the spec, the working tree, and run light verification commands.

`[agent.<name>].skip_permissions` (default `true`) controls this:

- **`true`**: the adapter passes the agent's "skip permission prompts" flag (claude maps this to `--dangerously-skip-permissions`; codex/gemini map to their own equivalents). The agent runs reads, writes, edits, and shell commands inside the project directory autonomously. The user accepts that risk.
- **`false`**: explicit opt-out. `bcc run` still launches the agents, but tool calls that would have prompted are aborted or skipped silently. The loop is unlikely to converge. Useful for dry-runs or for agents that have no permission system.

`bcc run` prints a loud warning on stderr at startup describing which mode is active and what the user is accepting. The wizard (`bcc init`) presents the same trade-off and requires an explicit choice; the default is `true` for autonomous mode.

The absolute restrictions embedded in [`internal/loop/agentcontract/absolute_restrictions.md`](internal/loop/agentcontract/absolute_restrictions.md) (no `git push`, no force operations, no touching credentials, etc.) are independent of this flag and cannot be relaxed by it.

## Language

- **All code, comments, docs, commit messages, and prompts in this repo are in English.**
- Localization is a runtime feature exposed through `.bcc.toml` (`project.language`). User-facing strings (TUI labels, error messages) localize per language; the wire protocol stays canonical English regardless.
- The default vocabulary embedded in the binary covers `en` and `pt-BR`. More languages added as PRs adding a row to the defaults table.
- **Never use the en-dash character (`—`) in prose.** Use commas, periods, or rephrase. Authorial preference, enforced.

## For the assistant (Claude Code, agents in autonomous execution)

- This is a solo project (one author plus you). When a better port shape, type, layout, or naming choice emerges, propose the breaking change directly and ship it. No backwards-compatibility shims, deprecation aliases, or parallel old-and-new APIs unless explicitly requested. Compatibility scaffolding only matters once external users exist.
- **Specs are normative, not historical.** Describe what to build, not how the spec got here. When refining a spec, rewrite the affected text in place. Do not narrate the change with "the previous version did X", "after the prior draft", "REMOVED:", "now we changed to Y", or "Breaking changes from previous spec". Each rewrite must read as if the doc were always this way. Design history belongs in commit messages and in the spec's Execution Journal, not in the body. Same rule applies to ADRs, PRDs, initiative docs, and any other design doc under `docs/`. Apply it equally to your own first drafts: write the target state directly, never the diff.
- Before touching any package, scan the existing tests to understand the contract.
- Respect layer boundaries: never import an adapter from `internal/loop/`, `internal/director/`, `internal/director/dag/`, or `internal/config/`. Wire adapters at the cli boundary.
- Never put a god `util` or `helpers` package. If a helper is small and obvious, inline it; if it is reused, it has a real home.
- Working tree clean between milestones. Use `git reset` (non-destructive) before `git add <specific paths>` before `git status -s` to confirm. Never `git add -A` blindly.
- Tests must pass on `go test -race ./...` before any commit that touches concurrent code.
- TODOs require a concrete next action. No `// TODO: improve this`.
- Commit messages: imperative mood, lowercase prefix matching `git log` style (`spec:`, `loop:`, `director:`, `executor:`, `tui:`, `cmd:`, `cli:`, `config:`, `docs:`, `refac:`). One commit per milestone.
- When in doubt about whether a piece of code belongs on the domain side or the adapter side, ask: would replacing the agent (Claude → Codex) require touching this code? If yes, it is in the wrong place; move it to an adapter.

## Open knowledge

The state of the project is in [`docs/specs/director/index.md`](docs/specs/director/index.md), with the normative model in [`docs/specs/director/2026-05-02-executable-plan-dag.md`](docs/specs/director/2026-05-02-executable-plan-dag.md) and the implemented migration captured by [`docs/specs/director/2026-05-02-reviewed-execution-corrections.md`](docs/specs/director/2026-05-02-reviewed-execution-corrections.md). Read those first.

How-to: troubleshoot the SSE event stream end to end (cooperative-spec methodology, wire contract, common pitfalls) in [`docs/how-to/event-stream-troubleshooting.md`](docs/how-to/event-stream-troubleshooting.md).
