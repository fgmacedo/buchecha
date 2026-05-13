// Package jsonl parses codex CLI's `--json` output (one JSON envelope per
// line) into vendor-neutral agentcontract.AgentEvent values. The parser
// mirrors the role of internal/provider/claude/streamjson for the codex
// adapter: input is a single line of bytes plus a timestamp, output is
// zero or more AgentEvents.
//
// The codex 0.130 wire schema is not yet documented upstream. The mapping
// here is grounded in real fixtures captured under testdata/. The parser
// stays tolerant: unknown top-level types and unknown nested item shapes
// are logged at slog.Debug and dropped so the wire format can evolve
// without blocking iteration.
package jsonl

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// ParseLine turns one codex JSONL line into zero or more AgentEvents.
//
// The mapping per the briefing (P5/T5.2) is:
//
//   - agent_message     -> KindAssistantText
//   - tool_call         -> KindToolUse
//   - tool_call_result  -> KindToolResult
//   - task_complete     -> KindResultSummary
//   - result            -> KindResultSummary
//
// In codex 0.130 the wire wraps tool invocations inside `item.started`
// (start) and `item.completed` (finish) envelopes; the nested
// `item.type` carries the semantic tag (agent_message, command_execution,
// file_change, ...). The parser dispatches on the top-level `type` first
// and looks at the nested item only when the envelope is an item event.
// turn.completed carries the per-turn usage totals and maps to
// KindResultSummary.
//
// `at` is stamped onto every produced event; callers pass time.Now() on
// the live pipe and a fixed time in tests.
func ParseLine(line []byte, at time.Time) ([]agentcontract.AgentEvent, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		// Malformed lines never block the stream; the agent's wire format
		// must be tolerant of trailing whitespace or accidental noise.
		return nil, nil
	}
	switch head.Type {
	case "item.started":
		return parseItem(line, at, false)
	case "item.completed":
		return parseItem(line, at, true)
	case "turn.completed":
		return parseTurnCompleted(line, at)
	case "agent_message":
		// Defensive: future codex versions may flatten the envelope.
		return parseFlatAgentMessage(line, at)
	case "tool_call":
		return parseFlatToolCall(line, at, false)
	case "tool_call_result":
		return parseFlatToolCall(line, at, true)
	case "task_complete", "result":
		return parseFlatResult(line, at)
	case "thread.started", "turn.started":
		// Bookkeeping envelopes carry no payload the loop consumes.
		return nil, nil
	default:
		slog.Debug("provider/codex/jsonl: unknown envelope type, dropped", "type", head.Type)
		return nil, nil
	}
}

// itemEnvelope is the shared shape of item.started / item.completed
// envelopes. Only fields the parser consumes are decoded; unknown nested
// fields fall through json.Unmarshal silently.
type itemEnvelope struct {
	Item struct {
		ID               string          `json:"id"`
		Type             string          `json:"type"`
		Text             string          `json:"text"`
		Command          string          `json:"command"`
		AggregatedOutput string          `json:"aggregated_output"`
		ExitCode         *int            `json:"exit_code"`
		Status           string          `json:"status"`
		Changes          json.RawMessage `json:"changes"`
	} `json:"item"`
}

