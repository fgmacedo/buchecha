---
title: "bcc cooperative validation smoke"
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
  - validation
  - smoke
  - test
---

# bcc cooperative validation smoke

## Summary

Cooperative end-to-end test of the bcc autonomous loop. The agent (sub-claude invoked by `bcc run`) **knows** it is being observed and is expected to report back with structured feedback in every journal entry. The observer (Claude in the parent Claude Code session that triggered `bcc run`) reads the JSONL stream and the journal, may edit this spec between iterations to refine or expand the protocol based on what was learned, and is the human's eyes on what is happening.

This is not a typical autonomous run. The goals are NOT just to deliver the implementation — they are to validate that bcc works end to end and to surface friction in the tool itself.

## Cooperative protocol

### What the agent (you) should do

1. Treat every iteration as a probe of the bcc tooling, not just a feature delivery. You are co-validating the product.
1. In every journal entry, include a `**Notes for observer**` section with at least:
   - **Prompt experience**: did the prompt feel clear? Anything missing or contradictory?
   - **Env / config**: what env vars are present? Did `CLAUDE_CONFIG_DIR` point where you expected? Anything off?
   - **Friction**: at least one concrete thing that felt awkward in this iteration, even if minor. If you found nothing, say so explicitly.
   - **Suggestions for bcc**: zero or more concrete improvements you'd make if you owned the bcc codebase.
1. If the spec is unclear, do NOT guess. Set `Result: blocked`, put the question in `**Decisions**`, and exit. The observer will edit and re-trigger.
1. Mark `[x]` only on items fully delivered. Discoveries become new sub-items as usual.
1. You are running in `~/projects/buchecha`, branch `feat/phase-1`. You may commit on this branch; the observer is in read-only mode while bcc is running.

### What the observer does between iterations

1. Reads the JSONL stream (`/var/folders/.../bcc/<slug>-iter<N>.jsonl` or wherever `os.TempDir()` resolves), the latest journal entry, and the `git log` of new commits.
1. May edit this spec to:
   - Fill `P2` (placeholder; observer-driven).
   - Add an `### Observer guidance` block at the top of the next phase to steer the agent.
   - Add new phases.
1. Reports back to the human user.

### What is out of scope here

- Production-grade implementation. Tasks are deliberately small and low-risk.
- Touching files outside `~/projects/buchecha` or anything credential-bearing.
- The absolute restrictions in `docs/guides/autonomous-execution.md` apply unchanged.

## Implementation Plan

### P1: Smoke round-trip

1. [x] Read this repository's `README.md` and write a 3-sentence summary of what `buchecha` is, in plain English, to `testdata/bcc-validation/summary.md`. The summary should be understandable to someone who has never seen the project.
1. [x] Add a `**Notes for observer**` section to your journal entry as described in the cooperative protocol above. Be specific; vague feedback is worse than no feedback.

### P2: Observer-driven iteration

The observer will fill this phase after reading the P1 results. Treat as `[ ]` placeholder until the observer adds concrete sub-items.

1. [ ] (placeholder; observer fills before next iteration)

## Autonomous execution

Follows the [Autonomous execution guide](../../guides/autonomous-execution.md) defaults except for the cooperative protocol described above (which adds journaling requirements; it does not relax any rule).

### Done criteria

1. P1 fully `[x]` in plan.
1. `testdata/bcc-validation/summary.md` exists with the 3-sentence summary.
1. Journal has at least one `**Notes for observer**` block per iteration.
1. `gofmt -l ./...` empty, `go vet ./...` clean, `go test -race ./...` zero failures (do not break existing code).

### Stop criteria

1. **Success**: when the observer marks `Result: done` in the plan after P2 is filled and delivered (this is unusual; the observer will typically extend or stop the test).
1. **Block**: spec unclear, env broken, claude API errors, or absolute restriction temptation.
1. **Human decision**: observer sees something that needs offline discussion.

## Execution Journal

### 2026-04-29 12:52, P2: Observer-driven iteration

