package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
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

func TestRisk_onBccEvent_AccumulatesTaskCounts(t *testing.T) {
	r := riskPanel{}
	r.onBccEvent(agentcontract.BccEvent{Kind: agentcontract.BccEventTaskStarted})
	r.onBccEvent(agentcontract.BccEvent{Kind: agentcontract.BccEventTaskStarted})
	r.onBccEvent(agentcontract.BccEvent{Kind: agentcontract.BccEventTaskCompleted})
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "tasks completed: 1/2") {
		t.Errorf("expected '1/2' in view: %q", out)
	}
}

func TestRisk_onBccEvent_IterationResult(t *testing.T) {
	r := riskPanel{currentIter: 1}
	r.onBccEvent(agentcontract.BccEvent{
		Kind:   agentcontract.BccEventIterationResult,
		Signal: agentcontract.SignalDone,
		Raw:    map[string]any{"value": "done"},
	})
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "signal: done") {
		t.Errorf("expected 'signal: done' in view: %q", out)
	}
}

func TestRisk_onBccEvent_UnknownSignalSurfacesRaw(t *testing.T) {
	r := riskPanel{currentIter: 1}
	r.onBccEvent(agentcontract.BccEvent{
		Kind:   agentcontract.BccEventIterationResult,
		Signal: agentcontract.SignalUnknown,
		Raw:    map[string]any{"value": "weird"},
	})
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, `"weird" (unknown)`) {
		t.Errorf("expected raw value in view: %q", out)
	}
}

func TestRisk_onIterStarted_ClearsSignal(t *testing.T) {
	r := riskPanel{}
	r.onBccEvent(agentcontract.BccEvent{Kind: agentcontract.BccEventIterationResult, Signal: agentcontract.SignalDone, Raw: map[string]any{"value": "done"}})
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
	r.onAgentEvent(loop.AgentEvent{
		Kind: loop.KindToolUse, At: when,
		Tool: &loop.ToolCallInfo{Name: "Edit"},
	})
	out := r.view(when.Add(time.Second), 80)
	if !strings.Contains(out, "last edit") {
		t.Errorf("expected last-edit hint: %q", out)
	}
}
