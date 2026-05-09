package loop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/supervision"
	"github.com/fgmacedo/buchecha/internal/supervision/dag"
	"github.com/fgmacedo/buchecha/internal/supervision/menu"
	"github.com/fgmacedo/buchecha/internal/supervision/render"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
	"github.com/fgmacedo/buchecha/internal/supervision/stats"
)

// runDirector drives the DAG-driven Director state machine:
//
//	for !state.HasPending():
//	  brief                                    (Briefer emits via MCP)
//	  for attempt = 1..1+budget:
//	    execute  (Executor mutates DAG via MCP)
//	    review   (Reviewer mutates DAG + reports outcome via MCP)
//	    decide   advance / retry / escalate / abort
//
// The loop reads briefings, per-task statuses, and review outcomes
// from the run-wide handler; it never re-parses the spec. Per-task
// transitions reach the events channel via MCP audit translation:
// every successful TaskStarted/Completed/Approved/NeedsFix call lands
// as the corresponding loop event so the TUI surfaces them on the
// timeline alongside the older phase-level summaries.
func (l *Loop) runDirector(ctx context.Context, events chan<- Event, logger *slog.Logger) (code int, runErr error) {
	d := l.Director
	if d.Plan == nil || d.Briefer == nil || d.Reviewer == nil ||
		d.Store == nil || d.NewExecutor == nil || d.Handler == nil {
		return l.terminate(events, "fatal", ExitInvalid),
			errors.New("loop: Director requires Plan, Briefer, Reviewer, Store, NewExecutor, and Handler")
	}
	if err := supervision.ValidatePlan(d.Plan); err != nil {
		return l.terminate(events, "fatal", ExitInvalid), fmt.Errorf("loop: invalid director plan: %w", err)
	}
	if err := menu.ValidatePlanAgainstMenus(d.Plan, d.Handler.RoleMenus()); err != nil {
		return l.terminate(events, "fatal", ExitInvalid), fmt.Errorf("loop: invalid director plan routing: %w", err)
	}
	state := d.Handler.State()
	if state == nil {
		state = dag.NewStateFromPlan(d.Plan)
		d.Handler.SetState(state)
		d.Handler.SetPlan(d.Plan)
	}
	bridge := &taskEventBridge{events: events, reg: d.Handler.Registry()}
	d.Handler.AttachObserver(bridge)
	defer d.Handler.AttachObserver(nil)
	defer func() {
		if d.Store == nil || d.Store.Session() == nil {
			return
		}
		status := session.SessionAborted
		if code == ExitDone {
			status = session.SessionDone
		}
		_ = d.Store.Touch(status, time.Now())
	}()

	startedAt := time.Now()
	logger.Info("director loop start",
		"spec", l.SpecPath,
		"phases", len(d.Plan.Phases),
	)
	emit(events, PhasePlanned{Plan: d.Plan, At: startedAt})
	emit(events, TaskStarted{TaskID: dag.PlanningTaskID, At: startedAt})
	emit(events, TaskCompleted{TaskID: dag.PlanningTaskID, At: startedAt})

	registry := d.Handler.Registry()
	globalIter := 0
	priorFeedback := ""
	pendingHint := ""
	phaseIterations := map[string]int{}
	capturedBaselines := map[string]string{}

	for state.HasPending() {
		eligible := state.EligiblePhases()
		if len(eligible) == 0 {
			return l.terminate(events, "invalid", ExitInvalid),
				errors.New("director: pending tasks remain but no phase is eligible (cycle?)")
		}
		phaseID := eligible[0]
		phase := d.Plan.PhaseByID(phaseID)
		if phase == nil {
			return l.terminate(events, "fatal", ExitInvalid),
				fmt.Errorf("director: eligible phase %q not in plan", phaseID)
		}
		subDAG := state.PendingTasks(phaseID)
		if len(subDAG) == 0 {
			return l.terminate(events, "fatal", ExitInvalid),
				fmt.Errorf("director: phase %q is eligible but has no pending tasks", phaseID)
		}
		budget := effectiveRetryBudget(phase, subDAG, l.Config.Loop.RetryBudget)

		phaseIterations[phaseID]++
		iteration := phaseIterations[phaseID]
		iterationID := fmt.Sprintf("%s-%02d", phaseID, iteration)
		var briefing *supervision.Briefing
		if phase.PreparedBriefing != nil {
			synthetic := supervision.Briefing{
				IterationID:   iterationID,
				PhaseID:       phaseID,
				SubDAGTaskIDs: append([]string(nil), phase.PreparedBriefing.SubDAGTaskIDs...),
				Instructions:  phase.PreparedBriefing.Instructions,
				SpecPath:      l.SpecPath,
			}
			if priorFeedback != "" {
				pf := priorFeedback
				synthetic.PriorFeedback = &pf
			}
			if err := d.Handler.RecordSyntheticBriefing(synthetic); err != nil {
				return l.terminate(events, "fatal", ExitInvalid),
					fmt.Errorf("director: record synthetic briefing for phase %s: %w", phaseID, err)
			}
			briefing = d.Handler.Briefing(iterationID)
		} else {
			brieferID, err := registry.Register(dag.RoleBriefer, dag.RegisterArgs{PhaseID: phaseID})
			if err != nil {
				return l.terminate(events, "fatal", ExitInvalid),
					fmt.Errorf("director: register briefer: %w", err)
			}
			briefIn, err := supervision.BriefingFor(d.Plan, l.SpecPath, phaseID, iteration, subDAG, priorFeedback)
			if err != nil {
				registry.Deregister(brieferID)
				return l.terminate(events, "fatal", ExitInvalid),
					fmt.Errorf("director: briefing input phase %s: %w", phaseID, err)
			}
			briefIn.AgentID = string(brieferID)
			briefIn.Assignment = phase.AssignmentFor("briefer")
			briefIn.Attempt = iteration
			var brieferStats *supervision.SpawnStats
			brierr := runWithAgentEvents(ctx, events, func(agentEvents chan<- agentcontract.AgentEvent) error {
				stats, e := d.Briefer.Brief(ctx, *briefIn, agentEvents)
				brieferStats = stats
				return e
			})
			registry.Deregister(brieferID)
			if brieferStats != nil {
				if err := d.Stats.Append(stats.StatsEntry{
					At:           time.Now(),
					Role:         string(dag.RoleBriefer),
					PhaseID:      phaseID,
					IterationID:  iterationID,
					DurationMS:   brieferStats.DurationMS,
					CostUSD:      brieferStats.CostUSD,
					InputTokens:  brieferStats.InputTokens,
					OutputTokens: brieferStats.OutputTokens,
				}); err != nil {
					logger.Warn("director stats append briefer", "err", err)
				}
			}
			if brierr != nil {
				return l.terminate(events, "fatal", ExitInvalid),
					fmt.Errorf("director: brief phase %s: %w", phaseID, brierr)
			}
			briefing = d.Handler.Briefing(briefIn.IterationID)
		}
		if briefing == nil {
			return l.terminate(events, "fatal", ExitInvalid),
				fmt.Errorf("director: briefer did not emit briefing %q", iterationID)
		}
		if briefing.PhaseID != phaseID {
			return l.terminate(events, "fatal", ExitInvalid),
				fmt.Errorf("director: briefer emitted phase %q, expected %q", briefing.PhaseID, phaseID)
		}
		actualSub := briefing.SubDAGTaskIDs
		if len(actualSub) == 0 {
			actualSub = subDAG
		}
		priorFeedback = ""

		hintForIteration := pendingHint
		pendingHint = ""
		userPrompt, err := render.RenderBriefingUser(briefing, phase, hintForIteration)
		if err != nil {
			return l.terminate(events, "fatal", ExitInvalid),
				fmt.Errorf("director: render briefing user prompt: %w", err)
		}
		briefingsDir := filepath.Join(d.Store.SessionDir(), "briefings")
		if err := os.MkdirAll(briefingsDir, 0o755); err != nil {
			return l.terminate(events, "fatal", ExitInvalid),
				fmt.Errorf("director: mkdir briefings: %w", err)
		}
		userPromptPath := filepath.Join(briefingsDir,
			fmt.Sprintf("%s.prompt.md", briefing.IterationID))
		if err := os.WriteFile(userPromptPath, []byte(userPrompt), 0o644); err != nil {
			return l.terminate(events, "fatal", ExitInvalid),
				fmt.Errorf("director: write briefing user prompt: %w", err)
		}
		systemPromptPath := filepath.Join(briefingsDir,
			fmt.Sprintf("%s.system.md", briefing.IterationID))
		// renderSystem is invoked by the NewExecutor factory once the
		// Executor's per-spawn agent_id is known. The factory passes the
		// freshly registered agent_id; we render the system prompt with
		// the matching Identity block and persist it. Each attempt
		// rewrites the same path because each attempt registers a fresh
		// agent_id; only the latest rendered file is consumed.
		renderSystem := func(agentID string) (string, error) {
			systemPrompt, err := render.RenderBriefingSystem(agentID)
			if err != nil {
				return "", fmt.Errorf("director: render briefing system prompt: %w", err)
			}
			if err := os.WriteFile(systemPromptPath, []byte(systemPrompt), 0o644); err != nil {
				return "", fmt.Errorf("director: write briefing system prompt: %w", err)
			}
			return systemPromptPath, nil
		}

		brieferModel, brieferEffort := resolveAssignment(phase.AssignmentFor("briefer"))
		executorModel, executorEffort := resolveAssignment(phase.AssignmentFor("executor"))
		reviewerModel, reviewerEffort := resolveAssignment(phase.AssignmentFor("reviewer"))
		brieferSkipped := phase.PreparedBriefing != nil
		reviewSkipped := phase.SkipReview
		emit(events, PhaseBriefed{
			PhaseID: phaseID, Iteration: iteration,
			Briefing:       briefing,
			BrieferModel:   brieferModel,
			BrieferEffort:  brieferEffort,
			ExecutorModel:  executorModel,
			ExecutorEffort: executorEffort,
			ReviewerModel:  reviewerModel,
			ReviewerEffort: reviewerEffort,
			BrieferSkipped: brieferSkipped,
			ReviewSkipped:  reviewSkipped,
			At:             time.Now(),
		})

		// Capture phase baseline once before any attempt.
		if _, seen := capturedBaselines[phaseID]; !seen {
			sha, err := l.Git.HeadSHA(ctx)
			if err != nil {
				return l.terminate(events, "fatal", ExitInvalid),
					fmt.Errorf("director: git head capturing baseline for phase %s: %w", phaseID, err)
			}
			capturedBaselines[phaseID] = sha
			d.Handler.SetPhaseBaseline(phaseID, sha)
		}

		iterationDone := false
		for attempt := 1; !iterationDone; attempt++ {
			globalIter++
			if l.Config.Loop.MaxIterations > 0 && globalIter > l.Config.Loop.MaxIterations {
				logger.Warn("director iteration cap reached", "iter", globalIter)
				return l.terminate(events, "max_iterations", ExitMaxIterations), nil
			}

			headBefore, err := l.Git.HeadSHA(ctx)
			if err != nil {
				return l.terminate(events, "fatal", ExitInvalid),
					fmt.Errorf("director: git head before phase %s attempt %d: %w", phaseID, attempt, err)
			}

			os.Setenv("BCC_RUNNING", "1")
			os.Setenv("BCC_ITERATION", strconv.Itoa(attempt))
			os.Setenv("BCC_MAX_ITERATIONS", strconv.Itoa(1+budget))
			os.Setenv("BCC_SPEC_PATH", l.SpecPath)
			if branch, gerr := l.Git.CurrentBranch(ctx); gerr == nil && branch != "" {
				os.Setenv("BCC_BRANCH", branch)
			}

			iterStart := time.Now()
			if l.Director.Store != nil {
				if err := l.Director.Store.SetIteration(globalIter, l.Config.Loop.MaxIterations, iterStart); err != nil {
					logger.Warn("director set iteration", "err", err)
				}
			}
			emit(events, IterationStarted{
				Index: globalIter, MaxIter: l.Config.Loop.MaxIterations,
				BaselineSHA: headBefore, At: iterStart,
			})

			execArgs := dag.RegisterArgs{
				BriefingID: briefing.IterationID,
				PhaseID:    phaseID,
				SubDAG:     actualSub,
				Attempt:    attempt,
			}
			signal, execStats, execErr := runDirectorExecutor(ctx, d.NewExecutor(execArgs, renderSystem, phase.AssignmentFor("executor")), userPrompt, events, d.Handler, briefing.IterationID)
			if execErr != nil {
				return l.terminate(events, "fatal", ExitInvalid), execErr
			}
			if execStats != nil {
				if err := d.Stats.Append(stats.StatsEntry{
					At:           time.Now(),
					Role:         string(dag.RoleExecutor),
					PhaseID:      phaseID,
					IterationID:  briefing.IterationID,
					Attempt:      attempt,
					DurationMS:   execStats.DurationMS,
					CostUSD:      execStats.CostUSD,
					InputTokens:  execStats.InputTokens,
					OutputTokens: execStats.OutputTokens,
				}); err != nil {
					logger.Warn("director stats append executor", "err", err)
				}
			}

			headAfter, err := l.Git.HeadSHA(ctx)
			if err != nil {
				return l.terminate(events, "fatal", ExitInvalid),
					fmt.Errorf("director: git head after phase %s attempt %d: %w", phaseID, attempt, err)
			}
			headAdvanced := headAfter != headBefore

			iterEnd := time.Now()
			emit(events, IterationFinished{
				Index: globalIter, Signal: signal,
				HEADAdvanced: headAdvanced,
				DurationMS:   iterEnd.Sub(iterStart).Milliseconds(),
				At:           iterEnd,
			})
			logger.Info("director iter finished",
				"phase", phaseID, "attempt", attempt,
				"signal", signal.String(), "head_advanced", headAdvanced,
			)

			if signal == agentcontract.SignalBlocked {
				return l.terminate(events, "blocked", ExitBlocked), nil
			}

			if phase.SkipReview {
				if err := d.Handler.RecordSyntheticApproval(briefing.IterationID); err != nil {
					return l.terminate(events, "fatal", ExitInvalid),
						fmt.Errorf("director: record synthetic approval phase %s attempt %d: %w", phaseID, attempt, err)
				}
			} else {
				d.Handler.ResetReviewOutcome(briefing.IterationID)
				reviewerID, err := registry.Register(dag.RoleReviewer, dag.RegisterArgs{
					BriefingID: briefing.IterationID,
					PhaseID:    phaseID,
					SubDAG:     actualSub,
				})
				if err != nil {
					return l.terminate(events, "fatal", ExitInvalid),
						fmt.Errorf("director: register reviewer: %w", err)
				}
				var reviewerStats *supervision.SpawnStats
				rerr := runWithAgentEvents(ctx, events, func(agentEvents chan<- agentcontract.AgentEvent) error {
					stats, e := d.Reviewer.Review(ctx, supervision.ReviewerInput{
						AgentID:     string(reviewerID),
						IterationID: briefing.IterationID,
						PhaseID:     phaseID,
						SubDAG:      actualSub,
						Assignment:  phase.AssignmentFor("reviewer"),
						Attempt:     attempt,
					}, agentEvents)
					reviewerStats = stats
					return e
				})
				registry.Deregister(reviewerID)
				if rerr != nil {
					return l.terminate(events, "fatal", ExitInvalid),
						fmt.Errorf("director: review phase %s attempt %d: %w", phaseID, attempt, rerr)
				}
				if reviewerStats != nil {
					if err := d.Stats.Append(stats.StatsEntry{
						At:           time.Now(),
						Role:         string(dag.RoleReviewer),
						PhaseID:      phaseID,
						IterationID:  briefing.IterationID,
						Attempt:      attempt,
						DurationMS:   reviewerStats.DurationMS,
						CostUSD:      reviewerStats.CostUSD,
						InputTokens:  reviewerStats.InputTokens,
						OutputTokens: reviewerStats.OutputTokens,
					}); err != nil {
						logger.Warn("director stats append reviewer", "err", err)
					}
				}
			}
			outcome := d.Handler.LastReviewOutcome(briefing.IterationID)
			reasoning := d.Handler.LastReviewReasoning(briefing.IterationID)

			emit(events, PhaseReviewed{
				PhaseID: phaseID, Attempt: attempt,
				Outcome: outcome, Reasoning: reasoning,
				At: time.Now(),
			})

			fullyDone := state.SubDAGFullyDone(phaseID, actualSub)
			anyNeedsFix := state.SubDAGAnyNeedsFix(phaseID, actualSub)

			decision := DirectorDecide(DirectorDeciderInput{
				Outcome:           ReviewOutcome(outcome),
				SubDAGFullyDone:   fullyDone,
				SubDAGAnyNeedsFix: anyNeedsFix,
				Attempt:           attempt,
				RetryBudget:       budget,
			})
			logger.Info("director decision",
				"phase", phaseID, "attempt", attempt,
				"action", decision.Action.String(),
			)

			switch decision.Action {
			case DirectorAdvance:
				iterationDone = true
			case DirectorRetry:
				priorFeedback = reasoning
				continue
			case DirectorEscalate:
				if d.Store != nil && d.Store.Session() != nil {
					_ = d.Store.Touch(session.SessionEscalatedPending, time.Now())
				}
				emit(events, DirectorEscalation{
					PhaseID: phaseID, Attempt: attempt,
					Reasoning: reasoning, At: time.Now(),
				})
				reply, err := awaitEscalation(ctx, d.Escalation)
				if err != nil {
					return l.terminate(events, "user cancelled", ExitInvalid), err
				}
				switch reply.Kind {
				case EscalationResume:
					if d.Store != nil && d.Store.Session() != nil {
						_ = d.Store.Touch(session.SessionRunning, time.Now())
					}
					priorFeedback = reasoning
					pendingHint = reply.Hint
					iterationDone = true
				case EscalationForceApprove:
					if d.Store != nil && d.Store.Session() != nil {
						_ = d.Store.Touch(session.SessionRunning, time.Now())
					}
					if err := d.Handler.ForceApprovePending(briefing.IterationID, reply.Hint); err != nil {
						return l.terminate(events, "fatal", ExitInvalid),
							fmt.Errorf("director: force-approve phase %s: %w", phaseID, err)
					}
					iterationDone = true
				case EscalationSkip:
					iterationDone = true
				case EscalationAbort:
					return l.terminate(events, "aborted", ExitInvalid), nil
				default:
					return l.terminate(events, "invalid", ExitInvalid),
						fmt.Errorf("director: unknown escalation reply %d", reply.Kind)
				}
			case DirectorAbort:
				return l.terminate(events, stopReason(decision.ExitCode), decision.ExitCode), nil
			default:
				return l.terminate(events, "invalid", ExitInvalid),
					fmt.Errorf("director: unknown decider action %s", decision.Action.String())
			}
		}
	}

	logger.Info("director run done",
		"total_elapsed", time.Since(startedAt).String(),
	)
	return l.terminate(events, "done", ExitDone), nil
}

