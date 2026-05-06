package director

import (
	"errors"
	"fmt"
	"time"
)

// Plan is the Director's canonical output at the start of a run: a
// distilled goal, spec-level success criteria, and a graph of typed
// Phases each owning its own task DAG. Plans are persisted to
// .bcc/sessions/<id>/plan.json and survive across resumes; SpecHash
// anchors a Plan to a specific spec content snapshot so the loop can
// detect drift and re-plan when needed.
type Plan struct {
	Goal            string    `json:"goal"`
	SuccessCriteria []string  `json:"success_criteria"`
	Phases          []Phase   `json:"phases"`
	SpecHash        string    `json:"spec_hash"`
	PlannedAt       time.Time `json:"planned_at"`
}

// Phase is a coarse-grained unit of work in a Plan. IDs are stable
// across re-plans (see PhaseID in ids.go) so DAG state collected
// against a previous plan version stays addressable. A Phase owns its
// task DAG; cross-phase task dependencies are not representable.
//
// The optional capability fields (BrieferAssignment,
// ExecutorAssignment, ReviewerAssignment, PreparedBriefing,
// SkipReview) carry the Planner's per-phase routing choices: which
// model and effort each role uses, an inline Briefing that lets the
// loop skip the Briefer agent, and an opt-out from the Reviewer agent
// for trivial phases. Each field is independent; absent fields fall
// back to the configured defaults and the regular Briefer/Reviewer
// flow.
type Phase struct {
	ID                 string            `json:"id"`
	Title              string            `json:"title"`
	Intent             string            `json:"intent"`
	DependsOn          []string          `json:"depends_on"`
	Parallelizable     bool              `json:"parallelizable"`
	Priority           int               `json:"priority,omitempty"`
	ScopeIn            []string          `json:"scope_in"`
	ScopeOut           []string          `json:"scope_out"`
	Tasks              []Task            `json:"tasks"`
	BrieferAssignment  *RoleAssignment   `json:"briefer_assignment,omitempty"`
	ExecutorAssignment *RoleAssignment   `json:"executor_assignment,omitempty"`
	ReviewerAssignment *RoleAssignment   `json:"reviewer_assignment,omitempty"`
	PreparedBriefing   *PreparedBriefing `json:"prepared_briefing,omitempty"`
	// SkipReview, when true, instructs the loop to mark every sub-DAG
	// task done synthetically after the Executor completes the
	// iteration, recording the approval under role "planner" in the
	// audit log instead of spawning the Reviewer agent. The Planner is
	// responsible for using this only on phases where the Executor's
	// output is mechanically verifiable (a rename, a flag flip, a
	// trivial wiring change); the loop has no separate gate to second-
	// guess the call.
	SkipReview bool `json:"skip_review,omitempty"`
}

// Task is the atomic unit of progress inside a Phase. Tasks own their
// acceptance criteria, intra-phase dependencies, priority, status, and
// retry budget. Task IDs are unique within their owning Phase but not
// globally; addressing a task across the wire uses the (phase_id,
// task_id) pair.
type Task struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Intent      string           `json:"intent"`
	DependsOn   []string         `json:"depends_on"`
	Priority    int              `json:"priority,omitempty"`
	Acceptance  []AcceptanceItem `json:"acceptance"`
	Status      TaskStatus       `json:"status"`
	RetryBudget int              `json:"retry_budget"`
}

// AcceptanceItem is a single checkable criterion attached to a Task.
// Evidence declares how the Reviewer checks it (diff inspection, test
// run, build, or human-judged manual review).
type AcceptanceItem struct {
	ID          string       `json:"id"`
	Description string       `json:"description"`
	Evidence    EvidenceKind `json:"evidence"`
}

// RoleAssignment carries the Planner's per-phase routing for one role
// (Briefer, Executor, or Reviewer). Model picks a member of the
// CapabilityRegistry exposed at planning time; Effort, when present,
// must be supported by that model. Provider names the vendor that owns
// the model (e.g. "claude") and is derived from the registry at
// bcc_plan_emit time so the persisted plan and the SPA can render
// vendor/model/effort without re-resolving the registry. Empty fields
// fall back to the configured default for the corresponding role; the
// loop and adapters never invent a value.
type RoleAssignment struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

// PreparedBriefing lets the Planner skip the Briefer agent for a phase
// it already understands well enough to brief inline. When present, the
// loop synthesizes a Briefing from these fields and records it in the
// audit log under role "planner" instead of spawning a Briefer
// subprocess; on retry the loop reuses the same instructions and
// prepends the Reviewer's prior_feedback automatically.
type PreparedBriefing struct {
	SubDAGTaskIDs []string `json:"sub_dag_task_ids"`
	Instructions  string   `json:"instructions"`
}

