package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// progressBarWidth is the cell count for the bubbles/v2 progress bar.
const progressBarWidth = 32

// progressPanel renders cumulative task progress derived from the wire
// protocol. bcc never parses the user's spec, so the panel cannot show
// a "X/Y items" ratio; it shows what the agent has reported (started
// vs completed task counts) plus a rolling iteration ETA. When the
// agent does not emit task_started / task_completed events, the panel
// stays at zero counts.
type progressPanel struct {
	tasksStarted   int
	tasksCompleted int
	durations      []time.Duration

	// bar is the bubbles/v2/progress component, fed by the
	// completed-vs-started ratio. The animated transitions land for
	// free; SetPercent runs on every event update.
	bar progress.Model
}

// newProgressBar configures the bubbles progress bar with a fixed
// width and the panel's "bar" colour scheme.
func newProgressBar() progress.Model {
	return progress.New(
		progress.WithWidth(progressBarWidth),
		progress.WithoutPercentage(),
		progress.WithColors(theme.bar.GetForeground()),
	)
}

// onBccEvent updates the panel from a wire-protocol event. Task counts
// are cumulative across the entire run; the bar tracks the
// completed-over-started ratio.
func (p *progressPanel) onBccEvent(ev agentcontract.BccEvent) {
	switch ev.Kind {
	case agentcontract.BccEventTaskStarted:
		p.tasksStarted++
	case agentcontract.BccEventTaskCompleted:
		p.tasksCompleted++
	}
	if p.tasksStarted > 0 {
		_ = p.bar.SetPercent(float64(p.tasksCompleted) / float64(p.tasksStarted))
	} else {
		_ = p.bar.SetPercent(0)
	}
}

// onIterationFinished records the duration so the ETA can average the
// rolling history. A duration of 0 is ignored (defensive against zero
// timestamps in fake events).
func (p *progressPanel) onIterationFinished(d time.Duration) {
	if d <= 0 {
		return
	}
	p.durations = append(p.durations, d)
	if len(p.durations) > 32 {
		p.durations = p.durations[len(p.durations)-32:]
	}
}

// view renders the panel body. width is reserved for future width-aware
// layout; the bar uses a fixed cell count.
func (p progressPanel) view(_ int) string {
	var b strings.Builder

	if p.tasksStarted == 0 && p.tasksCompleted == 0 {
		b.WriteString("  ")
		b.WriteString(theme.subtle.Render("waiting for first task event"))
		b.WriteByte('\n')
		return b.String()
	}

	pct := 0.0
	if p.tasksStarted > 0 {
		pct = float64(p.tasksCompleted) / float64(p.tasksStarted)
	}
	b.WriteString("  ")
	b.WriteString(p.bar.ViewAs(pct))
	b.WriteString(fmt.Sprintf("  %d/%d tasks", p.tasksCompleted, p.tasksStarted))

	if eta := computeETA(p.durations, p.tasksStarted-p.tasksCompleted); eta > 0 {
		b.WriteString(", ETA ~")
		b.WriteString(formatDuration(eta))
	}
	b.WriteByte('\n')
	return b.String()
}

// computeETA averages the recent iteration durations and multiplies
// by the remaining task count. Coarse but useful as an order-of-magnitude
// signal once a few iterations have run.
func computeETA(durations []time.Duration, remaining int) time.Duration {
	if len(durations) == 0 || remaining <= 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range durations {
		sum += d
	}
	avg := sum / time.Duration(len(durations))
	return avg * time.Duration(remaining)
}
