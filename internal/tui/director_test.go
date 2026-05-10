package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/supervision"
)

func samplePlan() *supervision.Plan {
	return &supervision.Plan{
		Goal:     "ship the director TUI panel",
		SpecHash: "abc123",
		Phases: []supervision.Phase{
			{ID: "p1", Title: "Domain types"},
			{ID: "p2", Title: "Adapter scaffold"},
			{ID: "p3", Title: "TUI panel"},
		},
	}
}

func TestCapabilityBadge(t *testing.T) {
	cases := []struct {
		name string
		cap  phaseCapability
		want string
	}{
		{"empty yields empty badge", phaseCapability{}, ""},
		{
			"executor only with effort",
			phaseCapability{ExecutorModel: "claude-opus-4-7", ExecutorEffort: "high"},
			"[E:opus-4-7/high]",
		},
		{
			"all three roles with skips",
			phaseCapability{
				ExecutorModel:  "claude-sonnet-4-6",
				ExecutorEffort: "low",
				BrieferSkipped: true,
				ReviewSkipped:  true,
			},
			"[E:sonnet-4-6/low B:skip R:skip]",
		},
		{
			"effort only without model surfaces (default)",
			phaseCapability{ExecutorEffort: "max"},
			"[E:(default)/max]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := capabilityBadge(tc.cap); got != tc.want {
				t.Fatalf("capabilityBadge:\n got=%q\nwant=%q", got, tc.want)
			}
		})
	}
}

func TestDirectorPanel_PhaseBriefed_RendersCapabilityBadge(t *testing.T) {
	d := directorPanel{}
	d.onPhasePlanned(samplePlan())
	d.onPhaseBriefed("p1", 1, &supervision.Briefing{IterationID: "p1-1", PhaseID: "p1"}, phaseCapability{
		ExecutorModel:  "claude-opus-4-7",
		ExecutorEffort: "high",
		BrieferSkipped: true,
	})
	out := d.view(120)
	if !strings.Contains(out, "E:opus-4-7/high") {
		t.Errorf("executor badge missing in panel:\n%s", out)
	}
	if !strings.Contains(out, "B:skip") {
		t.Errorf("briefer skip badge missing in panel:\n%s", out)
	}
}

func TestDirectorPanel_FreshPlan_RendersChecklist(t *testing.T) {
	d := directorPanel{}
	d.onPhasePlanned(samplePlan())
	out := d.view(80)
	for _, want := range []string{"phases: 0/3", "Domain types", "Adapter scaffold", "TUI panel"} {
		if !strings.Contains(out, want) {
			t.Errorf("fresh plan view missing %q\n%s", want, out)
		}
	}
	for _, ph := range []string{"p1", "p2", "p3"} {
		if d.phaseStatus[ph] != phasePending {
			t.Errorf("phase %s should start pending; got %v", ph, d.phaseStatus[ph])
		}
	}
}

func TestDirectorPanel_PhaseInProgress_HighlightsActive(t *testing.T) {
	d := directorPanel{}
	d.onPhasePlanned(samplePlan())
	d.onPhaseBriefed("p1", 1, &supervision.Briefing{IterationID: "p1-1", PhaseID: "p1"}, phaseCapability{})
	if d.currentPhaseID != "p1" {
		t.Errorf("currentPhaseID = %q, want p1", d.currentPhaseID)
	}
	if d.phaseStatus["p1"] != phaseInProgress {
		t.Errorf("p1 status = %v, want phaseInProgress", d.phaseStatus["p1"])
	}
	out := d.view(80)
	if !strings.Contains(out, "iter 1") {
		t.Errorf("active phase row missing 'iter 1'\n%s", out)
	}
}

func TestDirectorPanel_PhaseBriefed_SecondIterationRendersIter2(t *testing.T) {
	d := directorPanel{}
	d.onPhasePlanned(samplePlan())
	d.onPhaseBriefed("p1", 1, &supervision.Briefing{IterationID: "p1-01", PhaseID: "p1"}, phaseCapability{})
	d.onPhaseBriefed("p1", 2, &supervision.Briefing{IterationID: "p1-02", PhaseID: "p1"}, phaseCapability{})
	if d.currentIteration != 2 || d.currentAttempt != 0 {
		t.Errorf("after second brief: iteration=%d attempt=%d, want 2/0",
			d.currentIteration, d.currentAttempt)
	}
	out := d.view(80)
	if !strings.Contains(out, "iter 2") {
		t.Errorf("active phase row missing 'iter 2'\n%s", out)
	}
}

