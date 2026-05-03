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

## Roadmap

The current architecture is captured in [`docs/specs/director/index.md`](docs/specs/director/index.md). Open enhancements live as GitHub issues tagged `enhancement`.

## License

MIT. See [LICENSE](LICENSE).
