#!/usr/bin/env bash
# Writes each command-line argument on its own line to the path in
# $ECHO_ARGS_OUT; exits 0. Tests inject ECHO_ARGS_OUT to capture the
# argv the Executor assembled.
set -e
if [[ -n "$ECHO_ARGS_OUT" ]]; then
  : > "$ECHO_ARGS_OUT"
  for a in "$@"; do
    echo "$a" >> "$ECHO_ARGS_OUT"
  done
fi
