package director

import (
	"encoding/json"
	"fmt"
)

// TaskStatus is the closed set of lifecycle states a Task can carry as
// it moves through a sub-DAG iteration. The wire-protocol values are
// canonical English; localization happens in user-facing artifacts
// (TUI, journal text), never in the type.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskDone       TaskStatus = "done"
	TaskNeedsFix   TaskStatus = "needs_fix"
)

func (s TaskStatus) valid() bool {
	switch s {
	case TaskPending, TaskInProgress, TaskDone, TaskNeedsFix:
		return true
	}
	return false
}

// String returns the canonical wire value.
func (s TaskStatus) String() string { return string(s) }

// MarshalJSON enforces the closed set on serialization. The zero value
// is rejected so a half-built Task cannot escape unnoticed.
func (s TaskStatus) MarshalJSON() ([]byte, error) {
	if !s.valid() {
		return nil, fmt.Errorf("director: invalid TaskStatus %q", string(s))
	}
	return json.Marshal(string(s))
}

// UnmarshalJSON accepts only the canonical values.
func (s *TaskStatus) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("director: TaskStatus: %w", err)
	}
	v := TaskStatus(raw)
	if !v.valid() {
		return fmt.Errorf("director: invalid TaskStatus %q", raw)
	}
	*s = v
	return nil
}

// EvidenceKind declares how a Reviewer must check an AcceptanceItem.
// The set is intentionally narrow: diff inspection, test execution,
// build, or human-judged manual review.
type EvidenceKind string

const (
	EvidenceDiff   EvidenceKind = "diff"
	EvidenceTest   EvidenceKind = "test"
	EvidenceBuild  EvidenceKind = "build"
	EvidenceManual EvidenceKind = "manual"
)

func (e EvidenceKind) valid() bool {
	switch e {
	case EvidenceDiff, EvidenceTest, EvidenceBuild, EvidenceManual:
		return true
	}
	return false
}

// String returns the canonical wire value.
func (e EvidenceKind) String() string { return string(e) }

// MarshalJSON enforces the closed set on serialization.
func (e EvidenceKind) MarshalJSON() ([]byte, error) {
	if !e.valid() {
		return nil, fmt.Errorf("director: invalid EvidenceKind %q", string(e))
	}
	return json.Marshal(string(e))
}

// UnmarshalJSON accepts only the canonical values.
func (e *EvidenceKind) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("director: EvidenceKind: %w", err)
	}
	v := EvidenceKind(s)
	if !v.valid() {
		return fmt.Errorf("director: invalid EvidenceKind %q", s)
	}
	*e = v
	return nil
}
