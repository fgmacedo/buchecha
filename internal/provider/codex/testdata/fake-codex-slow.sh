#!/usr/bin/env bash
# Stand-in that emits one event and then sleeps long enough that the test
# context will time out and trigger cancellation. The SIGINT trap proves
# the graceful-interrupt path runs before WaitDelay escalates to SIGKILL.
echo '{"type":"thread.started","thread_id":"slow"}'
trap 'exit 130' INT
sleep 10
