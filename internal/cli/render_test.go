package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// scriptedTranscript returns a fixed sequence of events spanning every
// level. Used by the json-mode verbosity tests below.
func scriptedTranscript() []loop.Event {
	at := time.Date(2026, 4, 29, 14, 32, 0, 0, time.UTC)
	return []loop.Event{
		loop.IterationStarted{Index: 1, MaxIter: 5, At: at}, // info
		loop.AgentEventReceived{Event: agentcontract.AgentEvent{
			Kind: agentcontract.KindThinking, At: at, Text: "thinking...",
		}}, // trace
		loop.AgentEventReceived{Event: agentcontract.AgentEvent{
			Kind: agentcontract.KindToolUse, At: at,
			Tool: &agentcontract.ToolCallInfo{ID: "t1", Name: "Bash", Args: map[string]any{"cmd": "ls"}},
		}}, // info
		loop.AgentEventReceived{Event: agentcontract.AgentEvent{
			Kind: agentcontract.KindToolResult, At: at,
			Tool: &agentcontract.ToolCallInfo{ID: "t1", IsError: false, Summary: "ok"},
		}}, // debug
		loop.AgentEventReceived{Event: agentcontract.AgentEvent{
			Kind: agentcontract.KindToolResult, At: at,
			Tool: &agentcontract.ToolCallInfo{ID: "t2", IsError: true, Summary: "boom"},
		}}, // error
		loop.AgentEventReceived{Event: agentcontract.AgentEvent{
			Kind: agentcontract.KindRateLimit, At: at,
			Rate: &agentcontract.RateLimitInfo{Status: "warning"},
		}}, // warn
		loop.AgentEventReceived{Event: agentcontract.AgentEvent{
			Kind: agentcontract.KindResultSummary, At: at,
			Done: &agentcontract.ResultSummaryInfo{NumTurns: 1, TotalCostUSD: 0.5},
		}}, // info
		loop.IterationFinished{Index: 1, Signal: agentcontract.SignalDone, HEADAdvanced: true, At: at}, // info
		loop.LoopFinished{Reason: "spec done", ExitCode: 0, At: at},                                    // info
	}
}

// runJSONAtLevel drives the same transcript through FilterEvents +
// drainJSON and returns the captured NDJSON as lines. It uses the same
// goroutine wiring as dispatchEvents so the test exercises the real
// pipeline.
func runJSONAtLevel(t *testing.T, level loop.Level) []string {
	t.Helper()
	in := make(chan loop.Event, 32)
	out := make(chan loop.Event, 32)
	loop.FilterEvents(in, out, level)

	var buf bytes.Buffer
	done := make(chan struct{})
	go drainJSON(out, done, &buf)

	for _, ev := range scriptedTranscript() {
		in <- ev
	}
	close(in)
	<-done

	body := strings.TrimRight(buf.String(), "\n")
	if body == "" {
		return nil
	}
	return strings.Split(body, "\n")
}

func TestDrainJSON_VerbosityFiltersIncrementally(t *testing.T) {
	cases := []struct {
		level    loop.Level
		minLines int
	}{
		{loop.LevelError, 1}, // tool_result error + nothing else (loop_finished is exit=0=info)
		{loop.LevelWarn, 2},  // + rate_limit warn
		{loop.LevelInfo, 7},  // + iter_started, tool_use, result_summary, iter_finished, loop_finished
		{loop.LevelDebug, 8}, // + tool_result ok
		{loop.LevelTrace, 9}, // + thinking
	}

	prev := -1
	for _, tc := range cases {
		got := runJSONAtLevel(t, tc.level)
		n := len(got)
		if n != tc.minLines {
			t.Errorf("level=%s line count = %d, want %d (lines: %v)", tc.level, n, tc.minLines, got)
		}
		if n < prev {
			t.Errorf("level=%s line count decreased (%d < %d)", tc.level, n, prev)
		}
		prev = n
		// Every emitted line must parse as JSON containing a 'level' field
		for _, line := range got {
			if !strings.Contains(line, `"level":`) {
				t.Errorf("level=%s line missing level field: %s", tc.level, line)
			}
		}
	}
}

