---
title: "Capability-aware execution: Director-driven model and effort assignment"
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
  - capabilities
  - model-assignment
  - vendor-neutral
comments: true
---

# Capability-aware execution: Director-driven model and effort assignment

## Summary

A typed plan with explicit phases lets the Director answer a question the user is answering by hand today: which model, with how much reasoning effort, should execute this phase. A long spec rarely benefits from running every phase on the most powerful model; it also rarely benefits from running them all on the cheapest. The Director, having read the spec and authored the plan, is in the best position to assign the right tool to each phase. This PRD adds two things: a **capability registry** that each Executor adapter publishes (Claude, Codex, Gemini, future), and an **assignment step** in the Director that selects per-phase model and effort from that registry. bcc instantiates each Executor session with the assigned parameters. The user keeps override authority at the spec, config, and CLI levels.

## Problem

### Context

Different phases have different cognitive shapes. "Add a TOML field and wire it through three structs" is mechanical. "Decide how the new orchestration protocol composes with the existing wire format" is reasoning-dense. Today bcc runs every phase on whatever model and effort level the user configured globally. The user either:

- Picks a strong model and pays for it on every phase, including trivial ones, or
- Picks a cheap model and watches the Executor flounder on the hard phases, or
- Authors per-phase directives in the spec to vary model/effort by hand (the path that MVP Phase 4 opens up).

Hand-authored directives work but are tedious, easy to forget, and require the author to predict which phases will be hard, before any planning has happened. The Director already does that prediction implicitly when it plans; making it explicit is cheap and additive.

The vendor-neutral framing makes this stronger. If a project has Claude and Codex both configured, some phases may be a better fit for one or the other. Today bcc has no way to express that; with a registry per adapter, the Director can route phases across families based on capability, not just on a single global preference.

### User pain

1. **Pay-everything-for-everything.** Running a long spec on a frontier model wastes tokens on trivial phases. Running on a cheap model fails on the hard ones.
1. **Hand-authoring tax.** Per-phase directives demand the author predict difficulty before any plan exists. Most authors do not bother; the run pays the cost.
1. **No capability awareness.** Even when bcc supports multiple agent families (Phase 6+), the user picks one global agent at run start. There is no way to say "use the reasoning-strong family for the architecture phase and the code-volume family for the boilerplate phase".
1. **Silent retries on the same model.** When a phase fails review (PRD 2), the retry happens on the same model. Sometimes the right move is to escalate to a stronger model, not just to repeat the prompt with more feedback.

### Impact of not solving

bcc remains a one-knob tool: pick your model, pay your cost, accept the variance. The opportunity to differentiate on smart resource use, the most defensible argument for an orchestration layer over a single agent CLI, stays on the table.

## Goals

### What we want to achieve

- [ ] G1: Each Executor adapter exposes a structured **capability registry** of the models it can drive, with tier, reasoning-effort levels, capability flags, and a short description.
- [ ] G2: The Director, during planning (PRD 2), assigns a per-phase `executor_assignment` (model tier, effort level, optionally family) drawn from the registries of configured adapters.
- [ ] G3: bcc, when starting an Executor session for a phase, instantiates the adapter with the assigned parameters.
- [ ] G4: User overrides are preserved: per-spec directives (MVP Phase 4) and CLI flags trump the Director's assignment.
- [ ] G5: Assignments are visible in the TUI per phase, persisted in the plan, and recorded in the journal alongside the verdict.
- [ ] G6: On retry after a `revise` verdict, the Director may upgrade the assignment ("escalate to a stronger model") if the failure mode warrants.

### Success metrics

| Metric | Current | Target |
|---|---|---|
| Token cost per representative spec | high (single-model floor) | reduced 30-50% by routing trivial phases to smaller models |
| Failure rate on reasoning-dense phases when running cheap model globally | high | matched to "always-strong" baseline by selectively assigning strong on those phases |
| User overrides per spec | n/a | low; if users routinely override, the Director's policy is wrong |
| Time-to-first-good-iteration on a hard phase | varies | reduced by assigning effort up-front instead of discovering need on retry |

### Non-goals

- We do **not** discover capabilities at runtime by probing the agent CLI. Registries are static, owned by the adapter, updatable in code.
- We do **not** auto-rebalance assignments mid-phase based on streaming output. Assignments are decided at plan time and on retry.
- We do **not** offer fine-grained per-token routing (different paragraphs to different models). One assignment per phase per attempt.
- We do **not** optimize for cost minimization at all costs. The Director balances cost, capability, and the Executor's likelihood of success. The user can pin a cost ceiling.
- We do **not** ship a vendor-comparison harness. The registries describe each family on its own terms; bcc does not benchmark them.

## Audience

| Segment | Description | Estimated volume |
|---|---|---|
| Cost-aware power users | Run long specs and care about token spend | Primary |
| Multi-vendor users | Have Claude + Codex (or others) configured and want bcc to route smartly | High value, gated by Phase 6 multi-agent landing |
| Default users | Have one adapter; benefit from intra-family tier routing (Opus vs Sonnet vs Haiku) without thinking | Largest segment |

## Requirements

