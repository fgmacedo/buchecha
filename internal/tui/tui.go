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
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/spec"
)

// gitProbeInterval is the cadence at which the dashboard polls the
// working tree for dirty-file count. 2s matches the spec's "every 2s"
// data-source contract.
const gitProbeInterval = 2 * time.Second

// spinnerInterval is the per-frame tick for the now-panel spinner.
const spinnerInterval = 100 * time.Millisecond

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
	paused         bool
	helpVisible    bool   // true while the ? overlay is up
	runBaselineSHA string // captured from the first IterationStarted

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
		},
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
	cmds = append(cmds, spinnerTickCmd())
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
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case eventMsg:
		if msg.closed {
			m.finished = true
			return m, tea.Quit
		}
		return m.handleLoopEvent(msg.ev)

	case spinnerTickMsg:
		m.now.tick()
		if m.finished {
			return m, nil
		}
		return m, spinnerTickCmd()

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
	}

	return m, nil
}

// handleKey implements the keyboard contract: q / Ctrl+C cancel the
// loop ctx and ask bubbletea to quit; space toggles the pause gate;
// ? toggles the help overlay.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if !m.cancelled && m.cancel != nil {
			m.cancel()
		}
		m.cancelled = true
		return m, tea.Quit
	case " ", "space":
		if m.gate != nil {
			m.paused = !m.paused
			m.gate.SetPaused(m.paused)
			m.header.paused = m.paused
		}
		return m, nil
	case "?":
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
		// Treat the first iteration's BaselineSHA as the run-local baseline
		// so the risk panel can show how many commits the run has produced
		// (computed via GitProbe.CommitsSince in the next tick). Subsequent
		// IterationStarted events keep their own per-iter baseline; only
		// the first is preserved here.
		if m.runBaselineSHA == "" && e.BaselineSHA != "" {
			m.runBaselineSHA = e.BaselineSHA
		}
	case loop.IterationFinished:
		m.progress.onIterationFinished(time.Duration(e.DurationMS) * time.Millisecond)
		m.header.onAny(e.At)
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
func (m Model) View() string {
	if m.finished && m.cancelled {
		return "bcc: cancelled\n"
	}
	if m.finished {
		return "bcc: loop finished\n"
	}

	if m.helpVisible {
		return renderHelp()
	}

	now := time.Now()
	var b strings.Builder
	b.WriteString(m.header.view(now))
	b.WriteByte('\n')
	b.WriteString(m.now.view(now))
	b.WriteString(m.health.view(now))
	b.WriteString(m.progress.view())
	b.WriteString(m.risk.view(now))
	b.WriteString(m.actions.view())
	b.WriteString("[q]uit  [space]pause  [?]help\n")
	return b.String()
}

// --- internal tea.Msg / tea.Cmd plumbing -----------------------------

// spinnerTickMsg fires every spinnerInterval to advance the now-panel
// spinner frame.
type spinnerTickMsg struct{}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

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

func gitProbeCmd(ctx context.Context, probe GitProbe, baselineSHA string) tea.Cmd {
	return tea.Tick(gitProbeInterval, func(time.Time) tea.Msg {
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
	})
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
