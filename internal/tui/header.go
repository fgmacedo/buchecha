package tui

import (
	"fmt"
	"time"
)

// header is the top status line of the dashboard. It surfaces what the
// user needs to confirm at a glance: which spec, which branch, which
// iteration, how long the run has been alive, and a coloured liveness
// dot so a stalled run is obvious without reading any text.
type header struct {
	specPath  string
	branch    string
	iter      int
	maxIter   int
	startedAt time.Time
	lastEvent time.Time
	paused    bool
}

// onIter records the iteration index from an IterationStarted event
// so the header reflects the current step in real time.
func (h *header) onIter(idx, max int) {
	h.iter = idx
	if h.maxIter == 0 {
		h.maxIter = max
	}
}

// onAny stamps the heartbeat clock used by the alive dot. Called for
// every event the Model receives.
func (h *header) onAny(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	h.lastEvent = at
}

// view renders the single-line header. now is injected so tests are
// deterministic; production passes time.Now().
func (h header) view(now time.Time) string {
	elapsed := "0s"
	if !h.startedAt.IsZero() {
		elapsed = formatDuration(now.Sub(h.startedAt))
	}
	dot := aliveDot(h.lastEvent, now)
	pauseTag := ""
	if h.paused {
		pauseTag = " " + theme.warn.Render("[paused]")
	}
	return fmt.Sprintf("bcc %s  iter %d/%d  %s  %s%s  %s",
		trimEmpty(h.branch),
		h.iter, h.maxIter,
		elapsed,
		dot,
		pauseTag,
		theme.subtle.Render(h.specPath),
	)
}

// aliveDot maps the time since the last event to a coloured glyph.
// Green ●  < 30s, yellow ●  < 2m, red ●  ≥ 2m, hollow ○ when no event
// has arrived yet (the loop just started).
func aliveDot(last, now time.Time) string {
	if last.IsZero() {
		return theme.subtle.Render("○")
	}
	d := now.Sub(last)
	switch {
	case d < 30*time.Second:
		return theme.ok.Render("●")
	case d < 2*time.Minute:
		return theme.warn.Render("●")
	default:
		return theme.err.Render("●")
	}
}
