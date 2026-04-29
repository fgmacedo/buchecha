package tui

// layout caches the per-panel column widths the Model passes to its
// view methods and the box wrapper. Recomputed on every
// tea.WindowSizeMsg so panels read precomputed widths instead of
// re-deriving them on every render.
//
// All widths are total visible cells (borders included). The header,
// progress, risk, and actions boxes span the terminal's full width;
// the now+health row splits that width between two side-by-side boxes
// per nowSplitPercent.
type layout struct {
	headerW   int
	nowW      int
	healthW   int
	progressW int
	riskW     int
	actionsW  int
}

// nowSplitPercent is the fraction of the terminal width allocated to
// the now box; the rest goes to health. Lives in code as a named
// constant so future tuning is one edit and the split stays
// deterministic for tests.
const nowSplitPercent = 60

// computeLayout returns the per-panel widths for a terminal of the
// given total width. width <= 0 yields a zero layout (treated as
// "uninitialised" by Model.activeLayout).
func computeLayout(width int) layout {
	if width <= 0 {
		return layout{}
	}
	nowW := width * nowSplitPercent / 100
	if nowW < 1 {
		nowW = 1
	}
	healthW := width - nowW
	if healthW < 1 {
		healthW = 1
	}
	return layout{
		headerW:   width,
		nowW:      nowW,
		healthW:   healthW,
		progressW: width,
		riskW:     width,
		actionsW:  width,
	}
}
