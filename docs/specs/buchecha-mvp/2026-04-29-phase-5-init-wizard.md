---
title: "Phase 5: form-based `bcc init` wizard"
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
  - phase-5
  - mvp
  - init
  - wizard
  - tui
---

# Phase 5: form-based `bcc init` wizard

## Summary

`bcc init` is rewritten on top of `charm.land/huh/v2` so the project bootstrap experience matches the polish of the live dashboard: per-field validation, sensible defaults visible in-line, multi-group navigation (back / forward), a final review step, and an accessible fallback when the terminal is not a TTY. The pure `WriteConfigTOML(path, initInput)` writer stays unchanged; only the front-end that collects `initInput` is replaced.

## Context and motivation

The current wizard (`internal/cli/init.go`) is a linear `bufio.Reader` loop with one prompt per field, no back navigation, no per-field validation beyond inline `if` checks, and no review step. It works, but the experience is the weakest point of an otherwise polished CLI:

1. A typo ten questions deep means restarting from scratch.
2. Validation errors abort the whole flow.
3. There is no preview of the file that will be written until after writing.
4. Adding fields (Phase 4 wants MCP mode and planner) is a copy-paste-adjust against an organically grown function.
5. Running the wizard inside a non-TTY context (CI, Docker without `-t`) silently degrades.

`huh` solves all five out of the box and is the de-facto Charm form library. The dashboard's Phase 2 spec already commits the project to the v2 Charm stack; adopting `huh.Form` here is consistent and avoids two parallel front-end stacks.

## Goals and non-goals

### Goals

- [ ] `bcc init` opens a `huh.Form` with grouped fields (project, executor, specs, loop, git, env, review).
- [ ] Per-field `Validate(func(T) error)` with concrete messages: language is one of `en` / `pt-BR`; binary path is non-empty and resolvable on PATH or absolute; max iterations is a positive int ≤ 1000; branch prefix matches `[a-z][a-z0-9-]*`; spec dir is a non-empty relative path; env files are valid filenames.
- [ ] Defaults visible in-line on every field, pre-filled from the existing `initInput` defaults.
- [ ] Multi-group flow: back navigation (`shift+tab`) returns to the previous group; forward (`tab`) advances; `enter` submits the current group.
- [ ] Final review group renders the `.bcc.toml` that *will* be written (via `WriteConfigTOML` to a `bytes.Buffer`) plus a `huh.Confirm` ("write this file?"). Cancel returns to the first group.
- [ ] `huh.WithAccessible(true)` is engaged automatically when `os.Stdout` is not a TTY, when `BCC_ACCESSIBLE=1` is set, or when `--accessible` is passed. Output is plain prompts on stdout, suitable for screen readers and CI.
- [ ] `--force` keeps current behavior: existing `.bcc.toml` is overwritten; otherwise the wizard refuses with a one-line message.
- [ ] `--language <code>` keeps current behavior: pre-fills (and skips) the language field.
- [ ] Output byte-equality (modulo trailing newline) with the current wizard for an identical answer set; existing `init_test.go` fixtures pass without changes to `WriteConfigTOML`.
- [ ] Theme: `huh.ThemeCharm()`. `--no-color` (the same flag `bcc run` uses) flips the form to the no-color theme.
- [ ] Phase 4 P4.7 fields (MCP mode, planner) land as additional `huh.Field`s in the executor / loop groups; the wizard surface absorbs them without churning the form skeleton.

### Non-goals

- Editing an existing `.bcc.toml`. A future `bcc config edit` is out of scope here.
- Theme picker. The wizard ships one theme plus the no-color fallback.
- Localizing the wizard prompts. The wizard speaks English; `project.language` is itself a wizard answer but the prompt text stays English (the spec content the user later writes is localized).
- Detecting an existing valid config and offering "merge with new defaults". Out of scope; users re-run with `--force` for now.
- A full TUI dashboard for `bcc init` (multi-pane, live preview as fields are filled). The single right-hand "current TOML preview" pane is tempting but adds complexity; deferred.

## Proposal

### Form structure

```
huh.NewForm(
  Group("Project",       Select language),
  Group("Executor",      Select agent, Input binary, Input model, Confirm skip_permissions, Select mcp_mode),
  Group("Specs",         Input specs_dir),
  Group("Loop",          Select mode, Input max_iterations, Confirm planner_enabled),
  Group("Git",           Input branch_prefix),
  Group("Env",           Text env_files (newline-separated)),
  Group("Review",        Note (rendered TOML preview), Confirm write),
)
```

Each group is one screen. `tab` / `enter` advance, `shift+tab` returns. The Review group is special: its `Note` field is rebuilt from the live answers every time the user lands on it, so back-navigating from Review and changing a value updates the preview on re-entry.

