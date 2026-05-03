package loop_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

func TestMarshalJSONEvent_IterStarted(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 32, 0, 0, time.UTC)
	ev := loop.IterationStarted{Index: 3, MaxIter: 20, At: at}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-04-29T14:32:00Z","index":3,"level":"info","max_iter":20,"type":"iter_started"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_IterStartedWithBaseline(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 32, 0, 0, time.UTC)
	ev := loop.IterationStarted{
		Index: 1, MaxIter: 20, At: at,
		BaselineSHA: "abc123def456",
	}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-04-29T14:32:00Z","baseline_sha":"abc123def456","index":1,"level":"info","max_iter":20,"type":"iter_started"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_IterFinished(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 35, 0, 0, time.UTC)
	ev := loop.IterationFinished{
		Index:        3,
		Signal:       agentcontract.SignalContinue,
		HEADAdvanced: true,
		DurationMS:   420000,
		At:           at,
	}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-04-29T14:35:00Z","duration_ms":420000,"head_advanced":true,"index":3,"level":"info","signal":"continue","type":"iter_finished"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_LoopFinishedOK(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 40, 0, 0, time.UTC)
	ev := loop.LoopFinished{Reason: "spec done", ExitCode: 0, At: at}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-04-29T14:40:00Z","exit_code":0,"level":"info","reason":"spec done","type":"loop_finished"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_LoopFinishedNonZeroIsError(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 40, 0, 0, time.UTC)
	ev := loop.LoopFinished{Reason: "blocked", ExitCode: 1, At: at}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(got), `"level":"error"`) {
		t.Errorf("expected level=error, got: %s", got)
	}
}

func TestMarshalJSONEvent_AgentToolUse(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 32, 5, 0, time.UTC)
	ev := loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindToolUse,
		At:   at,
		Tool: &agentcontract.ToolCallInfo{
			ID:   "toolu_01",
			Name: "Edit",
			Args: map[string]any{"file_path": "internal/spec/plan.go"},
		},
	}}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-04-29T14:32:05Z","kind":"tool_use","level":"info","tool":{"args":{"file_path":"internal/spec/plan.go"},"id":"toolu_01","name":"Edit"},"type":"agent_event"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_AgentToolResult(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 32, 6, 0, time.UTC)
	ev := loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindToolResult,
		At:   at,
		Tool: &agentcontract.ToolCallInfo{ID: "toolu_01", IsError: false, Summary: "file edited"},
	}}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-04-29T14:32:06Z","kind":"tool_result","level":"debug","tool":{"id":"toolu_01","is_error":false,"summary":"file edited"},"type":"agent_event"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_AgentResultSummary(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 35, 0, 0, time.UTC)
	ev := loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindResultSummary,
		At:   at,
		Done: &agentcontract.ResultSummaryInfo{
			NumTurns:     12,
			TotalCostUSD: 0.34,
			InputTokens:  12000,
			OutputTokens: 2300,
			DurationMS:   42100,
		},
	}}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// done keys sorted alphabetically; cache token fields present even when zero.
	want := `{"at":"2026-04-29T14:35:00Z","done":{"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"duration_ms":42100,"input_tokens":12000,"num_turns":12,"output_tokens":2300,"total_cost_usd":0.34},"kind":"result_summary","level":"info","type":"agent_event"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_AgentResultSummaryWithCacheTokens(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 35, 0, 0, time.UTC)
	ev := loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindResultSummary,
		At:   at,
		Done: &agentcontract.ResultSummaryInfo{
			NumTurns:                 4,
			TotalCostUSD:             0.10,
			InputTokens:              5000,
			OutputTokens:             1000,
			CacheReadInputTokens:     800,
			CacheCreationInputTokens: 200,
			DurationMS:               15000,
		},
	}}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-04-29T14:35:00Z","done":{"cache_creation_input_tokens":200,"cache_read_input_tokens":800,"duration_ms":15000,"input_tokens":5000,"num_turns":4,"output_tokens":1000,"total_cost_usd":0.1},"kind":"result_summary","level":"info","type":"agent_event"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_AgentAssistantTextWithUsage(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 32, 10, 0, time.UTC)
	ev := loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindAssistantText,
		At:   at,
		Text: "Adjusting parser for edge case.",
		Usage: &agentcontract.UsageInfo{
			InputTokens:              1200,
			OutputTokens:             300,
			CacheReadInputTokens:     500,
			CacheCreationInputTokens: 100,
		},
	}}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// usage and its keys appear when non-nil; top-level keys sorted alphabetically.
	want := `{"at":"2026-04-29T14:32:10Z","kind":"assistant_text","level":"debug","text":"Adjusting parser for edge case.","type":"agent_event","usage":{"cache_creation_input_tokens":100,"cache_read_input_tokens":500,"input_tokens":1200,"output_tokens":300}}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_AgentAssistantTextWithoutUsage(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 32, 11, 0, time.UTC)
	ev := loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindAssistantText,
		At:   at,
		Text: "Plain message body.",
	}}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Re-parse and check key absence rather than substring-matching, so
	// the assertion does not depend on the text body containing "usage".
	var obj map[string]any
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := obj["usage"]; present {
		t.Errorf("usage key should be absent when Usage is nil: %s", got)
	}
}

