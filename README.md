# buchecha

> Director-driven coding pipeline for autonomous agent loops.

`bcc` runs a four-role pipeline against a Markdown spec: a Planner produces a typed DAG of phases and tasks, a Briefer picks the next sub-DAG to execute, an Executor edits the working tree, and a Reviewer audits per-task outcomes. All four roles communicate exclusively through an in-process MCP server. bcc owns the loop, per-session state, and the live status TUI.

Status: **early development, not stable, not yet released.**

## Why

Driving a single agent through a long Markdown spec is unreliable: the agent loses focus, declares premature `done`, drifts on scope. `bcc` replaces the single-loop pattern with separate cognitive roles (plan, brief, execute, review), each with its own context, all coordinated by bcc through MCP. The discipline that made the pattern work (typed plan, explicit acceptance criteria, clean working tree per iteration) is preserved; the supervision tax that one agent could not pay for itself moves into the Director.

## Quick start

```bash
# build
go install ./cmd/bcc

# scaffold .bcc.toml in the current project
bcc init

# run a spec end to end
bcc run docs/specs/<your-spec>.md

# inspect past runs
bcc sessions list
bcc sessions show <id>
```

See [`docs/guides/director.md`](docs/guides/director.md) ([pt-BR](docs/guides/director.pt-BR.md)) for the operator reference: sessions, MCP method surface, escalation, troubleshooting.

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

## Tooling

The project uses a Makefile for the build pipeline:

```bash
make api-openapi     # Generate OpenAPI 3.1 spec from internal/api/
make webui           # Build the SPA bundle (depends on api-openapi)
make build           # Build the bcc binary (depends on webui)
```

These targets are chained: `make build` regenerates the OpenAPI spec and SPA bundle if the API or SPA sources have changed.

## Roadmap

The current architecture is captured in [`docs/specs/director/index.md`](docs/specs/director/index.md). Open enhancements live as GitHub issues tagged `enhancement`.

## License

MIT. See [LICENSE](LICENSE).
