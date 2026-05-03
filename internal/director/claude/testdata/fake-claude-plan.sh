#!/usr/bin/env bash
# Stand-in for `claude -p ...` in Director Plan tests. Emits a single
# assistant message carrying a hard-coded Plan JSON, then a result event.
# Ignores all args; exits 0.
set -e
echo '{"type":"system","subtype":"init","model":"fake","session_id":"sess-1"}'
cat <<'EOF'
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"{\"goal\":\"ship the thing\",\"success_criteria\":[\"tests pass\"],\"phases\":[{\"id\":\"p1\",\"title\":\"Phase one\",\"intent\":\"build\",\"depends_on\":[],\"tasks\":[{\"id\":\"t1\",\"title\":\"green\",\"intent\":\"green tests\",\"acceptance\":[{\"id\":\"a1\",\"description\":\"green tests\",\"evidence\":\"test\"}],\"status\":\"pending\",\"retry_budget\":1}]}],\"spec_hash\":\"abc123\",\"planned_at\":\"2026-05-02T00:00:00Z\"}"}]}}
EOF
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":300,"total_cost_usd":0.012,"usage":{"input_tokens":1000,"output_tokens":300}}'
