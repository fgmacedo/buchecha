package tui

import (
	"fmt"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"strings"
	"time"
)

// healthPanel surfaces the run-level vital signs: heartbeat, throughput
// (tools/min, 60s window), error count (5m window), rate-limit status,
// token usage, cost, and the loop-suspect heuristic (≥ 7 of the last 10
// tool calls share the same (name, primary_arg) key).
type healthPanel struct {
	startedAt    time.Time
	lastEvent    time.Time
	totalTools   int
	totalErrors  int
	totalCostUSD float64
	rate         agentcontract.RateLimitInfo

	// totalTokens accumulates the authoritative token sum from completed
	// iterations (reconciled from KindResultSummary at each iteration end).
	totalTokens int64

	// iterTokens accumulates per-message usage from KindAssistantText events
	// during the current in-progress iteration. Reset at each IterationStarted
	// and reconciled into totalTokens when KindResultSummary arrives.
	// Showing totalTokens+iterTokens in the view gives a live count.
	iterTokens int64

	// toolStamps is a ring of recent tool_use timestamps used for the
	// 60s tools/min figure. Bounded to 1024 entries to keep memory flat
	// regardless of run length.
	toolStamps []time.Time

	// errorStamps is the equivalent ring for tool_result with IsError,
	// capped at 1024 entries. The 5-minute window is computed at view
	// time.
	errorStamps []time.Time

	// suspect tracks the last 10 tool_use keys for the loop-suspect
	// heuristic. Renders a warning row when triggered.
	suspect loopSuspect
}

const healthRingCap = 1024

// onAny stamps the heartbeat for any loop event (IterationStarted,
// IterationFinished, LoopFinished) so the heartbeat dot stays green
// throughout the run, not only while agent events are flowing.
func (h *healthPanel) onAny(at time.Time) {
	if at.IsZero() {
		return
	}
	if at.After(h.lastEvent) {
		h.lastEvent = at
	}
}

// onIterStarted resets the current-iteration token counter so the live
// running total does not carry over stale partial counts from the
// previous iteration. Called before the executor starts each new iteration.
func (h *healthPanel) onIterStarted() {
	h.iterTokens = 0
}

// onAgentEvent folds an agent event into the panel's counters. It
// trims the timestamp rings on every push so the slice never grows
// past healthRingCap.
func (h *healthPanel) onAgentEvent(ev agentcontract.AgentEvent) {
	at := ev.At
	if at.IsZero() {
		at = time.Now()
	}
	h.onAny(at)

	switch ev.Kind {
	case agentcontract.KindToolUse:
		h.totalTools++
		h.toolStamps = pushStamp(h.toolStamps, at)
		h.suspect.onAgentEvent(ev)
	case agentcontract.KindToolResult:
		if ev.Tool != nil && ev.Tool.IsError {
			h.totalErrors++
			h.errorStamps = pushStamp(h.errorStamps, at)
		}
	case agentcontract.KindRateLimit:
		if ev.Rate != nil {
			h.rate = *ev.Rate
		}
	case agentcontract.KindAssistantText:
		// Accumulate per-message token usage incrementally so the health
		// panel shows a live count during the iteration. The terminal
		// KindResultSummary reconciles to the authoritative total.
		if ev.Usage != nil {
			h.iterTokens += ev.Usage.Total()
		}
	case agentcontract.KindResultSummary:
		if ev.Done != nil {
			// Reconcile: replace the live per-message estimate with the
			// authoritative 5-bucket total from the terminal result event.
			h.totalTokens += ev.Done.Tokens.Total()
			h.iterTokens = 0
			h.totalCostUSD += ev.Done.TotalCostUSD
		}
	}
}

// view renders the panel body (heartbeat, rates, totals). width is
// reserved for future per-panel sizing; the rows are naturally short
// (a single label + value) so no truncation is currently needed.
func (h healthPanel) view(now time.Time, _ int) string {
	var b strings.Builder

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

	b.WriteString(fmt.Sprintf("  tokens: %s\n", formatTokens(h.totalTokens+h.iterTokens)))
	b.WriteString(fmt.Sprintf("  cost: $%.2f\n", h.totalCostUSD))

	if key, count, ok := h.suspect.triggered(); ok {
		b.WriteString("  ")
		b.WriteString(theme.err.Render(fmt.Sprintf(
			"loop-suspect: %s (%d/%d)",
			suspectLabel(key), count, loopSuspectWindow,
		)))
		b.WriteByte('\n')
	}
	return b.String()
}

// suspectLabel renders the dominant key as one short line: "Edit x.go"
// or just "Edit" when the tool has no primary arg.
func suspectLabel(k loopSuspectKey) string {
	if k.arg == "" {
		return k.name
	}
	return k.name + " " + truncate(k.arg, 40)
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
