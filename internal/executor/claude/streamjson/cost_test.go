package streamjson

import (
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

func resultSummaryEvent(usd float64, input, output, cacheRead, cacheCreate int64) agentcontract.AgentEvent {
	return agentcontract.AgentEvent{
		Kind: agentcontract.KindResultSummary,
		At:   time.Now(),
		Done: &agentcontract.ResultSummaryInfo{
			TotalCostUSD:             usd,
			InputTokens:              input,
			OutputTokens:             output,
			CacheReadInputTokens:     cacheRead,
			CacheCreationInputTokens: cacheCreate,
		},
	}
}

func TestLastResultSummary_EmptySlice(t *testing.T) {
	c, ok := LastResultSummary(nil)
	if ok {
		t.Fatalf("expected false for nil slice, got true")
	}
	if c != (Cost{}) {
		t.Errorf("expected zero Cost, got %+v", c)
	}
	c2, ok2 := LastResultSummary([]agentcontract.AgentEvent{})
	if ok2 {
		t.Fatalf("expected false for empty slice, got true")
	}
	if c2 != (Cost{}) {
		t.Errorf("expected zero Cost for empty slice, got %+v", c2)
	}
}

func TestLastResultSummary_NoResultSummary(t *testing.T) {
	events := []agentcontract.AgentEvent{
		{Kind: agentcontract.KindInit, At: time.Now(), Init: &agentcontract.InitInfo{Model: "fake"}},
		{Kind: agentcontract.KindAssistantText, At: time.Now(), Text: "hello"},
	}
	_, ok := LastResultSummary(events)
	if ok {
		t.Errorf("expected false when no result_summary present")
	}
}

func TestLastResultSummary_SingleResult(t *testing.T) {
	events := []agentcontract.AgentEvent{
		{Kind: agentcontract.KindInit, At: time.Now(), Init: &agentcontract.InitInfo{Model: "fake"}},
		resultSummaryEvent(0.012, 1000, 300, 50, 20),
	}
	c, ok := LastResultSummary(events)
	if !ok {
		t.Fatalf("expected true for slice with result_summary")
	}
	if c.USD != 0.012 {
		t.Errorf("USD = %v, want 0.012", c.USD)
	}
	if c.InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", c.InputTokens)
	}
	if c.OutputTokens != 300 {
		t.Errorf("OutputTokens = %d, want 300", c.OutputTokens)
	}
	if c.CacheReadTokens != 50 {
		t.Errorf("CacheReadTokens = %d, want 50", c.CacheReadTokens)
	}
	if c.CacheCreateTokens != 20 {
		t.Errorf("CacheCreateTokens = %d, want 20", c.CacheCreateTokens)
	}
}

func TestLastResultSummary_ReturnsLast(t *testing.T) {
	// Two result_summary events; LastResultSummary must return the second one.
	events := []agentcontract.AgentEvent{
		resultSummaryEvent(0.001, 100, 50, 0, 0),
		{Kind: agentcontract.KindAssistantText, At: time.Now(), Text: "between"},
		resultSummaryEvent(0.099, 9000, 800, 200, 100),
	}
	c, ok := LastResultSummary(events)
	if !ok {
		t.Fatalf("expected true")
	}
	if c.USD != 0.099 {
		t.Errorf("USD = %v, want 0.099 (last entry)", c.USD)
	}
	if c.InputTokens != 9000 {
		t.Errorf("InputTokens = %d, want 9000", c.InputTokens)
	}
}

func TestLastResultSummary_NilDoneSkipped(t *testing.T) {
	// A KindResultSummary with Done == nil must be skipped.
	events := []agentcontract.AgentEvent{
		{Kind: agentcontract.KindResultSummary, At: time.Now(), Done: nil},
		resultSummaryEvent(0.007, 700, 70, 0, 0),
	}
	c, ok := LastResultSummary(events)
	if !ok {
		t.Fatalf("expected true; second entry has non-nil Done")
	}
	if c.USD != 0.007 {
		t.Errorf("USD = %v, want 0.007", c.USD)
	}
}

func TestLastResultSummary_ParsedFromFixtureStream(t *testing.T) {
	// Verify round-trip through ParseLine → LastResultSummary using the
	// same JSON shape the real claude CLI emits.
	line := []byte(`{"type":"result","subtype":"success","is_error":false,"duration_ms":300,"total_cost_usd":0.012,"usage":{"input_tokens":1000,"output_tokens":300,"cache_read_input_tokens":50,"cache_creation_input_tokens":20}}`)
	at := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	events := ParseLine(line, at)
	if len(events) != 1 {
		t.Fatalf("ParseLine returned %d events, want 1", len(events))
	}
	c, ok := LastResultSummary(events)
	if !ok {
		t.Fatalf("LastResultSummary returned false")
	}
	if c.USD != 0.012 {
		t.Errorf("USD = %v, want 0.012", c.USD)
	}
	if c.InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", c.InputTokens)
	}
	if c.OutputTokens != 300 {
		t.Errorf("OutputTokens = %d, want 300", c.OutputTokens)
	}
	if c.CacheReadTokens != 50 {
		t.Errorf("CacheReadTokens = %d, want 50", c.CacheReadTokens)
	}
	if c.CacheCreateTokens != 20 {
		t.Errorf("CacheCreateTokens = %d, want 20", c.CacheCreateTokens)
	}
}
