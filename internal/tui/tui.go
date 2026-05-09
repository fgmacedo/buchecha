// Package tui implements the bubbletea-driven dashboard rendered when
// `bcc run --output tui` is selected.
//
// The Model owns the panels (header, now, health, progress, risk,
// actions) and routes incoming messages to them. Every loop.Event,
// every periodic tick (spinner, git probe), and every spec re-parse
// arrives as a tea.Msg in Update; panels mutate their own state and
// the next View call composes their output.
package tui

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/services"
)

// gitProbeInterval is the cadence at which the dashboard polls the
// working tree for dirty-file count. 2s matches the spec's "every 2s"
// data-source contract.
const gitProbeInterval = 2 * time.Second

// nowHealthSplit is the fraction of the layout's full width allocated to
// the now box; the health box receives the rest. Centralised so future
// tuning is one edit (kept in code, not config, per the spec).
const nowHealthSplit = 0.6

// keyMap is the single source of truth for every keybinding the model
// honors. The handler matches via key.Matches; the help model derives
// both the inline footer and the ? overlay from the same set, so a new
// binding lights up in both places without an extra edit.
type keyMap struct {
	Quit  key.Binding
	Pause key.Binding
	Mouse key.Binding
	Webui key.Binding
	Help  key.Binding
}

// ShortHelp returns the bindings rendered as a single inline footer line
// (the help.Model's ShortHelp mode).
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Pause, k.Mouse, k.Webui, k.Help}
}

// FullHelp returns the bindings grouped by column for the ? overlay
// (help.Model's FullHelp mode). One column today; columns are added by
// returning more nested slices.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Quit, k.Pause, k.Mouse, k.Webui, k.Help}}
}

func defaultKeyMap() keyMap {
	return keyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q / Ctrl+C", "cancel the loop and quit"),
		),
		Pause: key.NewBinding(
			key.WithKeys(" ", "space"),
			key.WithHelp("space", "pause / resume between iterations"),
		),
		Mouse: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "toggle mouse capture (off: select/copy text)"),
		),
		Webui: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "open the web dashboard in the browser"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle this help overlay"),
		),
	}
}

// Model is the bubbletea model for the dashboard.
type Model struct {
	// Inputs wired by the caller (cmd/run.go).
	events   <-chan loop.Event
	cancel   context.CancelFunc
	gate     *Gate
	gitProbe GitProbe
	gitCtx   context.Context

	// svc is the application services handle. When non-nil, the TUI
	// receives its event stream via svc.Events.Subscribe instead of the
	// raw events channel. Wired from Options.Services.
	svc *services.Services

	// newEvents builds a fresh loop, spawns its goroutine, and returns
	// the new events channel. Invoked when the user presses [r] in the
	// session menu. Nil disables the resume action (e.g., in tests that
	// only exercise rendering).
	newEvents func() <-chan loop.Event

	// program references the bubbletea program that hosts this Model.
	// Required for the [e] edit action: ReleaseTerminal / RestoreTerminal
	// are program-scoped in bubbletea v2. Wired via SetProgram after
	// tea.NewProgram returns; nil disables the edit action.
	program *tea.Program

	// Panels.
	header   header
	now      nowPanel
	health   healthPanel
	progress progressPanel
	risk     riskPanel
	actions  actionsPanel
	director directorPanel

	// escalationGate is the Director-mode reply channel: when the user
	// resolves an escalation modal, the Model sends EscalationResume,
	// EscalationSkip, or EscalationAbort here. The host wires the same
	// channel into DirectorPorts.Escalation so the loop unblocks. nil in
	// non-Director or test contexts.
	escalationGate chan<- loop.EscalationReply

	// planningPending tracks the "planner is in flight" state. While
	// true the director panel renders a placeholder. Set via the
	// PlanningPending option; cleared once SignalPlanReady fires (or
	// SignalPlanSkipped / SignalPlanFailed on the failure paths).
	planningPending bool

	// Live state.
	width, height  int
	layout         layout // recomputed on tea.WindowSizeMsg
	paused         bool
	helpVisible    bool   // true while the ? overlay is up
	runBaselineSHA string // captured from the first IterationStarted

	// lastIterSignal latches the most recent iteration_result signal
	// the agent emitted via the wire protocol. The session badge reads
	// it when the loop terminates.
	lastIterSignal agentcontract.Signal

	// Session-mode state (P2.11). sessionMode latches when LoopFinished
	// arrives with a reason other than "user cancelled" or "fatal";
	// sessionReason carries the LoopFinished.Reason for the badge label.
	sessionMode    bool
	sessionReason  string
	sessionExitMsg string // best-effort hint surfaced after a failed [e] edit

	// nothingToDoMode latches when the Planner reports via plan_skip
	// that the spec is already complete. The dashboard renders a
	// quit-only friendly screen and stays alive until the user presses
	// q / Ctrl+C; nothingToDoReason carries the planner's free-text
	// explanation rendered to the user.
	nothingToDoMode   bool
	nothingToDoReason string

	// mouseCaptureOff toggles the View's MouseMode between
	// MouseModeCellMotion (default) and MouseModeNone. Off allows the
	// terminal/multiplexer to handle mouse events itself, restoring
	// native drag-to-select for copy/paste at the cost of in-app
	// scroll-wheel handling.
	mouseCaptureOff bool

	// webuiURL is the dashboard URL to open when the user presses the
	// Webui key (default 'w'). Empty disables the binding silently.
	webuiURL string
	// openBrowser is the platform-aware browser launcher the host wires
	// in (cli's openBrowser, in production). Nil disables the Webui
	// binding silently. The handler swallows errors via slog so the
	// alt-screen renderer is not corrupted by stray writes.
	openBrowser func(url string) error

	// Keybindings + help renderer (single source of truth).
	keys         keyMap
	sessionKeys  sessionKeyMap
	directorKeys directorKeyMap
	helpView     help.Model

	// Terminal state.
	finished  bool // true once the loop emitted LoopFinished or events closed
	cancelled bool // true once user pressed q or Ctrl+C
}

