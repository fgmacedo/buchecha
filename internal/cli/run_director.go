package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/fgmacedo/buchecha/internal/config"
	"github.com/fgmacedo/buchecha/internal/director"
	directorclaude "github.com/fgmacedo/buchecha/internal/director/claude"
	"github.com/fgmacedo/buchecha/internal/director/dag"
	"github.com/fgmacedo/buchecha/internal/executor/claude"
	gitcli "github.com/fgmacedo/buchecha/internal/git/cli"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/tui"
)

// errDirectorAborted is returned when the user answers [A]bort at the
// confirmation prompt. ExitCode is set to ExitInvalid before returning.
var errDirectorAborted = errors.New("director: user aborted plan at confirmation")

// errDirectorRePlanAborted is returned when the user answers [A]bort
// at the re-plan diff confirmation prompt under --resume after the
// spec hash diverged from the persisted plan. ExitCode is ExitInvalid.
var errDirectorRePlanAborted = errors.New("director: user aborted replanned plan at diff confirmation")

// errPlannerSkipped is returned by freshPlan / resolveDirectorPlan when
// the Planner declared the spec done by calling bcc_plan_skip. It is a
// success path: callers map it to ExitDone and surface a friendly
// "nothing to do" message instead of a fatal error.
type errPlannerSkipped struct {
	reason string
}

func (e errPlannerSkipped) Error() string {
	if e.reason == "" {
		return "director: planner skipped: nothing to do"
	}
	return "director: planner skipped: " + e.reason
}

// directorDeps groups the ports runDirector consumes. Production code
// builds Claude adapters; tests inject scripted fakes. Session
// resolution happens inside runDirectorWith from baseDir + the dio
// flags, unless a test pre-populates store, in which case that store
// is used as-is and no session helpers are consulted. newExecutor is
// a factory because the Executor is parameterized per-phase by the
// briefing prompt file path, which only exists after the Briefer has
// produced a Briefing.
type directorDeps struct {
	planner     director.Planner
	briefer     director.Briefer
	reviewer    director.Reviewer
	registerFn  func(role dag.Role) (string, func(), error)
	baseDir     string
	store       *director.Store
	git         loop.GitProbe
	newExecutor func(args dag.RegisterArgs, renderSystem func(agentID string) (string, error), assignment *director.RoleAssignment) loop.Executor
	now         func() time.Time
	// handler, when non-nil, overrides the run-wide MCP handler the
	// loop receives in DirectorPorts. Tests inject one to drive the
	// loop without standing up the full MCP boot.
	handler *dag.Handler
	// boot is the run-wide MCP plumbing the cli wired in
	// runDirector. The deps struct keeps a back-reference so
	// runDirectorWith can call bindSession after session resolution
	// without re-constructing the boot. Tests leave this nil and skip
	// the bind step.
	boot *mcpBoot
}

// directorIO captures the I/O surface so tests can drive the
// confirmation prompt without touching os.Stdin / os.Stderr. The
// session and resume flags drive how runDirectorWith resolves the
// per-run Store; the spec's first acceptance pins the matrix.
type directorIO struct {
	stdin       io.Reader
	stderr      io.Writer
	autoProceed bool
	resume      bool
	session     string
}

// runDirector is the entry point for the Director-driven loop, called
// from runSpec when [director].enabled is on. P4 wires planning,
// persistence, and user confirmation; the brief/execute/review pipeline
// lands in P5-P7.
func runDirector(ctx context.Context, cancel context.CancelFunc, specPath string, cfg *config.Config) error {
	boot, err := startMCPBoot(nil)
	if err != nil {
		ExitCode = loop.ExitInvalid
		return err
	}
	defer boot.Close()

	deps := defaultDirectorDeps(cfg, boot)
	deps.boot = boot
	dio := directorIO{
		stdin:       os.Stdin,
		stderr:      os.Stderr,
		autoProceed: runAutoProceed,
		resume:      runResume,
		session:     runSessionID,
	}
	return runDirectorWith(ctx, cancel, specPath, cfg, deps, dio)
}

