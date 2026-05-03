package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/fgmacedo/buchecha/internal/director"
)

// PlanConfirmKind classifies the user's reply to the plan-confirmation
// modal that pops up after the planner returns a plan in TUI mode.
type PlanConfirmKind int

const (
	// PlanConfirmProceed: the user accepted the plan; the loop should start.
	PlanConfirmProceed PlanConfirmKind = iota + 1
	// PlanConfirmAbort: the user rejected the plan; the run terminates.
	PlanConfirmAbort
)

// PlanConfirmReply is the user's reply on the plan-confirmation modal.
type PlanConfirmReply struct {
	Kind PlanConfirmKind
}

// planReadyMsg is the tea.Msg the host sends via Program.Send when the
// planner has resolved a plan and confirmation is needed. The Model
// transitions into "plan ready, awaiting confirmation" state.
type planReadyMsg struct {
	plan *director.Plan
}

// clearPlanningPendingMsg is the tea.Msg the host sends to clear the
// "planning pending" flag once the plan is confirmed and the loop is
// about to start. After that, the director panel reverts to its
// usual rendering driven by PhasePlanned and friends.
type clearPlanningPendingMsg struct{}

// SignalPlanReady is invoked by the orchestrator once the planner has
// returned and a plan is in hand awaiting user confirmation. It posts
// a tea.Msg through the program so the Update path can latch the plan
// and surface the confirm modal. Safe to call once per run; subsequent
// calls are ignored by the Update handler.
func (m *Model) SignalPlanReady(plan *director.Plan) {
	if m.program == nil {
		return
	}
	m.program.Send(planReadyMsg{plan: plan})
}

// ClearPlanningPending tells the Model that planning is no longer in
// flight (e.g., after the user confirmed and the loop started). The
// director panel placeholder yields to its standard rendering.
func (m *Model) ClearPlanningPending() {
	if m.program == nil {
		return
	}
	m.program.Send(clearPlanningPendingMsg{})
}

// planConfirmKeyMap binds [P]roceed and [A]bort on the
// plan-confirmation modal. Keys mirror the legacy stdin prompt so
// users have one mental model across modes.
type planConfirmKeyMap struct {
	Proceed key.Binding
	Abort   key.Binding
}

func defaultPlanConfirmKeyMap() planConfirmKeyMap {
	return planConfirmKeyMap{
		Proceed: key.NewBinding(key.WithKeys("p", "P", "enter"), key.WithHelp("[P]", "proceed")),
		Abort:   key.NewBinding(key.WithKeys("a", "A", "esc"), key.WithHelp("[A]", "abort")),
	}
}

// handlePlanConfirmKey processes key presses while the plan-confirm
// modal is up. Pressing [P] or Enter dispatches PlanConfirmProceed and
// clears the modal; [A] or Esc dispatches PlanConfirmAbort. Other keys
// are swallowed so the user cannot interact with the underlying
// dashboard while the modal owns input focus.
func (m Model) handlePlanConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keys := defaultPlanConfirmKeyMap()
	switch {
	case key.Matches(msg, keys.Proceed):
		m.dispatchPlanConfirm(PlanConfirmProceed)
		m.planReady = false
		return m, nil
	case key.Matches(msg, keys.Abort):
		m.dispatchPlanConfirm(PlanConfirmAbort)
		m.planReady = false
		// Cancel the run-level context so the orchestrator unwinds
		// cleanly. The orchestrator emits the synthetic LoopFinished
		// itself; we just signal abort here.
		if m.cancel != nil {
			m.cancel()
		}
		return m, nil
	}
	return m, nil
}

// dispatchPlanConfirm sends a reply on the plan-confirm gate. Buffered
// channel + non-blocking select keeps the UI responsive when the host
// has not picked up an earlier reply (defensive; the orchestrator
// always reads exactly once).
func (m Model) dispatchPlanConfirm(kind PlanConfirmKind) {
	if m.planConfirmGate == nil {
		return
	}
	select {
	case m.planConfirmGate <- PlanConfirmReply{Kind: kind}:
	default:
	}
}

// renderPlanConfirmModal returns a centered confirm overlay shown over
// the rendered plan tree. Plain-text by design; lipgloss styles only
// the border and the keys to keep noise low.
func renderPlanConfirmModal(width int, plan *director.Plan) string {
	var b strings.Builder
	b.WriteString("Plan ready\n")
	if plan != nil && plan.Goal != "" {
		b.WriteString("goal: ")
		b.WriteString(plan.Goal)
		b.WriteString("\n")
	}
	if plan != nil {
		fmt := "phases: %d\n"
		_ = fmt
		b.WriteString(planPhaseSummary(plan))
	}
	b.WriteString("\n[P]roceed  [A]bort")

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(0, 1)
	if width > 6 {
		style = style.Width(min(width-4, 60))
	}
	return style.Render(b.String())
}

func planPhaseSummary(plan *director.Plan) string {
	if plan == nil || len(plan.Phases) == 0 {
		return ""
	}
	return "phases: " + itoa(len(plan.Phases)) + "\n"
}

// itoa keeps plan_confirm.go free of an "strconv" dependency for one
// trivial call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	const digits = "0123456789"
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
