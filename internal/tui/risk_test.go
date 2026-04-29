package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/spec"
)

func TestRisk_view_BeforeAnyParse_RendersUnknowns(t *testing.T) {
	r := riskPanel{currentIter: 1}
	out := r.view(time.Now(), 80)
	for _, w := range []string{"committed:", "uncommitted:", "journal:"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q\n%s", w, out)
		}
	}
	if !strings.Contains(out, "...") {
		t.Errorf("uncommitted should render placeholder when not yet probed: %q", out)
	}
	if !strings.Contains(out, "not yet written") {
		t.Errorf("journal line should call out missing entry: %q", out)
	}
}

func TestRisk_onSpecParsed_ReflectsItemsAndJournal(t *testing.T) {
	r := riskPanel{currentIter: 3}
	plan := samplePlan() // 3 [x] / 3 [ ]
	r.onSpecParsed(plan, spec.LatestResult{Raw: "ok", Result: spec.ResultOK}, true)

	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "3/6 items") {
		t.Errorf("missing 3/6 items: %q", out)
	}
	if !strings.Contains(out, "Result ok") {
		t.Errorf("expected Result ok in journal line: %q", out)
	}
}

func TestRisk_onSpecParsed_UnknownResultFlagsWarn(t *testing.T) {
	r := riskPanel{currentIter: 1}
	r.onSpecParsed(samplePlan(), spec.LatestResult{Raw: "yolo", Result: spec.ResultUnknown}, true)
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "yolo") || !strings.Contains(out, "unknown") {
		t.Errorf("unknown result not surfaced: %q", out)
	}
}

func TestRisk_onDirtyFileCount_RendersFiles(t *testing.T) {
	r := riskPanel{}
	r.onDirtyFileCount(3)
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "3 files") {
		t.Errorf("dirty file count not rendered: %q", out)
	}
}

func TestRisk_onCommitCount_RendersCountWhenKnown(t *testing.T) {
	r := riskPanel{}
	r.onSpecParsed(samplePlan(), spec.LatestResult{Raw: "ok", Result: spec.ResultOK}, true)
	r.onCommitCount(12)
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "(12 commits)") {
		t.Errorf("expected '(12 commits)' on committed line: %q", out)
	}
}

func TestRisk_onCommitCount_SingularOnOne(t *testing.T) {
	r := riskPanel{}
	r.onSpecParsed(samplePlan(), spec.LatestResult{Raw: "ok", Result: spec.ResultOK}, true)
	r.onCommitCount(1)
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "(1 commit)") {
		t.Errorf("expected '(1 commit)' singular form: %q", out)
	}
}

func TestRisk_view_OmitsCommitCountWhenUnknown(t *testing.T) {
	r := riskPanel{}
	r.onSpecParsed(samplePlan(), spec.LatestResult{Raw: "ok", Result: spec.ResultOK}, true)
	out := r.view(time.Now(), 80)
	// The "committed:" label is unconditional; the parenthesised "(N
	// commit[s])" appears only after a successful CommitsSince probe.
	if strings.Contains(out, " commits)") || strings.Contains(out, " commit)") {
		t.Errorf("commit count should not appear before any probe: %q", out)
	}
}

func TestRisk_onCommitCount_ZeroStillRenders(t *testing.T) {
	r := riskPanel{}
	r.onSpecParsed(samplePlan(), spec.LatestResult{Raw: "ok", Result: spec.ResultOK}, true)
	r.onCommitCount(0)
	out := r.view(time.Now(), 80)
	if !strings.Contains(out, "(0 commits)") {
		t.Errorf("zero commit count must still render to distinguish 'probed' from 'unknown': %q", out)
	}
}

func TestRisk_onAgentEvent_TracksLastEditOnlyForWriteTools(t *testing.T) {
	r := riskPanel{}
	at := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)

	r.onAgentEvent(loop.AgentEvent{
		Kind: loop.KindToolUse, At: at,
		Tool: &loop.ToolCallInfo{Name: "Read"},
	})
	if !r.lastEditAt.IsZero() {
		t.Errorf("Read should not count as an edit; lastEditAt=%v", r.lastEditAt)
	}

	r.onAgentEvent(loop.AgentEvent{
		Kind: loop.KindToolUse, At: at,
		Tool: &loop.ToolCallInfo{Name: "Edit"},
	})
	if r.lastEditAt != at {
		t.Errorf("Edit should set lastEditAt; got %v", r.lastEditAt)
	}
}

func TestRisk_onIterStarted_ResetsJournalKnownFlag(t *testing.T) {
	r := riskPanel{currentIter: 2}
	r.onSpecParsed(spec.Plan{}, spec.LatestResult{Raw: "ok", Result: spec.ResultOK}, true)
	if !r.journalKnown {
		t.Fatalf("setup: journalKnown should be true")
	}
	r.onIterStarted(3)
	if r.journalKnown {
		t.Errorf("onIterStarted must reset journalKnown until next IterationFinished re-parse")
	}
	if r.currentIter != 3 {
		t.Errorf("currentIter not advanced: got %d", r.currentIter)
	}
}

func TestJournalLine_KnownAndUnknownPaths(t *testing.T) {
	line := journalLine(2, "", spec.ResultUnknown, false)
	if !strings.Contains(line, "not yet written") {
		t.Errorf("missing 'not yet written': %q", line)
	}
	line = journalLine(2, "ok", spec.ResultOK, true)
	if !strings.Contains(line, "ok") {
		t.Errorf("known journal should include raw: %q", line)
	}
	line = journalLine(2, "yolo", spec.ResultUnknown, true)
	if !strings.Contains(line, "yolo") || !strings.Contains(line, "unknown") {
		t.Errorf("unknown journal should surface raw + tag: %q", line)
	}
}
