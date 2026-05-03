package director

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEvidenceKind_RoundTrip(t *testing.T) {
	for _, e := range []EvidenceKind{EvidenceDiff, EvidenceTest, EvidenceBuild, EvidenceManual} {
		t.Run(string(e), func(t *testing.T) {
			data, err := json.Marshal(e)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got EvidenceKind
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != e {
				t.Fatalf("round-trip: got %q, want %q", got, e)
			}
		})
	}
}

func TestEvidenceKind_RejectsUnknown(t *testing.T) {
	var e EvidenceKind
	if err := json.Unmarshal([]byte(`"manual-ish"`), &e); err == nil {
		t.Fatalf("expected error for unknown evidence kind")
	}
}

func TestEvidenceKind_MarshalRejectsZeroValue(t *testing.T) {
	var e EvidenceKind
	if _, err := json.Marshal(e); err == nil {
		t.Fatalf("expected marshal error for zero EvidenceKind")
	}
}

func TestPlan_RoundTrip(t *testing.T) {
	in := samplePlan(t)
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Plan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Goal != in.Goal || got.SpecHash != in.SpecHash || len(got.Phases) != len(in.Phases) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
	}
	if got.Phases[0].Tasks[0].Acceptance[0].Evidence != EvidenceTest {
		t.Fatalf("nested enum did not survive round-trip: got %q", got.Phases[0].Tasks[0].Acceptance[0].Evidence)
	}
	if got.Phases[0].Tasks[0].Status != TaskPending {
		t.Fatalf("task status did not survive round-trip: got %q", got.Phases[0].Tasks[0].Status)
	}
}

func TestValidatePlan(t *testing.T) {
	good := samplePlan(t)
	if err := ValidatePlan(good); err != nil {
		t.Fatalf("good plan rejected: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*Plan)
		wantSub string
	}{
		{
			name:    "nil",
			mutate:  func(p *Plan) { *p = Plan{} },
			wantSub: "empty goal",
		},
		{
			name: "no phases",
			mutate: func(p *Plan) {
				p.Phases = nil
			},
			wantSub: "no phases",
		},
		{
			name: "duplicate id",
			mutate: func(p *Plan) {
				p.Phases = append(p.Phases, p.Phases[0])
			},
			wantSub: "duplicate phase id",
		},
		{
			name: "missing dep",
			mutate: func(p *Plan) {
				p.Phases[0].DependsOn = []string{"ghost"}
			},
			wantSub: "depends on unknown phase",
		},
		{
			name: "empty id",
			mutate: func(p *Plan) {
				p.Phases[0].ID = ""
			},
			wantSub: "empty id",
		},
		{
			name: "phase with no tasks",
			mutate: func(p *Plan) {
				p.Phases[0].Tasks = nil
			},
			wantSub: "no tasks",
		},
		{
			name: "duplicate task id within phase",
			mutate: func(p *Plan) {
				p.Phases[0].Tasks = append(p.Phases[0].Tasks, p.Phases[0].Tasks[0])
			},
			wantSub: "duplicate task id",
		},
		{
			name: "task dep crossing phases",
			mutate: func(p *Plan) {
				p.Phases[0].Tasks[0].DependsOn = []string{"t1"}
				p.Phases[0].Tasks = append(p.Phases[0].Tasks, Task{
					ID:     "t2",
					Title:  "second",
					Intent: "second",
					Acceptance: []AcceptanceItem{
						{ID: "X", Description: "ok", Evidence: EvidenceDiff},
					},
					Status:    TaskPending,
					DependsOn: []string{"non-local"},
				})
			},
			wantSub: "depends on unknown task",
		},
		{
			name: "phase cycle",
			mutate: func(p *Plan) {
				p.Phases[0].DependsOn = []string{"p2"}
				p.Phases[1].DependsOn = []string{"p1"}
			},
			wantSub: "cycle in phase DAG",
		},
		{
			name: "task cycle within phase",
			mutate: func(p *Plan) {
				p.Phases[0].Tasks = []Task{
					{
						ID:         "t1",
						Title:      "one",
						Intent:     "one",
						DependsOn:  []string{"t2"},
						Acceptance: []AcceptanceItem{{ID: "A", Description: "x", Evidence: EvidenceDiff}},
						Status:     TaskPending,
					},
					{
						ID:         "t2",
						Title:      "two",
						Intent:     "two",
						DependsOn:  []string{"t1"},
						Acceptance: []AcceptanceItem{{ID: "B", Description: "y", Evidence: EvidenceDiff}},
						Status:     TaskPending,
					},
				}
			},
			wantSub: "cycle in task DAG",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := samplePlan(t)
			tc.mutate(p)
			err := ValidatePlan(p)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q missing substring %q", err.Error(), tc.wantSub)
			}
		})
	}

	if err := ValidatePlan(nil); err == nil {
		t.Fatalf("ValidatePlan(nil) returned nil error")
	}

	t.Run("duplicate task id across phases is accepted", func(t *testing.T) {
		p := samplePlan(t)
		// samplePlan already uses task id "t1" in both p1 and p2; the
		// validator must accept this since task ids are phase-scoped.
		if err := ValidatePlan(p); err != nil {
			t.Fatalf("phase-scoped duplicate task id rejected: %v", err)
		}
	})
}

