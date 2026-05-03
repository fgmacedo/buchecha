package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/loop"
)

// escalationState is the modal sub-state machine: while latched, the
// panel is either offering the four-option choice or collecting a hint
// for a Resume reply.
type escalationState int

const (
	escalationStateChoosing escalationState = iota
	escalationStateHintInput
)

// planSkippedMsg latches the "planner declared the spec done" state on
// the Model. Sent by the host orchestrator (cli/run_director.go) when
// the Planner returned via bcc_plan_skip; carries the reason string so
// the View can show it on the friendly terminal screen.
type planSkippedMsg struct {
	reason string
}

// SignalPlanSkipped tells the Model that the Planner declared the spec
// done via bcc_plan_skip. The Model latches a quit-only terminal
// screen instead of tearing down the program. Safe to call before or
// after the bubbletea program has started.
func (m *Model) SignalPlanSkipped(reason string) {
	if m.program == nil {
		return
	}
	m.program.Send(planSkippedMsg{reason: reason})
}

// planFailedMsg latches the planner-failed details on the Model so the
// session view surfaces the underlying error (e.g. "claude exited 1:
// out of credits") instead of tearing the dashboard down. Carried via
// program.Send from the host orchestrator.
type planFailedMsg struct {
	message string
}

// SignalPlanFailed tells the Model the Planner subprocess failed
// without producing a terminal MCP call. The Model surfaces the
// supplied message in the session footer so the user can read what
// went wrong before pressing [r]/[e]/[q]. Safe to call before or after
// the bubbletea program has started.
func (m *Model) SignalPlanFailed(message string) {
	if m.program == nil {
		return
	}
	m.program.Send(planFailedMsg{message: message})
}

// directorPanel renders the Director-mode state on the dashboard: the
// confirmed Plan as a checklist, the active phase in highlight, the
// latest verdict's outcome, and the cumulative executor cost. The
// Model owns the panel and feeds it events directly; nothing here
// reads from disk.
//
// The panel is hidden when the run is not Director-driven (plan == nil),
// keeping the MVP layout intact for the legacy path.
type directorPanel struct {
	plan           *director.Plan
	currentPhaseID string
	currentAttempt int
	latestOutcome  string
	cumulativeCost float64

	// planningStatus tracks the well-known "planning" task on the
	// timeline. The Planner task is not part of the DAG; the loop emits a
	// TaskStarted/TaskCompleted("planning") pair at run boot so the panel
	// shows planning as the first finished entry of every Director run.
	planningStatus phaseMark

	// phaseStatus tracks each phase's lifecycle marker derived from the
	// stream of PhaseBriefed / PhaseReviewed / DirectorEscalation events.
	// Approved phases stay approved across attempts; escalations override
	// in-progress; revise stays in-progress.
	phaseStatus map[string]phaseMark

	// currentSubDAG is the set of task ids the active iteration's
	// Briefing scoped the Executor to. Rendered as indented rows under
	// the active phase so the human can see which slice of the DAG is in
	// flight.
	currentSubDAG []string

	// phaseCapability records the resolved per-role spawn parameters
	// for each phase as the loop emits PhaseBriefed events. The panel
	// renders these inline alongside the phase row so the human can see
	// which model and effort each role used and whether the Planner
	// asked the loop to skip the Briefer or Reviewer agent.
	phaseCapability map[string]phaseCapability

	// escalation latches when a DirectorEscalation event arrives so the
	// View renders the modal overlay; cleared by handleEscalationKey when
	// the user resolves the modal by sending an EscalationReply on the
	// gate.
	escalation        bool
	escalationMsg     string
	escalationFor     string // phase id under escalation
	escalationAttempt int

	// escalationState routes key events: Choosing accepts R/F/S/A,
	// HintInput accepts free text and Enter/Esc.
	escalationState escalationState
	hintInput       textinput.Model
}

// phaseCapability captures the resolved per-role spawn parameters the
// loop reports on PhaseBriefed: what model and effort each role will
// use for the upcoming iteration, and whether the Planner asked the
// loop to skip the Briefer or Reviewer agent. The panel renders this
// next to the phase row so the human sees the routing as it happens.
type phaseCapability struct {
	BrieferModel   string
	BrieferEffort  string
	ExecutorModel  string
	ExecutorEffort string
	ReviewerModel  string
	ReviewerEffort string
	BrieferSkipped bool
	ReviewSkipped  bool
}