### Functional

#### Capability registry

- [ ] FR1: Each Executor adapter implements a `Capabilities() Registry` method (or equivalent port). The registry is a typed payload owned by `internal/director/` (or a shared schema package).
- [ ] FR2: The registry per adapter contains, per model: `id` (vendor-native), `family` (e.g., `claude`, `codex`, `gemini`), `tier` (`small | medium | large | frontier`), `reasoning_effort_levels` (subset of `low | medium | high`), `capabilities` (flags: `tool_use`, `vision`, `long_context`), `cost_tier` (relative 1-5), `description` (short prose, framework-owned, not vendor marketing).
- [ ] FR3: Registries are static in the binary and updated via PR. No runtime discovery in this PRD.
- [ ] FR4: Adapter publishes a `default_model` and a `default_effort` for the case when the Director is not enabled.
- [ ] FR5: Registry has a stable schema; new fields land additively. Plans persisted with an older schema still load (forward compatibility for `--resume`).

#### Director assignment

- [ ] FR6: At plan time, the Director receives the merged registry (all configured adapters) and assigns a per-phase `executor_assignment`: `{ family?, model_tier, effort, max_cost_tier? }`.
- [ ] FR7: The plan persists the assignment per phase in `.bcc/plan.json`.
- [ ] FR8: On retry after `revise`, the Director may produce an updated assignment for the next attempt, with reasoning recorded in the verdict.
- [ ] FR9: When multiple adapters are configured and the Director chose `family` explicitly, bcc routes the Executor session to that adapter. When `family` is unset, bcc uses the configured default adapter and selects within its registry.

#### bcc instantiation

- [ ] FR10: bcc's adapter wiring (`cmd/`) reads the `executor_assignment` and constructs the Executor session with the corresponding model, effort, and family.
- [ ] FR11: The Executor adapter translates abstract `tier`/`effort` into vendor-native flags. Translation is owned by the adapter, not by the Director.
- [ ] FR12: If the assigned model is unavailable at runtime (rate limit, deprecation), the adapter returns a structured error; bcc surfaces it to the Director, which produces a fallback assignment.

#### User overrides

- [ ] FR13: Per-spec directives (MVP Phase 4) override the Director's assignment for that phase.
- [ ] FR14: CLI flag `--force-model <model_id>` overrides all assignments for the run, with a warning that the Director's policy is being bypassed.
- [ ] FR15: Config-level cost cap (`[director].max_cost_tier`) limits the Director's selection range. The Director cannot assign a model whose cost exceeds the cap.

#### Visibility and audit

- [ ] FR16: The TUI shows the assignment per phase: family, model tier, effort. On retry, both old and new assignment are visible.
- [ ] FR17: The journal entry per phase records the assignment used (vendor-native model id, effort), so post-hoc analysis is possible.

### Non-functional

- [ ] NFR1: Adding a new model to an adapter is a single-file change in that adapter, no edits elsewhere.
- [ ] NFR2: Adding a new agent family is a new adapter package implementing the existing ports. The Director needs no edits to consume its registry.
- [ ] NFR3: The Director's assignment prompt is owned by `internal/director/`, embedded via `//go:embed`. Not user-editable.
- [ ] NFR4: Performance: registry merging and assignment add negligible overhead to planning (target: under 1 second compute time, excluding the LLM call).
- [ ] NFR5: Privacy: registries are static metadata, not telemetry. bcc does not phone home about which models were assigned.

### Schema sketch (illustrative)

Bound to PRD 2's schema. The new fields are marked `// new`.

```typescript
type Phase = {
  id: string;
  title: string;
  intent: string;
  depends_on: string[];
  parallelizable: boolean;
  scope_in: string[];
  scope_out: string[];
  acceptance: AcceptanceItem[];
  retry_budget: number;
  executor_assignment: ExecutorAssignment; // new
};

type ExecutorAssignment = {                   // new
  family?: string;                            // adapter selector; absent => default adapter
  model_tier: "small" | "medium" | "large" | "frontier";
  effort: "low" | "medium" | "high";
  max_cost_tier?: number;                     // honors config cap
  rationale: string;                          // why the Director picked this
};

type ModelEntry = {                           // new (per adapter registry)
  id: string;                                 // vendor-native, e.g., "claude-opus-4-7"
  family: string;
  tier: "small" | "medium" | "large" | "frontier";
  reasoning_effort_levels: ("low" | "medium" | "high")[];
  capabilities: { tool_use: boolean; vision: boolean; long_context: boolean };
  cost_tier: number;                          // relative 1-5
  description: string;                        // framework-owned short prose
};

type Registry = {                             // new
  family: string;
  default_model: string;
  default_effort: "low" | "medium" | "high";
  models: ModelEntry[];
};
```

## Expected experience

### Story 1: intra-family tier routing

> As a Claude-only user, I want the Director to route my mechanical phases to a smaller Claude model and my reasoning-dense phases to Opus, so my long spec costs less without losing quality where it matters.

Flow:

