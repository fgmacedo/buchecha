package loop

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// AllEventKinds is the canonical, exhaustive list of "type" values
// MarshalJSONEvent emits for the closed Event union. Order matches the
// MarshalJSONEvent switch arms. The api package's event.schema.json
// enum mirrors this list; TestAllEventKindsMatchSchema enforces the
// match. Adding a new Event variant requires updating this slice, the
// MarshalJSONEvent switch, the schema enum, and the sample registry in
// the eventjson_test sample table; the corresponding tests fail loudly
// when any of those drift.
var AllEventKinds = []string{
	"iter_started",
	"iter_finished",
	"loop_finished",
	"agent_event",
	"phase_planned",
	"phase_briefed",
	"phase_reviewed",
	"spawn_finished",
	"spawn_started",
	"task_started",
	"task_completed",
	"task_approved",
	"task_needs_fix",
	"director_escalation",
}

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
			"type":      "phase_briefed",
			"at":        formatAt(e.At),
			"level":     level,
			"phase_id":  e.PhaseID,
			"iteration": e.Iteration,
		}
		assignments := map[string]any{}
		if a := assignmentJSON(e.BrieferModel, e.BrieferEffort, e.BrieferSkipped); a != nil {
			assignments["briefer"] = a
		}
		if a := assignmentJSON(e.ExecutorModel, e.ExecutorEffort, false); a != nil {
			assignments["executor"] = a
		}
		if a := assignmentJSON(e.ReviewerModel, e.ReviewerEffort, e.ReviewSkipped); a != nil {
			assignments["reviewer"] = a
		}
		if len(assignments) > 0 {
			out["assignments"] = assignments
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
	case TaskStarted:
		out := map[string]any{
			"type":     "task_started",
			"at":       formatAt(e.At),
			"level":    level,
			"phase_id": e.PhaseID,
			"task_id":  e.TaskID,
		}
		if e.AgentID != "" {
			out["agent_id"] = e.AgentID
		}
		return out
	case TaskCompleted:
		out := map[string]any{
			"type":     "task_completed",
			"at":       formatAt(e.At),
			"level":    level,
			"phase_id": e.PhaseID,
			"task_id":  e.TaskID,
		}
		if e.AgentID != "" {
			out["agent_id"] = e.AgentID
		}
		return out
	case TaskApproved:
		out := map[string]any{
			"type":     "task_approved",
			"at":       formatAt(e.At),
			"level":    level,
			"phase_id": e.PhaseID,
			"task_id":  e.TaskID,
		}
		if e.AgentID != "" {
			out["agent_id"] = e.AgentID
		}
		return out
	case TaskNeedsFix:
		out := map[string]any{
			"type":     "task_needs_fix",
			"at":       formatAt(e.At),
			"level":    level,
			"phase_id": e.PhaseID,
			"task_id":  e.TaskID,
		}
		if e.AgentID != "" {
			out["agent_id"] = e.AgentID
		}
		if e.Note != "" {
			out["note"] = e.Note
		}
		return out
	case SpawnStarted:
		out := map[string]any{
			"type":     "spawn_started",
			"at":       formatAt(e.At),
			"level":    level,
			"spawn_id": e.SpawnID,
			"role":     e.Role,
		}
		if e.PhaseID != "" {
			out["phase_id"] = e.PhaseID
		}
		if e.TaskID != "" {
			out["task_id"] = e.TaskID
		}
		if e.IterationID != "" {
			out["iteration_id"] = e.IterationID
		}
		if e.Attempt != 0 {
			out["attempt"] = e.Attempt
		}
		if e.Provider != "" {
			out["provider"] = e.Provider
		}
		if e.Model != "" {
			out["model"] = e.Model
		}
		if e.Effort != "" {
			out["effort"] = e.Effort
		}
		if e.PromptPath != "" {
			out["prompt_path"] = e.PromptPath
		}
		return out
	case SpawnFinished:
		return map[string]any{
			"type":        "spawn_finished",
			"at":          formatAt(e.At),
			"level":       level,
			"spawn_id":    e.SpawnID,
			"role":        e.Role,
			"exit_code":   e.ExitCode,
			"duration_ms": e.DurationMS,
			"cost": map[string]any{
				"input_tokens":                e.Cost.InputTokens,
				"output_tokens":               e.Cost.OutputTokens,
				"cache_read_input_tokens":     e.Cost.CacheReadTokens,
				"cache_creation_input_tokens": e.Cost.CacheCreateTokens,
				"usd":                         e.Cost.USD,
			},
		}
	default:
		// A new Event variant landed without an arm here. Log loudly so
		// the gap is visible in production, then synthesize a payload that
		// preserves the Go type name so an operator can grep the source.
		// The companion test TestMarshalJSONEvent_AllKindsCovered also
		// guards this at build time.
		slog.Error("loop: MarshalJSONEvent missing case for event type",
			"go_type", fmt.Sprintf("%T", ev),
		)
		return map[string]any{
			"type":    "unknown",
			"level":   level,
			"go_type": fmt.Sprintf("%T", ev),
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
	// Origin is populated by adapters (agent_id, role, phase_id,
	// iteration_id, attempt) and by the EventService fan-out (task_id).
	// Each field is optional on the wire so legacy fixtures and tests
	// that build AgentEvents with zero values keep working.
	if ae.AgentID != "" {
		out["agent_id"] = ae.AgentID
	}
	if ae.Role != "" {
		out["role"] = string(ae.Role)
	}
	if ae.PhaseID != "" {
		out["phase_id"] = ae.PhaseID
	}
	if ae.IterationID != "" {
		out["iteration_id"] = ae.IterationID
	}
	if ae.Attempt > 0 {
		out["attempt"] = ae.Attempt
	}
	if ae.TaskID != "" {
		out["task_id"] = ae.TaskID
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

// assignmentJSON returns the per-role spawn parameters for the
// phase_briefed event. Returns nil when nothing useful applies (no
// model, no effort, not skipped) so the caller can omit the role
// entry entirely.
func assignmentJSON(model, effort string, skipped bool) map[string]any {
	if model == "" && effort == "" && !skipped {
		return nil
	}
	out := map[string]any{}
	if model != "" {
		out["model"] = model
	}
	if effort != "" {
		out["effort"] = effort
	}
	if skipped {
		out["skipped"] = true
	}
	return out
}
