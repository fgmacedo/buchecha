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

Before the Director plans, it asks: is this spec plannable? A validation pass reads the spec end-to-end and produces a structured **validation report**: are acceptance criteria concrete, where is intent ambiguous, which cross-cutting concerns (observability, security, testability, performance) need spelling out. Inside `bcc run`, validation is the first step of the Director-led flow and runs autonomously: blockers abort the run with the report visible; warnings and suggestions are persisted to the journal and execution proceeds. As a standalone command, `bcc validate <spec>` lets authors check a spec at edit time and gates CI on PRs that touch specs. This is the cheapest Director layer and the one that prevents the largest class of failures: starting the loop on a spec that was never going to converge.

## Problem

### Context

Today bcc trusts the spec. The Executor reads it and starts. If the spec has open questions, missing acceptance criteria, or under-specified cross-cutting concerns, the Executor improvises (poorly), or stalls, or completes phases that the user later realizes did not solve the problem. The cost of a bad spec is paid in iterations, not at edit time.

The Director, when enabled, is the role that turns the spec into a typed plan: a DAG of phases with explicit dependencies and acceptance criteria, against which progress can be measured. That work is only as good as the spec it consumes. Validation is the gate that decides whether the spec is ready to be planned, or whether it needs the author's attention first.

Spec quality is also unevenly enforced. Authors who run bcc daily develop intuition for what "executable spec" means. New authors do not. The threshold of "good enough to run" is implicit and project-specific.

### User pain

1. **Wasted iterations.** A two-hour run produces a tree of half-done work because the spec did not say what "done" meant for phase 3. The user only discovers it after the fact.
1. **Lost confidence.** "Did the agent miss this, or was it never in the spec?" The user re-reads the spec to find out, defeating the point of autonomy.
1. **Author overhead.** Spec authoring is the highest-leverage human work in this stack. Anything that catches issues at author time, before the loop runs, multiplies that leverage.
1. **Onboarding friction.** New users learn the implicit standards by losing iterations on their first long run.

### Impact of not solving

The Director plans on a foundation it cannot trust. The MVP loop runs whatever you give it. The bar for "good spec" stays fuzzy and learned by burning iterations. New users churn at first contact with their first long run.

## Goals

### What we want to achieve

- [ ] G1: Inside `bcc run` with the Director enabled, validation runs as the first step of the autonomous flow. Blockers abort the run before any Executor session starts and surface the report; warnings and suggestions are persisted to the journal and execution proceeds without prompting the user.
- [ ] G2: A standalone `bcc validate <spec>` surfaces concrete, actionable issues against the spec at edit time. Same engine, same report, no execution side-effect.
- [ ] G3: Issues categorized so the author can triage: blockers (must fix), warnings (should fix), suggestions (consider). Each issue is concrete enough to act on without re-reading the whole spec.
- [ ] G4: Exit codes suitable for CI: zero on clean, non-zero on threshold-or-worse issues, threshold configurable.
- [ ] G5: Validation results are cached per spec hash; `--resume` and re-runs against an unmodified spec skip the gate.

### Success metrics

| Metric | Current | Target |
|---|---|---|
| Specs that fail mid-execution due to ambiguity | qualitative; high | reduced by half on Director-enabled runs |
| Time from "draft spec" to "spec the author would run on bcc with confidence" | hours, multiple revisions | one validation pass, often no revisions |
| First-run convergence rate (no human intervention until phase done) | baseline TBD | +20% on Director-validated specs |
| Author-perceived "did the validator save me time?" | n/a | majority yes after three uses |

### Non-goals

