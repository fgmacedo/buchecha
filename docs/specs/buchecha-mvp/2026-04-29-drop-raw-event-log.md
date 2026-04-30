---
title: "Drop the per-iteration raw event log"
type: spec
status: draft
authors:
  - Fernando Macedo
reviewers: []
created: 2026-04-29
decision-date:
superseded-by:
supersedes:
review-by:
tags:
  - mvp
  - architecture
  - cleanup
---

# Drop the per-iteration raw event log

## Summary

`bcc` writes a per-iteration raw event log file (`.bcc/logs/<spec-slug>-iter<n>.jsonl`) and exposes its path to the agent subprocess via `BCC_JSONL_PATH`. The file has no consumer at runtime: the loop drives the live TUI from the in-process `AgentEvent` channel, and orchestrators that need a durable record use `bcc run --output json`. The file is a leftover from an earlier design where a separate `bcc watch` process tailed it. This spec removes the file, the env var, the `bcc watch` stub, and every type, field, and CLI surface that exists only to support them.

## Status

**Draft, floating.** Not numbered as a phase. Pull forward whenever; the work is small and isolated.

## Context and motivation

The current shape:

- `internal/loop/loop.go` sets `BCC_JSONL_PATH = <JSONLDir>/<slug>-iter<n>.jsonl` per iteration and `os.MkdirAll(jsonlDir, ...)` ahead of time.
- `internal/executor/claude/log.go` owns a `rawLog` that opens the path, writes one stream-json line per scanned event, optionally appends a synthetic `{"type":"interrupted"}` terminator on cancel, and closes on return. The path comes back in `ExecResult.LogPath`.
- `internal/executor/fake/fake.go` mirrors that contract for tests.
- The path is surfaced in `IterationFinished.LogPath`, in NDJSON as `log_path`, in the text backend's slog attrs, in the loop's milestone slog calls, and in the startup banner printed by `bcc run`.
- The prompt template instructs the agent to "Cite `BCC_JSONL_PATH` (or its short form) in your Notes for observer so the observer can correlate."
- `bcc watch <spec>` is a stub that returns "not implemented yet (Phase 2)".

What reads the file: nothing. The TUI consumes events live from the in-process channel. No package outside the writer's own tests opens it. `bcc watch` was the planned consumer and was abandoned when the watcher moved into `bcc run`.

