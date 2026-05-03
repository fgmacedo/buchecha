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

// titleText is the string the box wrapper embeds in the header's top
// border per the dashboard mockup: branch, iter n/N, elapsed. Before
// the first iteration starts (iter == 0) the iteration counter is
// replaced with "planning..." so the user sees activity from t=0 in
// Director mode rather than a misleading "iter 0/N".
func (h header) titleText(now time.Time) string {
	elapsed := "0s"
	if !h.startedAt.IsZero() {
		elapsed = formatDuration(now.Sub(h.startedAt))
	}
	stage := fmt.Sprintf("iter %d/%d", h.iter, h.maxIter)
	if h.iter == 0 {
		stage = "planning..."
	}
	return fmt.Sprintf("bcc %s  %s  %s",
		trimEmpty(h.branch), stage, elapsed)
}

// view renders the header body line: spec path, alive dot, and the
// optional [paused] tag. now is injected so tests are deterministic;
// production passes time.Now(). width is provided for symmetry with
// other panels and reserved for future use (the body is naturally
// short).
func (h header) view(now time.Time, _ int) string {
	dot := aliveDot(h.lastEvent, now)
	pauseTag := ""
	if h.paused {
		pauseTag = " " + theme.warn.Render("[paused]")
	}
	return fmt.Sprintf("%s  %s%s",
		theme.subtle.Render(h.specPath), dot, pauseTag)
}

// viewSession renders the session-mode body line: spec path plus the
// idle-state badge that replaces the alive dot in TUI session mode
// (P2.11.7). The Journal binding is omitted because the journal viewer
// is carved out to spec-vendor-neutrality; the badge advertises only
// the wired-up actions.
func (h header) viewSession(status string) string {
	badge := fmt.Sprintf("idle (%s) %s r resume %s e edit %s q exit",
		status,
		theme.subtle.Render("•"),
		theme.subtle.Render("•"),
		theme.subtle.Render("•"),
	)
	return fmt.Sprintf("%s  %s",
		theme.subtle.Render(h.specPath),
		theme.warn.Render(badge),
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
