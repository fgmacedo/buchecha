// Package dag holds the in-memory DAG state and the MCP handler that
// validates and serves it. The state is the source of truth for every
// task's lifecycle (pending, in_progress, done, needs_fix) during a
// session; the handler is the only legal mutator.
//
// Layer boundaries (cross-cutting requirement #1 of the migration spec):
// dag is a sibling of internal/supervision and may import it; dag itself
// imports nothing under internal/executor, internal/loop, internal/cli,
// internal/tui. internal/supervision must not import dag.
package dag

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/fgmacedo/buchecha/internal/supervision"
)

// PhaseID and TaskID are nominal aliases that document intent at call
// sites without adding ceremony. The validators in supervision.ValidatePlan
// are the gatekeepers; dag trusts well-formed plans.
type (
	PhaseID = string
	TaskID  = string
)

// TaskState is the live status of one task in the DAG plus the
// retry budget the planner attached to it. dependsOn is captured at
// load so eligibility checks need no plan lookups.
type TaskState struct {
	ID          TaskID
	Status      supervision.TaskStatus
	DependsOn   []TaskID
	RetryBudget int
}

// PhaseState is the live state of one phase: its task table plus the
// phase-level dependency edges so dag.EligiblePhases can resolve
// readiness without going back to the plan.
type PhaseState struct {
	ID        PhaseID
	DependsOn []PhaseID
	Tasks     map[TaskID]*TaskState
	TaskOrder []TaskID
}

// State holds the run-wide DAG. Every mutation goes through a method
// that takes mu; concurrent reads via Snapshot or HasPending also take
// mu so a race-free copy is always observable.
type State struct {
	mu         sync.Mutex
	phases     map[PhaseID]*PhaseState
	phaseOrder []PhaseID
}

// NewStateFromPlan initializes the DAG with every task in pending
// status. The plan must already be valid (callers run
// supervision.ValidatePlan first).
func NewStateFromPlan(p *supervision.Plan) *State {
	s := &State{
		phases:     make(map[PhaseID]*PhaseState, len(p.Phases)),
		phaseOrder: make([]PhaseID, 0, len(p.Phases)),
	}
	for i := range p.Phases {
		ph := &p.Phases[i]
		ps := &PhaseState{
			ID:        ph.ID,
			DependsOn: append([]PhaseID(nil), ph.DependsOn...),
			Tasks:     make(map[TaskID]*TaskState, len(ph.Tasks)),
			TaskOrder: make([]TaskID, 0, len(ph.Tasks)),
		}
		for j := range ph.Tasks {
			t := &ph.Tasks[j]
			status := t.Status
			if status == "" {
				status = supervision.TaskPending
			}
			ps.Tasks[t.ID] = &TaskState{
				ID:          t.ID,
				Status:      status,
				DependsOn:   append([]TaskID(nil), t.DependsOn...),
				RetryBudget: t.RetryBudget,
			}
			ps.TaskOrder = append(ps.TaskOrder, t.ID)
		}
		s.phases[ph.ID] = ps
		s.phaseOrder = append(s.phaseOrder, ph.ID)
	}
	return s
}

// Snapshot returns a deep copy of the DAG. The copy is safe to share
// across goroutines and across MCP handler turns; mutations to it do
// not affect the live state.
func (s *State) Snapshot() *State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *State) snapshotLocked() *State {
	out := &State{
		phases:     make(map[PhaseID]*PhaseState, len(s.phases)),
		phaseOrder: append([]PhaseID(nil), s.phaseOrder...),
	}
	for id, ps := range s.phases {
		copyPS := &PhaseState{
			ID:        ps.ID,
			DependsOn: append([]PhaseID(nil), ps.DependsOn...),
			Tasks:     make(map[TaskID]*TaskState, len(ps.Tasks)),
			TaskOrder: append([]TaskID(nil), ps.TaskOrder...),
		}
		for tid, t := range ps.Tasks {
			copyT := *t
			copyT.DependsOn = append([]TaskID(nil), t.DependsOn...)
			copyPS.Tasks[tid] = &copyT
		}
		out.phases[id] = copyPS
	}
	return out
}