func TestMarshalJSONEvent_AgentThinkingIsTrace(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 32, 0, 0, time.UTC)
	ev := loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindThinking,
		At:   at,
		Text: "Adjusting parser...",
	}}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-04-29T14:32:00Z","kind":"thinking","level":"trace","text":"Adjusting parser...","type":"agent_event"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_AgentRateLimitWarn(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 32, 0, 0, time.UTC)
	reset := time.Date(2026, 4, 29, 15, 0, 0, 0, time.UTC)
	ev := loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindRateLimit,
		At:   at,
		Rate: &agentcontract.RateLimitInfo{Status: "warning", ResetAt: reset},
	}}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-04-29T14:32:00Z","kind":"rate_limit","level":"warn","rate":{"reset_at":"2026-04-29T15:00:00Z","status":"warning"},"type":"agent_event"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_PhaseBriefed_WithAssignments(t *testing.T) {
	at := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	ev := loop.PhaseBriefed{
		PhaseID:        "p1",
		Attempt:        1,
		At:             at,
		ExecutorModel:  "claude-opus-4-7",
		ExecutorEffort: "high",
		BrieferSkipped: true,
		ReviewSkipped:  true,
	}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assignments, ok := payload["assignments"].(map[string]any)
	if !ok {
		t.Fatalf("expected assignments in payload, got %v", payload)
	}
	exec, _ := assignments["executor"].(map[string]any)
	if exec["model"] != "claude-opus-4-7" || exec["effort"] != "high" {
		t.Errorf("executor entry = %v, want model=claude-opus-4-7 effort=high", exec)
	}
	briefer, _ := assignments["briefer"].(map[string]any)
	if briefer["skipped"] != true {
		t.Errorf("briefer skipped = %v, want true", briefer)
	}
	reviewer, _ := assignments["reviewer"].(map[string]any)
	if reviewer["skipped"] != true {
		t.Errorf("reviewer skipped = %v, want true", reviewer)
	}
}

func TestMarshalJSONEvent_PhaseBriefed_OmitsEmptyAssignments(t *testing.T) {
	at := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	ev := loop.PhaseBriefed{PhaseID: "p1", Attempt: 1, At: at}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(got), "assignments") {
		t.Errorf("payload should omit assignments when nothing applies: %s", got)
	}
}

func TestMarshalJSONEvent_AgentInit(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 32, 0, 0, time.UTC)
	ev := loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind: agentcontract.KindInit,
		At:   at,
		Init: &agentcontract.InitInfo{SessionID: "s1", Model: "claude-sonnet-4", CWD: "/tmp"},
	}}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-04-29T14:32:00Z","init":{"cwd":"/tmp","model":"claude-sonnet-4","session_id":"s1"},"kind":"init","level":"debug","type":"agent_event"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}
