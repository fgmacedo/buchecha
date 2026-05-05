package streamjson

import "github.com/fgmacedo/buchecha/internal/loop/agentcontract"

// Cost holds the token and USD cost fields extracted from a result_summary
// event. Field names mirror loop.SpawnCost; this package does not import
// internal/loop to avoid circular imports. Conversion to loop.SpawnCost is
// the call site's responsibility.
type Cost struct {
	InputTokens       int
	OutputTokens      int
	CacheReadTokens   int
	CacheCreateTokens int
	USD               float64
}

// LastResultSummary scans events in reverse and returns the Cost extracted
// from the last KindResultSummary entry with a non-nil Done field. Returns
// (Cost{}, false) when no such entry is present in the slice.
func LastResultSummary(events []agentcontract.AgentEvent) (Cost, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Kind == agentcontract.KindResultSummary && ev.Done != nil {
			return Cost{
				InputTokens:       int(ev.Done.InputTokens),
				OutputTokens:      int(ev.Done.OutputTokens),
				CacheReadTokens:   int(ev.Done.CacheReadInputTokens),
				CacheCreateTokens: int(ev.Done.CacheCreationInputTokens),
				USD:               ev.Done.TotalCostUSD,
			}, true
		}
	}
	return Cost{}, false
}