func defaultDirectorDeps(cfg *config.Config, boot *mcpBoot) directorDeps {
	// In TUI mode the alt-screen renderer owns the terminal: any byte the
	// agent subprocess writes to os.Stderr would be drawn on top of the
	// dashboard, scrambling panels. Discard the live tee in that mode and
	// rely on the adapter's internal ringBuffer for diagnostics; in
	// text/json mode forward stderr verbatim so users see auth/quota
	// errors as they happen.
	var subprocessStderr io.Writer = os.Stderr
	if runOutput == OutputTUI {
		subprocessStderr = io.Discard
	}
	adapter := directorclaude.New(directorclaude.Config{
		Binary:       cfg.Director.Claude.Binary,
		Model:        cfg.Director.Claude.Model,
		Effort:       cfg.Director.Claude.Effort,
		ExtraArgs:    cfg.Director.Claude.ExtraArgs,
		MaxBudgetUSD: cfg.Director.Claude.MaxBudgetUSD,
		Stderr:       subprocessStderr,
		MCPURL:       boot.url(),
		MCPToken:     boot.token(),
	})
	registry := director.MergeCapabilityRegistries(
		directorclaude.Capabilities(),
		claude.Capabilities(),
	)
	if boot != nil && boot.handler != nil {
		boot.handler.SetCapabilityRegistry(&registry)
	}
	return directorDeps{
		planner:    adapter,
		briefer:    adapter,
		reviewer:   adapter,
		registerFn: boot.registerDirectorAgent,
		baseDir:    ".bcc",
		git:        gitcli.New(""),
		newExecutor: func(args dag.RegisterArgs, renderSystem func(agentID string) (string, error), assignment *director.RoleAssignment) loop.Executor {
			mcpCfg, cleanup, err := boot.executorMCPConfig(dag.RoleExecutor, args)
			if err != nil {
				return &failingExecutor{err: fmt.Errorf("register executor agent: %w", err)}
			}
			systemPromptFile, err := renderSystem(mcpCfg.AgentID)
			if err != nil {
				cleanup()
				return &failingExecutor{err: fmt.Errorf("render executor system prompt: %w", err)}
			}
			model := cfg.Agent.Claude.Model
			effort := cfg.Agent.Claude.Effort
			if assignment != nil {
				if assignment.Model != "" {
					model = assignment.Model
				}
				if assignment.Effort != "" {
					effort = assignment.Effort
				}
			}
			inner := claude.New(claude.Config{
				Binary:            cfg.Agent.Claude.Binary,
				Model:             model,
				Effort:            effort,
				ExtraArgs:         cfg.Agent.Claude.ExtraArgs,
				SkipPermissions:   cfg.Agent.Claude.ShouldSkipPermissions(),
				SystemPromptFile:  systemPromptFile,
				Stderr:            subprocessStderr,
				MCPURL:            mcpCfg.MCPURL,
				MCPToken:          mcpCfg.MCPToken,
				MCPConnectionName: mcpCfg.MCPConnectionName,
				AgentID:           mcpCfg.AgentID,
			})
			return &deregisteringExecutor{inner: inner, cleanup: cleanup}
		},
		now: time.Now,
	}
}

// failingExecutor is the loop.Executor returned when registration fails
// inside the newExecutor closure. The Director loop surfaces the error
// from Run as a fatal abort.
type failingExecutor struct{ err error }

func (e *failingExecutor) Run(ctx context.Context, _ string, _ chan<- agentcontract.AgentEvent) (loop.ExecResult, error) {
	return loop.ExecResult{ExitCode: -1}, e.err
}

// runDirectorWith is the testable core: same signature as runDirector
// plus injected ports. All error paths set ExitCode before returning so
// the cobra wrapper exits with the right code.
func runDirectorWith(
	ctx context.Context,
	cancel context.CancelFunc,
	specPath string,
	cfg *config.Config,
	deps directorDeps,
	dio directorIO,
) error {
	content, err := os.ReadFile(specPath)
	if err != nil {
		ExitCode = loop.ExitInvalid
		return fmt.Errorf("director: read spec %s: %w", specPath, err)
	}
	hash := director.SpecHash(content)

	if deps.store == nil {
		store, sessErr := resolveDirectorSession(deps, dio, specPath, hash)
		if sessErr != nil {
			ExitCode = loop.ExitInvalid
			return sessErr
		}
		deps.store = store
	}
	fmt.Fprintf(dio.stderr, "bcc: session=%s status=%s\n", deps.store.Session().ID, deps.store.Session().Status)

	if deps.boot != nil {
		var gitProvider dag.GitDiffProvider
		if g, ok := deps.git.(dag.GitDiffProvider); ok {
			gitProvider = g
		}
		deps.boot.bindSession(deps.store, cfg.Director.IsMCPAuditEnabled(), gitProvider, director.JournalDeltaProvider{})
	}

	if runOutput == OutputTUI {
		return runDirectorTUI(ctx, cancel, specPath, hash, cfg, deps, dio)
	}

	fmt.Fprintf(dio.stderr, "bcc: director enabled; planning %s (model thinking, typically 30s-3min)...\n", specPath)
	stopHeartbeat := startPlanningHeartbeat(ctx, dio.stderr, deps.now)
	plan, err := resolveDirectorPlan(ctx, specPath, hash, deps, dio, nil)
	stopHeartbeat()
	if err != nil {
		var skipped errPlannerSkipped
		if errors.As(err, &skipped) {
			return finishHeadlessNothingToDo(deps, dio, skipped.reason)
		}
		_ = deps.store.Touch(director.SessionAborted, deps.now())
		return err
	}

	escalation := stdinEscalationGate(ctx, dio.stdin)

	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      deps.git,
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     deps.briefer,
			Reviewer:    deps.reviewer,
			Store:       deps.store,
			NewExecutor: deps.newExecutor,
			Handler:     directorEffectiveHandler(deps),
			Escalation:  escalation,
		},
	}

	events, drained, derr := dispatchEvents(runOutput, loop.LevelInfo)
	if derr != nil {
		ExitCode = loop.ExitInvalid
		_ = deps.store.Touch(director.SessionAborted, deps.now())
		return derr
	}

	code, runErr := l.Run(ctx, events)
	<-drained
	ExitCode = code
	return runErr
}

