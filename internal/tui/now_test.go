package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
)

func TestNow_onAgentEvent_TracksToolUseAndClearsOnResult(t *testing.T) {
	n := nowPanel{}
	at := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)

	n.onAgentEvent(loop.AgentEvent{
		Kind: loop.KindToolUse,
		At:   at,
		Tool: &loop.ToolCallInfo{ID: "t1", Name: "Edit", Args: map[string]any{"file_path": "internal/spec/plan.go"}},
	})
	if n.currentTool == nil || n.currentTool.ID != "t1" {
		t.Fatalf("currentTool not tracked: %+v", n.currentTool)
	}

	// Mismatched tool_result: no clear.
	n.onAgentEvent(loop.AgentEvent{Kind: loop.KindToolResult, Tool: &loop.ToolCallInfo{ID: "other"}})
	if n.currentTool == nil {
		t.Errorf("mismatched tool_result must not clear current tool")
	}

	// Matching tool_result: clears.
	n.onAgentEvent(loop.AgentEvent{Kind: loop.KindToolResult, Tool: &loop.ToolCallInfo{ID: "t1"}})
	if n.currentTool != nil {
		t.Errorf("matching tool_result must clear current tool, got %+v", n.currentTool)
	}
}

func TestNow_onAgentEvent_RecordsLatestAssistantText(t *testing.T) {
	n := nowPanel{}
	n.onAgentEvent(loop.AgentEvent{Kind: loop.KindAssistantText, Text: "first"})
	n.onAgentEvent(loop.AgentEvent{Kind: loop.KindAssistantText, Text: "  "}) // empty after trim, ignored
	n.onAgentEvent(loop.AgentEvent{Kind: loop.KindAssistantText, Text: "second"})
	if n.lastAssistant != "second" {
		t.Errorf("lastAssistant = %q, want second", n.lastAssistant)
	}
}

func TestNow_view_IdleWhenNoTool(t *testing.T) {
	n := nowPanel{}
	out := n.view(time.Now())
	if !strings.Contains(out, "now") {
		t.Errorf("missing panel title: %q", out)
	}
	if !strings.Contains(out, "idle") {
		t.Errorf("idle state not rendered: %q", out)
	}
}

func TestNow_view_RendersToolHeadline(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 30, 12, 0, time.UTC)
	n := nowPanel{
		currentTool:   &loop.ToolCallInfo{Name: "Bash", Args: map[string]any{"command": "go test ./..."}},
		currentToolAt: now.Add(-10 * time.Second),
		lastAssistant: "Adjusting parser",
	}
	out := n.view(now)
	if !strings.Contains(out, "Bash") {
		t.Errorf("missing tool name in view: %q", out)
	}
	if !strings.Contains(out, "go test ./...") {
		t.Errorf("missing tool command in view: %q", out)
	}
	if !strings.Contains(out, "10s") {
		t.Errorf("missing elapsed time in view: %q", out)
	}
	if !strings.Contains(out, "Adjusting parser") {
		t.Errorf("missing assistant text in view: %q", out)
	}
}

func TestNow_tick_AdvancesSpinnerFrame(t *testing.T) {
	n := nowPanel{}
	start := n.spinnerFrame
	for i := 0; i < len(spinnerFrames)+1; i++ {
		n.tick()
	}
	// After len(frames)+1 ticks, frame should be (start+len+1) % len = (start+1) % len.
	if n.spinnerFrame != (start+len(spinnerFrames)+1)%len(spinnerFrames) {
		t.Errorf("spinner frame did not wrap as expected: got %d", n.spinnerFrame)
	}
}

func TestFormatToolHeadline_KnownTools(t *testing.T) {
	cases := []struct {
		tool loop.ToolCallInfo
		want string
	}{
		{loop.ToolCallInfo{Name: "Bash", Args: map[string]any{"command": "ls"}}, "Bash  ls"},
		{loop.ToolCallInfo{Name: "Edit", Args: map[string]any{"file_path": "a.go"}}, "Edit  a.go"},
		{loop.ToolCallInfo{Name: "Read", Args: map[string]any{"file_path": "b.go"}}, "Read  b.go"},
		{loop.ToolCallInfo{Name: "Glob", Args: map[string]any{"pattern": "**/*.go"}}, "Glob  **/*.go"},
	}
	for _, tc := range cases {
		got := formatToolHeadline(tc.tool)
		if got != tc.want {
			t.Errorf("tool=%s args=%v: got %q, want %q", tc.tool.Name, tc.tool.Args, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello world", 5); got != "hell…" {
		t.Errorf("truncate len=5: got %q, want hell…", got)
	}
	if got := truncate("hi", 5); got != "hi" {
		t.Errorf("truncate short: got %q, want hi", got)
	}
}
