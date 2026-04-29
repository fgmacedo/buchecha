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

`bcc` is an orchestrator, not a spec-format authority. Today the framework is silently coupled to one specific markdown layout (the bcc-markdown convention with `Implementation Plan` checkboxes and `## Execution Journal` Result blocks). This spec carves the vocabulary of that layout out of the domain, behind ports that speak in **signals** instead of **content**, so other formats (open-spec, spec-kit, bmad, custom in-house) can be supported by writing an adapter rather than forking the framework.

It also separates **journal storage** from **spec content**, so the journal can live in the spec file (current default), in a sibling file, in a database, or in an external system, configurable via `.bcc.toml`.

## Status

**Draft.** Not numbered as a phase: priority is fluid relative to the existing P3, P4, P5 work and may shift as the dogfooding feedback comes in. Pull this spec forward when (a) we want to run `bcc` on a project that uses a non-bcc spec format, or (b) we add a feature that would otherwise deepen the coupling.

## Context and motivation

`internal/spec/` is treated as a domain layer (per `CLAUDE.md`'s hexagonal model), but it is in practice the **vocabulary of one parser**: `Plan`, `Phase`, `Item`, `Result` (enum: ok/partial/done/review/blocked), `JournalEntry`, `LatestResult`. These types reflect the bcc-markdown convention; they are not neutral concepts.

Concrete coupling sites today:

- `internal/loop/decider.go:36,57` reads `LatestResult` (enum `spec.Result`) and `UncheckedAfter`. The decider is the most format-agnostic layer in principle, yet consumes parser-specific types.
- `internal/tui/progress.go:18,31` and `internal/tui/risk.go:34` consume `spec.Plan`, `spec.LatestResult`. The "X/Y items" panel and "phase label" only make sense for hierarchical-checkbox formats.
- `internal/tui/tui.go:385,389` calls `spec.ParsePlan(content, cfg.PlanHeading)` and `spec.ParseLatestResult(...)`. Today's "configurability" is only `PlanHeading` (i18n: "Implementation Plan" vs "Plano"); that is **localization**, not format-agnosticism.
- `internal/loop/ports.go:52-55` defines `SpecReader.Read(path) (string, error)` returning raw markdown. The port is shaped around content, not signals.
- The journal has no port. Today it is parsed back from the same markdown file as an appendix; agents rewrite the file to append entries.

The user-facing problem this creates: every new spec format requires a fork of `internal/spec/` and edits across `loop/`, `tui/`, and `executor/`. That violates the "replace one adapter, touch one package" rule from `CLAUDE.md`.

## Goal shape

Define ports that the loop and TUI consume in **signals**, with concrete formats living entirely behind adapters.

```
internal/loop/ports.go
    type Signal int
        SignalContinue, SignalReview, SignalDone, SignalBlocked, SignalUnknown

    type SpecReader interface {
        // Latest decision-relevant signal from the journal (or whatever
        // the format uses to record per-iteration outcomes).
        LatestSignal(ctx context.Context, specPath string) (Signal, error)

        // True when there is still work to be done. The format decides
        // what "work" means: unchecked checkbox, open task, pending phase.
        WorkRemaining(ctx context.Context, specPath string) (bool, error)

        // Optional progress for UI; adapters that have no notion of
        // progress return ok=false and the TUI degrades to a textless
        // progress bar.
        Progress(ctx context.Context, specPath string) (checked, total int, ok bool, err error)
    }

    type JournalStore interface {
        AppendEntry(ctx context.Context, e JournalEntry) error
        Latest(ctx context.Context) (JournalEntry, error)
    }

    type JournalEntry struct {
        At      time.Time
        Phase   string
        Signal  Signal
        Summary string
        // Free-form payload preserved by the adapter for round-tripping.
        Raw     map[string]any
    }
```

Adapters:

- `internal/specreader/markdown_bcc/` (rename of today's `markdown` adapter): owns `Plan`, `Phase`, `Item`, `Result`, `JournalEntry`. Maps the bcc-markdown vocabulary to the signal shape. Today's `internal/spec/` types **move into this package** as adapter-private types.
- `internal/specreader/openspec/`, `internal/specreader/speckit/`, `internal/specreader/bmad/`: future siblings. Each one knows its own format's notion of "task complete", "phase done", "review needed".
- `internal/journal/markdown_inspec/`: writes journal entries as appended blocks in the spec file (today's behavior).
- `internal/journal/file/`: writes journal entries to a sibling file (e.g., `<spec>.journal.ndjson` or `<spec>.journal.md`).
- Future: `internal/journal/sqlite/`, `internal/journal/external/` for ticket systems.

`.bcc.toml` config:

```toml
[spec]
format = "markdown_bcc"      # default; values: markdown_bcc | openspec | speckit | bmad

[journal]
store = "markdown_inspec"    # default; values: markdown_inspec | file | <future>
path  = ""                   # store-specific; e.g., explicit path for "file" store
```

## TUI items pulled in from Phase 2

These were originally scoped inside [Phase 2](./2026-04-29-phase-2-tui-dashboard.md) but cannot land without the ports defined here. They live under this spec's umbrella so the work stays in one place. Each ships as a follow-up commit after the corresponding port is in place; the bcc-markdown adapter implements the per-format details.

1. [ ] **Spec parsed at startup** (was P2.9 sub-item 4). Once `SpecReader.Progress()` and `LatestSignal()` exist, dispatch them from `Model.Init()` so the progress and risk panels populate on the first render rather than waiting for the first `IterationFinished`. No-op when the adapter returns `ok=false` from `Progress()`.
1. [ ] **Optional spec preview panel** (was P2.10 sub-item 6). The `SpecReader` port exposes an optional `Render(profile RenderProfile) (string, bool)` method (or similar). The bcc-markdown adapter implements it via `charm.land/glamour/v2`; other adapters return their own pretty-printer output or `false`. The TUI keybinding (`s`) toggles a modal viewport; absent renderers hide the binding.
1. [ ] **Journal viewer** (was P2.11 sub-item 4). The `[j]` binding uses `JournalStore.Latest()` to fetch the most recent entry, then renders it through the adapter's `Render` (markdown_bcc → glamour; other adapters → text). The viewer respects `--no-color`.
1. [ ] **Edit-spec post-edit refresh** (was P2.11 sub-item 6). After the user returns from `$EDITOR`, the menu's data is refreshed by re-calling the `SpecReader` signals; the editor-suspension mechanics (`ReleaseTerminal` / `RestoreTerminal`) stay in `internal/tui/` and are format-neutral.
1. [ ] **Edit-spec end-to-end smoke** (was P2.12 sub-item 11). End-to-end test: edit the spec from the session menu, confirm the journal viewer reflects the edited content. Depends on the journal viewer above.

## Implementation plan

Items are intentionally not numbered as P-X.Y; this spec stands on its own.

1. [ ] **Inventory and freeze.** List every import of `internal/spec` outside the planned `markdown_bcc` adapter (i.e., from `loop/`, `tui/`, `executor/`, `cli/`). Each one is a coupling site to remove or adapt. Land the list in this spec, not in code.
1. [ ] **Define `Signal` and the new ports** in `internal/loop/ports.go`. No implementation yet; just types and a doc comment per method explaining the contract.
1. [ ] **Refactor `LoopDecider`** (`internal/loop/decider.go`) to accept `Signal` and `WorkRemaining bool` instead of `spec.Result` and `UncheckedAfter`. Update callers in `internal/loop/loop.go` and tests.
1. [ ] **Carve `markdown_bcc` adapter.** Create `internal/specreader/markdown_bcc/` with the existing parsing code. Move `internal/spec/` types in as `markdown_bcc`-private types. The adapter implements the new `SpecReader` port, mapping its `Result` to `Signal` and `unchecked > 0` to `WorkRemaining`. Wire it from `cmd/bcc/` as the default when `[spec].format = "markdown_bcc"`.
1. [ ] **Decouple TUI panels.** `progressPanel` consumes `(checked, total, ok)` from `Progress()`; degrades to a textless ratio bar when `ok=false`. `riskPanel` reads `LatestSignal()` instead of `spec.Result`. Re-introduce the immediate-on-startup parse only via the new port, not via `parseSpecCmd` directly.
1. [ ] **Define `JournalStore` port.** Implement `markdown_inspec` (today's behavior) as the default adapter. Move journal-append logic out of `internal/spec/` and into the adapter.
1. [ ] **Add `[spec].format` and `[journal].store` to `.bcc.toml`.** Validate at config load. Wire the adapter selection at `cmd/bcc/`. Document defaults.
1. [ ] **Tests.** Adapter round-trip: parse a fixture, assert the right signal and progress emerge. Decider tests: re-express the existing cases against `Signal` (no `spec.Result` import in `internal/loop/`).
1. [ ] **Migration note.** Update `CLAUDE.md`'s architecture section to reflect: `internal/spec/` is gone (or shrunk to truly format-neutral helpers); the new ports are the boundary; `markdown_bcc` is the default adapter, not the assumed format.
1. [ ] **Open questions for follow-ups (do not block this spec).** Whether `Phase` is a TUI-only concept (some formats may not have phases). Whether `WorkRemaining` should be richer (e.g., ETA hints). Whether the loop should support format-specific extensions (e.g., open-spec's task dependencies).

## Done criteria

- `go test -race ./...` clean.
- No package outside `internal/specreader/markdown_bcc/` imports `internal/spec/` (or its successor).
- A second adapter (even a minimal stub) can be added under `internal/specreader/` without editing `loop/`, `tui/`, or `executor/`.
- `.bcc.toml` supports `[spec].format` and `[journal].store`; running with the default values produces the same observable behavior as today.

## Stop criteria

Reverse the work and reopen the design if:

- The signal shape proves too lossy for a real adapter (e.g., bmad cannot be expressed in `Continue/Review/Done/Blocked`). In that case, expand the `Signal` set rather than leaking format types upward.
- TUI panels degrade so far that the dashboard stops being useful for the bcc-markdown default. Adjust the `Progress()` contract to give the panels enough to render.

## Out of scope

- Multi-language localization of the journal storage. Keep this orthogonal; localization is a per-adapter concern.
- A general-purpose plugin system for adapters at runtime. Adapters are compiled-in; selection is via config string, not dynamic loading.
- Migrating existing specs to a different format. Existing `bcc-markdown` specs continue to work as-is.

## Related

- [Skill: fast-iteration spec authoring](./2026-04-29-skill-spec-authoring.md): the prompt-shaping techniques that let the agent get to work faster live in a skill, not in this framework. They are independent of which format the spec uses.

## Execution Journal

Most recent entries on top. Contract in [Autonomous execution guide](../../guides/autonomous-execution.md#execution-journal).

(no entries yet)