// phaseMark is the visual state of a single phase in the panel.
type phaseMark int

const (
	phasePending phaseMark = iota
	phaseInProgress
	phaseApproved
	phaseEscalated
)

// glyph returns the checkbox-style glyph for a phase. Colors are
// applied by the caller so the same glyph can be rendered differently
// in different rows.
func (m phaseMark) glyph() string {
	switch m {
	case phaseApproved:
		return "[x]"
	case phaseInProgress:
		return "[/]"
	case phaseEscalated:
		return "[!]"
	default:
		return "[ ]"
	}
}

// onPhasePlanned latches the confirmed Plan and resets the per-phase
// status map. The plan pointer is shared read-only with the loop; the
// panel never mutates it.
func (d *directorPanel) onPhasePlanned(p *director.Plan) {
	d.plan = p
	d.phaseStatus = make(map[string]phaseMark, len(p.Phases))
	d.currentPhaseID = ""
	d.currentAttempt = 0
	d.latestOutcome = ""
	d.escalation = false
	d.currentSubDAG = nil
	if d.planningStatus == phasePending {
		d.planningStatus = phaseApproved
	}
}

// onTaskStarted updates the planning track when the loop emits a
// TaskStarted("planning") synthetic event at run boot. Per-task events
// for non-planning ids are recorded as informational only; the existing
// phase-level rows continue to drive the active highlight.
func (d *directorPanel) onTaskStarted(taskID string) {
	if taskID == planningTaskID {
		d.planningStatus = phaseInProgress
	}
}

// onTaskCompleted closes the planning task on the timeline. Non-planning
// ids are no-ops here; the per-phase mark already covers them.
func (d *directorPanel) onTaskCompleted(taskID string) {
	if taskID == planningTaskID {
		d.planningStatus = phaseApproved
	}
}

// planningTaskID mirrors dag.PlanningTaskID without taking a dag
// dependency from the TUI package.
const planningTaskID = "planning"

// onPhaseBriefed marks the phase as in-progress and points the active
// cursor at it.
func (d *directorPanel) onPhaseBriefed(phaseID string, attempt int, b *director.Briefing, cap phaseCapability) {
	if d.phaseStatus == nil {
		d.phaseStatus = map[string]phaseMark{}
	}
	d.currentPhaseID = phaseID
	d.currentAttempt = attempt
	if d.phaseStatus[phaseID] != phaseApproved {
		d.phaseStatus[phaseID] = phaseInProgress
	}
	if b != nil && len(b.SubDAGTaskIDs) > 0 {
		d.currentSubDAG = append(d.currentSubDAG[:0], b.SubDAGTaskIDs...)
	} else {
		d.currentSubDAG = nil
	}
	if d.phaseCapability == nil {
		d.phaseCapability = map[string]phaseCapability{}
	}
	d.phaseCapability[phaseID] = cap
}

// onPhaseReviewed records the review outcome and updates the phase
// mark. An approve outcome locks the phase as approved; revise leaves
// the phase in-progress for the next attempt; escalate marks it
// pending user input.
func (d *directorPanel) onPhaseReviewed(phaseID string, attempt int, outcome string) {
	if d.phaseStatus == nil {
		d.phaseStatus = map[string]phaseMark{}
	}
	d.currentPhaseID = phaseID
	d.currentAttempt = attempt
	d.latestOutcome = outcome
	switch outcome {
	case "approve":
		d.phaseStatus[phaseID] = phaseApproved
	case "revise":
		d.phaseStatus[phaseID] = phaseInProgress
	case "escalate":
		d.phaseStatus[phaseID] = phaseEscalated
	}
}