func TestDirectorPanel_PhaseReviewed_AfterBriefShowsIterAndAttempt(t *testing.T) {
	d := directorPanel{}
	d.onPhasePlanned(samplePlan())
	d.onPhaseBriefed("p1", 1, &supervision.Briefing{IterationID: "p1-01", PhaseID: "p1"}, phaseCapability{})
	d.onPhaseReviewed("p1", 2, "revise")
	out := d.view(80)
	if !strings.Contains(out, "iter 1 · attempt 2") {
		t.Errorf("active phase row should combine iteration and attempt; got\n%s", out)
	}
}

func TestDirectorPanel_PhaseEscalated_MarksRow(t *testing.T) {
	d := directorPanel{}
	d.onPhasePlanned(samplePlan())
	d.onPhaseBriefed("p2", 1, nil, phaseCapability{})
	d.onEscalation("p2", 3, "reviewer kept rejecting changes")
	if d.phaseStatus["p2"] != phaseEscalated {
		t.Errorf("p2 status = %v, want phaseEscalated", d.phaseStatus["p2"])
	}
	if !d.escalation || d.escalationFor != "p2" || d.escalationAttempt != 3 {
		t.Errorf("escalation latch = (%v, %q, %d), want (true, p2, 3)",
			d.escalation, d.escalationFor, d.escalationAttempt)
	}
	out := d.view(80)
	if !strings.Contains(out, "[!]") {
		t.Errorf("escalated phase row should carry [!]; got\n%s", out)
	}
}

func TestDirectorPanel_AllApproved_ShowsFullCount(t *testing.T) {
	d := directorPanel{}
	plan := samplePlan()
	d.onPhasePlanned(plan)
	for _, ph := range plan.Phases {
		d.onPhaseReviewed(ph.ID, 1, "approve")
	}
	out := d.view(80)
	if !strings.Contains(out, "phases: 3/3") {
		t.Errorf("expected '3/3 approved'; got\n%s", out)
	}
	if !strings.Contains(out, "verdict:") || !strings.Contains(out, "approve") {
		t.Errorf("expected verdict label and 'approve' value; got\n%s", out)
	}
	for _, ph := range plan.Phases {
		if d.phaseStatus[ph.ID] != phaseApproved {
			t.Errorf("phase %s status = %v, want phaseApproved",
				ph.ID, d.phaseStatus[ph.ID])
		}
	}
}

func TestDirectorPanel_OnSpawnFinished_AccumulatesAcrossRoles(t *testing.T) {
	d := directorPanel{}
	d.onSpawnFinished("bcc-planner", 0.10)
	d.onSpawnFinished("executor", 0.25)
	d.onSpawnFinished("executor", 0.05)
	if d.cumulativeCost < 0.39 || d.cumulativeCost > 0.41 {
		t.Errorf("cumulativeCost = %f, want ~0.40", d.cumulativeCost)
	}
	if d.costByRole["planner"] != 0.10 {
		t.Errorf("costByRole[planner] = %f, want 0.10 (bcc- prefix stripped)", d.costByRole["planner"])
	}
	if d.costByRole["executor"] < 0.29 || d.costByRole["executor"] > 0.31 {
		t.Errorf("costByRole[executor] = %f, want ~0.30", d.costByRole["executor"])
	}
}

func TestDirectorPanel_PlanningTrack_RendersFirstEntry(t *testing.T) {
	d := directorPanel{}
	d.onPhasePlanned(samplePlan())
	d.onTaskStarted(planningTaskID)
	out := d.view(80)
	if !strings.Contains(out, "planning") {
		t.Errorf("planning row missing\n%s", out)
	}
	if d.planningStatus != phaseInProgress {
		t.Errorf("planningStatus = %v, want phaseInProgress", d.planningStatus)
	}
	d.onTaskCompleted(planningTaskID)
	if d.planningStatus != phaseApproved {
		t.Errorf("planningStatus = %v, want phaseApproved", d.planningStatus)
	}
	if !strings.Contains(d.view(80), "planning") {
		t.Errorf("planning row missing after completion\n%s", d.view(80))
	}

	// Planning row appears before any phase title.
	finalOut := d.view(80)
	planIdx := strings.Index(finalOut, "planning")
	phaseIdx := strings.Index(finalOut, "Domain types")
	if planIdx < 0 || phaseIdx < 0 || planIdx > phaseIdx {
		t.Errorf("planning row should precede first phase row\n%s", finalOut)
	}
}

