package loop

import (
	"fmt"
	"strings"
)

// Level is the event severity used by the verbosity filter.
//
// Lower numeric values denote higher priority. The verbosity flag is a
// low-water mark: at verbosity X, every event whose level rank is <= X
// is forwarded to the render backend.
type Level int

const (
	// LevelError is the most severe; always emitted regardless of verbosity.
	LevelError Level = iota + 1
	LevelWarn
	LevelInfo
	LevelDebug
	LevelTrace
)

// String returns the canonical lowercase name of l.
func (l Level) String() string {
	switch l {
	case LevelError:
		return "error"
	case LevelWarn:
		return "warn"
	case LevelInfo:
		return "info"
	case LevelDebug:
		return "debug"
	case LevelTrace:
		return "trace"
	default:
		return "unknown"
	}
}

// ParseLevel maps a CLI string to a Level. Accepts the canonical names
// in any case; "warning" is accepted as an alias of "warn".
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error":
		return LevelError, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "info":
		return LevelInfo, nil
	case "debug":
		return LevelDebug, nil
	case "trace":
		return LevelTrace, nil
	default:
		return 0, fmt.Errorf("unknown verbosity level %q (want error|warn|info|debug|trace)", s)
	}
}

// LevelOf returns the implicit Level of ev per the verbosity table in the
// Phase 2 spec. The mapping is total: every concrete Event variant has a
// Level. Unknown variants default to LevelInfo.
func LevelOf(ev Event) Level {
	switch e := ev.(type) {
	case IterationStarted:
		return LevelInfo
	case IterationFinished:
		return LevelInfo
	case LoopFinished:
		if e.ExitCode != 0 {
			return LevelError
		}
		return LevelInfo
	case AgentEventReceived:
		return levelOfAgentEvent(e.Event)
	default:
		return LevelInfo
	}
}

func levelOfAgentEvent(ae AgentEvent) Level {
	switch ae.Kind {
	case KindInit:
		return LevelDebug
	case KindThinking:
		return LevelTrace
	case KindToolUse:
		return LevelInfo
	case KindToolResult:
		if ae.Tool != nil && ae.Tool.IsError {
			return LevelError
		}
		return LevelDebug
	case KindAssistantText:
		return LevelDebug
	case KindRateLimit:
		if ae.Rate != nil && ae.Rate.Status != "" && ae.Rate.Status != "allowed" {
			return LevelWarn
		}
		return LevelDebug
	case KindResultSummary:
		return LevelInfo
	case KindBccEvent:
		return LevelInfo
	default:
		return LevelInfo
	}
}

// FilterEvents launches a goroutine that reads from in and forwards
// only events whose level rank is <= max to out, closing out when in
// is closed. The filter is a backpressure-respecting middleware between
// the loop's events channel and a render backend.
func FilterEvents(in <-chan Event, out chan<- Event, max Level) {
	go func() {
		defer close(out)
		for ev := range in {
			if LevelOf(ev) <= max {
				out <- ev
			}
		}
	}()
}
