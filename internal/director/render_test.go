package director

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/*.md golden files")

// TestRenderBriefingPrompt_Golden pins the rendered system prompt for a
// fixed (Briefing, Phase) pair to a checked-in fixture. Run with
// `go test -update-golden ./internal/director/` after intentional
// template changes to refresh the file.
func TestRenderBriefingPrompt_Golden(t *testing.T) {
	phase := &Phase{
		ID:       "p1",
		Title:    "Phase one",
		Intent:   "Bootstrap the package layout and types.",
		ScopeIn:  []string{"internal/foo/", "internal/foo/types.go"},
		ScopeOut: []string{"internal/bar/"},
		Tasks: []Task{
			{
				ID:     "t1",
				Title:  "Add types",
				Intent: "Define the new domain shape.",
				Acceptance: []AcceptanceItem{
					{ID: "A1", Description: "go test ./internal/foo/... is green", Evidence: EvidenceTest},
					{ID: "A2", Description: "no import of internal/bar in foo", Evidence: EvidenceDiff},
				},
				Status:      TaskPending,
				RetryBudget: 2,
			},
		},
	}
	priorFeedback := "Attempt 1 left out the table-driven test for the parser. Required: add it."
	briefing := &Briefing{
		IterationID:   "p1-2",
		PhaseID:       "p1",
		SubDAGTaskIDs: []string{"t1"},
		Instructions:  "Earlier phases delivered the spec parser. This phase wires the typed domain.",
		SpecPath:      "/tmp/spec.md",
		PriorFeedback: &priorFeedback,
	}

	got, err := RenderBriefingPrompt(briefing, phase, "")
	if err != nil {
		t.Fatalf("RenderBriefingPrompt: %v", err)
	}

	goldenPath := filepath.Join("testdata", "briefing_golden.md")
	if *updateGolden {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update-golden to create)", err)
	}
	if got != string(want) {
		t.Errorf("rendered prompt diverged from golden.\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderBriefingPrompt_AttemptOneOmitsPriorFeedback(t *testing.T) {
	phase := &Phase{
		ID: "p1", Title: "t", Intent: "i",
		Tasks: []Task{
			{
				ID: "t1", Title: "x", Intent: "y",
				Acceptance: []AcceptanceItem{{ID: "a", Description: "ok", Evidence: EvidenceDiff}},
				Status:     TaskPending,
			},
		},
	}
	briefing := &Briefing{
		IterationID:  "p1-1",
		PhaseID:      "p1",
		Instructions: "ctx",
		SpecPath:     "/tmp/spec.md",
	}
	got, err := RenderBriefingPrompt(briefing, phase, "")
	if err != nil {
		t.Fatalf("RenderBriefingPrompt: %v", err)
	}
	if strings.Contains(got, "Prior feedback") {
		t.Errorf("empty prior_feedback should omit the section:\n%s", got)
	}
}

func TestRenderBriefingPrompt_RejectsMismatchedPhaseID(t *testing.T) {
	phase := &Phase{ID: "p1", Title: "t", Intent: "i"}
	briefing := &Briefing{IterationID: "p2-1", PhaseID: "p2"}
	if _, err := RenderBriefingPrompt(briefing, phase, ""); err == nil {
		t.Fatalf("expected error on phase id mismatch")
	}
}

func TestRenderBriefingPrompt_RejectsNil(t *testing.T) {
	if _, err := RenderBriefingPrompt(nil, &Phase{ID: "p1"}, ""); err == nil {
		t.Errorf("nil briefing accepted")
	}
	if _, err := RenderBriefingPrompt(&Briefing{PhaseID: "p1"}, nil, ""); err == nil {
		t.Errorf("nil phase accepted")
	}
}

// TestRenderBriefingPrompt_IncludesPartials verifies the three
// agentcontract partials end up in the rendered prompt; their absence
// would relax the bcc contract.
func TestRenderBriefingPrompt_IncludesPartials(t *testing.T) {
	phase := &Phase{
		ID: "p1", Title: "t", Intent: "i",
		Tasks: []Task{
			{
				ID: "t1", Title: "x", Intent: "y",
				Acceptance: []AcceptanceItem{{ID: "a", Description: "ok", Evidence: EvidenceDiff}},
				Status:     TaskPending,
			},
		},
	}
	briefing := &Briefing{
		IterationID: "p1-1", PhaseID: "p1",
		Instructions: "ctx", SpecPath: "/tmp/spec.md",
	}
	got, err := RenderBriefingPrompt(briefing, phase, "")
	if err != nil {
		t.Fatalf("RenderBriefingPrompt: %v", err)
	}
	for _, marker := range []string{
		"bcc_task_started",
		"absolute restrictions",
		"git push",
		"Clean on entry",
	} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(marker)) {
			t.Errorf("partial marker %q missing from rendered prompt", marker)
		}
	}
}

// TestRenderBriefingPrompt_HintBlock verifies that a non-empty hint
// renders an "User hint (escalation)" block above the prior feedback,
// while an empty hint omits the block entirely.
func TestRenderBriefingPrompt_HintBlock(t *testing.T) {
	phase := &Phase{
		ID: "p1", Title: "t", Intent: "i",
		Tasks: []Task{
			{
				ID: "t1", Title: "x", Intent: "y",
				Acceptance: []AcceptanceItem{{ID: "a", Description: "ok", Evidence: EvidenceDiff}},
				Status:     TaskPending,
			},
		},
	}
	prior := "reviewer feedback"
	briefing := &Briefing{
		IterationID: "p1-2", PhaseID: "p1",
		SubDAGTaskIDs: []string{"t1"},
		SpecPath:      "/tmp/spec.md",
		PriorFeedback: &prior,
	}
	got, err := RenderBriefingPrompt(briefing, phase, "tighten the diff")
	if err != nil {
		t.Fatalf("RenderBriefingPrompt: %v", err)
	}
	if !strings.Contains(got, "User hint (escalation)") {
		t.Errorf("rendered prompt missing hint heading:\n%s", got)
	}
	if !strings.Contains(got, "tighten the diff") {
		t.Errorf("rendered prompt missing hint text:\n%s", got)
	}
	hintIdx := strings.Index(got, "User hint")
	priorIdx := strings.Index(got, "Prior feedback")
	if hintIdx < 0 || priorIdx < 0 || hintIdx > priorIdx {
		t.Errorf("hint block must precede prior feedback; hintIdx=%d priorIdx=%d", hintIdx, priorIdx)
	}

	got2, err := RenderBriefingPrompt(briefing, phase, "")
	if err != nil {
		t.Fatalf("RenderBriefingPrompt: %v", err)
	}
	if strings.Contains(got2, "User hint") {
		t.Errorf("empty hint should omit the block:\n%s", got2)
	}
}

// TestRenderBriefingPrompt_FiltersToSubDAG verifies that when
// SubDAGTaskIDs is non-empty, the rendered prompt includes only those
// tasks and omits the rest.
func TestRenderBriefingPrompt_FiltersToSubDAG(t *testing.T) {
	phase := &Phase{
		ID: "p1", Title: "t", Intent: "i",
		Tasks: []Task{
			{
				ID: "t1", Title: "First", Intent: "first",
				Acceptance: []AcceptanceItem{{ID: "a", Description: "first ok", Evidence: EvidenceDiff}},
				Status:     TaskPending,
			},
			{
				ID: "t2", Title: "Second", Intent: "second",
				Acceptance: []AcceptanceItem{{ID: "b", Description: "second ok", Evidence: EvidenceDiff}},
				Status:     TaskPending,
			},
		},
	}
	briefing := &Briefing{
		IterationID:   "p1-1",
		PhaseID:       "p1",
		SubDAGTaskIDs: []string{"t2"},
		SpecPath:      "/tmp/spec.md",
	}
	got, err := RenderBriefingPrompt(briefing, phase, "")
	if err != nil {
		t.Fatalf("RenderBriefingPrompt: %v", err)
	}
	if strings.Contains(got, "First") {
		t.Errorf("sub-DAG filter leaked excluded task into prompt:\n%s", got)
	}
	if !strings.Contains(got, "Second") {
		t.Errorf("sub-DAG filter dropped target task:\n%s", got)
	}
}
