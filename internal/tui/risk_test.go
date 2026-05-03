package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

func TestRisk_view_BeforeAnyEvent_RendersUnknowns(t *testing.T) {
	r := riskPanel{currentIter: 1}
	out := r.view(time.Now(), 80)
	for _, w := range []string{"tasks completed", "uncommitted:", "signal:"} {
		if !strings.Contains(out, w) {
			t.Errorf("view missing %q: %q", w, out)
		}
	}
}

func TestRisk_onTaskCounts_AccumulatesAcrossIters(t *testing.T) {
	r := riskPanel{}
	r.onTaskStarted()
	r.onTaskStarted()
	r.onTaskCompleted()
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "tasks completed: 1/2") {
		t.Errorf("expected '1/2' in view: %q", out)
	}
}

func TestRisk_onIterFinished_LatchesSignal(t *testing.T) {
	r := riskPanel{currentIter: 1}
	r.onIterFinished(agentcontract.SignalDone)
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "signal: done") {
		t.Errorf("expected 'signal: done' in view: %q", out)
	}
}

func TestRisk_onIterStarted_ClearsSignal(t *testing.T) {
	r := riskPanel{}
	r.onIterFinished(agentcontract.SignalDone)
	r.onIterStarted(2)
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "not yet emitted") {
		t.Errorf("after onIterStarted, signal line should reset: %q", out)
	}
}

func TestRisk_onDirtyFileCount(t *testing.T) {
	r := riskPanel{}
	r.onDirtyFileCount(3)
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "3 files") {
		t.Errorf("expected '3 files': %q", out)
	}
}

func TestRisk_onCommitCount(t *testing.T) {
	r := riskPanel{}
	r.onCommitCount(5)
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "(5 commits)") {
		t.Errorf("expected '(5 commits)': %q", out)
	}
}

func TestRisk_onAgentEvent_TracksLastEdit(t *testing.T) {
	r := riskPanel{}
	r.onDirtyFileCount(2)
	when := time.Now()
	r.onAgentEvent(agentcontract.AgentEvent{
		Kind: agentcontract.KindToolUse, At: when,
		Tool: &agentcontract.ToolCallInfo{Name: "Edit"},
	})
	out := r.view(when.Add(time.Second), 80)
	if !strings.Contains(out, "last edit") {
		t.Errorf("expected last-edit hint: %q", out)
	}
}