// Briefing is the Briefer's per-iteration instruction set for one
// Executor. The Briefer picks a sub-DAG of tasks within a single
// eligible phase and emits the Briefing through bcc_briefing_emit; the
// loop persists it and renders the Executor prompt from it.
type Briefing struct {
	IterationID   string   `json:"iteration_id"`
	PhaseID       string   `json:"phase_id"`
	SubDAGTaskIDs []string `json:"sub_dag_task_ids"`
	Instructions  string   `json:"instructions"`
	SpecPath      string   `json:"spec_path"`
	PriorFeedback *string  `json:"prior_feedback,omitempty"`
}

// PhaseByID returns a pointer to the Phase whose ID matches id, or nil
// when no phase matches. The pointer is into the Plan's slice; callers
// must not retain it across mutations of Plan.Phases.
func (p *Plan) PhaseByID(id string) *Phase {
	if p == nil {
		return nil
	}
	for i := range p.Phases {
		if p.Phases[i].ID == id {
			return &p.Phases[i]
		}
	}
	return nil
}

// AssignmentFor returns the Phase's RoleAssignment for the named role,
// or nil when the Planner did not attribute one. Accepted role names
// are "briefer", "executor", and "reviewer"; any other value yields
// nil. The returned pointer aliases the Phase field; callers must not
// mutate it.
func (p *Phase) AssignmentFor(role string) *RoleAssignment {
	if p == nil {
		return nil
	}
	switch role {
	case "briefer":
		return p.BrieferAssignment
	case "executor":
		return p.ExecutorAssignment
	case "reviewer":
		return p.ReviewerAssignment
	}
	return nil
}

// TaskByID returns a pointer to the Task within the Phase whose ID
// matches id, or nil when no task matches. Task IDs are scoped to the
// owning phase: the same id can repeat across different phases.
func (p *Phase) TaskByID(id string) *Task {
	if p == nil {
		return nil
	}
	for i := range p.Tasks {
		if p.Tasks[i].ID == id {
			return &p.Tasks[i]
		}
	}
	return nil
}

// ValidatePlan returns nil when the Plan is structurally well-formed
// for execution. The validator enforces the two-level DAG invariants
// that PRD 5 defines: phase-id uniqueness, per-phase task-id
// uniqueness, phase-level deps resolving to existing phases,
// task-level deps resolving to task ids within the same phase, and
// acyclic edge sets at both levels. Failures carry the offending ids
// so the Planner can correct and re-emit.
//
// When registry is non-nil the validator additionally rejects per-phase
// role assignments whose Model is not in the registry or whose Effort
// is not among the model's supported levels, and PreparedBriefings
// referencing tasks not in the owning phase. nil registry skips those
// checks; tests that drive ValidatePlan without a configured registry
// rely on this.
func ValidatePlan(p *Plan, registry *CapabilityRegistry) error {
	if p == nil {
		return errors.New("director: nil plan")
	}
	if p.Goal == "" {
		return errors.New("director: plan has empty goal")
	}
	if len(p.Phases) == 0 {
		return errors.New("director: plan has no phases")
	}

	phaseIDs := make(map[string]struct{}, len(p.Phases))
	for i, ph := range p.Phases {
		if ph.ID == "" {
			return fmt.Errorf("director: phase %d has empty id", i)
		}
		if _, dup := phaseIDs[ph.ID]; dup {
			return fmt.Errorf("director: duplicate phase id %q", ph.ID)
		}
		phaseIDs[ph.ID] = struct{}{}
	}

	for _, ph := range p.Phases {
		if len(ph.Tasks) == 0 {
			return fmt.Errorf("director: phase %q has no tasks", ph.ID)
		}
		for _, dep := range ph.DependsOn {
			if _, ok := phaseIDs[dep]; !ok {
				return fmt.Errorf("director: phase %q depends on unknown phase %q", ph.ID, dep)
			}
		}
		taskIDs := make(map[string]struct{}, len(ph.Tasks))
		for j, t := range ph.Tasks {
			if t.ID == "" {
				return fmt.Errorf("director: phase %q task %d has empty id", ph.ID, j)
			}
			if _, dup := taskIDs[t.ID]; dup {
				return fmt.Errorf("director: phase %q has duplicate task id %q", ph.ID, t.ID)
			}
			taskIDs[t.ID] = struct{}{}
		}
		for _, t := range ph.Tasks {
			for _, dep := range t.DependsOn {
				if _, ok := taskIDs[dep]; !ok {
					return fmt.Errorf("director: phase %q task %q depends on unknown task %q", ph.ID, t.ID, dep)
				}
			}
		}
		if err := detectTaskCycle(ph); err != nil {
			return err
		}
		if err := validatePhaseCapabilityFields(ph, registry); err != nil {
			return err
		}
	}

	return detectPhaseCycle(p)
}