func TestDirectorPanel_SubDAGHighlight_RendersUnderActivePhase(t *testing.T) {
	d := directorPanel{}
	d.onPhasePlanned(samplePlan())
	d.onPhaseBriefed("p2", 1, &supervision.Briefing{
		IterationID:   "p2-1",
		PhaseID:       "p2",
		SubDAGTaskIDs: []string{"p2.t1", "p2.t2"},
	}, phaseCapability{})
	out := d.view(80)
	for _, want := range []string{"p2.t1", "p2.t2"} {
		if !strings.Contains(out, want) {
			t.Errorf("sub-DAG row %q missing\n%s", want, out)
		}
	}

	// Switching the active phase to one without sub-DAG drops the rows.
	d.onPhaseBriefed("p3", 1, &supervision.Briefing{
		IterationID:   "p3-1",
		PhaseID:       "p3",
		SubDAGTaskIDs: nil,
	}, phaseCapability{})
	out = d.view(80)
	if strings.Contains(out, "p2.t1") {
		t.Errorf("stale sub-DAG row should be cleared\n%s", out)
	}
}

func TestUpdate_TaskEventsUpdatePlanningTrack(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	at := time.Date(2026, 5, 2, 14, 0, 0, 0, time.UTC)

	got, _ := m.Update(eventMsg{ev: loop.PhasePlanned{Plan: samplePlan(), At: at}})
	mm := got.(Model)
	got, _ = mm.Update(eventMsg{ev: loop.TaskStarted{TaskID: planningTaskID, At: at.Add(time.Millisecond)}})
	mm = got.(Model)
	if mm.director.planningStatus != phaseInProgress {
		t.Errorf("planningStatus after TaskStarted = %v, want phaseInProgress", mm.director.planningStatus)
	}
	got, _ = mm.Update(eventMsg{ev: loop.TaskCompleted{TaskID: planningTaskID, At: at.Add(2 * time.Millisecond)}})
	mm = got.(Model)
	if mm.director.planningStatus != phaseApproved {
		t.Errorf("planningStatus after TaskCompleted = %v, want phaseApproved", mm.director.planningStatus)
	}
}

func TestRenderEscalationModal_HiddenWhenNotEscalated(t *testing.T) {
	d := directorPanel{}
	got := renderEscalationModal(d, defaultDirectorKeyMap())
	if got != "" {
		t.Errorf("modal should be empty when not escalated; got %q", got)
	}
}

func TestRenderEscalationModal_ShowsReasoningAndKeyHints(t *testing.T) {
	d := directorPanel{}
	d.onEscalation("p1", 2, "the diff dropped acceptance #4")
	out := renderEscalationModal(d, defaultDirectorKeyMap())
	for _, want := range []string{"escalation on p1", "the diff dropped", "[R]", "[F]", "[S]", "[A]"} {
		if !strings.Contains(out, want) {
			t.Errorf("modal missing %q\n%s", want, out)
		}
	}
}

func TestUpdate_DirectorEventsFlowToPanel(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	at := time.Date(2026, 5, 2, 14, 0, 0, 0, time.UTC)

	got, _ := m.Update(eventMsg{ev: loop.PhasePlanned{Plan: samplePlan(), At: at}})
	mm := got.(Model)
	if !mm.director.active() {
		t.Errorf("PhasePlanned did not activate the director panel")
	}

	got, _ = mm.Update(eventMsg{ev: loop.PhaseBriefed{
		PhaseID: "p1", Iteration: 1,
		Briefing: &supervision.Briefing{IterationID: "p1-1", PhaseID: "p1"},
		At:       at.Add(time.Second),
	}})
	mm = got.(Model)
	if mm.director.currentPhaseID != "p1" {
		t.Errorf("currentPhaseID = %q, want p1", mm.director.currentPhaseID)
	}

	got, _ = mm.Update(eventMsg{ev: loop.PhaseReviewed{
		PhaseID: "p1", Attempt: 1,
		Outcome: "approve",
		At:      at.Add(2 * time.Second),
	}})
	mm = got.(Model)
	if mm.director.phaseStatus["p1"] != phaseApproved {
		t.Errorf("p1 status = %v, want phaseApproved", mm.director.phaseStatus["p1"])
	}

	got, _ = mm.Update(eventMsg{ev: loop.DirectorEscalation{
		PhaseID: "p2", Attempt: 3, Reasoning: "stalled", At: at.Add(3 * time.Second),
	}})
	mm = got.(Model)
	if !mm.director.escalation {
		t.Errorf("DirectorEscalation did not latch escalation modal")
	}
}