### Field-by-field detail

| Group | Field | Type | Default | Validation |
|---|---|---|---|---|
| Project | language | `huh.Select[string]` | `en` (or `--language`) | one of `en`, `pt-BR` |
| Executor | agent | `huh.Select[string]` | `claude` | one of `claude`, `codex`, `gemini`, `custom` |
| Executor | binary | `huh.Input` | `exec.LookPath(agent)` or agent name | non-empty; resolvable via `exec.LookPath` OR absolute path that `os.Stat` finds |
| Executor | model | `huh.Input` | `claude-opus-4-7` when agent is `claude`, empty otherwise | optional; non-empty when agent is `claude` |
| Executor | skip_permissions | `huh.Confirm` | true | none (boolean) |
| Executor | mcp_mode | `huh.Select[string]` | `isolated` | one of `isolated`, `inherit`, `disabled` (Phase 4 vocabulary) |
| Specs | dir | `huh.Input` | `docs/specs` | non-empty; relative path; no `..` segments |
| Loop | mode | `huh.Select[string]` | `phase` | one of `phase`, `single-shot` |
| Loop | max_iterations | `huh.Input` | `20` | parses as positive int ≤ 1000 |
| Loop | planner_enabled | `huh.Confirm` | false | none |
| Git | branch_prefix | `huh.Input` | `feat` | matches `^[a-z][a-z0-9-]*$` |
| Env | files | `huh.Text` | `.env` | each non-empty line is a valid filename (no path separators on Windows-shaped inputs) |
| Review | preview | `huh.Note` | rendered `WriteConfigTOML` output | none (display only) |
| Review | write | `huh.Confirm` | true | none |

The skip_permissions field gets a `Description` that mirrors the existing wizard's multi-line warning text; `huh.Confirm` supports a long description below the question.

### Accessible mode

`huh.WithAccessible(true)` is engaged when any of the following is true:

1. `--accessible` is passed.
2. `BCC_ACCESSIBLE=1` is in the environment.
3. `os.Stdout` is not a TTY (`golang.org/x/term.IsTerminal(int(os.Stdout.Fd()))` returns false).

Accessible mode prints one prompt per line on stdout, accepts answers via stdin, and skips the alt-screen entirely. Validation errors print on the same line and re-ask. The Review group prints the rendered TOML with a `Confirm? [y/N]` line at the end.

### Theming

Form theme defaults to `huh.ThemeCharm()`. `--no-color` (mapped from the same flag `bcc run` accepts) switches to `huh.ThemeBase()` so output is ANSI-free. The theme is selected once at form construction; runtime toggles are out of scope.

### Output and idempotency

The wizard never writes the file until the Review group's `Confirm` is true. At that point, `WriteConfigTOML(target, in)` is called once. The pure writer is **not modified** in this phase: byte-for-byte equality with current `init_test.go` fixtures is a green-bar invariant. New fields (mcp_mode, planner) require minimal additions to `initInput` and `WriteConfigTOML` to round-trip them; that diff lives in P5.7 and stays additive.

### CLI surface

```bash
bcc init [flags]
  --force                   # overwrite existing .bcc.toml
  --language <code>         # pre-fill the language field (en | pt-BR)
  --accessible              # force huh accessible mode
  --no-color                # force huh.ThemeBase() (ANSI-free)
```

Behavior on existing file: as today, refuse without `--force`.

### Architecture

```mermaid
graph LR
    CMD[cmd init.go RunE] --> WIZ[runInitWizard]
    WIZ --> FORM[huh.Form: 7 groups]
    FORM --> A1[Project]
    FORM --> A2[Executor]
    FORM --> A3[Specs]
    FORM --> A4[Loop]
    FORM --> A5[Git]
    FORM --> A6[Env]
    FORM --> A7[Review]
    A7 --> WRITE[WriteConfigTOML]
    WRITE --> FILE[.bcc.toml]
    WIZ -.--> ACC[Accessible mode dispatcher]
    ACC --> FORM
```

### Package layout impact

```
internal/cli/
├── init.go              # cobra command + thin dispatcher
├── init_form.go         # huh.Form construction; pure (returns Form, *initInput)
├── init_validate.go     # field validators (table-tested)
├── init_accessible.go   # decision for huh.WithAccessible
└── init_test.go         # existing tests + new ones
```

Splitting the file keeps each piece small and unit-testable without driving stdin.

## Alternatives considered

### Alternative 1: stay on the linear stdin loop and just add MCP / planner fields

