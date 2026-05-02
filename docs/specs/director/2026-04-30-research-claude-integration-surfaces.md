---
title: "Research: Claude Code integration surfaces"
type: reference
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
  - research
  - claude-code
  - vendor-claude
comments: true
---

# Research: Claude Code integration surfaces

## Purpose

When we derive specs from the Director PRDs, we will repeatedly ask: *can Claude Code do this for us, or do we have to build it ourselves?* This doc is the answer key. It catalogs the integration surfaces Claude Code exposes, with a per-surface mapping to the four Director PRDs so we know, before we write a spec, which features ride on Claude primitives and which we have to invent.

This is **Claude-specific**. Codex, Gemini, and future executor families will have their own surfaces, captured in their own reference docs. The Director's port shapes stay vendor-neutral. Adapters absorb the specifics.

This doc is **not normative**. It does not commit us to using any of these surfaces. It tells us what is on the table.

## How to use this doc

When writing a spec for any Director PRD:

1. Skim the PRD-to-surface map at the bottom to find the candidate surfaces.
1. Read the section for each candidate, including its caveats.
1. Decide explicitly: use the Claude surface (and document the dependency) or build vendor-neutral (and document the cost).

If a surface lands in production, link this doc from the spec's References.

---

## 1. Hooks

**What it is.** Shell-level event handlers that fire at 27+ Claude Code lifecycle points. Configured in JSON (`settings.json` at user/project/local scopes, or bundled with a plugin). Each hook receives a structured JSON payload on stdin and returns a JSON decision on stdout, optionally short-circuiting the next step.

### Key events

| Event | When |
|---|---|
| `SessionStart` | Session begins or resumes |
| `Setup` | One-time prep with `--init-only` or `-p --init/--maintenance` |
| `UserPromptSubmit` | Before Claude processes a user prompt |
| `PreToolUse` | Before any tool call (can allow / deny / defer / modify input) |
| `PostToolUse`, `PostToolUseFailure` | After a tool call resolves |
| `PostToolBatch` | After parallel tool calls resolve, before next model call |
| `PermissionRequest`, `PermissionDenied` | Permission dialog opens / denied call (can allow retry) |
| `Stop`, `StopFailure` | Claude finishes / API error |
| `SubagentStart`, `SubagentStop` | Subagent lifecycle |
| `TaskCreated`, `TaskCompleted` | TaskCreate / TaskUpdate lifecycle |
| `FileChanged` | Watched files change on disk |
| `WorktreeCreate`, `WorktreeRemove` | Git worktree lifecycle |
| `PreCompact`, `PostCompact` | Context compaction |
| `Elicitation`, `ElicitationResult` | MCP elicitation |
| `SessionEnd` | Session terminates |

### Hook types

`command` (shell), `http` (POST JSON to a URL), `mcp_tool` (call a tool on a connected MCP server), `prompt` (LLM evaluation, yes/no), `agent` (spawn a verification subagent).

### Decision powers

- **Block**: exit 2 or `decision: "block"` aborts the action with feedback to Claude.
- **Allow / deny / ask / defer** (PreToolUse): rich permission verdict per call.
- **Modify input** (`updatedInput`): the next step sees rewritten args.
- **Inject context**: stdout (or `additionalContext` in JSON output) gets prepended to the next model call.
- **Defer in `-p` mode**: PreToolUse returning `permissionDecision: "defer"` pauses Claude with `stop_reason: "tool_deferred"`, exposes `deferred_tool_use`, and lets the parent process resume via `claude -p --resume <session-id>` after handling the call externally. **This is the cleanest hook into our autonomous loop.**

### Implications per PRD

- **PRD 1 (validation gate).** A `SessionStart` hook can inject the validation report into Claude's context for the run. A `UserPromptSubmit` hook can block a run if the spec hash has not been validated, with `additionalContext` pointing at the report. Cheap, low-leverage gate.
- **PRD 2 (reviewed execution).** Several leverage points:
  - `Stop` hook is the natural place to enforce "do not finalize until Director approves". Exit 2 keeps Claude alive; the Director's verdict decides.
  - `PostToolBatch` is the right point for the Director to look at a phase's diff after the Executor's tool calls resolve, before the next model turn.
  - `PreToolUse` with `defer` lets us route specific tool calls (e.g., `git commit`) through the Director protocol externally and resume.
  - `TaskCreated` / `TaskCompleted` mirror the phase boundaries we model in the plan; reading them gives us "free" progress signals.
