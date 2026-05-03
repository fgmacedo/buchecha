package tui

import (
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"strings"
	"testing"
	"time"
)

func TestHealth_onAgentEvent_AccumulatesCounters(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	h := healthPanel{startedAt: now.Add(-time.Minute)}

	h.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindToolUse, At: now.Add(-30 * time.Second), Tool: &agentcontract.ToolCallInfo{}})
	h.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindToolUse, At: now.Add(-20 * time.Second), Tool: &agentcontract.ToolCallInfo{}})
	h.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindToolResult, At: now.Add(-15 * time.Second), Tool: &agentcontract.ToolCallInfo{IsError: true}})
	h.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindResultSummary, At: now, Done: &agentcontract.ResultSummaryInfo{
		InputTokens: 1500, OutputTokens: 700, TotalCostUSD: 0.42,
	}})
	h.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindRateLimit, At: now, Rate: &agentcontract.RateLimitInfo{Status: "warning"}})

	if h.totalTools != 2 {
		t.Errorf("totalTools = %d, want 2", h.totalTools)
	}
	if h.totalErrors != 1 {
		t.Errorf("totalErrors = %d, want 1", h.totalErrors)
	}
	if h.totalTokens != 2200 {
		t.Errorf("totalTokens = %d, want 2200", h.totalTokens)
	}
	if h.totalCostUSD < 0.4 || h.totalCostUSD > 0.45 {
		t.Errorf("totalCostUSD = %f, want ~0.42", h.totalCostUSD)
	}
	if h.rate.Status != "warning" {
		t.Errorf("rate.Status = %q, want warning", h.rate.Status)
	}
}

func TestHealth_AssistantTextUsageAccumulatesAndReconciles(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	h := healthPanel{startedAt: now.Add(-time.Minute)}

	// Two assistant messages with per-message Usage. The four-field sum
	// should accumulate into iterTokens (the live, in-flight counter).
	h.onAgentEvent(agentcontract.AgentEvent{
		Kind: agentcontract.KindAssistantText, At: now.Add(-30 * time.Second),
		Text: "first turn",
		Usage: &agentcontract.UsageInfo{
			InputTokens: 100, OutputTokens: 20,
			CacheReadInputTokens: 10, CacheCreationInputTokens: 5,
		},
	})
	h.onAgentEvent(agentcontract.AgentEvent{
		Kind: agentcontract.KindAssistantText, At: now.Add(-15 * time.Second),
		Text: "second turn",
		Usage: &agentcontract.UsageInfo{
			InputTokens: 200, OutputTokens: 40,
			CacheReadInputTokens: 0, CacheCreationInputTokens: 0,
		},
	})
	if h.iterTokens != 100+20+10+5+200+40 {
		t.Errorf("iterTokens = %d, want %d", h.iterTokens, 375)
	}
	if h.totalTokens != 0 {
		t.Errorf("totalTokens should still be 0 before result_summary; got %d", h.totalTokens)
	}
	// Live display sums totalTokens + iterTokens.
	if got := h.totalTokens + h.iterTokens; got != 375 {
		t.Errorf("live total = %d, want 375", got)
	}

	// Terminal result event reconciles to the authoritative four-field
	// total and resets the per-iteration counter. The authoritative value
	// can differ slightly from the per-message sum (ok by design).
	h.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindResultSummary, At: now, Done: &agentcontract.ResultSummaryInfo{
		InputTokens: 320, OutputTokens: 64,
		CacheReadInputTokens: 12, CacheCreationInputTokens: 5,
		TotalCostUSD: 0.10,
	}})
	if h.iterTokens != 0 {
		t.Errorf("iterTokens should reset on result_summary; got %d", h.iterTokens)
	}
	if h.totalTokens != 320+64+12+5 {
		t.Errorf("totalTokens = %d, want %d", h.totalTokens, 401)
	}
}

