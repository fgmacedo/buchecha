package loop

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fgmacedo/buchecha/internal/config"
)

// Loop drives the Director plan/brief/execute/review pipeline.
//
// Construct it with the wired adapters (Git, Director ports) and a
// loaded Config, then call Run. Run is single-call; do not reuse a
// Loop across multiple specs in the same process.
type Loop struct {
	// SpecPath is the absolute or cwd-relative path to the spec markdown.
	SpecPath string

	// Config is the loaded Config (with defaults applied).
	Config *config.Config

	// Git is the read-only working-tree probe. Required.
	Git GitProbe

	// Director carries the dependencies the DAG-driven pipeline needs:
	// confirmed Plan, Briefer/Reviewer ports, per-session Store,
	// NewExecutor factory, run-wide MCP Handler, and the optional
	// Escalation channel. Required.
	Director *DirectorPorts

	// Logger receives milestone messages. Defaults to slog.Default().
	Logger *slog.Logger
}

// Run drives the Director loop, emitting Events on the provided channel.
//
// The events channel is owned by the loop for the duration of Run: the
// loop sends every IterationStarted, AgentEventReceived,
// IterationFinished, and a final LoopFinished, then closes the channel.
//
// Returns one of the bash-compatible exit codes. err is non-nil on
// invocation failures (binary missing, ctx cancellation, IO errors).
// When err is non-nil, the returned code is meaningful: callers
// translate it directly to os.Exit. err carries the diagnostic for
// stderr.
func (l *Loop) Run(ctx context.Context, events chan<- Event) (int, error) {
	defer func() {
		if events != nil {
			close(events)
		}
	}()

	logger := l.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if l.Config == nil {
		return l.terminate(events, "fatal", ExitInvalid), errors.New("loop: Config is nil")
	}
	if l.Director == nil {
		return l.terminate(events, "fatal", ExitInvalid), errors.New("loop: Director ports are required")
	}
	if l.Git == nil {
		return l.terminate(events, "fatal", ExitInvalid), errors.New("loop: Git is required")
	}
	return l.runDirector(ctx, events, logger)
}

// terminate emits a final LoopFinished event with the given reason and
// exit code, then returns the exit code so callers can `return
// l.terminate(...), err`.
func (l *Loop) terminate(events chan<- Event, reason string, code int) int {
	emit(events, LoopFinished{
		Reason:   reason,
		ExitCode: code,
		At:       time.Now(),
	})
	return code
}

// emit sends ev on events when events is non-nil. The Loop accepts a
// nil events channel for callers that do not want to consume events;
// every emit becomes a no-op in that case.
func emit(events chan<- Event, ev Event) {
	if events == nil {
		return
	}
	events <- ev
}

func stopReason(code int) string {
	switch code {
	case ExitDone:
		return "done"
	case ExitBlocked:
		return "blocked"
	case ExitInvalid:
		return "invalid"
	case ExitHEADStuck:
		return "head_stuck"
	case ExitMaxIterations:
		return "max_iterations"
	case ExitReview:
		return "review"
	default:
		return "unknown"
	}
}
