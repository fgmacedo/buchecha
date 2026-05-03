package tui

import (
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"testing"
	"time"
)

func toolUse(name string, args map[string]any) agentcontract.AgentEvent {
	return agentcontract.AgentEvent{
		Kind: agentcontract.KindToolUse,
		Tool: &agentcontract.ToolCallInfo{Name: name, Args: args},
	}
}

func TestLoopSuspect_NotTriggeredBelowWindow(t *testing.T) {
	var l loopSuspect
	for i := 0; i < loopSuspectWindow-1; i++ {
		l.onAgentEvent(toolUse("Edit", map[string]any{"file_path": "x.go"}))
	}
	if _, _, ok := l.triggered(); ok {
		t.Errorf("triggered with n=%d (window=%d); want not triggered until full", l.n, loopSuspectWindow)
	}
}

func TestLoopSuspect_TriggeredAtThreshold(t *testing.T) {
	var l loopSuspect
	// 7 same + 3 different = 7/10 → triggered
	for i := 0; i < loopSuspectThreshold; i++ {
		l.onAgentEvent(toolUse("Edit", map[string]any{"file_path": "plan.go"}))
	}
	for i := 0; i < loopSuspectWindow-loopSuspectThreshold; i++ {
		l.onAgentEvent(toolUse("Read", map[string]any{"file_path": "other.go"}))
	}
	key, count, ok := l.triggered()
	if !ok {
		t.Fatalf("triggered = false at %d/%d, want true", loopSuspectThreshold, loopSuspectWindow)
	}
	if count != loopSuspectThreshold {
		t.Errorf("count = %d, want %d", count, loopSuspectThreshold)
	}
	if key.name != "Edit" || key.arg != "plan.go" {
		t.Errorf("dominant key = {%q, %q}, want {Edit, plan.go}", key.name, key.arg)
	}
}

func TestLoopSuspect_NotTriggeredBelowThreshold(t *testing.T) {
	var l loopSuspect
	// 6 same + 4 different = 6/10 → not triggered
	for i := 0; i < loopSuspectThreshold-1; i++ {
		l.onAgentEvent(toolUse("Edit", map[string]any{"file_path": "plan.go"}))
	}
	for i := 0; i < loopSuspectWindow-(loopSuspectThreshold-1); i++ {
		l.onAgentEvent(toolUse("Read", map[string]any{"file_path": "other.go"}))
	}
	if _, _, ok := l.triggered(); ok {
		t.Errorf("triggered with 6/10 same key; want not triggered")
	}
}

func TestLoopSuspect_OldestEntryEvicted(t *testing.T) {
	var l loopSuspect
	// Fill to threshold, then overwrite past the window.
	for i := 0; i < loopSuspectThreshold; i++ {
		l.onAgentEvent(toolUse("Edit", map[string]any{"file_path": "plan.go"}))
	}
	for i := 0; i < loopSuspectWindow-loopSuspectThreshold; i++ {
		l.onAgentEvent(toolUse("Read", map[string]any{"file_path": "other.go"}))
	}
	if _, _, ok := l.triggered(); !ok {
		t.Fatalf("setup: window should be triggered")
	}
	// Push 4 fresh "Read other.go" entries: oldest 4 (the Edits) get
	// evicted. Remaining: 3 Edit + 7 Read → key flips to Read.
	for i := 0; i < 4; i++ {
		l.onAgentEvent(toolUse("Read", map[string]any{"file_path": "other.go"}))
	}
	key, count, ok := l.triggered()
	if !ok {
		t.Fatalf("triggered = false after eviction; want true with new dominant key")
	}
	if key.name != "Read" || key.arg != "other.go" {
		t.Errorf("dominant key = {%q, %q}, want {Read, other.go}", key.name, key.arg)
	}
	if count != 7 {
		t.Errorf("count = %d, want 7", count)
	}
}

func TestLoopSuspect_DifferentArgsAreDifferentKeys(t *testing.T) {
	var l loopSuspect
	// 10 Edits but each on a different file → no key reaches 7.
	for i := 0; i < loopSuspectWindow; i++ {
		l.onAgentEvent(toolUse("Edit", map[string]any{"file_path": runeFile(i)}))
	}
	if _, _, ok := l.triggered(); ok {
		t.Errorf("triggered with 10 Edits on 10 different files; want not triggered")
	}
}

func TestLoopSuspect_SameArgDifferentToolsAreDifferentKeys(t *testing.T) {
	var l loopSuspect
	// 5 Read + 5 Edit, same file_path → no key reaches 7.
	for i := 0; i < 5; i++ {
		l.onAgentEvent(toolUse("Read", map[string]any{"file_path": "x.go"}))
		l.onAgentEvent(toolUse("Edit", map[string]any{"file_path": "x.go"}))
	}
	if _, _, ok := l.triggered(); ok {
		t.Errorf("triggered with 5+5 split across two tools; want not triggered")
	}
}

func TestLoopSuspect_NoArgToolStillCounts(t *testing.T) {
	var l loopSuspect
	for i := 0; i < loopSuspectThreshold; i++ {
		l.onAgentEvent(toolUse("AmbientThing", nil))
	}
	for i := 0; i < loopSuspectWindow-loopSuspectThreshold; i++ {
		l.onAgentEvent(toolUse("Read", map[string]any{"file_path": "x.go"}))
	}
	key, _, ok := l.triggered()
	if !ok {
		t.Fatalf("triggered = false for repeated no-arg tool; want true")
	}
	if key.name != "AmbientThing" || key.arg != "" {
		t.Errorf("dominant key = {%q, %q}, want {AmbientThing, \"\"}", key.name, key.arg)
	}
}

func TestLoopSuspect_NonToolEventsIgnored(t *testing.T) {
	var l loopSuspect
	// Saturate with non-tool_use events; window should never fill.
	for i := 0; i < 50; i++ {
		l.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindAssistantText, Text: "talking"})
		l.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindThinking, Text: "thinking"})
		l.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindToolResult, Tool: &agentcontract.ToolCallInfo{}})
	}
	if l.n != 0 {
		t.Errorf("n = %d after non-tool_use events; want 0", l.n)
	}
}

// runeFile returns "f<i>.go" so each test entry has a unique path.
func runeFile(i int) string {
	return string(rune('a'+i)) + ".go"
}

// --- existing windowed counters (P2.6.2 errors 5m, P2.6.3 tools/min 60s) ----

func TestToolsPerMin_RollsOffOldEntries(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	stamps := []time.Time{
		now.Add(-3 * time.Minute),  // out
		now.Add(-90 * time.Second), // out
		now.Add(-60 * time.Second), // boundary: included (>= now-60s)
		now.Add(-30 * time.Second),
		now.Add(-1 * time.Second),
	}
	if got := toolsPerMin(stamps, now); got != 3 {
		t.Errorf("tools/min = %d, want 3 (60s window)", got)
	}
}

func TestErrors5m_RollsOffOldEntries(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	stamps := []time.Time{
		now.Add(-10 * time.Minute), // out
		now.Add(-7 * time.Minute),  // out
		now.Add(-5 * time.Minute),  // boundary: included
		now.Add(-2 * time.Minute),
		now.Add(-30 * time.Second),
	}
	if got := countSince(stamps, now.Add(-5*time.Minute)); got != 3 {
		t.Errorf("errors (5m) = %d, want 3 (5m window)", got)
	}
}