// PhaseOrder returns the phase ids in plan order. Callers must not
// mutate the returned slice.
func (s *State) PhaseOrder() []PhaseID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]PhaseID(nil), s.phaseOrder...)
}

// Phase returns a snapshot of one phase by id, or nil if unknown. The
// returned struct is independent of the live state.
func (s *State) Phase(id PhaseID) *PhaseState {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, ok := s.phases[id]
	if !ok {
		return nil
	}
	copyPS := &PhaseState{
		ID:        ps.ID,
		DependsOn: append([]PhaseID(nil), ps.DependsOn...),
		Tasks:     make(map[TaskID]*TaskState, len(ps.Tasks)),
		TaskOrder: append([]TaskID(nil), ps.TaskOrder...),
	}
	for tid, t := range ps.Tasks {
		copyT := *t
		copyT.DependsOn = append([]TaskID(nil), t.DependsOn...)
		copyPS.Tasks[tid] = &copyT
	}
	return copyPS
}

// SetTaskStatus assigns a new status to (phaseID, taskID). Returns an
// error if either id is unknown, or if the new status is not in the
// canonical set. The handler is expected to perform any cross-task
// invariant checks before calling this; SetTaskStatus is the leaf
// mutator.
func (s *State) SetTaskStatus(phaseID PhaseID, taskID TaskID, status supervision.TaskStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, ok := s.phases[phaseID]
	if !ok {
		return fmt.Errorf("dag: unknown phase %q", phaseID)
	}
	t, ok := ps.Tasks[taskID]
	if !ok {
		return fmt.Errorf("dag: unknown task %q in phase %q", taskID, phaseID)
	}
	switch status {
	case supervision.TaskPending, supervision.TaskInProgress, supervision.TaskDone, supervision.TaskNeedsFix:
	default:
		return fmt.Errorf("dag: invalid task status %q", string(status))
	}
	t.Status = status
	return nil
}

// HasPending reports whether any task in any phase is still pending or
// needs_fix. The outer loop drives on this predicate.
func (s *State) HasPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ps := range s.phases {
		for _, t := range ps.Tasks {
			if t.Status == supervision.TaskPending || t.Status == supervision.TaskNeedsFix {
				return true
			}
		}
	}
	return false
}

// PendingTasks returns the ids of tasks within phaseID whose status is
// pending or needs_fix, in plan-declared order. Returns nil if the
// phase is unknown or fully done.
func (s *State) PendingTasks(phaseID PhaseID) []TaskID {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, ok := s.phases[phaseID]
	if !ok {
		return nil
	}
	out := make([]TaskID, 0, len(ps.Tasks))
	for _, id := range ps.TaskOrder {
		t := ps.Tasks[id]
		if t.Status == supervision.TaskPending || t.Status == supervision.TaskNeedsFix {
			out = append(out, id)
		}
	}
	return out
}

