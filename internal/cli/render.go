package cli

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/fgmacedo/buchecha/internal/loop"
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
			slog.String("result", e.Result.String()),
			slog.Bool("head_advanced", e.HEADAdvanced),
			slog.Int("newly_checked", e.NewlyChecked),
			slog.Int64("duration_ms", e.DurationMS),
			slog.String("log_path", e.LogPath),
		}
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

func textRenderAgentEvent(ae loop.AgentEvent) (string, []slog.Attr) {
	attrs := []slog.Attr{slog.String("kind", string(ae.Kind))}
	switch ae.Kind {
	case loop.KindInit:
		if ae.Init != nil {
			attrs = append(attrs,
				slog.String("session_id", ae.Init.SessionID),
				slog.String("model", ae.Init.Model),
			)
		}
	case loop.KindThinking, loop.KindAssistantText:
		attrs = append(attrs, slog.String("text", ae.Text))
	case loop.KindToolUse:
		if ae.Tool != nil {
			attrs = append(attrs,
				slog.String("tool_id", ae.Tool.ID),
				slog.String("tool", ae.Tool.Name),
			)
		}
	case loop.KindToolResult:
		if ae.Tool != nil {
			attrs = append(attrs,
				slog.String("tool_id", ae.Tool.ID),
				slog.Bool("is_error", ae.Tool.IsError),
				slog.String("summary", ae.Tool.Summary),
			)
		}
	case loop.KindRateLimit:
		if ae.Rate != nil {
			attrs = append(attrs, slog.String("status", ae.Rate.Status))
			if !ae.Rate.ResetAt.IsZero() {
				attrs = append(attrs, slog.Time("reset_at", ae.Rate.ResetAt))
			}
		}
	case loop.KindResultSummary:
		if ae.Done != nil {
			attrs = append(attrs,
				slog.Int("num_turns", ae.Done.NumTurns),
				slog.Float64("total_cost_usd", ae.Done.TotalCostUSD),
				slog.Int64("input_tokens", ae.Done.InputTokens),
				slog.Int64("output_tokens", ae.Done.OutputTokens),
				slog.Int64("duration_ms", ae.Done.DurationMS),
			)
		}
	}
	return "agent event", attrs
}
