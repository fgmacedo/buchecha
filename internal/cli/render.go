package cli

import (
	"bufio"
	"fmt"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/supervision"
)

// Output mode names accepted by --output.
const (
	OutputTUI  = "tui"
	OutputText = "text"
	OutputJSON = "json"
)

// dispatchEvents wires the loop events channel to the render backend
// chosen by --output, applying the verbosity filter as a middleware. It
// returns a writeable channel for the loop and a done channel that
// closes once the backend has drained.
//
// The caller MUST close the returned events channel when the loop has
// returned (the loop itself does so today). The done channel signals
// when the backend goroutine has finished consuming and rendering.
func dispatchEvents(mode string, level loop.Level) (chan loop.Event, <-chan struct{}, error) {
	raw := make(chan loop.Event, 256)
	filtered := make(chan loop.Event, 256)
	loop.FilterEvents(raw, filtered, level)

	done := make(chan struct{})
	switch mode {
	case OutputTUI:
		// P2.4 replaces this drain with the bubbletea program. For now,
		// the no-op drain keeps the loop's existing slog diagnostics on
		// stderr as the user-visible output.
		go drainNoop(filtered, done)
	case OutputText:
		go drainText(filtered, done, slog.Default())
	case OutputJSON:
		go drainJSON(filtered, done, os.Stdout)
	default:
		close(raw)
		close(done)
		return nil, nil, fmt.Errorf("unknown --output %q (want tui|text|json)", mode)
	}
	return raw, done, nil
}

func drainNoop(in <-chan loop.Event, done chan<- struct{}) {
	defer close(done)
	for range in {
	}
}

// drainText emits one slog line per event, at the slog level matching
// the event's loop.Level. The loop's existing diagnostic slog calls
// continue to flow on the same logger; both interleave on stderr.
func drainText(in <-chan loop.Event, done chan<- struct{}, logger *slog.Logger) {
	defer close(done)
	for ev := range in {
		level := slogLevelOf(loop.LevelOf(ev))
		msg, attrs := textRenderEvent(ev)
		logger.LogAttrs(nil, level, msg, attrs...)
	}
}

// drainJSON writes one NDJSON line per event to w, flushing between
// lines so live consumers see them promptly.
func drainJSON(in <-chan loop.Event, done chan<- struct{}, w io.Writer) {
	defer close(done)
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	for ev := range in {
		line, err := loop.MarshalJSONEvent(ev)
		if err != nil {
			slog.Error("ndjson marshal failed", "err", err.Error())
			continue
		}
		bw.Write(line)
		bw.WriteByte('\n')
		bw.Flush()
	}
}

