---
title: "Phase 3: live steering of the agent"
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
  - phase-3
  - mvp
  - tui
  - executor
---

# Phase 3: live steering of the agent

## Summary

Allow the user to send mid-run user messages to the running agent from the `bcc run` TUI. Press `i`, type a message, hit enter; the message is delivered to the agent on the next safe boundary and the agent's response stream picks it up. Optional, capability-gated per executor adapter.

## Status

**Draft.** Open this spec in earnest only after Phase 2 ships and the per-adapter feasibility research below is complete.

## Context and motivation

Once Phase 2 makes the loop observable, the next pain is: "I can see the agent doing the wrong thing, but can't course-correct without killing it." A small steering channel from the user into the agent process closes that gap.

This is independent of pause/quit (Phase 2). Pause stops the loop boundary; steering inserts a user turn into the live agent conversation.

## Open research per adapter

| Adapter | Mechanism (hypothesis) | Risk |
|---|---|---|
| Claude Code | `--input-format stream-json` accepts JSON-encoded user-turn messages on stdin while the process runs | Unclear if mid-iteration injection is allowed or only between assistant turns |
| Codex | TBD; documentation review needed | Likely no streaming-input mode; may require killing and restarting with appended history |
| Gemini | TBD | Same |

If only Claude supports it natively, Phase 3 ships claude-only steering, with an interface contract that other adapters can opt into later.

## Design sketch

### Capability port

```go
package loop

type Steerable interface {
    SendUserMessage(ctx context.Context, text string) error
}
```

`Executor` adapters that implement `Steerable` advertise the capability. The TUI does a type assertion at startup; if the running adapter does not implement it, the steering UI is hidden.

### TUI surface

- `i` opens a textinput at the bottom of the screen.
- Enter sends the message via `Steerable.SendUserMessage`. Esc cancels.
- The sent message appears in the recent-actions panel as `→ user: "..."` so it is visible in the timeline.
- A queued indicator shows if the message is waiting for a turn boundary.

### Constraints

- Steering messages do not bypass the journal contract: the agent still must commit and write a `**Result**` entry. Steering can nudge the work, not transfer scope.
- Steering history is persisted in the per-iteration log (`.bcc/logs/<spec-slug>-<iter>.jsonl`) for audit.
- The NDJSON event stream from Phase 2 (`--output json`) gets a new `agent_event` kind `user_message` representing the injected turn, so machine consumers can observe steering too.

## Implementation Plan (placeholder)

1. [ ] Research per adapter (Claude, Codex, Gemini): document the actual mechanism each CLI offers for mid-run input.
1. [ ] Define `Steerable` port in `internal/loop`.
1. [ ] Implement `Steerable` in `internal/executor/claude/` (manage stdin pipe with stream-json input mode).
1. [ ] Add `KindUserMessage` to the agent-event taxonomy and propagate through the NDJSON serializer.
1. [ ] TUI textinput component, `i` key opens it, Enter dispatches via `Steerable`.
1. [ ] Recent-actions panel renders user-injected messages distinctly.
1. [ ] Tests: fake `Steerable` adapter receives the call; TUI integration via `tea.Test`.
1. [ ] Manual end-to-end: run a real spec, steer mid-iteration, observe agent behavior.

## Open questions

- [ ] Does steering apply mid-iteration or only between iterations? The latter is far simpler (just inject into the next prompt). Decision deferred to research.
- [ ] Should steering messages be replayable across restarts (i.e., persisted prompt addenda)?
- [ ] Rate-limit on user steering messages to avoid drowning the agent?
- [ ] Should `--output json` mode also accept inbound steering on stdin, so a parent `bcc` can steer a child `bcc`?

## References

- Phase 2 spec (live TUI): [2026-04-29-phase-2-tui-dashboard.md](./2026-04-29-phase-2-tui-dashboard.md)
- Claude Code CLI reference (`--input-format stream-json`)