- We do **not** edit the user's spec. Spec management is the user's responsibility; bcc is an orchestrator for execution. The validation report describes issues; the author writes the fix.
- We do **not** prompt the user to "press P to proceed" on clean specs. Autonomous execution is the default behavior of `bcc run`; validation is invisible when there are no blockers.
- We do **not** validate against project-specific style (lint rules, naming conventions). That is author skill territory.
- We do **not** read the codebase during validation. The Director judges the spec on its own terms; codebase-aware validation is a separate concern (PRD 2).
- We do **not** ship a static linter. Validation is LLM-based; deterministic linters are complementary, not substitutes.
- We do **not** validate cross-spec relationships in this PRD (initiative + child specs). The unit is the file the user passes.

## Audience

| Segment | Description | Estimated volume |
|---|---|---|
| Spec authors (existing bcc users) | Author runs `bcc validate` on their own spec at edit time; Director-led `bcc run` uses the same gate | Primary |
| New bcc users | First exposure; the report teaches them what a good spec looks like | High value, low volume initially |
| CI users | Run `bcc validate` on PRs that touch specs; gate merge on clean validation | Secondary; ships with the standalone command |

## Requirements

### Functional

- [ ] FR1: `bcc validate <spec>` runs the Director against the spec and prints a validation report. Read-only; no execution side-effect.
- [ ] FR2: The report contains, per issue: `severity` (`blocker | warning | suggestion`), `category` (`acceptance | clarity | completeness | observability | security | testability | architecture | scope | other`), `location` (heading or line range), `description`, and `recommendation` (concrete guidance on how to address the gap, free-text). The recommendation describes the fix the author should make; bcc never produces or applies a patch.
- [ ] FR3: Inside `bcc run` with the Director enabled, validation is the first step of the autonomous flow. Blockers abort the run before any Executor session starts and surface the report on stderr (and in the TUI). Warnings and suggestions are persisted to the journal as Director output; execution proceeds without further prompts.
- [ ] FR4: Output formats for `bcc validate`: text (default, TUI-friendly), json (`--output json`) for tooling and CI.
- [ ] FR5: The report is persisted to `.bcc/validation/<spec-slug>-<timestamp>.json`. `--resume` and re-runs against an unmodified spec (matched by content hash) skip re-validation.
- [ ] FR6: Configurable severity threshold for non-zero exit on `bcc validate` via `--severity <level>`: default `blocker` only.
- [ ] FR7: The Director's validation prompt is owned by `internal/director/`, embedded via `//go:embed` (same pattern as `internal/format/markdown_bcc/contract.md`). Not user-editable.
- [ ] FR8: Per-spec opt-out via a directive in the spec (e.g., `bcc-directive: validate=skip`) for cases where the author has already validated externally and wants `bcc run` to skip the gate.

### Non-functional

- [ ] NFR1: Validation completes in one Director call. No iterative validation loop in this PRD.
- [ ] NFR2: Cost reporting: the TUI/CLI shows tokens consumed by the validation call.
- [ ] NFR3: Vendor agnostic: works on any Director adapter bcc supports.
- [ ] NFR4: Privacy: only the spec content is sent to the Director model. No codebase, no env, no journal. The user opts into the Director the same way they opt into the Executor today.
- [ ] NFR5: Performance: typical spec (under 500 lines) validates in under 60 seconds.

## Expected experience

### Story 1: author validates at edit time

> As a spec author, I want to run `bcc validate docs/specs/my-feature/index.md` before I commit hours to a run, so that I find missing acceptance criteria and unclear scope while I am still editing.

Flow:

1. Author writes a spec.
1. Runs `bcc validate <path>`.
1. CLI prints a categorized list of issues, color-coded by severity. Each issue includes the location, the description, and a recommendation.
1. The author edits the spec by hand to address the issues.
1. Re-validates; the report is now clean.

### Story 2: Director-led run on a clean spec

> As a user starting a Director-led run, I want validation to be invisible when the spec is clean, so that autonomous execution stays autonomous.

Flow:

1. User runs `bcc run --director docs/specs/my-feature/index.md`.
1. Director validates (a few seconds to a minute).
1. No blockers. Warnings and suggestions, if any, are persisted to the journal and visible in the TUI's Director panel.
1. Director plans and the Executor starts. The user sees the live TUI; no extra prompts.

