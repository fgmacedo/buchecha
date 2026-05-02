---
title: "Parallel phase execution via worktrees"
type: prd
status: draft
authors:
  - Fernando Macedo
reviewers: []
created: 2026-04-30
decision-date:
superseded-by:
supersedes:
review-by:
tags:
  - director
  - parallel
  - worktrees
comments: true
---

# Parallel phase execution via worktrees

## Summary

Once the Director produces a typed plan with explicit dependencies, the structure to run independent phases in parallel is sitting there. This PRD adds a parallel execution mode: bcc spins up N git worktrees, dispatches one Executor session per independent phase, and runs them concurrently. After parallel phases complete, the Director coordinates a reconciliation pass that merges the worktrees back into the main tree, surfacing conflicts as Director verdicts. For specs whose plan has independent leaves, wall-clock time drops sub-linearly. For specs without independence, the mode is a no-op.

## Problem

### Context

Long specs often have phases that touch different parts of the codebase. Phase 4 (TUI changes) and phase 5 (config loader changes) frequently have no shared files. Today bcc runs them serially because the loop has no model of independence. With the Director's typed plan (PRD 2), independence is explicit.

Multi-worktree workflows are well-supported by git. The pattern is: `git worktree add ../bcc-phase-4 feat/phase-4`; do work; merge back. Doing this by hand is annoying. Automating it inside bcc is a small step from the planning we already do.

### User pain

1. **Wall-clock waste.** A spec with 6 independent phases takes 6x the time of one phase. Three of those phases could have run during the first three.
1. **Engagement decay.** A 4-hour serial run is harder to hold attention on than a 90-minute parallel run with the same total token spend.
1. **Cost without speedup.** Token cost is largely the same either way; elapsed time is the user-visible metric, and today bcc gives up the parallel win.

### Impact of not solving

bcc remains attractive for short specs and tolerable for long specs. The wall-clock advantage that would justify "use bcc, not interactive Claude" on big work stays unrealized.

## Goals

### What we want to achieve

- [ ] G1: bcc identifies parallelizable phases from the plan's dependency graph.
- [ ] G2: bcc creates ephemeral git worktrees, one per parallel phase, on a fresh branch derived from the current HEAD.
- [ ] G3: bcc dispatches one Executor session per worktree concurrently, with a single bcc loop coordinating.
- [ ] G4: After all parallel phases complete (or fail), the Director runs a reconciliation pass: review each phase, then merge in dependency order.
- [ ] G5: On merge conflict, the Director produces a structured verdict (auto-resolve trivial, retry-serial, escalate semantic).
- [ ] G6: The TUI shows multiple concurrent Executor streams and the Director-driven reconciliation as a final phase.

### Success metrics

| Metric | Current | Target |
|---|---|---|
| Wall-clock for parallelizable specs (3+ independent phases) | linear | sub-linear; bound by longest critical path + reconciliation cost |
| Merge conflict rate on auto-merge | n/a | low (target < 20% of parallel phases) on real specs; higher rates trigger UX rethink |
| User incidents from leaked worktrees | n/a | zero (worktrees cleaned on every exit, including crash) |
| User-perceived speedup on a parallelizable spec | n/a | > 2x on a 3-independent-phase spec |

### Non-goals

- Cross-worktree sharing of in-flight state. Each Executor session is independent until merge.
- Phases sharing tests they need to keep green. The Director may serialize them in the plan.
- Distributed execution across machines. All worktrees on the same host.
- Conflict-free merge guarantees. We surface conflicts; we do not magic them away.
- Running multiple Director sessions. One Director per run; it coordinates the fan-out and the merge.
- Speculative execution. We do not start a phase whose dependencies are unfinished.

## Audience

| Segment | Description | Estimated volume |
|---|---|---|
| Large-spec users | Specs with many independent phases | Primary beneficiaries |
| Power users on fast machines | Want the wall-clock win | Secondary |
| Users on slow / single-core / shared hardware | Will keep this off | Default-off respects them |

## Requirements

### Functional

- [ ] FR1: The Director's plan declares per-phase a `parallelizable` flag and an explicit `depends_on` list. Phases without unsatisfied `depends_on` and with `parallelizable: true` form the next parallel batch.
- [ ] FR2: bcc reads the plan, identifies the next parallel batch, and creates one worktree per phase under `.bcc/worktrees/<phase-id>/`.
- [ ] FR3: Each worktree is on a branch named `bcc/<run-id>/<phase-id>`, branched from the current HEAD.
- [ ] FR4: bcc dispatches one Executor session per worktree concurrently, capped by `[director].max_parallel` (default: 3).
- [ ] FR5: Each Executor receives the same briefing format as PRD 2, with the `cwd` set to its worktree.
- [ ] FR6: bcc collects all phase outcomes; the Director runs a per-phase review (PRD 2 semantics) before merge.
- [ ] FR7: On all approved, the Director enters reconciliation: merge worktree branches into the main branch in dependency order. On conflict, the Director runs a per-conflict verdict.
- [ ] FR8: After reconciliation, worktrees and their branches are removed.
- [ ] FR9: On any Executor or Director failure, bcc cleans up worktrees (no leaks). On crash, `bcc run --resume` detects orphan worktrees and offers to clean or recover.
- [ ] FR10: TUI: side-by-side Executor panes during parallel execution; reconciliation pane during merge.
- [ ] FR11: Cancellation: `Ctrl-C` cancels all parallel Executor sessions cleanly via context propagation; bcc does not leave stale processes.
- [ ] FR12: Reconciliation conflict policy: trivial (whitespace, comment-only) auto-resolve; semantic escalates. The boundary is defined in the spec phase.

