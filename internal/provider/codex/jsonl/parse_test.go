package jsonl

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
	at := time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		line string
		want []agentcontract.AgentEvent
	}{
		{
			name: "thread.started bookkeeping dropped",
			line: `{"type":"thread.started","thread_id":"abc"}`,
			want: nil,
		},
		{
			name: "turn.started bookkeeping dropped",
			line: `{"type":"turn.started"}`,
			want: nil,
		},
		{
			name: "item.completed agent_message",
			line: `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"hello"}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindAssistantText, At: at, Text: "hello",
			}},
		},
		{
			name: "item.completed empty agent_message dropped",
			line: `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"   "}}`,
			want: nil,
		},
		{
			name: "item.started command_execution -> tool_use",
			line: `{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"ls -la","aggregated_output":"","exit_code":null,"status":"in_progress"}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolUse, At: at,
				Tool: &agentcontract.ToolCallInfo{
					ID:   "item_1",
					Name: "command_execution",
					Args: map[string]any{"command": "ls -la"},
				},
			}},
		},
		{
			name: "item.completed command_execution success -> tool_result",
			line: `{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"ls","aggregated_output":"file1\nfile2\n","exit_code":0,"status":"completed"}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolResult, At: at,
				Tool: &agentcontract.ToolCallInfo{
					ID:      "item_1",
					Name:    "command_execution",
					IsError: false,
					Summary: "file1\nfile2",
				},
			}},
		},
		{
			name: "item.completed command_execution failure -> tool_result with IsError",
			line: `{"type":"item.completed","item":{"id":"item_2","type":"command_execution","command":"false","aggregated_output":"","exit_code":1,"status":"completed"}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolResult, At: at,
				Tool: &agentcontract.ToolCallInfo{
					ID:      "item_2",
					Name:    "command_execution",
					IsError: true,
					Summary: "",
				},
			}},
		},
		{
			name: "item.started file_change -> tool_use",
			line: `{"type":"item.started","item":{"id":"item_3","type":"file_change","changes":[{"path":"/p/a.txt","kind":"add"}],"status":"in_progress"}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolUse, At: at,
				Tool: &agentcontract.ToolCallInfo{
					ID:   "item_3",
					Name: "file_change",
					Args: map[string]any{"changes": []any{map[string]any{"path": "/p/a.txt", "kind": "add"}}},
				},
			}},
		},
		{
			name: "item.completed file_change -> tool_result with summary",
			line: `{"type":"item.completed","item":{"id":"item_3","type":"file_change","changes":[{"path":"/p/a.txt","kind":"add"},{"path":"/p/b.txt","kind":"modify"}],"status":"completed"}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolResult, At: at,
				Tool: &agentcontract.ToolCallInfo{
					ID:      "item_3",
					Name:    "file_change",
					IsError: false,
					Summary: "add /p/a.txt\nmodify /p/b.txt",
				},
			}},
		},
		{
			name: "item.completed unknown item.type dropped",
			line: `{"type":"item.completed","item":{"id":"item_x","type":"web_fetch","url":"https://example.com"}}`,
			want: nil,
		},
		{
			name: "turn.completed -> result summary with codex usage mapping",
			line: `{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":30,"output_tokens":50,"reasoning_output_tokens":10}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindResultSummary, At: at,
				Done: &agentcontract.ResultSummaryInfo{
					NumTurns: 1,
					Tokens: agentcontract.TokenUsage{
						InputFresh:  70,
						InputCached: 30,
						Output:      40,
						Reasoning:   10,
						Provider:    agentcontract.ProviderOpenAI,
					},
				},
			}},
		},
		{
			name: "turn.completed without usage fields -> zeroed best-effort summary",
			line: `{"type":"turn.completed"}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindResultSummary, At: at,
				Done: &agentcontract.ResultSummaryInfo{
					NumTurns: 1,
					Tokens:   agentcontract.TokenUsage{Provider: agentcontract.ProviderOpenAI},
				},
			}},
		},
		{
			name: "flat agent_message envelope",
			line: `{"type":"agent_message","text":"hi"}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindAssistantText, At: at, Text: "hi",
			}},
		},
		{
			name: "flat tool_call envelope",
			line: `{"type":"tool_call","id":"t1","name":"Read","input":{"file_path":"/x"}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolUse, At: at,
				Tool: &agentcontract.ToolCallInfo{
					ID:   "t1",
					Name: "Read",
					Args: map[string]any{"file_path": "/x"},
				},
			}},
		},
		{
			name: "flat tool_call_result envelope",
			line: `{"type":"tool_call_result","tool_use_id":"t1","is_error":false,"output":"ok"}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolResult, At: at,
				Tool: &agentcontract.ToolCallInfo{
					ID:      "t1",
					IsError: false,
					Summary: "ok",
				},
			}},
		},
		{
			name: "flat task_complete envelope -> result summary",
			line: `{"type":"task_complete","num_turns":3,"duration_ms":1200,"total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":5}}`,
			want: []agentcontract.AgentEvent{{
				Kind: agentcontract.KindResultSummary, At: at,
				Done: &agentcontract.ResultSummaryInfo{
					NumTurns:     3,
					DurationMS:   1200,
					TotalCostUSD: 0.01,
					Tokens: agentcontract.TokenUsage{
						InputFresh: 10,
						Output:     5,
						Provider:   agentcontract.ProviderOpenAI,
					},
				},
			}},
		},
		{
			name: "unknown envelope dropped",
			line: `{"type":"some_future_event","data":1}`,
			want: nil,
		},
		{
			name: "malformed json dropped",
			line: `{"type":"item.completed`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLine([]byte(tt.line), at)
			if err != nil {
				t.Fatalf("ParseLine err=%v", err)
			}
			if len(tt.want) == 0 && len(got) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseLine:\n got=%#v\nwant=%#v", got, tt.want)
			}
		})
	}
}

