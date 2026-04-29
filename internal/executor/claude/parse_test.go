package claude

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
)

func TestParseLine_Cases(t *testing.T) {
	at := time.Date(2026, 4, 29, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		line string
		want []loop.AgentEvent
	}{
		{
			name: "system init",
			line: `{"type":"system","subtype":"init","cwd":"/p","session_id":"s","model":"m"}`,
			want: []loop.AgentEvent{{
				Kind: loop.KindInit, At: at,
				Init: &loop.InitInfo{SessionID: "s", Model: "m", CWD: "/p"},
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
			want: []loop.AgentEvent{{
				Kind: loop.KindRateLimit, At: at,
				Rate: &loop.RateLimitInfo{Status: "allowed", ResetAt: time.Unix(1777491600, 0)},
			}},
		},
		{
			name: "assistant text",
			line: `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
			want: []loop.AgentEvent{{
				Kind: loop.KindAssistantText, At: at, Text: "hi",
			}},
		},
		{
			name: "assistant thinking",
			line: `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"why"}]}}`,
			want: []loop.AgentEvent{{
				Kind: loop.KindThinking, At: at, Text: "why",
			}},
		},
		{
			name: "assistant empty thinking dropped",
			line: `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"","signature":"sig"}]}}`,
			want: []loop.AgentEvent{},
		},
		{
			name: "assistant tool_use",
			line: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x"}}]}}`,
			want: []loop.AgentEvent{{
				Kind: loop.KindToolUse, At: at,
				Tool: &loop.ToolCallInfo{ID: "t1", Name: "Read", Args: map[string]any{"file_path": "/x"}},
			}},
		},
		{
			name: "assistant multi-content emits per item",
			line: `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"plan"},{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"ls"}},{"type":"text","text":"done"}]}}`,
			want: []loop.AgentEvent{
				{Kind: loop.KindThinking, At: at, Text: "plan"},
				{Kind: loop.KindToolUse, At: at, Tool: &loop.ToolCallInfo{ID: "t2", Name: "Bash", Args: map[string]any{"command": "ls"}}},
				{Kind: loop.KindAssistantText, At: at, Text: "done"},
			},
		},
		{
			name: "user tool_result string content",
			line: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":false,"content":"ok"}]}}`,
			want: []loop.AgentEvent{{
				Kind: loop.KindToolResult, At: at,
				Tool: &loop.ToolCallInfo{ID: "t1", IsError: false, Summary: "ok"},
			}},
		},
		{
			name: "user tool_result array content",
			line: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]}]}}`,
			want: []loop.AgentEvent{{
				Kind: loop.KindToolResult, At: at,
				Tool: &loop.ToolCallInfo{ID: "t2", IsError: true, Summary: "line1\nline2"},
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
			want: []loop.AgentEvent{{
				Kind: loop.KindResultSummary, At: at,
				Done: &loop.ResultSummaryInfo{
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLine([]byte(tt.line), at)
			// Normalize empty slice vs nil for reflect.DeepEqual.
			if len(tt.want) == 0 && len(got) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseLine:\n got=%#v\nwant=%#v", got, tt.want)
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
	var got []loop.AgentEvent
	for _, line := range bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n")) {
		got = append(got, parseLine(line, at)...)
	}

	want := []loop.AgentEvent{
		{Kind: loop.KindInit, At: at, Init: &loop.InitInfo{SessionID: "sess-1", Model: "claude-opus-4-7", CWD: "/tmp/proj"}},
		{Kind: loop.KindRateLimit, At: at, Rate: &loop.RateLimitInfo{Status: "allowed", ResetAt: time.Unix(1777491600, 0)}},
		{Kind: loop.KindThinking, At: at, Text: "considering options"},
		// Empty thinking line is dropped.
		{Kind: loop.KindToolUse, At: at, Tool: &loop.ToolCallInfo{ID: "toolu_01", Name: "Read", Args: map[string]any{"file_path": "/tmp/proj/spec.md"}}},
		{Kind: loop.KindToolResult, At: at, Tool: &loop.ToolCallInfo{ID: "toolu_01", IsError: false, Summary: "file body"}},
		{Kind: loop.KindToolUse, At: at, Tool: &loop.ToolCallInfo{ID: "toolu_02", Name: "Bash", Args: map[string]any{"command": "go test ./..."}}},
		{Kind: loop.KindToolResult, At: at, Tool: &loop.ToolCallInfo{ID: "toolu_02", IsError: true, Summary: "FAIL\nexit 1"}},
		{Kind: loop.KindAssistantText, At: at, Text: "Result: ok"},
		{Kind: loop.KindResultSummary, At: at, Done: &loop.ResultSummaryInfo{NumTurns: 3, TotalCostUSD: 0.01, DurationMS: 4200, InputTokens: 100, OutputTokens: 50}},
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
