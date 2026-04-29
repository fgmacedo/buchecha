package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
)

func TestActions_view_EmptyState(t *testing.T) {
	a := actionsPanel{}
	out := a.view(80)
	if !strings.Contains(out, "no tool calls yet") {
		t.Errorf("empty state hint missing: %q", out)
	}
}

func TestActions_onAgentEvent_KeepsLastFiveNewestFirst(t *testing.T) {
	a := actionsPanel{}
	at := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	for i := 0; i < 7; i++ {
		a.onAgentEvent(loop.AgentEvent{
			Kind: loop.KindToolUse,
			At:   at.Add(time.Duration(i) * time.Second),
			Tool: &loop.ToolCallInfo{Name: "Read", Args: map[string]any{"file_path": "f" + string(rune('0'+i)) + ".go"}},
		})
	}
	if len(a.entries) != actionsCap {
		t.Fatalf("entries len = %d, want %d", len(a.entries), actionsCap)
	}
	out := a.view(80)
	// f6.go (latest) must appear; f0/f1 dropped.
	if !strings.Contains(out, "f6.go") {
		t.Errorf("latest event missing in view: %q", out)
	}
	if strings.Contains(out, "f0.go") || strings.Contains(out, "f1.go") {
		t.Errorf("oldest events should have been evicted: %q", out)
	}
	// Newest first ordering: f6 must appear before f5 in the rendered text.
	if i6, i5 := strings.Index(out, "f6.go"), strings.Index(out, "f5.go"); i6 >= i5 {
		t.Errorf("expected newest-first ordering (f6 before f5): %q", out)
	}
}

func TestActions_onAgentEvent_IgnoresNonToolUseEvents(t *testing.T) {
	a := actionsPanel{}
	a.onAgentEvent(loop.AgentEvent{Kind: loop.KindAssistantText, Text: "hi"})
	a.onAgentEvent(loop.AgentEvent{Kind: loop.KindToolResult, Tool: &loop.ToolCallInfo{ID: "x"}})
	if len(a.entries) != 0 {
		t.Errorf("non-tool_use events should not enter the ring, got %d entries", len(a.entries))
	}
}

func TestActions_view_RendersTimestamp(t *testing.T) {
	a := actionsPanel{}
	at := time.Date(2026, 4, 29, 14, 30, 5, 0, time.UTC)
	a.onAgentEvent(loop.AgentEvent{
		Kind: loop.KindToolUse, At: at,
		Tool: &loop.ToolCallInfo{Name: "Bash", Args: map[string]any{"command": "go test ./..."}},
	})
	out := a.view(80)
	if !strings.Contains(out, "14:30:05") {
		t.Errorf("missing HH:MM:SS timestamp in %q", out)
	}
	if !strings.Contains(out, "go test ./...") {
		t.Errorf("missing tool command in %q", out)
	}
}