// SetProgram wires the bubbletea program reference into the Model so the
// [e] edit action can call ReleaseTerminal / RestoreTerminal. Called by
// the host (cmd/run.go) after tea.NewProgram returns.
func (m *Model) SetProgram(p *tea.Program) {
	m.program = p
}

// Options bundles construction parameters for New. Optional fields may
// be left zero.
type Options struct {
	Cancel    context.CancelFunc
	Gate      *Gate
	SpecPath  string
	Branch    string
	SessionID string
	MaxIter   int
	GitProbe  GitProbe
	GitCtx    context.Context

	// Services, when non-nil, is the application services handle from
	// which the TUI obtains its event stream via
	// Services.Events.Subscribe. The TUI subscribes directly to the
	// shared services instance and derives its newEvents factory from
	// the same handle.
	Services *services.Services

	// EscalationGate is the channel the Model writes into when the user
	// resolves a Director escalation modal. The host wires the same
	// channel into the loop's DirectorPorts.Escalation. nil disables
	// the modal's R/S/A actions silently (useful in tests).
	EscalationGate chan<- loop.EscalationReply

	// PlanningPending, when true, tells the Model that the planner is
	// still in flight. The director panel renders a "planning..."
	// placeholder until the host signals plan resolution via
	// SignalPlanReady (or one of the failure-path signals).
	PlanningPending bool

	// WebUIURL is the dashboard URL the [w] keybinding launches the
	// browser at. Empty disables the binding silently (help overlay
	// hides it; handler is a no-op).
	WebUIURL string

	// OpenBrowser is the platform-aware browser launcher the host wires
	// in. Nil disables the [w] binding silently. The Model swallows
	// errors via slog so the alt-screen renderer stays clean.
	OpenBrowser func(url string) error
}

// New returns a Model wired to the given services and cancel callback.
// The gate is required; nil disables the pause feature silently.
// GitProbe is optional: nil disables the commit-count and dirty-file
// probes (panels still render with empty placeholders).
func New(opts Options) Model {
	now := time.Now()
	m := Model{
		cancel:   opts.Cancel,
		gate:     opts.Gate,
		gitProbe: opts.GitProbe,
		gitCtx:   opts.GitCtx,
		svc:      opts.Services,
		header: header{
			specPath:  opts.SpecPath,
			branch:    opts.Branch,
			maxIter:   opts.MaxIter,
			sessionID: opts.SessionID,
			startedAt: now,
		},
		health:          healthPanel{startedAt: now},
		progress:        progressPanel{bar: newProgressBar()},
		actions:         newActionsPanel(),
		now:             newNowPanel(),
		keys:            defaultKeyMap(),
		sessionKeys:     defaultSessionKeyMap(),
		directorKeys:    defaultDirectorKeyMap(),
		helpView:        help.New(),
		escalationGate:  opts.EscalationGate,
		planningPending: opts.PlanningPending,
		webuiURL:        opts.WebUIURL,
		openBrowser:     opts.OpenBrowser,
	}
	// Disable the [w] binding (and hide it from help) when the host did
	// not wire a URL or a launcher. The handler is no-op anyway, but
	// disabling the binding keeps the help overlay honest.
	if m.webuiURL == "" || m.openBrowser == nil {
		m.keys.Webui.SetEnabled(false)
	}
	if m.gitCtx == nil {
		m.gitCtx = context.Background()
	}
	if opts.Services != nil {
		svc := opts.Services
		sid := opts.SessionID
		ctx := m.gitCtx
		m.events = serviceEventsChan(ctx, svc, sid, 0)
		m.newEvents = func() <-chan loop.Event {
			return serviceEventsChan(context.Background(), svc, sid, 0)
		}
	}
	return m
}

