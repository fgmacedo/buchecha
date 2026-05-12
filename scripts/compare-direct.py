#!/usr/bin/env python3
"""Compare one or more bcc runs against a direct claude invocation on the
same prompt/spec.

The script provisions one disposable git worktree per variant from the
current HEAD, runs each variant inside its own worktree, then aggregates
cost and tokens into the same vendor-neutral 5-bucket shape so the
differences are meaningful.

A variant is a (label, kind, binary) triple. Default variants are one bcc
side (using `bcc` on PATH) and one direct claude side (using `claude` on
PATH). Extra bcc variants can be added with `--bcc-variant LABEL[=BINARY]`
so the same spec can be run against multiple bcc versions in a single
table. `--no-direct` skips the direct claude side. `--reuse PATH` folds in
the results of a prior report.json without re-running those variants.

Usage:
    # Default: one bcc + one direct
    python scripts/compare-direct.py --prompt "describe the bug fix"
    python scripts/compare-direct.py --spec testdata/specs/diag-dag.md
    python scripts/compare-direct.py --baseline diag-dag

    # Add an extra bcc variant pointing at a freshly built binary
    python scripts/compare-direct.py --baseline diag-dag \\
        --bcc-variant v2=/tmp/bcc-v2

    # Reuse a prior report; only run the new variant this time
    python scripts/compare-direct.py \\
        --reuse .compare-worktrees/report-20260511-162736.json \\
        --bcc-variant v2=/tmp/bcc-v2 --no-direct

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
from concurrent.futures import ThreadPoolExecutor
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


# A Variant is one row in the comparison table. The comparator orchestrates
# a list of variants; each one gets its own worktree and runs independently
# under its own binary. kind selects which runner function to call.
@dataclass
class Variant:
    label: str
    kind: str  # "bcc" or "direct"
    binary: str  # path or PATH name; resolved per-variant


def parse_bcc_variant_arg(s: str) -> Variant:
    """Parse a `--bcc-variant` argument of the form `LABEL[=BINARY]`. The
    label must be a non-empty filesystem-safe token; `direct` is reserved
    for the claude-direct variant so the worktree directory naming stays
    unambiguous."""
    if "=" in s:
        label, binary = s.split("=", 1)
    else:
        label, binary = s, "bcc"
    label = label.strip()
    binary = binary.strip() or "bcc"
    if not label:
        raise argparse.ArgumentTypeError(f"empty bcc variant label: {s!r}")
    if label.lower() == "direct":
        raise argparse.ArgumentTypeError(
            "label 'direct' is reserved for the claude-direct variant"
        )
    if any(c in label for c in " \t/\\"):
        raise argparse.ArgumentTypeError(
            f"invalid characters in bcc variant label: {label!r}"
        )
    return Variant(label=label, kind="bcc", binary=binary)


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


def binary_fingerprint(name: str) -> dict[str, str | int | None]:
    """Resolve `name` (a PATH lookup name or an absolute/relative path),
    capture mtime/size, and best-effort the `--version` output. Returned
    as a dict so the report records exactly which binary measured. Missing
    binary returns {"path": None, ...}."""
    # An explicit path (absolute or relative-with-separator) bypasses
    # PATH lookup; that lets per-variant binaries like ./bcc-v2 work.
    if "/" in name or os.path.isabs(name):
        resolved = str(Path(name).resolve()) if Path(name).exists() else None
    else:
        resolved = shutil.which(name)
    info: dict[str, str | int | None] = {
        "name": name,
        "path": resolved,
        "size_bytes": None,
        "mtime": None,
        "version": None,
    }
    if not resolved:
        return info
    try:
        st = os.stat(resolved)
        info["size_bytes"] = int(st.st_size)
        info["mtime"] = time.strftime(
            "%Y-%m-%dT%H:%M:%S", time.localtime(st.st_mtime)
        )
    except OSError:
        pass
    proc = run([resolved, "--version"])
    if proc.returncode == 0:
        info["version"] = (proc.stdout + proc.stderr).strip().splitlines()[0][:200] if (proc.stdout or proc.stderr) else None
    return info


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
    # Inoculate the new worktree against parent core.bare=true. Two
    # cases collapse to the same fix:
    #   - parent already bare at creation time (worktrunk pattern);
    #   - parent becomes bare mid-run (worktrunk, invoked by something
    #     external to this script, flips it on the fly).
    # The per-worktree core.bare=false in config.worktree wins over the
    # inherited value, so git status / add / diff inside this worktree
    # keeps working regardless. Setting it unconditionally is harmless
    # for non-bare parents (the override matches the inherited value).
    # Requires extensions.worktreeConfig=true on the parent for the
    # --worktree config namespace to exist.
    must_run(["git", "config", "extensions.worktreeConfig", "true"], cwd=repo)
    must_run(["git", "config", "--worktree", "core.bare", "false"], cwd=path)
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
    ".compare-spec.md",  # external spec staged into each worktree
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
    binary: str,
    label: str,
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
        binary,
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
            label=label,
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
            label=label,
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
            label=label,
            wall_time_s=elapsed,
            total_usd=0.0,
            tokens=TokenUsage(provider="anthropic"),
            extra={"session_id": session_dir.name, "error": "missing cost.json"},
        )
    body = json.loads(cost_path.read_text())
    raw = body.get("total_tokens") or {}
    return RunResult(
        label=label,
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


def run_direct_claude(
    worktree: Path, binary: str, label: str, prompt: str
) -> RunResult:
    """Run claude as a single non-interactive invocation against the
    same prompt and aggregate its terminal `result` event into the
    5-bucket shape. Stream-json output is captured to a tempfile so a
    crashed run still leaves something to inspect.
    """
    log = worktree / "direct.stream.jsonl"
    args = [
        binary,
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
        label=label,
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


def render_table(
    results: Iterable[RunResult], kinds: dict[str, str] | None = None
) -> str:
    """Render the comparison table for N variants. Plain-text monospace
    so the script has no third-party deps. `kinds` maps label -> kind
    ("bcc" or "direct") and is used to compute per-bcc ratios against a
    single direct row when present.
    """
    rows = list(results)
    kinds = kinds or {}
    label_width = max(8, max((len(r.label) for r in rows), default=8) + 1)
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
        "  ".join(
            f"{h:<{label_width}}" if i == 0 else f"{h:>12}"
            for i, h in enumerate(headers)
        ),
    ]
    for r in rows:
        cells = [
            f"{r.label:<{label_width}}",
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

    # Ratios are only meaningful when exactly one direct variant is
    # present (the baseline). For each bcc variant, print its cost and
    # token ratios against that baseline. With no direct variant we
    # print nothing; the absolute numbers above carry the signal.
    directs = [r for r in rows if kinds.get(r.label) == "direct"]
    bccs = [r for r in rows if kinds.get(r.label) == "bcc"]
    if len(directs) == 1 and bccs:
        d = directs[0]
        out.append("")
        for b in bccs:
            if d.total_usd > 0:
                usd_ratio = b.total_usd / d.total_usd
                out.append(
                    f"{b.label} / {d.label} cost ratio:   {usd_ratio:.2f}x"
                )
            if d.tokens.total() > 0:
                tok_ratio = b.tokens.total() / d.tokens.total()
                out.append(
                    f"{b.label} / {d.label} tokens ratio: {tok_ratio:.2f}x"
                )
    return "\n".join(out)


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


BASELINES = {
    "diag-dag": "testdata/specs/diag-dag.md",
}


# Top-level on-disk shape for a comparator invocation. Bumped when the
# report layout changes in a way that prior --reuse readers cannot
# transparently consume. v2 introduces the variants[] list (kind, binary
# per row) and drops the fixed bcc/claude binaries pair.
REPORT_SCHEMA_VERSION = 2


def variant_to_sidecar(
    variant: Variant,
    binary_info: dict,
    worktree: Path | None,
    branch: str | None,
    result: RunResult,
) -> dict:
    """Build the per-variant JSON record used both as a standalone sidecar
    and as one entry inside the top-level report. Keeping a single shape
    lets `--reuse` consume either form without branching."""
    return {
        "label": variant.label,
        "kind": variant.kind,
        "binary": binary_info,
        "worktree": str(worktree) if worktree else None,
        "branch": branch,
        "wall_time_s": result.wall_time_s,
        "total_usd": result.total_usd,
        "tokens": result.tokens.__dict__,
        "files_changed": result.files_changed,
        "lines_added": result.lines_added,
        "lines_removed": result.lines_removed,
        "extra": result.extra,
    }


def result_from_sidecar(rec: dict) -> RunResult:
    """Inverse of variant_to_sidecar: rebuild a RunResult from a record
    loaded out of a prior report or sidecar so `--reuse` can fold past
    rows into the new table."""
    t = rec.get("tokens") or {}
    return RunResult(
        label=str(rec.get("label") or ""),
        wall_time_s=float(rec.get("wall_time_s") or 0.0),
        total_usd=float(rec.get("total_usd") or 0.0),
        tokens=TokenUsage(
            input_fresh=int(t.get("input_fresh") or 0),
            input_cached=int(t.get("input_cached") or 0),
            cache_write=int(t.get("cache_write") or 0),
            output=int(t.get("output") or 0),
            reasoning=int(t.get("reasoning") or 0),
            provider=str(t.get("provider") or ""),
        ),
        files_changed=int(rec.get("files_changed") or 0),
        lines_added=int(rec.get("lines_added") or 0),
        lines_removed=int(rec.get("lines_removed") or 0),
        extra=dict(rec.get("extra") or {}),
    )


def load_reused_report(path: Path) -> dict:
    """Load a prior report.json (the canonical multi-variant shape).
    Raises ValueError if the file is missing or unreadable; callers
    surface the message to the user."""
    if not path.exists():
        raise ValueError(f"--reuse path missing: {path}")
    body = json.loads(path.read_text())
    if not isinstance(body, dict) or "variants" not in body:
        raise ValueError(
            f"--reuse target is not a comparator report (no 'variants' key): {path}"
        )
    return body


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--prompt", help="Free-form prompt to feed every variant.")
    p.add_argument(
        "--spec",
        type=Path,
        help="Spec path passed to every variant (claude reads it via the prompt).",
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
        "--bcc-variant",
        type=parse_bcc_variant_arg,
        action="append",
        default=[],
        metavar="LABEL[=BINARY]",
        help=(
            "Add a bcc variant. Repeatable. LABEL is a unique short name; "
            "BINARY is an optional path (defaults to `bcc` on PATH). When at "
            "least one --bcc-variant is given, the implicit default bcc variant "
            "is dropped."
        ),
    )
    p.add_argument(
        "--no-direct",
        action="store_true",
        help="Skip the direct claude variant. Useful for re-running only bcc sides.",
    )
    p.add_argument(
        "--reuse",
        type=Path,
        help=(
            "Path to a prior report.json. Its variants are folded into the new "
            "table without re-running. The base SHA is pinned from the report; "
            "the script aborts if no new variants are added."
        ),
    )
    p.add_argument(
        "--keep",
        action="store_true",
        help="Do not remove the worktrees after the run; useful for inspection.",
    )
    p.add_argument(
        "--out",
        type=Path,
        help="Optional path to write the JSON report to (in addition to the auto-saved one).",
    )
    args = p.parse_args()

    repo = repo_root()
    reused: dict | None = None
    if args.reuse is not None:
        try:
            reused = load_reused_report(args.reuse)
        except ValueError as exc:
            p.error(str(exc))

    # Build the list of variants to run this invocation. When --reuse is
    # set we default to "no new variants"; the user must opt in via
    # --bcc-variant. Otherwise we keep the historical defaults so existing
    # invocations behave the same.
    variants_to_run: list[Variant] = []
    if args.bcc_variant:
        variants_to_run.extend(args.bcc_variant)
    elif reused is None:
        variants_to_run.append(Variant(label="bcc", kind="bcc", binary="bcc"))
    if not args.no_direct and reused is None:
        variants_to_run.append(
            Variant(label="direct", kind="direct", binary="claude")
        )

    # Label uniqueness across both new and reused variants; collisions
    # would render two indistinguishable rows in the table and overwrite
    # each other on disk.
    seen_labels: set[str] = set()
    reused_labels: set[str] = set()
    if reused is not None:
        for v in reused.get("variants") or []:
            label = str(v.get("label") or "")
            if label in reused_labels:
                p.error(f"--reuse report contains duplicate label: {label!r}")
            reused_labels.add(label)
    for v in variants_to_run:
        if v.label in seen_labels:
            p.error(f"duplicate variant label: {v.label!r}")
        if v.label in reused_labels:
            p.error(
                f"variant label {v.label!r} collides with a reused variant; "
                "pick a different label"
            )
        seen_labels.add(v.label)

    if not variants_to_run and reused is None:
        p.error("nothing to run; specify --bcc-variant or omit --no-direct")
    if not variants_to_run and reused is not None:
        p.error(
            "--reuse alone has nothing to add; specify at least one --bcc-variant "
            "or drop --reuse to use the default bcc + direct pair"
        )

    # Spec resolution. When --reuse is set and the user did not override,
    # rebuild the spec context from the prior report so the new variant
    # sees the same input. The spec path is interpreted relative to repo
    # root for in-tree specs and copied into each worktree for external
    # specs.
    spec_relpath: Path | None = None
    spec_source: Path | None = None
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
            spec_relpath = Path(".compare-spec.md")
            spec_source = candidate
        spec_text = candidate.read_text()
    elif reused is not None:
        reused_spec = reused.get("spec") or {}
        if reused_spec.get("baseline"):
            spec_relpath = Path(BASELINES[reused_spec["baseline"]])
            spec_text = (repo / spec_relpath).read_text()
        elif reused_spec.get("source"):
            src = Path(reused_spec["source"])
            if not src.exists():
                p.error(
                    f"reused report references a spec that no longer exists: {src}"
                )
            relpath_str = reused_spec.get("relpath_in_worktree")
            spec_relpath = Path(relpath_str) if relpath_str else Path(".compare-spec.md")
            if reused_spec.get("external"):
                spec_source = src
            spec_text = src.read_text()

    if not args.prompt and spec_text is None:
        p.error(
            "no input: provide --prompt / --spec / --baseline, or pass "
            "--reuse with a report that records the spec"
        )

    prompt_for_direct = args.prompt
    if prompt_for_direct is None and spec_text is not None:
        prompt_for_direct = (
            f"Read and execute this spec end to end:\n\n{spec_text}"
        )

    if not working_tree_clean(repo):
        sys.stderr.write(
            "working tree is dirty; the comparator refuses to run so a"
            " local change does not contaminate any variant. commit, stash,"
            " or discard changes and retry. for a per-variant binary use"
            " --bcc-variant LABEL=PATH so the parent tree can stay clean.\n"
        )
        return 2

    # Fingerprint every binary we are about to run. Fingerprints land in
    # the per-variant sidecar so the report records exactly which binary
    # produced each row.
    binary_infos: dict[str, dict] = {}
    for variant in variants_to_run:
        info = binary_fingerprint(variant.binary)
        if info["path"] is None:
            sys.stderr.write(
                f"binary not found for variant {variant.label!r}: {variant.binary}\n"
            )
            return 2
        binary_infos[variant.label] = info
        sys.stderr.write(
            f"{variant.label} ({variant.kind}): {info['path']} "
            f"({info['version']}, mtime {info['mtime']}, {info['size_bytes']} bytes)\n"
        )

    # When --reuse pins the base SHA, the new variants must spawn against
    # the same commit so the spec content (and any in-tree references) is
    # identical to what the reused variants saw. If HEAD has moved, warn
    # but proceed using the pinned SHA; the worktree checkout makes this
    # safe regardless of HEAD position.
    head = head_sha(repo)
    if reused is not None:
        base_sha = str(reused.get("base_sha") or head)
        if base_sha != head:
            sys.stderr.write(
                f"--reuse pins base SHA {base_sha[:12]}; current HEAD is "
                f"{head[:12]}. running new variants at the pinned SHA.\n"
            )
    else:
        base_sha = head
    sys.stderr.write(f"base SHA: {base_sha}\n")

    parent = repo / ".compare-worktrees"
    parent.mkdir(exist_ok=True)

    # Provision worktrees up front (sequential, fast) so the parallel
    # phase only runs subprocesses. make_worktree creates a fresh branch
    # per call so concurrent variants do not race on a single branch name.
    worktrees: dict[str, tuple[Path, str]] = {}
    for variant in variants_to_run:
        wt, branch = make_worktree(repo, variant.label, base_sha)
        worktrees[variant.label] = (wt, branch)
        if spec_source is not None and spec_relpath is not None:
            (wt / spec_relpath).write_text(spec_text or "")

    def _run_variant(variant: Variant) -> RunResult:
        wt, _branch = worktrees[variant.label]
        sys.stderr.write(f"running {variant.label} ({variant.kind}) in {wt}\n")
        if variant.kind == "bcc":
            result = run_bcc(
                wt,
                binary=variant.binary,
                label=variant.label,
                prompt=args.prompt,
                spec=(wt / spec_relpath) if spec_relpath else None,
                max_iter=args.max_iter,
                parent_toml=repo / ".bcc.toml",
            )
        elif variant.kind == "direct":
            result = run_direct_claude(
                wt,
                binary=variant.binary,
                label=variant.label,
                prompt=prompt_for_direct or "",
            )
        else:
            raise ValueError(f"unknown variant kind: {variant.kind!r}")
        try:
            (
                result.files_changed,
                result.lines_added,
                result.lines_removed,
            ) = diff_stat(wt, base_sha)
        except Exception as exc:  # noqa: BLE001 - record and keep going
            sys.stderr.write(f"{variant.label} diff_stat failed: {exc}\n")
            result.extra["diff_stat_error"] = str(exc)
        return result

    try:
        # Run every new variant in parallel. Workers wait on subprocesses
        # against disjoint worktrees, so the GIL is not the bottleneck;
        # the cap matches the variant count.
        results_by_label: dict[str, RunResult] = {}
        if variants_to_run:
            with ThreadPoolExecutor(max_workers=len(variants_to_run)) as pool:
                futures = {
                    pool.submit(_run_variant, v): v for v in variants_to_run
                }
                for fut, variant in futures.items():
                    results_by_label[variant.label] = fut.result()

        # Per-variant sidecars: written next to the worktree under
        # `.compare-worktrees/<label>-<ts>.sidecar.json`. Useful when a
        # user wants to merge results manually or feed one variant into a
        # later report without keeping the whole worktree around.
        for variant in variants_to_run:
            wt, branch = worktrees[variant.label]
            sidecar = variant_to_sidecar(
                variant,
                binary_infos[variant.label],
                wt,
                branch,
                results_by_label[variant.label],
            )
            sidecar_path = parent / f"{wt.name}.sidecar.json"
            sidecar_path.write_text(json.dumps(sidecar, indent=2))

        # Assemble the final result list: reused variants first (in their
        # original order), then the newly-run ones. The table reads top
        # to bottom in roughly chronological order, which keeps the
        # baseline on top when adding a new bcc version later.
        report_variants: list[dict] = []
        if reused is not None:
            report_variants.extend(reused.get("variants") or [])
        for variant in variants_to_run:
            wt, branch = worktrees[variant.label]
            report_variants.append(
                variant_to_sidecar(
                    variant,
                    binary_infos[variant.label],
                    wt,
                    branch,
                    results_by_label[variant.label],
                )
            )

        kinds = {rec["label"]: rec.get("kind", "") for rec in report_variants}
        results_for_table = [result_from_sidecar(rec) for rec in report_variants]
        print(render_table(results_for_table, kinds=kinds))

        # Build the canonical report. Spec metadata mirrors the reused
        # report when present, then falls back to whatever this invocation
        # resolved. The auto path lives under .compare-worktrees/ so the
        # next `--reuse` call has somewhere to point at.
        spec_meta: dict
        if reused is not None and not (args.baseline or args.spec):
            spec_meta = reused.get("spec") or {}
        else:
            spec_meta = {
                "source": str(args.spec.resolve()) if args.spec else None,
                "baseline": args.baseline,
                "relpath_in_worktree": str(spec_relpath) if spec_relpath else None,
                "external": spec_source is not None,
            }

        report = {
            "schema_version": REPORT_SCHEMA_VERSION,
            "base_sha": base_sha,
            "spec": spec_meta,
            "variants": report_variants,
        }
        report_text = json.dumps(report, indent=2)
        auto_report = parent / f"report-{time.strftime('%Y%m%d-%H%M%S')}.json"
        auto_report.write_text(report_text)
        sys.stderr.write(f"report written to {auto_report}\n")
        if args.out:
            args.out.write_text(report_text)

        return 0
    finally:
        if not args.keep:
            for variant in variants_to_run:
                wt, branch = worktrees[variant.label]
                remove_worktree(repo, wt, branch)
        else:
            for variant in variants_to_run:
                wt, _ = worktrees[variant.label]
                sys.stderr.write(
                    f"--keep set: worktree for {variant.label} left at {wt}\n"
                )


if __name__ == "__main__":
    sys.exit(main())
