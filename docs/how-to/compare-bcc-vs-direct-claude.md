# Compare bcc vs a direct claude run

`scripts/compare-direct.py` is an external Python script that runs the same
prompt or spec through one or more bcc variants (Director-driven pipeline)
and a direct invocation of the `claude` CLI, then aggregates cost and tokens
for every variant into the vendor-neutral five-bucket shape so you can tell
whether the supervision tax pays for itself, and how two bcc versions compare
on the same workload.

## Prerequisites

- `bcc`, `claude`, and `git` on `PATH`.
- Python 3.11+ (stdlib only; no third-party deps).
- A clean working tree (`git status --porcelain` empty). The script refuses to
  run otherwise so a local change does not contaminate either side.

## Quick start

```bash
# Built-in baseline: a small, deterministic spec that exercises the DAG.
# Default variants: one bcc + one direct.
python scripts/compare-direct.py --baseline diag-dag

# Free-form prompt
python scripts/compare-direct.py --prompt "remove the redundant bcc_ prefix from MCP endpoints"

# Existing spec on disk
python scripts/compare-direct.py --spec docs/specs/example.md
```

## Comparing two bcc versions on the same spec

When you want to measure the cost or behavior delta between two bcc binaries
on the same input, add extra bcc variants. Each variant gets its own
worktree, runs in parallel with the others, and lands as a separate row in
the table.

```bash
# Build the new bcc binary somewhere outside the working tree
go build -o /tmp/bcc-v2 ./cmd/bcc

# Run baseline bcc + new bcc + direct against the same spec in one pass
python scripts/compare-direct.py --baseline diag-dag \
    --bcc-variant baseline \
    --bcc-variant v2=/tmp/bcc-v2
```

Or, if you already ran a comparison and only want to add a new bcc variant
without re-running the others, point `--reuse` at the prior report and skip
the direct side:

```bash
python scripts/compare-direct.py \
    --reuse .compare-worktrees/report-20260511-162736.json \
    --bcc-variant v2=/tmp/bcc-v2 \
    --no-direct
```

`--reuse` pins the base SHA and spec from the prior report so the new variant
runs against the same input as the original ones. The prior rows are folded
into the new table unchanged, and the new variant appears beneath them.

## What it does

1. Records the current `HEAD` SHA (or pins the prior `base_sha` when
   `--reuse` is set).
2. Creates one disposable git worktree per variant under
   `.compare-worktrees/<label>-<ts>`, each on a fresh branch pointing at
   the base SHA.
3. For each `bcc` variant, runs `bcc run --output json --no-color` in its
   worktree using the configured binary. bcc materializes
   `.bcc/sessions/<id>/cost.json` from the live event stream.
4. For the `direct` variant (unless `--no-direct`), runs
   `claude -p --output-format stream-json --verbose
   --dangerously-skip-permissions <prompt>` in its worktree and captures
   the stream-json output to `direct.stream.jsonl`.
5. Aggregates every variant into the same `TokenUsage` shape:
   - bcc: reads `cost.json` from the session directory.
   - claude direct: parses the terminal `result` event from the
     stream-json log; falls back to summing `assistant.message.usage`
     when the run was killed before the result arrived.
6. Computes the diff (files changed, lines added/removed) against the base
   SHA on each worktree, including ignored files the agent produced.
7. Writes a per-variant sidecar at `.compare-worktrees/<label>-<ts>.sidecar.json`,
   then prints the comparison table to stdout. The canonical multi-variant
   report is auto-saved at `.compare-worktrees/report-<ts>.json`; pass
   `--out PATH` to also write it elsewhere.
8. Removes every worktree and disposable branch (skip with `--keep` when
   you want to inspect the working trees afterwards).

## Reading the table

```
side               wall(s)           usd   input_fresh        cached   cache_write        output     reasoning         total         files           +/-
bcc                 245.32        2.3500           225       1536549        142783         27478             0       1707035            12  430/      87
bcc-v2              312.70        2.9100           180       1800000        160000         32000             0       1992180            14  502/     110
direct               87.10        1.4200           312        760000         50000         15000             0        825312             9  290/      45

bcc / direct cost ratio:    1.65x
bcc / direct tokens ratio:  2.07x
bcc-v2 / direct cost ratio: 2.05x
bcc-v2 / direct tokens ratio: 2.41x
```

Reading top to bottom:

- **wall(s)** is real time, not CPU. Director runs do more work
  (planning, briefing, reviewing) so wall time grows.
- **usd** is the provider-reported scalar; bcc sums across all spawns.
- The five token buckets always add up to **total**. **cached** dominates
  whenever caching is healthy, which is most of the time on Anthropic.
  See [the discussion above the cost-meter component](../specs/director/2026-05-02-executable-plan-dag.md)
  if you want the deeper background on bucket semantics.
- **files** / **+/-** measure the actual code change each side made.
  A bcc run that took twice as much money but changed the same files
  with the same correctness is a real signal that the supervision tax
  is too high for that task.

## When to use this

- Decide whether to direct-shell a one-shot task or queue it through bcc.
- Measure cost regressions when bumping the model or adjusting the
  Director prompts.
- Validate a new provider adapter: run the comparator against an existing
  baseline both before and after wiring the adapter to confirm the
  TokenUsage shape stays disjoint.
- Compare two bcc versions on the same spec to measure the impact of a
  Director, prompt, or executor change (`--bcc-variant baseline
  --bcc-variant v2=...`).

## Caveats

- All variants run in parallel, one subprocess per worktree. Network
  jitter and rate limits can affect any variant independently; rerun for
  a more stable signal.
- `--max-iter` defaults to 5 to keep bcc bounded; bump it for spec-heavy
  runs that need more iterations to converge.
- The comparator does **not** evaluate code correctness. Two identical
  totals can still mean one variant produced wrong code; review the diffs
  manually or add a test runner step on top of `--keep` if you need that.
- The bcc binary used by a variant must already exist on disk. Building a
  new version from a dirty working tree before invoking the comparator is
  the supported workflow: `go build -o /tmp/bcc-v2 ./cmd/bcc`, then pass
  `--bcc-variant v2=/tmp/bcc-v2`. The comparator still refuses to run if
  the parent working tree is dirty so the worktrees stay representative
  of the recorded base SHA.