### Story 3: Director-led run on a spec with blockers

> As a user, I want bcc to refuse to plan a spec it cannot plan, so that I do not waste a long run on a foundation the Director already flagged as ambiguous.

Flow:

1. User runs `bcc run --director docs/specs/my-feature/index.md`.
1. Director validates and finds blockers.
1. The run aborts before any Executor session starts. The report is surfaced on stderr and in the TUI; the persisted report is at `.bcc/validation/...`. Exit code is non-zero.
1. The user reads the report, edits the spec, runs again. The next run picks up the cached clean validation (FR5) when the spec is unchanged after the fix.

### Story 4: CI gate

> As a team using bcc, we want PRs that change specs to pass `bcc validate --severity warning` before merge, so we never ship a spec that has known ambiguity.

Flow: `bcc validate` in CI; non-zero exit on threshold-or-worse issues.

## Constraints and dependencies

- Requires the bcc-markdown adapter (or any future format adapter) to expose spec content via the existing `loop.AgentBriefing`/`SpecReader` ports. The Director consumes content, not file paths.
- Requires a Director adapter wired in `cmd/`. The same agent binary used as Executor is acceptable for the first cut.
- Independent of PRD 2 (reviewed execution) and PRD 3 (parallel). Ships alone, but is the natural first step of any Director-led flow.
- Subject to the autonomy and permission contract: validation does not require `[executor].skip_permissions = true`. It is a read-only Director call.
- Spec privacy: shipping the spec to the Director model is implicit in opting into the Director. Documented prominently.

## Alternatives considered

| Alternative | Pros | Cons |
|---|---|---|
| Static linter for bcc-markdown | Cheap, deterministic, vendor-free | Catches structural issues only; misses semantic gaps that are the whole point |
| Skill-side guidance to spec author (no framework support) | Zero new code | Already covered by the floating skill spec; complementary, not a substitute for an automated check |
| Confirmation prompt on every `bcc run` | Eliminates the case where a warning slips by | Breaks the autonomous-execution contract; bcc is a long-session orchestrator, not an interactive wizard. Long sessions exist precisely so the human is not in the per-phase loop. |
| Apply-fixes flow (bcc edits the spec) | Faster turnaround for the author | Crosses the orchestration boundary: spec management is the user's. Also makes the framework liable for spec quality decisions it should not own. |
| Continuous validation (every iteration) | Catches mid-run drift in spec quality | Out of scope here; PRD 2's review covers in-loop drift |
| Validation as a separate binary | Clean separation | Splits the user surface and the prompt ownership; not worth the complexity |

## Open questions

- [ ] How aggressive should completeness checking be? A small spec does not need an observability section; the Director should not invent demands. Calibration by example specs.
- [ ] Severity drift across model versions: how do we keep the bar stable? (Maybe a golden-set of canonical specs evaluated each upgrade.)
- [ ] When the spec is multi-file (initiative + child specs), what is the unit of validation? Proposed: the file the user passes; cross-doc concerns are a manual extension out of scope here.
- [ ] What happens when the Director itself returns a malformed report (the typed payload fails validation)? Proposed: hard fail with a diagnostic; never proceed silently.
- [ ] When the user edits the spec mid-run (after `--resume`), do we re-validate before the next phase, or trust the cached pass until the next full run? Proposed: re-validate on any spec hash change, consistent with the index.md cross-cutting decision on plan persistence.
- [ ] Should warnings escalate to blockers above a threshold count? Proposed: no; severity is per-issue and the Director should not aggregate. If the bar feels wrong, the prompt is wrong.

## References

- [Initiative index](./index.md)
- [Skill: fast-iteration spec authoring](../buchecha-mvp/2026-04-29-skill-spec-authoring.md): author-side complement; the validator is the framework-side complement.
- `internal/loop/agentcontract/`: discipline patterns to mirror in the Director schema.
