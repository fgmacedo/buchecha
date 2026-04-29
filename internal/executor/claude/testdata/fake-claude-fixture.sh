#!/usr/bin/env bash
# Replays the canned stream-json fixture verbatim on stdout. Used by
# Run-level tests that need to exercise the streaming pipe path.
set -e
cat "$(dirname "$0")/full-iter.jsonl"