// RenderPlan prints a Director Plan in plain text suitable for the
// confirmation prompt in non-TUI modes. The TUI variant lands in P8.
//
// Output is intentionally simple: a header, the goal, success criteria,
// and a numbered phase table (id, title, retry_budget, depends_on). It
// targets a wide terminal and never colorizes; styling is decided by the
// renderer that wraps this in TUI/text modes.
func RenderPlan(p *supervision.Plan, w io.Writer) {
	if p == nil {
		fmt.Fprintln(w, "bcc: director plan: <nil>")
		return
	}
	fmt.Fprintln(w, "bcc: Director plan")
	fmt.Fprintf(w, "  spec_hash: %s\n", shortHash(p.SpecHash))
	fmt.Fprintf(w, "  goal:      %s\n", p.Goal)
	if len(p.SuccessCriteria) > 0 {
		fmt.Fprintln(w, "  success_criteria:")
		for _, c := range p.SuccessCriteria {
			fmt.Fprintf(w, "    - %s\n", c)
		}
	}
	fmt.Fprintf(w, "  phases (%d):\n", len(p.Phases))
	for i, ph := range p.Phases {
		deps := "-"
		if len(ph.DependsOn) > 0 {
			deps = strings.Join(ph.DependsOn, ",")
		}
		fmt.Fprintf(w, "    %2d. %s  %s  tasks=%d  deps=%s\n",
			i+1, ph.ID, ph.Title, len(ph.Tasks), deps)
	}
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func slogLevelOf(l loop.Level) slog.Level {
	switch l {
	case loop.LevelError:
		return slog.LevelError
	case loop.LevelWarn:
		return slog.LevelWarn
	case loop.LevelInfo:
		return slog.LevelInfo
	case loop.LevelDebug, loop.LevelTrace:
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

func textRenderEvent(ev loop.Event) (string, []slog.Attr) {
	switch e := ev.(type) {
	case loop.IterationStarted:
		return "iter started", []slog.Attr{
			slog.Int("index", e.Index),
			slog.Int("max_iter", e.MaxIter),
		}
	case loop.IterationFinished:
		return "iter finished", []slog.Attr{
			slog.Int("index", e.Index),
			slog.String("signal", e.Signal.String()),
			slog.Bool("head_advanced", e.HEADAdvanced),
			slog.Int64("duration_ms", e.DurationMS),
		}
	case loop.PhaseBriefed:
		attrs := []slog.Attr{
			slog.String("phase", e.PhaseID),
			slog.Int("iteration", e.Iteration),
		}
		attrs = append(attrs, roleSpawnAttrs("briefer", e.BrieferModel, e.BrieferEffort, e.BrieferSkipped)...)
		attrs = append(attrs, roleSpawnAttrs("executor", e.ExecutorModel, e.ExecutorEffort, false)...)
		attrs = append(attrs, roleSpawnAttrs("reviewer", e.ReviewerModel, e.ReviewerEffort, e.ReviewSkipped)...)
		return "phase briefed", attrs
	case loop.LoopFinished:
		return "loop finished", []slog.Attr{
			slog.String("reason", e.Reason),
			slog.Int("exit_code", e.ExitCode),
		}
	case loop.AgentEventReceived:
		return textRenderAgentEvent(e.Event)
	default:
		return "event", nil
	}
}

// roleSpawnAttrs renders a role's resolved spawn parameters as slog
// attrs. When skipped is true the role attribute reads "skip"; when
// model and effort are both empty (no override and no default) the
// role contributes no attrs at all so the line stays terse.
func roleSpawnAttrs(role, model, effort string, skipped bool) []slog.Attr {
	if skipped {
		return []slog.Attr{slog.String(role, "skip")}
	}
	if model == "" && effort == "" {
		return nil
	}
	value := model
	if effort != "" {
		if value == "" {
			value = "(default)"
		}
		value += "/" + effort
	}
	return []slog.Attr{slog.String(role, value)}
}

func textRenderAgentEvent(ae agentcontract.AgentEvent) (string, []slog.Attr) {
	attrs := []slog.Attr{slog.String("kind", string(ae.Kind))}
	switch ae.Kind {
	case agentcontract.KindInit:
		if ae.Init != nil {
			attrs = append(attrs,
				slog.String("session_id", ae.Init.SessionID),
				slog.String("model", ae.Init.Model),
			)
		}
	case agentcontract.KindThinking, agentcontract.KindAssistantText:
		attrs = append(attrs, slog.String("text", ae.Text))
	case agentcontract.KindToolUse:
		if ae.Tool != nil {
			attrs = append(attrs,
				slog.String("tool_id", ae.Tool.ID),
				slog.String("tool", ae.Tool.Name),
			)
		}
	case agentcontract.KindToolResult:
		if ae.Tool != nil {
			attrs = append(attrs,
				slog.String("tool_id", ae.Tool.ID),
				slog.Bool("is_error", ae.Tool.IsError),
				slog.String("summary", ae.Tool.Summary),
			)
		}
	case agentcontract.KindRateLimit:
		if ae.Rate != nil {
			attrs = append(attrs, slog.String("status", ae.Rate.Status))
			if !ae.Rate.ResetAt.IsZero() {
				attrs = append(attrs, slog.Time("reset_at", ae.Rate.ResetAt))
			}
		}
	case agentcontract.KindResultSummary:
		if ae.Done != nil {
			attrs = append(attrs,
				slog.Int("num_turns", ae.Done.NumTurns),
				slog.Float64("total_cost_usd", ae.Done.TotalCostUSD),
				slog.Int64("input_tokens", ae.Done.Tokens.InputFresh),
				slog.Int64("output_tokens", ae.Done.Tokens.Output),
				slog.Int64("cache_read_tokens", ae.Done.Tokens.InputCached),
				slog.Int64("cache_write_tokens", ae.Done.Tokens.CacheWrite),
				slog.Int64("duration_ms", ae.Done.DurationMS),
			)
		}
	}
	return "agent event", attrs
}
