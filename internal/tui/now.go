package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// nowSpinnerStyle is the spinner frame set the now panel uses. Dot is
// the visually closest match to the manual ⠋⠙⠹⠸... ring P2.10 replaces.
// Single named constant so future tuning (Line, MiniDot, Pulse) is one
// edit.
var nowSpinnerStyle = spinner.Dot

// nowPanel surfaces what the agent is doing at this instant: the latest
// in-flight tool call, its elapsed time, and the most recent assistant
// text. The active tool is cleared when its matching tool_result
// arrives, so a stalled tool keeps showing while real work hangs.
type nowPanel struct {
	currentTool   *loop.ToolCallInfo
	currentToolAt time.Time
	lastAssistant string
	spinner       spinner.Model
}

// newNowPanel builds a nowPanel with the bubbles/v2 spinner pre-styled
// with the panel's "ok" colour so an active tool reads as "alive".
func newNowPanel() nowPanel {
	s := spinner.New(spinner.WithSpinner(nowSpinnerStyle))
	s.Style = theme.ok
	return nowPanel{spinner: s}
}

// onAgentEvent folds a single agent event into the panel state.
func (n *nowPanel) onAgentEvent(ev loop.AgentEvent) {
	switch ev.Kind {
	case loop.KindToolUse:
		if ev.Tool != nil {
			tool := *ev.Tool
			n.currentTool = &tool
			n.currentToolAt = ev.At
			if n.currentToolAt.IsZero() {
				n.currentToolAt = time.Now()
			}
		}
	case loop.KindToolResult:
		if ev.Tool != nil && n.currentTool != nil && n.currentTool.ID == ev.Tool.ID {
			n.currentTool = nil
		}
	case loop.KindAssistantText:
		text := strings.TrimSpace(ev.Text)
		if text != "" {
			n.lastAssistant = text
		}
	}
}

// onIterStarted clears the per-iteration buffer; what was running
// last iteration is no longer "now".
func (n *nowPanel) onIterStarted() {
	n.currentTool = nil
	n.currentToolAt = time.Time{}
	// keep lastAssistant: between iterations the prior reasoning is
	// still the most recent thing the agent said.
}

// view renders the panel body. width is the total column the box
// wrapper will allocate; long content (assistant text especially) is
// truncated to fit so the rendered lines never overflow the box.
func (n nowPanel) view(now time.Time, width int) string {
	max := bodyMax(width)
	var b strings.Builder

	if n.currentTool == nil {
		b.WriteString("  ")
		b.WriteString(theme.subtle.Render("idle"))
		b.WriteByte('\n')
	} else {
		b.WriteString("  ")
		b.WriteString(n.spinner.View())
		b.WriteByte(' ')
		headline := formatToolHeadline(*n.currentTool)
		if room := max - 4; room > 0 { // 2 indent + spinner + space
			headline = truncate(headline, room)
		}
		b.WriteString(headline)
		if !n.currentToolAt.IsZero() {
			elapsed := formatDuration(now.Sub(n.currentToolAt))
			b.WriteString("  ")
			b.WriteString(theme.subtle.Render("(" + elapsed + " in)"))
		}
		b.WriteByte('\n')
	}

	if n.lastAssistant != "" {
		room := max - 4 // 2 indent + "» "
		if room <= 0 {
			room = 1
		}
		b.WriteString("  ")
		b.WriteString(theme.subtle.Render("» " + truncate(n.lastAssistant, room)))
		b.WriteByte('\n')
	}
	return b.String()
}

// bodyMax returns the maximum visible width a single body line may
// occupy inside the box wrapper. width is the box's total width
// (borders included). Below the bordered threshold the body uses the
// full width; otherwise the two border columns are subtracted.
func bodyMax(width int) int {
	if width <= 0 {
		return 0
	}
	if width < boxThreshold {
		return width
	}
	return width - 2
}

// formatToolHeadline renders a one-line label for a tool call. The
// representation is tool-specific so the user reads "Edit foo.go"
// instead of a generic "Edit (id=toolu_01)".
func formatToolHeadline(t loop.ToolCallInfo) string {
	switch t.Name {
	case "Bash":
		return "Bash  " + truncate(stringArg(t.Args, "command"), 60)
	case "Edit", "Write":
		return t.Name + "  " + stringArg(t.Args, "file_path")
	case "Read":
		path := stringArg(t.Args, "file_path")
		if path == "" {
			path = stringArg(t.Args, "path")
		}
		return "Read  " + path
	case "Glob":
		return "Glob  " + stringArg(t.Args, "pattern")
	case "Grep":
		return "Grep  " + stringArg(t.Args, "pattern")
	default:
		hint := primaryArg(t.Args)
		if hint == "" {
			return t.Name
		}
		return fmt.Sprintf("%s  %s", t.Name, truncate(hint, 60))
	}
}

// stringArg pulls a string field from the tool args map. Returns
// empty string when the key is missing or the value is non-string.
func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// primaryArg picks the most user-relevant string field from an
// unknown tool. Used as a fallback for tools we have not specialised.
func primaryArg(args map[string]any) string {
	for _, k := range []string{"file_path", "path", "command", "pattern", "url", "query"} {
		if v, ok := args[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// truncate returns s with a … suffix when it exceeds max runes. Plain
// rune count keeps the math friendly to non-ASCII text.
func truncate(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
