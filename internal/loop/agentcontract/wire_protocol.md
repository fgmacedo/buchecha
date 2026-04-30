bcc parses JSON Lines on stdout to track your progress. Emit these in addition to your normal output (they are not visible to humans reading the agent's text). Each line is a single complete JSON object.

When you start working on a unit:

```jsonc
{"type":"bcc_event","event":"task_started","id":"<unit-id>","summary":"<one-line>"}
```

When you finish a unit (or sub-item; emitting per sub-item is encouraged):

```jsonc
{"type":"bcc_event","event":"task_completed","id":"<unit-id>"}
```

Immediately before exit, **exactly once**:

```jsonc
{"type":"bcc_event","event":"iteration_result","value":"<value>","summary":"<one-line>"}
```

Where `<value>` is one of:

- `continue`: the iteration produced normal progress; bcc runs another iteration.
- `review`: an observer-driven gate is reached; bcc stops and waits for the user to edit and re-trigger.
- `done`: every pending work unit is complete; bcc terminates with success.
- `blocked`: unrecoverable failure; bcc stops with non-zero exit.

The wire protocol uses fixed English values regardless of the project's natural language. Localize human-facing artifacts (commits, journal text) freely; never localize the wire.

A missing or malformed `iteration_result` causes bcc to exit invalid. Do not exit without emitting it.
