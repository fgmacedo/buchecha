# buchecha

> Director-driven coding pipeline for autonomous agent loops.

`bcc` runs a four-role pipeline against a Markdown spec: a Planner produces a typed DAG of phases and tasks, a Briefer picks the next sub-DAG to execute, an Executor edits the working tree, and a Reviewer audits per-task outcomes. All four roles communicate exclusively through an in-process MCP server. bcc owns the loop, per-session state, and the live status TUI.

Status: **early development, not stable, not yet released.**

## Why

Driving a single agent through a long Markdown spec is unreliable: the agent loses focus, declares premature `done`, drifts on scope. `bcc` replaces the single-loop pattern with separate cognitive roles (plan, brief, execute, review), each with its own context, all coordinated by bcc through MCP. The discipline that made the pattern work (typed plan, explicit acceptance criteria, clean working tree per iteration) is preserved; the supervision tax that one agent could not pay for itself moves into the Director.

## Quick start

```bash
# build the local binary into ./bcc (includes the embedded webui bundle)
make build

# or install it on $GOBIN / $GOPATH/bin so `bcc` is on your PATH
make install

# scaffold .bcc.toml in the current project
bcc init

# run a spec end to end
bcc run docs/specs/<your-spec>.md

# inspect past runs
bcc sessions list
bcc sessions show <id>
```

See [`docs/guides/director.md`](docs/guides/director.md) ([pt-BR](docs/guides/director.pt-BR.md)) for the operator reference: sessions, MCP method surface, escalation, troubleshooting.

## Onboarding for new developers

### 1. Prerequisites

Toolchain versions are pinned in [`.mise.toml`](.mise.toml). The simplest path is to install [mise](https://mise.jdx.dev/) and let it manage them:

```bash
mise install            # installs Go and Node at the versions this repo expects
```

Without mise, install equivalent versions yourself:

- **Go** matching `.mise.toml` (currently 1.25.x). `make build` invokes `go build`.
- **Node 22.x + npm**. Required to build the embedded SPA used by `--webui`. `make webui` runs `npm ci && npm run build` under `internal/webui/web/`.
- **git**. The Director shells out to `git` for diffs and probes.
- **An agent CLI** (e.g. `claude`) on `$PATH` for `bcc run` to drive a real pipeline. Unit tests do not need it.

### 2. Clone and build

```bash
git clone https://github.com/<org>/buchecha.git
cd buchecha
mise install            # if using mise
go mod download
make build              # produces ./bcc
./bcc --help
```

The first `make build` is the slowest: it generates the OpenAPI spec, runs `npm ci` for the SPA, builds the SPA bundle, and finally compiles the Go binary. Later builds skip the steps whose inputs did not change.

If you only touch Go files and do not need to rebuild the dashboard, `make install` (which depends on `check-build` only) is faster. Use `make build` whenever API schemas or SPA sources change.

### 3. Inner dev loop

```bash
make test               # full unit test suite
make test-race          # same, with the race detector (required before commits to concurrent code)
make vet                # go vet ./...
make fmt                # gofmt -w .
make fmt-check          # CI-style check, fails on diff
make tidy               # go mod tidy
make clean              # remove ./bcc
```

Targeted tests during iteration:

```bash
go test ./internal/director/...
go test -run TestDirectorDecide ./internal/loop
```

### 4. Where to read first

- [`CLAUDE.md`](CLAUDE.md) and [`AGENTS.md`](AGENTS.md): architecture, layer boundaries, conventions, and the "for the assistant" contract. Read these before editing any package.
- [`docs/specs/director/index.md`](docs/specs/director/index.md): current state of the project and pointers to the normative specs.
- [`docs/guides/director.md`](docs/guides/director.md): operator reference for `bcc run`, sessions, MCP method surface, escalation, and troubleshooting.
- [`docs/how-to/event-stream-troubleshooting.md`](docs/how-to/event-stream-troubleshooting.md): end-to-end SSE event stream debugging and the anti-drift contract between `loop.AllEventKinds` and the SPA schema.
- [`internal/`](internal/): the code. Layer rules in `CLAUDE.md` are load-bearing; respect them.

### 5. House rules

- All code, comments, docs, commit messages, and prompts in this repo are in English. Localization is a runtime feature, not a source-tree feature.
- Never use the en-dash character in prose. Commas, periods, or rephrase.
- Working tree clean between milestones. Never `git add -A` blindly.
- One commit per milestone, imperative mood, lowercase prefix matching `git log` style (`spec:`, `loop:`, `director:`, etc.).

## Run

Core command syntax:

```bash
# TUI mode (default): opens the live dashboard in the terminal
bcc run docs/specs/<spec>.md

# Headless text output: progress logged to stderr, result to stdout
bcc run --output text docs/specs/<spec>.md

# Headless JSON output: structured result to stdout
bcc run --output json docs/specs/<spec>.md

# HTTP API with browser dashboard
bcc run --webui docs/specs/<spec>.md

# HTTP API only (no dashboard)
bcc run --api docs/specs/<spec>.md
```

The `--webui` flag enables the embedded web dashboard at `http://127.0.0.1:<port>/?t=<token>` and launches the browser automatically with `-W`. The dashboard is read-only in V1, matching TUI inspection capabilities. See [`docs/surface-coverage.md`](docs/surface-coverage.md) for the live capability matrix.

The `--api` flag exposes the HTTP API at `/api/v1/*` and the OpenAPI document at `/api/v1/openapi.json`. Both `--api` and `--webui` use the same listener; the dashboard implies the API if not explicitly set.

## Build pipeline

The Makefile chains three stages so a single `make build` produces a binary with a fresh API contract and a fresh SPA bundle embedded:

```bash
make api-openapi     # generate OpenAPI 3.1 spec from internal/api/
make webui           # build the SPA bundle into internal/webui/web/dist/ (depends on api-openapi)
make webui-size      # enforce the SPA bundle size ceiling (depends on webui)
make build           # build the ./bcc binary with the SPA embedded (depends on webui-size + check-build)
make install         # go install ./cmd/bcc into $GOBIN (skips webui rebuild; use after `make build` if needed)
```

Use `make install` for the fastest Go-only iteration. Use `make build` whenever API schemas or SPA sources change so the embedded bundle stays in sync with the binary.

## Roadmap

The current architecture is captured in [`docs/specs/director/index.md`](docs/specs/director/index.md). Open enhancements live as GitHub issues tagged `enhancement`.

## License

MIT. See [LICENSE](LICENSE).
