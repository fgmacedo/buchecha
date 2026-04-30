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

Design implication: ports speak in `Signal`, `Progress`, and `NextWorkItem`, never in `Phase` or `Item`. Format-specific data structures stay inside the adapter.

## Goal shape

Four ports, each owned by the consumer that calls it, each implemented by the active adapter package.

### Port: `SpecReader` (read-side introspection)

```go
type Signal int

const (
    SignalUnknown Signal = iota
    SignalContinue
    SignalReview
    SignalDone
    SignalBlocked
)

type SpecReader interface {
    // LatestSignal returns the most recent decision-relevant signal from
    // the spec's progress record (the journal, the tasks.json status,
    // whatever the format uses).
    LatestSignal(ctx context.Context, specPath string) (Signal, error)

    // WorkRemaining is true when at least one pending unit exists. The
    // format decides what "pending" means: unchecked checkbox, open
    // task, task with unmet dependencies, etc.
    WorkRemaining(ctx context.Context, specPath string) (bool, error)

    // Progress is an optional UI hint. Adapters with no notion of
    // progress return ok=false and the TUI degrades to a textless
    // progress bar.
    Progress(ctx context.Context, specPath string) (checked, total int, ok bool, err error)

    // NextWorkItem returns an opaque, format-defined identifier for the
    // unit the agent should focus on next (phase number for
    // bcc-markdown, task ID for Ralph or OpenSpec). Adapters with no
    // notion of "next item" return ok=false; the briefing falls back to
    // a generic "implement the next pending item" prompt.
    NextWorkItem(ctx context.Context, specPath string) (id string, ok bool, err error)

    // Render produces a TUI-friendly view of the spec for the optional
    // preview panel. Adapters without a renderer return ok=false; the
    // panel is hidden.
    Render(ctx context.Context, specPath string, profile RenderProfile) (text string, ok bool, err error)
}
```

### Port: `AgentBriefing` (prompt-side)

```go
type AgentBriefing interface {
    // BuildPrompt returns the full prompt for one agent invocation. The
    // adapter owns the per-format operating contract (today's
    // autonomous-execution.md content for bcc-markdown), embedded in
    // the binary via //go:embed and stitched into the prompt here.
    BuildPrompt(ctx context.Context, in BriefingInput) (string, error)
}

type BriefingInput struct {
    SpecPath   string
    Iteration  int
    NextItemID string // empty when SpecReader.NextWorkItem returned ok=false
    Mode       Mode   // ModeLoop | ModeSingleShot
}
```

The loop no longer constructs prompts directly. It calls `BuildPrompt` and passes the result to the executor. This retires `internal/loop/prompt.go` into the adapter.

### Port: `AgentEvents` (event-side)

```go
type AgentEvents interface {
    // ParseLine inspects one JSONL line from the executor's stdout and
    // returns a normalized BccEvent if the line matches the adapter's
    // wire protocol. Returns ok=false when the line is unrelated; the
    // executor falls through to its existing handling.
    ParseLine(line []byte) (BccEvent, bool)
}

type BccEvent struct {
    Kind    BccEventKind // TaskStarted | TaskCompleted | IterationResult | ProgressTick
    ID      string       // task or phase ID, format-defined
    Signal  Signal       // populated for IterationResult
    Summary string
    Raw     map[string]any
}
```