func parseItem(line []byte, at time.Time, completed bool) ([]agentcontract.AgentEvent, error) {
	var v itemEnvelope
	if err := json.Unmarshal(line, &v); err != nil {
		return nil, nil
	}
	switch v.Item.Type {
	case "agent_message":
		// codex emits one agent_message per assistant turn-step; the
		// `item.completed` carries the final text. `item.started` for an
		// agent_message is not observed in fixtures but if it appears we
		// drop it (text is empty until completed).
		if !completed {
			return nil, nil
		}
		if strings.TrimSpace(v.Item.Text) == "" {
			return nil, nil
		}
		return []agentcontract.AgentEvent{{
			Kind: agentcontract.KindAssistantText,
			At:   at,
			Text: v.Item.Text,
		}}, nil
	case "command_execution":
		// command_execution: started -> KindToolUse (the shell command),
		// completed -> KindToolResult (aggregated_output, exit_code).
		args := map[string]any{}
		if v.Item.Command != "" {
			args["command"] = v.Item.Command
		}
		if !completed {
			return []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolUse,
				At:   at,
				Tool: &agentcontract.ToolCallInfo{
					ID:   v.Item.ID,
					Name: "command_execution",
					Args: args,
				},
			}}, nil
		}
		isErr := v.Item.ExitCode != nil && *v.Item.ExitCode != 0
		if v.Item.Status != "" && v.Item.Status != "completed" {
			isErr = true
		}
		return []agentcontract.AgentEvent{{
			Kind: agentcontract.KindToolResult,
			At:   at,
			Tool: &agentcontract.ToolCallInfo{
				ID:      v.Item.ID,
				Name:    "command_execution",
				IsError: isErr,
				Summary: strings.TrimRight(v.Item.AggregatedOutput, "\n"),
			},
		}}, nil
	case "file_change":
		// file_change: started -> KindToolUse, completed -> KindToolResult.
		args := map[string]any{}
		if len(v.Item.Changes) > 0 {
			var raw any
			if err := json.Unmarshal(v.Item.Changes, &raw); err == nil {
				args["changes"] = raw
			}
		}
		if !completed {
			return []agentcontract.AgentEvent{{
				Kind: agentcontract.KindToolUse,
				At:   at,
				Tool: &agentcontract.ToolCallInfo{
					ID:   v.Item.ID,
					Name: "file_change",
					Args: args,
				},
			}}, nil
		}
		isErr := v.Item.Status != "" && v.Item.Status != "completed"
		return []agentcontract.AgentEvent{{
			Kind: agentcontract.KindToolResult,
			At:   at,
			Tool: &agentcontract.ToolCallInfo{
				ID:      v.Item.ID,
				Name:    "file_change",
				IsError: isErr,
				Summary: summarizeChanges(v.Item.Changes),
			},
		}}, nil
	default:
		slog.Debug("provider/codex/jsonl: unknown item.type, dropped",
			"item_type", v.Item.Type, "completed", completed)
		return nil, nil
	}
}

func summarizeChanges(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var changes []struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &changes); err != nil {
		return ""
	}
	var b strings.Builder
	for i, c := range changes {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(c.Kind)
		b.WriteByte(' ')
		b.WriteString(c.Path)
	}
	return b.String()
}

// codexUsageRaw matches codex 0.130's turn.completed.usage payload.
// Adapter-internal; never escapes the package.
type codexUsageRaw struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

// codexUsage normalizes codex's four-field usage payload into the
// vendor-neutral 5-bucket TokenUsage. Mapping:
//
//	input_tokens - cached_input_tokens  -> InputFresh
//	cached_input_tokens                  -> InputCached
//	(no cache-write bucket)              -> CacheWrite = 0
//	output_tokens - reasoning_output_tokens -> Output
//	reasoning_output_tokens              -> Reasoning
//
// codex reports cached_input_tokens as a subset of input_tokens (the
// hosted prefix-cache hit count) and reasoning_output_tokens as a subset
// of output_tokens (hidden chain-of-thought billed as output). Subtracting
// keeps the buckets disjoint, matching the contract of TokenUsage.
func codexUsage(u codexUsageRaw) agentcontract.TokenUsage {
	inputFresh := max(u.InputTokens-u.CachedInputTokens, 0)
	output := max(u.OutputTokens-u.ReasoningOutputTokens, 0)
	return agentcontract.TokenUsage{
		InputFresh:  inputFresh,
		InputCached: u.CachedInputTokens,
		Output:      output,
		Reasoning:   u.ReasoningOutputTokens,
		Provider:    agentcontract.ProviderOpenAI,
	}
}

