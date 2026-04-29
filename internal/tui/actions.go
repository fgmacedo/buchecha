package tui

import (
	"strings"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// actionsPanel renders a tail of the most recent tool calls, one line
// each, newest first. The user reads it like a shoulder-surf: at a
// glance, what has the agent been doing in the last few seconds?
type actionsPanel struct {
	entries []actionEntry
}

const actionsCap = 5

type actionEntry struct {
	at   time.Time
	tool loop.ToolCallInfo
}

// onAgentEvent appends tool_use events to the ring (capped at
// actionsCap). Other event kinds are ignored.
func (a *actionsPanel) onAgentEvent(ev loop.AgentEvent) {
	if ev.Kind != loop.KindToolUse || ev.Tool == nil {
		return
	}
	at := ev.At
	if at.IsZero() {
		at = time.Now()
	}
	a.entries = append(a.entries, actionEntry{at: at, tool: *ev.Tool})
	if len(a.entries) > actionsCap {
		a.entries = a.entries[len(a.entries)-actionsCap:]
	}
}

// view renders the panel body, newest first.
func (a actionsPanel) view() string {
	var b strings.Builder
	b.WriteString(panelTitle("recent actions"))
	b.WriteByte('\n')

	if len(a.entries) == 0 {
		b.WriteString("  ")
		b.WriteString(theme.subtle.Render("(no tool calls yet)"))
		b.WriteByte('\n')
		return b.String()
	}

	for i := len(a.entries) - 1; i >= 0; i-- {
		e := a.entries[i]
		b.WriteString("  ")
		b.WriteString(theme.subtle.Render(e.at.Format("15:04:05")))
		b.WriteString("  ")
		b.WriteString(formatToolHeadline(e.tool))
		b.WriteByte('\n')
	}
	return b.String()
}
