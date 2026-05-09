#!/usr/bin/env bash
# Simulates a Reviewer that idled: model ignored the prompt and returned without calling bcc_review_finished.
set -e
echo '{"type":"system","subtype":"init","model":"fake","session_id":"sess-1"}'
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":50,"total_cost_usd":0.001,"usage":{"input_tokens":100,"output_tokens":10}}'