### Non-functional

- [ ] NFR1: Resource bounds enforced. `max_parallel` is hard-capped by config; bcc refuses to exceed.
- [ ] NFR2: All worktree paths are inside `.bcc/worktrees/`. We never write a worktree outside the project's `.bcc/` area.
- [ ] NFR3: Performance: parallel mode adds at most 5% overhead on a single-phase run (it must detect the no-op case).
- [ ] NFR4: Safety: worktrees never push, never persist past run completion, never receive credentials. Same `[executor].skip_permissions` semantics; no escalation of permissions.
- [ ] NFR5: Observability: the journal records the merge order, conflicts encountered, and resolutions. `--resume` can reconstruct the merge state.
- [ ] NFR6: OS support: tested on Linux and macOS first. Windows on best-effort basis (modern git supports worktrees but path-handling needs care).

## Expected experience

### Story 1: spec with three independent phases

> As a user with a spec where phases 2, 3, 4 are independent, I want bcc to run them in parallel and merge the results, so I get the result in roughly one phase's time instead of three.

Flow:

1. Validation, planning (PRDs 1 and 2).
1. Director identifies phases 2, 3, 4 as parallel (no shared files, no dependency).
1. bcc creates three worktrees, dispatches three Executor sessions.
1. TUI shows three live panes; the user watches all three progress.
1. All complete; Director reviews each (PRD 2).
1. Reconciliation: merge phase 2 (no conflicts), merge phase 3 (no conflicts), merge phase 4 (one conflict in shared file). Director auto-resolves a comment-line conflict, escalates a logic conflict.
1. User resolves the logic conflict; bcc continues to phase 5 (serial).

### Story 2: parallel batch with one failure

> As a user, I want a single failed parallel phase to not poison the others. The successful work should be preserved; the failed phase should be the only one that retries.

Flow: phases A and B run in parallel. B fails review. A is approved and merged. B retries (serial or parallel per plan). The user does not lose A.

### Story 3: emergency cleanup

> As a user whose machine crashed mid-parallel-run, I want bcc to detect the orphan worktrees on next run and offer cleanup or resume, not silently leave a mess.

Flow: `bcc run --resume` detects `.bcc/worktrees/` is non-empty and inconsistent with the current plan. TUI: "Detected 3 orphan worktrees from a prior run. [C]lean and re-plan, [R]esume from last verdict, [I]nspect."

### Story 4: cost-aware fallback

> As a user on a flaky network, I want bcc to fall back to serial when a parallel session fails to start (rate limit, transient API issue), rather than leave the run wedged.

Flow: phase A starts, phase B fails with a rate-limit error. bcc retries B once; on second failure, the Director reduces parallelism and continues B serially after A. User sees the fallback in the TUI.

## Constraints and dependencies

- **Hard dependency on PRD 2.** Without a typed plan with `depends_on`, parallel mode has nothing to parallelize from.
- **Git constraint.** Project must be a git repo. Submodule edge cases are out of scope; bcc errors cleanly.
- **OS constraint.** Worktrees use `git worktree add`. Posix and modern Windows git both support this.
- **Disk space.** Each worktree is a checkout. Documented prominently; not silently ignored.
- **Subprocess constraint.** Concurrent Executor sessions multiply file descriptors and CPU. `max_parallel` default 3 is conservative.

## Alternatives considered

| Alternative | Pros | Cons |
|---|---|---|
| Multi-process with file copies (no git worktree) | Works without a git repo | Doubles disk for no benefit; loses commit-level reconciliation |
| In-process concurrent edits to one tree | No worktree overhead | Race conditions on file edits; defeats the independence model |
| Distributed across machines | Massive parallelism for very large specs | Out of scope; sets up an infra problem we are not solving |
| Always-parallel, no opt-in | Wall-clock by default | Surprises users on small machines and small specs; default-off is safer |
| Director auto-resolves all conflicts | Highest automation | Erodes user trust; semantic merges need human judgment |

## Open questions

- [ ] Conflict resolution authority: what is the exact line between trivial auto-resolve and escalation? Proposed: structural-only diffs (whitespace, formatting) auto-resolve; anything touching logic escalates. Calibrated by example.
- [ ] When two parallel phases legitimately need to depend on each other after starting (the plan was wrong), how does the Director recover? Proposed: cancel both, re-plan, retry serial.
- [ ] Should reconciliation produce one merge commit per phase, or a single squashed commit? Proposed: one per phase, preserves traceability.
- [ ] How do we test this at scale without burning real cloud agents? Proposed: a fake Executor that simulates work and shapes per phase, plus a small set of real-world golden specs in CI.
- [ ] Cleanup vs. recovery on crash: cleanup is safe but loses work; recovery is lossless but state-heavy. Default? Proposed: prompt; do not pick by fiat.
- [ ] How does the TUI handle three live executor streams without overwhelming the user? Compact-by-default with a key to expand any pane? Best left to the spec phase.
- [ ] Parallel sessions on the same Director adapter risk vendor rate limits. Should bcc provide global rate-limiting? Proposed: surface vendor rate limits as a warning; do not implement bcc-side throttling in this PRD.

## References

- [Initiative index](./index.md)
- [PRD 2: Reviewed execution](./2026-04-30-reviewed-execution.md)
- `git worktree` manpage and how `condo-fiscal/scripts/exec-spec.sh` already shells out to `git` for HEAD checks (the same `internal/git/cli/` adapter is the natural home for worktree operations).
