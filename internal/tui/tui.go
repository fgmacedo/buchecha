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
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/spec"
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
	Help  key.Binding
}

// ShortHelp returns the bindings rendered as a single inline footer line
// (the help.Model's ShortHelp mode).
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Pause, k.Help}
}

// FullHelp returns the bindings grouped by column for the ? overlay
// (help.Model's FullHelp mode). One column today; columns are added by
// returning more nested slices.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Quit, k.Pause, k.Help}}
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
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle this help overlay"),
		),
	}
}

// Model is the bubbletea model for the dashboard.
type Model struct {
	// Inputs wired by the caller (cmd/run.go).
	events     <-chan loop.Event
	cancel     context.CancelFunc
	gate       *Gate
	specReader SpecReader
	gitProbe   GitProbe
	gitCtx     context.Context
	specCfg    SpecConfig

	// Panels.
	header   header
	now      nowPanel
	health   healthPanel
	progress progressPanel
	risk     riskPanel
	actions  actionsPanel

	// Live state.
	width, height  int
	layout         layout // recomputed on tea.WindowSizeMsg
	paused         bool
	helpVisible    bool   // true while the ? overlay is up
	runBaselineSHA string // captured from the first IterationStarted

	// Keybindings + help renderer (single source of truth).
	keys     keyMap
	helpView help.Model

	// Terminal state.
	finished  bool // true once the loop emitted LoopFinished or events closed
	cancelled bool // true once user pressed q or Ctrl+C
}

// Options bundles construction parameters for New. Optional fields may
// be left zero.
type Options struct {
	Events     <-chan loop.Event
	Cancel     context.CancelFunc
	Gate       *Gate
	SpecPath   string
	Branch     string
	MaxIter    int
	SpecReader SpecReader
	GitProbe   GitProbe
	GitCtx     context.Context
	SpecConfig SpecConfig
}

// New returns a Model wired to the given event channel and cancel
// callback. The gate is required; nil disables the pause feature
// silently. SpecReader / GitProbe are optional: nil disables the
// corresponding panel data source (panels still render with empty
// placeholders).
func New(opts Options) Model {
	now := time.Now()
	m := Model{
		events:     opts.Events,
		cancel:     opts.Cancel,
		gate:       opts.Gate,
		specReader: opts.SpecReader,
		gitProbe:   opts.GitProbe,
		gitCtx:     opts.GitCtx,
		specCfg:    opts.SpecConfig,
		header: header{
			specPath:  opts.SpecPath,
			branch:    opts.Branch,
			maxIter:   opts.MaxIter,
			startedAt: now,
		},
		health: healthPanel{startedAt: now},
		progress: progressPanel{
			currentPhaseIdx: -1,
			bar:             newProgressBar(),
		},
		actions:  newActionsPanel(),
		now:      newNowPanel(),
		keys:     defaultKeyMap(),
		helpView: help.New(),
	}
	if m.gitCtx == nil {
		m.gitCtx = context.Background()
	}
	return m
}

// Init starts the event-pump cmd plus the periodic ticks (spinner,
// git probe). The bubbletea program runs them as soon as the program
// enters its event loop.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{}
	if m.events != nil {
		cmds = append(cmds, readEventCmd(m.events))
	}
	cmds = append(cmds, m.now.spinner.Tick)
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
		return m.handleKey(msg)

	case eventMsg:
		if msg.closed {
			m.finished = true
			return m, tea.Quit
		}
		return m.handleLoopEvent(msg.ev)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.now.spinner, cmd = m.now.spinner.Update(msg)
		if m.finished {
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

	case specParsedMsg:
		if msg.ok {
			m.progress.onSpecParsed(msg.plan)
			m.risk.onSpecParsed(msg.plan, msg.latest, msg.latestKnown)
		}
		return m, nil

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
		m.header.onAny(e.At)
		m.health.onAny(e.At)
		// Release the gate (no-op if paused).
		if m.gate != nil {
			m.gate.IterDone()
		}
		// Re-parse the spec asynchronously: progress + journal status.
		if m.specReader != nil {
			cmds = append(cmds, parseSpecCmd(m.specReader, m.header.specPath, m.specCfg))
		}
	case loop.LoopFinished:
		m.finished = true
		m.header.onAny(e.At)
		m.health.onAny(e.At)
		// Drain remaining messages until the channel closes; tea.Quit
		// via the next eventMsg{closed: true} from readEventCmd.
	case loop.AgentEventReceived:
		m.now.onAgentEvent(e.Event)
		m.health.onAgentEvent(e.Event)
		m.actions.onAgentEvent(e.Event)
		m.risk.onAgentEvent(e.Event)
		m.header.onAny(e.Event.At)
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
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// viewContent composes the dashboard body so View() can wrap it in a
// tea.View. Kept separate so tests that read the rendered string can
// call m.View().Content directly without losing the AltScreen / mouse
// metadata.
func (m Model) viewContent() string {
	if m.finished && m.cancelled {
		return "bcc: cancelled\n"
	}
	if m.finished {
		return "bcc: loop finished\n"
	}

	if m.helpVisible {
		return renderHelpOverlay(m.helpView, m.keys)
	}

	lay := m.activeLayout()
	now := time.Now()

	headerBox := box(
		m.header.titleText(now),
		m.header.view(now, lay.headerW),
		lay.headerW,
	)
	nowBox := box("now", m.now.view(now, lay.nowW), lay.nowW)
	healthBox := box("health", m.health.view(now, lay.healthW), lay.healthW)
	nowHealth := lipgloss.JoinHorizontal(lipgloss.Top, nowBox, healthBox)
	progressBox := box("progress", m.progress.view(lay.progressW), lay.progressW)
	riskBox := box("if you close now", m.risk.view(now, lay.riskW), lay.riskW)
	actionsBox := box("recent actions", m.actions.view(lay.actionsW), lay.actionsW)
	footer := m.helpView.View(m.keys)

	return lipgloss.JoinVertical(lipgloss.Left,
		headerBox,
		nowHealth,
		progressBox,
		riskBox,
		actionsBox,
		footer,
	) + "\n"
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

// specParsedMsg carries a freshly parsed plan and latest result.
type specParsedMsg struct {
	ok          bool
	plan        spec.Plan
	latest      spec.LatestResult
	latestKnown bool
}

// parseSpecCmd reads the spec file and parses the plan + latest
// journal Result. Errors silently downgrade to ok=false; the panels
// then keep their previous state instead of flashing partial output.
func parseSpecCmd(reader SpecReader, path string, cfg SpecConfig) tea.Cmd {
	return func() tea.Msg {
		content, err := reader.Read(path)
		if err != nil {
			return specParsedMsg{ok: false}
		}
		plan, err := spec.ParsePlan(content, cfg.PlanHeading)
		if err != nil {
			return specParsedMsg{ok: false}
		}
		latest, lerr := spec.ParseLatestResult(
			content, cfg.JournalHeading, cfg.ResultKeyword, cfg.ResultVocab)
		return specParsedMsg{
			ok:          true,
			plan:        plan,
			latest:      latest,
			latestKnown: lerr == nil,
		}
	}
}