func TestUpdate_SpawnFinished_FoldsCostIntoDirectorPanel(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	at := time.Date(2026, 5, 2, 14, 30, 0, 0, time.UTC)
	got, _ := m.Update(eventMsg{ev: loop.SpawnFinished{
		Role: "executor",
		Cost: loop.SpawnCost{USD: 0.42},
		At:   at,
	}})
	mm := got.(Model)
	if mm.director.cumulativeCost < 0.41 || mm.director.cumulativeCost > 0.43 {
		t.Errorf("supervision.cumulativeCost = %f, want ~0.42", mm.director.cumulativeCost)
	}
	if mm.director.costByRole["executor"] < 0.41 || mm.director.costByRole["executor"] > 0.43 {
		t.Errorf("costByRole[executor] = %f, want ~0.42", mm.director.costByRole["executor"])
	}
}

func TestUpdate_EscalationModalRoutesKeysToGate(t *testing.T) {
	cases := []struct {
		key  string
		want loop.EscalationReply
	}{
		{"f", loop.EscalationReply{Kind: loop.EscalationForceApprove}},
		{"s", loop.EscalationReply{Kind: loop.EscalationSkip}},
		{"a", loop.EscalationReply{Kind: loop.EscalationAbort}},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			gate := make(chan loop.EscalationReply, 1)
			ts := newTestSvc(t)
			m := New(Options{
				Services:       ts.Svc,
				SessionID:      ts.SessionID,
				Cancel:         func() {},
				Gate:           NewGate(),
				SpecPath:       "spec.md",
				EscalationGate: gate,
			})
			m, _ = asModel(m.Update(eventMsg{ev: loop.PhasePlanned{Plan: samplePlan()}}))
			m, _ = asModel(m.Update(eventMsg{ev: loop.DirectorEscalation{
				PhaseID: "p1", Attempt: 2, Reasoning: "needs revisit",
			}}))
			if !m.director.escalation {
				t.Fatalf("setup: modal not latched")
			}
			m, _ = asModel(m.Update(keyPress(tc.key)))
			if m.director.escalation {
				t.Errorf("modal should clear after %q", tc.key)
			}
			select {
			case got := <-gate:
				if got != tc.want {
					t.Errorf("gate received %+v, want %+v", got, tc.want)
				}
			default:
				t.Errorf("gate received no reply for %q", tc.key)
			}
		})
	}
}

// TestUpdate_EscalationResumeOpensHintInput verifies the two-step flow
// for [R]: pressing R transitions to the hint input state without
// dispatching, typing fills the hint, and Enter packages the hint into
// an EscalationResume reply on the gate.
func TestUpdate_EscalationResumeOpensHintInput(t *testing.T) {
	gate := make(chan loop.EscalationReply, 1)
	ts := newTestSvc(t)
	m := New(Options{
		Services:       ts.Svc,
		SessionID:      ts.SessionID,
		Cancel:         func() {},
		Gate:           NewGate(),
		SpecPath:       "spec.md",
		EscalationGate: gate,
	})
	m, _ = asModel(m.Update(eventMsg{ev: loop.PhasePlanned{Plan: samplePlan()}}))
	m, _ = asModel(m.Update(eventMsg{ev: loop.DirectorEscalation{
		PhaseID: "p1", Attempt: 2, Reasoning: "needs revisit",
	}}))
	m, _ = asModel(m.Update(keyPress("r")))
	if m.director.escalationState != escalationStateHintInput {
		t.Fatalf("escalationState = %v, want hint input", m.director.escalationState)
	}
	if !m.director.escalation {
		t.Fatal("escalation modal cleared on R; expected to remain latched")
	}
	select {
	case <-gate:
		t.Fatal("gate received reply before user submitted hint")
	default:
	}
	for _, r := range "tighten" {
		m, _ = asModel(m.Update(keyPress(string(r))))
	}
	m, _ = asModel(m.Update(keyPress("enter")))
	if m.director.escalation {
		t.Errorf("escalation modal should clear after enter")
	}
	select {
	case got := <-gate:
		want := loop.EscalationReply{Kind: loop.EscalationResume, Hint: "tighten"}
		if got != want {
			t.Errorf("gate reply = %+v, want %+v", got, want)
		}
	default:
		t.Errorf("gate received no reply on enter")
	}
}