// runWithAgentEvents creates a per-call agentEvents channel, pumps it
// onto the loop events channel as AgentEventReceived, and invokes fn
// with the channel. Returns when fn returns and the pump drains. The
// helper exists so Briefer / Reviewer / standalone calls all surface
// agent telemetry to the TUI without each call site repeating the
// goroutine + channel-close dance.
func runWithAgentEvents(ctx context.Context, events chan<- Event, fn func(chan<- agentcontract.AgentEvent) error) error {
	agentEvents := make(chan agentcontract.AgentEvent, 256)
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		for ae := range agentEvents {
			emit(events, AgentEventReceived{Event: ae})
		}
	}()
	err := fn(agentEvents)
	close(agentEvents)
	<-pumpDone
	_ = ctx
	return err
}

// runDirectorExecutor invokes one Executor.Run for an iteration attempt
// and reads the terminal signal from the run-wide MCP handler after
// the subprocess exits. Agent events are forwarded onto the loop
// events channel for the TUI; the canonical signal source is the
// handler-stored value populated by the executor's
// iteration_finished call. An executor that exits without calling
// the terminal method falls back to SignalReview, the safe default
// (the Reviewer audits regardless and decides advance/retry). The
// returned stats pointer carries the executor's last result summary
// (cost, tokens, duration) when the agent emitted one, nil otherwise.
func runDirectorExecutor(ctx context.Context, exec Executor, userPrompt string, events chan<- Event, handler *dag.Handler, briefingID string) (agentcontract.Signal, *supervision.SpawnStats, error) {
	if exec == nil {
		return agentcontract.SignalUnknown, nil, errors.New("director: NewExecutor returned nil executor")
	}
	agentEvents := make(chan agentcontract.AgentEvent, 256)
	pumpDone := make(chan struct{})
	var lastSummary *agentcontract.ResultSummaryInfo
	var summaryAt time.Time
	go func() {
		defer close(pumpDone)
		for ae := range agentEvents {
			if ae.Kind == agentcontract.KindResultSummary && ae.Done != nil {
				lastSummary = ae.Done
				summaryAt = ae.At
			}
			emit(events, AgentEventReceived{Event: ae})
		}
	}()
	result, err := exec.Run(ctx, userPrompt, agentEvents)
	close(agentEvents)
	<-pumpDone
	var stats *supervision.SpawnStats
	if lastSummary != nil {
		var dur int64
		if !summaryAt.IsZero() {
			dur = lastSummary.DurationMS
		}
		stats = &supervision.SpawnStats{
			DurationMS:   dur,
			CostUSD:      lastSummary.TotalCostUSD,
			InputTokens:  lastSummary.InputTokens,
			OutputTokens: lastSummary.OutputTokens,
		}
	}
	if err != nil {
		return agentcontract.SignalUnknown, stats, fmt.Errorf("director: executor run: %w", err)
	}
	if result.ExitCode != 0 && handler != nil && handler.IterationSignal(briefingID) == "" {
		// Executor crashed without emitting iteration_finished. Surface
		// the captured stderr tail so the dashboard shows a real diagnostic
		// instead of a generic blocked state.
		return agentcontract.SignalBlocked, stats, formatExecutorCrash(result, briefingID)
	}
	signal := agentcontract.SignalUnknown
	if handler != nil {
		signal = parseSignalString(handler.IterationSignal(briefingID))
	}
	if signal == agentcontract.SignalUnknown {
		signal = agentcontract.SignalReview
	}
	return signal, stats, nil
}

