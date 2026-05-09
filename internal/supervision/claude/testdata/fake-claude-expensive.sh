#!/usr/bin/env bash
# Stand-in that emits a result event reporting cost above the cap.
# Exercises the budget enforcement path in the adapter.
set -e
echo '{"type":"system","subtype":"init","model":"fake","session_id":"sess-1"}'
cat <<'EOF'
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"{\"phase_id\":\"p1\",\"attempt\":1,\"spec_excerpt\":\"x\",\"context_summary\":\"y\"}"}]}}
EOF
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":42,"total_cost_usd":99.99}'