What `--output json` already covers: a durable, structured, loop-level NDJSON record on stdout. Every field that today appears in the raw stream is normalized into `AgentEvent` and serialised. The only loss is byte-level fidelity (raw `tool_use.input`, raw `tool_result.content` shape, unknown event types that hit the parser's silent default). That fidelity has had no concrete user since Phase 2 introduced the live TUI.

The cost of keeping the file: `BCC_JSONL_PATH` plumbing crosses the loop / executor / cli boundary; `JSONLDir` is a Loop field; `ExecResult.LogPath` is a port-level concept; the prompt carries a breadcrumb instruction; tests assert file contents; documentation lists an env var the agent does not strictly need. Every adapter we add later (codex, gemini) inherits the choice and either copies the boilerplate or has to opt out.

The right shape is to delete the file and everything that exists only to support it. When a real parser-debug need shows up, a focused `--dump-stream <file>` flag on `bcc run` is a five-line addition.

## Goal

- The Claude adapter no longer writes any file. Stream-json arrives via stdout pipe, is parsed into `AgentEvent`s, and is forwarded; nothing else.
- `BCC_JSONL_PATH` is gone from the agent subprocess environment, from the prompt, from `bcc run`'s banner, and from documentation.
- `loop.Loop` no longer takes `JSONLDir`, no longer creates `.bcc/logs/`, no longer sets `BCC_JSONL_PATH`.
- `loop.ExecResult.LogPath` and `loop.IterationFinished.LogPath` are removed. NDJSON loses the `log_path` key on `iter_finished`. Text backend loses the `log_path` slog attr.
- `bcc watch` is removed (no consumer, no subcommand stub).
- Phase 2 P2.11 loses the `[l] Audit log path` action and the "view audit log" menu item; the session menu shows `[r] resume / [e] edit / [j] journal / [q] quit` only.
- The bcc-markdown skill / authoring guidance loses any reference to `BCC_JSONL_PATH` as a journal breadcrumb.
- `.gitignore`'s `.bcc/` entry stays. It is cheap and still covers `.bcc.toml` and any future per-project scratch.

## Implementation Plan

Items are intentionally not numbered as P-X.Y; this spec stands on its own.

### Adapter and loop surgery

1. [x] **Delete `internal/executor/claude/log.go`** along with `rawLog`, `openRawLog`, `writeLine`, `writeInterruptedTerminator`, and `(*rawLog).close/Path`.
1. [x] **Simplify `internal/executor/claude/claude.go`**: drop the `openRawLog(os.Getenv("BCC_JSONL_PATH"))` call, the `defer rl.close()`, and every `LogPath: rl.Path()` field on returned `ExecResult`s. `streamLines` no longer takes `*rawLog` and no longer writes the line before parsing. The cancel path no longer appends an interrupted terminator (no file to write to).
1. [x] **Simplify `internal/executor/fake/fake.go`**: remove `Step.RawLog` and the `os.WriteFile(logPath, ...)` block. The fake replays only `Events`. Returned `ExecResult` carries `ExitCode` only.
1. [x] **Strip `BCC_JSONL_PATH` and `JSONLDir` from `internal/loop/loop.go`**: drop the `JSONLDir` field from `Loop`, the default `.bcc/logs` resolution, the `os.MkdirAll(jsonlDir, ...)` call, the per-iteration `jsonlPath := ...`, the `os.Setenv("BCC_JSONL_PATH", ...)` call, the `"jsonl_dir"` and `"jsonl"` slog attrs.
1. [x] **Remove `LogPath` from the port and event types** (`internal/loop/events.go`, `internal/loop/ports.go`): `ExecResult` has `ExitCode` only; `IterationFinished` has no `LogPath` field; the `Executor.Run` doc comment no longer mentions a raw log.

### CLI and rendering

1. [x] **Drop the banner mention in `internal/cli/run.go`**: the "agent subprocess will see ..." line lists `BCC_RUNNING`, `BCC_ITERATION`, `BCC_MAX_ITERATIONS`, `BCC_SPEC_PATH`, `BCC_BRANCH` only.
1. [x] **Remove `log_path` from the text backend** in `internal/cli/render.go`: `textRenderEvent`'s `IterationFinished` case drops the `slog.String("log_path", e.LogPath)` attr.
1. [x] **Remove `log_path` from the NDJSON schema** in `internal/loop/eventjson.go`: `iter_finished` carries `index`, `result`, `head_advanced`, `newly_checked`, `duration_ms` only. Update the schema example in the Phase 2 spec accordingly.
1. [x] **Delete `internal/cli/watch.go` and the `watch` subcommand registration**. `bcc --help` no longer lists `watch`. Tests that probed for the subcommand are removed.

### Prompt and documentation

1. [x] **Trim `internal/loop/prompt.go`**: drop the line "BCC_JSONL_PATH is the path of this iteration's raw event log." from the env var list, the sentence "Cite BCC_JSONL_PATH (or its short form) in your Notes for observer so the observer can correlate." entirely, and the `BCC_JSONL_PATH` token from the single-shot template's env var list.
1. [x] **Update `docs/guides/autonomous-execution.md`**: remove the `BCC_JSONL_PATH` row from the env var table; do not narrate the removal.
1. [x] **Update `AGENTS.md`**: remove `JSONLPath` from the value-objects example (it never became a type and the example now reflects only what exists).
1. [x] **Rewrite Phase 1 (`2026-04-29-phase-1-bash-parity.md`) body in place** so the goals and plan describe the target state: the executor streams events, the loop drives iterations off of those events, no per-iteration file is written. Journal entries stay untouched (they are history). Specifically: the "Executor (...) streams JSONL events to a per-iteration file" goal becomes "Executor (...) streams agent events from the subprocess"; P1.2's `Step{JSONL, ...}` becomes `Step{Events, ...}`; P1.3's "opens per-iteration JSONL files under JSONLDir" line and the "JSONL file emission" test bullet are removed; the architecture overview's mermaid diagram drops the JSONL artifact.
1. [x] **Rewrite Phase 2 (`2026-04-29-phase-2-tui-dashboard.md`) body in place** so it describes today's behavior without the raw log: drop the "Persists raw stream to `.bcc/logs/...`" sentence from the goals; drop the "show audit log" item from the post-loop session contract; remove `LogPath string` from the `IterationFinished` and `ExecResult` snippets; remove the `log_path` field from the NDJSON example; drop `log.go` from the package layout block; delete P2.2's "persist raw stream to .bcc/logs/" sub-item; delete P2.11's `[l] Audit log path` menu line and the corresponding sub-item; update P2.11's "Session overlay" sub-item bindings to `Resume / Edit / Journal / Quit`; remove the JSONL-path capture from the loop-control change. Journal entries stay untouched.
1. [x] **Index update**: list this spec under "Floating specs" in `docs/specs/buchecha-mvp/index.md`. Remove the open question about whether `bcc watch` auto-attaches via PID file (the question is moot once `bcc watch` is gone). Phase 0's `watch` subcommand bullet is rewritten to list `run` and `init` only.

### Tests

1. [x] **Delete `TestRun_RawLogWrittenAtBCCJSONLPath`** in `internal/loop/loop_test.go`.
1. [x] **Delete `TestRun_RawLogWrittenAtBCCJSONLPath`** in `internal/executor/fake/fake_test.go`.
1. [x] **Drop `withLogPath`, all `BCC_JSONL_PATH` setenvs, and every `LogPath` assertion** in `internal/executor/claude/claude_test.go`. The `TestRun_StreamsEventsFromFixture` and adjacent tests assert the parsed `AgentEvent` stream and the exit code; that is the contract.
1. [x] **Drop `JSONLDir: t.TempDir()`** from every loop test (`internal/loop/loop_test.go`, `internal/loop/integration_test.go`). The Loop no longer takes the field.
1. [x] **Update `internal/loop/eventjson_test.go`**: the `iter_finished` byte-for-byte fixtures lose the `log_path` key.
1. [x] **Update `internal/cli/render_test.go`**: same change in the text and NDJSON expectations.
1. [x] **Update `internal/loop/prompt_test.go`**: drop `BCC_JSONL_PATH` from the env var list assertion.
1. [x] **Verify `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test -race ./...`** are all clean. `bcc --help` lists `run` and `init` only. Re-running an existing spec under `bcc run` (any output mode) shows no `BCC_JSONL_PATH` in the banner, no `log_path` in NDJSON, no file under `.bcc/logs/`.

## Done criteria

- The four shell checks above are clean (`go build`, `go vet`, `gofmt`, `go test -race`).
- `grep -r 'BCC_JSONL_PATH\|JSONLDir\|JSONLPath\|rawLog\|LogPath\|log_path' --include='*.go'` returns nothing in non-test code, and only intentional fixture strings (none) in tests.
- `bcc run` on any spec produces zero files under `.bcc/logs/` (the directory is not created).
- The Phase 1 and Phase 2 specs read as if this design were always in place. No "REMOVED:" markers, no "previous version" prose.

## Stop criteria

Stop and reopen the design if:

- A real adapter-debug need shows up during the cleanup that is not addressed by `--output json`. In that case, land a small `--dump-stream <file>` flag on `bcc run` (opt-in, off by default) inside this spec rather than spawn a separate one.
- Removing `LogPath` from `IterationFinished` breaks an external consumer the user has in mind. The user owns the call.

## Out of scope

- Adding `--dump-stream` or any other adapter-debug surface. Land it only when a concrete debug session needs it.
- Rewriting the agent's "Notes for observer" guidance. The breadcrumb pattern stays useful (commit hash, iteration index, branch); it just no longer cites a non-existent log path.
- Touching `--output json`'s schema beyond removing the `log_path` key. The "additive only" rule for the schema is preserved by removing a field that was never load-bearing.

## Related

- [Phase 1: bash parity](./2026-04-29-phase-1-bash-parity.md): originally introduced the per-iteration JSONL artifact; body is rewritten as part of this work.
- [Phase 2: TUI dashboard](./2026-04-29-phase-2-tui-dashboard.md): introduced the live event channel that obviated the file; body is rewritten as part of this work.

## Execution Journal

Most recent entries on top. Contract in [Autonomous execution guide](../../guides/autonomous-execution.md#execution-journal).

### 2026-04-29 20:55, spec verification

- **Result**: done
- **Summary**: Verified the previous iteration's work satisfies every done criterion. All plan checkboxes are `[x]`. `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go test -race ./...` are clean. `grep -E 'BCC_JSONL_PATH|JSONLDir|JSONLPath|rawLog|LogPath|log_path' --include='*.go'` against the tree returns no matches. The previous entry recorded `Result: ok`; with zero `[ ]` items remaining, the correct closing value is `done`.
- **Commits**: this commit `spec: close drop-raw-event-log spec`
- **Next**: none

### 2026-04-29 20:30, drop the per-iteration raw event log

- **Result**: ok
- **Summary**: Removed the `.bcc/logs/<slug>-iter<n>.jsonl` artifact and every plumbing point that existed only to support it. `internal/executor/claude/log.go` is gone (along with `rawLog`, `openRawLog`, `writeLine`, `writeInterruptedTerminator`); `internal/executor/claude/claude.go` no longer opens a raw log, no longer appends an `{"type":"interrupted"}` terminator, and `streamLines` parses straight off the pipe. `internal/executor/fake/fake.go` lost `Step.RawLog` and the `os.WriteFile(logPath, ...)` block. `internal/loop/loop.go` no longer carries `JSONLDir`, no longer creates `.bcc/logs/`, and no longer sets `BCC_JSONL_PATH` on the agent subprocess. `loop.ExecResult` now carries `ExitCode` only; `loop.IterationFinished` lost `LogPath`. NDJSON's `iter_finished` lost `log_path`; the text backend lost the `log_path` slog attr; the startup banner in `internal/cli/run.go` lists only the surviving `BCC_*` vars. `internal/cli/watch.go` and the `watch` subcommand registration are gone (no consumer; the planned watcher had moved into `bcc run`). Prompt template trimmed: `internal/loop/prompt.go` no longer mentions `BCC_JSONL_PATH` in the loop or single-shot env var lists, and the "Cite BCC_JSONL_PATH..." breadcrumb sentence is removed. Tests updated to match: `TestRun_RawLogWrittenAtBCCJSONLPath` deleted from both `internal/loop/loop_test.go` and `internal/executor/fake/fake_test.go`; `withLogPath` and every `BCC_JSONL_PATH` setenv / `LogPath` assertion removed from `internal/executor/claude/claude_test.go`; `JSONLDir: t.TempDir()` dropped from every loop and integration test; `internal/loop/eventjson_test.go` and `internal/cli/render_test.go` lose the `log_path` key in their byte-for-byte fixtures; `internal/loop/prompt_test.go` no longer asserts `BCC_JSONL_PATH` in the env var list. Documentation: `AGENTS.md` value-objects example loses `JSONLPath`; `docs/guides/autonomous-execution.md` env-var table loses the `BCC_JSONL_PATH` row; Phase 1 and Phase 2 spec bodies rewritten in place to describe the target state without raw logs (journal entries untouched); index lists this spec under Floating specs, removes the moot `bcc watch` PID-file open question, and rewrites Phase 0's `watch` subcommand bullet to list `run` and `init` only. Final state: `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test -race ./...` all clean; `bcc --help` lists `run` and `init`; `grep` for any of the six target tokens against `*.go` returns nothing.
- **Commits**: this commit `refac: drop the per-iteration raw event log`
- **Decisions**: The working tree was not clean at iteration start: most of the code surgery and a tiny unrelated heading-case fix in `docs/specs/buchecha-mvp/2026-04-29-spec-vendor-neutrality.md` (`## Implementation plan` → `## Implementation Plan`, aligning with the parser's canonical heading) were already staged by hand. Folded the heading fix into this commit rather than leaving the working tree dirty; it is a one-line correction, not a scope expansion. The Phase 1 spec item said "the architecture overview's mermaid diagram drops the JSONL artifact" but Phase 1 has no architecture section or mermaid diagram; verified the index.md mermaid (the only one in the initiative) already does not depict the JSONL artifact, so that part was a no-op. Left the `.gitignore` entry for `.bcc/` in place per the spec (it still covers `.bcc.toml` and any future per-project scratch).
- **Next**: none (floating spec; pull the next item from the MVP plan)
