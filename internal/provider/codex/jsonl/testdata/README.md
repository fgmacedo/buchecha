# codex JSONL fixtures

Real captures from `codex-cli 0.130.0`. They ground the parser in observed
output instead of an undocumented schema. Re-capture when the codex CLI
ships a new event shape.

## How they were produced

```bash
# read-only listing
mkdir -p /tmp/codex-sample-work && cd /tmp/codex-sample-work
codex -a never exec --skip-git-repo-check --ephemeral --ignore-user-config \
  -C /tmp/codex-sample-work --json -s read-only \
  "List the files in the current directory and describe each in one line." \
  < /dev/null

# workspace-write file creation
mkdir -p /tmp/codex-sample-work2 && cd /tmp/codex-sample-work2
codex -a never exec --skip-git-repo-check --ephemeral --ignore-user-config \
  -C /tmp/codex-sample-work2 --json -s workspace-write \
  "Create a file named hello.txt in the current directory with content 'hi'." \
  < /dev/null
```

Two notes worth recording for future maintenance:

1. `codex exec` does **not** accept `--ask-for-approval`; the flag lives at
   the top level. The invocation passes it as `codex -a never exec ...`.
2. The `< /dev/null` redirection is necessary because `codex exec` waits on
   stdin even when the prompt comes as a positional argument.

## Distinct `type` values observed

Top-level envelopes (the `type` field on the outer JSON object):

| type             | meaning                                                  |
|------------------|----------------------------------------------------------|
| `thread.started` | session start; carries `thread_id`                       |
| `turn.started`   | model turn begins; no payload                            |
| `item.started`   | an item enters the `in_progress` state (tool execution)  |
| `item.completed` | an item reaches a terminal state                         |
| `turn.completed` | model turn ends; carries `usage` token counters          |

Nested `item.type` values observed inside `item.started` / `item.completed`:

| item.type           | meaning                                                                |
|---------------------|------------------------------------------------------------------------|
| `agent_message`     | model-authored prose (`text` field)                                    |
| `command_execution` | the agent ran a shell command (`command`, `aggregated_output`, `exit_code`, `status`) |
| `file_change`       | the agent edited the workspace (`changes[]` with `path` and `kind`)    |

The schema is not yet documented upstream. Other `item.type` values likely
exist (MCP tool calls, web fetches, etc.); the parser treats unknown
shapes as `KindToolUse` / `KindToolResult` skeletons so they surface in
the UI without crashing, and drops envelopes it cannot identify after a
`slog.Debug` line.
