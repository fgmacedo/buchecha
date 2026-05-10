// Package streamjson parses Claude's `--output-format stream-json`
// envelope into normalized agentcontract.AgentEvent values. The same
// parser is used by the executor adapter (per-iteration agent runs)
// and the director adapter (planner / briefer / reviewer runs); both
// receive identical event shapes and emit them on the caller's
// chan<- agentcontract.AgentEvent.
//
// The parser is pure: input is a single stream-json line as bytes plus
// a timestamp, output is zero or more AgentEvents. Unknown top-level
// types are silently dropped so the wire format can evolve without
// blocking iteration.
package streamjson

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// Stream reads stream-json from r line by line, parses each line into
// zero or more AgentEvents, and forwards each event on the events
// channel. Returns when r EOFs or ctx is done. Lines longer than
// maxLine are dropped by bufio.Scanner; callers pick a generous bound.
func Stream(ctx context.Context, r io.Reader, events chan<- agentcontract.AgentEvent, maxLine int) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	for sc.Scan() {
		raw := slices.Clone(sc.Bytes())
		for _, ev := range ParseLine(raw, time.Now()) {
			select {
			case events <- ev:
			case <-ctx.Done():
				return
			}
		}
	}
}

// ParseLine turns one stream-json line into zero or more normalized
// AgentEvents. Unknown top-level types are silently dropped: the wire
// format evolves and unknown events do not block iteration.
//
// `at` is stamped onto every produced event; callers pass time.Now()
// when reading off the live pipe and a fixed time in tests.
func ParseLine(raw []byte, at time.Time) []agentcontract.AgentEvent {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil
	}
	switch head.Type {
	case "system":
		return parseSystem(raw, at)
	case "assistant":
		return parseAssistant(raw, at)
	case "user":
		return parseUser(raw, at)
	case "rate_limit_event":
		return parseRateLimit(raw, at)
	case "result":
		return parseResult(raw, at)
	default:
		return nil
	}
}