// resolveDirectorSession picks a session for this run based on the
// flag matrix in the migration spec acceptance:
//
//  1. --resume + --session <id>: resume the named session; spec must
//     match.
//  2. --resume only: pick the most recent session for this spec; if
//     none exists, create a fresh session and proceed.
//  3. --session <id> only (no --resume): require the session to exist;
//     do not silently overwrite by creating a fresh one.
//  4. neither: create a new session.
func resolveDirectorSession(deps directorDeps, dio directorIO, specPath, hash string) (*director.Store, error) {
	now := deps.now
	if now == nil {
		now = time.Now
	}
	switch {
	case dio.session != "":
		sess, err := director.ResolveSession(deps.baseDir, dio.session, specPath)
		if err != nil {
			return nil, fmt.Errorf("director: resolve session: %w", err)
		}
		store, err := director.OpenSession(deps.baseDir, sess.ID)
		if err != nil {
			return nil, fmt.Errorf("director: open session %s: %w", sess.ID, err)
		}
		return store, nil
	case dio.resume:
		matches, err := director.FindSessionsForSpec(deps.baseDir, specPath)
		if err != nil {
			return nil, fmt.Errorf("director: list sessions: %w", err)
		}
		switch len(matches) {
		case 0:
			fmt.Fprintln(dio.stderr, "bcc: --resume requested but no session for this spec; creating a fresh one")
			store, _, cerr := director.CreateSession(deps.baseDir, specPath, hash, now())
			if cerr != nil {
				return nil, fmt.Errorf("director: create session: %w", cerr)
			}
			return store, nil
		case 1:
			store, err := director.OpenSession(deps.baseDir, matches[0].ID)
			if err != nil {
				return nil, fmt.Errorf("director: open session %s: %w", matches[0].ID, err)
			}
			return store, nil
		default:
			ids := make([]string, 0, len(matches))
			for _, m := range matches {
				ids = append(ids, m.ID)
			}
			return nil, fmt.Errorf("%w: candidates: %s",
				director.ErrSessionAmbiguous, strings.Join(ids, ", "))
		}
	default:
		store, _, err := director.CreateSession(deps.baseDir, specPath, hash, now())
		if err != nil {
			return nil, fmt.Errorf("director: create session: %w", err)
		}
		return store, nil
	}
}

// startPlanningHeartbeat prints "bcc: planner still working (Xs
// elapsed)" to stderr every 15s while the planner subprocess is
// blocking. The returned stop function cancels the ticker; safe to
// call multiple times. The first heartbeat fires at 15s, not
// immediately, so quick plans stay quiet.
//
// Without this the user sees nothing between the "planning..."
// banner and the eventual plan render, since claude --bare emits
// almost no stderr and we drain its stdout silently.
func startPlanningHeartbeat(ctx context.Context, w io.Writer, now func() time.Time) func() {
	stop := make(chan struct{})
	if now == nil {
		now = time.Now
	}
	start := now()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := now().Sub(start).Round(time.Second)
				fmt.Fprintf(w, "bcc: planner still working (%s elapsed)\n", elapsed)
			case <-ctx.Done():
				return
			case <-stop:
				return
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }
}

