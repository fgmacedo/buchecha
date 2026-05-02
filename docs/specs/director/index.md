---
title: "Director: orchestrated planning and review"
type: initiative
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
  - product-vision
  - post-mvp
comments: true
---

# Director: orchestrated planning and review

## Summary

bcc today runs one agent on a loop: read spec, do next phase, write journal, exit. Long, ambitious specs expose the limit of that model. A single agent session that has to plan, execute, and self-review accumulates context until it loses focus, drifts, declares "done" prematurely, or quietly skips requirements. The human picks up the supervision tax: tracking what is done vs. left, noticing drift, redirecting, ensuring quality. This initiative proposes a second AI role inside bcc, called the **Director**, that owns the supervision work the human is doing today. The Director plans, briefs, and reviews; the Executor does. bcc remains the host: it owns the loop, persistence, UI, and the protocol between roles.

The user-facing concept the author had in mind is the *orchestrator pattern*: a planning and supervising AI on top of executing AIs. We name the role **Director** in code and docs because bcc itself is already an orchestrator at the process layer; using a distinct word for the cognitive layer keeps both readable.

## Context and motivation

### The supervision tax

Running a multi-hour spec on Claude Code (or Codex, or Gemini) reveals a pattern. The first iterations are crisp: the agent reads the spec, picks the next phase, makes a focused commit. After hour two or three, behavior degrades. Scope expands to "while I'm here, let me also". Acceptance criteria get reinterpreted. The agent declares a phase complete when half the criteria slipped. The journal entry says `ok` but the diff says otherwise. None of this is malice; it is the natural decay of one context window asked to hold the full state of an ambitious project.

The human compensates. They re-read the spec to remember what was promised. They diff each commit. They paste corrective feedback. They cancel and restart with a tighter scope. The pattern is identifiable: the human becomes the planner and reviewer the agent cannot reliably be for itself.

### Why a single agent cannot be its own supervisor

Bigger context windows are not the answer. The problem is one attention budget asked to hold both the ground-level "what am I editing right now" and the bird's-eye "are we still on track for the goal". Anchoring effects, recency bias, and the agent's reasonable preference for closing the loop in front of it push toward "ship it" rather than "did this actually meet the criteria". A separate session, with a different prompt and a different scope of context, can ask the question the executor cannot ask itself.

This is not a Claude limitation. It is structural to autoregressive agents working a long task.

### Why bcc is the right place to solve it

bcc already separates the loop from the agent. The MVP introduces the wire protocol, the journal contract, and the TUI. Adding a Director is an additive change to the existing architecture: a new port, a new adapter, a new prompt template. No vendor we depend on needs to change. The Director can run on the same model as the Executor, on a cheaper one, on a more capable one, or on a different vendor entirely. Vendor neutrality is preserved by construction.

### Why now (and what stays out)

The Director loop is post-MVP. The MVP delivers parity, observability, and the wire protocol. The Director assumes those primitives work. We do not block MVP on this initiative; we set the direction so that MVP decisions do not foreclose it.

This initiative is **not** a multi-agent framework. It is one extra role with a narrow brief: plan, brief, review. We resist the urge to add planner sub-agents, critic sub-agents, or arbitrary tool-use surfaces. The discipline that keeps bcc small stays.

## Hypothesis

If bcc runs a Director session alongside the Executor, with the Director responsible for:

1. **Validating** the spec is executable before the loop starts (acceptance criteria are concrete, cross-cutting concerns are addressed, open questions are flagged),
2. **Planning** execution as a typed graph of phases with explicit dependencies and acceptance criteria, replacing "go do the next item" with "go do these tasks, with this scope, against these criteria",
3. **Reviewing** each phase's output against the criteria the Director itself set, with the authority to send the phase back for another pass with concrete feedback,

then the human supervision time per session drops, the rate of premature `ok` declarations drops, and the loop converges on real specs that today require the human to babysit.

The same machinery, once it works serially, unlocks **parallel** execution: independent phases in the planning graph can run in separate worktrees with independent Executor sessions and a single reconciliation pass.

## Architecture overview

```mermaid
graph TD
    USER[User] --> BCC[bcc run]
    BCC --> CFG[".bcc.toml"]
    BCC --> SPEC[Markdown spec]
    BCC --> DIR[Director session]
    DIR --> PLAN["Validated plan<br/>(typed phases DAG)"]
    PLAN --> BCC
    BCC --> EXEC[Executor session]
    EXEC --> EVT[bcc_event stream]
    EVT --> BCC
    BCC --> REV[Director review]
    REV --> VERDICT["approve / revise / escalate"]
    VERDICT --> BCC
    BCC --> NEXT["next phase / retry / done"]
    BCC --> TUI[Live TUI]
```

bcc is still the orchestrator at the process layer. The Director is the cognitive layer above the Executor: it decides what to attempt, what to check, when to escalate. The Executor is unchanged in shape; it gets a sharper, smaller brief per session.