// onEscalation latches the escalation modal and stores the reasoning
// the Reviewer attached to the verdict. The modal opens on the
// four-option choosing screen; the hint input is initialised but
// blurred until the user picks Resume.
func (d *directorPanel) onEscalation(phaseID string, attempt int, reasoning string) {
	d.escalation = true
	d.escalationMsg = reasoning
	d.escalationFor = phaseID
	d.escalationAttempt = attempt
	d.escalationState = escalationStateChoosing
	ti := textinput.New()
	ti.Placeholder = "describe the correction; submit empty for none"
	ti.CharLimit = 512
	ti.SetWidth(60)
	d.hintInput = ti
	if d.phaseStatus == nil {
		d.phaseStatus = map[string]phaseMark{}
	}
	d.phaseStatus[phaseID] = phaseEscalated
}

// onCost folds an executor result-summary cost into the running total.
// Director-call costs (planner / briefer / reviewer) are not currently
// surfaced as events; the panel treats per-iteration executor cost as
// the live cumulative total. A future event for DirectorCallStats can
// flow into the same accumulator without a render-side change.
func (d *directorPanel) onCost(usd float64) {
	d.cumulativeCost += usd
}

// active reports whether the panel has a plan to render. The host uses
// it to decide whether to allocate layout space.
func (d directorPanel) active() bool { return d.plan != nil }