// Init starts the event-pump cmd plus the periodic ticks (git probe).
// The spinner is animated on demand: it only ticks while the agent has
// an in-flight tool call or while the planner is still in flight, so
// an idle dashboard does not repaint at 10 FPS for no visible change.
// KindToolUse arms the spinner from inside Update; here we arm it once
// when the run boots in planning mode so the placeholder animates.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{}
	if m.events != nil {
		cmds = append(cmds, readEventCmd(m.events))
	}
	if m.planningPending {
		cmds = append(cmds, m.now.spinner.Tick)
	}
	if m.gitProbe != nil {
		cmds = append(cmds, gitProbeCmd(m.gitCtx, m.gitProbe, m.runBaselineSHA))
	}
	return tea.Batch(cmds...)
}

// Update handles every incoming message: loop events, key presses,
// resize, and the periodic ticks (spinner, git probe, spec re-parse).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout = computeLayout(m.width)
		m.actions.SetSize(m.layout.actionsW, actionsViewportHeight)
		m.progress.bar.SetWidth(progressBarWidth)
		m.helpView.SetWidth(m.width)
		return m, nil

	case tea.KeyPressMsg:
		if m.nothingToDoMode {
			return m.handleNothingToDoKey(msg)
		}
		if m.director.escalation {
			return m.handleEscalationKey(msg)
		}
		if m.sessionMode {
			return m.handleSessionKey(msg)
		}
		return m.handleKey(msg)

	case eventMsg:
		if msg.closed {
			m.finished = true
			// In session mode (loop reached a recoverable terminal state)
			// or nothing-to-do mode (planner said the spec is done) the
			// dashboard stays alive until the user resolves the screen.
			if m.sessionMode || m.nothingToDoMode {
				return m, nil
			}
			return m, tea.Quit
		}
		return m.handleLoopEvent(msg.ev)

	case planSkippedMsg:
		m.nothingToDoMode = true
		m.nothingToDoReason = msg.reason
		m.planningPending = false
		return m, nil

	case planFailedMsg:
		// Surface the planner error inside the session footer so the
		// user sees why the run failed before deciding whether to
		// retry / edit / quit. The session menu itself is latched by
		// the LoopFinished{Reason: "planner_failed"} the host emits
		// alongside this message.
		m.sessionExitMsg = "planner: " + msg.message
		m.planningPending = false
		return m, nil

	case planReadyMsg:
		// Planner returned a plan; latch it onto the director panel
		// (mirrors what PhasePlanned will do once the loop starts) and
		// drop the planning placeholder so the dashboard reverts to
		// its normal rendering until the loop kicks off.
		if msg.plan != nil {
			m.director.onPhasePlanned(msg.plan)
		}
		m.planningPending = false
		return m, nil

	case restartLoopMsg:
		// User pressed [r] in the session menu: ask the host's factory
		// for a freshly built loop and rebind the bridge channel.
		if m.newEvents == nil {
			return m, nil
		}
		ch := m.newEvents()
		return m, func() tea.Msg { return rebindEventsMsg{events: ch} }

	case rebindEventsMsg:
		m.events = msg.events
		m.sessionMode = false
		m.sessionReason = ""
		m.sessionExitMsg = ""
		m.lastIterSignal = agentcontract.SignalUnknown
		m.finished = false
		// Iteration counter resets per session; run-local baseline SHA
		// is preserved (the run, not the iteration, is the baseline).
		m.header.iter = 0
		m.now.onIterStarted()
		return m, readEventCmd(m.events)

	case editorFinishedMsg:
		if msg.err != nil {
			m.sessionExitMsg = "editor: " + msg.err.Error()
		} else {
			m.sessionExitMsg = ""
		}
		// Re-arm the spinner only if there is something to animate: a
		// tool may still be in flight after the user returned from the
		// editor. Idle session mode does not need a tick.
		if m.now.currentTool != nil && !m.finished {
			return m, m.now.spinner.Tick
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.now.spinner, cmd = m.now.spinner.Update(msg)
		// Stop the tick loop when there is nothing to animate: the run
		// finished, no tool is in flight, and the planner is no longer
		// pending. The spinner re-arms on the next KindToolUse (or on
		// the next planner-driven boot) so the panel resumes animating
		// without polling between iterations.
		if m.finished || (m.now.currentTool == nil && !m.planningPending) {
			return m, nil
		}
		return m, cmd

	case gitProbeMsg:
		if msg.dirtyKnown {
			m.risk.onDirtyFileCount(msg.dirtyCount)
		}
		if msg.commitsKnown {
			m.risk.onCommitCount(msg.commitCount)
		}
		if m.finished || m.gitProbe == nil {
			return m, nil
		}
		return m, gitProbeCmd(m.gitCtx, m.gitProbe, m.runBaselineSHA)

	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		m.actions.viewport, cmd = m.actions.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

// handleKey implements the keyboard contract: q / Ctrl+C cancel the
// loop ctx and ask bubbletea to quit; space toggles the pause gate;
// ? toggles the help overlay. Bindings live in keyMap (single source
// of truth shared with the help renderer).
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		if !m.cancelled && m.cancel != nil {
			m.cancel()
		}
		m.cancelled = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Pause):
		if m.gate != nil {
			m.paused = !m.paused
			m.gate.SetPaused(m.paused)
			m.header.paused = m.paused
		}
		return m, nil
	case key.Matches(msg, m.keys.Mouse):
		m.mouseCaptureOff = !m.mouseCaptureOff
		return m, nil
	case key.Matches(msg, m.keys.Webui):
		return m, m.openWebuiCmd()
	case key.Matches(msg, m.keys.Help):
		m.helpVisible = !m.helpVisible
		return m, nil
	}
	return m, nil
}