### Director responsibilities (and non-responsibilities)

The Director:

1. Reads the user's spec.
1. Produces a **validation report** (issues, suggested patches, confidence to proceed).
1. On user approval (or auto-proceed), produces a **canonical plan** as a typed DAG of phases.
1. For each phase, **assigns an executor configuration** (model tier, reasoning effort, optionally agent family) drawn from the capability registries published by the configured Executor adapters.
1. For each phase, packages a **briefing**: scope, files of interest, acceptance criteria, out-of-scope guard, distilled context from prior phases, plus the executor assignment.
1. After the Executor completes a phase, produces a **review verdict**: approve, revise (with feedback), escalate (to user).
1. On revise, the same phase runs again with feedback bundled, and the Director may upgrade the executor assignment if the failure mode points to capability rather than misunderstanding. After N revises, escalates.
1. Maintains the plan state: which phases are done, which are queued, which depend on which.

The Director does **not**:

1. Edit user files. The Executor does the editing.
1. Run `git` operations that mutate state. bcc owns those.
1. Talk to the user freely. It produces structured outputs that bcc renders in the TUI.
1. Re-read the codebase per iteration. It works from the spec, the plan, and the prior verdicts. Code-level investigation is the Executor's job.

### Protocol with bcc

The Director communicates with bcc through a **structured tool surface** owned by bcc, in the same spirit as MCP tool calls. bcc gives the Director a small, controlled set of tools:

- `propose_validation_report(report)`
- `propose_plan(plan)` (each phase carries an `executor_assignment`)
- `propose_phase_briefing(phase_id, briefing)`
- `propose_review(phase_id, verdict, feedback, next_assignment?)`
- `request_user_input(question, options)` (escalation only)

The Director does not free-form. Every meaningful output is a typed payload bcc can validate, render, and persist. This mirrors the existing `bcc_event` discipline on the Executor side and gives the TUI exactly what it needs to display Director state.

### Capability registry on the Executor side

Each Executor adapter (claude, codex, gemini, future) publishes a typed **capability registry** that lists its available models with structured metadata: tier, reasoning-effort levels, capability flags, cost tier, and a short description. bcc merges the registries of configured adapters and passes them to the Director at planning time. The Director assigns per-phase executor configuration (tier, effort, optionally family) drawn from the merged registry; the adapter is responsible for translating those abstract values into vendor-native flags. This keeps vendor specifics on the adapter side and lets the Director reason in framework-owned terms. Full mechanism in [PRD 4](./2026-04-30-capability-aware-execution.md).

## Spec map

```mermaid
graph LR
    INIT["index.md<br/>(this doc)"] --> P1["PRD 1<br/>Validation gate"]
    INIT --> P2["PRD 2<br/>Reviewed execution"]
    INIT --> P3["PRD 3<br/>Parallel execution"]
    INIT --> P4["PRD 4<br/>Capability-aware execution"]
    P1 --> P2
    P2 --> P3
    P2 --> P4
```

PRDs are ordered by ambition and dependency. PRD 1 ships value alone (no execution-time changes). PRD 2 introduces the steered loop. PRD 3 and PRD 4 are siblings building on PRD 2: parallelism on the time axis, capability assignment on the resource axis. Either can ship first.

## Documents in this initiative

| Document | Type | Status | Summary |
|---|---|---|---|
| [index.md](./index.md) | initiative | draft | This vision document |
| [2026-04-30-spec-validation-gate.md](./2026-04-30-spec-validation-gate.md) | prd | draft | Pre-flight Director pass that scores a spec for executability and proposes patches |
| [2026-04-30-reviewed-execution.md](./2026-04-30-reviewed-execution.md) | prd | draft | The full Director loop: planning, briefed execution, per-phase review, escalation |
| [2026-04-30-parallel-phase-execution.md](./2026-04-30-parallel-phase-execution.md) | prd | draft | Independent phases run in parallel worktrees with a Director-driven reconciliation |
| [2026-04-30-capability-aware-execution.md](./2026-04-30-capability-aware-execution.md) | prd | draft | Executor adapters publish capability registries; Director assigns per-phase model and effort |
| [2026-04-30-research-claude-integration-surfaces.md](./2026-04-30-research-claude-integration-surfaces.md) | reference | draft | Claude Code integration surfaces (hooks, plugins, channels, tools, CLI) mapped to PRD opportunities |

## Cross-cutting decisions