// formatExecutorCrash builds the diagnostic message for an iteration
// where the Executor exited non-zero without emitting the terminal
// iteration_finished call. The format is human-readable, single
// error wrapping a multi-line string, with the iteration id, agent id
// (when known), the captured stderr tail (when present), and either the
// path to the persisted capture file or a hint to enable --debug-logs.
func formatExecutorCrash(result ExecResult, iterationID string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "director: executor (iteration %s", iterationID)
	if result.AgentID != "" {
		fmt.Fprintf(&b, ", agent %s", result.AgentID)
	}
	fmt.Fprintf(&b, ") exited %d with no terminal signal", result.ExitCode)
	if tail := strings.TrimSpace(result.StderrTail); tail != "" {
		fmt.Fprintf(&b, "\nlast stderr: %s", tail)
	}
	if result.StderrLogPath != "" {
		fmt.Fprintf(&b, "\nfull output at: %s", result.StderrLogPath)
	} else {
		b.WriteString("\nhint: rerun with --debug-logs to capture full subprocess output to .bcc/sessions/<id>/runs/<iteration>/")
	}
	return errors.New(b.String())
}

// parseSignalString converts the wire string the agent sent on
// iteration_finished to an agentcontract.Signal. Unknown values
// degrade to SignalUnknown so the caller can fall back to a default.
func parseSignalString(v string) agentcontract.Signal {
	switch v {
	case "continue":
		return agentcontract.SignalContinue
	case "review":
		return agentcontract.SignalReview
	case "done":
		return agentcontract.SignalDone
	case "blocked":
		return agentcontract.SignalBlocked
	default:
		return agentcontract.SignalUnknown
	}
}

