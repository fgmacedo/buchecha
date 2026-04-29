---
title: "bcc cooperative validation smoke v2"
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

# bcc cooperative validation smoke v2

## Summary

Second cooperative validation run. The first run surfaced five concrete improvements to bcc (BCC_* env vars, Result: review, prompt order/placeholder pre-check, `<HEAD>` convention, working-tree invariant). This run verifies those improvements with phases that exercise each one explicitly.

The agent (sub-claude invoked by `bcc run`) **knows** it is being observed and is co-validating bcc itself. Continue using the **Notes for observer** pattern from v1.

## Cooperative protocol

Same as v1. Summary:

1. Every journal entry includes `**Notes for observer**` with: prompt experience, env/config (now with BCC_* check), friction, suggestions for bcc.
1. Mark `[x]` only on items fully delivered.
1. If a phase contains only placeholder items, set `Result: review` (NOT `blocked`) per the new contract.

## Implementation Plan

### P1: BCC_* env var visibility

1. [x] Read all `BCC_*` env vars present in your subprocess and write them to `testdata/bcc-validation/bcc-env-iter1.txt`, one per line as `KEY=VALUE`. If any expected var (BCC_RUNNING, BCC_ITERATION, BCC_MAX_ITERATIONS, BCC_SPEC_PATH, BCC_JSONL_PATH, BCC_BRANCH) is missing, list it as `MISSING_<NAME>` so the observer sees the gap.
1. [x] In your `**Notes for observer**`, cite `BCC_JSONL_PATH` (its value) so the observer can correlate the journal entry with the raw event log.

### P2: `<HEAD>` convention in **Commits**

1. [x] In your journal entry for this iteration, the `**Commits**` field MUST use the `<HEAD>` convention for the journal commit (i.e., the line `<HEAD> <commit message>`). Earlier commits, if any, use their actual short hash. This validates that the documented convention reads naturally and that the loop tolerates it.

### P3: Observer-driven placeholder (validates Result: review)

This phase deliberately has only a placeholder item. The agent should set `Result: review` (NOT `blocked`) and exit. The observer will fill before any future iteration.

1. [ ] (placeholder; observer fills before next iteration — DO NOT IMPLEMENT)

## Autonomous execution

Follows the [Autonomous execution guide](../../guides/autonomous-execution.md) defaults plus the cooperative protocol above.

### Done criteria

1. P1 and P2 fully `[x]`.
1. `testdata/bcc-validation/bcc-env-iter1.txt` exists and is non-trivial.
1. `gofmt -l ./...` empty, `go vet ./...` clean, `go test -race ./...` zero failures.

### Stop criteria

1. **Success**: P1 + P2 `[x]` then P3 triggers `Result: review` (exit 6).
1. **Block**: only on real tech failure or absolute-restriction temptation. NOT for placeholder items in P3 — those are review.

## Execution Journal

### 2026-04-29 13:14, P3: observer-driven placeholder

- **Result**: review
- **Summary**: P3 contains a single placeholder item explicitly marked "DO NOT IMPLEMENT" pending the observer. Per the procedure (step 4) and the spec's stop criteria, I did not invent content; left the `[ ]` as-is and exited with `Result: review`.
- **Commits**: <HEAD> spec(smoke-v2): record P3 review checkpoint
- **Decisions**: Did not modify the P3 plan item, did not check the box, did not write any code. The phase's only deliverable is the human edit that turns the placeholder into a real, actionable sub-item (or removes the phase). Per the cooperative protocol the observer fills before the next iteration; loop exit code 6 is the expected outcome.
- **Problems**: Working tree was again NOT clean at iteration start (same set as iter1/iter2: `AGENTS.md`, `docs/specs/buchecha-mvp/2026-04-29-phase-2-tui-dashboard.md`, `docs/specs/buchecha-mvp/index.md`, untracked `docs/specs/buchecha-mvp/2026-04-29-phase-3-steering.md`). Used a path-scoped `git add` of the spec file only.
- **Notes for observer**:
  - `BCC_JSONL_PATH` for this iteration: `/var/folders/6s/bqzmgmsn5kz7l6ny1r0k17_r0000gp/T/bcc/2026-04-29-bcc-cooperative-smoke-v2-iter3.jsonl`.
  - All 6 expected `BCC_*` vars set; `BCC_ITERATION=3`, `BCC_MAX_ITERATIONS=3`, `BCC_BRANCH=feat/phase-1` matched `git branch --show-current`. Note that `BCC_ITERATION` equals `BCC_MAX_ITERATIONS`, but the loop should still stop on `Result: review` (exit 6) before reaching the cap (exit 4); validating that precedence is part of what this iteration exercises.
  - Prompt experience: the placeholder pre-check (procedure step 4) made the decision unambiguous. The phrase "set 'Result: review', record what you need from the observer in **Decisions**, and exit" mapped one-to-one to what I did. Strong improvement vs. v1 where the natural temptation would have been `blocked`.
  - Friction: the dirty-tree-on-entry pattern repeats for the third iteration in a row. Reiterating the suggestion from iter1/iter2: `bcc` should refuse to start (or auto-stash) when the tree carries unrelated changes; otherwise every iteration must hand-curate `git add` paths.
  - Suggestions: when the observer re-triggers after editing P3, it would be useful for `bcc run` to print the previous iteration's `Result` and the reason the loop stopped (e.g., "previous run stopped on `review` at P3"), so the human knows why the new invocation exists. Today the env vars cover the current iteration but not the prior loop's outcome.