func parseSystem(raw []byte, at time.Time) []agentcontract.AgentEvent {
	var v struct {
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
		Model     string `json:"model"`
		CWD       string `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &v); err != nil || v.Subtype != "init" {
		return nil
	}
	return []agentcontract.AgentEvent{{
		Kind: agentcontract.KindInit,
		At:   at,
		Init: &agentcontract.InitInfo{SessionID: v.SessionID, Model: v.Model, CWD: v.CWD},
	}}
}

// assistantContent matches each item of message.content on assistant
// events. Fields not relevant to a given subtype stay at zero.
type assistantContent struct {
	Type     string         `json:"type"`
	Text     string         `json:"text"`
	Thinking string         `json:"thinking"`
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Input    map[string]any `json:"input"`
}

func parseAssistant(raw []byte, at time.Time) []agentcontract.AgentEvent {
	var v struct {
		Message struct {
			Content []assistantContent `json:"content"`
			Usage   anthropicUsageRaw  `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	out := make([]agentcontract.AgentEvent, 0, len(v.Message.Content))
	for _, c := range v.Message.Content {
		switch c.Type {
		case "text":
			if strings.TrimSpace(c.Text) == "" {
				continue
			}
			out = append(out, agentcontract.AgentEvent{
				Kind: agentcontract.KindAssistantText,
				At:   at,
				Text: c.Text,
			})
		case "thinking":
			if strings.TrimSpace(c.Thinking) == "" {
				continue
			}
			out = append(out, agentcontract.AgentEvent{
				Kind: agentcontract.KindThinking,
				At:   at,
				Text: c.Thinking,
			})
		case "tool_use":
			out = append(out, agentcontract.AgentEvent{
				Kind: agentcontract.KindToolUse,
				At:   at,
				Tool: &agentcontract.ToolCallInfo{ID: c.ID, Name: c.Name, Args: c.Input},
			})
		}
	}
	if usage := anthropicUsage(v.Message.Usage); !usage.IsZero() {
		for i := range out {
			if out[i].Kind == agentcontract.KindAssistantText {
				out[i].Usage = &usage
				break
			}
		}
	}
	return out
}

// anthropicUsageRaw is the four-field shape Claude's stream-json carries
// on assistant.usage and result.usage. Adapter-internal; never escapes.
type anthropicUsageRaw struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// anthropicUsage normalizes Claude's four-field usage payload into the
// vendor-neutral 5-bucket TokenUsage. Mapping:
//
//	input_tokens                → InputFresh   (uncached prompt)
//	cache_read_input_tokens     → InputCached  (served from prefix cache)
//	cache_creation_input_tokens → CacheWrite   (written to cache)
//	output_tokens               → Output
//	(reasoning is 0 today; extended-thinking would surface it)
//
// The four Anthropic buckets are already disjoint and additive, so the
// normalization is a 1:1 rename — no subtraction needed (unlike OpenAI
// and Gemini, where cached_tokens is a subset of prompt_tokens).
func anthropicUsage(u anthropicUsageRaw) agentcontract.TokenUsage {
	return agentcontract.TokenUsage{
		InputFresh:  u.InputTokens,
		InputCached: u.CacheReadInputTokens,
		CacheWrite:  u.CacheCreationInputTokens,
		Output:      u.OutputTokens,
		Reasoning:   0,
		Provider:    agentcontract.ProviderAnthropic,
	}
}

func parseUser(raw []byte, at time.Time) []agentcontract.AgentEvent {
	var v struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(v.Message.Content, &items); err != nil {
		return nil
	}
	var out []agentcontract.AgentEvent
	for _, item := range items {
		var tr struct {
			Type      string          `json:"type"`
			ToolUseID string          `json:"tool_use_id"`
			IsError   bool            `json:"is_error"`
			Content   json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(item, &tr); err != nil || tr.Type != "tool_result" {
			continue
		}
		out = append(out, agentcontract.AgentEvent{
			Kind: agentcontract.KindToolResult,
			At:   at,
			Tool: &agentcontract.ToolCallInfo{
				ID:      tr.ToolUseID,
				IsError: tr.IsError,
				Summary: summarizeToolResult(tr.Content),
			},
		})
	}
	return out
}

// summarizeToolResult flattens the heterogeneous content shape of a
// tool_result block into a plain string. Claude emits either a string
// (most tools) or an array of {type:"text", text:"..."} parts (some
// MCP-backed tools); other shapes degrade to an empty string.
func summarizeToolResult(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type != "text" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return ""
}

func parseRateLimit(raw []byte, at time.Time) []agentcontract.AgentEvent {
	var v struct {
		Info struct {
			Status   string `json:"status"`
			ResetsAt int64  `json:"resetsAt"`
		} `json:"rate_limit_info"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	var resetAt time.Time
	if v.Info.ResetsAt > 0 {
		resetAt = time.Unix(v.Info.ResetsAt, 0)
	}
	return []agentcontract.AgentEvent{{
		Kind: agentcontract.KindRateLimit,
		At:   at,
		Rate: &agentcontract.RateLimitInfo{Status: v.Info.Status, ResetAt: resetAt},
	}}
}

func parseResult(raw []byte, at time.Time) []agentcontract.AgentEvent {
	var v struct {
		NumTurns     int               `json:"num_turns"`
		TotalCostUSD float64           `json:"total_cost_usd"`
		DurationMS   int64             `json:"duration_ms"`
		Usage        anthropicUsageRaw `json:"usage"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return []agentcontract.AgentEvent{{
		Kind: agentcontract.KindResultSummary,
		At:   at,
		Done: &agentcontract.ResultSummaryInfo{
			NumTurns:     v.NumTurns,
			TotalCostUSD: v.TotalCostUSD,
			DurationMS:   v.DurationMS,
			Tokens:       anthropicUsage(v.Usage),
		},
	}}
}
