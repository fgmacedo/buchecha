package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/spec"
)

// riskPanel answers the user's "if I close this laptop right now, what
// do I lose?" question. Three lines summarise committed progress, the
// uncommitted working tree, and the journal status of the current
// iteration.
type riskPanel struct {
	checked        int
	totalItems     int
	dirtyFileCount int
	dirtyKnown     bool
	lastEditAt     time.Time

	currentIter   int
	journalRaw    string
	journalResult spec.Result
	journalKnown  bool
}

// onSpecParsed updates the spec-derived parts (committed item count
// and journal status) from a fresh parse.
func (r *riskPanel) onSpecParsed(plan spec.Plan, latest spec.LatestResult, latestKnown bool) {
	r.checked = plan.CountChecked()
	r.totalItems = r.checked + plan.CountUnchecked()
	r.journalRaw = latest.Raw
	r.journalResult = latest.Result
	r.journalKnown = latestKnown
}

// onDirtyFileCount records the latest probe result.
func (r *riskPanel) onDirtyFileCount(n int) {
	r.dirtyFileCount = n
	r.dirtyKnown = true
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

// onIterStarted updates the current-iter pointer used by the journal
// line ("Result for iter N: ...").
func (r *riskPanel) onIterStarted(idx int) {
	r.currentIter = idx
	// Clear the journal "known" flag for the new iteration: the
	// previous iteration's result is no longer current. The next
	// IterationFinished re-parses the spec and re-fills it.
	r.journalKnown = false
}

// view renders the panel body.
func (r riskPanel) view(now time.Time) string {
	var b strings.Builder
	b.WriteString(panelTitle("if you close now"))
	b.WriteByte('\n')

	b.WriteString(fmt.Sprintf("  %s committed: %d/%d items\n",
		theme.ok.Render("✓"), r.checked, r.totalItems))

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

	journal := journalLine(r.currentIter, r.journalRaw, r.journalResult, r.journalKnown)
	b.WriteString("  ")
	b.WriteString(journal)
	b.WriteByte('\n')
	return b.String()
}

// journalLine formats the journal status with a glyph: ✓ when the
// latest entry's Result is recognised, ⚠ when missing or unknown.
func journalLine(iter int, raw string, res spec.Result, known bool) string {
	if !known || raw == "" {
		return theme.warn.Render("⚠") + fmt.Sprintf(" journal: Result for iter %d not yet written", iter)
	}
	if res == spec.ResultUnknown {
		return theme.warn.Render("⚠") + fmt.Sprintf(" journal: latest Result %q (unknown)", raw)
	}
	return theme.ok.Render("✓") + fmt.Sprintf(" journal: latest Result %s", raw)
}
