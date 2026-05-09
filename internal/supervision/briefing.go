package supervision

import (
	"errors"
	"fmt"
)

// BriefingFor assembles a BrieferInput for (phaseID, iteration). The
// caller supplies the Plan, the spec path, and the per-iteration sub-DAG
// task ids (drawn from the live DAG state). PriorFeedback is the
// loop-supplied prose the next iteration should prepend (an escalation
// hint or a per-task feedback summary); empty on iteration 1.
func BriefingFor(plan *Plan, specPath, phaseID string, iteration int, subDAG []string, priorFeedback string) (*BrieferInput, error) {
	if plan == nil {
		return nil, errors.New("director: BriefingFor: nil plan")
	}
	if phaseID == "" {
		return nil, errors.New("director: BriefingFor: empty phase_id")
	}
	if iteration < 1 {
		return nil, fmt.Errorf("director: BriefingFor: iteration must be >= 1, got %d", iteration)
	}
	phase := plan.PhaseByID(phaseID)
	if phase == nil {
		return nil, fmt.Errorf("director: BriefingFor: phase %q not in plan", phaseID)
	}
	in := &BrieferInput{
		Plan:          plan,
		SpecPath:      specPath,
		IterationID:   fmt.Sprintf("%s-%02d", phaseID, iteration),
		PhaseID:       phaseID,
		SubDAGTaskIDs: append([]string(nil), subDAG...),
		PriorFeedback: priorFeedback,
	}
	return in, nil
}

// PendingTaskIDs returns the ids of every task in phase whose status
// is pending or needs_fix. Tasks already done are excluded so the
// resulting sub-DAG covers only outstanding work; an empty slice means
// the phase is fully done. Exposed for callers that build sub-DAGs
// directly from a Plan snapshot.
func PendingTaskIDs(phase *Phase) []string {
	out := make([]string, 0, len(phase.Tasks))
	for _, t := range phase.Tasks {
		if t.Status == TaskDone {
			continue
		}
		out = append(out, t.ID)
	}
	return out
}
