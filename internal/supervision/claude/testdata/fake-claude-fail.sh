#!/usr/bin/env bash
# Stand-in that emits one event then exits with a non-zero code.
# Used to verify SpawnFinished is emitted even on non-zero exit.
echo '{"type":"system","subtype":"init","model":"fake","session_id":"sess-1"}'
exit 7