// resolveAssignment unpacks the (model, effort) from a per-phase
// assignment. With config.Roles menus and FillPlanFromMenus in place,
// every assignment is non-nil and complete by the time the loop
// reaches it; nil here returns empty strings so headless renderers
// have something to print without crashing.
func resolveAssignment(a *supervision.RoleAssignment) (string, string) {
	if a == nil {
		return "", ""
	}
	return a.Model, a.Effort
}

// effectiveRetryBudget resolves the per-iteration retry budget by
// taking the higher of the sub-DAG's per-task maximum and the run
// config floor. The floor exists because a Planner that emits
// retry_budget=1 on every task would otherwise silently shrink the
// run-wide budget (Config.Loop.RetryBudget) the user asked for in
// .bcc.toml. Per-task overrides may raise the budget above the floor;
// they cannot drop below it.
func effectiveRetryBudget(phase *supervision.Phase, subDAG []string, configFloor int) int {
	return max(maxRetryBudget(phase, subDAG), configFloor)
}

// maxRetryBudget aggregates the per-task retry budgets in subDAG into
// a single iteration-level budget. The maximum across the sub-DAG's
// tasks is the safe choice: every task tolerates at least its own
// budget, and the sub-DAG retries as a whole.
func maxRetryBudget(phase *supervision.Phase, subDAG []string) int {
	best := 0
	for _, tid := range subDAG {
		t := phase.TaskByID(tid)
		if t == nil {
			continue
		}
		if t.RetryBudget > best {
			best = t.RetryBudget
		}
	}
	return best
}

