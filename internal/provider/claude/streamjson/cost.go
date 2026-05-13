package streamjson

import "github.com/fgmacedo/buchecha/internal/loop/agentcontract"

// LastResultSummary scans events in reverse and returns the
// ResultSummaryInfo from the last KindResultSummary entry with a non-nil
// Done field. Returns (nil, false) when no such entry is present.
//
// Callers translate the returned summary into loop.SpawnCost (or any
// other downstream wire shape) themselves; this package keeps the
// vendor-neutral TokenUsage as its only token-bearing type.
func LastResultSummary(events []agentcontract.AgentEvent) (*agentcontract.ResultSummaryInfo, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Kind == agentcontract.KindResultSummary && ev.Done != nil {
			return ev.Done, true
		}
	}
	return nil, false
}
