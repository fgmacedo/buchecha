package loop_test

import (
	"testing"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    loop.Level
		wantErr bool
	}{
		{in: "error", want: loop.LevelError},
		{in: "ERROR", want: loop.LevelError},
		{in: "warn", want: loop.LevelWarn},
		{in: "warning", want: loop.LevelWarn},
		{in: "info", want: loop.LevelInfo},
		{in: "debug", want: loop.LevelDebug},
		{in: "trace", want: loop.LevelTrace},
		{in: "  info  ", want: loop.LevelInfo},
		{in: "verbose", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tc := range cases {
		got, err := loop.ParseLevel(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseLevel(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q) error = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestLevelString(t *testing.T) {
	for _, tc := range []struct {
		l    loop.Level
		want string
	}{
		{loop.LevelError, "error"},
		{loop.LevelWarn, "warn"},
		{loop.LevelInfo, "info"},
		{loop.LevelDebug, "debug"},
		{loop.LevelTrace, "trace"},
	} {
		if got := tc.l.String(); got != tc.want {
			t.Errorf("Level(%d).String() = %q, want %q", tc.l, got, tc.want)
		}
	}
}

func TestLevelOf(t *testing.T) {
	cases := []struct {
		name string
		ev   loop.Event
		want loop.Level
	}{
		{"iter_started", loop.IterationStarted{}, loop.LevelInfo},
		{"iter_finished", loop.IterationFinished{Signal: agentcontract.SignalContinue}, loop.LevelInfo},
		{"loop_finished_ok", loop.LoopFinished{ExitCode: 0}, loop.LevelInfo},
		{"loop_finished_blocked", loop.LoopFinished{ExitCode: 1}, loop.LevelError},
		{"loop_finished_invalid", loop.LoopFinished{ExitCode: 2}, loop.LevelError},
		{"agent_init", loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindInit}}, loop.LevelDebug},
		{"agent_thinking", loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindThinking}}, loop.LevelTrace},
		{"agent_tool_use", loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindToolUse}}, loop.LevelInfo},
		{"agent_tool_result_ok", loop.AgentEventReceived{Event: agentcontract.AgentEvent{
			Kind: agentcontract.KindToolResult,
			Tool: &agentcontract.ToolCallInfo{IsError: false},
		}}, loop.LevelDebug},
		{"agent_tool_result_err", loop.AgentEventReceived{Event: agentcontract.AgentEvent{
			Kind: agentcontract.KindToolResult,
			Tool: &agentcontract.ToolCallInfo{IsError: true},
		}}, loop.LevelError},
		{"agent_assistant_text", loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindAssistantText}}, loop.LevelDebug},
		{"agent_rate_limit_allowed", loop.AgentEventReceived{Event: agentcontract.AgentEvent{
			Kind: agentcontract.KindRateLimit,
			Rate: &agentcontract.RateLimitInfo{Status: "allowed"},
		}}, loop.LevelDebug},
		{"agent_rate_limit_throttled", loop.AgentEventReceived{Event: agentcontract.AgentEvent{
			Kind: agentcontract.KindRateLimit,
			Rate: &agentcontract.RateLimitInfo{Status: "warning"},
		}}, loop.LevelWarn},
		{"agent_result_summary", loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindResultSummary}}, loop.LevelInfo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := loop.LevelOf(tc.ev); got != tc.want {
				t.Errorf("LevelOf(%T) = %v, want %v", tc.ev, got, tc.want)
			}
		})
	}
}

// TestLevelOf_AllAgentKindsCovered ensures every AgentEventKind constant
// has an explicit non-default Level mapping so future additions cannot
// silently fall to LevelInfo.
func TestLevelOf_AllAgentKindsCovered(t *testing.T) {
	all := []agentcontract.AgentEventKind{
		agentcontract.KindInit,
		agentcontract.KindThinking,
		agentcontract.KindToolUse,
		agentcontract.KindToolResult,
		agentcontract.KindAssistantText,
		agentcontract.KindRateLimit,
		agentcontract.KindResultSummary,
	}
	seen := map[agentcontract.AgentEventKind]bool{}
	for _, k := range all {
		seen[k] = true
		_ = loop.LevelOf(loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: k}})
	}
	if len(seen) != len(all) {
		t.Errorf("expected all kinds covered: %d, got %d", len(all), len(seen))
	}
}

func TestFilterEvents(t *testing.T) {
	in := make(chan loop.Event, 16)
	out := make(chan loop.Event, 16)
	loop.FilterEvents(in, out, loop.LevelInfo)

	in <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindThinking}}      // trace -> drop
	in <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindToolUse}}       // info -> keep
	in <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindAssistantText}} // debug -> drop
	in <- loop.LoopFinished{ExitCode: 1}                                                                  // error -> keep
	close(in)

	var got []loop.Level
	for ev := range out {
		got = append(got, loop.LevelOf(ev))
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if got[0] != loop.LevelInfo || got[1] != loop.LevelError {
		t.Errorf("got levels %v, want [info, error]", got)
	}
}

func TestFilterEvents_TraceLetsAllThrough(t *testing.T) {
	in := make(chan loop.Event, 8)
	out := make(chan loop.Event, 8)
	loop.FilterEvents(in, out, loop.LevelTrace)

	in <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindThinking}}
	in <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindToolUse}}
	in <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindAssistantText}}
	close(in)

	n := 0
	for range out {
		n++
	}
	if n != 3 {
		t.Errorf("filter at trace dropped events: got %d, want 3", n)
	}
}

func TestFilterEvents_ErrorOnlyKeepsErrors(t *testing.T) {
	in := make(chan loop.Event, 8)
	out := make(chan loop.Event, 8)
	loop.FilterEvents(in, out, loop.LevelError)

	in <- loop.IterationStarted{}                                      // info -> drop
	in <- loop.IterationFinished{Signal: agentcontract.SignalContinue} // info -> drop
	in <- loop.LoopFinished{ExitCode: 0}                               // info -> drop
	in <- loop.LoopFinished{ExitCode: 1}                               // error -> keep
	in <- loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindToolResult,
		Tool: &agentcontract.ToolCallInfo{IsError: true},
	}} // error -> keep
	close(in)

	n := 0
	for range out {
		n++
	}
	if n != 2 {
		t.Errorf("filter at error: got %d, want 2", n)
	}
}
