#!/usr/bin/env bash
# Stand-in that emits one event then exits with a non-zero code so the
# adapter's exit-propagation path can be tested.
echo '{"type":"thread.started","thread_id":"fail"}'
echo 'codex: simulated quota exhausted' >&2
exit 9
