---
title: "Capability-aware execution"
type: prd
status: done
authors:
  - Fernando Macedo
reviewers: []
created: 2026-05-03
decision-date: 2026-05-03
supersedes:
superseded-by:
review-by:
tags:
  - director
  - capabilities
  - cost
comments: true
---

# Capability-aware execution

## Summary

Each Phase carries optional per-role capability assignments and an
optional inline Briefing. The Planner picks model and effort for the
Briefer, Executor, and Reviewer per phase from a CapabilityRegistry the
configured adapters publish at boot. When the Planner already has
enough context to author a Briefing for a mechanical phase, it may emit
a `prepared_briefing` and the loop will skip the Briefer agent for that
phase. Per-phase routing flows through the Director ports as
RoleAssignment overrides and lands as `--model` / `--effort` flags on
the spawned agent process.

## Goals

- Each Executor and Director adapter publishes a typed list of models
  it can spawn, with per-model effort levels and a one-line
  description.
- The cli merges the lists into a CapabilityRegistry, attaches it to
  the run-wide MCP handler, and feeds it to the Planner via
  `PlannerInput.Registry`.
- The Planner sees the registry rendered as a markdown table in its
  prompt and attributes per-phase Model+Effort to each role
  (`briefer_assignment`, `executor_assignment`, `reviewer_assignment`).
- Per-phase assignments propagate through `BrieferInput.Assignment`,
  `ReviewerInput.Assignment`, and the `NewExecutor` factory's third
  argument, becoming `--model` and `--effort` flags on the spawned CLI.
- The Planner may emit a `prepared_briefing` on a Phase. When present,
  the loop calls `Handler.RecordSyntheticBriefing` instead of spawning
  the Briefer agent; the audit log records the synthetic briefing under
  `role: "planner"`.
- Configured defaults (`[agent.claude].model`, `.effort`,
  `[director.claude].model`, `.effort`) apply when the Planner does not
  attribute one; assignments override them per call.

## Non-goals

- CLI / spec-level user overrides on top of Planner assignments.
- Automatic escalation to a stronger model on retry after review.
- TUI badges that surface the assigned model per phase. The
  `mcp-log.jsonl` audit log captures the spawn args; a dashboard widget
  is future work.
- Adapters beyond Claude. The ports already accept any
  `CapabilityProvider`; Codex and Gemini follow when their adapters
  land.

## Domain model

```go
type Capability struct {
    Family      string   // "claude", "codex"
    Model       string   // canonical id
    Tier        string   // "frontier" | "balanced" | "fast"
    Efforts     []string // empty when the adapter exposes no effort knob
    Description string
}

type CapabilityRegistry struct {
    Models []Capability
}

type RoleAssignment struct {
    Model  string
    Effort string
}

type PreparedBriefing struct {
    SubDAGTaskIDs []string
    Instructions  string
}

type Phase struct {
    // ... existing fields ...
    BrieferAssignment  *RoleAssignment
    ExecutorAssignment *RoleAssignment
    ReviewerAssignment *RoleAssignment
    PreparedBriefing   *PreparedBriefing
}
```

`director.CapabilityProvider` is the discovery port adapters implement:

```go
type CapabilityProvider interface {
    Capabilities() []Capability
}
```

The cli aggregates the lists with `MergeCapabilityRegistries` and
installs the result on the run-wide handler via
`Handler.SetCapabilityRegistry`.

## Wire surface

`bcc_plan_emit` accepts the four optional Phase fields documented
above. The handler validates per-phase assignments against the attached
CapabilityRegistry (model must be in the registry, effort must be in
the model's `Efforts` list when set) and validates `prepared_briefing`
structurally (instructions non-empty, at least one task id, every
referenced task owned by the phase). Rejections come back as structured
errors so the Planner can correct and re-emit, same as any other
ValidatePlan failure.

There is no new MCP method. The registry travels into the Planner
prompt at template render time; the Briefer reads assignments from the
DAG snapshot it already fetches via `bcc_get_dag_snapshot`. The
Executor never queries the registry; the loop applies the assignment
when constructing the per-iteration Executor.

## Loop behavior

```
for each eligible phase:
  if phase.PreparedBriefing != nil:
    handler.RecordSyntheticBriefing(synthesized)        # role="planner" in audit log
  else:
    spawn Briefer with phase.BrieferAssignment override
  for each attempt:
    spawn Executor with phase.ExecutorAssignment override
    review:
      spawn Reviewer with phase.ReviewerAssignment override
    decide
```

On retry the loop reuses the prepared briefing and prepends the
Reviewer's `prior_feedback` to it; the Briefer is not invoked even on
retry when `PreparedBriefing` is set, since the Planner judged the
phase mechanical enough that a separate briefing pass adds no value.

## Adapter contract

`claude.Config` and `directorclaude.Config` both gain an `Effort`
field. The Executor passes `--model` and `--effort` whenever the
respective field is non-empty; the Director adapter's `runRole`
accepts per-call `modelOverride` and `effortOverride` that take
precedence over the configured defaults. Empty overrides preserve the
configured defaults; an empty configured default omits the flag
entirely so claude uses its built-in choice.

The hardcoded capability lists live next to the adapters
(`internal/executor/claude/capabilities.go`,
`internal/director/claude/capabilities.go`). Effort lists are
conservative: the claude CLI rejects unsupported levels at spawn time,
which surfaces as a per-iteration failure the Reviewer's `escalate`
path can absorb.

## Configuration

```toml
[agent.claude]
model  = "claude-sonnet-4-6"
effort = "low"          # default for the Executor when the Planner does not attribute one

[director.claude]
model  = "claude-sonnet-4-6"
effort = "low"          # default for the Briefer/Reviewer when the Planner does not attribute one
```

Defaults apply when the Planner omits the assignment for a role on a
phase. There is no global override that beats the Planner's per-phase
choice, by design: the cli surface is to either trust the Planner or
edit the spec.

## References

- `internal/director/capability.go`: `Capability`, `CapabilityRegistry`,
  `CapabilityProvider`.
- `internal/director/types.go`: `RoleAssignment`, `PreparedBriefing`,
  the four optional `Phase` fields, `Phase.AssignmentFor`,
  `ValidatePlan(p, registry)`.
- `internal/director/dag/handler.go`: `SetCapabilityRegistry`,
  `CapabilityRegistry()`, `RecordSyntheticBriefing`,
  `storeValidatedBriefing` (shared with `bcc_briefing_emit`).
- `internal/director/prompts/plan.md`: Available models table and the
  PreparedBriefing guidance.
- `internal/director/schemas/plan.schema.json`: `roleAssignment`,
  `preparedBriefing`, optional Phase fields.
- `internal/director/claude/`: `(*Adapter).Capabilities`, `runRole`
  per-call overrides.
- `internal/executor/claude/`: `(*Executor).Capabilities`,
  `Config.Effort`, `--effort` flag.
- `internal/loop/director_run.go`: PreparedBriefing skip path,
  per-phase Assignment propagation.
- `internal/cli/run_director.go`: registry collection via package-level
  `Capabilities()` calls, handler injection, planner registry
  population, executor override at spawn time.
