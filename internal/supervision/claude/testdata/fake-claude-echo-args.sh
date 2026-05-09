#!/usr/bin/env bash
# Writes each command-line argument on its own line to $ECHO_ARGS_OUT;
# emits a minimal stream-json conversation that satisfies the adapter
# parser (assistant text with valid JSON + result), and exits 0.
set -e
if [[ -n "$ECHO_ARGS_OUT" ]]; then
  : > "$ECHO_ARGS_OUT"
  for a in "$@"; do
    echo "$a" >> "$ECHO_ARGS_OUT"
  done
fi
echo '{"type":"system","subtype":"init","model":"fake"}'
cat <<'EOF'
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"{\"phase_id\":\"p1\",\"attempt\":1,\"spec_excerpt\":\"x\",\"context_summary\":\"y\"}"}]}}
EOF
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":1}'