- **PRD 3 (parallel execution).** `WorktreeCreate` hook can replace default git worktree behavior (so bcc owns the path scheme), and `WorktreeRemove` is the reconciliation trigger. `--worktree` flag plus these hooks could remove a lot of the bespoke worktree code we would otherwise write.
- **PRD 4 (capability-aware).** Limited direct fit. `PostToolUse` on long-running tool calls could feed cost telemetry. Not a primary surface for this PRD.

### Caveats

- Hooks are *agent-side*: they run inside the Claude Code session's environment, not bcc's. Communication between hook and bcc happens via filesystem, sockets, or HTTP. Pick a contract early.
- Output capped at 10,000 chars; excess gets truncated.
- Hook scope (user / project / local / plugin / managed) determines security and shareability. A Director-driven hook that ships with a bcc plugin lives at plugin scope.
- The `defer` mechanism ties us to `-p` (print) mode, which the autonomous loop already uses.

---

## 2. Plugins

**What it is.** A self-contained directory of components that extends Claude Code: skills, agents, hooks, MCP servers, LSP servers, monitors, channels, themes, output styles, executables in `bin/`. Distributed via marketplaces (or `--plugin-dir` for local dev). Manifest is `.claude-plugin/plugin.json`, optional. Installed at user / project / local / managed scope.

### What a plugin can ship

- **Skills**: `/name` shortcuts (markdown with optional scripts).
- **Agents**: subagent definitions with frontmatter (name, description, model, effort, maxTurns, tools, disallowedTools, skills, memory, isolation: "worktree").
- **Hooks**: same surface as user hooks, but bundled.
- **MCP servers**: tools, resources, prompts, plus channel and elicitation extensions.
- **Monitors**: background commands whose stdout lines arrive as notifications.
- **Executables**: files in `bin/` get added to `PATH` for the Bash tool.
- **userConfig**: prompted at install time, available as `${user_config.KEY}` substitutions and `CLAUDE_PLUGIN_OPTION_<KEY>` env vars. Sensitive values go to keychain.
- **Channels**: declared in manifest, bound to a bundled MCP server. See section 3.
- **Persistent data**: `${CLAUDE_PLUGIN_DATA}` survives plugin updates; `${CLAUDE_PLUGIN_ROOT}` is the install dir.

### Implications per PRD

- **All PRDs.** A `bcc` plugin is the natural distribution vehicle for *every* piece of Claude-specific glue we need: hooks for lifecycle, MCP server for typed tools, monitors for log-following, channel for bidirectional protocol. One install command (`/plugin install bcc@<our-marketplace>`) gives a Claude user the full Director integration in one shot. Without a plugin, the user is hand-editing settings.json.
- **PRD 1.** A plugin can ship a `bcc-validate` skill (`/bcc-validate`) the user invokes manually, alongside the `bcc validate` CLI. The skill triggers the Director's validation pass against the current project's spec.
- **PRD 2.** The plugin's MCP server is the right home for the typed tool surface (`propose_review`, `propose_briefing`, `request_user_input`). Plugin-bundled hooks enforce the loop discipline.
- **PRD 3.** Plugin monitors can tail per-worktree logs and stream them to the orchestrating session. Plugin-bundled `WorktreeCreate` and `WorktreeRemove` hooks own the worktree lifecycle.
- **PRD 4.** A plugin can declare `userConfig` for `max_cost_tier` and similar Director knobs. Sensitive values (API keys for non-default vendors, if any) go to keychain.

### Caveats

- Plugins are Claude-only. The bcc product is multi-vendor. The plugin is one **adapter-side** distribution, not a substitute for the binary. We will need analogous distributions for codex/gemini.
- Plugins cannot reference paths outside their install dir. Use symlinks if needed.
- Plugin agents cannot ship their own hooks, MCP servers, or permissionMode (security carve-out).

---

## 3. Channels (the high-leverage one)