// validatePhaseCapabilityFields enforces the capability-aware fields on
// a Phase: per-role assignments must reference a model in the registry
// (and an effort that model supports, when set), and a PreparedBriefing
// must list at least one task and only tasks owned by the phase. A nil
// registry skips the model/effort checks but still validates the
// PreparedBriefing structure since it does not depend on the registry.
func validatePhaseCapabilityFields(ph Phase, registry *CapabilityRegistry) error {
	taskIDs := make(map[string]struct{}, len(ph.Tasks))
	for _, t := range ph.Tasks {
		taskIDs[t.ID] = struct{}{}
	}

	type roleSlot struct {
		name       string
		assignment *RoleAssignment
	}
	for _, slot := range []roleSlot{
		{"briefer", ph.BrieferAssignment},
		{"executor", ph.ExecutorAssignment},
		{"reviewer", ph.ReviewerAssignment},
	} {
		if slot.assignment == nil {
			continue
		}
		if registry == nil {
			continue
		}
		if slot.assignment.Model != "" {
			if _, ok := registry.ByModel(slot.assignment.Model); !ok {
				return fmt.Errorf("director: phase %q %s_assignment model %q not in capability registry",
					ph.ID, slot.name, slot.assignment.Model)
			}
			if slot.assignment.Effort != "" && !registry.SupportsEffort(slot.assignment.Model, slot.assignment.Effort) {
				return fmt.Errorf("director: phase %q %s_assignment effort %q not supported by model %q",
					ph.ID, slot.name, slot.assignment.Effort, slot.assignment.Model)
			}
		} else if slot.assignment.Effort != "" {
			return fmt.Errorf("director: phase %q %s_assignment has effort %q without model",
				ph.ID, slot.name, slot.assignment.Effort)
		}
	}

	if pb := ph.PreparedBriefing; pb != nil {
		if pb.Instructions == "" {
			return fmt.Errorf("director: phase %q prepared_briefing has empty instructions", ph.ID)
		}
		if len(pb.SubDAGTaskIDs) == 0 {
			return fmt.Errorf("director: phase %q prepared_briefing has empty sub_dag_task_ids", ph.ID)
		}
		for _, tid := range pb.SubDAGTaskIDs {
			if _, ok := taskIDs[tid]; !ok {
				return fmt.Errorf("director: phase %q prepared_briefing references unknown task %q", ph.ID, tid)
			}
		}
	}
	return nil
}

// detectPhaseCycle runs DFS three-color marking on the phase-level DAG
// and returns a structured error naming the cycle entry.
func detectPhaseCycle(p *Plan) error {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(p.Phases))
	deps := make(map[string][]string, len(p.Phases))
	for _, ph := range p.Phases {
		deps[ph.ID] = ph.DependsOn
	}
	var visit func(id string) error
	visit = func(id string) error {
		switch color[id] {
		case gray:
			return fmt.Errorf("director: cycle in phase DAG at %q", id)
		case black:
			return nil
		}
		color[id] = gray
		for _, dep := range deps[id] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		color[id] = black
		return nil
	}
	for _, ph := range p.Phases {
		if err := visit(ph.ID); err != nil {
			return err
		}
	}
	return nil
}

// detectTaskCycle runs DFS three-color marking on the task-level DAG of
// a single phase and returns a structured error naming the cycle entry.
func detectTaskCycle(ph Phase) error {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(ph.Tasks))
	deps := make(map[string][]string, len(ph.Tasks))
	for _, t := range ph.Tasks {
		deps[t.ID] = t.DependsOn
	}
	var visit func(id string) error
	visit = func(id string) error {
		switch color[id] {
		case gray:
			return fmt.Errorf("director: cycle in task DAG of phase %q at %q", ph.ID, id)
		case black:
			return nil
		}
		color[id] = gray
		for _, dep := range deps[id] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		color[id] = black
		return nil
	}
	for _, t := range ph.Tasks {
		if err := visit(t.ID); err != nil {
			return err
		}
	}
	return nil
}
