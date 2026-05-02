---
title: "Spec validation gate"
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
  - validation
  - pre-flight
comments: true
---

# Spec validation gate

## Summary

Before bcc runs a single iteration, a Director pass reads the spec end-to-end and produces a structured **validation report**: is this spec executable, what would a careful engineer flag as missing or ambiguous, and where do cross-cutting concerns (observability, security, testability, performance) need spelling out. The user sees the report, can apply suggested patches, and proceeds with confidence (or fixes the spec first). This is the cheapest Director layer to ship and the one that prevents the largest class of failures: starting the loop on a spec that was never going to converge.

## Problem

### Context

Today bcc trusts the spec. The Executor reads it and starts. If the spec has open questions, missing acceptance criteria, or under-specified cross-cutting concerns, the Executor improvises (poorly), or stalls, or completes phases that the user later realizes did not solve the problem. The cost of a bad spec is paid in iterations, not at edit time.

Spec quality is also unevenly enforced. Authors who run bcc daily develop intuition for what "executable spec" means. New authors do not. The threshold of "good enough to run" is implicit and project-specific.

### User pain

1. **Wasted iterations.** A two-hour run produces a tree of half-done work because the spec did not say what "done" meant for phase 3. The user only discovers it after the fact.
1. **Lost confidence.** "Did the agent miss this, or was it never in the spec?" The user re-reads the spec to find out, defeating the point of autonomy.
1. **Author overhead.** Spec authoring is the highest-leverage human work in this stack. Anything that catches issues at author time, before the loop runs, multiplies that leverage.
1. **Onboarding friction.** New users learn the implicit standards by losing iterations on their first long run.

### Impact of not solving

bcc remains a tool that runs whatever you give it. The bar for "good spec" stays fuzzy and learned by burning iterations. New users churn at first contact with their first long run.

## Goals

### What we want to achieve

- [ ] G1: A pre-flight `bcc validate <spec>` (and `bcc run --validate-only`) that surfaces concrete, actionable issues against the spec before the loop runs.
- [ ] G2: Issues categorized so the user can triage: blockers (must fix), warnings (should fix), suggestions (consider).
- [ ] G3: For each issue, an optional **suggested patch** the user can preview and apply with one keystroke.
- [ ] G4: Exit codes suitable for CI: zero on clean, non-zero on blockers, configurable threshold for warnings.
- [ ] G5: When the user runs `bcc run` with the Director enabled, validation is the first step, with a single confirmation before execution proceeds.

### Success metrics

| Metric | Current | Target |
|---|---|---|
| Specs that fail mid-execution due to ambiguity | qualitative; high | reduced by half on opt-in users |
| Time from "draft spec" to "spec the author would run on bcc with confidence" | hours, multiple revisions | one validation pass, often no revisions |
| First-run convergence rate (no human intervention until phase done) | baseline TBD | +20% on validated specs |
| Author-perceived "did the validator save me time?" | n/a | majority yes after three uses |

### Non-goals

- We do **not** auto-fix the spec without user approval. Suggested patches are previewed; the user accepts or rejects.
- We do **not** validate against project-specific style (lint rules, naming conventions). That is author skill territory.
- We do **not** read the codebase during validation. The Director judges the spec on its own terms; codebase-aware validation is a separate concern (PRD 2).
- We do **not** ship a static linter. Validation is LLM-based; deterministic linters are complementary, not substitutes.
- We do **not** validate cross-spec relationships in this PRD (initiative + child specs). The unit is the file the user passes.

## Audience

| Segment | Description | Estimated volume |
|---|---|---|
| Spec authors (existing bcc users) | Author runs bcc on their own spec; wants pre-flight assurance | Primary |
| New bcc users | First exposure; the report teaches them what a good spec looks like | High value, low volume initially |
| CI users | Run validation on PRs that touch specs; gate merge on clean validation | Speculative; ship if signal warrants |

## Requirements

### Functional

- [ ] FR1: `bcc validate <spec>` runs the Director against the spec and prints a validation report.
- [ ] FR2: The report contains, per issue: `severity` (`blocker | warning | suggestion`), `category` (`acceptance | clarity | completeness | observability | security | testability | architecture | scope | other`), `location` (heading or line range), `description`, and optional `suggested_patch`.
- [ ] FR3: `--apply <issue-id>` applies a suggested patch after preview confirmation. Patch generation may be a second Director call if not bundled in FR1's output.
- [ ] FR4: `--apply-all` applies all suggestions whose severity matches a configured threshold; defaults to interactive confirmation per patch.
- [ ] FR5: `bcc run --director` runs validation as the first step. With `--auto-proceed`, it skips the confirmation if no blockers are present; otherwise it pauses for the user.
- [ ] FR6: Output formats: text (default, TUI-friendly), json (`--output json`) for tooling and CI.
- [ ] FR7: The report is persisted to `.bcc/validation/<spec-slug>-<timestamp>.json`. `--resume` skips re-validation when the spec hash is unchanged.
- [ ] FR8: Configurable severity threshold for non-zero exit: default `blocker` only.
- [ ] FR9: The Director's validation prompt is owned by `internal/director/`, embedded via `//go:embed` (same pattern as `internal/format/markdown_bcc/contract.md`). Not user-editable.
- [ ] FR10: Per-spec opt-out via a directive in the spec (e.g., `bcc-directive: validate=skip`) for cases where the author has already validated and wants `bcc run` to skip the gate.

