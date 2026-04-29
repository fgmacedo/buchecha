---
title: "Skill: fast-iteration spec authoring"
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
  - mvp
  - skill
  - authoring
---

# Skill: fast-iteration spec authoring

## Summary

A skill (Claude Code `/skill`) that helps the spec author write phases the agent can execute fast. The framework (`bcc`) is format-agnostic and does not know how to optimize prompts; the optimization belongs on the **author** side. This skill captures three techniques that materially shorten per-iteration ramp-up time without compromising the autonomous-loop reset guarantee.

## Status

**Draft.** Quick capture so I do not lose the idea. Iterate later when I sit down to actually build the skill.

## Why this is not a framework feature

Encoding spec-authoring conventions into `bcc` would re-introduce the format coupling that [Spec-format vendor neutrality](./2026-04-29-spec-vendor-neutrality.md) is unwinding. A well-written spec produces fast iterations regardless of which orchestrator runs it. So the techniques live next to the author, not next to the runtime.

## Techniques to capture

### 1. Active-phase scoping

The author writes each phase to be readable on its own. The `Goal` and the implementation items mention only files and concepts the agent needs for that phase. References to other phases use `(see X)` style; the agent does not need to read X to start work.

Anti-pattern: a phase whose first paragraph reads "this builds on P3.4 and P5.7 and assumes you have understood the context from P2.10". The agent will go read all four phases before doing anything.

### 2. Relevant files block

Each phase ends with a short `Relevant files:` list naming the files the author already knows the agent will touch or read. The agent treats it as the search-bound and skips the broad Glob/Grep that would otherwise eat 5 minutes per iteration.

Example:

```markdown
### Sub-items
1. [ ] ...
1. [ ] ...

Relevant files:
- internal/executor/claude/claude.go (parseAssistant)
- internal/loop/events.go (AgentEvent.Usage)
- internal/tui/health.go (healthPanel)
```

The agent's instructions in the skill say: "Read every file in `Relevant files` before doing anything else; do not Glob or Grep beyond these unless the work itself proves you need to."

### 3. Context summary in the journal entry

When closing a phase, the agent writes a `**Context summary**` block in the journal entry: 5 lines max, the decisions it validated, the invariants it discovered, the tools it used. The next iteration reads this block plus the **next** phase, instead of redeciding from scratch.

Example block:

```markdown
- **Context summary**:
  - This package has no init(); wiring is from cmd/bcc/main.go.
  - parseAssistant in claude.go is the right hook for per-message data.
  - go test -race must pass before commit; the goroutine in loop.go is the usual offender.
```

This compresses cross-iteration carry-over **into the journal**, which is the only authoritative handoff channel under the autonomous-loop contract. Reset is preserved.

## Skill shape (rough)

When invoked (`/spec-authoring`):

1. Asks which spec the user is editing.
1. Asks which phase to inspect or draft.
1. Reviews the phase against the three techniques and proposes specific edits (add `Relevant files`, tighten cross-phase prose, add a Context summary template to the journal contract).
1. Optionally drafts a new phase from a one-line goal, applying the techniques by default.

## Open questions

- Is there a fourth technique worth including? E.g., a `Done check:` block per phase that names the exact `go test` / `lint` / build commands the agent should run before claiming the phase done. Possibly redundant with the autonomous-execution guide's "Done criteria", possibly worth restating per phase.
- Should the skill enforce length budgets (e.g., "no phase body over N lines")? Probably not; rigid limits hurt more than help.

## Out of scope

- Anything about the orchestrator (`bcc`) itself.
- Format conversion between bcc-markdown, open-spec, spec-kit, bmad. The skill targets `bcc-markdown` first; portability comes from the techniques themselves, not from cross-format tooling.
