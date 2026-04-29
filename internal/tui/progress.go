package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/fgmacedo/buchecha/internal/spec"
)

// progressPanel renders the plan as phase-by-phase checkboxes with a
// progress bar and an ETA derived from the rolling iteration time.
//
// The plan is repopulated by the Model on every IterationFinished
// (re-parsing the spec), so progress always reflects the current
// `[x]`/`[ ]` state on disk.
type progressPanel struct {
	plan      spec.Plan
	durations []time.Duration

	// currentPhaseIdx is the index of the phase the next iteration will
	// touch (the first phase containing any [ ] item). -1 when no phase
	// has unchecked items.
	currentPhaseIdx int
}

const progressBarWidth = 32

// onSpecParsed swaps in a freshly parsed plan and recomputes the
// "current phase" pointer.
func (p *progressPanel) onSpecParsed(plan spec.Plan) {
	p.plan = plan
	p.currentPhaseIdx = firstUncheckedPhase(plan)
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
// layout; the bar uses a fixed cell count and the phase row wraps via
// natural content length (callers that need a tighter rendering can
// pass a shorter width once panels truncate on it).
func (p progressPanel) view(_ int) string {
	var b strings.Builder

	if len(p.plan.Phases) == 0 {
		b.WriteString("  ")
		b.WriteString(theme.subtle.Render("plan not parsed yet"))
		b.WriteByte('\n')
		return b.String()
	}

	// Phase rows: "P1 [x][x][x] P2 [x][ ]► P3 [ ][ ]"
	b.WriteString("  ")
	for i, ph := range p.plan.Phases {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(phaseLabel(i, ph))
		b.WriteByte(' ')
		for _, it := range ph.Items {
			if it.Checked {
				b.WriteString(theme.ok.Render("☑"))
			} else {
				b.WriteString("☐")
			}
		}
		if i == p.currentPhaseIdx {
			b.WriteString(theme.warn.Render("►"))
		}
	}
	b.WriteByte('\n')

	checked := p.plan.CountChecked()
	total := checked + p.plan.CountUnchecked()
	bar := renderBar(checked, total, progressBarWidth)
	b.WriteString("  ")
	b.WriteString(bar)
	b.WriteString(fmt.Sprintf("  %d/%d items", checked, total))

	if eta := computeETA(p.durations, p.plan.CountUnchecked()); eta > 0 {
		b.WriteString(", ETA ~")
		b.WriteString(formatDuration(eta))
	}
	b.WriteByte('\n')
	return b.String()
}

// firstUncheckedPhase returns the index of the first phase with at
// least one [ ] item, or -1 when every item is checked. Phases with
// empty Title (the implicit pre-H3 phase) participate just like real
// phases so plans without H3 headings still light up the marker.
func firstUncheckedPhase(plan spec.Plan) int {
	for i, ph := range plan.Phases {
		for _, it := range ph.Items {
			if !it.Checked {
				return i
			}
		}
	}
	return -1
}

// phaseLabel produces a compact label for the row. When the phase
// title starts with "P<n>:" or "P<n> ", the prefix is the label.
// Otherwise we fall back to "P<i+1>".
func phaseLabel(i int, ph spec.Phase) string {
	t := strings.TrimSpace(ph.Title)
	if t != "" {
		// Take the first whitespace-separated token, stripping a
		// trailing colon: "P2.5:" → "P2.5".
		first := strings.Fields(t)[0]
		first = strings.TrimSuffix(first, ":")
		if first != "" {
			return first
		}
	}
	return fmt.Sprintf("P%d", i+1)
}

// renderBar draws a progress bar of width characters, with checked /
// total filled. Empty total renders an all-empty bar.
func renderBar(checked, total, width int) string {
	if width <= 0 {
		return ""
	}
	if total <= 0 {
		return theme.barEmpty.Render(strings.Repeat("░", width))
	}
	filled := checked * width / total
	if checked > 0 && filled == 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	return theme.bar.Render(strings.Repeat("█", filled)) +
		theme.barEmpty.Render(strings.Repeat("░", width-filled))
}

// computeETA averages the recent iteration durations and multiplies
// by the remaining unchecked items, treating each item as one iter's
// worth of work. Coarse but useful as an order-of-magnitude signal.
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