1. User runs `bcc run --director` with the Claude adapter configured.
1. Director plans 8 phases. Assigns: 5 phases to Sonnet at medium effort, 2 to Opus at high effort, 1 to Haiku at low effort.
1. TUI shows assignment per phase with one-line rationale ("phase 4 is mechanical struct wiring; Haiku low").
1. User confirms; loop runs. Per-phase Executor sessions instantiate with the assigned model.
1. Journal records the model used per phase.
1. End of run: TUI cost summary shows split spend.

### Story 2: cross-family routing (gated)

> As a user with Claude and Codex configured, I want the Director to route phases to whichever family is the better fit, so I get the strengths of each without manual switching.

Flow:

1. User runs `bcc run --director`.
1. Director's plan assigns reasoning-heavy phases to Claude (`family: "claude", tier: "frontier"`) and code-volume phases to Codex (`family: "codex", tier: "large"`).
1. bcc dispatches each Executor session to the corresponding adapter.
1. TUI shows family per phase. Journal records family + model.

### Story 3: retry escalation

> As a user, I want the Director to escalate the model on retry when its review reasoning suggests the previous attempt failed because of capability, not because of misunderstanding.

Flow:

1. Phase 6 runs at Sonnet medium. Review fails: complex refactor missed an edge case.
1. Director's verdict reasons: "Likely capability ceiling; recommend escalation".
1. Retry assignment: Opus high.
1. Retry passes review.
1. Journal records both attempts and the rationale for escalation.

### Story 4: cost cap

> As a cost-sensitive user, I want a hard cap on what the Director can assign, so I never get surprised by a frontier-model spend on a long run.

Flow: user sets `[director].max_cost_tier = 3`. Director plans; whenever it would have selected tier 4 or 5, it picks the highest-tier available within the cap and notes the constraint in `rationale`. User sees the trade-off in the plan review screen and can lift the cap if they want.

## Constraints and dependencies

- **Hard dependency on PRD 2.** Capability assignment is decoration on the typed plan; without the plan, there is nothing to assign to.
- **Soft dependency on MVP Phase 4** (`docs/specs/buchecha-mvp/2026-04-29-phase-4-execution-tuning.md`). Phase 4 introduces per-phase model/effort tuning surfaces in adapters via directives. This PRD reuses those surfaces; the Director becomes another consumer alongside the spec author.
- **Hard dependency on multi-family adapter support** for cross-family routing only (Story 2). Intra-family tier routing (Story 1) ships as soon as the Claude adapter has more than one model in its registry, which is already the case in production.
- **No new permission surface.** The Director's authority extends to choosing among models the user already authorized. It cannot use a vendor the user did not configure.
- **Subject to absolute restrictions.** A capability assignment cannot grant the Executor permissions the framework forbids.

## Alternatives considered

| Alternative | Pros | Cons |
|---|---|---|
| Hand-authored per-phase directives (status quo, MVP Phase 4) | Zero new infrastructure; predictable | Tedious; requires authors to predict difficulty; complementary, not a replacement |
| One global model per run | Simplest | Pays the floor on every phase; loses the multi-tier value proposition |
| Adapter-side automatic model selection (each family decides internally) | No Director needed | Couples capability decisions to vendor; defeats vendor neutrality |
| Dynamic capability discovery (probe agent at startup) | Always up-to-date | Slow, brittle, vendor-specific. Static registry plus PR updates is enough |
| Cost-only optimization (cheapest model that "might work") | Maximum savings | Tanks success rate; the Director is supposed to balance, not minimize |

## Open questions

- [ ] Registry update cadence: how often do we revise the registry as vendors release new models? Proposed: per-release, treated like dependency bumps.
- [ ] Cost-tier scale: relative (1-5) or absolute (USD per Mtok)? Lean relative, because absolute drifts and bcc has no business tracking pricing. Open for discussion.
- [ ] Should the user see the assignment plan and approve, like they approve the plan itself (PRD 2)? Lean toward yes by default, with `--auto-proceed` to skip.
- [ ] Effort interaction across families: Claude has reasoning effort levels; not all vendors do. Translation policy for adapters that lack the concept? Proposed: adapter declares `reasoning_effort_levels: []` and bcc downgrades silently with a debug log.
- [ ] Mid-spec capability change (a model removed from the registry while a plan is in flight): how does the Director respond? Proposed: re-plan affected phases on the next tick.
- [ ] Should there be a "challenge mode" where the Director deliberately picks a smaller model than its policy suggests, for benchmark-like measurement? Probably out of scope; mention as a future hook.
- [ ] User trust calibration: when the Director escalates to a more expensive model, does bcc require user confirmation by default, or only above the cost cap? Proposed: only above the cap; below the cap, the Director is trusted by configuration.

## References

- [Initiative index](./index.md)
- [PRD 2: Reviewed execution](./2026-04-30-reviewed-execution.md): introduces the typed plan this PRD decorates.
- [PRD 3: Parallel phase execution](./2026-04-30-parallel-phase-execution.md): independent of this PRD; capability assignments apply per worktree as they would serially.
- [MVP Phase 4: Execution tuning](../buchecha-mvp/2026-04-29-phase-4-execution-tuning.md): per-phase tuning surfaces this PRD reuses.