func TestTaskStatus_RoundTrip(t *testing.T) {
	for _, s := range []TaskStatus{TaskPending, TaskInProgress, TaskDone, TaskNeedsFix} {
		t.Run(string(s), func(t *testing.T) {
			data, err := json.Marshal(s)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got TaskStatus
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != s {
				t.Fatalf("round-trip: got %q, want %q", got, s)
			}
		})
	}
}

func TestTaskStatus_RejectsUnknown(t *testing.T) {
	var s TaskStatus
	if err := json.Unmarshal([]byte(`"working"`), &s); err == nil {
		t.Fatalf("expected error for unknown task status")
	}
}

func TestTaskStatus_MarshalRejectsZeroValue(t *testing.T) {
	var s TaskStatus
	if _, err := json.Marshal(s); err == nil {
		t.Fatalf("expected marshal error for zero TaskStatus")
	}
}

func TestPlan_PhaseByID(t *testing.T) {
	p := samplePlan(t)
	if got := p.PhaseByID("p1"); got == nil || got.ID != "p1" {
		t.Fatalf("PhaseByID(p1) = %+v", got)
	}
	if got := p.PhaseByID("missing"); got != nil {
		t.Fatalf("PhaseByID(missing) = %+v, want nil", got)
	}
	var nilPlan *Plan
	if got := nilPlan.PhaseByID("p1"); got != nil {
		t.Fatalf("nil plan: PhaseByID(p1) = %+v, want nil", got)
	}
}

func TestPhase_TaskByID(t *testing.T) {
	p := samplePlan(t)
	ph := p.PhaseByID("p1")
	if got := ph.TaskByID("t1"); got == nil || got.ID != "t1" {
		t.Fatalf("TaskByID(t1) = %+v", got)
	}
	if got := ph.TaskByID("missing"); got != nil {
		t.Fatalf("TaskByID(missing) = %+v, want nil", got)
	}
}

func samplePlan(t *testing.T) *Plan {
	t.Helper()
	return &Plan{
		Goal:            "build the thing",
		SuccessCriteria: []string{"tests pass", "vet clean"},
		Phases: []Phase{
			{
				ID:             "p1",
				Title:          "phase one",
				Intent:         "first",
				DependsOn:      nil,
				Parallelizable: false,
				ScopeIn:        []string{"internal/foo/"},
				ScopeOut:       []string{"internal/bar/"},
				Tasks: []Task{
					{
						ID:     "t1",
						Title:  "task one",
						Intent: "do the test",
						Acceptance: []AcceptanceItem{
							{ID: "A1", Description: "test exists", Evidence: EvidenceTest},
						},
						Status:      TaskPending,
						RetryBudget: 2,
					},
				},
			},
			{
				ID:        "p2",
				Title:     "phase two",
				Intent:    "second",
				DependsOn: []string{"p1"},
				Tasks: []Task{
					{
						ID:     "t1",
						Title:  "task one",
						Intent: "build it",
						Acceptance: []AcceptanceItem{
							{ID: "B1", Description: "build green", Evidence: EvidenceBuild},
						},
						Status:      TaskPending,
						RetryBudget: 2,
					},
				},
			},
		},
		SpecHash:  "deadbeef",
		PlannedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
	}
}
