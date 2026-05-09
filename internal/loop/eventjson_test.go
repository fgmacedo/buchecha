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
		Iteration:      1,
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
	ev := loop.PhaseBriefed{PhaseID: "p1", Iteration: 1, At: at}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(got), "assignments") {
		t.Errorf("payload should omit assignments when nothing applies: %s", got)
	}
}

func TestMarshalJSONEvent_TaskStarted(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	ev := loop.TaskStarted{PhaseID: "P1", TaskID: "T1.1", At: at}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-05-05T12:00:00Z","level":"info","phase_id":"P1","task_id":"T1.1","type":"task_started"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

// TestMarshalJSONEvent_TaskStartedWithAgentID covers the post-enrichment
// wire shape where the EventService keys its active-task index by
// AgentID. Consumers that group activity per agent rely on this field.
func TestMarshalJSONEvent_TaskStartedWithAgentID(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	ev := loop.TaskStarted{AgentID: "bcc-executor-deadbeef", PhaseID: "P1", TaskID: "T1.1", At: at}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"agent_id":"bcc-executor-deadbeef","at":"2026-05-05T12:00:00Z","level":"info","phase_id":"P1","task_id":"T1.1","type":"task_started"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_TaskCompleted(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	ev := loop.TaskCompleted{PhaseID: "P1", TaskID: "T1.1", At: at}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-05-05T12:00:00Z","level":"info","phase_id":"P1","task_id":"T1.1","type":"task_completed"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_TaskApproved(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	ev := loop.TaskApproved{PhaseID: "P1", TaskID: "T1.1", At: at}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-05-05T12:00:00Z","level":"info","phase_id":"P1","task_id":"T1.1","type":"task_approved"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_TaskNeedsFixWithNote(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	ev := loop.TaskNeedsFix{PhaseID: "P1", TaskID: "T1.1", Note: "missing assertion", At: at}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-05-05T12:00:00Z","level":"info","note":"missing assertion","phase_id":"P1","task_id":"T1.1","type":"task_needs_fix"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_TaskNeedsFixOmitsEmptyNote(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	ev := loop.TaskNeedsFix{PhaseID: "P1", TaskID: "T1.1", At: at}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(got), "note") {
		t.Errorf("payload should omit note when empty: %s", got)
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

// TestMarshalJSONEvent_AgentEventWithOrigin covers the post-decoration
// wire shape carrying agent_id, role, phase_id, iteration_id, attempt
// (set by adapters) and task_id (set by the EventService fan-out).
// These are what lets the SPA group activity per agent and per task.
func TestMarshalJSONEvent_AgentEventWithOrigin(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 32, 0, 0, time.UTC)
	ev := loop.AgentEventReceived{Event: agentcontract.AgentEvent{
		Kind:        agentcontract.KindToolUse,
		At:          at,
		AgentID:     "bcc-executor-deadbeef",
		Role:        agentcontract.RoleExecutor,
		PhaseID:     "P7",
		IterationID: "P7-01",
		Attempt:     1,
		TaskID:      "T7.3",
		Tool:        &agentcontract.ToolCallInfo{ID: "t1", Name: "Edit", Args: map[string]any{"file_path": "foo.go"}},
	}}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"agent_id":"bcc-executor-deadbeef","at":"2026-04-29T14:32:00Z","attempt":1,"iteration_id":"P7-01","kind":"tool_use","level":"info","phase_id":"P7","role":"bcc-executor","task_id":"T7.3","tool":{"args":{"file_path":"foo.go"},"id":"t1","name":"Edit"},"type":"agent_event"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_SpawnStartedFull(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	ev := loop.SpawnStarted{
		SpawnID:     "01arya0000000000000001",
		Role:        "executor",
		PhaseID:     "P1",
		TaskID:      "T1.1",
		IterationID: "P1-iter1",
		Attempt:     1,
		Provider:    "claude",
		Model:       "claude-opus-4-7",
		Effort:      "high",
		PromptPath:  "/tmp/spawns/01arya0000000000000001.md",
		At:          at,
	}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-05-05T12:00:00Z","attempt":1,"effort":"high","iteration_id":"P1-iter1","level":"info","model":"claude-opus-4-7","phase_id":"P1","prompt_path":"/tmp/spawns/01arya0000000000000001.md","provider":"claude","role":"executor","spawn_id":"01arya0000000000000001","task_id":"T1.1","type":"spawn_started"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_SpawnStartedMinimal(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	ev := loop.SpawnStarted{
		SpawnID:  "01arya0000000000000002",
		Role:     "planner",
		Provider: "claude",
		At:       at,
	}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-05-05T12:00:00Z","level":"info","provider":"claude","role":"planner","spawn_id":"01arya0000000000000002","type":"spawn_started"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_SpawnFinished(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 5, 0, 0, time.UTC)
	ev := loop.SpawnFinished{
		SpawnID:    "01arya0000000000000001",
		Role:       "executor",
		ExitCode:   0,
		DurationMS: 5000,
		Cost: loop.SpawnCost{
			InputTokens:       12000,
			OutputTokens:      2300,
			CacheReadTokens:   800,
			CacheCreateTokens: 200,
			USD:               0.34,
		},
		At: at,
	}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-05-05T12:05:00Z","cost":{"cache_creation_input_tokens":200,"cache_read_input_tokens":800,"input_tokens":12000,"output_tokens":2300,"usd":0.34},"duration_ms":5000,"exit_code":0,"level":"info","role":"executor","spawn_id":"01arya0000000000000001","type":"spawn_finished"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestMarshalJSONEvent_SpawnFinishedZeroCost(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 5, 0, 0, time.UTC)
	ev := loop.SpawnFinished{
		SpawnID:    "01arya0000000000000001",
		Role:       "executor",
		ExitCode:   1,
		DurationMS: 1000,
		Cost:       loop.SpawnCost{},
		At:         at,
	}
	got, err := loop.MarshalJSONEvent(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"at":"2026-05-05T12:05:00Z","cost":{"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"input_tokens":0,"output_tokens":0,"usd":0},"duration_ms":1000,"exit_code":1,"level":"info","role":"executor","spawn_id":"01arya0000000000000001","type":"spawn_finished"}`
	if string(got) != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

// TestMarshalJSONEvent_AllKindsCovered locks the contract between the
// loop.Event union, MarshalJSONEvent, and loop.AllEventKinds. Adding a
// new Event variant requires:
//
//  1. an arm in MarshalJSONEvent's switch,
//  2. an entry in loop.AllEventKinds,
//  3. a sample below.
//
// Skipping any of those breaks this test, which is the point.
func TestMarshalJSONEvent_AllKindsCovered(t *testing.T) {
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	samples := []loop.Event{
		loop.IterationStarted{Index: 1, MaxIter: 1, At: at},
		loop.IterationFinished{Index: 1, At: at},
		loop.LoopFinished{Reason: "done", ExitCode: 0, At: at},
		loop.AgentEventReceived{Event: agentcontract.AgentEvent{Kind: agentcontract.KindInit, At: at}},
		loop.PhasePlanned{At: at},
		loop.PhaseBriefed{PhaseID: "P1", Iteration: 1, At: at},
		loop.PhaseReviewed{PhaseID: "P1", Attempt: 1, At: at},
		loop.SpawnStarted{SpawnID: "spawn123", Role: "executor", PhaseID: "P1", TaskID: "T1.1", IterationID: "iter1", Attempt: 1, Model: "claude-opus", Effort: "high", PromptPath: "/path/to/prompt.md", At: at},
		loop.SpawnFinished{SpawnID: "spawn123", Role: "executor", ExitCode: 0, DurationMS: 5000, Cost: loop.SpawnCost{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheCreateTokens: 0, USD: 0.05}, At: at},
		loop.TaskStarted{PhaseID: "P1", TaskID: "T1.1", At: at},
		loop.TaskCompleted{PhaseID: "P1", TaskID: "T1.1", At: at},
		loop.TaskApproved{PhaseID: "P1", TaskID: "T1.1", At: at},
		loop.TaskNeedsFix{PhaseID: "P1", TaskID: "T1.1", At: at},
		loop.DirectorEscalation{PhaseID: "P1", Attempt: 1, At: at},
	}

	gotKinds := make(map[string]bool, len(samples))
	for _, ev := range samples {
		raw, err := loop.MarshalJSONEvent(ev)
		if err != nil {
			t.Fatalf("marshal %T: %v", ev, err)
		}
		var head struct {
			Type   string `json:"type"`
			GoType string `json:"go_type"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			t.Fatalf("parse %T: %v", ev, err)
		}
		if head.Type == "" || head.Type == "unknown" {
			t.Errorf("%T marshaled as type=%q (go_type=%q): missing case in MarshalJSONEvent",
				ev, head.Type, head.GoType)
			continue
		}
		gotKinds[head.Type] = true
	}

	wantKinds := make(map[string]bool, len(loop.AllEventKinds))
	for _, k := range loop.AllEventKinds {
		wantKinds[k] = true
	}

	for k := range wantKinds {
		if !gotKinds[k] {
			t.Errorf("loop.AllEventKinds lists %q but no sample maps to it (add to samples or remove from AllEventKinds)", k)
		}
	}
	for k := range gotKinds {
		if !wantKinds[k] {
			t.Errorf("MarshalJSONEvent emits %q but loop.AllEventKinds does not list it", k)
		}
	}
}
