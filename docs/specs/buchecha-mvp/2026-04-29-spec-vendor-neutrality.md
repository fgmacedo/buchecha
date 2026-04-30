---
title: "Spec-format vendor neutrality"
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
  - ports
---

# Spec-format vendor neutrality

## Summary

`bcc` is an orchestrator, not a spec-format authority. Today the framework is silently coupled to one specific markdown layout, the bcc-markdown convention with `Implementation Plan` checkboxes and `## Execution Journal` Result blocks. This spec carves the vocabulary of that layout out of the domain behind ports that speak in **signals** instead of content, so other formats (OpenSpec, Kiro, spec-kit, BMAD, Ralph Loop, custom in-house) are supported by writing an adapter rather than forking the framework.

It also retires two latent assumptions: that the agent's operating contract lives at a fixed path inside the user's project (`docs/guides/autonomous-execution.md`), and that progress is only discoverable by re-parsing the spec file. The contract becomes per-adapter content embedded in the bcc binary, and a small wire protocol of `bcc_event` JSONL lines lets agents emit progress in real time. Journal storage is independent of spec content, so it can live in the spec file (current default), in a sibling file, in a database, or in an external system, configurable via `.bcc.toml`. The bcc-markdown format remains the out-of-the-box default; observable behavior for existing specs does not change.

## Status

**Draft.** Not numbered as a phase: priority is fluid relative to the existing P3, P4, P5 work and may shift as the dogfooding feedback comes in. Pull this spec forward when (a) we want to run `bcc` on a project that uses a non-bcc-markdown spec format, or (b) we add a feature that would otherwise deepen the coupling.

## Context and motivation