func parseTurnCompleted(line []byte, at time.Time) ([]agentcontract.AgentEvent, error) {
	var v struct {
		Usage        codexUsageRaw `json:"usage"`
		DurationMS   int64         `json:"duration_ms"`
		TotalCostUSD float64       `json:"total_cost_usd"`
	}
	if err := json.Unmarshal(line, &v); err != nil {
		return nil, nil
	}
	return []agentcontract.AgentEvent{{
		Kind: agentcontract.KindResultSummary,
		At:   at,
		Done: &agentcontract.ResultSummaryInfo{
			NumTurns:     1,
			TotalCostUSD: v.TotalCostUSD,
			DurationMS:   v.DurationMS,
			Tokens:       codexUsage(v.Usage),
		},
	}}, nil
}

// parseFlatAgentMessage handles a hypothetical future codex envelope that
// surfaces `agent_message` at the top level instead of inside item.completed.
// Today's fixtures do not exercise this branch; it exists so the parser
// honors the briefing's mapping contract verbatim and copes with the
// schema flattening if codex ships it.
func parseFlatAgentMessage(line []byte, at time.Time) ([]agentcontract.AgentEvent, error) {
	var v struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(line, &v); err != nil {
		return nil, nil
	}
	if strings.TrimSpace(v.Text) == "" {
		return nil, nil
	}
	return []agentcontract.AgentEvent{{
		Kind: agentcontract.KindAssistantText,
		At:   at,
		Text: v.Text,
	}}, nil
}

func parseFlatToolCall(line []byte, at time.Time, result bool) ([]agentcontract.AgentEvent, error) {
	var v struct {
		ID        string         `json:"id"`
		ToolUseID string         `json:"tool_use_id"`
		Name      string         `json:"name"`
		Input     map[string]any `json:"input"`
		IsError   bool           `json:"is_error"`
		Output    string         `json:"output"`
	}
	if err := json.Unmarshal(line, &v); err != nil {
		return nil, nil
	}
	id := v.ID
	if id == "" {
		id = v.ToolUseID
	}
	if result {
		return []agentcontract.AgentEvent{{
			Kind: agentcontract.KindToolResult,
			At:   at,
			Tool: &agentcontract.ToolCallInfo{
				ID:      id,
				Name:    v.Name,
				IsError: v.IsError,
				Summary: v.Output,
			},
		}}, nil
	}
	return []agentcontract.AgentEvent{{
		Kind: agentcontract.KindToolUse,
		At:   at,
		Tool: &agentcontract.ToolCallInfo{
			ID:   id,
			Name: v.Name,
			Args: v.Input,
		},
	}}, nil
}

func parseFlatResult(line []byte, at time.Time) ([]agentcontract.AgentEvent, error) {
	var v struct {
		NumTurns     int           `json:"num_turns"`
		TotalCostUSD float64       `json:"total_cost_usd"`
		DurationMS   int64         `json:"duration_ms"`
		Usage        codexUsageRaw `json:"usage"`
	}
	if err := json.Unmarshal(line, &v); err != nil {
		return nil, nil
	}
	return []agentcontract.AgentEvent{{
		Kind: agentcontract.KindResultSummary,
		At:   at,
		Done: &agentcontract.ResultSummaryInfo{
			NumTurns:     v.NumTurns,
			TotalCostUSD: v.TotalCostUSD,
			DurationMS:   v.DurationMS,
			Tokens:       codexUsage(v.Usage),
		},
	}}, nil
}

// LastResultSummary scans events in reverse and returns the
// ResultSummaryInfo from the last KindResultSummary entry with a non-nil
// Done field. Returns (nil, false) when no such entry is present.
//
// Best-effort: a turn.completed always produces a non-nil Done, but
// TotalCostUSD and DurationMS are typically zero because codex 0.130 does
// not report them. The caller leaves SpawnResult.CostUSD/DurationMS at
// the zero from this struct in that case.
func LastResultSummary(events []agentcontract.AgentEvent) (*agentcontract.ResultSummaryInfo, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Kind == agentcontract.KindResultSummary && ev.Done != nil {
			return ev.Done, true
		}
	}
	return nil, false
}
