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

1. [ ] Read all `BCC_*` env vars present in your subprocess and write them to `testdata/bcc-validation/bcc-env-iter1.txt`, one per line as `KEY=VALUE`. If any expected var (BCC_RUNNING, BCC_ITERATION, BCC_MAX_ITERATIONS, BCC_SPEC_PATH, BCC_JSONL_PATH, BCC_BRANCH) is missing, list it as `MISSING_<NAME>` so the observer sees the gap.
1. [ ] In your `**Notes for observer**`, cite `BCC_JSONL_PATH` (its value) so the observer can correlate the journal entry with the raw event log.

### P2: `<HEAD>` convention in **Commits**

1. [ ] In your journal entry for this iteration, the `**Commits**` field MUST use the `<HEAD>` convention for the journal commit (i.e., the line `<HEAD> <commit message>`). Earlier commits, if any, use their actual short hash. This validates that the documented convention reads naturally and that the loop tolerates it.

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

(empty until first iteration)
