#!/usr/bin/env bash
# Stand-in for `claude -p ...` that emits an assistant message whose
# text content is not valid JSON. Exercises the parse-failure path of
# the adapter.
set -e
echo '{"type":"system","subtype":"init","model":"fake","session_id":"sess-1"}'
echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"not json {{{"}]}}'
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":42}'