// openWebuiCmd returns a tea.Cmd that launches the dashboard in the
// user's default browser. The command runs the launcher in its own
// goroutine (tea drains commands serially in a worker pool) and
// swallows the error via slog so a failed launch does not corrupt the
// alt-screen renderer. Nil URL or launcher returns nil so the caller
// can treat this as a no-op binding.
func (m Model) openWebuiCmd() tea.Cmd {
	if m.webuiURL == "" || m.openBrowser == nil {
		return nil
	}
	url := m.webuiURL
	open := m.openBrowser
	return func() tea.Msg {
		if err := open(url); err != nil {
			slog.Warn("tui: open webui", "url", url, "err", err)
		}
		return nil
	}
}

// handleNothingToDoKey routes input while the nothing-to-do terminal
// screen is up. Only quit bindings are honoured; everything else is a
// no-op so the user cannot accidentally trigger pause/help/etc on what
// is effectively a final screen.
func (m Model) handleNothingToDoKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}
	return m, nil
}

// handleSessionKey implements the session-mode keyboard contract: [r]
// dispatches restartLoopMsg (the host's factory builds a fresh loop and
// returns a new events channel via rebindEventsMsg); [e] suspends the
// terminal and runs $EDITOR on the spec; [q] / Ctrl+C exit immediately
// (the loop is already done, nothing to cancel). The `?` overlay still
// works: pressing `?` flips helpVisible, the View routes through the
// existing helpKeyMap renderer, and pressing `?` again returns to the
// frozen dashboard plus session menu. The [w] binding stays live in
// session mode so the user can inspect the dashboard while the run
// idles.
func (m Model) handleSessionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.sessionKeys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.sessionKeys.Resume):
		if m.newEvents == nil {
			return m, nil
		}
		return m, func() tea.Msg { return restartLoopMsg{} }
	case key.Matches(msg, m.sessionKeys.Edit):
		if m.program == nil {
			m.sessionExitMsg = "editor: tea.Program reference missing"
			return m, nil
		}
		return m, editSpecCmd(m.program, m.header.specPath)
	case key.Matches(msg, m.keys.Webui):
		return m, m.openWebuiCmd()
	case key.Matches(msg, m.keys.Help):
		m.helpVisible = !m.helpVisible
		return m, nil
	}
	return m, nil
}