func TestHealth_view_RendersAllRows(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	h := healthPanel{startedAt: now.Add(-time.Minute)}
	h.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindToolUse, At: now.Add(-10 * time.Second), Tool: &agentcontract.ToolCallInfo{}})
	h.onAgentEvent(agentcontract.AgentEvent{Kind: agentcontract.KindResultSummary, At: now, Done: &agentcontract.ResultSummaryInfo{
		InputTokens: 1500, OutputTokens: 700, TotalCostUSD: 0.42,
	}})

	out := h.view(now, 40)
	for _, w := range []string{"heartbeat", "tools/min", "errors", "rate", "tokens", "cost"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing label %q\n%s", w, out)
		}
	}
	if !strings.Contains(out, "$0.42") {
		t.Errorf("cost not rendered as $0.42: %q", out)
	}
	if !strings.Contains(out, "2.2k") {
		t.Errorf("tokens not humanised to 2.2k: %q", out)
	}
}

func TestToolsPerMin_CountsRecentOnly(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	stamps := []time.Time{
		now.Add(-90 * time.Second), // outside window
		now.Add(-30 * time.Second),
		now.Add(-10 * time.Second),
		now.Add(-1 * time.Second),
	}
	if got := toolsPerMin(stamps, now); got != 3 {
		t.Errorf("tools/min = %d, want 3", got)
	}
}

func TestCountSince_HandlesEmpty(t *testing.T) {
	if got := countSince(nil, time.Now()); got != 0 {
		t.Errorf("empty stamps should yield 0, got %d", got)
	}
}

func TestPushStamp_TrimsToCap(t *testing.T) {
	var s []time.Time
	for i := 0; i < healthRingCap+50; i++ {
		s = pushStamp(s, time.Unix(int64(i), 0))
	}
	if len(s) != healthRingCap {
		t.Errorf("pushStamp cap not enforced: len=%d, want %d", len(s), healthRingCap)
	}
	// Most recent stamp must be the last we pushed.
	if s[len(s)-1].Unix() != int64(healthRingCap+50-1) {
		t.Errorf("last stamp = %v, want %d", s[len(s)-1], healthRingCap+50-1)
	}
}

func TestHealth_view_LoopSuspectRowAppearsWhenTriggered(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)
	h := healthPanel{startedAt: now.Add(-time.Minute)}

	// Below threshold: no row.
	for i := 0; i < loopSuspectThreshold-1; i++ {
		h.onAgentEvent(agentcontract.AgentEvent{
			Kind: agentcontract.KindToolUse, At: now,
			Tool: &agentcontract.ToolCallInfo{Name: "Edit", Args: map[string]any{"file_path": "plan.go"}},
		})
	}
	for i := 0; i < loopSuspectWindow-(loopSuspectThreshold-1); i++ {
		h.onAgentEvent(agentcontract.AgentEvent{
			Kind: agentcontract.KindToolUse, At: now,
			Tool: &agentcontract.ToolCallInfo{Name: "Read", Args: map[string]any{"file_path": "other.go"}},
		})
	}
	if got := h.view(now, 40); strings.Contains(got, "loop-suspect") {
		t.Errorf("loop-suspect row should be hidden below threshold; got\n%s", got)
	}

	// Bump one Read → Edit so the dominant key reaches threshold.
	h.suspect = loopSuspect{}
	for i := 0; i < loopSuspectThreshold; i++ {
		h.onAgentEvent(agentcontract.AgentEvent{
			Kind: agentcontract.KindToolUse, At: now,
			Tool: &agentcontract.ToolCallInfo{Name: "Edit", Args: map[string]any{"file_path": "plan.go"}},
		})
	}
	for i := 0; i < loopSuspectWindow-loopSuspectThreshold; i++ {
		h.onAgentEvent(agentcontract.AgentEvent{
			Kind: agentcontract.KindToolUse, At: now,
			Tool: &agentcontract.ToolCallInfo{Name: "Read", Args: map[string]any{"file_path": "other.go"}},
		})
	}
	out := h.view(now, 40)
	if !strings.Contains(out, "loop-suspect") {
		t.Errorf("loop-suspect row missing when triggered: %q", out)
	}
	if !strings.Contains(out, "Edit plan.go") {
		t.Errorf("loop-suspect row should name dominant key; got %q", out)
	}
	if !strings.Contains(out, "7/10") {
		t.Errorf("loop-suspect row should show count/window; got %q", out)
	}
}

func TestFormatTokens(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1500, "1.5k"},
		{12345, "12.3k"},
		{1_500_000, "1.5M"},
	}
	for _, c := range cases {
		if got := formatTokens(c.n); got != c.want {
			t.Errorf("formatTokens(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