// capabilityBadge formats a phase's resolved capability state as a
// compact bracketed annotation, e.g. "[E:opus/high B:skip R:skip]".
// Returns empty when nothing meaningful applies (no overrides set, no
// skips). Models are shortened to drop the family prefix when present
// so the badge stays narrow on the dashboard.
func capabilityBadge(c phaseCapability) string {
	parts := []string{}
	if part := roleBadge("E", c.ExecutorModel, c.ExecutorEffort, false); part != "" {
		parts = append(parts, part)
	}
	if part := roleBadge("B", c.BrieferModel, c.BrieferEffort, c.BrieferSkipped); part != "" {
		parts = append(parts, part)
	}
	if part := roleBadge("R", c.ReviewerModel, c.ReviewerEffort, c.ReviewSkipped); part != "" {
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// roleBadge renders one role's badge fragment. Empty model+effort
// without skip yields an empty string so default-only roles do not
// clutter the line; skipped roles always render.
func roleBadge(prefix, model, effort string, skipped bool) string {
	if skipped {
		return prefix + ":skip"
	}
	if model == "" && effort == "" {
		return ""
	}
	value := shortModel(model)
	if effort != "" {
		if value == "" {
			value = "(default)"
		}
		value += "/" + effort
	}
	return prefix + ":" + value
}

// shortModel drops the family prefix from a canonical model id so the
// dashboard badge fits in narrow terminals. "claude-opus-4-7" becomes
// "opus-4-7"; ids that do not match the expected pattern stay
// untouched.
func shortModel(model string) string {
	if i := strings.Index(model, "-"); i > 0 {
		return model[i+1:]
	}
	return model
}

// viewWithPlanning is the dispatch the host calls. When the planner is
// still in flight (planningPending is true and plan == nil), it renders
// a "planning..." placeholder with a live spinner, the model running,
// and current tokens/cost so the user sees activity even before the
// plan exists. Once the plan is available, behavior is identical to
// view().
func (d directorPanel) viewWithPlanning(width int, planningPending bool, spinner string, iterTokens int64, costUSD float64) string {
	if d.plan != nil {
		return d.view(width)
	}
	if !planningPending {
		return d.view(width)
	}
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(spinner)
	b.WriteString(" ")
	b.WriteString(theme.warn.Render("planning..."))
	b.WriteByte('\n')
	if iterTokens > 0 {
		b.WriteString(fmt.Sprintf("  tokens: %d\n", iterTokens))
	}
	if costUSD > 0 {
		b.WriteString(fmt.Sprintf("  director cost: $%.2f\n", costUSD))
	}
	b.WriteString("  ")
	b.WriteString(theme.subtle.Render("the planner is reading the spec; activity is shown in 'now' and 'recent actions'"))
	b.WriteByte('\n')
	_ = width
	return b.String()
}

// view renders the panel body (header line, phases checklist, latest
// verdict line). The escalation modal is rendered separately on top of
// the dashboard frame; this body is also visible behind the modal so
// the user keeps the run state in view while answering.
func (d directorPanel) view(width int) string {
	if d.plan == nil {
		return "  " + theme.subtle.Render("waiting for director plan") + "\n"
	}
	var b strings.Builder

	if d.plan.Goal != "" {
		b.WriteString("  ")
		b.WriteString(theme.subtle.Render("goal: "))
		b.WriteString(truncate(d.plan.Goal, max(width-10, 8)))
		b.WriteByte('\n')
	}

	approved := 0
	for _, ph := range d.plan.Phases {
		if d.phaseStatus[ph.ID] == phaseApproved {
			approved++
		}
	}
	b.WriteString(fmt.Sprintf("  phases: %d/%d approved\n", approved, len(d.plan.Phases)))

	planMark := d.planningStatus
	if planMark == phasePending {
		planMark = phaseApproved
	}
	planGlyph := planMark.glyph()
	switch planMark {
	case phaseApproved:
		planGlyph = theme.ok.Render(planGlyph)
	case phaseInProgress:
		planGlyph = theme.warn.Render(planGlyph)
	default:
		planGlyph = theme.subtle.Render(planGlyph)
	}
	b.WriteString(fmt.Sprintf("  %s planning\n", planGlyph))

	for _, ph := range d.plan.Phases {
		mark := d.phaseStatus[ph.ID]
		glyph := mark.glyph()
		styled := glyph
		switch mark {
		case phaseApproved:
			styled = theme.ok.Render(glyph)
		case phaseInProgress:
			styled = theme.warn.Render(glyph)
		case phaseEscalated:
			styled = theme.err.Render(glyph)
		default:
			styled = theme.subtle.Render(glyph)
		}
		title := ph.Title
		if title == "" {
			title = ph.ID
		}
		row := fmt.Sprintf("  %s %s", styled, title)
		if ph.ID == d.currentPhaseID {
			active := fmt.Sprintf(" (attempt %d)", d.currentAttempt)
			row += theme.subtle.Render(active)
			row = lipgloss.NewStyle().Bold(true).Render(row)
		}
		if cap, ok := d.phaseCapability[ph.ID]; ok {
			if badge := capabilityBadge(cap); badge != "" {
				row += " " + theme.subtle.Render(badge)
			}
		}
		b.WriteString(row)
		b.WriteByte('\n')

		if ph.ID == d.currentPhaseID && len(d.currentSubDAG) > 0 {
			for _, tid := range d.currentSubDAG {
				b.WriteString("      ")
				b.WriteString(theme.keyHint.Render("> "))
				b.WriteString(theme.warn.Render(tid))
				b.WriteByte('\n')
			}
		}
	}

	if d.latestOutcome != "" {
		b.WriteString("  verdict: ")
		switch d.latestOutcome {
		case "approve":
			b.WriteString(theme.ok.Render(d.latestOutcome))
		case "revise":
			b.WriteString(theme.warn.Render(d.latestOutcome))
		case "escalate":
			b.WriteString(theme.err.Render(d.latestOutcome))
		default:
			b.WriteString(d.latestOutcome)
		}
		b.WriteByte('\n')
	}

	b.WriteString(fmt.Sprintf("  director cost: $%.2f\n", d.cumulativeCost))
	return b.String()
}

// --- Escalation modal ------------------------------------------------

// directorKeyMap is the binding set the Director modal honors. Active
// only while directorPanel.escalation is latched; the dashboard's main
// keymap is suspended during the modal.
type directorKeyMap struct {
	Resume       key.Binding
	ForceApprove key.Binding
	Skip         key.Binding
	Abort        key.Binding
}

func (k directorKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Resume, k.ForceApprove, k.Skip, k.Abort}
}

func (k directorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Resume, k.ForceApprove, k.Skip, k.Abort}}
}

func defaultDirectorKeyMap() directorKeyMap {
	return directorKeyMap{
		Resume: key.NewBinding(
			key.WithKeys("r", "R"),
			key.WithHelp("r", "resume hint: retry with optional hint"),
		),
		ForceApprove: key.NewBinding(
			key.WithKeys("f", "F"),
			key.WithHelp("f", "force-approve: accept pending tasks as done"),
		),
		Skip: key.NewBinding(
			key.WithKeys("s", "S"),
			key.WithHelp("s", "skip: advance past the phase"),
		),
		Abort: key.NewBinding(
			key.WithKeys("a", "A"),
			key.WithHelp("a", "abort: stop the run"),
		),
	}
}

