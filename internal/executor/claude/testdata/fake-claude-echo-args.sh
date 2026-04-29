#!/usr/bin/env bash
# Echoes each command-line argument on its own line; exits 0.
for a in "$@"; do
  echo "$a"
done
