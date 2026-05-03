package loop

import (
	"encoding/json"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// MarshalJSONEvent serialises ev into one NDJSON line per the schema
// documented in the Phase 2 spec. The returned bytes do NOT include a
// trailing newline; callers append "\n" before writing to stdout.
//
// Schema rules: every line carries "type", "at", and "level"; the rest
// of the fields depend on the variant. The schema is additive; new
// fields and new "type" values may be introduced without a version
// bump and consumers are expected to ignore unknown ones.
func MarshalJSONEvent(ev Event) ([]byte, error) {
	return json.Marshal(jsonPayload(ev))
}

func jsonPayload(ev Event) map[string]any {
	level := LevelOf(ev).String()
	switch e := ev.(type) {
	case IterationStarted:
		out := map[string]any{
			"type":     "iter_started",
			"at":       formatAt(e.At),
			"level":    level,
			"index":    e.Index,
			"max_iter": e.MaxIter,
		}
		if e.BaselineSHA != "" {
			out["baseline_sha"] = e.BaselineSHA
		}
		return out
	case IterationFinished:
		return map[string]any{
			"type":          "iter_finished",
			"at":            formatAt(e.At),
			"level":         level,
			"index":         e.Index,
			"signal":        e.Signal.String(),
			"head_advanced": e.HEADAdvanced,
			"duration_ms":   e.DurationMS,
		}
	case LoopFinished:
		return map[string]any{
			"type":      "loop_finished",
			"at":        formatAt(e.At),
			"level":     level,
			"reason":    e.Reason,
			"exit_code": e.ExitCode,
		}
	case AgentEventReceived:
		return agentEventJSON(e.Event, level)
	case PhasePlanned:
		out := map[string]any{
			"type":  "phase_planned",
			"at":    formatAt(e.At),
			"level": level,
		}
		if e.Plan != nil {
			out["spec_hash"] = e.Plan.SpecHash
			out["goal"] = e.Plan.Goal
			out["phases"] = len(e.Plan.Phases)
		}
		return out
	case PhaseBriefed:
		out := map[string]any{
			"type":     "phase_briefed",
			"at":       formatAt(e.At),
			"level":    level,
			"phase_id": e.PhaseID,
			"attempt":  e.Attempt,
		}
		return out
	case PhaseReviewed:
		out := map[string]any{
			"type":     "phase_reviewed",
			"at":       formatAt(e.At),
			"level":    level,
			"phase_id": e.PhaseID,
			"attempt":  e.Attempt,
		}
		if e.Outcome != "" {
			out["outcome"] = e.Outcome
		}
		if e.Reasoning != "" {
			out["reasoning"] = e.Reasoning
		}
		return out
	case DirectorEscalation:
		return map[string]any{
			"type":      "director_escalation",
			"at":        formatAt(e.At),
			"level":     level,
			"phase_id":  e.PhaseID,
			"attempt":   e.Attempt,
			"reasoning": e.Reasoning,
		}
	default:
		return map[string]any{
			"type":  "unknown",
			"level": level,
		}
	}
}

func agentEventJSON(ae agentcontract.AgentEvent, level string) map[string]any {
	out := map[string]any{
		"type":  "agent_event",
		"at":    formatAt(ae.At),
		"level": level,
		"kind":  string(ae.Kind),
	}
	switch ae.Kind {
	case agentcontract.KindInit:
		if ae.Init != nil {
			out["init"] = map[string]any{
				"session_id": ae.Init.SessionID,
				"model":      ae.Init.Model,
				"cwd":        ae.Init.CWD,
			}
		}
	case agentcontract.KindThinking, agentcontract.KindAssistantText:
		out["text"] = ae.Text
		if ae.Kind == agentcontract.KindAssistantText && ae.Usage != nil {
			out["usage"] = map[string]any{
				"input_tokens":                ae.Usage.InputTokens,
				"output_tokens":               ae.Usage.OutputTokens,
				"cache_read_input_tokens":     ae.Usage.CacheReadInputTokens,
				"cache_creation_input_tokens": ae.Usage.CacheCreationInputTokens,
			}
		}
	case agentcontract.KindToolUse:
		if ae.Tool != nil {
			tool := map[string]any{
				"id":   ae.Tool.ID,
				"name": ae.Tool.Name,
			}
			if ae.Tool.Args != nil {
				tool["args"] = ae.Tool.Args
			}
			out["tool"] = tool
		}
	case agentcontract.KindToolResult:
		if ae.Tool != nil {
			out["tool"] = map[string]any{
				"id":       ae.Tool.ID,
				"is_error": ae.Tool.IsError,
				"summary":  ae.Tool.Summary,
			}
		}
	case agentcontract.KindRateLimit:
		if ae.Rate != nil {
			rate := map[string]any{
				"status": ae.Rate.Status,
			}
			if !ae.Rate.ResetAt.IsZero() {
				rate["reset_at"] = formatAt(ae.Rate.ResetAt)
			}
			out["rate"] = rate
		}
	case agentcontract.KindResultSummary:
		if ae.Done != nil {
			out["done"] = map[string]any{
				"num_turns":                   ae.Done.NumTurns,
				"total_cost_usd":              ae.Done.TotalCostUSD,
				"input_tokens":                ae.Done.InputTokens,
				"output_tokens":               ae.Done.OutputTokens,
				"cache_read_input_tokens":     ae.Done.CacheReadInputTokens,
				"cache_creation_input_tokens": ae.Done.CacheCreationInputTokens,
				"duration_ms":                 ae.Done.DurationMS,
			}
		}
	}
	return out
}

func formatAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