### Non-functional

- [ ] NFR1: Validation completes in one Director call. No iterative validation loop in this PRD.
- [ ] NFR2: Cost reporting: the TUI/CLI shows tokens consumed by the validation call.
- [ ] NFR3: Vendor agnostic: works on any Director adapter bcc supports.
- [ ] NFR4: Privacy: only the spec content is sent to the Director model. No codebase, no env, no journal. The user opts into the Director the same way they opt into the Executor today.
- [ ] NFR5: Performance: typical spec (under 500 lines) validates in under 60 seconds.

## Expected experience

### Story 1: author validates before running

> As a spec author, I want to run `bcc validate docs/specs/my-feature/index.md` before I commit hours to a run, so that I find missing acceptance criteria and unclear scope at edit time.

Flow:

1. Author writes a spec.
1. Runs `bcc validate <path>`.
1. TUI shows a categorized list of issues, color-coded by severity.
1. For each issue, the author can press a key to see the suggested patch (a unified diff against the spec).
1. The author accepts patches one by one or in bulk.
1. The spec is updated in place; the author commits.
1. Re-validates; the report is now clean.

### Story 2: pre-run confirmation

> As a user about to start a long run, I want bcc to validate the spec first and show me a single screen "ready to run, X warnings, Y suggestions" so I can decide whether to fix or proceed.

Flow:

1. User runs `bcc run --director docs/specs/my-feature/index.md`.
1. Validation runs (a few seconds to a minute).
1. TUI shows: "Spec ready to run. 0 blockers, 2 warnings, 5 suggestions. [P]roceed, [R]eview issues, [A]bort."
1. User picks; the loop starts or the validation report opens for triage.

### Story 3: CI gate (speculative)

> As a team using bcc, we want PRs that change specs to pass `bcc validate --severity warning` before merge, so we never ship a spec that has known ambiguity.

Flow: `bcc validate` in CI; non-zero exit on threshold-or-worse issues.

## Constraints and dependencies

- Requires the bcc-markdown adapter (or any future format adapter) to expose spec content via the existing `loop.AgentBriefing`/`SpecReader` ports. The Director consumes content, not file paths.
- Requires a Director adapter wired in `cmd/`. The same agent binary used as Executor is acceptable for the first cut.
- Independent of PRD 2 (reviewed execution) and PRD 3 (parallel). Ships alone.
- Subject to the autonomy and permission contract: validation does not require `[executor].skip_permissions = true`. It is a read-only Director call.
- Spec privacy: shipping the spec to the Director model is implicit in opting into the Director. Documented prominently.

## Alternatives considered

| Alternative | Pros | Cons |
|---|---|---|
| Static linter for bcc-markdown | Cheap, deterministic, vendor-free | Catches structural issues only; misses semantic gaps that are the whole point |
| Skill-side guidance to spec author (no framework support) | Zero new code | Already covered by the floating skill spec; complementary, not a substitute for an automated check |
| Mandatory validation, not opt-in | Eliminates the case where the user forgets | Wrong default; some users want to run a known-clean spec without the cost |
| Continuous validation (every iteration) | Catches mid-run drift in spec quality | Out of scope here; PRD 2's review covers in-loop drift |
| Validation as a separate binary | Clean separation | Splits the user surface and the prompt ownership; not worth the complexity |

## Open questions

- [ ] Should patch generation be in the initial report (cheaper, lower quality) or a follow-up call per-issue (higher quality, higher cost)? Lean toward follow-up for accepted patches only.
- [ ] How aggressive should completeness checking be? A small spec does not need an observability section; the Director should not invent demands. Calibration by example specs.
- [ ] Severity drift across model versions: how do we keep the bar stable? (Maybe a golden-set of canonical specs evaluated each upgrade.)
- [ ] When the spec is multi-file (initiative + child specs), what is the unit of validation? Proposed: the file the user passes; cross-doc concerns are a manual extension out of scope here.
- [ ] What happens when the Director itself returns a malformed report (the typed payload fails validation)? Proposed: hard fail with a diagnostic; never proceed silently.

## References

- [Initiative index](./index.md)
- [Skill: fast-iteration spec authoring](../buchecha-mvp/2026-04-29-skill-spec-authoring.md): author-side complement; the validator is the framework-side complement.
- `internal/loop/agentcontract/`: discipline patterns to mirror in the Director schema.