// TestUpdate_EscalationHintEscReturnsToChoosing verifies that Esc
// during hint input cancels back to the choosing state without
// touching the gate.
func TestUpdate_EscalationHintEscReturnsToChoosing(t *testing.T) {
	gate := make(chan loop.EscalationReply, 1)
	ts := newTestSvc(t)
	m := New(Options{
		Services:       ts.Svc,
		SessionID:      ts.SessionID,
		Cancel:         func() {},
		Gate:           NewGate(),
		SpecPath:       "spec.md",
		EscalationGate: gate,
	})
	m, _ = asModel(m.Update(eventMsg{ev: loop.PhasePlanned{Plan: samplePlan()}}))
	m, _ = asModel(m.Update(eventMsg{ev: loop.DirectorEscalation{
		PhaseID: "p1", Attempt: 2, Reasoning: "needs revisit",
	}}))
	m, _ = asModel(m.Update(keyPress("r")))
	m, _ = asModel(m.Update(keyPress("esc")))
	if m.director.escalationState != escalationStateChoosing {
		t.Errorf("escalationState = %v, want choosing", m.director.escalationState)
	}
	select {
	case got := <-gate:
		t.Fatalf("gate should be empty; got %+v", got)
	default:
	}
}

func TestUpdate_EscalationModalIgnoresUnboundKeys(t *testing.T) {
	gate := make(chan loop.EscalationReply, 1)
	ts := newTestSvc(t)
	m := New(Options{
		Services:       ts.Svc,
		SessionID:      ts.SessionID,
		Cancel:         func() {},
		Gate:           NewGate(),
		SpecPath:       "spec.md",
		EscalationGate: gate,
	})
	m, _ = asModel(m.Update(eventMsg{ev: loop.PhasePlanned{Plan: samplePlan()}}))
	m, _ = asModel(m.Update(eventMsg{ev: loop.DirectorEscalation{
		PhaseID: "p1", Attempt: 2, Reasoning: "needs revisit",
	}}))
	m, _ = asModel(m.Update(keyPress("x")))
	if !m.director.escalation {
		t.Errorf("unbound keys should not clear the modal")
	}
	select {
	case got := <-gate:
		t.Errorf("gate should be empty; got %v", got)
	default:
	}
}

func TestView_DirectorPanelHiddenForLegacyRun(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	out := mm.(Model).View().Content
	if strings.Contains(out, "[ director ]") || strings.Contains(out, "director cost") {
		t.Errorf("director panel must stay hidden when no plan was planned\n%s", out)
	}
}

func TestView_DirectorPanelVisibleAfterPhasePlanned(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm, _ = mm.(Model).Update(eventMsg{ev: loop.PhasePlanned{Plan: samplePlan()}})
	out := mm.(Model).View().Content
	if !strings.Contains(out, "director") {
		t.Errorf("director panel title missing after PhasePlanned\n%s", out)
	}
	if !strings.Contains(out, "phases: 0/3") {
		t.Errorf("director panel did not render phase summary\n%s", out)
	}
	if !strings.Contains(out, "director cost: $0.00") {
		t.Errorf("director cost line missing from health panel\n%s", out)
	}
}

