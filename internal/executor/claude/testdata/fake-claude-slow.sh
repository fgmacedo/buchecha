#!/usr/bin/env bash
# Stand-in that emits one event and then sleeps long enough that the test
# context will time out and trigger cancellation.
echo '{"type":"system","subtype":"init"}'
# Trap SIGINT so we can confirm graceful interrupt path; sleep would
# otherwise be killed by SIGKILL too.
trap 'exit 130' INT
sleep 10