// taskEventBridge translates successful bcc_task_* calls observed on
// the dag.Handler into typed loop events. Other methods are dropped.
// The send is non-blocking: a stalled TUI consumer must not freeze
// the MCP HTTP handler that runs inside the Executor's MCP call. A
// dropped progress event delays the bar by one frame; a stalled
// handler stalls the entire pipeline.
type taskEventBridge struct {
	events chan<- Event
	reg    *dag.AgentRegistry
}

func (b *taskEventBridge) OnCall(method, agentID string, _ dag.Role, input map[string]any) {
	id, _ := input["id"].(string)
	phase := ""
	if entry, ok := b.reg.Lookup(dag.AgentID(agentID)); ok {
		phase = string(entry.PhaseID)
	}
	now := time.Now()
	var ev Event
	switch method {
	case dag.MethodTaskStarted:
		ev = TaskStarted{AgentID: agentID, PhaseID: phase, TaskID: id, At: now}
	case dag.MethodTaskCompleted:
		ev = TaskCompleted{AgentID: agentID, PhaseID: phase, TaskID: id, At: now}
	case dag.MethodTaskApproved:
		ev = TaskApproved{AgentID: agentID, PhaseID: phase, TaskID: id, At: now}
	case dag.MethodTaskNeedsFix:
		feedback, _ := input["feedback"].(string)
		ev = TaskNeedsFix{AgentID: agentID, PhaseID: phase, TaskID: id, Note: feedback, At: now}
	default:
		return
	}
	if b.events == nil {
		return
	}
	select {
	case b.events <- ev:
	default:
	}
}

// awaitEscalation blocks until the escalation channel delivers a reply
// or the context is canceled. nil channel means "no escalation handler
// is wired"; callers should treat that as Abort.
func awaitEscalation(ctx context.Context, ch <-chan EscalationReply) (EscalationReply, error) {
	if ch == nil {
		return EscalationReply{Kind: EscalationAbort}, nil
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return EscalationReply{Kind: EscalationAbort}, nil
		}
		return r, nil
	case <-ctx.Done():
		return EscalationReply{}, ctx.Err()
	}
}
