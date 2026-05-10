#!/usr/bin/env python3
"""Compare a bcc run against a direct claude invocation on the same prompt/spec.

The script provisions two disposable git worktrees (one per side) from the
current HEAD, runs bcc in one and a single claude invocation in the other,
then aggregates cost and tokens for both into the same vendor-neutral
5-bucket shape so the difference is meaningful.

Usage:
    python scripts/compare-direct.py --prompt "describe the bug fix"
    python scripts/compare-direct.py --spec testdata/specs/diag-dag.md
    python scripts/compare-direct.py --baseline diag-dag

Reads only the artifacts bcc already writes (`.bcc/sessions/<id>/cost.json`)
and the stream-json the claude CLI emits, so the comparator is fully outside
the bcc core. Requires `bcc`, `claude`, and `git` on PATH; Python 3.11+.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterable


# 5-bucket vendor-neutral TokenUsage. Mirrors agentcontract.TokenUsage so
# the comparator can reuse one shape for bcc and direct-claude side. The
# Anthropic stream-json carries four billable buckets (input_tokens,
# output_tokens, cache_read_input_tokens, cache_creation_input_tokens);
# the mapping is 1:1 with input_tokens => input_fresh,
# cache_read_input_tokens => input_cached, cache_creation_input_tokens
# => cache_write, output_tokens => output. reasoning is 0 today (extended
# thinking would surface it).
@dataclass
class TokenUsage:
    input_fresh: int = 0
    input_cached: int = 0
    cache_write: int = 0
    output: int = 0
    reasoning: int = 0
    provider: str = ""

    def total(self) -> int:
        return (
            self.input_fresh
            + self.input_cached
            + self.cache_write
            + self.output
            + self.reasoning
        )

    def add(self, other: "TokenUsage") -> "TokenUsage":
        return TokenUsage(
            input_fresh=self.input_fresh + other.input_fresh,
            input_cached=self.input_cached + other.input_cached,
            cache_write=self.cache_write + other.cache_write,
            output=self.output + other.output,
            reasoning=self.reasoning + other.reasoning,
            provider=self.provider or other.provider,
        )


@dataclass
class RunResult:
    label: str
    wall_time_s: float
    total_usd: float
    tokens: TokenUsage
    files_changed: int = 0
    lines_added: int = 0
    lines_removed: int = 0
    extra: dict = field(default_factory=dict)


def run(
    args: list[str], cwd: Path | None = None, env: dict | None = None
) -> subprocess.CompletedProcess[str]:
    """Run a subprocess synchronously and return the completed process.

    stdout and stderr are captured as text. Non-zero exit codes are not
    raised: callers inspect returncode and interpret partial output (a
    crashed bcc run still leaves an events.ndjson with usage).
    """
    return subprocess.run(
        args,
        cwd=cwd,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )


def must_run(args: list[str], cwd: Path | None = None) -> str:
    """Run a subprocess and raise on non-zero exit. Returns stdout."""
    proc = run(args, cwd=cwd)
    if proc.returncode != 0:
        sys.stderr.write(
            f"command failed: {' '.join(args)}\nstdout:\n{proc.stdout}\nstderr:\n{proc.stderr}\n"
        )
        raise RuntimeError(f"command exited {proc.returncode}: {args[0]}")
    return proc.stdout


def repo_root() -> Path:
    """Return the absolute path of the repository root, derived from the
    location of this script: <root>/scripts/compare-direct.py."""
    return Path(__file__).resolve().parent.parent


def working_tree_clean(repo: Path) -> bool:
    out = must_run(["git", "status", "--porcelain"], cwd=repo)
    return out.strip() == ""


def head_sha(repo: Path) -> str:
    return must_run(["git", "rev-parse", "HEAD"], cwd=repo).strip()


def make_worktree(repo: Path, label: str, base_sha: str) -> tuple[Path, str]:
    """Create a temporary worktree at <repo>/.compare-worktrees/<label>-<ts>
    pointing at base_sha on a disposable branch, and return (path, branch).

    The worktree directory sits inside the repo so the bcc binary still
    sees a valid git repo and can run the same flow as a regular checkout.
    """
    ts = time.strftime("%Y%m%d-%H%M%S")
    branch = f"compare-{label}-{ts}"
    parent = repo / ".compare-worktrees"
    parent.mkdir(exist_ok=True)
    path = parent / f"{label}-{ts}"
    must_run(
        ["git", "worktree", "add", "-b", branch, str(path), base_sha],
        cwd=repo,
    )
    return path, branch


def remove_worktree(repo: Path, path: Path, branch: str) -> None:
    """Delete the worktree and its disposable branch. Errors are logged
    and swallowed: cleanup is best-effort so a failure here does not
    mask the actual run result."""
    try:
        run(["git", "worktree", "remove", "--force", str(path)], cwd=repo)
    except Exception as exc:  # noqa: BLE001 - cleanup is best-effort
        sys.stderr.write(f"worktree remove failed: {exc}\n")
    try:
        run(["git", "branch", "-D", branch], cwd=repo)
    except Exception as exc:  # noqa: BLE001
        sys.stderr.write(f"branch delete failed: {exc}\n")


DIFF_EXCLUDE_PREFIXES = (
    ".bcc/",          # bcc session artifacts (cost.json, plan.json, etc.)
    ".bcc.toml",      # comparator-injected config patch
    "direct.stream.jsonl",  # comparator-captured claude stream log
)


def diff_stat(repo: Path, base_sha: str) -> tuple[int, int, int]:
    """Compute (files_changed, lines_added, lines_removed) for the
    worktree at `repo` against base_sha. Counts tracked changes, plus
    untracked-and-non-ignored files (`git add -N`), plus gitignored
    files the agent created (specs in this repo write outputs to
    ignored fixture dirs like testdata/diag-dag-output/, so a plain
    diff would report 0 even when the agent wrote real files).

    Framework artifacts under DIFF_EXCLUDE_PREFIXES are filtered out
    so the count reflects what the agent did on the project, not what
    the harness wrote alongside.

    For ignored paths we count file count + line additions by reading
    them off the filesystem; deletions of ignored paths are not
    detected because their pre-image is by definition never tracked."""
    must_run(["git", "add", "-N", "--", "."], cwd=repo)
    # Per-file numstat so we can filter framework artifacts before summing.
    out = must_run(
        ["git", "diff", "--numstat", base_sha, "--"],
        cwd=repo,
    )
    files = added = removed = 0
    for line in out.splitlines():
        parts = line.split("\t", 2)
        if len(parts) != 3:
            continue
        a_str, d_str, path = parts
        if path.startswith(DIFF_EXCLUDE_PREFIXES):
            continue
        files += 1
        if a_str != "-":
            added += int(a_str)
        if d_str != "-":
            removed += int(d_str)

    ignored = must_run(
        ["git", "ls-files", "--others", "--ignored", "--exclude-standard"],
        cwd=repo,
    )
    for rel in ignored.splitlines():
        rel = rel.strip()
        if not rel or rel.startswith(DIFF_EXCLUDE_PREFIXES):
            continue
        path = repo / rel
        try:
            data = path.read_bytes()
        except (OSError, IsADirectoryError):
            continue
        files += 1
        if data:
            added += data.count(b"\n") + (0 if data.endswith(b"\n") else 1)
    return files, added, removed


# ---------------------------------------------------------------------------
# bcc side
# ---------------------------------------------------------------------------


def patch_max_iterations(toml_path: Path, max_iter: int) -> None:
    """Replace `max_iterations = N` under `[loop]` in a .bcc.toml in
    place. If the file or section is missing, append a fresh `[loop]`
    block. Targeted regex edit so comments and formatting elsewhere in
    the file survive."""
    if not toml_path.exists():
        toml_path.write_text(f"[loop]\nmax_iterations = {max_iter}\n")
        return
    body = toml_path.read_text()
    pattern = re.compile(
        r"(^\[loop\][^\[]*?^\s*max_iterations\s*=\s*)\d+",
        re.MULTILINE | re.DOTALL,
    )
    new_body, n = pattern.subn(rf"\g<1>{max_iter}", body, count=1)
    if n == 0:
        sep = "" if body.endswith("\n") else "\n"
        new_body = f"{body}{sep}\n[loop]\nmax_iterations = {max_iter}\n"
    toml_path.write_text(new_body)


def run_bcc(
    worktree: Path,
    prompt: str | None,
    spec: Path | None,
    max_iter: int,
    parent_toml: Path | None,
) -> RunResult:
    """Run bcc inside `worktree` and return its aggregate.

    Output mode is `json` so the TUI is suppressed; cost.json is written
    by the materializer on every SpawnFinished and on LoopFinished.
    Returns the aggregate plus the diff stats against the worktree's
    base SHA. Wall time is measured externally so it matches the user's
    experience of "how long did the run take".

    .bcc.toml is gitignored and untracked in this repo, so the worktree
    is born without it. We seed the worktree with the parent repo's
    .bcc.toml (so providers, env files, debug flags, etc. propagate)
    then patch loop.max_iterations on top. There is no `--max-iter`
    CLI flag and the env layer does not feed Config, so the disk side
    effect is unavoidable. The worktree is disposable.
    """
    toml_path = worktree / ".bcc.toml"
    if parent_toml is not None and parent_toml.exists() and not toml_path.exists():
        toml_path.write_bytes(parent_toml.read_bytes())
    patch_max_iterations(toml_path, max_iter)
    args = [
        "bcc",
        "run",
        "-W",
        "--output",
        "json",
        "--no-color",
    ]
    if prompt:
        args += ["--prompt", prompt]
    if spec:
        # bcc accepts a positional spec path; the repo conventionally
        # keeps fixtures under testdata/specs/.
        args.append(str(spec))
    started = time.monotonic()
    proc = run(args, cwd=worktree)
    elapsed = time.monotonic() - started
    if proc.returncode != 0:
        sys.stderr.write(
            f"bcc exited {proc.returncode}; reading partial cost.json anyway\n"
            f"args: {' '.join(args)}\n"
            f"stdout (tail):\n{proc.stdout[-2000:]}\n"
            f"stderr (tail):\n{proc.stderr[-2000:]}\n"
        )

    # No revert: diff_stat filters DIFF_EXCLUDE_PREFIXES (including
    # .bcc.toml) and leaving the file in place helps post-mortem
    # inspection of --keep worktrees.

    sessions_dir = worktree / ".bcc" / "sessions"
    if not sessions_dir.exists():
        return RunResult(
            label="bcc",
            wall_time_s=elapsed,
            total_usd=0.0,
            tokens=TokenUsage(),
            extra={"error": "no .bcc/sessions/ directory"},
        )

    # Pick the most recently modified session directory; with one bcc
    # run per worktree this is unambiguous.
    candidates = sorted(
        (p for p in sessions_dir.iterdir() if p.is_dir()),
        key=lambda p: p.stat().st_mtime,
        reverse=True,
    )
    if not candidates:
        return RunResult(
            label="bcc",
            wall_time_s=elapsed,
            total_usd=0.0,
            tokens=TokenUsage(),
            extra={"error": "no session directory"},
        )
    session_dir = candidates[0]
    cost_path = session_dir / "cost.json"
    if not cost_path.exists():
        # Crashed before any SpawnFinished; nothing to aggregate.
        return RunResult(
            label="bcc",
            wall_time_s=elapsed,
            total_usd=0.0,
            tokens=TokenUsage(provider="anthropic"),
            extra={"session_id": session_dir.name, "error": "missing cost.json"},
        )
    body = json.loads(cost_path.read_text())
    raw = body.get("total_tokens") or {}
    return RunResult(
        label="bcc",
        wall_time_s=elapsed,
        total_usd=float(body.get("total_usd") or 0.0),
        tokens=TokenUsage(
            input_fresh=int(raw.get("input_fresh") or 0),
            input_cached=int(raw.get("input_cached") or 0),
            cache_write=int(raw.get("cache_write") or 0),
            output=int(raw.get("output") or 0),
            reasoning=int(raw.get("reasoning") or 0),
            provider=str(raw.get("provider") or ""),
        ),
        extra={
            "session_id": body.get("session_id") or session_dir.name,
            "spawns": body.get("spawns"),
            "by_role": body.get("by_role"),
        },
    )


# ---------------------------------------------------------------------------
# direct claude side
# ---------------------------------------------------------------------------


def run_direct_claude(worktree: Path, prompt: str) -> RunResult:
    """Run claude as a single non-interactive invocation against the
    same prompt and aggregate its terminal `result` event into the
    5-bucket shape. Stream-json output is captured to a tempfile so a
    crashed run still leaves something to inspect.
    """
    log = worktree / "direct.stream.jsonl"
    args = [
        "claude",
        "-p",
        "--output-format",
        "stream-json",
        "--verbose",
        "--dangerously-skip-permissions",
        prompt,
    ]
    started = time.monotonic()
    with log.open("w") as fh:
        proc = subprocess.run(
            args,
            cwd=worktree,
            stdout=fh,
            stderr=subprocess.PIPE,
            text=True,
            check=False,
        )
    elapsed = time.monotonic() - started
    if proc.returncode != 0:
        sys.stderr.write(
            f"claude exited {proc.returncode}\nstderr:\n{proc.stderr[-2000:]}\n"
        )

    tokens, usd = parse_direct_stream(log)
    return RunResult(
        label="direct",
        wall_time_s=elapsed,
        total_usd=usd,
        tokens=tokens,
        extra={"stream_path": str(log)},
    )


def parse_direct_stream(log: Path) -> tuple[TokenUsage, float]:
    """Walk the stream-json log and return (TokenUsage, total_usd)
    derived from the terminal `result` event. Falls back to summing
    `assistant.message.usage` across the whole stream when the result
    event is missing (e.g. the run was killed mid-flight)."""
    if not log.exists():
        return TokenUsage(provider="anthropic"), 0.0
    last_result_usage: dict | None = None
    last_total_cost: float | None = None
    summed = TokenUsage(provider="anthropic")
    with log.open() as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                ev = json.loads(line)
            except json.JSONDecodeError:
                continue
            etype = ev.get("type")
            if etype == "result":
                last_result_usage = ev.get("usage") or {}
                last_total_cost = float(ev.get("total_cost_usd") or 0.0)
            elif etype == "assistant":
                msg = ev.get("message") or {}
                u = msg.get("usage") or {}
                summed = summed.add(_tokens_from_anthropic(u))
    if last_result_usage is not None and last_total_cost is not None:
        return _tokens_from_anthropic(last_result_usage), last_total_cost
    return summed, 0.0


def _tokens_from_anthropic(u: dict) -> TokenUsage:
    """Map the Anthropic four-field usage payload onto the 5-bucket
    vendor-neutral TokenUsage. The four Anthropic buckets are already
    disjoint and additive, so the conversion is a 1:1 rename. (OpenAI
    and Gemini would need a subtraction here because their cached
    tokens are a subset of prompt_tokens, not a separate bucket.)
    """
    return TokenUsage(
        input_fresh=int(u.get("input_tokens") or 0),
        input_cached=int(u.get("cache_read_input_tokens") or 0),
        cache_write=int(u.get("cache_creation_input_tokens") or 0),
        output=int(u.get("output_tokens") or 0),
        reasoning=0,
        provider="anthropic",
    )


# ---------------------------------------------------------------------------
# Reporting
# ---------------------------------------------------------------------------


def render_table(results: Iterable[RunResult]) -> str:
    """Render the side-by-side comparison table. Plain-text monospace so
    the script has no third-party deps; rich/tabulate are optional and
    skipped to keep stdlib-only."""
    rows = list(results)
    headers = [
        "side",
        "wall(s)",
        "usd",
        "input_fresh",
        "cached",
        "cache_write",
        "output",
        "reasoning",
        "total",
        "files",
        "+/-",
    ]
    out = [
        "",
        "  ".join(f"{h:>12}" if i > 0 else f"{h:<8}" for i, h in enumerate(headers)),
    ]
    for r in rows:
        cells = [
            f"{r.label:<8}",
            f"{r.wall_time_s:>12.2f}",
            f"{r.total_usd:>12.4f}",
            f"{r.tokens.input_fresh:>12}",
            f"{r.tokens.input_cached:>12}",
            f"{r.tokens.cache_write:>12}",
            f"{r.tokens.output:>12}",
            f"{r.tokens.reasoning:>12}",
            f"{r.tokens.total():>12}",
            f"{r.files_changed:>12}",
            f"{r.lines_added}/{r.lines_removed:>{12 - len(str(r.lines_added)) - 1}}"
            if r.lines_added or r.lines_removed
            else f"{'-':>12}",
        ]
        out.append("  ".join(cells))
    if len(rows) == 2:
        bcc, direct = rows
        if direct.total_usd > 0:
            usd_ratio = bcc.total_usd / direct.total_usd
            out.append(f"\nbcc / direct cost ratio: {usd_ratio:.2f}x")
        if direct.tokens.total() > 0:
            tok_ratio = bcc.tokens.total() / direct.tokens.total()
            out.append(f"bcc / direct tokens ratio: {tok_ratio:.2f}x")
    return "\n".join(out)


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


BASELINES = {
    "diag-dag": "testdata/specs/diag-dag.md",
}


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--prompt", help="Free-form prompt to feed both bcc and claude.")
    p.add_argument(
        "--spec",
        type=Path,
        help="Spec path passed to both sides (claude reads it via the prompt).",
    )
    p.add_argument(
        "--baseline",
        choices=sorted(BASELINES),
        help="Pick a built-in baseline spec by name (currently: diag-dag).",
    )
    p.add_argument(
        "--max-iter",
        type=int,
        default=5,
        help="Iteration budget for bcc (default: 5).",
    )
    p.add_argument(
        "--keep",
        action="store_true",
        help="Do not remove the worktrees after the run; useful for inspection.",
    )
    p.add_argument(
        "--out",
        type=Path,
        help="Optional path to write the JSON report to (in addition to stdout table).",
    )
    args = p.parse_args()

    if not args.prompt and not args.spec and not args.baseline:
        p.error("one of --prompt / --spec / --baseline is required")

    repo = repo_root()

    # spec is resolved as a path RELATIVE to the repo root. Each side
    # later joins this against its own worktree, so the agent sees a
    # spec inside the worktree, not in the parent repo. Passing the
    # parent-absolute path leaked writes to the parent on retry: the
    # agent occasionally derived "project root" from the spec's dir
    # rather than cwd.
    spec_relpath: Path | None = None
    spec_text: str | None = None
    if args.baseline:
        spec_relpath = Path(BASELINES[args.baseline])
        spec_abs = repo / spec_relpath
        if not spec_abs.exists():
            p.error(f"baseline spec missing: {spec_abs}")
        spec_text = spec_abs.read_text()
    elif args.spec:
        candidate = args.spec.resolve()
        if not candidate.exists():
            p.error(f"spec missing: {candidate}")
        try:
            spec_relpath = candidate.relative_to(repo)
        except ValueError:
            p.error(
                f"spec must live inside the repo root {repo}; got {candidate}"
            )
        spec_text = candidate.read_text()

    prompt_for_direct = args.prompt
    if prompt_for_direct is None and spec_text is not None:
        # The direct claude side has no MCP-shaped supervision: it reads
        # the spec inline and runs to completion. Pass the spec contents
        # plus a stable wrapper so both sides see the same text.
        prompt_for_direct = (
            f"Read and execute this spec end to end:\n\n{spec_text}"
        )

    if not working_tree_clean(repo):
        sys.stderr.write(
            "working tree is dirty; the comparator refuses to run so a"
            " local change does not contaminate either side. commit, stash,"
            " or discard changes and retry.\n"
        )
        return 2

    if shutil.which("bcc") is None:
        sys.stderr.write("bcc not found on PATH\n")
        return 2
    if shutil.which("claude") is None:
        sys.stderr.write("claude not found on PATH\n")
        return 2

    base_sha = head_sha(repo)
    sys.stderr.write(f"base SHA: {base_sha}\n")

    bcc_wt, bcc_branch = make_worktree(repo, "bcc", base_sha)
    direct_wt, direct_branch = make_worktree(repo, "direct", base_sha)

    try:
        sys.stderr.write(f"running bcc in {bcc_wt}\n")
        bcc_result = run_bcc(
            bcc_wt,
            prompt=args.prompt,
            spec=(bcc_wt / spec_relpath) if spec_relpath else None,
            max_iter=args.max_iter,
            parent_toml=repo / ".bcc.toml",
        )
        bcc_result.files_changed, bcc_result.lines_added, bcc_result.lines_removed = (
            diff_stat(bcc_wt, base_sha)
        )

        sys.stderr.write(f"running claude in {direct_wt}\n")
        direct_result = run_direct_claude(direct_wt, prompt=prompt_for_direct or "")
        (
            direct_result.files_changed,
            direct_result.lines_added,
            direct_result.lines_removed,
        ) = diff_stat(direct_wt, base_sha)

        results = [bcc_result, direct_result]

        print(render_table(results))

        if args.out:
            args.out.write_text(
                json.dumps(
                    {
                        "base_sha": base_sha,
                        "results": [
                            {
                                "label": r.label,
                                "wall_time_s": r.wall_time_s,
                                "total_usd": r.total_usd,
                                "tokens": r.tokens.__dict__,
                                "files_changed": r.files_changed,
                                "lines_added": r.lines_added,
                                "lines_removed": r.lines_removed,
                                "extra": r.extra,
                            }
                            for r in results
                        ],
                    },
                    indent=2,
                )
            )

        return 0
    finally:
        if not args.keep:
            remove_worktree(repo, bcc_wt, bcc_branch)
            remove_worktree(repo, direct_wt, direct_branch)
        else:
            sys.stderr.write(
                f"--keep set: worktrees left at {bcc_wt} and {direct_wt}\n"
            )


if __name__ == "__main__":
    sys.exit(main())
