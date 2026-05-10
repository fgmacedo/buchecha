# Compare bcc vs a direct claude run

`scripts/compare-direct.py` is an external Python script that runs the same
prompt or spec through bcc (Director-driven pipeline) and through a single
direct invocation of the `claude` CLI, then aggregates cost and tokens for
both into the vendor-neutral five-bucket shape so you can tell whether the
supervision tax pays for itself on a given workload.

## Prerequisites

- `bcc`, `claude`, and `git` on `PATH`.
- Python 3.11+ (stdlib only; no third-party deps).
- A clean working tree (`git status --porcelain` empty). The script refuses to
  run otherwise so a local change does not contaminate either side.

## Quick start

```bash
# Built-in baseline: a small, deterministic spec that exercises the DAG
python scripts/compare-direct.py --baseline diag-dag

# Free-form prompt
python scripts/compare-direct.py --prompt "remove the redundant bcc_ prefix from MCP endpoints"

# Existing spec on disk
python scripts/compare-direct.py --spec docs/specs/example.md
```

## What it does

1. Records the current `HEAD` SHA.
2. Creates two disposable git worktrees under `.compare-worktrees/`, one
   per side, each on a fresh branch pointing at the recorded SHA.
3. Runs `bcc run --output json --no-color` in the first worktree with the
   prompt or spec; bcc materializes `.bcc/sessions/<id>/cost.json` from the
   live event stream.
4. Runs `claude -p --output-format stream-json --verbose
   --dangerously-skip-permissions <prompt>` in the second worktree and
   captures the stream-json output to `direct.stream.jsonl`.
5. Aggregates both sides into the same `TokenUsage` shape:
   - bcc: reads `cost.json` from the session directory.
   - claude direct: parses the terminal `result` event from the
     stream-json log; falls back to summing `assistant.message.usage`
     when the run was killed before the result arrived.
6. Computes `git diff --shortstat` against the base SHA on each worktree
   so the table shows files changed and lines added/removed too.
7. Prints a side-by-side comparison table to stdout. With `--out report.json`
   it also writes a JSON report for CI or longitudinal tracking.
8. Removes both worktrees and their disposable branches (skip with `--keep`
   when you want to inspect the working trees afterwards).

## Reading the table

```
side       wall(s)           usd  input_fresh        cached   cache_write       output    reasoning        total       files          +/-
bcc           245.32        2.350          225      1536549        142783        27478            0      1707035          12        430/87
direct         87.10        1.420          312       760000         50000        15000            0       825312           9        290/45

bcc / direct cost ratio: 1.65x
bcc / direct tokens ratio: 2.07x
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

## Caveats

- The two sides run sequentially (first bcc, then claude). Network jitter
  and rate limits can affect either side independently; rerun for a more
  stable signal.
- `--max-iter` defaults to 5 to keep bcc bounded; bump it for spec-heavy
  runs that need more iterations to converge.
- The comparator does **not** evaluate code correctness. Two identical
  totals can still mean one side produced wrong code; review the diffs
  manually or add a test runner step on top of `--keep` if you need that.
