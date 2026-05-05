# How-to: troubleshoot the loop event stream

Loop events flow from `internal/loop` through `internal/services` (fan-out, ring buffer, monotonic seq) to two protocol adapters: the chi-mounted SSE endpoint at `GET /api/v1/sessions/{id}/events` and the in-process TUI subscription. The SPA dashboard consumes the SSE stream. This guide covers how to verify that delivery end to end, the wire contract that keeps the producer and consumer in lockstep, and the pitfalls that produce a silently empty dashboard.

## Verify live event delivery end to end

The fastest oracle for "what does the server actually emit" is a tiny cooperative spec, run through bcc against itself, with the SSE bytes inspected via `curl`.

### 1. Write a cooperative spec

The spec must be mechanical, predictable, and finish in a couple of minutes. One phase, one task, no real engineering judgement required.

```markdown
# Diag spec

## P1: smoke

### T1.1: write the marker file

Create `/tmp/diag-output.txt` with the literal content `hello from bcc`.
Acceptance: the file exists with that exact content.
```

Save as `/tmp/diag-spec.md`.

### 2. Build a debug binary and run it

```bash
go build -o /tmp/bcc-debug ./cmd/bcc
/tmp/bcc-debug run /tmp/diag-spec.md \
  --webui --output text --allow-dirty --no-color \
  > /tmp/bcc-debug.stdout 2> /tmp/bcc-debug.stderr &
```

Why `--output text` and not the default TUI:

- The text renderer keeps `stderr` clean and greppable; the TUI alt-screen makes the listener banner unreadable.
- `printRunBanner` (`internal/cli/run_banner.go:24`) writes the dashboard URL to the writer the cli passes; under text mode that is the same `stderr` you can `grep` from a script.

### 3. Extract the listener URL and token

```bash
grep -oE 'http://[^[:space:]]+' /tmp/bcc-debug.stderr
# => http://127.0.0.1:54321/?t=8f3c2e0a4b6d1c9e
```

The query-string token authorizes browser sessions via a cookie. For `curl` use the same token as a bearer header. Both forms map to the same per-run session token issued by the API listener.

### 4. Curl the SSE endpoint

The `live` alias resolves to whichever session the EventService is bound to (`services.LiveSessionAlias`, `internal/services/sessions.go:61`). It saves you the round-trip of looking up the session id first.

```bash
TOKEN=8f3c2e0a4b6d1c9e
PORT=54321

curl -sN --max-time 30 \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Accept: text/event-stream' \
  "http://127.0.0.1:${PORT}/api/v1/sessions/live/events"
```

The output is the raw SSE framing emitted by `SSEWriter.WriteEvent` (`internal/api/sse.go:59`):

```
retry: 5000

id: 1
event: iter_started
data: {"at":"...","index":1,"max_iter":50,"level":"info","type":"iter_started"}

id: 2
event: phase_briefed
data: {"phase_id":"P1","iteration":1,"level":"info","type":"phase_briefed", ...}

:heartbeat

id: 3
event: task_started
data: {"phase_id":"P1","task_id":"T1.1","level":"info","type":"task_started", ...}
```

Two lines that are not bugs:

- `retry: 5000` is the EventSource reconnect-backoff directive. The browser stores it and uses it on the next disconnect.
- `:heartbeat` is an SSE comment emitted every 15s (`defaultHeartbeatInterval` in `internal/api/handlers/events.go:18`) to defeat idle-timeout cuts on reverse proxies. EventSource consumers ignore comments.

### 5. Summarise what kinds the server actually emitted

```bash
curl -sN --max-time 60 \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Accept: text/event-stream' \
  "http://127.0.0.1:${PORT}/api/v1/sessions/live/events" \
  | grep -oE '^event: [a-z_]+' | sort | uniq -c
```

You should see one row per kind the run produced. If a kind you expected is missing, the producer side is at fault; if a kind is present in `curl` but not in the SPA, the consumer side is at fault.

### Live vs replay

`pickEventSource` (`internal/api/handlers/events.go:121`) tries `EventService.Subscribe` first; if the session id is not the live one (or `live`), it falls back to `EventService.Replay` against `.bcc/sessions/<id>/events.ndjson`. Use `live` while the run is in progress; use a concrete session id to inspect an archived run from disk.