`internal/spec/` is treated as a domain layer (per `CLAUDE.md`'s hexagonal model), but it is in practice the **vocabulary of one parser**: `Plan`, `Phase`, `Item`, `Result` (enum: ok/partial/done/review/blocked), `JournalEntry`, `LatestResult`. These types reflect the bcc-markdown convention; they are not neutral concepts.

### Coupling inventory

Ten files outside `internal/spec/` import it. They use only four symbols (`Result`, `ResultVocab`, `Plan`, `LatestResult`) plus the parser entry points (`ParsePlan`, `ParseLatestResult`):

| File | Purpose |
|---|---|
| `internal/loop/decider.go:3` | reads `Result` enum to decide continue/stop |
| `internal/loop/events.go:6` | `Result` field on `IterationFinished` event |
| `internal/loop/loop.go:13` | `ParsePlan`, `ParseLatestResult`, `ResultVocab` for prompt and decision |
| `internal/cli/run.go:24` | `ResultVocab` and `ParseLatestResult` for output rendering |
| `internal/cli/render_test.go:10` | test fixture using `ResultDone` |
| `internal/tui/ports.go:6` | `ResultVocab` field on TUI config |
| `internal/tui/progress.go:10` | `Plan` for "X/Y items" panel |
| `internal/tui/risk.go:9` | `Result`, `LatestResult` for risk panel |
| `internal/tui/session.go:14` | `LatestResult` for session state |
| `internal/tui/tui.go:22` | `ResultVocab` on session config |

The boundary is at the right place. The problem is what is on each side: a "domain" layer that is actually a markdown parser, and consumers that depend on parser-shaped types instead of decision-shaped signals.

### The agent-contract leak

`internal/loop/loop.go:81` defaults `PromptInput.GuidePath` to the literal string `"docs/guides/autonomous-execution.md"`. `internal/loop/prompt.go:36` and `internal/loop/prompt.go:78` inject that path into the agent prompt; `internal/loop/prompt.go:46` instructs the agent "read the autonomous-execution guide". The file is **not** embedded and **not** loaded by bcc at runtime; the agent reads it from its own working directory.

The leak runs deeper than "the file might not exist". `docs/` is user space: project documentation, design notes, style guides, `AGENTS.md`, `CLAUDE.md`, custom skills. The framework cannot read user space at runtime, and cannot defensively shield itself against the noise that lives there. Even when the file **does** exist (as in this repo), bcc has no business shipping a user-space path into its prompt as if it were a framework asset. The fix is structural, not cosmetic: the contract belongs inside `internal/`, owned by the framework. See [Framework and user-space boundary](#framework-and-user-space-boundary).

### Today's `SpecReader` is content-shaped

`internal/loop/ports.go` defines:

```go
type SpecReader interface {
    Read(path string) (string, error)
}
```

That contract returns raw markdown. The loop and TUI then re-parse it through `internal/spec/`. The port is shaped around content, not signals. The journal has no port at all: it is parsed back from the same markdown file as an appendix; agents rewrite the file to append entries.

The user-facing problem: every new spec format requires a fork of `internal/spec/` and edits across `loop/`, `tui/`, `cli/`, and the prompt template. That violates the "replace one adapter, touch one package" rule from `CLAUDE.md`.

## Spec ecosystems

The design must survive contact with formats that exist today, not only with bcc-markdown rewritten in another language. Each entry below lists what the format does and what that teaches the port shape.

1. **bcc-markdown (current).** Plan and journal share one markdown file. Tasks are `[ ]` checkboxes under an `## Implementation Plan` heading; iteration outcome is a `**Result**:` line inside an entry under `## Execution Journal`. Strength: a single file, trivial to share and review. Limitation: opinionated about Markdown headings and journal shape. **Teaches:** in-spec journal is a viable default but cannot be the only journal storage.

1. **OpenSpec** ([repo](https://github.com/Fission-AI/OpenSpec)). A change is three files: `proposal.md` (rationale), `design.md` (architecture), `tasks.md` (operational checklist). Vocabulary clash: their `specs/` directory holds current-system behavior and `changes/` holds proposals; `bcc`'s `specs/` plays the role of their `changes/`. **Teaches:** the unit `bcc` cares about is the operational checklist, not the narrative; an adapter must point the loop at `tasks.md` while the prompt still surfaces `proposal.md` and `design.md` as context.

1. **Kiro** ([docs](https://kiro.dev/docs/specs/)). Workflow `requirements → design → tasks`; tasks live in their own file; the IDE shows live progress. **Teaches:** "tasks file" is a recurring pattern across modern frameworks; "where do tasks live" is a per-format question, decoupled from "where does narrative live".

1. **Ralph Loop** ([repo](https://github.com/snarktank/ralph)). `tasks.json` with explicit dependencies; the agent picks the first task whose deps are satisfied. **Teaches:** task selection (which work item is next?) is a per-format concern that may involve graph reasoning; the port must let the adapter answer "next item" without `bcc` knowing the format's selection rules.

1. **BMAD, spec-kit, others.** Variants on the same shape: a structured artifact captures intent, a structured artifact captures progress, the agent acts on both. The exact files and headings differ; the loop semantics ("pick a pending unit, work it, record outcome") do not.

Design implication: bcc never parses the user's spec or journal. The agent reads, the agent reports state on the wire (`bcc_event` JSONL sentinels). Format adapters live entirely in two seams: the prompt that goes out (`AgentBriefing`) and the events that come back (`AgentEvents`). Format-specific data structures stay inside the adapter; nothing format-shaped crosses into the loop or the TUI.

## Goal shape

Three ports total. Two are existing infrastructure (`Executor`, `GitProbe`); one is the new format seam (`AgentBriefing`). The wire protocol (canonical bcc-level contract with any agent regardless of format) lives in its own package, `internal/loop/agentcontract/`, alongside the markdown blocks every format adapter composes. bcc never reads the user's spec, journal, or any other user-space artifact at runtime. The agent reads the spec, the agent reports progress on the wire, bcc orchestrates and renders.

### Port: `Executor`

Existing port. Runs the configured agent subprocess against a prompt and emits a stream of normalized `AgentEvent`s on the events channel. Cancellable via `ctx`. The executor calls `agentcontract.ParseLine` on lines its native parser does not recognize, so `bcc_event` sentinels round-trip into the loop's event channel.

### Port: `GitProbe`

Existing port. Read-only view of the working tree (`HeadSHA`, `CurrentBranch`, `IsClean`). Unchanged.

### Port: `AgentBriefing` (prompt-side)

The active spec-format adapter implements it; the loop calls it once per iteration to get the prompt string.

```go
type AgentBriefing interface {
    BuildPrompt(ctx context.Context, in BriefingInput) (string, error)
}

type BriefingInput struct {
    SpecPath  string
    Iteration int
    Mode      Mode   // ModeLoop | ModeSingleShot
    Extra     string // user-provided extra instructions, optional
}
```

The adapter owns the format-specific portion of the contract (procedure, scope discipline, journal shape, stop conditions) as a Go `text/template` and **composes** the final prompt by extending `agentcontract.Partials()` and invoking the shared blocks via `{{template "wire_protocol" .}}`, `{{template "absolute_restrictions" .}}`, `{{template "working_tree" .}}`. The contract instructs the agent to read the spec from `SpecPath` itself; bcc does not interpolate spec content into the prompt. The composed contract is assertive enough to dominate user-space noise (`CLAUDE.md`, `AGENTS.md`, custom skills).

### Subsystem: `internal/loop/agentcontract/` (wire protocol + shared markdown blocks)

The wire protocol is bcc's canonical contract with any agent: same JSON shape, same fixed English values (`continue`/`review`/`done`/`blocked`), regardless of which spec format the user picked. Code that defines the wire types and the markdown that documents them live together so they evolve in lockstep:

```go
type Signal int  // SignalUnknown | SignalContinue | SignalReview | SignalDone | SignalBlocked
type BccEventKind int  // BccEventTaskStarted | BccEventTaskCompleted | BccEventIterationResult | BccEventProgressTick
type BccEvent struct { Kind BccEventKind; ID string; Signal Signal; Summary string; Raw map[string]any }

func ParseLine(line []byte) (BccEvent, bool)
func Partials() *template.Template
```

`Partials()` returns a template containing the shared markdown blocks every adapter composes:

- `wire_protocol`: the bcc_event JSON shape and value vocabulary (see [Wire protocol](#wire-protocol-shape)).
- `absolute_restrictions`: bcc-level safety rules no instruction may relax.
- `working_tree`: clean-enter / clean-exit, branch naming, commit style.

Format adapters parse their own contract template on top of `Partials()` and invoke the blocks via `{{template "wire_protocol" .}}` etc. The wire protocol is **never** redefined by an adapter: there is exactly one wire shape, one parser, one block of canonical text describing it.

#### Wire protocol shape

```jsonc
{"type":"bcc_event","event":"task_started","id":"P1.2","summary":"..."}
{"type":"bcc_event","event":"task_completed","id":"P1.2"}
{"type":"bcc_event","event":"iteration_result","value":"continue","summary":"..."}
```

`iteration_result.value` is the canonical English signal. The agent emits `iteration_result` exactly once per iteration, immediately before exit. A missing or malformed `iteration_result` causes bcc to exit invalid.

### Format adapters

- `internal/format/markdown_bcc/`: implements `AgentBriefing`. Owns `contract.md` (`//go:embed`) and per-format `Config` (heading text, journal store choice, etc.). Composes its template with `agentcontract.Partials()`.
- `internal/format/openspec/`, `.../kiro/`, `.../speckit/`, `.../bmad/`: future siblings. Each one ships its own `contract.md`; none of them re-implement the wire protocol.

The directory is `internal/format/`, not `internal/specreader/`: bcc does not read the spec, the package is the format adapter (prompt only). There is no `internal/spec/` package, no parser helpers, no `internal/journal/` tree, no per-format event parser. `[journal].store` is **prompt input** for `AgentBriefing`: the active format adapter consumes it to choose which journal-writing fragment its template injects ("append to spec", "write to sidecar at `<path>`", "skip"). bcc never reads the journal, the user reads it in their editor.

### `.bcc.toml` shape

Hierarchical: a global section per concern names the active adapter; per-adapter subtables hold that adapter's options. Multiple adapters live side by side; switching is one key change in the global section, not a rewrite.

```toml
[spec]
format = "markdown_bcc"      # active format; markdown_bcc | openspec | kiro | speckit | bmad

[spec.markdown_bcc]
plan_heading    = "Implementation Plan"
journal_heading = "Execution Journal"
# All fields are template inputs to the AgentBriefing prompt: bcc does
# not parse the spec, so localizing the headings only changes what the
# agent is told to write. Wire protocol stays in canonical English.

[spec.openspec]
tasks_path    = "tasks.md"
proposal_path = "proposal.md"
design_path   = "design.md"

[spec.kiro]
tasks_path        = "tasks.md"
requirements_path = "requirements.md"
design_path       = "design.md"

[journal]
store = "markdown_inspec"    # active store; markdown_inspec | file | sqlite | none

[journal.markdown_inspec]
# no options today; section reserved for forward-compat

[journal.file]
path = ""                    # required when [journal].store = "file"

[journal.none]
# no options; agent skips journal writes entirely

[agent]
name = "claude"              # active agent; claude | codex | gemini

[agent.claude]
skip_permissions = true
extra_args       = []

[agent.codex]
# defaults populated by `bcc init` even when not active
```

Rules:

1. The global selectors (`[spec].format`, `[journal].store`, `[agent].name`) name which subtable is currently active. Other subtables stay valid and untouched; switching is one line.
1. Per-adapter subtables hold only that adapter's options. Adapters validate their own subtable at startup and fail fast on unknown keys.
1. `bcc init` writes sane defaults for **every** known adapter, not only the chosen one. A user who later runs `bcc run --spec-format openspec` does not need to re-edit `.bcc.toml`.
1. `format = "auto"` (and the agent equivalent) is explicitly **not** offered. The value must name a known adapter.
1. CLI flags `--spec-format <name>` and `--agent <name>` override the active selectors for one run; they never modify the file.

## Discovery strategy

bcc's only signal channel is the wire protocol. The agent emits `bcc_event` JSONL sentinels on stdout; bcc's executor adapter routes unknown lines through the active spec adapter's `AgentEvents.ParseLine`. The loop, the TUI panels, and the decision table all consume `BccEvent`s; nothing parses the spec or the journal.

Failure modes:

1. **Agent does not emit `iteration_result`.** Loop receives `SignalUnknown`, exits invalid. The agent contract makes this a hard requirement; missing it is a contract violation, not a soft state.
1. **Agent emits a malformed event.** Adapter's `ParseLine` returns `(BccEvent{}, false)`; the line falls through to existing handling. The event is effectively absent; same fate as case 1 if it was the iteration's `iteration_result`.
1. **Agent lies (`value=done` while items pending).** bcc trusts the wire signal. The agent's contract treats this as a contract violation the user catches when they review the spec; bcc does not double-check by parsing.

The HEAD-stuck check (no commit during an iteration) is the orthogonal safety net: if the agent did nothing, it surfaces regardless of what the wire protocol said.

## Framework and user-space boundary

`internal/` is framework space. `docs/` is user space. bcc reads from the former at runtime; it never reads from the latter.

Framework prompts live under `internal/format/<name>/` (and similarly for any other framework-owned text), embedded via `//go:embed`. They are Go `text/template`s where customization is supported. Substitutions provided by the active config include `{{.PlanHeading}}`, `{{.JournalHeading}}`, `{{.JournalStore}}`, `{{.SpecPath}}`, etc.; conditional sections such as `{{if eq .JournalStore "none"}}...{{end}}` cover features that toggle by config.

The user's project `docs/`, `CLAUDE.md`, `AGENTS.md`, and agent skills are theirs. They may inject content the agent reads, but bcc neither depends on them nor defends against them. The framework prompt is **assertive**: a clear, prescriptive contract with the agent that defines required behavior regardless of surrounding noise. Tone is rule-based and unambiguous, not advisory. Where user-space content could plausibly conflict (custom commit-message conventions, alternate review workflows), the prompt names the conflict and tells the agent how to resolve it (e.g., "follow the project's convention if visible from `git log`, otherwise use the lowercase prefix from this contract").

Consequence for `docs/guides/autonomous-execution.md`: the file is removed; its contents split into three artifacts per [Retiring the legacy guide](#retiring-the-legacy-guide).

## Retiring the legacy guide

Today's `docs/guides/autonomous-execution.md` is doing three jobs at once. Splitting them lands cleanly under the boundary above:

1. **Framework agent contract.** The "Operating mode", "Absolute restrictions", "Execution Journal", "Done criteria", and "Stop criteria" portions move to `internal/format/markdown_bcc/contract.md`. Embedded via `//go:embed`. Templated where config substitution applies.
1. **CLI documentation.** "How to invoke", "Command line", "BCC_* env vars in the agent subprocess", and "When not to use" stay as user-facing documentation, but live in bcc's `--help` output and a small README. They are not part of the agent prompt.
1. **Spec-authoring guidance.** Anything resembling "how to write a spec that the agent can execute" belongs in [Skill: fast-iteration spec authoring](./2026-04-29-skill-spec-authoring.md), per the existing skill design.

Once the three artifacts are in place, `docs/guides/autonomous-execution.md` is deleted.

## bcc-markdown contract

### Why the contract changes

With the `bcc_event` wire protocol and the four-port design above, bcc no longer needs the journal to observe what an iteration produced. That changes what the journal is for: agent-to-human attribution, not orchestration plumbing. Today's contract is a fossil from the era when the journal **was** the only signal: 21% of the [Phase 2 spec](./2026-04-29-phase-2-tui-dashboard.md) is journal, 3 of its 15 entries are no-op review checkpoints whose only purpose is keeping `HEAD` advancing, and every entry duplicates spec-body content via "Notes for observer" walls.

### Five rules

1. **The journal is never load-bearing for bcc.** All control-flow signals come from the `bcc_event` wire protocol and the plan checkboxes. bcc does not parse `**Result**:` and does not require entry presence. The journal is purely an agent-to-human channel.
1. **The journal is optional.** `[journal].store = "none"` is a valid setting; with that store, the agent's contract instructs it to skip journal writes entirely, and bcc runs to completion. Default remains `markdown_inspec`; the wizard offers `none` as a discrete choice.
1. **No no-op entries.** An iteration writes a journal entry only when it has something decision-bearing to record: a technical decision future iterations must respect, a problem encountered and how it was resolved, or new `[ ]` sub-items added to the plan ("discovered work"). An iteration that produced no commit, or whose only output was a structural / observer-driven gate, writes nothing.
1. **Minimally structured, not strictly schema'd.** A heading and a short lead are the only required parts. Retired fields: `**Result**:`, `**Commits**:`, `**Next**:`, and the mandatory `**Summary**:` block (all derivable from the wire log, git history, or the plan). Surviving callouts (Decisions, Problems, Discovered) are optional bullets. Free-form prose between heading and bullets is allowed.
1. **No "Notes for observer" walls.** Observer-driven steps belong in the spec's phase body, not in every iteration's journal. When a review checkpoint is needed, the wire event carries `signal=review`; the agent may add a short prose paragraph explaining the block, but it does not re-explain the spec.

### Entry shape (when one is written)

````markdown
### YYYY-MM-DD HH:MM, <phase or topic>

<one-line lead: what this entry is about, why it exists>

<optional free-form prose paragraph for context the next reader needs>

- **Decisions**: <technical choice future iterations must respect> (omit field if empty)
- **Problems**: <incident> → <resolution> (omit if empty)
- **Discovered**: added `[ ]` <item> to P<n> ... (omit if empty)
````

The contract is **negative**: heading and lead are the only required parts. The bullets may be empty, the prose may be empty; if all three are empty, **do not write the entry**.

### Off-switch

```toml
[journal]
store = "none"

[journal.none]
# no options; agent skips journal writes entirely
```

`store = "none"` is purely a template input: the embedded markdown_bcc contract renders without journal-writing instructions when `{{.JournalEnabled}}` is false, so the agent never sees a "write a journal entry" instruction in that mode. There is no port to disable, no viewer to hide. bcc never touches the journal regardless of the store value.

### What the wire protocol carries instead

| Old journal field | New source |
|---|---|
| `**Result**:` | `bcc_event` of kind `iteration_result`, field `value` |
| `**Commits**:` | `git log <baseline>..HEAD` (already feeds the run-local commit count) |
| `**Summary**:` (mandatory) | Commit messages plus per-task `bcc_event` summaries |
| `**Next**:` | The agent picks the next pending unit itself when it reads the spec; bcc does not parse a "next item" |
| `**Notes for observer**` | The spec's phase body; for actionable signal, `bcc_event` with `value=review` and a short prose `summary` |

## TUI items pulled in from Phase 2

These were originally scoped inside [Phase 2](./2026-04-29-phase-2-tui-dashboard.md). The phase-2 design assumed bcc would parse the spec for display; this spec retires that assumption. The carried-over items become event-driven; everything that depended on `SpecReader.Render`, `Progress`, or `LatestSignal` (the parser-based TUI features) is dropped.

1. [ ] **Risk and progress panels migrate to event-driven state.** `progressPanel` consumes `BccEventTaskStarted` / `BccEventTaskCompleted` to track checked-vs-total per run; the totals come from cumulative observations rather than a parse. `riskPanel` consumes the most recent `BccEventIterationResult.Signal`. Initial render (before any event arrives) shows empty placeholders; no parse, no first-render hack.
1. [ ] **Edit-spec post-edit re-trigger** (was P2.11 sub-item 6). After the user returns from `$EDITOR`, the session menu offers `[r]` to start the next iteration. The agent re-reads the spec on its own; bcc does nothing format-aware. Editor-suspension mechanics (`ReleaseTerminal` / `RestoreTerminal`) stay in `internal/tui/` and are format-neutral.

The phase-2 "spec parsed at startup", "optional spec preview panel", "journal viewer", and "edit-spec end-to-end smoke (journal-driven)" sub-items are dropped. The user opens the spec in their editor for any in-depth view; bcc's TUI surfaces only what the wire protocol delivers.

## Implementation Plan

Items are intentionally not numbered as P-X.Y; this spec stands on its own.

1. [x] **Inventory and freeze.** Coupling map and prompt-template references captured in [Coupling inventory](#coupling-inventory) and [The agent-contract leak](#the-agent-contract-leak) above.
1. [x] **Define `Mode`, `BriefingInput`, and the `AgentBriefing` port** in `internal/loop/ports.go`. Wire-protocol types and `ParseLine` live in `internal/loop/agentcontract/`, alongside the shared markdown blocks (`wire_protocol.md`, `absolute_restrictions.md`, `working_tree.md`).
1. [x] **Author the bcc-markdown agent contract.** `internal/format/markdown_bcc/contract.md` is a Go `text/template` embedded via `//go:embed`, composed at init time on top of `agentcontract.Partials()`. Format-specific blocks (procedure, scope discipline, journal contract, stop conditions in bcc-markdown vocabulary) live in `contract.md`; format-neutral blocks (wire protocol, absolute restrictions, working tree) come from the shared partials via `{{template "..." .}}`. `markdown_bcc.New(Config)` returns the adapter; `BuildPrompt(in)` renders the composed template.
1. [ ] **Plumb `agentcontract.ParseLine` in the executor.** Add a hook in the executor's type switch (`internal/executor/claude/claude.go:199-211`) that calls `agentcontract.ParseLine(line)` for unrecognized lines and forwards `BccEvent`s on the existing event channel as a new `AgentEventKind`. The executor depends on `agentcontract` directly; format adapters are not consulted (the wire protocol is canonical, not per-format).
1. [ ] **Refactor `LoopDecider` to consume `Signal`.** `Decide(latest Signal, headAdvanced bool) Action`. Drop `LatestResult`, `UncheckedAfter`, and `ExitDoneWithLeftovers`: bcc trusts the wire signal for done-verification, the user catches lies when reviewing the spec, and HEAD-stuck remains the orthogonal safety net. Update tests accordingly.
1. [ ] **Rewrite `Loop.Run` to consume the wire protocol.** Drop the spec-content read at the top of the iteration. Drop the `ParsePlan` / `ParseLatestResult` calls between iterations. Tracking-state for the iteration becomes "the latest `BccEvent` of kind `IterationResult` seen on the event channel", which feeds the decider after the executor returns. Loop's struct loses the `SpecContent` and `GuidePath` fields and gains a `Briefing AgentBriefing` field.
1. [ ] **Migrate TUI panels to event-driven state** per the [TUI items](#tui-items-pulled-in-from-phase-2) section. `progressPanel` and `riskPanel` re-route to `BccEvent`s. The `tui.SpecReader` port and `tui.SpecConfig.ResultVocab` are removed.
1. [ ] **Wire the hierarchical config.** Global selectors (`[spec].format`, `[journal].store`, `[agent].name`) plus per-adapter subtables. Drop `[loop.results]` (vocab mapping no longer exists; wire is canonical English). Drop `[specs]` (its fields move into `[spec.markdown_bcc]` as adapter-private inputs). `bcc init` writes sane defaults for every known adapter; CLI flags `--spec-format <name>` and `--agent <name>` override the active selectors for one run.
1. [ ] **Delete `internal/spec/`, `internal/specreader/`, `internal/loop/prompt.go`, `docs/guides/autonomous-execution.md`.** All four are superseded by the per-adapter contract embedded in `internal/format/markdown_bcc/`. Drop `loop.SpecContent` from `internal/loop/ports.go` (the legacy parser-style ports were already removed in commit `cc7f3f6`).
1. [ ] **Tests.** `markdown_bcc.AgentBriefing` golden-output tests (template rendering for each `Mode` × `JournalStore` combination, asserting shared partials are included). `agentcontract.ParseLine` fixtures for each `event` value (already landed). Decider tests in `Signal`. End-to-end fake-executor test where the fake emits a scripted `bcc_event` stream; the loop converges to the right exit code.
1. [ ] **Migration note in `CLAUDE.md`.** Update the architecture section: `internal/spec/` is gone, `markdown_bcc` is the default format adapter, the agent contract is per-format and embedded, the three ports are `Executor`/`GitProbe`/`AgentBriefing`, with the wire protocol owned by `internal/loop/agentcontract/`. Drop the line in the [Orthogonality](CLAUDE.md#orthogonality-pragmatic-programmer) section that mentions `docs/guides/autonomous-execution.md` as a single-package change.
1. [ ] **Open questions for follow-ups (do not block this spec).** Whether `ModeSingleShot` survives in its current form once the contract is per-format. Whether `BccEventProgressTick` is worth keeping or dropping until a use case appears. Whether the shared markdown partials should grow (e.g., a `discovered_work` block) once a second adapter materializes and the duplication shape is visible.

## Done criteria

- `go test -race ./...` clean.
- `internal/spec/`, `internal/specreader/`, `internal/loop/prompt.go`, and `docs/guides/autonomous-execution.md` are deleted.
- `internal/loop/ports.go` exposes only `Executor`, `GitProbe`, `AgentBriefing` (plus `Mode`, `BriefingInput`). Wire-protocol types and the canonical `ParseLine` live in `internal/loop/agentcontract/`. No `SpecContent`, no `SpecReader`, no per-format event parser.
- A second spec-format adapter (even a minimal stub) can be added under `internal/format/` without editing `loop/`, `tui/`, or `executor/`.
- bcc reads no file from the user's `docs/` directory or anywhere else under the user's project at runtime, except for the spec path validation (`os.Stat`) and what the executor adapter does to launch the agent. `bcc run` works in a project that has no `docs/` directory at all.
- The loop's continue/stop decision is driven entirely by the `bcc_event` wire protocol and the HEAD-advanced check.
- `bcc init` prompts for `[spec].format` and `[agent].name` from discrete lists, and writes sane defaults for **every** known adapter (not only the active ones) into the corresponding subtables.
- `--spec-format` and `--agent` CLI flags override the active selectors for a single run without modifying `.bcc.toml`.
- `[journal].store` values (`markdown_inspec`, `file`, `none`) flow into the embedded contract template; bcc never reads or writes the journal regardless of the value.

## Stop criteria

Reverse the work and reopen the design if:

- A real-world adapter (OpenSpec, Kiro) needs the loop to know something the wire protocol does not carry, and extending the protocol is more invasive than restoring a small read-side port.
- The agent reliably fails to emit `iteration_result` (across model versions, prompt variations, etc.) such that the loop cannot make progress. In that case, restore a minimal verification port; do not silently fall back to parsing.
- A real format pushes back on the canonical wire protocol (e.g., wants different event names or a different signal vocabulary). Extend the wire protocol once in `agentcontract` for everyone; do not re-introduce a per-format event parser.

## Out of scope

- **Agent-mediated discovery** (`format = "auto"` with a constrained one-shot LLM call). Parked, not killed; revisit when a user actually needs to point bcc at an unknown format.
- **Migration tooling between formats** (e.g., bcc-markdown → OpenSpec converter). Existing specs continue to work as-is in their original format.
- **Runtime adapter loading.** Adapters are compiled-in; selection is via config string, not dynamic loading.
- **Multi-language localization of journal storage.** Localization is a per-adapter concern.
- **Retroactive trimming of existing journal entries** (the phase-2 spec, the index, etc.). The new contract applies forward; old entries stay as a historical record of how the contract evolved.

## Related

- [Skill: fast-iteration spec authoring](./2026-04-29-skill-spec-authoring.md): the prompt-shaping techniques that let the agent get to work faster live in a skill, not in this framework. Independent of which format the spec uses.
- The bcc-markdown agent contract lives at `internal/format/markdown_bcc/contract.md`, embedded in the binary. The historical `docs/guides/autonomous-execution.md` file is split per [Retiring the legacy guide](#retiring-the-legacy-guide).

## Execution Journal

Most recent entries on top. Contract in [bcc-markdown contract](#bcc-markdown-contract).

### 2026-04-30 03:35, plumb agentcontract.ParseLine in executor

Added a `case "bcc_event":` arm in the claude executor's type switch (`internal/executor/claude/claude.go`). Unknown lines that match the `bcc_event` sentinel route through `agentcontract.ParseLine`, producing a normalized `loop.AgentEvent` with the new `KindBccEvent` and a populated `Bcc *agentcontract.BccEvent`. Aditive change; no consumer of `KindBccEvent` yet (LoopDecider still consumes `spec.Result`).

- **Decisions**: `AgentEvent` gained a `Bcc` field rather than a separate event union arm to keep the wire-event flow inside the existing `AgentEventReceived` -> level filter -> JSON / TUI pipeline. Verbosity defaults to `LevelInfo`. NDJSON shape: `{"type":"agent_event","kind":"bcc_event","bcc":{"event_kind":"...","id":"...","signal":"...","summary":"..."}}`. Fields are omitted when empty so existing tests' byte-for-byte outputs are unaffected.

### 2026-04-30 03:10, extract agentcontract package; drop AgentEvents port

Reviewer pointed out that the wire protocol (markdown describing `bcc_event`, plus `BccEvent`/`Signal`/`BccEventKind` types and `ParseLine`) is bcc-level, not per-format. Every format adapter would emit and parse the same shape; making it format-specific was over-design. Extracted to `internal/loop/agentcontract/`: types + `ParseLine` + `Partials() *template.Template` exposing the three shared markdown blocks (`wire_protocol.md`, `absolute_restrictions.md`, `working_tree.md`). The `AgentEvents` port disappeared along with its per-format implementations.

`markdown_bcc.contract.md` now invokes `{{template "wire_protocol" .}}`, `{{template "absolute_restrictions" .}}`, `{{template "working_tree" .}}` for the format-neutral sections; format-specific blocks (procedure, scope discipline, journal contract, stop conditions) stay in `contract.md`. Final port count: three (`Executor`, `GitProbe`, `AgentBriefing`).

- **Decisions**: The wire protocol is canonical English with one shape; OpenSpec / Kiro / Ralph adapters will reuse `agentcontract.ParseLine` directly, with no AgentEvents implementation of their own. The shared markdown partials are body-only (no heading); the parent template provides the heading at whatever level fits. If an adapter ever needs a format-specific event variation, the answer is to extend the wire protocol once for everyone in `agentcontract`, not to re-introduce a per-format parser.

### 2026-04-30 02:30, author contract.md and the markdown_bcc adapter

Created `internal/format/markdown_bcc/` with the embedded `contract.md` (Go `text/template`), the `Reader` struct implementing `loop.AgentBriefing.BuildPrompt` and `loop.AgentEvents.ParseLine`, and golden-output tests. The contract is wire-protocol-first: the agent reads the spec from `SpecPath` itself; bcc never injects content. Mode switch (`loop` vs `single-shot`) and journal-store branch (`markdown_inspec`/`file`/`none`) are template conditionals on the `Mode` and `JournalStore` template vars.

- **Decisions**: The contract opens by claiming primacy over project-local instructions (`CLAUDE.md`, `AGENTS.md`, custom skills) on points where they conflict, except absolute restrictions which nothing may relax. This is the assertive tone required by the [framework boundary](#framework-and-user-space-boundary). Wire protocol's emission point for `iteration_result` is "exactly once, immediately before exit"; missing or malformed = bcc exits invalid (per [Discovery strategy](#discovery-strategy)). The contract is not yet wired into Loop; Loop still uses `internal/loop/prompt.go`. Wiring + retiring the legacy template comes next, alongside the executor's `bcc_event` plumbing.

### 2026-04-30 01:55, rename internal/specreader → internal/format

`specreader` was the name of what the package used to do. The package now contains the format adapter (`AgentBriefing` template + contract, `AgentEvents` parser, per-format Config) and reads nothing. Renamed to `internal/format/<name>/`, paralleling `internal/executor/<name>/`. The embedded contract file is `contract.md`, not `prompt.md`, because it is the agent contract, not just a prompt template.

- **Decisions**: Future adapters land at `internal/format/openspec/`, `internal/format/kiro/`, etc. The doc comment in `internal/loop/ports.go` was updated to point at `format/<flavor>` instead of `specreader/<flavor>`. Spec body explicitly notes the rename so a reader catches the intent.

### 2026-04-30 01:30, drop SpecReader and SpecContent (wire-protocol-first)

Reviewer pushed back on `SpecReader` for the same reason `JournalReader` was dropped: parsing the user's spec is brittle and bcc has no good reason to do it once the wire protocol carries every signal it needs. Five `SpecReader` methods (`LatestSignal`, `WorkRemaining`, `Progress`, `NextWorkItem`, `Render`) all have wire-protocol equivalents (`bcc_event` for the first three; the agent picks the next item from the spec itself; `Render` was for a preview panel that has no business existing). `SpecContent` follows: bcc has no reason to inject spec content into the prompt either, the agent reads `SpecPath` itself.

Result: bcc never reads any user-space file at runtime. Four ports total: `Executor`, `GitProbe`, `AgentBriefing`, `AgentEvents`. Loop's `SpecContent` field, `GuidePath` field, and the inline parser calls all retire in the next commit. `internal/spec/` and the entire `internal/specreader/` tree are deleted as part of the same migration; the format adapters land under `internal/format/<name>/`.

- **Decisions**: Wire protocol's `iteration_result.value` is canonical English (`continue` / `review` / `done` / `blocked`); journal localization stays a per-adapter prompt-template concern. `[loop.results]` config section is dropped entirely (was the surface-vocab mapping). `[specs].PlanHeading` / `JournalHeading` / `ResultKeyword` move into `[spec.markdown_bcc]` as adapter-private template inputs that the agent uses when writing, not for bcc to parse. `BriefingInput` stops carrying `NextItemID` and `JournalEnabled`; the adapter's Config holds the journal-store hint as a template variable. `RenderProfile` is removed. The TUI items "spec parsed at startup", "optional spec preview panel", "journal viewer", and the original "edit-spec end-to-end smoke" are all dropped; preview is what the user's editor is for. Done-verification (no leftover `[ ]` after `done` signal) is the agent's responsibility per the contract; bcc trusts the signal, the user catches lies on review. Implementation Plan rewritten end to end around the 4-port shape.

### 2026-04-30 00:48, drop JournalReader entirely

Removed `JournalReader`, `JournalEntry`, and the `internal/journal/` adapter family from the design. The port existed only to feed the `[j]` TUI viewer, and the only viable read path was a markdown parser over the user's spec file, which the new contract makes brittle by allowing free-form prose between heading and bullets. The wire protocol already covers display: `bcc_event` of kind `iteration_result` carries signal and a short summary; richer payload extends that event, not a new port. The `[j]` viewer is dropped; the user opens the spec in their editor if they want the journal.

- **Decisions**: `[journal].store` survives as **template input** to `AgentBriefing`, not as a port selector. The store value picks which journal-writing fragment the embedded contract injects (`markdown_inspec` → "append to the spec", `file` → "write to `<path>`", `none` → block omitted). bcc never reads the journal; the agent is the sole writer, the user the sole reader.

### 2026-04-30 00:23, JournalStore → JournalReader (intermediate step, superseded)

bcc has no journal write port. `AppendEntry` was a fossil from the pre-event-protocol design where bcc owned the write; under the contract codified in this spec, the agent owns journal writes (instructed via `AgentBriefing`), and bcc only reads, and only for the optional TUI viewer. Renamed the port to `JournalReader`, dropped `AppendEntry`, added an `ok` return on `Latest` so the `none` store can signal "nothing to show" cleanly. Superseded by the 00:48 entry which dropped the port entirely.

### 2026-04-30 00:07, plan items 1 and 2

Defined `Signal`, `BccEvent`, `BriefingInput`, `JournalEntry`, and the four new ports (`SpecReader`, `AgentBriefing`, `AgentEvents`, `JournalStore`) as additive declarations in `internal/loop/ports.go`. The legacy content-shaped port was renamed to `SpecContent` in the same commit so the canonical `SpecReader` name belongs to the new shape from day one.

- **Decisions**: Renamed legacy `loop.SpecReader` to `loop.SpecContent` here rather than in a follow-up commit. Frees the canonical name immediately and makes downstream phases refer to a stable target, at the cost of touching every `loop.Loop` construction site (`loop.go`, `loop_test.go`, `integration_test.go`, `cli/run.go`) and the markdown adapter. Test fakes (`stepfulSpecReader`, `errSpecReader`) keep their original names; they are internal test types whose names describe behavior, not the port.
