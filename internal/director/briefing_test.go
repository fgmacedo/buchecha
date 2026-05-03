package director

import (
	"strings"
	"testing"
)

func TestBriefingFor_FirstAttempt_NoPriors(t *testing.T) {
	plan := samplePlan(t)
	in, err := BriefingFor(plan, "/tmp/spec.md", "p1", 1, []string{"t1"}, "")
	if err != nil {
		t.Fatalf("BriefingFor: %v", err)
	}
	if in.PhaseID != "p1" || in.Attempt != 1 {
		t.Errorf("identity: %+v", in)
	}
	if in.IterationID != "p1-1" {
		t.Errorf("iteration_id = %q, want p1-1", in.IterationID)
	}
	if in.SpecPath != "/tmp/spec.md" {
		t.Errorf("spec_path = %q, want /tmp/spec.md", in.SpecPath)
	}
	if len(in.SubDAGTaskIDs) != 1 || in.SubDAGTaskIDs[0] != "t1" {
		t.Errorf("SubDAGTaskIDs = %v, want [t1]", in.SubDAGTaskIDs)
	}
	if in.PriorFeedback != "" {
		t.Errorf("attempt=1 should have empty prior feedback, got %q", in.PriorFeedback)
	}
}

func TestBriefingFor_RetryPropagatesPriorFeedback(t *testing.T) {
	plan := samplePlan(t)
	in, err := BriefingFor(plan, "/tmp/spec.md", "p1", 2, []string{"t1"}, "missing test")
	if err != nil {
		t.Fatalf("BriefingFor: %v", err)
	}
	if in.PriorFeedback != "missing test" {
		t.Errorf("PriorFeedback = %q, want %q", in.PriorFeedback, "missing test")
	}
	if in.IterationID != "p1-2" {
		t.Errorf("iteration_id = %q, want p1-2", in.IterationID)
	}
}

func TestBriefingFor_RejectsUnknownPhase(t *testing.T) {
	plan := samplePlan(t)
	_, err := BriefingFor(plan, "/tmp/spec.md", "ghost", 1, nil, "")
	if err == nil || !strings.Contains(err.Error(), "phase \"ghost\"") {
		t.Fatalf("err = %v, want phase-not-in-plan", err)
	}
}

func TestBriefingFor_RejectsBadAttempt(t *testing.T) {
	plan := samplePlan(t)
	_, err := BriefingFor(plan, "/tmp/spec.md", "p1", 0, nil, "")
	if err == nil || !strings.Contains(err.Error(), "attempt must be >= 1") {
		t.Fatalf("err = %v, want attempt-must-be-positive", err)
	}
}

func TestBriefingFor_RejectsNilPlan(t *testing.T) {
	if _, err := BriefingFor(nil, "/tmp/spec.md", "p1", 1, nil, ""); err == nil {
		t.Fatalf("expected error for nil plan")
	}
}

// TestPendingTaskIDs_OmitsDoneTasks verifies sub-DAG selection excludes
// tasks whose status is already done.
func TestPendingTaskIDs_OmitsDoneTasks(t *testing.T) {
	plan := samplePlan(t)
	plan.Phases[0].Tasks = append(plan.Phases[0].Tasks, Task{
		ID:     "t2",
		Title:  "task two",
		Intent: "second task",
		Acceptance: []AcceptanceItem{
			{ID: "A2", Description: "another", Evidence: EvidenceDiff},
		},
		Status:      TaskDone,
		RetryBudget: 1,
	})
	got := PendingTaskIDs(&plan.Phases[0])
	if len(got) != 1 || got[0] != "t1" {
		t.Errorf("PendingTaskIDs = %v, want [t1] (done task excluded)", got)
	}
}
