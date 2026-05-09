package dag

import "github.com/fgmacedo/buchecha/internal/supervision"

// SubDAGFullyDone reports whether every task id in subDAG within phaseID
// has status done. A missing phase or task id returns false. The
// Director loop uses this predicate as the inner-loop break condition.
func (s *State) SubDAGFullyDone(phaseID PhaseID, subDAG []TaskID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, ok := s.phases[phaseID]
	if !ok {
		return false
	}
	for _, tid := range subDAG {
		t, ok := ps.Tasks[tid]
		if !ok {
			return false
		}
		if t.Status != supervision.TaskDone {
			return false
		}
	}
	return true
}

// SubDAGAnyNeedsFix reports whether any task id in subDAG within phaseID
// has status needs_fix. The decider uses this to route revise outcomes.
func (s *State) SubDAGAnyNeedsFix(phaseID PhaseID, subDAG []TaskID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, ok := s.phases[phaseID]
	if !ok {
		return false
	}
	for _, tid := range subDAG {
		t, ok := ps.Tasks[tid]
		if !ok {
			continue
		}
		if t.Status == supervision.TaskNeedsFix {
			return true
		}
	}
	return false
}

// SubDAGStatuses returns a snapshot of the per-task status for the
// requested sub-DAG ids. Missing ids are omitted from the result.
func (s *State) SubDAGStatuses(phaseID PhaseID, subDAG []TaskID) map[TaskID]supervision.TaskStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[TaskID]supervision.TaskStatus, len(subDAG))
	ps, ok := s.phases[phaseID]
	if !ok {
		return out
	}
	for _, tid := range subDAG {
		if t, ok := ps.Tasks[tid]; ok {
			out[tid] = t.Status
		}
	}
	return out
}
