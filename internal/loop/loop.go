package loop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/fgmacedo/buchecha/internal/config"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// Loop orchestrates phase-by-phase iteration over a spec.
//
// Construct it with the wired adapters (Executor, Git, Briefing) and a
// loaded Config, then call Run. Run is single-call; do not reuse a
// Loop across multiple specs in the same process.
type Loop struct {
	// SpecPath is the absolute or cwd-relative path to the spec markdown.
	SpecPath string

	// Config is the loaded Config (with defaults applied).
	Config *config.Config

	// Ports.
	Executor Executor
	Git      GitProbe
	Briefing AgentBriefing

	// Extra is an optional extra-instructions block injected into prompts.
	Extra string

	// SingleShot, when true, runs single-shot mode: max iterations is
	// forced to 1 and the briefing renders single-shot framing.
	SingleShot bool

	// PauseGate, when non-nil, gates iterations beyond the first. The
	// loop receives one value from PauseGate before starting iteration
	// n+1. The TUI is the canonical sender: it pushes a token after each
	// iteration finishes (when not paused) and stops while the user has
	// paused the run. nil disables gating entirely (text/json modes).
	PauseGate <-chan struct{}

	// Logger receives milestone messages. Defaults to slog.Default().
	Logger *slog.Logger
}

// Run drives the loop, emitting Events on the provided channel.
//
// The events channel is owned by the loop for the duration of Run: the
// loop sends every IterationStarted, AgentEventReceived,
// IterationFinished, and a final LoopFinished, then closes the channel.
// Callers consume events to drive a renderer (TUI, slog, NDJSON) and
// observe the terminal state via LoopFinished.
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

	cfg := l.Config
	if cfg == nil {
		return l.terminate(events, "fatal", ExitInvalid), errors.New("loop: Config is nil")
	}
	if l.Executor == nil || l.Git == nil || l.Briefing == nil {
		return l.terminate(events, "fatal", ExitInvalid), errors.New("loop: Executor, Git, and Briefing are required")
	}

	maxIter := cfg.Loop.MaxIterations
	if l.SingleShot {
		maxIter = 1
	}
	if maxIter <= 0 {
		return l.terminate(events, "fatal", ExitInvalid), fmt.Errorf("loop: max_iterations must be > 0, got %d", maxIter)
	}

	mode := ModeLoop
	if l.SingleShot {
		mode = ModeSingleShot
	}

	startedAt := time.Now()
	logger.Info("loop start",
		"spec", l.SpecPath,
		"max_iterations", maxIter,
		"single_shot", l.SingleShot,
	)

	for iter := 1; iter <= maxIter; iter++ {
		if iter > 1 && l.PauseGate != nil {
			select {
			case <-l.PauseGate:
			case <-ctx.Done():
				return l.terminate(events, "user cancelled", ExitInvalid), ctx.Err()
			}
		}

		iterStart := time.Now()
		logger.Info("iter start", "iter", iter, "max", maxIter)

		prompt, err := l.Briefing.BuildPrompt(ctx, BriefingInput{
			SpecPath:  l.SpecPath,
			Iteration: iter,
			Mode:      mode,
			Extra:     l.Extra,
		})
		if err != nil {
			return l.terminate(events, "fatal", ExitInvalid), fmt.Errorf("build prompt iter %d: %w", iter, err)
		}

		headBefore, err := l.Git.HeadSHA(ctx)
		if err != nil {
			return l.terminate(events, "fatal", ExitInvalid), fmt.Errorf("git head before iter %d: %w", iter, err)
		}

		emit(events, IterationStarted{
			Index:       iter,
			MaxIter:     maxIter,
			BaselineSHA: headBefore,
			At:          iterStart,
		})

		// Set BCC_* env vars before invoking the executor. The subprocess
		// inherits them via exec.Cmd default env. The agent uses them to:
		//   - confirm it is running under bcc (BCC_RUNNING=1)
		//   - breadcrumb iteration / spec / branch in journal entries
		//   - self-check (refuse to proceed if expected vars are absent)
		// Each iteration overwrites; last-write-wins is fine because
		// these values are well-defined per iteration.
		os.Setenv("BCC_RUNNING", "1")
		os.Setenv("BCC_ITERATION", strconv.Itoa(iter))
		os.Setenv("BCC_MAX_ITERATIONS", strconv.Itoa(maxIter))
		os.Setenv("BCC_SPEC_PATH", l.SpecPath)
		if branch, gerr := l.Git.CurrentBranch(ctx); gerr == nil && branch != "" {
			os.Setenv("BCC_BRANCH", branch)
		}

		// Track the latest iteration_result the agent emits over the
		// wire protocol. Last-write-wins: if the agent (incorrectly)
		// emits multiple, the final one is the one bcc consumes.
		latestSignal := agentcontract.SignalUnknown

		agentEvents := make(chan AgentEvent, 256)
		pumpDone := make(chan struct{})
		go func() {
			defer close(pumpDone)
			for ae := range agentEvents {
				if ae.Kind == KindBccEvent && ae.Bcc != nil &&
					ae.Bcc.Kind == agentcontract.BccEventIterationResult {
					latestSignal = ae.Bcc.Signal
				}
				emit(events, AgentEventReceived{Event: ae})
			}
		}()

		execResult, execErr := l.Executor.Run(ctx, prompt, agentEvents)
		close(agentEvents)
		<-pumpDone

		logger.Info("iter executor returned",
			"iter", iter,
			"agent_exit", execResult.ExitCode,
			"err", execErrMsg(execErr),
		)
		if execErr != nil {
			return l.terminate(events, "fatal", ExitInvalid), execErr
		}

		headAfter, err := l.Git.HeadSHA(ctx)
		if err != nil {
			return l.terminate(events, "fatal", ExitInvalid), fmt.Errorf("git head after iter %d: %w", iter, err)
		}

		headAdvanced := headAfter != headBefore

		decision := Decide(DeciderInput{
			Signal:       latestSignal,
			HEADAdvanced: headAdvanced,
		})

		iterEnd := time.Now()
		logger.Info("iter decision",
			"iter", iter,
			"signal", latestSignal.String(),
			"head_advanced", headAdvanced,
			"action", decision.Action.String(),
			"exit_if_stop", decision.ExitCode,
			"elapsed", iterEnd.Sub(iterStart).String(),
		)

		emit(events, IterationFinished{
			Index:        iter,
			Signal:       latestSignal,
			HEADAdvanced: headAdvanced,
			DurationMS:   iterEnd.Sub(iterStart).Milliseconds(),
			At:           iterEnd,
		})

		if decision.Action == ActionStop {
			reason := stopReason(decision.ExitCode)
			logger.Info("loop stop",
				"reason", reason,
				"exit_code", decision.ExitCode,
				"total_elapsed", time.Since(startedAt).String(),
			)
			return l.terminate(events, reason, decision.ExitCode), nil
		}
	}

	logger.Warn("loop iteration cap reached",
		"max", maxIter,
		"total_elapsed", time.Since(startedAt).String(),
	)
	return l.terminate(events, "max_iterations", ExitMaxIterations), nil
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

func execErrMsg(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