- **Result**: blocked
- **Summary**: P2's only sub-item is the literal placeholder `(placeholder; observer fills before next iteration)`. The cooperative protocol forbids guessing on unclear spec content, so the iteration stops without modifying the plan or the working tree (other than this journal entry). The previous entry predicted this exact outcome.
- **Commits**: this commit docs(spec): block on P2 awaiting observer-defined sub-items
- **Decisions**: Did not invent sub-items for P2. Did not delete or rewrite the placeholder; that is the observer's edit. Did not mark any checkbox `[x]` because no work was delivered. Did not touch `testdata/bcc-validation/` or any other file.
- **Problems**: (none, this is a designed-in stop point)
- **Next**: P2: Observer-driven iteration (awaiting observer to replace the placeholder with concrete `[ ]` sub-items, then re-trigger `bcc run`).
- **Notes for observer**:
  - **Prompt experience**: The launch prompt is identical to P1's (same ordinal-less, JSONL-path-less body). Two small frictions specific to a `blocked` iteration: (1) the prompt's procedure says "Implement that phase end to end" before "Identify the next phase", which reads as if implementation is the assumed branch; a reader optimizing for speed could miss the cooperative protocol's "do not guess, block" rule. Putting "If the next phase has only a placeholder, set `Result: blocked` and exit" as an explicit step in the procedure (or in a pre-check) would harden it. (2) The prompt's strict-rules block emphasizes `[x]` discipline and `Result` values for `ok`/`partial`/`done`, but says less about the `blocked` flow; a one-liner reminding the agent that blocked iterations still produce a journal entry and a single commit would close the gap.
  - **Env / config**: Re-checked. `pwd=/Users/fernando.macedo/projects/buchecha`. Only `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_EXECPATH`, `CLAUDE_CONFIG_DIR=/Users/fernando.macedo/.claude-pessoal`, `CLAUDECODE=1` are set. Still **no `BCC_*` env vars**, confirming P1's observation: the agent has no machine-readable signal that it is running under bcc, no iteration index, no spec path, no JSONL path. The observer's previous suggestion (#1 in P1) remains unaddressed; if you want the agent to self-verify or include the JSONL path in this entry, that change is still pending.
  - **Friction**:
    1. The placeholder line `1. [ ] (placeholder; observer fills before next iteration)` is a `[ ]` item from the loop's perspective; the loop will keep dispatching iterations until the observer edits the spec. A blocked result correctly stops the loop, but a future improvement could be a spec-level convention (e.g., a fenced "observer-only" tag) that the loop recognizes and skips without dispatching an iteration. Cheaper than another full agent invocation.
    2. The journal entry I'm writing has to record `**Commits**: this commit ...` again, same workaround as P1. It is not wrong, but each blocked iteration that produces only a journal commit re-hits the same self-referential awkwardness. Suggestion #3 from P1 (a documented `<HEAD>` placeholder convention) would resolve it.
    3. Within this iteration there is no way to confirm I'm actually running under `bcc run` (vs. the human dropping into a terminal and running me directly with the same prompt). For a cooperative validation smoke, that distinction matters; the absence of a `BCC_RUNNING=1` flag means I cannot assert the assumption.
  - **Suggestions for bcc**:
    1. **Fast-path for placeholder-only phases.** When the next phase contains only one sub-item whose body matches a configured "placeholder" pattern (e.g., starts with `(placeholder`), the loop could short-circuit to a `blocked` outcome itself, write a synthetic journal entry, and exit. Saves one full agent invocation per "waiting on observer" stop. Optional and behind a config flag, since it adds complexity to the loop.
    2. **Iteration metadata in the prompt header.** Even without env vars, prefacing the prompt with a fixed block like `Iteration: 2 / spec=docs/specs/.../foo.md / branch=feat/phase-1 / jsonl=/var/folders/.../bcc-foo-iter2.jsonl` would let the agent breadcrumb that into the journal and let the observer correlate JSONL and journal trivially.
    3. **Spec-level "observer checkpoint" as a first-class concept.** Right now the cooperative protocol piggybacks on `Result: blocked`, which is also used for technical blocks and absolute-restriction temptations. A dedicated `Result: review` (or similar) would let the outer loop and observer tell apart "the human/observer must edit the spec" from "something went wrong." Reuses the same stop mechanic, distinct semantics.
    4. **Prompt should mention the spec's stop criteria explicitly.** The cooperative protocol's "do not guess, block" rule is in the spec body, not in the prompt. A line like "Honor the spec's own stop criteria; if it tells you to block on a placeholder, do that and exit" would prevent a future agent from over-interpreting the placeholder as work to invent.
    5. **Surface the fact that the loop will read this entry next.** A short reminder in the prompt that the next iteration's agent will read `**Decisions**` to avoid undoing choices is a good nudge for thoughtful authoring of that field, especially in `blocked` entries where the temptation is to keep them sparse.

### 2026-04-29 12:50, P1: Smoke round-trip

- **Result**: ok
- **Summary**: Wrote a 3-sentence plain-English description of buchecha to `testdata/bcc-validation/summary.md` derived from `README.md`, then journaled this iteration with observer notes. Both P1 sub-items now `[x]`.
- **Commits**: 7791e93 test: write 3-sentence buchecha summary for cooperative smoke; this commit docs(spec): journal P1 of cooperative smoke with observer notes
- **Decisions**: Split the phase into two small commits along the two sub-items rather than bundling, matching the granularity of recent commits on `feat/phase-1` (e.g., `cli/run:`, `cli/init:`, `executor/claude:`). The journal entry's `Commits` line names the second commit by description because its hash is only known after the commit lands.
- **Problems**: (none)
- **Next**: P2: Observer-driven iteration (still a placeholder; observer should fill before the next run, or the next agent will block on the placeholder sub-item).
- **Notes for observer**:
  - **Prompt experience**: The launch prompt was clear and tightly scoped. The strict rules block (do not mark `[x]` partially, the binary "implement now or add a `[ ]` sub-item" rule, the strict `**Result**` values) was easy to follow because the spec body restates the same contract. Two minor frictions: (1) the prompt told me the JSONL path is "printed at start" per the guide, but I'm running inside the iteration so I never see that printout, which means I cannot mention the JSONL location in this journal entry as a breadcrumb for the observer; (2) the prompt does not say which iteration index this is, so the journal heading timestamp is the only ordinal I can offer.
  - **Env / config**: `pwd` is `/Users/fernando.macedo/projects/buchecha` as expected. `CLAUDE_CONFIG_DIR=/Users/fernando.macedo/.claude-pessoal`, which is non-default (the conventional path is `~/.claude`); I assume this is intentional and reflects an isolated profile the user runs `bcc` under, but worth confirming. `ANTHROPIC_API_KEY` is unset, so auth flows through the Claude profile in `CLAUDE_CONFIG_DIR`. The only Claude-related env vars present are `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_EXECPATH`, `CLAUDE_CONFIG_DIR`, `CLAUDECODE`. There is no `BCC_*` env var: the agent has no machine-readable signal that it is running under bcc, no iteration index, no spec path, no JSONL path.
  - **Friction**:
    1. No `BCC_ITERATION` / `BCC_SPEC_PATH` / `BCC_JSONL_PATH` exposed to the agent. I have to recover them from the prompt body or from `date`. Self-locating env vars would let the agent log them in the journal and self-check (e.g., refuse to act if it does not see them).
    2. The prompt asks the agent to "Read the entire spec, especially '## Implementation Plan' and '## Execution Journal'." For tiny specs that's fine, but it does not scale; a hint to read selectively (front matter, plan headings, current phase, journal head) would help on larger specs.
    3. The `**Commits**` requirement is awkward when an iteration has multiple commits and the journal entry itself is in the last commit: the last commit's hash is not knowable at write time. I'm working around it with the literal `this commit`, but a documented convention (or a post-commit hook that rewrites `<HEAD>` placeholders) would be cleaner.
  - **Suggestions for bcc**:
    1. Set `BCC_ITERATION`, `BCC_SPEC_PATH`, `BCC_JSONL_PATH`, `BCC_BRANCH`, `BCC_RUNNING=1` in the agent subprocess environment. Cheap, lets the agent self-verify and breadcrumb.
    2. Print the JSONL path inside the launch prompt (or via stderr that the agent sees) so the agent can include it in `**Notes for observer**`. Helps the observer locate the right log file.
    3. In `docs/guides/autonomous-execution.md`, add a one-line convention for the `**Commits**` field when the journal lives in the final commit of the iteration (e.g., literal `this commit` or `<HEAD>` and the loop tolerates it).
    4. Consider a pre-flight assertion in the prompt: "the working tree is clean" plus "you must end clean too." The guide implies it; making it explicit at the prompt boundary catches lazy iterations.
    5. Long-term: an opt-in journal stub the loop can pre-write (heading + `Result: blocked` placeholder) so a crashed iteration leaves the loop in a recoverable state instead of a missing entry.