- **Description**: Phase 4 P4.7 already plans this. Two extra `ask(...)` calls and a regenerated golden test.
- **Pros**: Smallest diff. No new dependency.
- **Cons**: Locks in the no-back-nav, no-validation, no-review experience as the wizard accumulates fields; the second time we add fields the same pressure shows up. Misses the chance to land on the same widget stack as the dashboard.
- **Why discarded**: The wizard is the user's first impression of `bcc`. It deserves the same polish bar as `bcc run`.

### Alternative 2: `gum` shelled out from a thin Go wrapper

- **Description**: Compose the wizard from `gum input`, `gum choose`, `gum confirm` invocations.
- **Pros**: Zero Go code beyond the orchestration.
- **Cons**: Adds an external binary dependency. No back navigation across `gum` invocations (each is one-shot). No final review step shy of writing a temp file. The CLI guarantees in CLAUDE.md (single binary, no shell-out) reject this on principle.
- **Why discarded**: Same widgets exist as Go libraries; we use them directly.

### Alternative 3: full bubbletea Model with hand-rolled forms

- **Description**: Skip `huh` and write a custom multi-screen Model.
- **Pros**: Maximum flexibility; matches the dashboard's hand-rolled approach.
- **Cons**: Reinvents Field, Validate, accessible mode, and theming, none of which the dashboard needs to share with the wizard. The wizard is a closed-form interaction; `huh` is purpose-built for it.
- **Why discarded**: Wrong tool for the job; the dashboard is a continuous live view, the wizard is a form. Different shapes, different libraries.

## Implementation Plan

### P5.1: dependency and scaffold

1. [ ] Add `charm.land/huh/v2` (latest stable) to `go.mod`. Verify `go mod tidy`, `go vet`, `gofmt -l`, `go test -race ./...` all clean after the add (no usage yet).
1. [ ] Split `internal/cli/init.go` into `init.go` (cobra wiring), `init_form.go` (Form construction stub), `init_validate.go` (validators stub), `init_accessible.go` (accessible-mode dispatcher stub). Empty stubs return `errNotImplemented`; existing tests are temporarily rerouted to the legacy code path behind a private flag so they keep passing.

### P5.2: validators

1. [ ] `init_validate.go`: pure functions per field (`validateLanguage`, `validateBinary`, `validateModel`, `validateSpecsDir`, `validateMaxIter`, `validateBranchPrefix`, `validateEnvFiles`). Each takes the typed value and returns `error`. No I/O except `exec.LookPath` and `os.Stat` for the binary validator.
1. [ ] Table-driven tests in `init_validate_test.go` covering positive and negative cases per field, including edge cases (empty string, leading whitespace, `..` in paths, non-existent binary on PATH, max_iterations 0 / 1001 / negative, non-int).

### P5.3: form construction

