bcc and the agent talk over MCP (Model Context Protocol). bcc runs a single MCP server per `bcc run`; every message from the agent to bcc and from bcc to the agent is an MCP method call routed through that server. There is no out-of-band channel: stdout text is rendered for the human, but it does not affect bcc's decisions.

Every method call carries the agent's `agent_id` as the first input field. bcc embeds `agent_id` into your prompt at a known marker; pass it back verbatim on every call. Calls without `agent_id`, or with an `agent_id` that does not match the role bcc registered for the connection, are rejected.

## Per-role method surface

The methods bcc exposes depend on the role bcc spawned you in (Planner, Briefer, Executor, Reviewer). Calling a method outside your role returns a structured MCP error.

### Planner

| Method | Purpose |
|---|---|
| `bcc_task_started(agent_id, "planning")` | Open the planning task on the timeline. Use the literal id `"planning"`. |
| `bcc_plan_emit(agent_id, plan)` | Submit the typed Plan. Validated against `plan.schema.json` and the structural invariants (phase ids unique, task ids unique within phase, no cycles, no cross-phase task deps). On rejection bcc returns the validator error; correct and re-emit. |
| `bcc_task_completed(agent_id, "planning", summary)` | Close the planning task. `summary` is one short sentence. |

### Briefer

| Method | Purpose |
|---|---|
| `bcc_get_dag_snapshot(agent_id)` | Read the full DAG state to pick the next eligible phase and sub-DAG. |
| `bcc_briefing_emit(agent_id, briefing)` | Submit the per-iteration Briefing. `iteration_id`, `phase_id`, and a non-empty `sub_dag_task_ids` are required; the sub-DAG must lie inside one eligible phase, and each task must be `pending` or `needs_fix`. |

### Executor

| Method | Purpose |
|---|---|
| `bcc_get_briefing(agent_id)` | Read the Briefing bcc bound your agent to. |
| `bcc_get_pending_tasks(agent_id)` | List tasks in your sub-DAG that are still `pending` or `needs_fix`. Use it at retry boundaries; the set shrinks as the Reviewer approves. |
| `bcc_task_started(agent_id, task_id)` | Mark a task `in_progress`. |
| `bcc_task_completed(agent_id, task_id)` | Mark a task `done`. |
| `bcc_iteration_finished(agent_id, signal, summary)` | Close the iteration. `signal` is `continue`, `review`, `done`, or `blocked`. Call this exactly once, immediately before exit. |

### Reviewer

| Method | Purpose |
|---|---|
| `bcc_get_briefing(agent_id)` | Re-read the Briefing the Executor was given. |
| `bcc_get_dag_snapshot(agent_id)` | Read your phase's task statuses. |
| `bcc_get_diff(agent_id)` | Get the unified diff between the Executor's baseline and head SHAs. |
| `bcc_get_journal_delta(agent_id)` | Get the spec-journal delta (added entries) the Executor produced. |
| `bcc_task_approved(agent_id, task_id)` | Mark a sub-DAG task `done`. |
| `bcc_task_needs_fix(agent_id, task_id, feedback)` | Return a sub-DAG task to `needs_fix`. `feedback` is the per-task correction the next attempt receives. |
| `bcc_review_finished(agent_id, outcome, reasoning)` | Close the review. `outcome` is `approve` (every sub-DAG task done), `revise` (one or more `needs_fix`), or `escalate` (non-empty `reasoning`). Call this exactly once, immediately before exit. |

## Polling pattern

There is no streaming notification from bcc to the agent. Read the state explicitly at the boundaries that matter for your role:

- **Entry**: read what bcc gave you (`bcc_get_briefing`, `bcc_get_dag_snapshot`).
- **Per task**: pair `bcc_task_started` with `bcc_task_completed` (Executor) or `bcc_task_approved` / `bcc_task_needs_fix` (Reviewer).
- **Retry boundary** (Executor): call `bcc_get_pending_tasks` after the Reviewer has audited so you only re-attempt what is still open.
- **Exit**: call the role's terminal method once (`bcc_iteration_finished` for the Executor, `bcc_review_finished` for the Reviewer, `bcc_task_completed("planning", summary)` for the Planner). A missing terminal call causes bcc to treat the run as invalid.

Do not over-poll. Three to five MCP calls per role per iteration is the working budget.

## Error handling

All MCP errors are structured. The cases you should expect and recover from:

- **Schema validation**: bcc returns the failing JSON pointer and constraint. Fix the input and call again.
- **Out of scope**: a Reviewer marking a task outside its sub-DAG, or an Executor mutating a task it does not own, gets `dag: <method>: agent_id ... not in scope`. Re-read your sub-DAG and only act on its members.
- **Plan validation** (`bcc_plan_emit`): cycle, duplicate id, cross-phase dep, empty phase. Fix and re-emit.
- **Briefing validation** (`bcc_briefing_emit`): empty sub-DAG, task not pending/needs_fix, dep neither in sub-DAG nor done. Fix and re-emit.

bcc never silently drops your call. Either you see `{"ok":true}` (or a typed result), or you get an error you can act on.

## Wire constants

The signal alphabet for `bcc_iteration_finished` and the outcome alphabet for `bcc_review_finished` are fixed English values regardless of the project's natural language. Localize human-facing artifacts (commits, journal text, prose) freely; never localize the wire.

| Wire field | Allowed values |
|---|---|
| `signal` (iteration) | `continue`, `review`, `done`, `blocked` |
| `outcome` (review) | `approve`, `revise`, `escalate` |
