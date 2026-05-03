package dag

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/fgmacedo/buchecha/internal/director"
)

func samplePlan() *director.Plan {
	return &director.Plan{
		Goal: "demo",
		Phases: []director.Phase{
			{
				ID: "P1",
				Tasks: []director.Task{
					{ID: "t1", Status: director.TaskPending, RetryBudget: 1},
					{ID: "t2", Status: director.TaskPending, DependsOn: []string{"t1"}, RetryBudget: 2},
				},
			},
			{
				ID:        "P2",
				DependsOn: []string{"P1"},
				Tasks: []director.Task{
					{ID: "t1", Status: director.TaskPending, RetryBudget: 1},
				},
			},
		},
	}
}

func TestNewStateFromPlan_InitialEverythingPending(t *testing.T) {
	s := NewStateFromPlan(samplePlan())
	if !s.HasPending() {
		t.Fatal("expected pending tasks after init")
	}
	if got := s.PendingTasks("P1"); len(got) != 2 || got[0] != "t1" || got[1] != "t2" {
		t.Errorf("PendingTasks(P1) = %v, want [t1 t2] in plan order", got)
	}
}

func TestEligiblePhases_OnlyP1WhileP1HasPendingTasks(t *testing.T) {
	s := NewStateFromPlan(samplePlan())
	got := s.EligiblePhases()
	if len(got) != 1 || got[0] != "P1" {
		t.Errorf("EligiblePhases = %v, want [P1] (P2 blocked by P1 deps)", got)
	}
}

func TestEligiblePhases_P2EligibleAfterP1Done(t *testing.T) {
	s := NewStateFromPlan(samplePlan())
	if err := s.SetTaskStatus("P1", "t1", director.TaskDone); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskStatus("P1", "t2", director.TaskDone); err != nil {
		t.Fatal(err)
	}
	got := s.EligiblePhases()
	if len(got) != 1 || got[0] != "P2" {
		t.Errorf("EligiblePhases = %v, want [P2] after P1 done", got)
	}
}

func TestSetTaskStatus_RejectsUnknownIDs(t *testing.T) {
	s := NewStateFromPlan(samplePlan())
	if err := s.SetTaskStatus("PX", "t1", director.TaskDone); err == nil {
		t.Error("expected error for unknown phase")
	}
	if err := s.SetTaskStatus("P1", "tX", director.TaskDone); err == nil {
		t.Error("expected error for unknown task")
	}
	if err := s.SetTaskStatus("P1", "t1", director.TaskStatus("weird")); err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestState_ConcurrentApplyIsRaceFree(t *testing.T) {
	s := NewStateFromPlan(samplePlan())
	var wg sync.WaitGroup
	mutators := []func() error{
		func() error { return s.SetTaskStatus("P1", "t1", director.TaskInProgress) },
		func() error { return s.SetTaskStatus("P1", "t1", director.TaskDone) },
		func() error { return s.SetTaskStatus("P1", "t2", director.TaskInProgress) },
		func() error { return s.SetTaskStatus("P1", "t2", director.TaskDone) },
		func() error { return s.SetTaskStatus("P2", "t1", director.TaskNeedsFix) },
		func() error { return s.SetTaskStatus("P2", "t1", director.TaskPending) },
	}
	for i := 0; i < 200; i++ {
		for _, m := range mutators {
			wg.Add(1)
			go func(m func() error) {
				defer wg.Done()
				_ = m()
			}(m)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.HasPending()
			_ = s.EligiblePhases()
			_ = s.Snapshot()
		}()
	}
	wg.Wait()
}

func TestState_SnapshotRoundTrip(t *testing.T) {
	s := NewStateFromPlan(samplePlan())
	if err := s.SetTaskStatus("P1", "t1", director.TaskDone); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskStatus("P1", "t2", director.TaskInProgress); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := LoadStateJSON(body)
	if err != nil {
		t.Fatalf("LoadStateJSON: %v", err)
	}
	// in_progress was reconciled to pending on load.
	gotPS := got.Phase("P1")
	if gotPS == nil {
		t.Fatal("phase P1 missing after round-trip")
	}
	if gotPS.Tasks["t2"].Status != director.TaskPending {
		t.Errorf("t2 status after round-trip = %q, want pending (in_progress reconciled)",
			string(gotPS.Tasks["t2"].Status))
	}
	if gotPS.Tasks["t1"].Status != director.TaskDone {
		t.Errorf("t1 status after round-trip = %q, want done", string(gotPS.Tasks["t1"].Status))
	}
}

func TestState_LoadReconcilesInProgressOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dag.json")
	body := `{
        "phases": [
            {"id":"P1","depends_on":[],"tasks":[
                {"id":"t1","status":"in_progress","retry_budget":1},
                {"id":"t2","status":"in_progress","depends_on":["t1"],"retry_budget":2}
            ]},
            {"id":"P2","depends_on":["P1"],"tasks":[
                {"id":"t1","status":"done","retry_budget":1}
            ]}
        ]
    }`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadStateFile(path)
	if err != nil {
		t.Fatalf("LoadStateFile: %v", err)
	}
	pending := got.PendingTasks("P1")
	if len(pending) != 2 || pending[0] != "t1" || pending[1] != "t2" {
		t.Errorf("PendingTasks(P1) after reconciliation = %v, want [t1 t2]", pending)
	}
	if got.Phase("P2").Tasks["t1"].Status != director.TaskDone {
		t.Error("done tasks should not be reconciled to pending")
	}
}
