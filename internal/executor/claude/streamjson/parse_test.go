package streamjson

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

func TestParseLine_Cases(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		line string
		want []agentcontract.AgentEvent
	}{
		{
			name: "system init",
			line: `{"type":"system","subtype":"init","cwd":"/p","session_id":"s","model":"m"}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindInit, At: at,
				Init: &agentcontract.InitInfo{SessionID: "s", Model: "m", CWD: "/p"},
			}},
		},
		{
			name: "system non-init dropped",
			line: `{"type":"system","subtype":"compact"}`,
			want: nil,
		},
		{
			name: "rate limit",
			line: `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":1777491600}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindRateLimit, At: at,
				Rate: &agentcontract.RateLimitInfo{Status: "allowed", ResetAt: time.Unix(1777491600, 0)},
			}},
		},
		{
			name: "assistant text",
			line: `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindAssistantText, At: at, Text: "hi",
			}},
		},
		{
			name: "assistant thinking",
			line: `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"why"}]}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindThinking, At: at, Text: "why",
			}},
		},
		{
			name: "assistant empty thinking dropped",
			line: `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"","signature":"sig"}]}}`,
			want: []agentcontract.AgentEvent{},
		},
		{
			name: "assistant tool_use",
			line: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x"}}]}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolUse, At: at,
				Tool: &agentcontract.ToolCallInfo{ID: "t1", Name: "Read", Args: map[string]any{"file_path": "/x"}},
			}},
		},
		{
			name: "assistant multi-content emits per item",
			line: `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"plan"},{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"ls"}},{"type":"text","text":"done"}]}}`,
			want: []agentcontract.AgentEvent{
				{Kind: agentcontract.KindThinking, At: at, Text: "plan"},
				{Kind: agentcontract.KindToolUse, At: at, Tool: &agentcontract.ToolCallInfo{ID: "t2", Name: "Bash", Args: map[string]any{"command": "ls"}}},
				{Kind: agentcontract.KindAssistantText, At: at, Text: "done"},
			},
		},
		{
			name: "user tool_result string content",
			line: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":false,"content":"ok"}]}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolResult, At: at,
				Tool: &agentcontract.ToolCallInfo{ID: "t1", IsError: false, Summary: "ok"},
			}},
		},
		{
			name: "user tool_result array content",
			line: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]}]}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolResult, At: at,
				Tool: &agentcontract.ToolCallInfo{ID: "t2", IsError: true, Summary: "line1\nline2"},
			}},
		},
		{
			name: "user with plain string content (interrupted) dropped",
			line: `{"type":"user","message":{"content":"[Request interrupted by user]"}}`,
			want: nil,
		},
		{
			name: "result summary",
			line: `{"type":"result","subtype":"success","num_turns":12,"total_cost_usd":0.34,"duration_ms":42100,"usage":{"input_tokens":12000,"output_tokens":2300}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindResultSummary, At: at,
				Done: &agentcontract.ResultSummaryInfo{
					NumTurns: 12, TotalCostUSD: 0.34, DurationMS: 42100,
					InputTokens: 12000, OutputTokens: 2300,
				},
			}},
		},
		{
			name: "unknown type silently dropped",
			line: `{"type":"some_future_event","data":42}`,
			want: nil,
		},
		{
			name: "malformed json dropped",
			line: `{"type":"system",`,
			want: nil,
		},
		{
			name: "bcc-prefixed mcp tool surfaces as plain tool_use",
			line: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_01","name":"mcp__bcc__bcc_plan_emit","input":{"agent_id":"a","plan":{}}}]}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolUse, At: at,
				Tool: &agentcontract.ToolCallInfo{
					ID:   "toolu_01",
					Name: "mcp__bcc__bcc_plan_emit",
					Args: map[string]any{"agent_id": "a", "plan": map[string]any{}},
				},
			}},
		},
		{
			name: "foreign mcp tool stays as tool_use",
			line: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_06","name":"mcp__notion__search","input":{"q":"x"}}]}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolUse, At: at,
				Tool: &agentcontract.ToolCallInfo{
					ID:   "toolu_06",
					Name: "mcp__notion__search",
					Args: map[string]any{"q": "x"},
				},
			}},
		},
		{
			name: "legacy bcc_event top-level line dropped",
			line: `{"type":"bcc_event","event":"task_started","id":"P1.2"}`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLine([]byte(tt.line), at)
			if len(tt.want) == 0 && len(got) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseLine:\n got=%#v\nwant=%#v", got, tt.want)
			}
		})
	}
}

func TestParseLine_FullIterFixture(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("testdata", "full-iter.jsonl"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	at := time.Date(2026, 4, 29, 14, 0, 0, 0, time.UTC)
	var got []agentcontract.AgentEvent
	for _, line := range bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n")) {
		got = append(got, ParseLine(line, at)...)
	}

	want := []agentcontract.AgentEvent{
		{Kind: agentcontract.KindInit, At: at, Init: &agentcontract.InitInfo{SessionID: "sess-1", Model: "claude-opus-4-7", CWD: "/tmp/proj"}},
		{Kind: agentcontract.KindRateLimit, At: at, Rate: &agentcontract.RateLimitInfo{Status: "allowed", ResetAt: time.Unix(1777491600, 0)}},
		{Kind: agentcontract.KindThinking, At: at, Text: "considering options"},
		{Kind: agentcontract.KindToolUse, At: at, Tool: &agentcontract.ToolCallInfo{ID: "toolu_w1", Name: "mcp__bcc__task_started", Args: map[string]any{"id": "P1.1", "summary": "begin first task"}}},
		{Kind: agentcontract.KindToolResult, At: at, Tool: &agentcontract.ToolCallInfo{ID: "toolu_w1", IsError: false, Summary: "ok"}},
		{Kind: agentcontract.KindToolUse, At: at, Tool: &agentcontract.ToolCallInfo{ID: "toolu_01", Name: "Read", Args: map[string]any{"file_path": "/tmp/proj/spec.md"}}},
		{Kind: agentcontract.KindToolResult, At: at, Tool: &agentcontract.ToolCallInfo{ID: "toolu_01", IsError: false, Summary: "file body"}},
		{Kind: agentcontract.KindToolUse, At: at, Tool: &agentcontract.ToolCallInfo{ID: "toolu_02", Name: "Bash", Args: map[string]any{"command": "go test ./..."}}},
		{Kind: agentcontract.KindToolResult, At: at, Tool: &agentcontract.ToolCallInfo{ID: "toolu_02", IsError: true, Summary: "FAIL\nexit 1"}},
		{Kind: agentcontract.KindToolUse, At: at, Tool: &agentcontract.ToolCallInfo{ID: "toolu_w2", Name: "mcp__bcc__task_completed", Args: map[string]any{"id": "P1.1"}}},
		{Kind: agentcontract.KindToolResult, At: at, Tool: &agentcontract.ToolCallInfo{ID: "toolu_w2", IsError: false, Summary: "ok"}},
		{Kind: agentcontract.KindAssistantText, At: at, Text: "Result: ok"},
		{Kind: agentcontract.KindToolUse, At: at, Tool: &agentcontract.ToolCallInfo{ID: "toolu_w3", Name: "mcp__bcc__iteration_result", Args: map[string]any{"value": "continue", "summary": "phase 1 advanced"}}},
		{Kind: agentcontract.KindToolResult, At: at, Tool: &agentcontract.ToolCallInfo{ID: "toolu_w3", IsError: false, Summary: "ok"}},
		{Kind: agentcontract.KindResultSummary, At: at, Done: &agentcontract.ResultSummaryInfo{NumTurns: 3, TotalCostUSD: 0.01, DurationMS: 4200, InputTokens: 100, OutputTokens: 50}},
	}
	if len(got) != len(want) {
		t.Fatalf("event count = %d, want %d:\n got=%#v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("event[%d]:\n got=%#v\nwant=%#v", i, got[i], want[i])
		}
	}
}