// stdinEscalationGate spawns a goroutine that reads escalation
// answers from stdin and turns them into EscalationReply tokens. The
// goroutine never writes to out: the user-facing prompt for an
// escalation is the DirectorEscalation event, which the text/json
// drain renders, and the TUI overlay (P8) handles separately. Keeping
// the gate write-free avoids races between the goroutine and tests
// that inspect stderr after the loop terminates.
//
// Tests with no stdin lines (strings.NewReader("")) drive the
// goroutine to EOF immediately; ctx cancellation also exits the loop.
func stdinEscalationGate(ctx context.Context, in io.Reader) <-chan loop.EscalationReply {
	if in == nil {
		return nil
	}
	ch := make(chan loop.EscalationReply, 1)
	go func() {
		defer close(ch)
		br := bufio.NewReader(in)
		readLine := func() (string, bool) {
			line, err := br.ReadString('\n')
			if errors.Is(err, io.EOF) && line == "" {
				return "", false
			}
			if err != nil && !errors.Is(err, io.EOF) {
				return "", false
			}
			return line, true
		}
		for {
			line, ok := readLine()
			if !ok {
				return
			}
			var reply loop.EscalationReply
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "r", "resume":
				hintLine, hintOk := readLine()
				if !hintOk {
					return
				}
				reply = loop.EscalationReply{
					Kind: loop.EscalationResume,
					Hint: strings.TrimRight(strings.TrimRight(hintLine, "\n"), "\r"),
				}
			case "f", "force", "force-approve":
				reply = loop.EscalationReply{Kind: loop.EscalationForceApprove}
			case "s", "skip":
				reply = loop.EscalationReply{Kind: loop.EscalationSkip}
			case "a", "abort":
				reply = loop.EscalationReply{Kind: loop.EscalationAbort}
			default:
				continue
			}
			select {
			case ch <- reply:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// runDirectorTUI runs the Director under the bubbletea dashboard from
// t=0: the program enters alt-screen before the planner starts, the
// planner runs in a background goroutine that streams its AgentEvents
// into the TUI's raw channel as loop.AgentEventReceived (so the now /
// health / actions panels render planning activity in real time), and
// only after the plan resolves (and is confirmed) does the inner Loop
// kick off in a second goroutine. The host returns the loop's ExitCode
// + error after the bubbletea program exits.
//
// Plan resolution failure: the planner goroutine emits a synthetic
// LoopFinished onto raw with reason "planner failed: <msg>"; the Model
// quits naturally on that signal.
//
// Confirmation: when dio.autoProceed is false, the planner goroutine
// asks the Model for confirmation via a PlanConfirm reply channel
// (mirrors the escalation modal pattern). The Model surfaces the modal
// over the rendered plan once PhasePlanned has latched the tree.
func runDirectorTUI(ctx context.Context, cancel context.CancelFunc, specPath, hash string, cfg *config.Config, deps directorDeps, dio directorIO) error {
	gate := tui.NewGate()
	escalation := make(chan loop.EscalationReply, 1)
	planConfirm := make(chan tui.PlanConfirmReply, 1)

	gitProbeAdapter, _ := deps.git.(tui.GitProbe)
	branch := ""
	if br, gerr := deps.git.CurrentBranch(ctx); gerr == nil {
		branch = br
	}

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	raw := make(chan loop.Event, 256)

	type runResult struct {
		code int
		err  error
	}
	var (
		resultMu sync.Mutex
		latest   runResult
	)
	setLatest := func(r runResult) {
		resultMu.Lock()
		defer resultMu.Unlock()
		latest = r
	}

	// resolvedPlan is the plan the orchestrator confirmed on the first
	// run; the session-menu Resume factory reuses it so [r] does not
	// re-run the planner. Captured by the orchestrator goroutine, read
	// by the factory closure on subsequent UI events.
	var (
		planMu       sync.Mutex
		resolvedPlan *director.Plan
	)
	setResolvedPlan := func(p *director.Plan) {
		planMu.Lock()
		defer planMu.Unlock()
		resolvedPlan = p
	}
	currentPlan := func() *director.Plan {
		planMu.Lock()
		defer planMu.Unlock()
		return resolvedPlan
	}

	// runLoopOn spins up loop.Loop.Run against a freshly built events
	// channel. Used by both the first-run orchestrator and the session
	// Resume factory; loop.Loop.Run owns the channel lifecycle and emits
	// a terminal LoopFinished plus close on every exit path.
	runLoopOn := func(plan *director.Plan, ch chan loop.Event) {
		defer func() {
			if r := recover(); r != nil {
				setLatest(runResult{
					code: loop.ExitInvalid,
					err:  fmt.Errorf("loop panicked: %v\n%s", r, debug.Stack()),
				})
			}
		}()
		l := &loop.Loop{
			SpecPath: specPath,
			Config:   cfg,
			Git:      deps.git,
			Logger:   discard,
			Director: &loop.DirectorPorts{
				Plan:        plan,
				Briefer:     deps.briefer,
				Reviewer:    deps.reviewer,
				Store:       deps.store,
				NewExecutor: deps.newExecutor,
				Handler:     directorEffectiveHandler(deps),
				Escalation:  escalation,
			},
		}
		code, err := l.Run(ctx, ch)
		setLatest(runResult{code: code, err: err})
	}

	// newEvents is the host's Resume factory: the user pressed [r] in
	// the session menu after the loop terminated. Build a fresh channel,
	// spawn the loop on it (reusing the confirmed plan), and hand the
	// channel back to the Model so panels live-update against the new
	// run.
	newEvents := func() <-chan loop.Event {
		plan := currentPlan()
		if plan == nil {
			ch := make(chan loop.Event, 1)
			emitLoopFinished(ch, "no plan to resume", loop.ExitInvalid)
			close(ch)
			return ch
		}
		ch := make(chan loop.Event, 256)
		go runLoopOn(plan, ch)
		return ch
	}

	model := tui.New(tui.Options{
		Events:          raw,
		Cancel:          cancel,
		Gate:            gate,
		SpecPath:        specPath,
		Branch:          branch,
		SessionID:       deps.store.Session().ID,
		MaxIter:         cfg.Loop.MaxIterations,
		GitProbe:        gitProbeAdapter,
		GitCtx:          ctx,
		EscalationGate:  escalation,
		PlanConfirmGate: planConfirm,
		PlanningPending: true,
		AutoProceed:     dio.autoProceed,
		NewEvents:       newEvents,
	})
	progOpts := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	}
	if runNoColor {
		progOpts = append(progOpts, tea.WithColorProfile(colorprofile.NoTTY))
	}
	program := tea.NewProgram(model, progOpts...)
	model.SetProgram(program)

	defer func() {
		if r := recover(); r != nil {
			_ = program.ReleaseTerminal()
			fmt.Fprintf(os.Stderr, "bcc: panic in TUI host: %v\n%s\n", r, debug.Stack())
			panic(r)
		}
	}()

	// orchestrator drives plan resolution -> confirmation -> loop start.
	// It owns the raw channel: closes it via a synthetic LoopFinished on
	// every terminal path so the bubbletea bridge sees the close and the
	// program exits cleanly.
	orchestrate := func() {
		defer func() {
			if r := recover(); r != nil {
				setLatest(runResult{
					code: loop.ExitInvalid,
					err:  fmt.Errorf("director TUI orchestrator panicked: %v\n%s", r, debug.Stack()),
				})
				// Best-effort terminal signal. raw may already be
				// closed (loop.Loop.Run owns the close on the post-
				// planning path), so swallow any send/close-on-closed
				// secondary panic and rely on the prior LoopFinished
				// to drive the bridge.
				func() {
					defer func() { _ = recover() }()
					emitLoopFinished(raw, "fatal", loop.ExitInvalid)
					close(raw)
				}()
			}
		}()

		plan, perr := resolveDirectorPlanTUI(ctx, specPath, hash, deps, dio, raw)
		if perr != nil {
			var skipped errPlannerSkipped
			if errors.As(perr, &skipped) {
				_ = deps.store.Touch(director.SessionDone, deps.now())
				model.SignalPlanSkipped(skipped.reason)
				setLatest(runResult{code: loop.ExitDone, err: nil})
				emitLoopFinished(raw, LoopFinishedReasonNothingToDo, loop.ExitDone)
				close(raw)
				return
			}
			_ = deps.store.Touch(director.SessionAborted, deps.now())
			if errors.Is(perr, errDirectorAborted) || errors.Is(perr, errDirectorRePlanAborted) {
				setLatest(runResult{code: loop.ExitInvalid, err: perr})
				emitLoopFinished(raw, "user cancelled", loop.ExitInvalid)
			} else {
				// Planner crashed without producing a terminal MCP call
				// (e.g. claude exited 1 because the account ran out of
				// credits). Keep the TUI alive in session mode so the
				// user reads the underlying error in the footer and
				// decides whether to retry, edit the spec, or quit.
				// The orchestrator returns nil here: the run "finished"
				// in the user-visible sense; the run-level ExitCode is
				// still ExitInvalid so headless callers see the same
				// failure.
				model.SignalPlanFailed(perr.Error())
				setLatest(runResult{code: loop.ExitInvalid, err: nil})
				emitLoopFinished(raw, LoopFinishedReasonPlannerFailed, loop.ExitInvalid)
			}
			close(raw)
			return
		}

		// Confirmation. autoProceed bypasses; otherwise wait on the
		// Model's PlanConfirm reply (sent when the user picks
		// [P]roceed / [A]bort in the modal).
		if !dio.autoProceed {
			model.SignalPlanReady(plan)
			select {
			case reply := <-planConfirm:
				if reply.Kind == tui.PlanConfirmAbort {
					_ = deps.store.Touch(director.SessionAborted, deps.now())
					setLatest(runResult{code: loop.ExitInvalid, err: errDirectorAborted})
					emitLoopFinished(raw, "user cancelled", loop.ExitInvalid)
					close(raw)
					return
				}
			case <-ctx.Done():
				setLatest(runResult{code: loop.ExitInvalid, err: ctx.Err()})
				emitLoopFinished(raw, "user cancelled", loop.ExitInvalid)
				close(raw)
				return
			}
		}
		model.ClearPlanningPending()
		setResolvedPlan(plan)
		runLoopOn(plan, raw)
		// loop.Loop.Run owns raw for the duration of Run: it emits a
		// final LoopFinished and closes the channel via its own defer,
		// even on panic. Closing here would double-close.
	}

	go orchestrate()

	if _, err := program.Run(); err != nil {
		cancel()
		printSessionResumeHint(dio.stderr, deps.store.Session().ID, specPath)
		return err
	}
	printSessionResumeHint(dio.stderr, deps.store.Session().ID, specPath)
	resultMu.Lock()
	defer resultMu.Unlock()
	ExitCode = latest.code
	return latest.err
}

// printSessionResumeHint writes a one-line stderr message after the TUI
// alt-screen has been released so the user sees the session id and the
// exact command needed to resume the run. Headless callers (text/json)
// already print session=<id> at startup; the TUI counterpart was lost
// to the alt-screen, so this restores it on exit.
func printSessionResumeHint(w io.Writer, sessionID, specPath string) {
	if w == nil || sessionID == "" {
		return
	}
	fmt.Fprintf(w, "bcc: session=%s (resume with: bcc run %s --resume --session %s)\n",
		sessionID, specPath, sessionID)
}

// LoopFinishedReasonNothingToDo is the canonical Reason carried on the
// LoopFinished event when the Planner declared the spec done via
// bcc_plan_skip. Headless render backends (text/json) emit this reason
// so consumers can branch deterministically; the TUI maps the same
// reason to a friendly terminal screen.
const LoopFinishedReasonNothingToDo = "nothing_to_do"

// LoopFinishedReasonPlannerFailed is the canonical Reason emitted when
// the planner subprocess exited with no terminal MCP call (no
// bcc_plan_emit, no bcc_plan_skip). The TUI keeps the dashboard alive
// in session mode so the user can read the underlying error in the
// session footer and decide whether to resume / edit / quit.
const LoopFinishedReasonPlannerFailed = "planner_failed"

// finishHeadlessNothingToDo runs the post-skip terminal sequence for
// the text/json output paths: stamps the session as done, emits a
// structured LoopFinished event through the chosen render backend so
// JSON consumers see one terminal line, prints a friendly stderr
// message in text mode, and exits with ExitDone (0). The TUI path has
// its own handler so the dashboard stays alive.
func finishHeadlessNothingToDo(deps directorDeps, dio directorIO, reason string) error {
	now := deps.now
	if now == nil {
		now = time.Now
	}
	_ = deps.store.Touch(director.SessionDone, now())

	events, drained, derr := dispatchEvents(runOutput, loop.LevelInfo)
	if derr != nil {
		ExitCode = loop.ExitInvalid
		return derr
	}
	events <- loop.LoopFinished{
		Reason:   LoopFinishedReasonNothingToDo,
		ExitCode: loop.ExitDone,
		At:       now(),
	}
	close(events)
	<-drained

	msg := reason
	if msg == "" {
		msg = "spec is already complete"
	}
	fmt.Fprintf(dio.stderr, "bcc: nothing to do; %s\n", msg)
	ExitCode = loop.ExitDone
	return nil
}

// emitLoopFinished sends a synthetic LoopFinished onto the events
// channel. Used by the TUI orchestrator for terminal paths that bypass
// loop.Loop.Run (planner failure, user abort at confirmation, fatal
// orchestrator panic). The Model treats this as a normal end-of-run
// signal and the program quits.
func emitLoopFinished(events chan<- loop.Event, reason string, exit int) {
	events <- loop.LoopFinished{Reason: reason, ExitCode: exit, At: time.Now()}
}

// resolveDirectorPlanTUI is the TUI-mode counterpart of
// resolveDirectorPlan. The signature differs only in that it takes a
// raw events channel onto which the planner's stream-json envelopes
// are forwarded as loop.AgentEventReceived; the rest of the resume /
// fresh / re-plan logic is shared via resolveDirectorPlan(events).
//
// Confirmation is deferred to the caller (the orchestrator), which
// surfaces a TUI modal when not running with --auto-proceed.
func resolveDirectorPlanTUI(
	ctx context.Context,
	specPath, hash string,
	deps directorDeps,
	dio directorIO,
	raw chan<- loop.Event,
) (*director.Plan, error) {
	// resolveDirectorPlan accepts an events sink that, when non-nil,
	// receives the planner's AgentEvents already wrapped as
	// loop.AgentEventReceived; the existing flow is otherwise unchanged.
	return resolveDirectorPlan(ctx, specPath, hash, deps, dio, raw)
}

// resolveDirectorPlan returns the Plan to run, handling --resume:
//
//   - --resume + persisted plan + matching SpecHash: reuse the plan,
//     skip planner + confirmation entirely (the user has already
//     approved this plan in a prior session).
//   - --resume + persisted plan + diverging SpecHash: call the planner,
//     compute a PlanDiff against the stored plan, render the diff, and
//     prompt [D]iff/[P]roceed/[A]bort. Persist the new plan on Proceed.
//   - --resume + no persisted plan: fall through to the fresh path.
//   - no --resume: always plan, persist, and run the standard
//     [P]roceed/[A]bort confirmation.
//
// All error paths set ExitCode before returning so the cobra wrapper
// exits with the right code.
// resolveDirectorPlan resolves the plan for this run. When raw is
// non-nil the planner's stream telemetry is forwarded onto raw as
// loop.AgentEventReceived (TUI mode); when raw is nil the planner runs
// silently and the caller is responsible for any progress indicator
// (text/json mode uses startPlanningHeartbeat).
//
// In TUI mode the [P]roceed/[A]bort confirmation is deferred to the
// caller (it owns the modal); resolveDirectorPlan returns the
// validated plan and the orchestrator decides whether to gate.
func resolveDirectorPlan(
	ctx context.Context,
	specPath string,
	hash string,
	deps directorDeps,
	dio directorIO,
	raw chan<- loop.Event,
) (*director.Plan, error) {
	if dio.resume {
		existing, readErr := deps.store.ReadPlan()
		if readErr == nil {
			if existing.SpecHash == hash {
				if raw == nil {
					RenderPlan(existing, dio.stderr)
					fmt.Fprintln(dio.stderr, "\nbcc: --resume; spec hash unchanged; resuming from persisted plan")
				}
				return existing, nil
			}
			return rePlanFlow(ctx, specPath, hash, existing, deps, dio, raw)
		}
		if !errors.Is(readErr, fs.ErrNotExist) {
			ExitCode = loop.ExitInvalid
			return nil, fmt.Errorf("director: read persisted plan: %w", readErr)
		}
		if raw == nil {
			fmt.Fprintln(dio.stderr, "bcc: session has no persisted plan; planning from scratch")
		}
	}

	plan, err := freshPlan(ctx, specPath, hash, deps, raw)
	if err != nil {
		return nil, err
	}
	if err := deps.store.WritePlan(plan); err != nil {
		ExitCode = loop.ExitInvalid
		return nil, fmt.Errorf("director: persist plan: %w", err)
	}
	if raw == nil {
		RenderPlan(plan, dio.stderr)
		if err := confirmDirectorPlan(dio); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// freshPlan calls the planner, normalises the bcc-owned fields, and
// validates. It is the shared kernel used by both the fresh path and
// rePlanFlow; neither writes to disk before validation. The planner is
// invoked under a freshly-registered Director agent_id so the run-wide
// MCP handler can scope its emit and audit log entries.
//
// When raw is non-nil, the planner's AgentEvents are forwarded onto
// raw as loop.AgentEventReceived for the TUI to render in real time.
func freshPlan(ctx context.Context, specPath string, hash string, deps directorDeps, raw chan<- loop.Event) (*director.Plan, error) {
	agentID, deregister, err := registerDirectorAgent(deps, dag.RolePlanner)
	if err != nil {
		ExitCode = loop.ExitInvalid
		return nil, err
	}
	defer deregister()

	var agentEvents chan agentcontract.AgentEvent
	pumpDone := make(chan struct{})
	if raw != nil {
		agentEvents = make(chan agentcontract.AgentEvent, 256)
		go func() {
			defer close(pumpDone)
			for ae := range agentEvents {
				select {
				case raw <- loop.AgentEventReceived{Event: ae}:
				case <-ctx.Done():
					return
				}
			}
		}()
	} else {
		close(pumpDone)
	}

	var plannerRegistry director.CapabilityRegistry
	if h := directorEffectiveHandler(deps); h != nil {
		if reg := h.CapabilityRegistry(); reg != nil {
			plannerRegistry = *reg
		}
	}
	plan, _, runErr := deps.planner.Plan(ctx, director.PlannerInput{
		AgentID:  agentID,
		SpecPath: specPath,
		SpecHash: hash,
		Registry: plannerRegistry,
	}, agentEvents)
	if agentEvents != nil {
		close(agentEvents)
	}
	<-pumpDone

	// The Plan flows through the MCP handler via bcc_plan_emit; the
	// adapter return is nil by design. Handler state is authoritative:
	// inspect it before honouring runErr so that a misbehaving agent
	// that crashed after calling bcc_plan_emit / bcc_plan_skip still has
	// its terminal call respected. A non-zero exit only becomes fatal
	// when the planner left no terminal state behind.
	if h := directorEffectiveHandler(deps); h != nil {
		if hp := h.Plan(); hp != nil {
			plan = hp
		} else if h.PlanSkipped() {
			return nil, errPlannerSkipped{reason: h.PlanSkipReason()}
		}
	}
	if plan == nil {
		ExitCode = loop.ExitInvalid
		if runErr != nil {
			return nil, fmt.Errorf("director: plan: %w", runErr)
		}
		return nil, errors.New("director: planner exited without emitting a plan or calling bcc_plan_skip")
	}
	plan.SpecHash = hash
	if plan.PlannedAt.IsZero() {
		plan.PlannedAt = deps.now()
	}
	var registry *director.CapabilityRegistry
	if h := directorEffectiveHandler(deps); h != nil {
		registry = h.CapabilityRegistry()
	}
	if err := director.ValidatePlan(plan, registry); err != nil {
		ExitCode = loop.ExitInvalid
		return nil, err
	}
	return plan, nil
}

// registerDirectorAgent obtains an agent_id from the run-wide registry
// for the given Director role. When deps.registerFn is unset (test
// fakes drive the loop without the real MCP boot), it returns a stable
// stub id so input.AgentID is non-empty.
func registerDirectorAgent(deps directorDeps, role dag.Role) (string, func(), error) {
	if deps.registerFn == nil {
		return "fake-" + string(role), func() {}, nil
	}
	return deps.registerFn(role)
}

// rePlanFlow handles the --resume hash-mismatch branch: replan, render
// the diff, prompt [D]iff/[P]roceed/[A]bort, persist on Proceed.
func rePlanFlow(
	ctx context.Context,
	specPath string,
	hash string,
	old *director.Plan,
	deps directorDeps,
	dio directorIO,
	raw chan<- loop.Event,
) (*director.Plan, error) {
	if raw == nil {
		fmt.Fprintln(dio.stderr, "bcc: --resume; spec hash diverged from persisted plan; replanning")
	}
	newPlan, err := freshPlan(ctx, specPath, hash, deps, raw)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		diff := director.ComputePlanDiff(old, newPlan)
		director.RenderPlanDiff(diff, dio.stderr)
		if dio.autoProceed {
			fmt.Fprintln(dio.stderr, "\nbcc: --auto-proceed; accepting replanned plan")
		} else {
			choice, err := promptDirectorRePlanConfirmation(dio.stdin, dio.stderr, diff)
			if err != nil {
				ExitCode = loop.ExitInvalid
				return nil, err
			}
			if choice == confirmAbort {
				ExitCode = loop.ExitInvalid
				return nil, errDirectorRePlanAborted
			}
		}
	}

	if err := deps.store.WritePlan(newPlan); err != nil {
		ExitCode = loop.ExitInvalid
		return nil, fmt.Errorf("director: persist replanned plan: %w", err)
	}
	return newPlan, nil
}

// confirmDirectorPlan runs the standard [P]roceed/[A]bort prompt for
// the fresh-plan path. Honors --auto-proceed.
func confirmDirectorPlan(dio directorIO) error {
	if dio.autoProceed {
		fmt.Fprintln(dio.stderr, "\nbcc: --auto-proceed; skipping confirmation")
		return nil
	}
	choice, err := promptDirectorConfirmation(dio.stdin, dio.stderr)
	if err != nil {
		ExitCode = loop.ExitInvalid
		return err
	}
	if choice == confirmAbort {
		ExitCode = loop.ExitInvalid
		return errDirectorAborted
	}
	return nil
}

type confirmChoice int

const (
	confirmProceed confirmChoice = iota + 1
	confirmAbort
)

// promptDirectorRePlanConfirmation runs the [D]iff/[P]roceed/[A]bort
// prompt for the --resume re-plan flow. [D]iff re-renders the diff
// against the originally rendered output (so the user can scroll back);
// [P] and [A] mirror the standard confirmation. EOF aborts.
func promptDirectorRePlanConfirmation(r io.Reader, w io.Writer, diff *director.PlanDiff) (confirmChoice, error) {
	br := bufio.NewReader(r)
	for {
		fmt.Fprint(w, "\nbcc: [D]iff again, [P]roceed with replanned plan, or [A]bort? ")
		line, err := br.ReadString('\n')
		if errors.Is(err, io.EOF) && line == "" {
			fmt.Fprintln(w, "bcc: stdin closed; treating as abort")
			return confirmAbort, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("director: read replan confirmation: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "p", "proceed", "y", "yes":
			return confirmProceed, nil
		case "a", "abort", "n", "no":
			return confirmAbort, nil
		case "d", "diff":
			director.RenderPlanDiff(diff, w)
		default:
			fmt.Fprintln(w, "bcc: please answer [D]iff, [P]roceed, or [A]bort")
		}
	}
}

// promptDirectorConfirmation reads a single line from r and returns the
// user's choice. Accepted answers: "p"/"P" (proceed), "a"/"A" (abort).
// Anything else loops; EOF on stdin is treated as Abort to fail closed.
func promptDirectorConfirmation(r io.Reader, w io.Writer) (confirmChoice, error) {
	br := bufio.NewReader(r)
	for {
		fmt.Fprint(w, "\nbcc: [P]roceed with this plan or [A]bort? ")
		line, err := br.ReadString('\n')
		if errors.Is(err, io.EOF) && line == "" {
			fmt.Fprintln(w, "bcc: stdin closed; treating as abort")
			return confirmAbort, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("director: read confirmation: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "p", "proceed", "y", "yes":
			return confirmProceed, nil
		case "a", "abort", "n", "no":
			return confirmAbort, nil
		default:
			fmt.Fprintln(w, "bcc: please answer [P]roceed or [A]bort")
		}
	}
}
