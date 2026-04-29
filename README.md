# buchecha

> Behavior-driven Coding Cycle for autonomous agent loops.

`bcc` runs a coding agent (Claude Code, Codex, Gemini) against a Markdown spec
in a phase-by-phase loop. Each iteration: read spec, implement one phase,
commit, write a structured journal entry, exit. The outer loop reads the journal
and decides whether to continue, stop, or escalate.

Status: **early development, not stable, not yet released.**

## Why

The Ralph-style "loop until done" pattern works, but most implementations are
opaque shell scripts. `buchecha` keeps the simplicity of the loop, adds a
strict journal contract for handoff between iterations, and ships a
single-binary CLI with a live status dashboard so you can see what the agent
is doing without piping logs.

## Roadmap

See [`docs/specs/buchecha-mvp/index.md`](docs/specs/buchecha-mvp/index.md).

## License

MIT. See [LICENSE](LICENSE).