**Status (2026-05-02): rejected as Director transport.** A PoC validated the wire protocol works end to end (initialize handshake, `notifications/claude/channel`, reply tool round-trip), but four blockers make the surface unusable for bcc today: (1) custom channels need `--dangerously-load-development-channels` with an interactive confirmation prompt and no non-interactive bypass; (2) channels require claude.ai login, so users on `ANTHROPIC_API_KEY` (CI, headless servers) are locked out; (3) in `claude -p` mode, channel notifications never reach the model context (verified via cache-token diff across turns), so headless adapter use is impossible; (4) forcing TTY/PTY wrapping just to deliver events contradicts the bcc loop shape. Re-evaluate when channels exit research preview, gain API-key auth, expose a non-interactive confirmation flag, and deliver notifications in `-p`. Until then, the implications below are recorded for future reference, not as candidate dependencies.

**What it is.** An MCP server that **pushes events into a Claude Code session** so the agent can react to things outside the terminal. Built on standard MCP stdio transport, with two Claude-specific capabilities: `claude/channel` (notification receiver) and `claude/channel/permission` (remote permission relay). One-way channels deliver alerts; two-way channels add a reply tool so Claude can talk back.

This is the surface that maps cleanest to the Director protocol the user described as *"íntimo, como MCP/tool calling"*. It is the most powerful of the five surfaces for our use case.

### How it works

1. Channel server is an MCP server with `capabilities.experimental['claude/channel'] = {}`.
2. Server emits `notifications/claude/channel` with `{ content, meta }`. Claude receives it as a `<channel source="..." key="value">body</channel>` tag in context.
3. Server's `instructions` tell Claude what to do with these tags.
4. For two-way: server registers a tool (e.g., `reply`); Claude calls it when it has something to send back. The tool implementation pushes via the platform-specific outbound (HTTP, chat API, etc.).

### Permission relay (the hidden gem)

When Claude calls a permission-gated tool (Bash, Write, Edit), Claude Code can forward the prompt to a channel server in parallel with the local terminal dialog. The remote can answer first, the dialog closes, the call proceeds. Both sides race; first verdict wins.

- Capability: `claude/channel/permission` opt-in.
- Outbound: `notifications/claude/channel/permission_request` with `request_id`, `tool_name`, `description`, `input_preview`.
- Inbound: server emits `notifications/claude/channel/permission` with `request_id` and `behavior: "allow" | "deny"`.
- Sender gating is mandatory: anyone who can reply to your channel can approve tool use. The docs explicitly require an authenticated allowlist before declaring this capability.

### Implications per PRD (this is where the value concentrates)

