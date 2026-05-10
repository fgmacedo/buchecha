package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
)

// progressBarWidth is the cell count for the bubbles/v2 progress bar.
const progressBarWidth = 32

// progressPanel renders this iteration's task progress derived from
// the wire protocol. bcc never parses the user's spec, so the panel
// cannot show a "X/Y items" ratio against the plan; it shows what the
// agent has reported in the current iteration (started vs completed
// task counts) plus a rolling iteration ETA. Counters reset on every
// IterationStarted so the bar reflects "this phase's progress", not
// run-cumulative totals.
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

// onTaskStarted increments the started counter and refreshes the bar.
// The wire contract pairs task_started with task_completed by
// id; the panel does not assume IDs and just counts the events.
func (p *progressPanel) onTaskStarted() {
	p.tasksStarted++
	p.refreshBar()
}

// onTaskCompleted increments the completed counter and refreshes the
// bar. Defensive invariant: tasksStarted is held >= tasksCompleted at
// all times so a misbehaving agent that skips a start cannot render
// nonsense (e.g. "5/1 tasks").
func (p *progressPanel) onTaskCompleted() {
	p.tasksCompleted++
	if p.tasksCompleted > p.tasksStarted {
		p.tasksStarted = p.tasksCompleted
	}
	p.refreshBar()
}

func (p *progressPanel) refreshBar() {
	if p.tasksStarted > 0 {
		_ = p.bar.SetPercent(float64(p.tasksCompleted) / float64(p.tasksStarted))
	} else {
		_ = p.bar.SetPercent(0)
	}
}

// onIterStarted resets the per-iteration task counters and the bar.
// The rolling iteration durations are intentionally preserved across
// iterations: ETA averages run-wide, but task-progress visualization
// scopes to the iteration the agent is currently working on.
func (p *progressPanel) onIterStarted() {
	p.tasksStarted = 0
	p.tasksCompleted = 0
	_ = p.bar.SetPercent(0)
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
	fmt.Fprintf(&b, "  %d/%d tasks", p.tasksCompleted, p.tasksStarted)

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