// renderEscalationModal composes the modal overlay shown while the
// loop is paused on a DirectorEscalation. The user picks an answer; the
// Model forwards it onto EscalationGate so the loop can proceed. The
// keymap is passed explicitly so a future binding tweak (e.g., sticky
// help glyph) does not have to read from the modal renderer.
func renderEscalationModal(d directorPanel, _ directorKeyMap) string {
	if !d.escalation {
		return ""
	}
	title := fmt.Sprintf("[ director: escalation on %s (attempt %d) ]", d.escalationFor, d.escalationAttempt)
	body := strings.Builder{}
	body.WriteString(theme.title.Render(title))
	body.WriteByte('\n')
	if d.escalationMsg != "" {
		for line := range strings.SplitSeq(strings.TrimRight(d.escalationMsg, "\n"), "\n") {
			body.WriteString("  ")
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	body.WriteByte('\n')
	if d.escalationState == escalationStateHintInput {
		body.WriteString("  hint (enter submits, esc cancels):\n")
		body.WriteString("  ")
		body.WriteString(d.hintInput.View())
		body.WriteByte('\n')
		return body.String()
	}
	body.WriteString("  ")
	body.WriteString(theme.keyHint.Render("[R]"))
	body.WriteString(" resume hint   ")
	body.WriteString(theme.keyHint.Render("[F]"))
	body.WriteString(" force-approve   ")
	body.WriteString(theme.keyHint.Render("[S]"))
	body.WriteString(" skip   ")
	body.WriteString(theme.keyHint.Render("[A]"))
	body.WriteString(" abort\n")
	return body.String()
}

// handleEscalationKey routes a key press during the escalation modal to
// the EscalationGate. While in choosing state, R opens the hint input,
// F packages a ForceApprove reply, S a Skip, A an Abort. While in
// hint-input state, Enter submits a Resume reply with the captured
// hint and Esc cancels back to the choosing state. The reply travels
// on the buffered channel the host wired into the Model; an unbuffered
// or full channel is treated as a no-op so the user can press the key
// again.
func (m Model) handleEscalationKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.director.escalationState == escalationStateHintInput {
		return m.handleEscalationHintKey(msg)
	}
	switch {
	case key.Matches(msg, m.directorKeys.Resume):
		m.director.escalationState = escalationStateHintInput
		_ = m.director.hintInput.Focus()
		return m, nil
	case key.Matches(msg, m.directorKeys.ForceApprove):
		return m.dispatchEscalation(loop.EscalationReply{Kind: loop.EscalationForceApprove})
	case key.Matches(msg, m.directorKeys.Skip):
		return m.dispatchEscalation(loop.EscalationReply{Kind: loop.EscalationSkip})
	case key.Matches(msg, m.directorKeys.Abort):
		return m.dispatchEscalation(loop.EscalationReply{Kind: loop.EscalationAbort})
	}
	return m, nil
}

func (m Model) handleEscalationHintKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		hint := strings.TrimSpace(m.director.hintInput.Value())
		return m.dispatchEscalation(loop.EscalationReply{Kind: loop.EscalationResume, Hint: hint})
	case "esc":
		m.director.escalationState = escalationStateChoosing
		m.director.hintInput.Reset()
		m.director.hintInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.director.hintInput, cmd = m.director.hintInput.Update(msg)
	return m, cmd
}

func (m Model) dispatchEscalation(reply loop.EscalationReply) (tea.Model, tea.Cmd) {
	if m.escalationGate != nil {
		select {
		case m.escalationGate <- reply:
		default:
		}
	}
	m.director.escalation = false
	m.director.escalationMsg = ""
	m.director.escalationFor = ""
	m.director.escalationAttempt = 0
	m.director.escalationState = escalationStateChoosing
	m.director.hintInput.Reset()
	m.director.hintInput.Blur()
	return m, nil
}