// TestDrainJSON_ErrorOnly asserts byte-for-byte the NDJSON emitted at
// --verbosity error against the scripted transcript. Locks the schema
// for the most useful CI verbosity.
func TestDrainJSON_ErrorOnly(t *testing.T) {
	got := runJSONAtLevel(t, loop.LevelError)
	want := []string{
		`{"at":"2026-04-29T14:32:00Z","kind":"tool_result","level":"error","tool":{"id":"t2","is_error":true,"summary":"boom"},"type":"agent_event"}`,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i, line := range got {
		if line != want[i] {
			t.Errorf("line %d:\n got: %s\nwant: %s", i, line, want[i])
		}
	}
}

// TestDrainJSON_InfoLockedSchema asserts byte-for-byte the orchestrator-
// friendly default verbosity. This is the contract a parent bcc reads.
func TestDrainJSON_InfoLockedSchema(t *testing.T) {
	got := runJSONAtLevel(t, loop.LevelInfo)
	want := []string{
		`{"at":"2026-04-29T14:32:00Z","index":1,"level":"info","max_iter":5,"type":"iter_started"}`,
		`{"at":"2026-04-29T14:32:00Z","kind":"tool_use","level":"info","tool":{"args":{"cmd":"ls"},"id":"t1","name":"Bash"},"type":"agent_event"}`,
		`{"at":"2026-04-29T14:32:00Z","kind":"tool_result","level":"error","tool":{"id":"t2","is_error":true,"summary":"boom"},"type":"agent_event"}`,
		`{"at":"2026-04-29T14:32:00Z","kind":"rate_limit","level":"warn","rate":{"status":"warning"},"type":"agent_event"}`,
		`{"at":"2026-04-29T14:32:00Z","done":{"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"duration_ms":0,"input_tokens":0,"num_turns":1,"output_tokens":0,"total_cost_usd":0.5},"kind":"result_summary","level":"info","type":"agent_event"}`,
		`{"at":"2026-04-29T14:32:00Z","duration_ms":0,"head_advanced":true,"index":1,"level":"info","signal":"done","type":"iter_finished"}`,
		`{"at":"2026-04-29T14:32:00Z","exit_code":0,"level":"info","reason":"spec done","type":"loop_finished"}`,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i, line := range got {
		if line != want[i] {
			t.Errorf("line %d:\n got: %s\nwant: %s", i, line, want[i])
		}
	}
}

// TestDispatchEvents_DrainsCleanlyOnChannelClose covers the structural
// guarantee P2.3 sub-item 7 cares about: when the events producer (the
// loop) closes its channel for any reason (normal termination or ctx
// cancellation that propagated up), every backend goroutine exits and
// the `done` signal fires. Exercised across all three output modes so
// the same wiring is verified for tui (no-op drain), text (slog) and
// json (NDJSON).
func TestDispatchEvents_DrainsCleanlyOnChannelClose(t *testing.T) {
	for _, mode := range []string{OutputTUI, OutputText, OutputJSON} {
		t.Run(mode, func(t *testing.T) {
			events, drained, err := dispatchEvents(mode, loop.LevelInfo)
			if err != nil {
				t.Fatalf("dispatchEvents(%s): %v", mode, err)
			}
			events <- loop.IterationStarted{Index: 1, MaxIter: 1}
			events <- loop.LoopFinished{Reason: "user cancelled", ExitCode: 0}
			close(events)
			select {
			case <-drained:
			case <-time.After(2 * time.Second):
				t.Fatalf("backend %s did not drain within 2s", mode)
			}
		})
	}
}

func TestDispatchEvents_RejectsUnknownMode(t *testing.T) {
	_, _, err := dispatchEvents("yaml", loop.LevelInfo)
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

// TestDispatchEvents_TextJSONExitWithoutInput is the parity regression
// for P2.11.9: TUI mode parks the user at a session menu when the loop
// finishes, but text and json modes must keep their existing
// exit-on-LoopFinished behaviour because their consumers (CI, parent
// bcc, log aggregators) expect the process to terminate. The drainer
// goroutines must drain and return without reading stdin or otherwise
// blocking on user input.
func TestDispatchEvents_TextJSONExitWithoutInput(t *testing.T) {
	for _, mode := range []string{OutputText, OutputJSON} {
		t.Run(mode, func(t *testing.T) {
			events, drained, err := dispatchEvents(mode, loop.LevelInfo)
			if err != nil {
				t.Fatalf("dispatchEvents(%s): %v", mode, err)
			}
			// Push a terminal LoopFinished so the drainer sees one
			// every event kind a real run would emit, then close the
			// channel to signal end of stream. No stdin is provided.
			events <- loop.IterationStarted{Index: 1, MaxIter: 1}
			events <- loop.LoopFinished{Reason: "spec done", ExitCode: 0}
			close(events)
			select {
			case <-drained:
			case <-time.After(2 * time.Second):
				t.Fatalf("backend %s did not drain within 2s; "+
					"likely waiting on user input", mode)
			}
		})
	}
}

func TestValidOutputMode(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"tui", true},
		{"text", true},
		{"json", true},
		{"TUI", false},
		{"", false},
		{"yaml", false},
	}
	for _, c := range cases {
		if got := validOutputMode(c.s); got != c.want {
			t.Errorf("validOutputMode(%q) = %v, want %v", c.s, got, c.want)
		}
	}
}