## The event-stream contract

The set of `type` discriminators is closed at runtime and policed by tests at build time. Four artefacts move together:

| Artefact | Role |
|---|---|
| `internal/loop/eventjson.go` `MarshalJSONEvent` switch | Produces the JSON payload per `loop.Event` variant. |
| `internal/loop/eventjson.go` `var AllEventKinds` | Canonical, exhaustive list of `type` values the switch emits. |
| `internal/api/schemas/event.schema.json` `enum` | Wire-level enum the SPA fetches at runtime. |
| `internal/loop/eventjson_test.go` `TestMarshalJSONEvent_AllKindsCovered` | Sample-table that round-trips one of each variant. |

Two anti-drift tests:

- `TestEventSchemaEnumMatchesLoopAllEventKinds` (`internal/api/schemas_test.go:76`) compiles the embedded schema and compares its enum against `loop.AllEventKinds`. Drift in either direction fails the test.
- `TestMarshalJSONEvent_AllKindsCovered` (`internal/loop/eventjson_test.go:393`) marshals one sample per variant, asserts every emitted `type` is in `AllEventKinds`, and asserts every entry in `AllEventKinds` is reachable from the samples.

The SPA closes the loop on the consumer side. `useEvents` (`internal/webui/web/src/hooks/use-events.ts`) fetches `/api/v1/schemas/event.schema.json` once per page load, parses the enum, and registers one `addEventListener(kind, handler)` per kind. A `FALLBACK_KINDS` constant matches the same canonical list and is used only when the schema fetch fails (offline dev, broken endpoint).

### Checklist when adding a new event kind

1. Add the variant to the `loop.Event` union and any decoder paths it needs.
2. Add a `case` arm in `MarshalJSONEvent` that emits a `"type"` field.
3. Append the `"type"` string to `loop.AllEventKinds` (order matches the switch arms).
4. Append the `"type"` string to the `enum` in `internal/api/schemas/event.schema.json`.
5. Add a sample of the variant to the `samples` table in `TestMarshalJSONEvent_AllKindsCovered`.
6. If the variant should round-trip through `Replay`, add a decode arm in `services.decodeEvent` (`internal/services/events.go`).

Run `go test ./internal/loop/... ./internal/api/...`. The two anti-drift tests fail loudly if any step is skipped.

## Common pitfalls

### Named SSE events vs `onmessage`

The server frames every record with `event: <kind>\n` (`SSEWriter.WriteEvent`). By the EventSource specification, named events dispatch to listeners registered via `addEventListener('<kind>', handler)`. The default `onmessage` handler only fires on records with `event: message` or with no event field at all.

Any SSE consumer (JavaScript in the SPA, a Go test client, a third-party tool) must register listeners against the actual kinds it expects. Relying on `onmessage` will produce a stream that connects, receives bytes, and drops every event on the floor. This is also why `FALLBACK_KINDS` exists in `use-events.ts`: if the schema fetch fails, the hook still needs a list of kinds to register, otherwise no event reaches the UI.

When writing a new SSE consumer, wire it the same way: discover the kinds (preferably from the schema endpoint), register one listener per kind, and handle the `addEventListener` payloads.

### Headless mode does not forward planner events

`runDirectorTUI` (`internal/cli/run_director.go`) creates a `raw chan loop.Event` that the TUI subscribes to. The planner's `AgentEvent`s, including its `tool_use` envelopes, are tee'd onto `raw` so they appear live as the Planner reads the spec.

`runDirectorWith` (the headless `--output text|json` path) calls `resolveDirectorPlan` with `raw=nil`. With nil `raw`, `freshPlan` does not allocate the agentEvents pump and the planner runs silently. Once the Planner returns and `loop.Loop.Run` starts, ordinary loop events flow normally; only the planning phase is dark.

Operator-visible consequence: a SPA opened against a headless run during a long planning phase shows zero `agent_event` records until the Planner finishes. The dashboard appears idle even though everything is healthy. To get planner-phase events on SSE, run with the TUI. To verify the rest of the loop is healthy, wait until the first `phase_planned` arrives; everything past that point is identical between TUI and headless modes.
