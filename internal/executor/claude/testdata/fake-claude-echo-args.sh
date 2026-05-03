#!/usr/bin/env bash
# Writes each command-line argument on its own line to the path in
# $ECHO_ARGS_OUT; exits 0. Tests inject ECHO_ARGS_OUT to capture the
# argv the Executor assembled. When $ECHO_STDIN_OUT is also set, the
# script reads stdin and writes it verbatim to that path so tests can
# verify what the adapter piped to claude.
set -e
if [[ -n "$ECHO_ARGS_OUT" ]]; then
  : > "$ECHO_ARGS_OUT"
  for a in "$@"; do
    echo "$a" >> "$ECHO_ARGS_OUT"
  done
fi
if [[ -n "$ECHO_STDIN_OUT" ]]; then
  cat > "$ECHO_STDIN_OUT"
fi