// EligiblePhases returns ids of phases with at least one pending /
// needs_fix task whose phase-level dependencies are fully done. Result
// is in plan-declared order so the Briefer can pick the head as a
// stable next target.
func (s *State) EligiblePhases() []PhaseID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PhaseID, 0, len(s.phases))
	for _, id := range s.phaseOrder {
		ps := s.phases[id]
		if !phaseHasPending(ps) {
			continue
		}
		if !depsDoneLocked(s, ps.DependsOn) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func phaseHasPending(ps *PhaseState) bool {
	for _, t := range ps.Tasks {
		if t.Status == supervision.TaskPending || t.Status == supervision.TaskNeedsFix {
			return true
		}
	}
	return false
}

func depsDoneLocked(s *State, deps []PhaseID) bool {
	for _, dep := range deps {
		ps, ok := s.phases[dep]
		if !ok {
			return false
		}
		for _, t := range ps.Tasks {
			if t.Status != supervision.TaskDone {
				return false
			}
		}
	}
	return true
}

// dagFile is the on-disk DAG snapshot shape persisted to
// <sessionDir>/dag.json. Keeping the format separate from State lets
// the in-memory representation change without breaking the file format.
type dagFile struct {
	Phases []phaseFile `json:"phases"`
}

type phaseFile struct {
	ID        PhaseID    `json:"id"`
	DependsOn []PhaseID  `json:"depends_on,omitempty"`
	Tasks     []taskFile `json:"tasks"`
}

type taskFile struct {
	ID          TaskID                 `json:"id"`
	Status      supervision.TaskStatus `json:"status"`
	DependsOn   []TaskID               `json:"depends_on,omitempty"`
	RetryBudget int                    `json:"retry_budget"`
}

// MarshalJSON serializes the live state to the canonical on-disk shape.
// Phase and task order follow the plan-declared order captured at
// NewStateFromPlan time so re-reads are deterministic.
func (s *State) MarshalJSON() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	df := dagFile{Phases: make([]phaseFile, 0, len(s.phases))}
	for _, id := range s.phaseOrder {
		ps := s.phases[id]
		pf := phaseFile{
			ID:        ps.ID,
			DependsOn: append([]PhaseID(nil), ps.DependsOn...),
			Tasks:     make([]taskFile, 0, len(ps.Tasks)),
		}
		for _, tid := range ps.TaskOrder {
			t := ps.Tasks[tid]
			pf.Tasks = append(pf.Tasks, taskFile{
				ID:          t.ID,
				Status:      t.Status,
				DependsOn:   append([]TaskID(nil), t.DependsOn...),
				RetryBudget: t.RetryBudget,
			})
		}
		df.Phases = append(df.Phases, pf)
	}
	return json.Marshal(df)
}

// LoadStateJSON parses a dag.json byte body into a State. Tasks recorded
// as in_progress are reconciled to pending: any agent that was working
// on the task when the previous process died has, by definition, no
// way to come back, so the next iteration must pick the task up fresh.
func LoadStateJSON(data []byte) (*State, error) {
	var df dagFile
	if err := json.Unmarshal(data, &df); err != nil {
		return nil, fmt.Errorf("dag: parse dag.json: %w", err)
	}
	s := &State{
		phases:     make(map[PhaseID]*PhaseState, len(df.Phases)),
		phaseOrder: make([]PhaseID, 0, len(df.Phases)),
	}
	for _, pf := range df.Phases {
		ps := &PhaseState{
			ID:        pf.ID,
			DependsOn: append([]PhaseID(nil), pf.DependsOn...),
			Tasks:     make(map[TaskID]*TaskState, len(pf.Tasks)),
			TaskOrder: make([]TaskID, 0, len(pf.Tasks)),
		}
		for _, tf := range pf.Tasks {
			status := tf.Status
			if status == supervision.TaskInProgress {
				status = supervision.TaskPending
			}
			ps.Tasks[tf.ID] = &TaskState{
				ID:          tf.ID,
				Status:      status,
				DependsOn:   append([]TaskID(nil), tf.DependsOn...),
				RetryBudget: tf.RetryBudget,
			}
			ps.TaskOrder = append(ps.TaskOrder, tf.ID)
		}
		s.phases[pf.ID] = ps
		s.phaseOrder = append(s.phaseOrder, pf.ID)
	}
	return s, nil
}

// SaveStateFile writes the DAG snapshot to path atomically (write to
// a sibling tmp, fsync, rename). Callers should hand it the absolute
// path of <sessionDir>/dag.json.
func SaveStateFile(s *State, path string) error {
	if s == nil {
		return errors.New("dag: nil state")
	}
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("dag: marshal: %w", err)
	}
	body = append(body, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("dag: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "dag-*.json.tmp")
	if err != nil {
		return fmt.Errorf("dag: temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("dag: write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("dag: sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("dag: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("dag: rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}

// LoadStateFile reads <sessionDir>/dag.json from disk and returns the
// reconciled State (in_progress collapses to pending). When the file
// does not exist the caller decides whether to initialize from a Plan;
// LoadStateFile reports the OS error verbatim.
func LoadStateFile(path string) (*State, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadStateJSON(body)
}

// SortedPhaseIDs returns the live phase ids in lexical order. Used by
// snapshot tests where a deterministic non-plan ordering is convenient.
func SortedPhaseIDs(s *State) []PhaseID {
	ids := s.PhaseOrder()
	sort.Strings(ids)
	return ids
}
