package tui

import (
	"strings"
	"testing"
	"time"
)

func TestIsPseudoTaskID(t *testing.T) {
	if !isPseudoTaskID(planningTaskID) {
		t.Errorf("planning must be classified as pseudo")
	}
	if !isPseudoTaskID(briefingTaskID) {
		t.Errorf("briefing must be classified as pseudo")
	}
	if !isPseudoTaskID(reviewingTaskID) {
		t.Errorf("reviewing must be classified as pseudo")
	}
	if isPseudoTaskID("real-task-id") {
		t.Errorf("real task id must not be classified as pseudo")
	}
	if isPseudoTaskID("") {
		t.Errorf("empty id must not be classified as pseudo")
	}
}

func TestProgress_View_NoEvents(t *testing.T) {
	p := progressPanel{bar: newProgressBar()}
	got := p.view(80)
	if !strings.Contains(got, "waiting for first task event") {
		t.Errorf("empty panel should hint at waiting state, got: %q", got)
	}
}

func TestProgress_TaskCounters(t *testing.T) {
	p := progressPanel{bar: newProgressBar()}
	p.onTaskStarted()
	p.onTaskStarted()
	p.onTaskCompleted()
	if p.tasksStarted != 2 {
		t.Errorf("tasksStarted = %d, want 2", p.tasksStarted)
	}
	if p.tasksCompleted != 1 {
		t.Errorf("tasksCompleted = %d, want 1", p.tasksCompleted)
	}
	got := p.view(80)
	if !strings.Contains(got, "1/2 tasks") {
		t.Errorf("view should report 1/2 tasks, got: %q", got)
	}
}

func TestProgress_BumpsStartedWhenCompletedExceedsIt(t *testing.T) {
	// The agent emits one task_started for the whole phase and N
	// task_completed for sub-items: defense bumps started so the panel
	// never renders "5/1 tasks".
	p := progressPanel{bar: newProgressBar()}
	p.onTaskStarted()
	for i := 0; i < 5; i++ {
		p.onTaskCompleted()
	}
	if p.tasksStarted != 5 || p.tasksCompleted != 5 {
		t.Errorf("started/completed = %d/%d, want 5/5 after defense", p.tasksStarted, p.tasksCompleted)
	}
	got := p.view(80)
	if !strings.Contains(got, "5/5 tasks") {
		t.Errorf("view should report 5/5 tasks (defense bumped started), got: %q", got)
	}
}

func TestProgress_OnIterStarted_ResetsCountersButKeepsDurations(t *testing.T) {
	p := progressPanel{bar: newProgressBar()}
	p.onTaskStarted()
	p.onTaskCompleted()
	p.onIterationFinished(3 * time.Second)
	p.onIterStarted()
	if p.tasksStarted != 0 || p.tasksCompleted != 0 {
		t.Errorf("counters not reset: started=%d completed=%d", p.tasksStarted, p.tasksCompleted)
	}
	if len(p.durations) != 1 {
		t.Errorf("durations should persist across iterations, got len=%d", len(p.durations))
	}
	p.onTaskStarted()
	got := p.view(80)
	if !strings.Contains(got, "0/1 tasks") {
		t.Errorf("after reset and one new task_started, view should show 0/1 tasks: %q", got)
	}
}

func TestProgress_OnIterationFinished_KeepsRollingDurations(t *testing.T) {
	p := progressPanel{bar: newProgressBar()}
	for i := 0; i < 40; i++ {
		p.onIterationFinished(time.Duration(i+1) * time.Second)
	}
	if len(p.durations) != 32 {
		t.Errorf("durations capped at 32, got %d", len(p.durations))
	}
}

func TestProgress_ETA_ScalesWithRemainingTasks(t *testing.T) {
	p := progressPanel{bar: newProgressBar()}
	p.onIterationFinished(2 * time.Second)
	p.onIterationFinished(4 * time.Second)
	for i := 0; i < 5; i++ {
		p.onTaskStarted()
	}
	p.onTaskCompleted()
	got := p.view(80)
	if !strings.Contains(got, "ETA ~") {
		t.Errorf("view should include an ETA, got: %q", got)
	}
}

func TestComputeETA_NoDurations(t *testing.T) {
	if got := computeETA(nil, 5); got != 0 {
		t.Errorf("ETA with no durations = %v, want 0", got)
	}
}

func TestComputeETA_NoRemaining(t *testing.T) {
	if got := computeETA([]time.Duration{time.Second}, 0); got != 0 {
		t.Errorf("ETA with zero remaining = %v, want 0", got)
	}
}
