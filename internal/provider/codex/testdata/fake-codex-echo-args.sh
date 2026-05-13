#!/usr/bin/env bash
# Writes each command-line argument on its own line to $ECHO_ARGS_OUT and,
# when $ECHO_STDIN_OUT is set, captures stdin verbatim to that path so
# tests can assert how the codex adapter assembled argv and what it piped
# in.
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
