---
title: Autonomous spec execution
---

# Autonomous spec execution

Operational contract for agents (Claude Code, Codex, Gemini, others) that execute a spec without immediate human supervision. The author invokes from the command line, walks away, and reviews later.

This guide is the **default**. An individual spec may strengthen or relax specific points in its body, and those customizations take precedence. When the spec does not mention a point, this guide applies.

## How to invoke

```bash
bcc run docs/specs/<spec>.md
```

`bcc` reads `.bcc.toml` in the current project, invokes the configured agent in a phase-by-phase loop by default, and decides based on each journal entry. Detail in [Command line](#command-line) below.

## Execution mode

Default: **phase-by-phase loop** (Ralph-style). `bcc` invokes a fresh instance of the agent for each pending phase. Each instance:

1. Reads the spec, this guide, and the journal.
1. Identifies the next phase with `[ ]` items in the plan.
1. Implements only that phase end to end (code, tests, commits, `[x]` checkboxes).
1. Writes a new entry on top of the Execution Journal with `**Result**: ok | partial | blocked | done`.
1. Exits.

The outer loop reads `Result` from the latest entry and decides:

1. `ok` or `partial` → next iteration.
1. `blocked` → stop, return non-zero exit, requires human review.
1. `done` → stop, exit 0, spec complete.

Safety signal: if `HEAD` did not advance during the iteration (the agent did not commit), the loop aborts. Prevents infinite loops when a child fails to write or commit.

Alternative mode: **single-shot** via `--single-shot`. One instance tries everything. Useful for one-phase specs or when per-iteration bootstrap overhead does not pay off.

## Spec prerequisites

To be executed autonomously, a spec must have:

1. **Normative language and literal identifiers.** Routes, modules, parameters, and defaults appear by name and with values. No narrative prose in the body. The agent implements what is written; what is not written becomes its invention.
1. Implementation Plan with numbered phases and `[ ]` checkboxes on every item.
1. Objective done criteria per phase (tests, lint, etc.) or explicit inheritance of this guide's defaults.
1. Objective stop criteria (success, block, human decision).
1. Real ambiguities surfaced as "Open questions". The agent stops on these instead of guessing.
1. An empty "Execution Journal" section at the end, for the agent to fill.
1. Where parallelism is allowed, indicate boundaries (which phases, how many workers, what unit each worker takes).

The template at `docs/templates/spec.md` ships with all of this.

## Operating mode

Each instance (one per iteration in loop mode, or a single one in single-shot) must:

1. Decide by the most conservative documented criterion. **Do not ask.**
1. Work on a fresh branch created from `main`. Branch name pattern: `<type>/<short-slug>` (e.g., `refac/api-ports-adapters`, `feat/web-search-ui`). Never commit directly to `main`. On subsequent loop iterations, reuse the same branch (verify with `git branch --show-current`).
1. Each milestone becomes a small commit following the project's commit style from recent `git log` (prefixes like `parser:`, `api:`, `refac:`, `docs:`).
1. Mark plan checkboxes as `[x]` in the same commit that delivers the item.
1. In loop mode, implement exactly **one pending phase** and exit. In single-shot, update the journal at every milestone until reaching a stop criterion.
1. Every journal entry records: result, commits, non-obvious decisions next iterations must respect (e.g., adapter name, chosen schema, return convention). The journal is the handoff between iterations; what is not there, the next iteration does not know.
1. **Do not mark `[x]` on a partially delivered item.** A checked box is a contract that the spec is satisfied at that point. If implementation only covers part of what the sub-item describes, the sub-item stays `[ ]` and the remaining work is recorded per [Discovered work](#discovered-work).

## Absolute restrictions

Violating any item below is grounds for abort. Record the temptation in the journal and stop.

1. Work **only inside the project directory** (cwd where invocation happened). Nothing outside.
1. **Do not execute** `git push`, `gh pr create`, `git reset --hard`, `git rebase -i`, nor use `--no-verify`/`--force`.
1. **Do not run** external data-collection commands. Use only what is already in local cache.
1. **Do not touch** `.env`, project state directories, or anything containing credentials and session state. Reading is fine where needed; writing never.
1. **Do not change** public contracts unless the spec authorizes it (HTTP routes, schemas, export formats). Existing tests must pass without modification.

A spec may add specific restrictions. It cannot relax this list.

## Discovered work

During implementation of a phase it is common to discover work covered by the spec (sections like "Components", "URL contract", "API contract") that does not fit entirely in the current sub-item, or that emerges as a consequence of a local technical decision. This work **may never be silenced** with prose like "deferred to another phase", "separate work", "out of scope of P<n>" in the `**Decisions**` field or in the `**Summary**`. The journal is not an instrument to transfer scope.

The rule is binary:

1. **Implement now**, within the current iteration, if trivial and within the planned phase.
1. **Record as a new `[ ]` sub-item** in the plan, in the current phase (if it belongs there) or in an existing future phase, **before exiting**. The sub-item enters as a numbered line with `1.` (project convention); the journal entry explicitly cites that new sub-items were added in `**Decisions**` or `**Problems**`. If no future phase fits, create a new phase `P<n+1>: <title>` in the plan with the corresponding items; this is structural and requires justification in the journal.

`**Decisions**` records technical choices within what was implemented (module names, schemas, conventions, shortcuts to undo). It is not the place to describe spec-covered work that was left out; for that, use a new `[ ]` sub-item.

Symptom of violation: phrases like "if P<n> wants X, that's separate work", "X is still not done", "Y goes for later", with no corresponding `[ ]` sub-item. That is recording pending work via prose, and the outer loop treats it as undeclared work.

## Done criteria

The spec defines the technical done criteria per phase. A reasonable default for Go projects:

1. `go vet ./<paths>` zero errors.
1. `gofmt -l <paths>` empty output.
1. `go test ./<paths>` zero failures and zero new unexplained skips.

For other ecosystems, the spec specifies the equivalents.

If a check fails and the agent does not resolve in 3 attempts, revert the last problematic commit with `git revert` (not `reset`), record the impasse in the journal, and stop the phase.

## Stop criteria

Each iteration ends with a `Result` value in the journal. The outer loop acts on it:

| Result | When | Loop action |
|---|---|---|
| `ok` | Phase complete: every sub-item of the current phase is `[x]` AND any discovered work was implemented or became a new `[ ]` sub-item in a future phase | Next iteration |
| `partial` | Phase made real progress but a `[ ]` sub-item from it remains (e.g., 4 of 6 delivered), or new `[ ]` sub-items appeared in the current phase that go to the next iteration | Next iteration |
| `done` | Zero `[ ]` sub-items in the entire plan. The outer loop verifies and aborts if any `[ ]` remains | Stop, success |
| `blocked` | Technical block (3 consecutive validation failures after `git revert`), real human decision (undocumented ambiguity, non-trivial trade-off), temptation to violate an absolute restriction, or explicit "stop for human review" point in the plan with items still pending | Stop, requires human review |

The journal entry **must** record:

1. `**Result**: <value>` on its own line, exact. The loop parses it.
1. `**Summary**`: 1-3 lines of what was done.
1. `**Commits**`: list `<short hash> <message>`.
1. `**Decisions**`: non-obvious points next iterations must respect (omit if none).
1. `**Problems**`: incidents and resolutions (omit if none).
1. `**Next**`: next pending phase, or `none` when `Result: done`.

## Execution Journal

Each spec keeps an "Execution Journal" section at the end of the document. New entries go on **top** of the list (most recent first). It is a contract read by the outer loop (it parses `**Result**:`) and by the next iteration's agent (reads `**Decisions**` to avoid undoing previous choices).

Format:

```markdown
### YYYY-MM-DD HH:MM, <phase or item>

- **Result**: ok | partial | blocked | done
- **Summary**: 1-3 lines of what was done.
- **Commits**: <short hash> <message>; <short hash> <message>
- **Decisions**: <non-obvious point next iteration must respect> (omit if none)
- **Problems**: <incident> → <resolution> (omit if none)
- **Next**: <next pending phase> | none
```

Strict rules:

1. `**Result**:` is on its own line, value without quotes, no extra text on the same line. The loop uses regex; any noise becomes "unknown result" and the loop aborts for safety.
1. `**Decisions**` is the technical handoff between iterations. Record: names of created modules/adapters, chosen schemas when alternatives existed, applied conventions, taken shortcuts that need undoing later. **Not** the place to defer spec-covered work; that goes as a new `[ ]` sub-item per [Discovered work](#discovered-work). Without `Decisions`, the next iteration reinvents.
1. `**Commits**` lists every commit in the iteration, not only the last.
1. When the iteration adds new `[ ]` sub-items to the plan (discovered work), explicitly cite in `**Decisions**` or `**Problems**`: "added sub-item `<description>` to P<n>".

The author reads top to bottom in the morning to understand what happened overnight.

## Localization

`bcc` supports localized vocabularies via `.bcc.toml`. The contract above uses English defaults: `## Implementation Plan`, `## Execution Journal`, `**Result**`, values `ok`/`partial`/`done`/`blocked`. For projects in another language, configure the corresponding strings in `.bcc.toml` (`[specs]` and `[loop.results]` sections). The loop semantics stay the same; only the surface vocabulary changes.

## Command line

```bash
# Default loop mode: one instance per phase, until done/blocked
bcc run docs/specs/2026-04-29-title.md

# Iteration cap (default 20)
bcc run --max-iterations 10 docs/specs/2026-04-29-title.md

# Single-shot: one instance for everything (short specs)
bcc run --single-shot docs/specs/2026-04-29-title.md

# Live status dashboard in another terminal/pane
bcc watch docs/specs/2026-04-29-title.md
```

Behavior:

1. Verifies the spec exists.
1. Loop mode invokes the agent in sequence. Each iteration has a focused prompt for one phase. Between iterations, it parses `**Result**:` from the latest journal entry and decides. Aborts also if `HEAD` did not advance in the iteration.
1. Each iteration logs JSONL events to a per-iteration file (path printed at start).
1. Runs in foreground; use a tmux session to survive logout (`Ctrl+B d` to detach).
1. Exit codes: `0` done, `1` blocked, `2` unknown result, `3` `HEAD` did not advance, `4` iteration cap reached, `5` `done` declared with `[ ]` items still in the plan.

**Account configuration.** `bcc` does not export agent-specific env vars by default. If your agent needs `CLAUDE_CONFIG_DIR`, `OPENAI_API_KEY`, or similar, configure them in the `[env]` section of `.bcc.toml` or in a loaded `.env` file.

## When not to use

This automation is designed for refactors and well-scoped implementations with detailed plans. Do not use for:

1. Exploratory research ("find the best way to do X"). Lacks objective done criteria.
1. Tasks involving open design decisions during execution. The agent correctly aborts in these cases.
1. Changes that depend on external coordination (deploy, communication, third-party approval).
