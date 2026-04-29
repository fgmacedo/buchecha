---
title: "bcc cooperative validation smoke"
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
  - validation
  - smoke
  - test
---

# bcc cooperative validation smoke

## Summary

Cooperative end-to-end test of the bcc autonomous loop. The agent (sub-claude invoked by `bcc run`) **knows** it is being observed and is expected to report back with structured feedback in every journal entry. The observer (Claude in the parent Claude Code session that triggered `bcc run`) reads the JSONL stream and the journal, may edit this spec between iterations to refine or expand the protocol based on what was learned, and is the human's eyes on what is happening.

This is not a typical autonomous run. The goals are NOT just to deliver the implementation — they are to validate that bcc works end to end and to surface friction in the tool itself.

## Cooperative protocol

### What the agent (you) should do

1. Treat every iteration as a probe of the bcc tooling, not just a feature delivery. You are co-validating the product.
1. In every journal entry, include a `**Notes for observer**` section with at least:
   - **Prompt experience**: did the prompt feel clear? Anything missing or contradictory?
   - **Env / config**: what env vars are present? Did `CLAUDE_CONFIG_DIR` point where you expected? Anything off?
   - **Friction**: at least one concrete thing that felt awkward in this iteration, even if minor. If you found nothing, say so explicitly.
   - **Suggestions for bcc**: zero or more concrete improvements you'd make if you owned the bcc codebase.
1. If the spec is unclear, do NOT guess. Set `Result: blocked`, put the question in `**Decisions**`, and exit. The observer will edit and re-trigger.
1. Mark `[x]` only on items fully delivered. Discoveries become new sub-items as usual.
1. You are running in `~/projects/buchecha`, branch `feat/phase-1`. You may commit on this branch; the observer is in read-only mode while bcc is running.

### What the observer does between iterations

1. Reads the JSONL stream (`/var/folders/.../bcc/<slug>-iter<N>.jsonl` or wherever `os.TempDir()` resolves), the latest journal entry, and the `git log` of new commits.
1. May edit this spec to:
   - Fill `P2` (placeholder; observer-driven).
   - Add an `### Observer guidance` block at the top of the next phase to steer the agent.
   - Add new phases.
1. Reports back to the human user.

### What is out of scope here

- Production-grade implementation. Tasks are deliberately small and low-risk.
- Touching files outside `~/projects/buchecha` or anything credential-bearing.
- The absolute restrictions in `docs/guides/autonomous-execution.md` apply unchanged.

## Implementation Plan

### P1: Smoke round-trip

1. [x] Read this repository's `README.md` and write a 3-sentence summary of what `buchecha` is, in plain English, to `testdata/bcc-validation/summary.md`. The summary should be understandable to someone who has never seen the project.
1. [ ] Add a `**Notes for observer**` section to your journal entry as described in the cooperative protocol above. Be specific; vague feedback is worse than no feedback.

### P2: Observer-driven iteration

The observer will fill this phase after reading the P1 results. Treat as `[ ]` placeholder until the observer adds concrete sub-items.

1. [ ] (placeholder; observer fills before next iteration)

## Autonomous execution

Follows the [Autonomous execution guide](../../guides/autonomous-execution.md) defaults except for the cooperative protocol described above (which adds journaling requirements; it does not relax any rule).

### Done criteria

1. P1 fully `[x]` in plan.
1. `testdata/bcc-validation/summary.md` exists with the 3-sentence summary.
1. Journal has at least one `**Notes for observer**` block per iteration.
1. `gofmt -l ./...` empty, `go vet ./...` clean, `go test -race ./...` zero failures (do not break existing code).

### Stop criteria

1. **Success**: when the observer marks `Result: done` in the plan after P2 is filled and delivered (this is unusual; the observer will typically extend or stop the test).
1. **Block**: spec unclear, env broken, claude API errors, or absolute restriction temptation.
1. **Human decision**: observer sees something that needs offline discussion.

## Execution Journal

(empty until first iteration)
