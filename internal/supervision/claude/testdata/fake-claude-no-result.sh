#!/usr/bin/env bash
# Stand-in that exits cleanly without emitting a result event.
# Used to verify SpawnFinished is emitted with Cost{} when no result_summary
# is present in the stream.
set -e
echo '{"type":"system","subtype":"init","model":"fake","session_id":"sess-1"}'
echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}'