// handleLoopEvent routes one event to the relevant panels and
// schedules the next read.
func (m Model) handleLoopEvent(ev loop.Event) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{readEventCmd(m.events)}

	switch e := ev.(type) {
	case loop.IterationStarted:
		m.header.onIter(e.Index, e.MaxIter)
		m.now.onIterStarted()
		m.progress.onIterStarted()
		m.risk.onIterStarted(e.Index)
		m.header.onAny(e.At)
		m.health.onAny(e.At)
		m.health.onIterStarted()
		// Treat the first iteration's BaselineSHA as the run-local baseline
		// so the risk panel can show how many commits the run has produced
		// (computed via GitProbe.CommitsSince in the next tick). Subsequent
		// IterationStarted events keep their own per-iter baseline; only
		// the first is preserved here.
		if m.runBaselineSHA == "" && e.BaselineSHA != "" {
			m.runBaselineSHA = e.BaselineSHA
			// Fire an immediate probe so the commit count appears on the
			// next render rather than after the next 2s tick.
			if m.gitProbe != nil {
				cmds = append(cmds, gitProbeNowCmd(m.gitCtx, m.gitProbe, m.runBaselineSHA))
			}
		}
	case loop.IterationFinished:
		m.progress.onIterationFinished(time.Duration(e.DurationMS) * time.Millisecond)
		m.risk.onIterFinished(e.Signal)
		m.lastIterSignal = e.Signal
		m.header.onAny(e.At)
		m.health.onAny(e.At)
		// Release the gate (no-op if paused).
		if m.gate != nil {
			m.gate.IterDone()
		}
	case loop.LoopFinished:
		m.finished = true
		m.header.onAny(e.At)
		m.health.onAny(e.At)
		// In TUI mode, the loop's terminal Result is a signal that the
		// human is needed, not a process exit. Latch session mode so
		// the dashboard freezes with a menu (P2.11). Two reasons bypass
		// the menu and let the program quit on the next channel close:
		//   - "user cancelled": Ctrl+C during run; the user already chose
		//     to leave, no menu;
		//   - "fatal": invalid config / executor missing; surface the
		//     error and exit.
		switch e.Reason {
		case "user cancelled", "fatal":
			// fall through to channel-close → tea.Quit
		default:
			m.sessionMode = true
			m.sessionReason = e.Reason
		}
		// Drain remaining messages until the channel closes; tea.Quit
		// via the next eventMsg{closed: true} from readEventCmd unless
		// sessionMode latched.
	case loop.AgentEventReceived:
		toolBefore := m.now.currentTool
		m.now.onAgentEvent(e.Event)
		m.health.onAgentEvent(e.Event)
		m.actions.onAgentEvent(e.Event)
		m.risk.onAgentEvent(e.Event)
		m.header.onAny(e.Event.At)
		if e.Event.Kind == agentcontract.KindResultSummary && e.Event.Done != nil {
			m.director.onCost(e.Event.Done.TotalCostUSD)
		}
		// Arm the spinner the first moment a tool becomes active. Idle
		// transitions (tool finished) leave the tick loop to wind down
		// naturally on the next spinner.TickMsg.
		if toolBefore == nil && m.now.currentTool != nil && !m.finished {
			cmds = append(cmds, m.now.spinner.Tick)
		}
	case loop.PhasePlanned:
		m.director.onPhasePlanned(e.Plan)
		m.header.onAny(e.At)
	case loop.PhaseBriefed:
		m.director.onPhaseBriefed(e.PhaseID, e.Iteration, e.Briefing, phaseCapability{
			BrieferModel:   e.BrieferModel,
			BrieferEffort:  e.BrieferEffort,
			ExecutorModel:  e.ExecutorModel,
			ExecutorEffort: e.ExecutorEffort,
			ReviewerModel:  e.ReviewerModel,
			ReviewerEffort: e.ReviewerEffort,
			BrieferSkipped: e.BrieferSkipped,
			ReviewSkipped:  e.ReviewSkipped,
		})
		m.header.onAny(e.At)
	case loop.PhaseReviewed:
		m.director.onPhaseReviewed(e.PhaseID, e.Attempt, e.Outcome)
		m.header.onAny(e.At)
	case loop.DirectorEscalation:
		m.director.onEscalation(e.PhaseID, e.Attempt, e.Reasoning)
		m.header.onAny(e.At)
	case loop.TaskStarted:
		m.director.onTaskStarted(e.TaskID)
		if !isPseudoTaskID(e.TaskID) {
			m.progress.onTaskStarted()
			m.risk.onTaskStarted()
		}
		m.header.onAny(e.At)
	case loop.TaskCompleted:
		m.director.onTaskCompleted(e.TaskID)
		if !isPseudoTaskID(e.TaskID) {
			m.progress.onTaskCompleted()
			m.risk.onTaskCompleted()
		}
		m.header.onAny(e.At)
	}
	return m, tea.Batch(cmds...)
}