1. [ ] `init_form.go`: `func newInitForm(defaults initInput) (*huh.Form, *initInput)` returns a configured `huh.Form` plus a pointer to the answer struct. All 7 groups wired with the fields from the table above. Each `Validate` is the corresponding validator from P5.2.
1. [ ] Group titles and field descriptions in English. The skip_permissions field carries the existing multi-line warning text as its `Description`.
1. [ ] Theme: `huh.ThemeCharm()` by default; `huh.ThemeBase()` when the no-color flag is set.
1. [ ] Tests: `newInitForm` returns a non-nil form; running the form with scripted input via `huh.NewForm(...).WithInput(strings.NewReader(...))` produces the expected `initInput`. (Use `huh`'s built-in input scripting, not stdin redirection.)

### P5.4: review group

1. [ ] Review group has one `huh.Note` and one `huh.Confirm`. The `Note.Description` is computed via `WriteConfigTOML` to a `bytes.Buffer` (the pure writer accepts a `io.Writer` companion overload added in P5.7).
1. [ ] Re-rendering on back-navigation: the form's `WithProgramOptions` or per-group hook regenerates the Note's body when the group is entered. Verify with a scripted test that changes a value, navigates to Review, navigates back, changes again, returns: the second preview reflects the second change.

### P5.5: accessible mode and CLI flags

1. [ ] `init_accessible.go`: `func shouldUseAccessibleMode(envLookup func(string) string, isTTY func() bool, accessibleFlag bool) bool` is pure and table-tested.
1. [ ] `init.go`: cobra adds `--accessible` and `--no-color` flags. The dispatcher chooses `huh.WithAccessible(true)` based on the function above; `--no-color` selects the base theme.
1. [ ] Smoke test: run with `os.Stdout` redirected to a `bytes.Buffer` (forces non-TTY), assert form runs in accessible mode and writes to the buffer line by line.

### P5.6: cobra wiring and dispatch

1. [ ] `cmd/init.go` (or equivalent): `RunE` builds the defaults (existing `initInput` shape), constructs the form via `newInitForm`, runs it (`form.Run()`), and on success calls `WriteConfigTOML(target, *answers)`. On user cancellation (`huh.ErrUserAborted`) prints "init cancelled" and returns nil with exit code 0.
1. [ ] `--force` checked once at the top before constructing the form (current behavior).
1. [ ] `--language` pre-fills `defaults.Language` and the language `Select` is rendered with that value; user can still change it.

### P5.7: writer additions for new fields

1. [ ] Extend `initInput` with `MCPMode string` and `PlannerEnabled bool`.
1. [ ] Extend `WriteConfigTOML` to emit `[executor].mcp_mode` and `[loop].planner_enabled`. Existing golden fixtures get one new line each; rebaselined in the same commit.
1. [ ] `internal/config/config.go` gains the corresponding TOML tags so the loader round-trips them. Defaults: `mcp_mode = "isolated"`, `planner_enabled = false`. Phase 4's resolver consumes these; this phase only ensures the wizard writes them.
1. [ ] Mark Phase 4 P4.7 as superseded by P5.6 and P5.7 (via a one-line note in P4.7, not in the spec body of Phase 4).

### P5.8: docs and discovery

1. [ ] Add a `docs/guides/init-wizard.md` walkthrough with a screenshot of the form (recorded with `vhs` or similar; if recording is out of band, link to a hosted asset). Includes the `--accessible` and `--no-color` flags.
1. [ ] `README.md` `bcc init` section gains one paragraph: per-field validation, back navigation, review step, accessible fallback.
1. [ ] Update `docs/specs/buchecha-mvp/index.md` table to include this spec.

## Autonomous execution

This spec follows the [Autonomous execution guide](../../guides/autonomous-execution.md) defaults.

### Done criteria

Default Go criteria (`gofmt`, `go vet`, `go test -race`, `go build`) plus:

1. `init_test.go` golden TOML output matches the pre-rework fixture for an identical answer set (proves writer byte-equality).
1. Manual run of `bcc init` in three environments: interactive terminal (TTY, full huh form), CI-shaped (non-TTY, accessible mode), and `BCC_ACCESSIBLE=1` (forced accessible). All three produce the same `.bcc.toml` for the same answers.
1. Running `bcc init` and pressing `shift+tab` from any group returns to the previous group with previously entered values intact.

### Stop criteria

1. **Success**: P5.1 through P5.8 all `[x]` and the manual three-environment run passes.
1. **Block**: huh API surprise (back navigation, programmatic input, accessible-mode dispatch) that needs design rethink.
1. **Human decision**: theme choice if the default `huh.ThemeCharm()` clashes with the dashboard palette in P2.10. Resolved by selecting one theme constant for both surfaces.

## Risks and mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| huh v2 API drift (recently moved to `charm.land/huh/v2`) | Medium | Medium | Pin exact tag in `go.mod`; lock the import path explicitly; CI runs `go mod verify` |
| Accessible mode rendering differs across screen readers | Low | Low | huh delegates to plain `fmt.Fprint` lines; manual test with VoiceOver and `script -q` capture |
| Back navigation loses field values mid-edit | Medium | Low | Form holds answers in the bound struct; covered by the scripted P5.4 test |
| TTY detection wrong on Windows / git-bash | Low | Medium | Use `golang.org/x/term.IsTerminal` with a fallback to `BCC_ACCESSIBLE=1` env override |
| Validation message style inconsistent across fields | Low | Low | All validators return `errors.New("<field>: <reason>")` and a lint test asserts the prefix |
| Phase 4 P4.7 already merged before P5 ships | Low | Low | P4.7 explicitly delegates the new fields to P5.6/P5.7; if P4.7 lands first, the wizard absorbs them via plain `ask` calls and P5 re-homes them in the form |

## References

- huh v2: `charm.land/huh/v2` ([github.com/charmbracelet/huh](https://github.com/charmbracelet/huh))
- bubbletea v2: `charm.land/bubbletea/v2` (huh.Form embeds as a `tea.Model`)
- Phase 2 TUI dashboard: [2026-04-29-phase-2-tui-dashboard.md](./2026-04-29-phase-2-tui-dashboard.md) (commits the project to the v2 Charm stack)
- Phase 4 execution tuning: [2026-04-29-phase-4-execution-tuning.md](./2026-04-29-phase-4-execution-tuning.md) (P4.7 lands MCP / planner fields via this phase)
- Current wizard: `internal/cli/init.go`
- Pure writer (unchanged in this phase): `WriteConfigTOML` in the same file

## Execution Journal

(empty until first execution)
