// Package claude implements loop.Executor for Claude Code (claude CLI in
// print mode with stream-json output).
//
// This is the only adapter today; codex and gemini will be added in
// Phase 3 as sibling packages under internal/executor.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// Compile-time check that *Executor satisfies loop.Executor.
var _ loop.Executor = (*Executor)(nil)

// Config configures the Claude executor.
type Config struct {
	// Binary is the path or PATH name of the claude binary.
	Binary string

	// Model is passed via --model. Empty omits the flag.
	Model string

	// ExtraArgs are appended to the command line after --model and before
	// the prompt positional argument. Reserve for ad-hoc additions.
	ExtraArgs []string

	// SkipPermissions, when true, adds --dangerously-skip-permissions to
	// the args so claude does not stall the loop with confirmation
	// prompts. This is the documented contract of bcc's autonomous mode.
	// When false (explicit user opt-out via .bcc.toml), the loop is
	// likely to hang on the first tool call; the user accepts that.
	SkipPermissions bool

	// Stderr, when non-nil, receives the subprocess stderr verbatim.
	// Default (nil) discards stderr; callers wanting it should pipe to a
	// log file or os.Stderr explicitly.
	Stderr io.Writer

	// CancelGrace is how long to wait after sending SIGINT before forcing
	// SIGKILL. Defaults to 5 seconds when zero.
	CancelGrace time.Duration

	// MaxLineBytes caps a single stream-json line. Defaults to 8 MiB
	// when zero. Some tool_result payloads (large file reads) exceed
	// the default 64 KiB buffer of bufio.Scanner; oversize lines are
	// truncated and skipped rather than aborting the iteration.
	MaxLineBytes int
}

// Executor invokes Claude Code in print mode and streams its stream-json
// events to a writer.
type Executor struct {
	cfg Config
}

// New returns a Claude Executor with cfg.
func New(cfg Config) *Executor {
	if cfg.CancelGrace == 0 {
		cfg.CancelGrace = 5 * time.Second
	}
	if cfg.MaxLineBytes == 0 {
		cfg.MaxLineBytes = 8 * 1024 * 1024
	}
	return &Executor{cfg: cfg}
}

// Run invokes the binary with print mode and stream-json output. For
// each line emitted on stdout the adapter parses it into zero or more
// loop.AgentEvents and forwards each event on the events channel.
//
// On context cancellation the subprocess receives SIGINT first; if it
// fails to exit within CancelGrace it is killed via SIGKILL (handled by
// exec.Cmd via WaitDelay).
//
// Returns (ExecResult{ExitCode: 0}, nil) on natural completion. Returns
// (ExecResult{ExitCode: n}, nil) when the agent exits non-zero (a
// normal control-flow signal, not a Run failure). Returns (ExecResult,
// ctx.Err()) on cancellation. Returns (ExecResult{ExitCode: -1}, err)
// on invocation failure (binary missing, pipe setup error).
func (e *Executor) Run(ctx context.Context, prompt string, events chan<- loop.AgentEvent) (loop.ExecResult, error) {
	// -p, --output-format stream-json, and --verbose are required for
	// the loop to function (line-by-line JSONL events). They are not
	// configurable.
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
	}
	// --dangerously-skip-permissions is the precondition for autonomous
	// mode; without it claude prompts on every tool use. Users who set
	// skip_permissions=false in .bcc.toml accept that the loop will
	// stall on the first prompt.
	if e.cfg.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if e.cfg.Model != "" {
		args = append(args, "--model", e.cfg.Model)
	}
	args = append(args, e.cfg.ExtraArgs...)
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, e.cfg.Binary, args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return loop.ExecResult{ExitCode: -1}, fmt.Errorf("stdout pipe: %w", err)
	}
	if e.cfg.Stderr != nil {
		cmd.Stderr = e.cfg.Stderr
	}
	cmd.Cancel = func() error {
		// Graceful interrupt; exec.Cmd escalates to SIGKILL after WaitDelay.
		return cmd.Process.Signal(syscall.SIGINT)
	}
	cmd.WaitDelay = e.cfg.CancelGrace

	if err := cmd.Start(); err != nil {
		return loop.ExecResult{ExitCode: -1}, fmt.Errorf("run %s: %w", e.cfg.Binary, err)
	}

	// Drain the stdout pipe in a goroutine. The pipe EOFs naturally when
	// the subprocess exits; on cancel, the streamLines loop exits early
	// via ctx.Done so a slow consumer never blocks the cmd.Wait below.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		streamLines(ctx, pipe, events, e.cfg.MaxLineBytes)
	}()

	// Per Cmd.StdoutPipe doc: callers must finish reading before Wait.
	wg.Wait()
	runErr := cmd.Wait()

	if ctxErr := ctx.Err(); ctxErr != nil {
		exitCode := -1
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
		}
		return loop.ExecResult{ExitCode: exitCode}, ctxErr
	}

	if runErr == nil {
		return loop.ExecResult{ExitCode: 0}, nil
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		// Agent exited non-zero; that is a normal control-flow signal,
		// not a Run failure. Caller decides what to do.
		return loop.ExecResult{ExitCode: ee.ExitCode()}, nil
	}
	return loop.ExecResult{ExitCode: -1}, fmt.Errorf("run %s: %w", e.cfg.Binary, runErr)
}

