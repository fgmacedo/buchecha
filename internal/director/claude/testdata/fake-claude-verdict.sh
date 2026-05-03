#!/usr/bin/env bash
# Stand-in for `claude -p ...` in Director Review tests. Emits a single
# assistant message carrying a hard-coded approve Verdict JSON.
set -e
echo '{"type":"system","subtype":"init","model":"fake","session_id":"sess-1"}'
cat <<'EOF'
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"{\"phase_id\":\"p1\",\"attempt\":1,\"outcome\":\"approve\",\"acceptance_results\":[{\"id\":\"a1\",\"passed\":true,\"note\":\"ok\"}],\"reasoning\":\"all green\"}"}]}}
EOF
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":120,"total_cost_usd":0.004,"usage":{"input_tokens":500,"output_tokens":80}}'
