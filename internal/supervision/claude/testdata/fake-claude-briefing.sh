#!/usr/bin/env bash
# Stand-in for `claude -p ...` in Director Brief tests. Emits a single
# assistant message carrying a hard-coded Briefing JSON.
set -e
echo '{"type":"system","subtype":"init","model":"fake","session_id":"sess-1"}'
cat <<'EOF'
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"{\"iteration_id\":\"p1-1\",\"phase_id\":\"p1\",\"sub_dag_task_ids\":[\"t1\"],\"instructions\":\"Earlier phases delivered the loop.\",\"spec_path\":\"/tmp/spec.md\"}"}]}}
EOF
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":80,"total_cost_usd":0.002,"usage":{"input_tokens":300,"output_tokens":60}}'
