package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

func TestProgress_View_NoEvents(t *testing.T) {
	p := progressPanel{bar: newProgressBar()}
	got := p.view(80)
	if !strings.Contains(got, "waiting for first task event") {
		t.Errorf("empty panel should hint at waiting state, got: %q", got)
	}
}

func TestProgress_OnBccEvent_TaskCounters(t *testing.T) {
	p := progressPanel{bar: newProgressBar()}
	p.onBccEvent(agentcontract.BccEvent{Kind: agentcontract.BccEventTaskStarted})
	p.onBccEvent(agentcontract.BccEvent{Kind: agentcontract.BccEventTaskStarted})
	p.onBccEvent(agentcontract.BccEvent{Kind: agentcontract.BccEventTaskCompleted})
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

func TestProgress_OnBccEvent_IgnoresOtherKinds(t *testing.T) {
	p := progressPanel{bar: newProgressBar()}
	p.onBccEvent(agentcontract.BccEvent{Kind: agentcontract.BccEventIterationResult, Signal: agentcontract.SignalDone})
	if p.tasksStarted != 0 || p.tasksCompleted != 0 {
		t.Errorf("iteration_result should not move task counters")
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
		p.onBccEvent(agentcontract.BccEvent{Kind: agentcontract.BccEventTaskStarted})
	}
	p.onBccEvent(agentcontract.BccEvent{Kind: agentcontract.BccEventTaskCompleted})
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
