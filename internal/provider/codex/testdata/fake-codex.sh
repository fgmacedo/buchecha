#!/usr/bin/env bash
# Minimal stand-in for `codex exec --json`. Ignores all args; emits a
# canned JSONL stream and exits 0.
set -e
cat <<'EOF'
{"type":"thread.started","thread_id":"fake-thread-1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"hi"}}
{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":10,"reasoning_output_tokens":0}}
EOF
