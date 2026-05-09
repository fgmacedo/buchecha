# Director: a planning and reviewing loop on top of bcc

The Director is the default mode of `bcc run`: a session that plans, briefs, executes, and reviews against your spec. There are four cognitive roles, each spawned as a separate Claude Code agent and wired to bcc through a single MCP server:

- **Planner**: reads the spec and emits a typed Plan (a DAG of phases and tasks).
- **Briefer**: picks the next eligible sub-DAG and emits a per-iteration Briefing.
- **Executor**: runs the briefed work and emits per-task progress.
- **Reviewer**: audits the Executor's diff against per-task acceptance criteria and decides approve, revise, or escalate.

bcc itself is the orchestrator: it owns the loop, the per-session state, the MCP server, the TUI, and the protocol between roles.

The full design lives in [PRD 5](../specs/director/2026-05-02-executable-plan-dag.md); this guide is the operator reference.

## Running

```bash
bcc run docs/specs/<spec>.md
```

Director is the only mode. The legacy single-agent loop is no longer exposed by the CLI.

```toml
# .bcc.toml
[director]
retry_budget = 2     # default attempts per sub-DAG before escalation
mcp_audit = true     # write every MCP call to <session-dir>/mcp-log.jsonl

[director.claude]
binary = "claude"
# model = "claude-opus-4-7"
extra_args = []
max_budget_usd = 0   # > 0 caps each Director call; fail-closed when exceeded
```

## What happens when you run

```
spec → Planner ─► Plan (DAG of phases and tasks)
          │
          ▼
   while pending tasks:
     Briefer ─► Briefing (one phase, one sub-DAG)
        │
        ▼
     for attempt in 1..1+retry_budget:
       Executor ─► per-task progress, iteration_finished
       Reviewer ─► per-task verdicts, review_finished
                       │
                       ▼
              approve | revise | escalate
```

1. The Planner reads the spec via the Read tool and submits the Plan through `bcc_plan_emit`. bcc validates the structure (phase ids unique, task ids unique within phase, no cycles, no cross-phase task deps) and persists it to the session directory.
2. While any task is still `pending` or `needs_fix`, the Briefer picks one eligible phase, decides which subset of its tasks to attempt this iteration (the **sub-DAG**), and submits the Briefing through `bcc_briefing_emit`.
3. The Executor reads the Briefing through `bcc_get_briefing`, runs the work, and reports per-task progress through `bcc_task_started` / `bcc_task_completed`. It closes with `bcc_iteration_finished(signal, summary)`.
4. The Reviewer reads the Briefing, the phase baseline (`bcc_get_baseline`), and the journal delta (`bcc_get_journal_delta`); uses Bash with git diff/log/show to inspect the cumulative work; audits each task; calls `bcc_task_approved` or `bcc_task_needs_fix(feedback)`; and closes with `bcc_review_finished(outcome, reasoning)`.
5. The decider walks the sub-DAG state: every task `done` advances the iteration; any `needs_fix` retries the Executor with the per-task feedback rolled into the next prompt; an explicit `escalate` outcome (or running out of retry budget) pauses the loop and asks the user.

`bcc run` returns `ExitDone` only when the DAG has no pending tasks left.

## Sessions

Each `bcc run` invocation is a **session** with a stable 12-hex-char id. Every artifact the run produces is rooted at the session directory; sessions never overwrite each other.

```
.bcc/
└── sessions/
    └── <session-id>/
        ├── manifest.json           Session{ID, SpecPath, SpecHash, CreatedAt, UpdatedAt, Status}
        ├── plan.json               canonical Plan emitted by the Planner
        ├── dag.json                live DAG state (per-task statuses)
        ├── briefings/<iter-id>.json         Briefing as emitted
        ├── briefings/<iter-id>.prompt.md    materialized Executor system prompt
        └── mcp-log.jsonl           append-only audit log of every MCP call
```

`Status` cycles through `running` (start), `escalated_pending` (loop paused for a human reply), `done` (clean exit), and `aborted` (any non-`done` exit). The manifest is rewritten atomically on every status change.

### Listing and inspecting

```bash
bcc sessions list             # most recently updated first
bcc sessions list --output json
bcc sessions show <id>        # full manifest as text
bcc sessions show <id> --output json
```

### Resume semantics

```bash
bcc run --resume docs/specs/<spec>.md                     # most recent session for this spec
bcc run --resume --session <id> docs/specs/<spec>.md      # specific session
bcc run --session <id> docs/specs/<spec>.md               # specific session, no fallback
```

Outcomes:

