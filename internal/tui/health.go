package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// healthPanel surfaces the run-level vital signs: heartbeat, throughput
// (tools/min), error count, rate-limit status, token usage, and cost.
//
// P2.5 keeps the math simple (cumulative counts, instantaneous rate
// from a 60-second sliding window) so panels render meaningful numbers
// from the first event. P2.6 swaps the windowed pieces in for the
// full sliding-window heuristic without changing the panel surface.
type healthPanel struct {
	startedAt    time.Time
	lastEvent    time.Time
	totalTools   int
	totalErrors  int
	totalTokens  int64
	totalCostUSD float64
	rate         loop.RateLimitInfo

	// toolStamps is a ring of recent tool_use timestamps used for the
	// 60s tools/min figure. Bounded to 1024 entries to keep memory flat
	// regardless of run length.
	toolStamps []time.Time

	// errorStamps is the equivalent ring for tool_result with IsError,
	// capped at 1024 entries. The 5-minute window is computed at view
	// time.
	errorStamps []time.Time
}

const healthRingCap = 1024

// onAgentEvent folds an agent event into the panel's counters. It
// trims the timestamp rings on every push so the slice never grows
// past healthRingCap.
func (h *healthPanel) onAgentEvent(ev loop.AgentEvent) {
	at := ev.At
	if at.IsZero() {
		at = time.Now()
	}
	h.lastEvent = at

	switch ev.Kind {
	case loop.KindToolUse:
		h.totalTools++
		h.toolStamps = pushStamp(h.toolStamps, at)
	case loop.KindToolResult:
		if ev.Tool != nil && ev.Tool.IsError {
			h.totalErrors++
			h.errorStamps = pushStamp(h.errorStamps, at)
		}
	case loop.KindRateLimit:
		if ev.Rate != nil {
			h.rate = *ev.Rate
		}
	case loop.KindResultSummary:
		if ev.Done != nil {
			h.totalTokens += ev.Done.InputTokens + ev.Done.OutputTokens
			h.totalCostUSD += ev.Done.TotalCostUSD
		}
	}
}

// view renders the panel body (heartbeat, rates, totals).
func (h healthPanel) view(now time.Time) string {
	var b strings.Builder
	b.WriteString(panelTitle("health"))
	b.WriteByte('\n')

	heartbeat := "..."
	if !h.lastEvent.IsZero() {
		heartbeat = formatDuration(now.Sub(h.lastEvent))
	}
	b.WriteString(fmt.Sprintf("  heartbeat: %s %s\n", heartbeat, aliveDot(h.lastEvent, now)))

	b.WriteString(fmt.Sprintf("  tools/min: %d\n", toolsPerMin(h.toolStamps, now)))

	errCount := countSince(h.errorStamps, now.Add(-5*time.Minute))
	errLine := fmt.Sprintf("  errors (5m): %d", errCount)
	if errCount > 0 {
		errLine = "  errors (5m): " + theme.err.Render(fmt.Sprintf("%d", errCount))
	}
	b.WriteString(errLine)
	b.WriteByte('\n')

	rate := "ok"
	if h.rate.Status != "" && h.rate.Status != "allowed" {
		rate = theme.err.Render(h.rate.Status)
	}
	b.WriteString("  rate: " + rate + "\n")

	b.WriteString(fmt.Sprintf("  tokens: %s\n", formatTokens(h.totalTokens)))
	b.WriteString(fmt.Sprintf("  cost: $%.2f\n", h.totalCostUSD))
	return b.String()
}

// pushStamp appends t to s and trims to the most recent healthRingCap
// entries. Cheap because the cap is small.
func pushStamp(s []time.Time, t time.Time) []time.Time {
	s = append(s, t)
	if len(s) > healthRingCap {
		s = s[len(s)-healthRingCap:]
	}
	return s
}

// countSince returns the number of stamps in s at or after threshold.
// stamps are append-only, so a linear walk from the back is enough
// for the ring sizes we keep.
func countSince(stamps []time.Time, threshold time.Time) int {
	n := 0
	for i := len(stamps) - 1; i >= 0; i-- {
		if stamps[i].Before(threshold) {
			break
		}
		n++
	}
	return n
}

// toolsPerMin returns the count of tool_use events in the trailing 60
// seconds. The window is fixed; P2.6 may tune the threshold but the
// shape stays the same.
func toolsPerMin(stamps []time.Time, now time.Time) int {
	return countSince(stamps, now.Add(-60*time.Second))
}

// formatTokens humanises a token total: 1234 → "1.2k", 1234567 →
// "1.2M". Below 1000 the raw integer is returned.
func formatTokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
}
