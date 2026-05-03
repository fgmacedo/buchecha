package tui

import (
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"strings"
	"testing"
	"time"
)

func TestActions_view_EmptyState(t *testing.T) {
	a := newActionsPanel()
	a.SetSize(80, actionsViewportHeight)
	out := a.view(80)
	if !strings.Contains(out, "no tool calls yet") {
		t.Errorf("empty state hint missing: %q", out)
	}
}

func TestActions_onAgentEvent_KeepsHistoryNewestFirstInVisibleWindow(t *testing.T) {
	a := newActionsPanel()
	a.SetSize(80, actionsViewportHeight)
	at := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	for i := 0; i < 7; i++ {
		a.onAgentEvent(agentcontract.AgentEvent{
			Kind: agentcontract.KindToolUse,
			At:   at.Add(time.Duration(i) * time.Second),
			Tool: &agentcontract.ToolCallInfo{Name: "Read", Args: map[string]any{"file_path": "f" + string(rune('0'+i)) + ".go"}},
		})
	}
	if len(a.entries) != 7 {
		t.Fatalf("entries len = %d, want 7 (history kept beyond the visible window)", len(a.entries))
	}
	out := a.view(80)
	// f6.go (latest) must appear in the visible window.
	if !strings.Contains(out, "f6.go") {
		t.Errorf("latest event missing in viewport view: %q", out)
	}
	// Newest first ordering inside the visible window: f6 before f5.
	if i6, i5 := strings.Index(out, "f6.go"), strings.Index(out, "f5.go"); i6 >= i5 {
		t.Errorf("expected newest-first ordering (f6 before f5): %q", out)
	}
}

func TestActions_onAgentEvent_IgnoresNonToolUseEvents(t *testing.T) {
	a := newActionsPanel()
	a.SetSize(80, actionsViewportHeight)
	a.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindAssistantText, Text: "hi"})
	a.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindToolResult, Tool: &agentcontract.ToolCallInfo{ID: "x"}})
	if len(a.entries) != 0 {
		t.Errorf("non-tool_use events should not enter the ring, got %d entries", len(a.entries))
	}
}

func TestActions_view_RendersTimestamp(t *testing.T) {
	a := newActionsPanel()
	a.SetSize(80, actionsViewportHeight)
	at := time.Date(2026, 4, 29, 14, 30, 5, 0, time.UTC)
	a.onAgentEvent(agentcontract.AgentEvent{
		Kind: agentcontract.KindToolUse, At: at,
		Tool: &agentcontract.ToolCallInfo{Name: "Bash", Args: map[string]any{"command": "go test ./..."}},
	})
	out := a.view(80)
	if !strings.Contains(out, "14:30:05") {
		t.Errorf("missing HH:MM:SS timestamp in %q", out)
	}
	if !strings.Contains(out, "go test ./...") {
		t.Errorf("missing tool command in %q", out)
	}
}

// TestActions_ScrollsBeyondVisibleWindow asserts the viewport keeps more
// than actionsViewportHeight entries reachable: after pushing many events,
// the visible window shows the newest, and a GotoTop call exposes older
// entries that would otherwise be clipped.
func TestActions_ScrollsBeyondVisibleWindow(t *testing.T) {
	a := newActionsPanel()
	a.SetSize(80, actionsViewportHeight)
	at := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	const n = 20
	for i := 0; i < n; i++ {
		a.onAgentEvent(agentcontract.AgentEvent{
			Kind: agentcontract.KindToolUse,
			At:   at.Add(time.Duration(i) * time.Second),
			Tool: &agentcontract.ToolCallInfo{Name: "Read", Args: map[string]any{"file_path": "f" + string(rune('A'+i)) + ".go"}},
		})
	}
	if len(a.entries) != n {
		t.Fatalf("entries len = %d, want %d (history kept past visible window)", len(a.entries), n)
	}
	// Default view (newest visible at top after GotoBottom in onAgentEvent).
	visible := a.view(80)
	if !strings.Contains(visible, "fT.go") { // index 19
		t.Errorf("latest entry not visible: %q", visible)
	}

	// Scrolling to bottom exposes the oldest (fA, fB, ...) - content is
	// rendered newest-first, so older entries sit below the visible window.
	a.viewport.GotoBottom()
	scrolled := a.view(80)
	if !strings.Contains(scrolled, "fA.go") {
		t.Errorf("oldest entry not reachable via scroll: %q", scrolled)
	}
}
