package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// riskPanel answers the user's "if I close this laptop right now, what
// do I lose?" question. Three lines summarise committed progress, the
// uncommitted working tree, and the agent's last reported signal.
type riskPanel struct {
	tasksCompleted int
	tasksStarted   int
	dirtyFileCount int
	dirtyKnown     bool
	lastEditAt     time.Time

	runCommitCount int
	commitsKnown   bool

	currentIter  int
	lastSignal   agentcontract.Signal
	signalKnown  bool
	signalRawVal string // raw "value" from iteration_result for diagnostics
}

// onBccEvent updates wire-driven counters and the latest signal.
func (r *riskPanel) onBccEvent(ev agentcontract.BccEvent) {
	switch ev.Kind {
	case agentcontract.BccEventTaskStarted:
		r.tasksStarted++
	case agentcontract.BccEventTaskCompleted:
		r.tasksCompleted++
	case agentcontract.BccEventIterationResult:
		r.lastSignal = ev.Signal
		r.signalKnown = true
		if v, ok := ev.Raw["value"].(string); ok {
			r.signalRawVal = v
		}
	}
}

// onDirtyFileCount records the latest probe result.
func (r *riskPanel) onDirtyFileCount(n int) {
	r.dirtyFileCount = n
	r.dirtyKnown = true
}

// onCommitCount records the run-local commit count probed via
// GitProbe.CommitsSince(BaselineSHA).
func (r *riskPanel) onCommitCount(n int) {
	r.runCommitCount = n
	r.commitsKnown = true
}

// onAgentEvent watches for write-shaped tool calls so the panel can
// surface "last edit Ns ago", a useful hint when the working tree is
// dirty.
func (r *riskPanel) onAgentEvent(ev loop.AgentEvent) {
	if ev.Kind != loop.KindToolUse || ev.Tool == nil {
		return
	}
	switch ev.Tool.Name {
	case "Edit", "Write", "NotebookEdit":
		at := ev.At
		if at.IsZero() {
			at = time.Now()
		}
		r.lastEditAt = at
	}
}

// onIterStarted updates the current-iter pointer used by the signal
// line ("Signal for iter N: ...").
func (r *riskPanel) onIterStarted(idx int) {
	r.currentIter = idx
	// Clear the signal "known" flag for the new iteration: the previous
	// iteration's signal is no longer current.
	r.signalKnown = false
	r.signalRawVal = ""
}

// view renders the panel body. width is reserved for future width-aware
// rendering; the lines are naturally short (counts + glyphs).
func (r riskPanel) view(now time.Time, _ int) string {
	var b strings.Builder

	commits := ""
	if r.commitsKnown {
		commits = theme.subtle.Render(fmt.Sprintf(" (%s)", pluralize(r.runCommitCount, "commit", "commits")))
	}
	b.WriteString(fmt.Sprintf("  %s tasks completed: %d/%d%s\n",
		theme.ok.Render("✓"), r.tasksCompleted, r.tasksStarted, commits))

	uncommitted := "..."
	if r.dirtyKnown {
		uncommitted = fmt.Sprintf("%d files", r.dirtyFileCount)
	}
	uncommittedGlyph := theme.ok.Render("✓")
	if r.dirtyKnown && r.dirtyFileCount > 0 {
		uncommittedGlyph = theme.warn.Render("⚠")
	}
	editHint := ""
	if !r.lastEditAt.IsZero() && r.dirtyKnown && r.dirtyFileCount > 0 {
		editHint = fmt.Sprintf(" (last edit %s ago)", formatDuration(now.Sub(r.lastEditAt)))
	}
	b.WriteString(fmt.Sprintf("  %s uncommitted: %s%s\n",
		uncommittedGlyph, uncommitted, theme.subtle.Render(editHint)))

	b.WriteString("  ")
	b.WriteString(signalLine(r.currentIter, r.lastSignal, r.signalRawVal, r.signalKnown))
	b.WriteByte('\n')
	return b.String()
}

// signalLine formats the latest iteration_result with a glyph: ✓ when
// the agent emitted a recognised value, ⚠ when missing or unknown.
func signalLine(iter int, sig agentcontract.Signal, raw string, known bool) string {
	if !known {
		return theme.warn.Render("⚠") + fmt.Sprintf(" signal: not yet emitted for iter %d", iter)
	}
	if sig == agentcontract.SignalUnknown {
		return theme.warn.Render("⚠") + fmt.Sprintf(" signal: %q (unknown)", raw)
	}
	return theme.ok.Render("✓") + fmt.Sprintf(" signal: %s", sig.String())
}