- **PRD 2 (reviewed execution).** This is the answer to "how does the Director communicate with the Executor session". Concretely:
  - bcc spawns an MCP **bcc-channel** server alongside each Executor session. The Director runs in bcc; its decisions flow through the channel as `<channel>` tags.
  - **Briefing delivery**: at phase start, bcc pushes a channel event with the typed `Briefing` payload (acceptance criteria, scope, retry feedback). The Executor reads it from its context and proceeds.
  - **Verdict feedback**: at phase end, after the Director runs its review, bcc pushes a verdict event. On `revise`, the next briefing arrives via the same channel.
  - **Reply tool**: the Executor calls a `bcc.signal_phase_complete(phase_id, summary)` tool to mark itself done; bcc routes that to the Director for review. Same shape for `bcc.request_clarification(question)` and similar.
  - **Permission relay** is the right abstraction for "the Director is the authority on tool use, not blanket `--dangerously-skip-permissions`". The Director can approve scoped operations (within the phase's `scope_in`) and deny out-of-scope edits, surfacing the boundary as concrete approve/deny verdicts. This is strictly stronger than the current binary autonomy mode.
- **PRD 1.** The validation report can arrive at session start as a `<channel>` event so the Executor reads its own validation context. Lower priority than for PRD 2 but a clean entry point.
- **PRD 3 (parallel execution).** Each parallel worktree has its own Executor session. We attach one channel per session. The Director coordinates them all from bcc, pushing per-session briefings and listening for completions.
- **PRD 4 (capability-aware).** Channels do not directly drive model assignment, but a channel-based `bcc.suggest_assignment_change(reason)` reply tool lets the Executor signal "this phase is harder than briefed" so the Director can re-assign on retry.

### Caveats

- **Research preview.** Requires Claude Code v2.1.80+. Custom channels need `--dangerously-load-development-channels` until they pass Anthropic's allowlist or are admin-allowlisted on Team/Enterprise. Production-readiness is not yet there; the protocol is stable enough to design against, but operational readiness is unconfirmed.
- **Claude.ai authentication required.** Console / API-key auth is not supported. This is a non-trivial constraint for users who run Claude Code via API key in CI.
- **Sender gating is mandatory** for permission relay. We need an authenticated path between bcc and the Claude session. Local stdio (the same machine, our process spawning the channel server) is the simplest; the docs explicitly support that pattern.
- **Vendor-locked.** The channel mechanism is Claude-specific. Our Director protocol must work over a vendor-neutral abstraction; channels are the **claude adapter** of that abstraction. Codex/Gemini will need their own bridges (likely simpler, with the price of less integration).

---

## 4. Tools

**What it is.** The set of capabilities Claude can invoke in a session. Built-ins (Bash, Read, Edit, Write, Glob, Grep, Agent, Task*, Worktree*, Monitor, etc.) plus anything an MCP server registers. Permissions are tool-specific.

### Surfaces relevant to us

- **MCP tools** are the official way to add custom tools. A bcc plugin (section 2) can ship an MCP server that exposes Director-protocol tools. `--mcp-config`, `--strict-mcp-config` control loading.
- **Monitor tool**: runs a command in the background, streams stdout lines back as notifications. Plugin-declared monitors auto-start. Same permission rules as Bash. Useful for **log-following from the Executor's session into the Director's view** without the Director needing to poll.
- **Task* tools**: `TaskCreate`, `TaskUpdate`, `TaskList`, `TaskGet` are first-class. The Executor already manages a session task checklist with these. The Director can read it for verdict context.
- **EnterWorktree / ExitWorktree / `--worktree`**: built-in worktree management Claude can drive itself, removing boilerplate from PRD 3 if we accept the Claude-specific dependency.
- **Agent tool**: spawns a subagent with its own context. Relevant if we ever want a sub-Director (a subagent that drills into a specific phase's review). Not in scope for the current PRDs.
- **`--tools`, `--allowedTools`, `--disallowedTools`**: per-session tool scoping.

### Implications per PRD

- **PRD 2.** Custom MCP tools are the home for the typed Director protocol surface (`propose_review`, etc., from the initiative doc). Channel reply tools (section 3) and MCP tools coexist; channels carry the conversational injection, MCP tools carry the structured data.
- **PRD 2 (audit).** `TaskList` polled by the Director (or surfaced via a hook) gives us a free "Executor's view of progress" alongside the journal.
- **PRD 3.** `--worktree` plus EnterWorktree/ExitWorktree means we may not need to shell out to git worktree ourselves on the Claude path. Trade-off: it ties parallel mode tighter to the adapter.
- **PRD 4.** The `Bash` tool's permission gating combined with the channel permission relay is the substrate for fine-grained, Director-authored permissions per phase. PRD 4 defines the model assignment; the **scope** of allowed tool calls per phase is a permissioning concern that this gives us.

### Caveats

- Plugin MCP servers run as subprocesses; their lifecycle is the session's lifecycle. Long-lived shared state across phases lives in `${CLAUDE_PLUGIN_DATA}` or in bcc's own state, not in the MCP server's memory.
- Restricting tools via `--tools` is per-session; if the Director assigns different scopes per phase, each phase needs its own session invocation with the right `--tools` value (which the loop already does today).

---

## 5. CLI reference

**What it is.** The set of flags `claude` accepts. Most of what bcc invokes today goes through these. They are the primary control surface for parameterizing each Executor session, and several flags are direct enablers for our PRDs.

### High-leverage flags for the Director

| Flag | Why it matters |
|---|---|
| `-p` / `--print` | The autonomous mode; the foundation of the loop. Director runs in `-p` per phase. |
| `--session-id <uuid>` | Lets bcc set a stable session ID per phase, useful for resuming after `defer` (hooks section). |
| `--max-turns <n>` | Bounds runaway phases. Errors out at the limit; bcc can read that and escalate. |
| `--max-budget-usd <n>` | **Direct PRD 4 lever.** Per-session cost cap, enforced by Claude Code itself. The Director's `max_cost_tier` config caps this flag. |
| `--model <name>` | **Direct PRD 4 lever.** Per-session model selection. The Director's `ExecutorAssignment.model_tier` translates to this. |
| `--effort <level>` | **Direct PRD 4 lever.** Per-session reasoning effort. The Director's `ExecutorAssignment.effort` translates to this. |
| `--system-prompt` / `--append-system-prompt[-file]` | The Director-authored briefing for each phase ships as an appended system prompt (or a file). |
| `--input-format stream-json` / `--output-format stream-json` | Structured I/O instead of plain text. The basis for typed Executor reporting. |
| `--include-hook-events`, `--include-partial-messages` | Richer event stream when bcc consumes Claude's stdout. |
| `--json-schema <schema>` | **Direct PRD 1 lever.** Validates the agent's output against a JSON Schema. The Director's typed payloads (`ValidationReport`, `Plan`, `Verdict`) ride on this. |
| `--mcp-config <file>` / `--strict-mcp-config` | Scope MCP servers per session. The Director controls which servers each phase sees. |
| `--allowedTools` / `--disallowedTools` / `--tools` | Per-session tool scope, the substrate for phase-scoped permissions. |
| `--bare` | Skip auto-discovery of hooks, skills, plugins, MCP, memory, CLAUDE.md. Faster scripted calls. Useful for the cheapest Director-internal calls (validation, review) where context is in the prompt, not the environment. |
| `--exclude-dynamic-system-prompt-sections` | Moves per-machine prompt content into the first user message. **Improves prompt-cache reuse across iterations and machines.** Direct cost optimization for PRD 4. |
| `--worktree <name>` | Built-in worktree creation. PRD 3 enabler. |
| `--permission-prompt-tool <mcp-tool>` | Routes permission prompts to an MCP tool in non-interactive mode. **The non-channel alternative for PRD 2's permission relay**: works without claude.ai auth or research-preview gates. |
| `--dangerously-skip-permissions` | The current autonomous mode. Once we have `--permission-prompt-tool` or channel permission relay, this becomes a fallback rather than the default. |
| `--fork-session` | When resuming, branches off a new session ID. Useful for retries that should not pollute the original session record. |
| `--agents '<json>'` | Define subagents inline via JSON. Could be used to ship the Director itself as an agent definition rather than as bcc-side state. |
| `--no-session-persistence` | Don't save to disk. Useful for the cheapest internal Director calls (validation, review) where we do not want lingering session files. |

### Implications per PRD

- **PRD 1.** `-p --json-schema --no-session-persistence --bare` is roughly the validation call shape. Cheapest possible Director invocation.
- **PRD 2.** Each phase = one `claude -p` invocation with `--system-prompt-file <briefing>`, `--max-turns N`, `--max-budget-usd X`, `--mcp-config bcc-channel`, `--permission-prompt-tool` (or channel relay). On `revise`, `--session-id <stable-id>` keeps the audit trail; alternatively `--fork-session` splits attempts.
- **PRD 3.** `--worktree <name>` per parallel phase. The bcc loop is the same; only the cwd and worktree flag change.
- **PRD 4.** `--model` and `--effort` are the direct translation targets for the `ExecutorAssignment`. `--max-budget-usd` is the cost-cap enforcement. `--exclude-dynamic-system-prompt-sections` is a free cache-hit win when the same Director runs across many machines or specs.

### Caveats

- The CLI is the **claude adapter's contract** with Claude Code. Other adapters have entirely different shapes. Decisions like "the briefing is a system prompt" must live in the adapter's translation layer, not in the Director or in bcc's loop.
- Some flags interact (`--json-schema` requires `-p`; `--include-hook-events` requires `--output-format stream-json`). Adapters bear this complexity so the Director does not.
- `--max-budget-usd` is print-mode only. In any non-print context the cost cap has to come from outside.

---

## Cross-cutting opportunities

Mapping the surfaces back to our four PRDs, the high-value bets are:

| PRD | Primary surface(s) | Why |
|---|---|---|
| PRD 1 (validation gate) | CLI (`--json-schema`, `--bare`, `--no-session-persistence`) | Cheap, isolated; no event-loop integration needed |
| PRD 2 (reviewed execution) | **Channels** + Hooks (`Stop`, `PostToolBatch`, `PreToolUse`/defer) + CLI (`--permission-prompt-tool`, system-prompt flags) + Plugin (distribution) | The richest interlock; channels carry the protocol, hooks enforce loop discipline, CLI parameterizes each session, plugin packages it |
| PRD 3 (parallel execution) | CLI (`--worktree`) + Hooks (`WorktreeCreate`, `WorktreeRemove`) + Plugin monitors (per-worktree logs) | Native worktree primitives plus hook-driven lifecycle replace bespoke shelling |
| PRD 4 (capability-aware) | CLI (`--model`, `--effort`, `--max-budget-usd`, `--exclude-dynamic-system-prompt-sections`) | The full assignment translation lands here; the registry is bcc-side metadata |

**Channels are the single highest-leverage surface for the Director hypothesis.** They give us a typed bidirectional protocol, permission relay (a clean replacement for `--dangerously-skip-permissions`), and an MCP-shaped contract. The cost is a research-preview status and a Claude.ai-auth dependency we should plan around.

**Plugins are the highest-leverage distribution surface.** A `bcc` plugin bundles every Claude-specific piece (hooks, MCP server, channel, monitors, skills) into one install command. Without it, every Claude user wires this up by hand.

## What Claude Code does not give us

So we are clear-eyed about what the Director still owns:

1. **The plan as a typed DAG.** Claude Code has `TaskCreate`/`TaskUpdate`, but those are session-local and shape-poor. The canonical plan with dependencies, acceptance, and assignments lives in bcc.
1. **The capability registry.** Each adapter publishes its registry; Claude Code does not have a portable "list models with metadata" surface. Static, in-binary registries are still our job.
1. **Cross-session state.** Briefings, verdicts, plan persistence, `--resume` recovery: bcc owns these. `${CLAUDE_PLUGIN_DATA}` exists but is plugin-scoped, Claude-specific, and not cross-session in the bcc sense.
1. **The decider.** Continue / stop / escalate logic is bcc's domain; Claude has no equivalent abstraction.
1. **Vendor neutrality itself.** Every surface in this doc is Claude-only. Codex and Gemini will deliver some equivalents and skip others. The Director's port shapes have to absorb that asymmetry.
1. **The TUI.** Claude Code has its own TUI; bcc's lives outside the agent's session and aggregates across sessions and worktrees.

## Operational notes

- **Versioning.** Channels need v2.1.80+, Monitor tool v2.1.98+, plugin monitors v2.1.105+, permission relay v2.1.81+. When a Director feature depends on a Claude version, the adapter declares the minimum and bcc surfaces it at startup.
- **Authentication.** Channels require Claude.ai login. Other surfaces work with API key. PRD 2's channel-based protocol therefore cannot be the only path for permission relay; we need `--permission-prompt-tool` as a fallback for API-key users.
- **Telemetry.** `DISABLE_TELEMETRY` and `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` disable the Monitor tool. Plugins and our reference docs need to flag that.
- **Org policy.** Team and Enterprise admins can disable channels (`channelsEnabled`), restrict allowed channel plugins (`allowedChannelPlugins`), and force `allowManagedHoursOnly` for hooks. The Director path must degrade gracefully when a feature is policy-blocked.

## References

- [Hooks reference](https://code.claude.com/docs/en/hooks)
- [Plugins reference](https://code.claude.com/docs/en/plugins-reference)
- [Channels reference](https://code.claude.com/docs/en/channels-reference)
- [Tools reference](https://code.claude.com/docs/en/tools-reference)
- [CLI reference](https://code.claude.com/docs/en/cli-reference)
- [Initiative index](./index.md)
- [PRD 1: Spec validation gate](./2026-04-30-spec-validation-gate.md)
- [PRD 2: Reviewed execution](./2026-04-30-reviewed-execution.md)
- [PRD 3: Parallel phase execution](./2026-04-30-parallel-phase-execution.md)
- [PRD 4: Capability-aware execution](./2026-04-30-capability-aware-execution.md)