- **Next**: P3 (observer must edit the placeholder to a concrete sub-item, then re-trigger; loop exits 6 here)

### 2026-04-29 13:30, P2: `<HEAD>` convention in **Commits**

- **Result**: ok
- **Summary**: P2 validated by writing this journal entry with `<HEAD>` referring to the iteration's only commit (the journal commit itself). Single sub-item marked `[x]`.
- **Commits**: <HEAD> spec(smoke-v2): complete P2 validating <HEAD> convention
- **Decisions**: This phase has no code deliverable; the validation is the journal-entry format itself. Kept the iteration to a single commit so the `<HEAD>` reference is unambiguous.
- **Problems**: Working tree was again NOT clean at iteration start, same set of unrelated files as iter1 (`AGENTS.md`, `docs/specs/buchecha-mvp/2026-04-29-phase-2-tui-dashboard.md`, `docs/specs/buchecha-mvp/index.md`, untracked `docs/specs/buchecha-mvp/2026-04-29-phase-3-steering.md`). Used a path-scoped `git add` so only the spec file enters the iteration commit.
- **Notes for observer**:
  - `BCC_JSONL_PATH` for this iteration: `/var/folders/6s/bqzmgmsn5kz7l6ny1r0k17_r0000gp/T/bcc/2026-04-29-bcc-cooperative-smoke-v2-iter2.jsonl`.
  - All 6 expected `BCC_*` vars set and non-empty; `BCC_ITERATION=2`, `BCC_MAX_ITERATIONS=3`, `BCC_BRANCH=feat/phase-1` matched `git branch --show-current`.
  - Prompt experience: the `<HEAD>` convention reads naturally for a single-commit iteration. The wording "this commit `<message>` or `<HEAD> <message>`" in the guide makes the alternatives explicit; chose `<HEAD>` to satisfy the phase contract.
  - Friction: the dirty-tree-on-entry pattern repeats from iter1. `bcc` still does not enforce a clean entry tree. Reiterating the iter1 suggestion: refuse to start, or stash unrelated work before invoking the agent.
  - Suggestions: when only the journal is the deliverable for a phase (no code), it might be worth an explicit note in the guide that the iteration commit IS the journal commit, so `<HEAD>` is the only valid reference.
- **Next**: P3 (observer-driven placeholder; expected `Result: review`)

### 2026-04-29 13:10, P1: BCC_* env var visibility

- **Result**: ok
- **Summary**: Captured all 6 expected `BCC_*` env vars from the subprocess and wrote them to `testdata/bcc-validation/bcc-env-iter1.txt`. None missing, so no `MISSING_<NAME>` lines were needed. Both P1 items are `[x]`.
- **Commits**: <HEAD> spec(smoke-v2): complete P1 with BCC_* env capture
- **Decisions**: Used the `<HEAD>` convention for the journal commit even though P2 is the phase that explicitly validates it; reads naturally and matches the documented contract.
- **Problems**: Working tree was NOT clean at iteration start. Pre-existing modifications (`AGENTS.md`, `docs/specs/buchecha-mvp/2026-04-29-phase-2-tui-dashboard.md`, `docs/specs/buchecha-mvp/index.md`) and one untracked file (`docs/specs/buchecha-mvp/2026-04-29-phase-3-steering.md`) were present and unrelated to this spec. I left them untouched and used specific paths in `git add` so the iteration commit only carries P1 work. → Observer should be aware that the "clean tree on entry" invariant in the autonomous-execution guide was not held by `bcc` here.
- **Notes for observer**:
  - `BCC_JSONL_PATH` for this iteration: `/var/folders/6s/bqzmgmsn5kz7l6ny1r0k17_r0000gp/T/bcc/2026-04-29-bcc-cooperative-smoke-v2-iter1.jsonl`.
  - All 6 expected vars (`BCC_RUNNING`, `BCC_ITERATION`, `BCC_MAX_ITERATIONS`, `BCC_SPEC_PATH`, `BCC_JSONL_PATH`, `BCC_BRANCH`) were set and non-empty. `BCC_BRANCH=feat/phase-1` matched `git branch --show-current`.
  - Prompt experience: the procedure's step 4 (placeholder pre-check) was clear; for P1 there are no placeholders so it was a no-op. The breadcrumb instruction to cite `BCC_JSONL_PATH` was easy to satisfy.
  - Friction: the dirty working tree on entry was the only surprise. It would help if `bcc` either refused to start with a dirty tree or staged-and-stashed the unrelated work before invoking the agent.
  - Suggestions: consider printing the resolved `BCC_*` values once at startup (names + values, since these are non-secret bcc-controlled vars) so the observer can confirm they match what each iteration records.
- **Next**: P2 (`<HEAD>` convention in **Commits**)

