package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// nowPanel surfaces what the agent is doing at this instant: the latest
// in-flight tool call, its elapsed time, and the most recent assistant
// text. The active tool is cleared when its matching tool_result
// arrives, so a stalled tool keeps showing while real work hangs.
type nowPanel struct {
	currentTool   *loop.ToolCallInfo
	currentToolAt time.Time
	lastAssistant string
	spinnerFrame  int
}

// spinnerFrames is the static cycle the panel walks through on each
// spinnerTick. Eight frames is a smooth enough loop at 100ms/frame.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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

// tick advances the spinner frame. Called from a 100ms tea.Tick cmd.
func (n *nowPanel) tick() {
	n.spinnerFrame = (n.spinnerFrame + 1) % len(spinnerFrames)
}

// view renders the panel body (header line is added by the Model).
func (n nowPanel) view(now time.Time) string {
	var b strings.Builder
	b.WriteString(panelTitle("now"))
	b.WriteByte('\n')

	if n.currentTool == nil {
		b.WriteString("  ")
		b.WriteString(theme.subtle.Render("idle"))
		b.WriteByte('\n')
	} else {
		spin := spinnerFrames[n.spinnerFrame]
		b.WriteString("  ")
		b.WriteString(theme.ok.Render(spin))
		b.WriteByte(' ')
		b.WriteString(formatToolHeadline(*n.currentTool))
		if !n.currentToolAt.IsZero() {
			elapsed := formatDuration(now.Sub(n.currentToolAt))
			b.WriteString("  ")
			b.WriteString(theme.subtle.Render("(" + elapsed + " in)"))
		}
		b.WriteByte('\n')
	}

	if n.lastAssistant != "" {
		b.WriteString("  ")
		b.WriteString(theme.subtle.Render("» " + truncate(n.lastAssistant, 80)))
		b.WriteByte('\n')
	}
	return b.String()
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