// TestUpdate_PlanSkippedLatchesNothingToDoMode covers the friendly
// terminal screen for the planner-skip path: planSkippedMsg latches
// the mode + reason, clears planning placeholders, and the channel
// close that follows on the wire does not schedule tea.Quit.
func TestUpdate_PlanSkippedLatchesNothingToDoMode(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	m.planningPending = true
	got, _ := m.Update(planSkippedMsg{reason: "spec is complete: 18/18 acceptance bullets done"})
	mm := got.(Model)
	if !mm.nothingToDoMode {
		t.Fatal("nothingToDoMode not latched after planSkippedMsg")
	}
	if mm.nothingToDoReason != "spec is complete: 18/18 acceptance bullets done" {
		t.Errorf("nothingToDoReason = %q, want the reason from planSkippedMsg", mm.nothingToDoReason)
	}
	if mm.planningPending {
		t.Error("planningPending should be cleared after planSkippedMsg")
	}

	got2, cmd := mm.Update(eventMsg{closed: true})
	if cmd != nil {
		t.Errorf("nothing-to-do mode must swallow channel close; got cmd %v", cmd)
	}
	mm2 := got2.(Model)
	if !mm2.finished {
		t.Error("finished should be true after channel close")
	}
	if !mm2.nothingToDoMode {
		t.Error("nothingToDoMode dropped after channel close; want sticky")
	}
}

func TestView_NothingToDoRendersFriendlyScreen(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	mm0, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm1, _ := mm0.(Model).Update(planSkippedMsg{reason: "every Done bullet is checked off"})
	out := mm1.(Model).View().Content
	for _, want := range []string{
		"bcc: nothing to do",
		"every Done bullet is checked off",
		"press [q] or Ctrl+C to exit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("nothing-to-do view missing %q\n%s", want, out)
		}
	}
}

func TestUpdate_NothingToDoQuitOnly(t *testing.T) {
	m, _, _, cancelled := newTestModel(t)
	mm0, _ := m.Update(planSkippedMsg{reason: "done"})
	m = mm0.(Model)

	if _, cmd := m.Update(keyPress(" ")); cmd != nil {
		t.Errorf("space (pause) must be a no-op in nothing-to-do mode; got cmd")
	}
	if _, cmd := m.Update(keyPress("r")); cmd != nil {
		t.Errorf("r must be a no-op in nothing-to-do mode; got cmd")
	}
	if *cancelled {
		t.Error("cancel callback fired for non-quit key")
	}

	_, cmd := m.Update(keyPress("q"))
	if cmd == nil {
		t.Error("q must trigger tea.Quit in nothing-to-do mode")
	}
}

// TestUpdate_PlanFailedSurfacesErrorIntoSession verifies the planner
// crash path: planFailedMsg latches the underlying error in the
// session footer and clears any planning placeholders. Combined with a
// LoopFinished{Reason:"planner_failed"} from the host, the dashboard
// stays alive in session mode so the user reads the error before
// pressing [r]/[e]/[q].
func TestUpdate_PlanFailedSurfacesErrorIntoSession(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	m.planningPending = true

	got, _ := m.Update(planFailedMsg{message: "claude exited 1: out of credits"})
	mm := got.(Model)
	if mm.planningPending {
		t.Error("planningPending should clear after planFailedMsg")
	}
	if !strings.Contains(mm.sessionExitMsg, "out of credits") {
		t.Errorf("sessionExitMsg = %q, want it to surface the planner error", mm.sessionExitMsg)
	}

	got2, _ := mm.Update(eventMsg{ev: loop.LoopFinished{Reason: "planner_failed"}})
	mm2 := got2.(Model)
	if !mm2.sessionMode {
		t.Fatal("sessionMode not latched after LoopFinished{planner_failed}")
	}
	if mm2.sessionReason != "planner_failed" {
		t.Errorf("sessionReason = %q, want %q", mm2.sessionReason, "planner_failed")
	}
}

func TestView_PlanFailedSessionShowsErrorAndStaysAlive(t *testing.T) {
	m, _, _, _ := newTestModel(t)
	mm0, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm1, _ := mm0.(Model).Update(planFailedMsg{message: "claude exited 1: out of credits"})
	mm2, cmd := mm1.(Model).Update(eventMsg{ev: loop.LoopFinished{Reason: "planner_failed"}})
	if cmd != nil {
		// readEventCmd will return one cmd; we only care about latching, not the cmd shape.
		_ = cmd
	}
	out := mm2.(Model).View().Content
	for _, want := range []string{
		"planner failed",
		"out of credits",
		"r resume",
		"q exit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("planner-failed session view missing %q\n%s", want, out)
		}
	}
}

// asModel narrows tea.Model to the concrete tui.Model. Test setups that
// chain multiple Update calls use this to avoid sprinkling type
// assertions through every step.
func asModel(m tea.Model, cmd tea.Cmd) (Model, tea.Cmd) {
	return m.(Model), cmd
}