The wire protocol agents are taught to emit (via the briefing's contract text):

```jsonc
{"type":"bcc_event","event":"task_started","id":"P1.2","summary":"..."}
{"type":"bcc_event","event":"task_completed","id":"P1.2"}
{"type":"bcc_event","event":"iteration_result","value":"ok","summary":"..."}
```

The executor's claude adapter routes lines its own type switch does not recognize through `AgentEvents.ParseLine`. The seam is `internal/executor/claude/claude.go:192-213`: a new `case "bcc_event":` arm wraps the adapter call. markdown_bcc returns `(BccEvent{}, false)` for everything in this iteration; parser remains the source of truth. Adapters for formats whose source-of-truth is hard to parse (Ralph DAGs, multi-file OpenSpec) lean on this from day one.

### Port: `JournalReader` (display-only)

bcc does not write journal entries. The agent owns the write side, instructed by the `AgentBriefing` prompt according to `[journal].store`. bcc does not read the journal for control flow either; signal comes from the `bcc_event` wire protocol. The only reason a port exists is the optional TUI viewer (see [TUI items](#tui-items-pulled-in-from-phase-2), item 3): pressing `[j]` shows the most recent entry. The no-op `none` store returns `ok=false`, hiding the binding.

```go
type JournalReader interface {
    Latest(ctx context.Context) (entry JournalEntry, ok bool, err error)
}

type JournalEntry struct {
    At      time.Time
    Phase   string
    Signal  Signal
    Summary string
    // Free-form payload preserved by the adapter for round-tripping.
    Raw map[string]any
}
```

### Adapters

- `internal/specreader/markdown_bcc/`: the rename of today's `markdown` adapter. Owns `Plan`, `Phase`, `Item`, `Result`. Implements `SpecReader`, `AgentBriefing`, `AgentEvents`. Today's `internal/spec/` types **move into this package** as adapter-private types.
- `internal/specreader/openspec/`, `.../kiro/`, `.../speckit/`, `.../bmad/`: future siblings. Each one knows its own format's notion of "task complete", "phase done", "review needed".
- `internal/journal/markdown_inspec/`: implements `JournalReader` by parsing the journal section out of the spec file. Default. The agent (instructed via `AgentBriefing`) is the writer; this adapter only reads.
- `internal/journal/file/`: implements `JournalReader` by reading a sibling file (e.g., `<spec>.journal.ndjson`). Same writer/reader split: agent writes per contract, adapter reads.
- `internal/journal/none/`: `JournalReader` returning `ok=false` from `Latest`; the briefing template suppresses journal-writing instructions.
- Future: `internal/journal/sqlite/`, `internal/journal/external/` for ticket systems.

### `.bcc.toml` shape

Hierarchical: a global section per concern names the active adapter; per-adapter subtables hold that adapter's options. Multiple adapters live side by side; switching is one key change in the global section, not a rewrite.

```toml
[spec]
format = "markdown_bcc"      # active format; markdown_bcc | openspec | kiro | speckit | bmad

[spec.markdown_bcc]
plan_heading    = "Implementation Plan"
journal_heading = "Execution Journal"
result_vocab    = { ok = "ok", partial = "partial", done = "done", blocked = "blocked", review = "review" }

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

## Discovery strategies

Three strategies for "how does bcc know what's pending":

| Strategy | When | Pros | Cons |
|---|---|---|---|
| **Native parser** (per-format adapter) | Default for known formats | Cheap, deterministic, no agent call | Requires per-format code |
| **Agent-reported events** (always-on supplement) | Real-time UI; primary signal between iterations | Format-agnostic, real-time | Trust agent reliability |
| **Agent-mediated discovery** (one-shot constrained call at startup) | Unknown/exotic formats | Universal | Slow, costs tokens |

Decision: default for any supported format is parser-as-source-of-truth plus event-stream-as-real-time-feed. The spec deliberately does not introduce agent-mediated discovery; if a user's format is unsupported, they pick a closer adapter or open an issue. Adding adapters is cheaper than reinventing the loop around one-shot LLM parsing. Agent-mediated discovery is parked, not killed, see [Out of scope](#out-of-scope).

## Framework and user-space boundary

`internal/` is framework space. `docs/` is user space. bcc reads from the former at runtime; it never reads from the latter.

Framework prompts live under `internal/specreader/<format>/` (and similarly for any other framework-owned text), embedded via `//go:embed`. They are Go `text/template`s where customization is supported. Substitutions provided by the active config include `{{.PlanHeading}}`, `{{.JournalHeading}}`, `{{.ResultVocab.OK}}`, etc.; conditional sections such as `{{if .JournalEnabled}}...{{end}}` cover features that toggle by config.

The user's project `docs/`, `CLAUDE.md`, `AGENTS.md`, and agent skills are theirs. They may inject content the agent reads, but bcc neither depends on them nor defends against them. The framework prompt is **assertive**: a clear, prescriptive contract with the agent that defines required behavior regardless of surrounding noise. Tone is rule-based and unambiguous, not advisory. Where user-space content could plausibly conflict (custom commit-message conventions, alternate review workflows), the prompt names the conflict and tells the agent how to resolve it (e.g., "follow the project's convention if visible from `git log`, otherwise use the lowercase prefix from this contract").

Consequence for `docs/guides/autonomous-execution.md`: the file is removed; its contents split into three artifacts per [Retiring the legacy guide](#retiring-the-legacy-guide).

## Retiring the legacy guide

Today's `docs/guides/autonomous-execution.md` is doing three jobs at once. Splitting them lands cleanly under the boundary above:

1. **Framework agent contract.** The "Operating mode", "Absolute restrictions", "Execution Journal", "Done criteria", and "Stop criteria" portions move to `internal/specreader/markdown_bcc/prompt-*.md` (single file or split, implementing PR's call). Embedded via `//go:embed`. Templated where config substitution applies.
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

`store = "none"` resolves to a no-op `JournalReader` that returns `ok=false` from `Latest`, so the TUI viewer hides its binding. The embedded markdown_bcc contract template renders without journal-writing instructions when `{{.JournalEnabled}}` is false, so the agent never sees a "write a journal entry" instruction in that mode. The agent's contract is the only journal writer; bcc has no write port to drop, only an instruction to suppress.

### What the wire protocol carries instead

| Old journal field | New source |
|---|---|
| `**Result**:` | `bcc_event` of kind `iteration_result`, field `value` |
| `**Commits**:` | `git log <baseline>..HEAD` (already feeds the run-local commit count) |
| `**Summary**:` (mandatory) | Commit messages plus per-task `bcc_event` summaries |
| `**Next**:` | `SpecReader.NextWorkItem()` |
| `**Notes for observer**` | The spec's phase body; for actionable signal, `bcc_event` with `value=review` and a short prose `summary` |

## TUI items pulled in from Phase 2

These were originally scoped inside [Phase 2](./2026-04-29-phase-2-tui-dashboard.md) but cannot land without the ports defined here. They live under this spec's umbrella so the work stays in one place. Each ships as a follow-up commit after the corresponding port is in place; the bcc-markdown adapter implements the per-format details.

1. [ ] **Spec parsed at startup** (was P2.9 sub-item 4). Once `SpecReader.Progress()` and `LatestSignal()` exist, dispatch them from `Model.Init()` so the progress and risk panels populate on the first render rather than waiting for the first `IterationFinished`. No-op when the adapter returns `ok=false` from `Progress()`.
1. [ ] **Optional spec preview panel** (was P2.10 sub-item 6). Uses `SpecReader.Render(profile)`. The bcc-markdown adapter implements it via `charm.land/glamour/v2`; other adapters return their own pretty-printer output or `ok=false`. The TUI keybinding (`s`) toggles a modal viewport; absent renderers hide the binding.
1. [ ] **Journal viewer** (was P2.11 sub-item 4). The `[j]` binding uses `JournalReader.Latest()` to fetch the most recent entry, then renders it through the adapter's `Render` (markdown_bcc → glamour; other adapters → text). The viewer respects `--no-color`. When the active reader returns `ok=false` (e.g., `[journal].store = "none"`), the binding is hidden.
1. [ ] **Edit-spec post-edit refresh** (was P2.11 sub-item 6). After the user returns from `$EDITOR`, the menu's data is refreshed by re-calling the `SpecReader` signals; the editor-suspension mechanics (`ReleaseTerminal` / `RestoreTerminal`) stay in `internal/tui/` and are format-neutral.
1. [ ] **Edit-spec end-to-end smoke** (was P2.12 sub-item 11). End-to-end test: edit the spec from the session menu, confirm the journal viewer reflects the edited content. Depends on the journal viewer above.

## Implementation Plan

Items are intentionally not numbered as P-X.Y; this spec stands on its own.

1. [x] **Inventory and freeze.** Capture the coupling map (table in [Coupling inventory](#coupling-inventory)) and the prompt-template references (`internal/loop/loop.go:81`, `internal/loop/prompt.go:36`, `internal/loop/prompt.go:78`) into this spec body so reviewers see the full surface area before any code moves.
1. [x] **Define `Signal` and the four ports** (`SpecReader`, `AgentBriefing`, `AgentEvents`, `JournalReader`) in `internal/loop/ports.go`. Types and doc comments only, no implementation.
1. [ ] **Refactor `LoopDecider`** (`internal/loop/decider.go`) to consume `Signal` and `WorkRemaining bool` instead of `spec.Result` and `UncheckedAfter`. Update callers in `internal/loop/loop.go` and tests.
1. [ ] **Carve the `markdown_bcc` adapter.** Create `internal/specreader/markdown_bcc/` with the existing parsing code. Move `internal/spec/` types in as `markdown_bcc`-private types. The adapter implements `SpecReader`, mapping its `Result` to `Signal` and `unchecked > 0` to `WorkRemaining`. Wire it from `cmd/bcc/` as the default when `[spec].format = "markdown_bcc"`.
1. [ ] **Author the bcc-markdown agent contract in framework space.** Create the prompt files at `internal/specreader/markdown_bcc/prompt-*.md` (single file or split, implementing PR's call), authored as Go `text/template` with substitutions (`{{.PlanHeading}}`, `{{.JournalHeading}}`, `{{.ResultVocab.*}}`, `{{.JournalEnabled}}`, ...) and embedded via `//go:embed`. Write the content to match the [bcc-markdown contract](#bcc-markdown-contract) rules: the journal is optional and never load-bearing for bcc; no-op entries are forbidden; the entry shape is heading + lead with optional Decisions/Problems/Discovered callouts; observer-driven steps live in the spec body. Implement `AgentBriefing.BuildPrompt` so the adapter, not `internal/loop/prompt.go`, owns prompt construction; render the templates with the active config. Delete `PromptInput.GuidePath`, the prompt-template references at `internal/loop/loop.go:81`, `internal/loop/prompt.go:36`, `internal/loop/prompt.go:78`, and the file `docs/guides/autonomous-execution.md` (per the [retiring plan](#retiring-the-legacy-guide)).
1. [ ] **Plumb `AgentEvents`.** Add a `case "bcc_event":` arm in the executor's type switch (`internal/executor/claude/claude.go:199-211`) that calls the active adapter's `AgentEvents.ParseLine` and forwards the resulting `BccEvent` on the existing event channel. markdown_bcc returns `(BccEvent{}, false)` for everything in this iteration; the executor falls through to its existing "unknown line, drop" behavior. Plumbing in place; non-breaking.
1. [ ] **Decouple TUI panels.** `progressPanel` consumes `(checked, total, ok)` from `SpecReader.Progress()`; degrades to a textless ratio bar when `ok=false`. `riskPanel` reads `LatestSignal()` instead of `spec.Result`. Re-introduce the immediate-on-startup parse only via the new port, not via `parseSpecCmd` directly.
1. [ ] **Implement `JournalReader` markdown_inspec.** Read the journal section out of the spec file and surface the latest entry via `Latest()`. Move whatever parser bits today's `internal/spec/` carries for journal reads into the new adapter; the agent owns the write side via the contract embedded in `AgentBriefing`.
1. [ ] **Implement the `none` journal reader.** Returns `ok=false` from `Latest`. Add `[journal].store = "none"` to the wizard's discrete choice list with description "skip journal writes (recommended for short specs and CI)". The store choice drives both the `AgentBriefing` template (whether to instruct journal writes) and the `JournalReader` wiring (which adapter the TUI viewer queries).
1. [ ] **Wire the hierarchical config.** Global selectors (`[spec].format`, `[journal].store`, `[agent].name`) plus per-adapter subtables (`[spec.markdown_bcc]`, `[spec.openspec]`, `[journal.file]`, `[agent.claude]`, ...). Each adapter validates its own subtable at startup and fails fast on unknown keys. `bcc init` writes sane defaults for every known adapter. CLI flags `--spec-format <name>` and `--agent <name>` override the active selectors for a single run without modifying `.bcc.toml`. This step absorbs and supersedes today's flat `[executor]` block.
1. [ ] **Tests.** Adapter round-trip per port (parse a fixture, assert the right signal and progress emerge). Decider tests re-expressed in `Signal` (no `spec.Result` import in `internal/loop/`). Smoke test that `bcc run` works against a fixture project with **no** `docs/guides/autonomous-execution.md`.
1. [ ] **Migration note.** Update `CLAUDE.md`'s architecture section: `internal/spec/` is gone (or shrunk to truly format-neutral helpers); `markdown_bcc` is the default adapter, not the assumed format; the agent contract is per-adapter, embedded in the binary.
1. [ ] **Open questions for follow-ups (do not block this spec).** Whether `Phase` is a TUI-only concept (some formats may not have phases). Whether `WorkRemaining` should be richer (e.g., ETA hints). Whether the loop should support format-specific extensions (Ralph-style `Dependencies()`, OpenSpec multi-file context). Whether `AgentEvents` should grow a `ProgressTick` for streamed sub-task progress.

## Done criteria

- `go test -race ./...` clean.
- No package outside `internal/specreader/markdown_bcc/` imports `internal/spec/` (or its successor).
- A second adapter (even a minimal stub) can be added under `internal/specreader/` without editing `loop/`, `tui/`, or `executor/`.
- bcc reads no file from the user's `docs/` directory at runtime. The framework prompt lives in `internal/specreader/<format>/` and ships embedded; `bcc run` works in a project that has no `docs/` directory at all.
- bcc never reads the in-spec journal for control flow; the loop's continue/stop decision is driven entirely by the `bcc_event` wire protocol and the plan checkboxes.
- `[journal].store = "none"` is a supported setting; a full loop run with that store completes without writing or reading any journal entry.
- A no-op iteration (no commits, no decisions, no discovered work) writes no journal entry. The phase-2 spec's three "no-op review checkpoint" entries cannot be reproduced under the new contract.
- `bcc init` prompts for `[spec].format` and `[agent].name` from discrete lists, and writes sane defaults for **every** known adapter (not only the active ones) into the corresponding subtables.
- `--spec-format` and `--agent` CLI flags override the active selectors for a single run without modifying `.bcc.toml`.
- `.bcc.toml` with default values produces the same observable behavior as today.

## Stop criteria

Reverse the work and reopen the design if:

- The signal shape proves too lossy for a real adapter (e.g., BMAD cannot be expressed in `Continue/Review/Done/Blocked`). In that case, expand the `Signal` set rather than leaking format types upward.
- TUI panels degrade so far that the dashboard stops being useful for the bcc-markdown default. Adjust the `Progress()` contract to give the panels enough to render.
- The four-port split proves over-engineered when applied to the second adapter (e.g., `AgentBriefing` and `SpecReader` collapse into one). Revert and reopen the design before adding a third adapter.
- After the contract rewrite, the next-round agent demonstrably loses information that today's verbose journal carried (e.g., a Decision is missed because the new format made it too easy to omit). Tighten the contract by elevating one or more callouts from optional to recommended; do not roll back to the mandatory schema.

## Out of scope

- **Agent-mediated discovery** (`format = "auto"` with a constrained one-shot LLM call). Parked, not killed; revisit when a user actually needs to point bcc at an unknown format.
- **Migration tooling between formats** (e.g., bcc-markdown → OpenSpec converter). Existing specs continue to work as-is in their original format.
- **Runtime adapter loading.** Adapters are compiled-in; selection is via config string, not dynamic loading.
- **Multi-language localization of journal storage.** Localization is a per-adapter concern.
- **Retroactive trimming of existing journal entries** (the phase-2 spec, the index, etc.). The new contract applies forward; old entries stay as a historical record of how the contract evolved.

## Related

- [Skill: fast-iteration spec authoring](./2026-04-29-skill-spec-authoring.md): the prompt-shaping techniques that let the agent get to work faster live in a skill, not in this framework. Independent of which format the spec uses.
- The bcc-markdown agent contract lives at `internal/specreader/markdown_bcc/prompt-*.md`, embedded in the binary. The historical `docs/guides/autonomous-execution.md` file is split per [Retiring the legacy guide](#retiring-the-legacy-guide).

## Execution Journal

Most recent entries on top. Contract in [bcc-markdown contract](#bcc-markdown-contract).

### 2026-04-30 00:23, JournalStore → JournalReader (drop write side)

bcc has no journal write port. `AppendEntry` was a fossil from the pre-event-protocol design where bcc owned the write; under the contract codified in this spec, the agent owns journal writes (instructed via `AgentBriefing` per `[journal].store`), and bcc only reads, and only for the optional TUI viewer. Renamed the port to `JournalReader`, dropped `AppendEntry`, added an `ok` return on `Latest` so the `none` store can signal "nothing to show" cleanly.

- **Decisions**: Adapters under `internal/journal/<store>/` own reading only. Writing lives entirely inside the embedded markdown_bcc contract, conditional on `{{.JournalEnabled}}`. `[journal].store = "none"` resolves to a `JournalReader` that returns `ok=false`, and the briefing template suppresses journal-writing instructions, so the off-switch is "agent does not write" plus "TUI viewer hides binding". No write port to drop, only an instruction to suppress.

### 2026-04-30 00:07, plan items 1 and 2

Defined `Signal`, `BccEvent`, `BriefingInput`, `JournalEntry`, and the four new ports (`SpecReader`, `AgentBriefing`, `AgentEvents`, `JournalStore`) as additive declarations in `internal/loop/ports.go`. The legacy content-shaped port was renamed to `SpecContent` in the same commit so the canonical `SpecReader` name belongs to the new shape from day one.

- **Decisions**: Renamed legacy `loop.SpecReader` to `loop.SpecContent` here rather than in a follow-up commit. Frees the canonical name immediately and makes downstream phases refer to a stable target, at the cost of touching every `loop.Loop` construction site (`loop.go`, `loop_test.go`, `integration_test.go`, `cli/run.go`) and the markdown adapter. Test fakes (`stepfulSpecReader`, `errSpecReader`) keep their original names; they are internal test types whose names describe behavior, not the port.
