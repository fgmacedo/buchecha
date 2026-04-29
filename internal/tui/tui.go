// Package tui implements the bubbletea-driven dashboard rendered when
// `bcc run --output tui` is selected.
//
// Phase 2.4 ships the skeleton: a Model with five empty placeholder
// panels, a tea.Cmd bridge that pumps loop events into Update, and the
// keyboard contract (q / Ctrl+C cancel; space toggles a pause gate
// shared with the loop). Subsequent phases (P2.5+) populate panel
// bodies, themes, and heuristics.
package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// Model is the bubbletea model for the dashboard.
type Model struct {
	// Inputs wired by the caller (cmd/run.go).
	events <-chan loop.Event
	cancel context.CancelFunc
	gate   *Gate

	// Static header context.
	specPath string
	branch   string
	maxIter  int

	// Live state mutated by Update.
	width, height int
	iter          int
	lastEvent     loop.Event
	paused        bool

	// Terminal state.
	finished  bool // true once the loop emitted LoopFinished or events closed
	cancelled bool // true once user pressed q or Ctrl+C
}

// Options bundles construction parameters for New. Optional fields may
// be left zero.
type Options struct {
	Events   <-chan loop.Event
	Cancel   context.CancelFunc
	Gate     *Gate
	SpecPath string
	Branch   string
	MaxIter  int
}

// New returns a Model wired to the given event channel and cancel
// callback. The gate is required; nil disables the pause feature
// silently.
func New(opts Options) Model {
	return Model{
		events:   opts.Events,
		cancel:   opts.Cancel,
		gate:     opts.Gate,
		specPath: opts.SpecPath,
		branch:   opts.Branch,
		maxIter:  opts.MaxIter,
	}
}

// Init starts the event-pump cmd. The bubbletea program runs the
// returned cmd as soon as the program enters its event loop.
func (m Model) Init() tea.Cmd {
	if m.events == nil {
		return nil
	}
	return readEventCmd(m.events)
}

// Update handles every incoming message. The set of messages this
// skeleton recognises is minimal:
//
//   - eventMsg: a loop.Event arrived from the bridge.
//   - tea.KeyMsg: q / Ctrl+C cancel the loop and quit; space toggles
//     pause; other keys are ignored.
//   - tea.WindowSizeMsg: track terminal size for View.
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
	}

	return m, nil
}

// handleKey implements the keyboard contract: q / Ctrl+C cancel the
// loop ctx and ask bubbletea to quit; space toggles the pause gate.
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
		}
		return m, nil
	}
	return m, nil
}

// handleLoopEvent updates panel state from a single loop event and
// schedules the next read. Iteration boundaries are folded into the
// header; the event itself is stored for the now/actions panels (the
// real rendering arrives in P2.5).
func (m Model) handleLoopEvent(ev loop.Event) (tea.Model, tea.Cmd) {
	m.lastEvent = ev
	switch e := ev.(type) {
	case loop.IterationStarted:
		m.iter = e.Index
		if m.maxIter == 0 {
			m.maxIter = e.MaxIter
		}
	case loop.IterationFinished:
		// Release the gate (no-op if paused).
		if m.gate != nil {
			m.gate.IterDone()
		}
	case loop.LoopFinished:
		m.finished = true
		// Drain remaining messages until the channel closes; tea.Quit
		// via the next eventMsg{closed: true} from readEventCmd.
	}
	return m, readEventCmd(m.events)
}

// View renders the dashboard. The skeleton draws empty placeholder
// panels with their titles so the layout is verifiable visually
// before P2.5 fills them.
func (m Model) View() string {
	if m.finished && m.cancelled {
		return "bcc: cancelled\n"
	}
	if m.finished {
		return "bcc: loop finished\n"
	}

	pauseTag := ""
	if m.paused {
		pauseTag = " [paused]"
	}
	header := fmt.Sprintf("bcc %s iter %d/%d%s", trimEmpty(m.branch), m.iter, m.maxIter, pauseTag)

	var body string
	body += panelTitle("now") + "\n"
	body += panelTitle("health") + "\n"
	body += panelTitle("progress") + "\n"
	body += panelTitle("if you close now") + "\n"
	body += panelTitle("recent actions") + "\n"

	footer := "[q]uit  [space]pause  [?]help"
	return header + "\n" + body + footer + "\n"
}

func panelTitle(name string) string {
	return "[ " + name + " ] (placeholder)"
}

func trimEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
