#!/usr/bin/env bash
# Minimal stand-in for `claude -p --output-format stream-json --verbose`.
# Ignores all args; emits canned JSONL events; exits 0.
set -e
echo '{"type":"system","subtype":"init","model":"fake"}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}'
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":42}'
