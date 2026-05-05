# Surface coverage

The table below maps bcc user-facing capabilities to the TUI and WebUI surfaces. V1 covers read-only inspection; V2+ will add write and mutation capabilities as specified in the linked PRDs.

| Capability | TUI | WebUI | Notes |
|---|---|---|---|
| **V1: Read inspection** |
| Live DAG view | check | check | Interactive phase/task graph with status colors. |
| Activity Gantt | check | check | Horizontal timeline showing task execution and retry boundaries. |
| Iteration timeline | check | check | Event stream log grouped by iteration, sorted newest first. |
| Per-phase briefing | check | check | Markdown rendering of the Briefer's output for the phase. |
| Per-role prompt | check | check | Markdown rendering of the system prompt sent to each role. |
| Sessions sidebar | check | check | List of live and archived sessions with status and iteration count. |
| Escalation gate | check | check | Visual indication when a phase is blocked awaiting user decision. |
| Director status | check | check | Current phase, task, attempt, and role in the loop. |
| Abort run | check | check | Signal to cleanly terminate the run. |
| **V2: Write and mutations** |
| Task approval | dash | TBD | Executor submits task outcomes; user approves or rejects. |
| Task rejection | dash | TBD | Return a task to needs_fix with per-task feedback. |
| Escalation reply | dash | TBD | User submits decision at an escalation gate. |
| Phase skip | dash | TBD | User-initiated skip of a phase and its downstream phases. |
| **V3+: Extended manipulation** |
| Task editing | dash | TBD | Edit task description, dependencies, or acceptance criteria mid-run. |
| Prompt override | dash | TBD | Override system prompts per role for the next iteration. |
| Replan from here | dash | TBD | Trigger a re-plan of downstream phases from the current state. |
| Session management | dash | TBD | Archive, delete, or export session state and artifacts. |

## Legend

- **check**: capability fully implemented and stable on this surface.
- **dash**: capability not yet implemented or deferred to a later release.
- **TBD**: capability planned but awaiting API endpoint and/or UI design.

## References

- Refer to `docs/specs/api/2026-05-04-http-api.md` for the read-only V1 API endpoints and the V2+ mutating endpoints under development.
- Refer to `docs/specs/webui/2026-05-04-embedded-web-dashboard.md` for the SPA architecture and planned dashboard panels.