1. **`--resume` only**: bcc looks up sessions whose `spec_path` matches and resumes the most recent. If the spec hash is unchanged, the persisted Plan is reused. If it diverged, bcc calls the Planner again, prints a `PlanDiff` for the user's information, persists the new Plan, and starts the loop. With no matching session, bcc creates a fresh one and proceeds.
2. **`--resume --session <id>`**: same as above, but the named session is the one resumed. Spec mismatch returns `ErrSessionSpecMismatch`; missing id returns `ErrSessionNotFound`.
3. **`--session <id>` without `--resume`**: the named session is reopened; bcc never creates a fresh session under this form. Missing id is fatal.
4. **No flags**: a fresh session is created.

When a resumed session has tasks stuck in `in_progress` (an agent that died mid-iteration), bcc rewrites them to `pending` so the next iteration picks them up.

## MCP communication

Every message between bcc and an agent is an MCP method call routed through bcc's run-wide MCP server. The full per-role surface is described in [`internal/loop/agentcontract/wire_protocol.md`](../../internal/loop/agentcontract/wire_protocol.md). The high-level shape:

| Role | Reads | Writes |
|---|---|---|
| Planner | spec via Read tool | `bcc_plan_emit`, `bcc_task_started/completed("planning")` |
| Briefer | `bcc_get_dag_snapshot` | `bcc_briefing_emit` |
| Executor | `bcc_get_briefing`, `bcc_get_pending_tasks` | `bcc_task_started/completed`, `bcc_iteration_finished` |
| Reviewer | `bcc_get_briefing`, `bcc_get_baseline`, `bcc_get_journal_delta`, `bcc_get_dag_snapshot` | `bcc_task_approved`, `bcc_task_needs_fix(feedback)`, `bcc_review_finished` |

Every call carries the `agent_id` bcc embedded in the role's prompt. Calls without `agent_id`, with an unregistered id, or with a role that does not match the connection are rejected with a structured error.

## Four-option escalation

When the Reviewer returns `escalate`, or when the retry budget is exhausted on `revise`, bcc pauses the loop and asks the user. The choices:

| Key | Reply | Effect |
|---|---|---|
| `R` | resume with hint | The next Briefing for the still-pending sub-DAG receives the hint as a "User hint (escalation)" block above the Reviewer feedback. |
| `F` | force-approve | bcc synthetically marks every still-pending sub-DAG task as `done`. The audit log records the synthetic write under `role: "user"` and `method: "bcc_force_approve"`. |
| `S` | skip | The phase is left as is and the run terminates with `ExitInvalid` at end-of-run. |
| `A` | abort | The run stops immediately. |

In TUI mode the modal opens on the choice screen and, on `R`, switches to a hint input where Enter submits and Esc cancels back. In text/json modes the stdin gate reads the letter and, on `r`, reads the next line as the hint (empty line means no hint).

## Troubleshooting

When a run misbehaves the audit log is the first place to look.

```bash
tail -n 50 .bcc/sessions/<id>/mcp-log.jsonl
```

Each line is `{at, role, agent_id, method, input, result, err?}`. Common patterns:

- **Planner kept rejecting `bcc_plan_emit`**: the validator error is in `err`; usually a phase cycle, an empty phase, or a duplicate id.
- **Briefer emitted an empty sub-DAG**: shows up as `bcc_briefing_emit` errors with `empty sub_dag_task_ids`. The eligible phase had no `pending` or `needs_fix` tasks; check `dag.json`.
- **Reviewer never called `bcc_review_finished`**: the loop treats this as `escalate`. The audit log shows the missing terminal method; the Reviewer probably exited early.
- **Executor head did not advance**: the loop terminates with `head_stuck`. The Executor produced no commits during the attempt; usually a tooling failure or a prompt that did not commit.

Disable the audit log with `[director].mcp_audit = false` if it grows uncomfortably large; the format is plain JSONL but a long run can produce hundreds of kilobytes.

## Limits today

- The Director runs only against the Claude adapter. The MCP protocol is vendor-neutral by construction; codex and gemini adapters are unblocked but not written.
- Mid-run spec edits are not detected automatically. Edit the spec, stop the run, then `bcc run --resume <spec>` to pick up the change.
- Capability-aware execution (per-task model assignment) is tracked in [issue #3](https://github.com/fgmacedo/buchecha/issues/3); the Plan does not yet carry per-task executor metadata.
- Parallel sub-DAG execution across worktrees is tracked in [issue #2](https://github.com/fgmacedo/buchecha/issues/2); today the loop runs one Executor at a time.