// streamLines reads stream-json from r line by line, parses each line
// into zero or more AgentEvents, and forwards each event on the events
// channel. Returns when r EOFs or ctx is done.
func streamLines(ctx context.Context, r io.Reader, events chan<- loop.AgentEvent, maxLine int) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	for sc.Scan() {
		// Scanner.Bytes is reused across Scan calls; copy before forwarding.
		raw := append([]byte(nil), sc.Bytes()...)
		for _, ev := range parseLine(raw, time.Now()) {
			select {
			case events <- ev:
			case <-ctx.Done():
				return
			}
		}
	}
}

// parseLine turns one stream-json line into zero or more normalized
// AgentEvents. Unknown top-level types are silently dropped: the wire
// format evolves and unknown events do not block iteration.
//
// `at` is stamped onto every produced event; callers pass time.Now()
// when reading off the live pipe and a fixed time in tests.
func parseLine(raw []byte, at time.Time) []loop.AgentEvent {
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
	case "bcc_event":
		return parseBccEvent(raw, at)
	default:
		return nil
	}
}

// parseBccEvent recognizes the canonical bcc wire-protocol sentinel and
// forwards a normalized BccEvent on the loop's event channel. The wire
// protocol is format-agnostic, so the parsing lives in agentcontract;
// this function is just the executor-side hook.
func parseBccEvent(raw []byte, at time.Time) []loop.AgentEvent {
	bcc, ok := agentcontract.ParseLine(raw)
	if !ok {
		return nil
	}
	return []loop.AgentEvent{{
		Kind: loop.KindBccEvent,
		At:   at,
		Bcc:  &bcc,
	}}
}

func parseSystem(raw []byte, at time.Time) []loop.AgentEvent {
	var v struct {
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
		Model     string `json:"model"`
		CWD       string `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &v); err != nil || v.Subtype != "init" {
		return nil
	}
	return []loop.AgentEvent{{
		Kind: loop.KindInit,
		At:   at,
		Init: &loop.InitInfo{SessionID: v.SessionID, Model: v.Model, CWD: v.CWD},
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

func parseAssistant(raw []byte, at time.Time) []loop.AgentEvent {
	var v struct {
		Message struct {
			Content []assistantContent `json:"content"`
			Usage   struct {
				InputTokens              int64 `json:"input_tokens"`
				OutputTokens             int64 `json:"output_tokens"`
				CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	out := make([]loop.AgentEvent, 0, len(v.Message.Content))
	for _, c := range v.Message.Content {
		switch c.Type {
		case "text":
			if strings.TrimSpace(c.Text) == "" {
				continue
			}
			out = append(out, loop.AgentEvent{
				Kind: loop.KindAssistantText,
				At:   at,
				Text: c.Text,
			})
		case "thinking":
			// Empty `thinking` strings appear when the assistant omits
			// reasoning but keeps the encrypted signature; skip them.
			if strings.TrimSpace(c.Thinking) == "" {
				continue
			}
			out = append(out, loop.AgentEvent{
				Kind: loop.KindThinking,
				At:   at,
				Text: c.Thinking,
			})
		case "tool_use":
			out = append(out, loop.AgentEvent{
				Kind: loop.KindToolUse,
				At:   at,
				Tool: &loop.ToolCallInfo{ID: c.ID, Name: c.Name, Args: c.Input},
			})
		}
	}
	// Attach the per-message usage to the first KindAssistantText event.
	// The usage block covers the whole message; attaching it to the text
	// event (the natural carrier) lets the health panel accumulate tokens
	// incrementally without waiting for the terminal result event.
	// Messages that produce no text event (tool-only turns) contribute
	// tokens only at iteration end via KindResultSummary reconciliation.
	u := v.Message.Usage
	if u.InputTokens+u.OutputTokens+u.CacheReadInputTokens+u.CacheCreationInputTokens > 0 {
		for i := range out {
			if out[i].Kind == loop.KindAssistantText {
				out[i].Usage = &loop.UsageInfo{
					InputTokens:              u.InputTokens,
					OutputTokens:             u.OutputTokens,
					CacheReadInputTokens:     u.CacheReadInputTokens,
					CacheCreationInputTokens: u.CacheCreationInputTokens,
				}
				break
			}
		}
	}
	return out
}

func parseUser(raw []byte, at time.Time) []loop.AgentEvent {
	// user.message.content is either a plain string (the agent's own
	// follow-up text, which the adapter ignores) or an array carrying
	// tool_result blocks; only the array form contributes events.
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
	var out []loop.AgentEvent
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
		out = append(out, loop.AgentEvent{
			Kind: loop.KindToolResult,
			At:   at,
			Tool: &loop.ToolCallInfo{
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

func parseRateLimit(raw []byte, at time.Time) []loop.AgentEvent {
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
	return []loop.AgentEvent{{
		Kind: loop.KindRateLimit,
		At:   at,
		Rate: &loop.RateLimitInfo{Status: v.Info.Status, ResetAt: resetAt},
	}}
}

func parseResult(raw []byte, at time.Time) []loop.AgentEvent {
	var v struct {
		NumTurns     int     `json:"num_turns"`
		TotalCostUSD float64 `json:"total_cost_usd"`
		DurationMS   int64   `json:"duration_ms"`
		Usage        struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return []loop.AgentEvent{{
		Kind: loop.KindResultSummary,
		At:   at,
		Done: &loop.ResultSummaryInfo{
			NumTurns:                 v.NumTurns,
			TotalCostUSD:             v.TotalCostUSD,
			DurationMS:               v.DurationMS,
			InputTokens:              v.Usage.InputTokens,
			OutputTokens:             v.Usage.OutputTokens,
			CacheReadInputTokens:     v.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: v.Usage.CacheCreationInputTokens,
		},
	}}
}