// View renders the dashboard. Terminal states bypass panel rendering
// and emit a one-line summary. The help overlay (toggled by `?`)
// replaces the panels with a keybinding listing.
//
// Layout per the dashboard mockup: header (full width), then a
// two-column row pairing now (wider) with health (narrower), then
// progress, risk, and recent actions each as full-width rows, then
// the keybinding footer below.
//
// AltScreen and MouseMode live on the View struct in v2: the program
// enters alt-screen mode and starts capturing mouse cell motion as
// soon as the first View arrives, no ProgramOption needed.
func (m Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	if m.mouseCaptureOff {
		v.MouseMode = tea.MouseModeNone
	} else {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// viewContent composes the dashboard body so View() can wrap it in a
// tea.View. Kept separate so tests that read the rendered string can
// call m.View().Content directly without losing the AltScreen / mouse
// metadata.
func (m Model) viewContent() string {
	// Nothing-to-do takes precedence: when the planner skipped, the
	// dashboard is replaced with a friendly terminal screen so the user
	// has nothing to act on except quit. This must come before session
	// mode and the bare finished branches.
	if m.nothingToDoMode {
		return m.viewNothingToDo()
	}
	// Session mode freezes the dashboard with a menu of next steps; it
	// must take precedence over the bare-string finished branches below
	// (which only fire for "user cancelled" / "fatal" reasons that bypass
	// session mode by design).
	if m.sessionMode {
		return m.viewSession()
	}
	if m.finished && m.cancelled {
		return "bcc: cancelled\n"
	}
	if m.finished {
		return "bcc: loop finished\n"
	}

	if m.helpVisible {
		return renderHelpOverlay(m.helpView, m.keys)
	}

	return m.viewDashboard(false)
}

// viewNothingToDo renders the friendly terminal screen latched when the
// Planner declared the spec done via plan_skip. The user has no
// remediation to perform, so the screen exposes only a quit
// instruction. The reason text the planner attached is shown verbatim
// so the user can see why the run was a no-op.
func (m Model) viewNothingToDo() string {
	var b lipgloss.Style = theme.title
	title := b.Render("[ bcc: nothing to do ]")
	reason := m.nothingToDoReason
	if reason == "" {
		reason = "the planner reported the spec is already complete"
	}
	body := "\n" + title + "\n\n"
	body += "  " + theme.ok.Render(reason) + "\n\n"
	body += "  " + theme.subtle.Render("press [q] or Ctrl+C to exit") + "\n"
	return body
}

// viewSession renders the dashboard frozen behind a session menu. The
// header switches from the alive-dot badge to the idle-state badge per
// P2.11.7. The `?` overlay still toggles the help screen on top of the
// session frame; pressing `?` again returns to the menu.
func (m Model) viewSession() string {
	if m.helpVisible {
		return renderHelpOverlay(m.helpView, m.sessionKeys)
	}
	dashboard := m.viewDashboard(true)
	status := sessionStatus(m.sessionReason, m.lastIterSignal)
	explanation := sessionExplanation(m.sessionReason)
	menu := renderSessionMenu(m.helpView, m.sessionKeys, status, explanation)
	hint := ""
	if m.sessionExitMsg != "" {
		hint = "  " + theme.warn.Render(m.sessionExitMsg) + "\n"
	}
	return dashboard + menu + hint
}

// viewDashboard composes the five-panel dashboard body. session toggles
// the header's alive dot for the session-state badge.
func (m Model) viewDashboard(session bool) string {
	lay := m.activeLayout()
	now := time.Now()

	headerView := m.header.view(now, lay.headerW)
	if session {
		headerView = m.header.viewSession(sessionStatus(m.sessionReason, m.lastIterSignal))
	}
	headerBox := box(
		m.header.titleText(now),
		headerView,
		lay.headerW,
	)
	nowBox := box("now", m.now.view(now, lay.nowW), lay.nowW)
	healthBody := m.health.view(now, lay.healthW)
	if m.director.active() {
		healthBody += fmt.Sprintf("  director cost: $%.2f\n", m.director.cumulativeCost)
	}
	healthBox := box("health", healthBody, lay.healthW)
	nowHealth := lipgloss.JoinHorizontal(lipgloss.Top, nowBox, healthBox)
	progressBox := box("progress", m.progress.view(lay.progressW), lay.progressW)
	riskBox := box("if you close now", m.risk.view(now, lay.riskW), lay.riskW)
	actionsBox := box("recent actions", m.actions.view(lay.actionsW), lay.actionsW)
	footer := m.helpView.View(m.keys)
	if session {
		footer = m.helpView.View(m.sessionKeys)
	}

	rows := []string{headerBox, nowHealth, progressBox}
	if m.director.active() || m.planningPending {
		rows = append(rows, box("director", m.director.viewWithPlanning(lay.headerW, m.planningPending, m.now.spinner.View(), m.health.iterTokens, m.health.totalCostUSD), lay.headerW))
	}
	rows = append(rows, riskBox, actionsBox, footer)
	dashboard := lipgloss.JoinVertical(lipgloss.Left, rows...) + "\n"

	if m.director.escalation {
		modal := renderEscalationModal(m.director, m.directorKeys)
		dashboard += "\n" + modal
	}
	return dashboard
}

// activeLayout returns the cached layout when a tea.WindowSizeMsg has
// been processed, otherwise a default 80-column layout. The default
// keeps tests that don't send a size message rendering something
// reasonable instead of zero-width content.
func (m Model) activeLayout() layout {
	if m.layout.headerW > 0 {
		return m.layout
	}
	return computeLayout(80)
}

// --- internal tea.Msg / tea.Cmd plumbing -----------------------------

// gitProbeMsg carries the periodic git-state snapshot the risk panel
// consumes: dirty-file count and the run-local commit count (number of
// commits HEAD is ahead of the run's baseline SHA). Either piece may be
// missing on a given tick (the baseline is unknown until the first
// IterationStarted, and individual git calls may fail).
type gitProbeMsg struct {
	dirtyCount   int
	dirtyKnown   bool
	commitCount  int
	commitsKnown bool
}

func doGitProbe(ctx context.Context, probe GitProbe, baselineSHA string) gitProbeMsg {
	var msg gitProbeMsg
	if n, err := probe.DirtyFileCount(ctx); err == nil {
		msg.dirtyCount = n
		msg.dirtyKnown = true
	}
	if baselineSHA != "" {
		if n, err := probe.CommitsSince(ctx, baselineSHA); err == nil {
			msg.commitCount = n
			msg.commitsKnown = true
		}
	}
	return msg
}

func gitProbeCmd(ctx context.Context, probe GitProbe, baselineSHA string) tea.Cmd {
	return tea.Tick(gitProbeInterval, func(time.Time) tea.Msg {
		return doGitProbe(ctx, probe, baselineSHA)
	})
}

// gitProbeNowCmd performs a git probe immediately (no tick delay). Used
// when the run-local baseline SHA is first known so the commit count
// appears on the next render rather than after the next 2s tick.
func gitProbeNowCmd(ctx context.Context, probe GitProbe, baselineSHA string) tea.Cmd {
	return func() tea.Msg {
		return doGitProbe(ctx, probe, baselineSHA)
	}
}

// serviceEventsChan subscribes to sessionID via svc.Events and returns a
// <-chan loop.Event that emits each event in the SeqEvent. The goroutine
// exits when ctx is cancelled or when the subscription channel closes (on
// LoopFinished or fan-out shutdown). The returned channel is closed when
// the goroutine exits so the caller's readEventCmd sees the terminal state.
func serviceEventsChan(ctx context.Context, svc *services.Services, sessionID string, fromSeq int64) <-chan loop.Event {
	out := make(chan loop.Event, 256)
	sub, err := svc.Events.Subscribe(ctx, sessionID, fromSeq)
	if err != nil {
		close(out)
		return out
	}
	go func() {
		defer close(out)
		for {
			select {
			case se, ok := <-sub:
				if !ok {
					return
				}
				select {
				case out <- se.Event:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