func parseFixture(t *testing.T, name string) []agentcontract.AgentEvent {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	at := time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC)
	var got []agentcontract.AgentEvent
	for _, line := range bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		evs, err := ParseLine(line, at)
		if err != nil {
			t.Fatalf("ParseLine err on %q: %v", line, err)
		}
		got = append(got, evs...)
	}
	return got
}

func countKinds(evs []agentcontract.AgentEvent) map[agentcontract.AgentEventKind]int {
	out := make(map[agentcontract.AgentEventKind]int)
	for _, ev := range evs {
		out[ev.Kind]++
	}
	return out
}

func TestParseLine_SampleReadonly(t *testing.T) {
	got := parseFixture(t, "sample-readonly.jsonl")
	counts := countKinds(got)
	// Expected from the fixture: 2 agent_message (item_0, item_2), 1
	// command_execution start, 1 command_execution complete, 1 turn.completed.
	wantCounts := map[agentcontract.AgentEventKind]int{
		agentcontract.KindAssistantText: 2,
		agentcontract.KindToolUse:       1,
		agentcontract.KindToolResult:    1,
		agentcontract.KindResultSummary: 1,
	}
	for k, want := range wantCounts {
		if counts[k] != want {
			t.Errorf("%s count = %d, want %d (all: %v)", k, counts[k], want, counts)
		}
	}
	summary, ok := LastResultSummary(got)
	if !ok {
		t.Fatalf("LastResultSummary not found")
	}
	if summary.Tokens.Provider != agentcontract.ProviderOpenAI {
		t.Errorf("summary.Tokens.Provider = %q, want %q",
			summary.Tokens.Provider, agentcontract.ProviderOpenAI)
	}
	// Fixture usage: input=48608, cached=31488 -> InputFresh=17120,
	// InputCached=31488; output=122, reasoning=0 -> Output=122, Reasoning=0.
	want := agentcontract.TokenUsage{
		InputFresh:  17120,
		InputCached: 31488,
		Output:      122,
		Provider:    agentcontract.ProviderOpenAI,
	}
	if summary.Tokens != want {
		t.Errorf("summary.Tokens = %+v, want %+v", summary.Tokens, want)
	}
}

func TestParseLine_SampleToolUse(t *testing.T) {
	got := parseFixture(t, "sample-tool-use.jsonl")
	counts := countKinds(got)
	// Expected: 2 agent_message, 1 file_change start, 1 file_change complete,
	// 1 turn.completed.
	wantCounts := map[agentcontract.AgentEventKind]int{
		agentcontract.KindAssistantText: 2,
		agentcontract.KindToolUse:       1,
		agentcontract.KindToolResult:    1,
		agentcontract.KindResultSummary: 1,
	}
	for k, want := range wantCounts {
		if counts[k] != want {
			t.Errorf("%s count = %d, want %d (all: %v)", k, counts[k], want, counts)
		}
	}
	// Walk events to find the file_change ToolResult and confirm the
	// summary lists the path with its kind.
	var fileResult *agentcontract.ToolCallInfo
	for i := range got {
		ev := got[i]
		if ev.Kind == agentcontract.KindToolResult && ev.Tool != nil && ev.Tool.Name == "file_change" {
			fileResult = ev.Tool
			break
		}
	}
	if fileResult == nil {
		t.Fatalf("file_change tool_result not found in events: %+v", got)
	}
	if fileResult.Summary != "add /tmp/codex-sample-work2/hello.txt" {
		t.Errorf("file_change Summary = %q", fileResult.Summary)
	}
	if fileResult.IsError {
		t.Errorf("file_change IsError = true, want false")
	}
}

func TestLastResultSummary_None(t *testing.T) {
	if _, ok := LastResultSummary(nil); ok {
		t.Errorf("LastResultSummary(nil) returned ok=true")
	}
	evs := []agentcontract.AgentEvent{{Kind: agentcontract.KindAssistantText}}
	if _, ok := LastResultSummary(evs); ok {
		t.Errorf("LastResultSummary without summary returned ok=true")
	}
}