1. **Default: off.** The Director loop is opt-in via `[director].enabled` in `.bcc.toml` or `--director` on the command line. The MVP loop remains the default until the Director earns trust on real specs.
1. **Per-component opt-in.** Validation, review, parallelism, and capability assignment are independently toggleable. A user can enable validation without review, or capability assignment without parallelism.
1. **Vendor agnostic.** The Director runs against any bcc adapter (claude, codex, gemini). Director prompt and tool surface live in `internal/director/`, not in any executor adapter.
1. **Director model is configurable separately from Executor.** Common deployments will pair a stronger Director with a cheaper Executor, but the framework does not assume that pairing.
1. **Capability registry as adapter contract.** Every Executor adapter publishes a typed registry of its available models and capabilities. The Director reasons in framework-owned abstractions (tier, effort, capability flags); the adapter translates to vendor-native flags. Adding a new model is a one-file change in the adapter.
1. **No silent overrides.** When the Director changes scope (e.g., re-orders the plan after a review) or escalates the executor assignment on retry, bcc records the change in the journal and surfaces it in the TUI. The user can always trace why a phase ran and on which model.
1. **Plan persistence.** The canonical plan, including per-phase executor assignments, is persisted to `.bcc/plan.json` so `--resume` recovers state without re-planning. The plan is regenerated when the spec changes.
1. **Spec is normative, plan is derived.** If the user edits the spec mid-run, the Director re-validates and re-plans on the next loop tick. The plan never silently diverges from the spec.
1. **User overrides win.** Per-spec directives (MVP Phase 4) and CLI flags trump the Director's executor assignment. The Director is the smart default, not the boss.
1. **Director never relaxes [absolute_restrictions](../../../internal/loop/agentcontract/absolute_restrictions.md).** No `git push`, no force operations, no credential access. The Director cannot grant the Executor permissions the framework forbids, regardless of which model it assigns.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| Cost: 2-3x token spend per phase (validate + brief + review) | Cost reporting in the TUI; per-spec budget in config; staged opt-in (start with validation only, the cheapest layer); capability assignment (PRD 4) tends to lower the per-phase floor by routing trivial phases to smaller models |
| Latency: extra round-trips before/after each phase | Director runs concurrently with bcc bookkeeping where possible; user can disable review for fast iteration |
| The Director itself drifts on long plans | Director is stateless across phases. It re-reads the (small) plan state and the (small) prior verdict per call. It does not accumulate session context. |
| Parallel worktrees produce merge conflicts | Reconciliation phase is a Director-led merge step with explicit conflict criteria; falls back to serial on unresolvable conflicts |
| Plan diverges from a moving codebase | Director re-validates against the current spec before each tick; user-visible warning when re-planning |
| User loses confidence ("what is the Director doing?") | TUI panel dedicated to Director state; every Director call logged to the journal with verdict, assignment, and reasoning summary |
| Capability registries become stale (vendors release new models faster than PRs land) | Treat registries as best-effort metadata, updated per release; CLI flag override always available as escape hatch |
| Director becomes a feature creep magnet | This initiative scopes four PRDs and stops there. Additional roles (critic, refactor agent, etc.) are explicitly out of scope. |

## Success metrics

Measured against representative long specs (the bcc repo itself, condo-fiscal phases, future trial users).

| Metric | Today (MVP) | Target with Director enabled |
|---|---|---|
| Human supervision time per spec hour | full attention | <= 25% attention |
| Premature `ok` rate (phases marked done that need rework) | qualitative; high on long specs | < 10% |
| Mean iterations to converge on a phase | varies | reduced; reviews catch drift before next phase |
| Wall-clock for parallelizable specs (PRD 3) | linear in phase count | sub-linear; bound by longest critical path + reconciliation cost |
| Token cost per representative long spec (PRD 4) | high (single-model floor) | reduced 30-50% via tier-aware routing of trivial phases |

These are directional targets; concrete instruments are defined per PRD.

## Open questions

- [ ] Director model defaults: do we ship a recommended pairing (stronger Director + cheaper Executor), or stay neutral?
- [ ] When Director and Executor disagree on whether a phase is done, who wins by default? (Proposed: Director, with override flag.)
- [ ] Mid-phase sampling: does the Director peek at the Executor's stream during long phases, or only at boundaries? (Proposed: boundaries only by default; sampling is a follow-up question for PRD 2.)
- [ ] How the Director declares "the whole spec is done" vs. handing back to the user. (Proposed: Director can only signal `done` when every phase in the plan has an approved review.)
- [ ] Failure escalation UX: a single user-prompt blocks the loop. Do we batch escalations, or always interrupt?
- [ ] Director and Executor running on different vendors: any sharp edge in the wire protocol or briefing format that breaks?
- [ ] Capability assignment trust: by default, does the Director get to escalate to a more expensive model on retry without confirmation, or only within a configured cap? (Proposed: free below cap, prompt above.)

## References

- [buchecha MVP initiative](../buchecha-mvp/index.md): the platform this builds on.
- [Spec-format vendor neutrality](../buchecha-mvp/2026-04-29-spec-vendor-neutrality.md): wire protocol the Director-Executor channel reuses.
- [Skill: fast-iteration spec authoring](../buchecha-mvp/2026-04-29-skill-spec-authoring.md): author-side complement to the validator (PRD 1).
- `internal/loop/agentcontract/`: canonical wire protocol shared by Executor and (forthcoming) Director adapters.
