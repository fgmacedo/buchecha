package tui

import (
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
)

// actionsPanel renders a tail of the most recent tool calls, one line
// each, newest first. The user reads it like a shoulder-surf: at a
// glance, what has the agent been doing in the last few seconds?
//
// The 5-entry visible cap stays as a default, but the panel keeps a
// longer history (actionsHistoryCap) accessible via mouse wheel and
// arrow-key scrolling through the embedded viewport.
type actionsPanel struct {
	entries  []actionEntry
	viewport viewport.Model
	width    int
	height   int
}

const (
	actionsCap            = 5   // visible default rows
	actionsHistoryCap     = 200 // total entries kept for scrolling
	actionsViewportHeight = 5   // viewport rows = actionsCap; visible window
)

type actionEntry struct {
	at   time.Time
	tool agentcontract.ToolCallInfo
}

// newActionsPanel wires the viewport with mouse wheel scrolling enabled
// and a sensible default height. The Model resizes it on every
// tea.WindowSizeMsg via SetSize.
func newActionsPanel() actionsPanel {
	vp := viewport.New(
		viewport.WithWidth(80),
		viewport.WithHeight(actionsViewportHeight),
	)
	vp.MouseWheelEnabled = true
	return actionsPanel{
		viewport: vp,
		width:    80,
		height:   actionsViewportHeight,
	}
}

// SetSize repoints the viewport at the latest panel dimensions. Called
// from Model.Update on tea.WindowSizeMsg.
func (a *actionsPanel) SetSize(width, height int) {
	if width > 0 {
		a.width = width
		a.viewport.SetWidth(width - 2) // borders
	}
	if height > 0 {
		a.height = height
		a.viewport.SetHeight(height)
	}
	a.refreshContent()
}

// onAgentEvent appends tool_use events to the ring (capped at
// actionsHistoryCap). Other event kinds are ignored. The rendered
// content is newest-first, so after each push the viewport snaps to the
// top so the freshest action is visible without a scroll; older entries
// are reachable by scrolling down (mouse wheel or arrow keys).
func (a *actionsPanel) onAgentEvent(ev agentcontract.AgentEvent) {
	if ev.Kind != agentcontract.KindToolUse || ev.Tool == nil {
		return
	}
	at := ev.At
	if at.IsZero() {
		at = time.Now()
	}
	a.entries = append(a.entries, actionEntry{at: at, tool: *ev.Tool})
	if len(a.entries) > actionsHistoryCap {
		a.entries = a.entries[len(a.entries)-actionsHistoryCap:]
	}
	a.refreshContent()
	a.viewport.GotoTop()
}

// refreshContent re-renders the entries into the viewport buffer.
// Called whenever the entry slice or viewport size changes.
func (a *actionsPanel) refreshContent() {
	a.viewport.SetContent(a.formattedEntries())
}

// formattedEntries renders every kept entry, newest first. The viewport
// clips the rendered string to its visible window; the user can scroll
// past the 5-entry default to reach earlier entries.
func (a actionsPanel) formattedEntries() string {
	if len(a.entries) == 0 {
		return "  " + theme.subtle.Render("(no tool calls yet)")
	}
	var b strings.Builder
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := a.entries[i]
		b.WriteString("  ")
		b.WriteString(theme.subtle.Render(e.at.Format("15:04:05")))
		b.WriteString("  ")
		b.WriteString(formatToolHeadline(e.tool))
		if i > 0 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// view renders the panel body via the viewport. width is the box
// allocation; the viewport already knows its size from the latest
// SetSize call.
func (a actionsPanel) view(_ int) string {
	return a.viewport.View() + "\n"
}
