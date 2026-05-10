package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/supervision"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/*.md golden files")

// TestRenderBriefingUser_Golden pins the rendered user prompt for a
// fixed (Briefing, Phase) pair to a checked-in fixture. Run with
// `go test -update-golden ./internal/supervision/render/` after intentional
// template changes to refresh the file.
func TestRenderBriefingUser_Golden(t *testing.T) {
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

	got, err := RenderBriefingUser(briefing, phase, "")
	if err != nil {
		t.Fatalf("RenderBriefingUser: %v", err)
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

func TestRenderBriefingUser_AttemptOneOmitsPriorFeedback(t *testing.T) {
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
	got, err := RenderBriefingUser(briefing, phase, "")
	if err != nil {
		t.Fatalf("RenderBriefingUser: %v", err)
	}
	if strings.Contains(got, "Prior feedback") {
		t.Errorf("empty prior_feedback should omit the section:\n%s", got)
	}
}

func TestRenderBriefingUser_RejectsMismatchedPhaseID(t *testing.T) {
	phase := &Phase{ID: "p1", Title: "t", Intent: "i"}
	briefing := &Briefing{IterationID: "p2-1", PhaseID: "p2"}
	if _, err := RenderBriefingUser(briefing, phase, ""); err == nil {
		t.Fatalf("expected error on phase id mismatch")
	}
}

func TestRenderBriefingUser_RejectsNil(t *testing.T) {
	if _, err := RenderBriefingUser(nil, &Phase{ID: "p1"}, ""); err == nil {
		t.Errorf("nil briefing accepted")
	}
	if _, err := RenderBriefingUser(&Briefing{PhaseID: "p1"}, nil, ""); err == nil {
		t.Errorf("nil phase accepted")
	}
}

// TestRenderBriefingUser_OmitsContractSections verifies that the user
// prompt no longer carries the contract partials; those moved to the
// system prompt rendered by RenderBriefingSystem.
func TestRenderBriefingUser_OmitsContractSections(t *testing.T) {
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
	got, err := RenderBriefingUser(briefing, phase, "")
	if err != nil {
		t.Fatalf("RenderBriefingUser: %v", err)
	}
	for _, banned := range []string{
		"## Wire protocol",
		"## Absolute restrictions",
		"## Working tree",
		"git push",
	} {
		if strings.Contains(got, banned) {
			t.Errorf("user prompt should not carry contract marker %q; it belongs in the system prompt", banned)
		}
	}
}

// TestRenderBriefingSystem_IncludesContract verifies the executor
// system prompt carries the contract pieces it needs to operate: the
// 5 wire methods, the working tree discipline, the agent_id, and the
// absolute_restrictions partial. Stricter than a substring sweep
// because the executor cannot recover from a missing contract piece
// at runtime.
func TestRenderBriefingSystem_IncludesContract(t *testing.T) {
	got, err := RenderBriefingSystem("agent-test")
	if err != nil {
		t.Fatalf("RenderBriefingSystem: %v", err)
	}
	for _, marker := range []string{
		// Wire methods the executor must call.
		"get_briefing",
		"get_pending_tasks",
		"task_started",
		"task_completed",
		"iteration_finished",
		// Working tree rules.
		"Clean on entry",
		"git add <specific paths>",
		// Agent identity.
		"agent-test",
		// Absolute restrictions partial.
		"git push",
		"Work **only inside the project directory**",
	} {
		if !strings.Contains(got, marker) {
			t.Errorf("system prompt missing required marker %q", marker)
		}
	}
	// The executor prompt must not expose the other roles' identities;
	// it ships only the executor's own contract.
	for _, otherRole := range []string{"## Your role: the Planner", "## Your role: the Briefer", "## Your role: the Reviewer", "## What bcc is"} {
		if strings.Contains(got, otherRole) {
			t.Errorf("system prompt leaks %q; the executor prompt should not carry pipeline framing", otherRole)
		}
	}
}

// TestRenderBriefingSystem_RejectsEmptyAgentID guards the Identity
// block: an Executor system prompt without a populated agent_id would
// leave the agent unable to authenticate any MCP call, stalling the
// iteration.
func TestRenderBriefingSystem_RejectsEmptyAgentID(t *testing.T) {
	if _, err := RenderBriefingSystem(""); err == nil {
		t.Fatalf("expected error on empty agent_id")
	}
}

// TestRenderBriefingUser_HintBlock verifies that a non-empty hint
// renders an "User hint (escalation)" block above the prior feedback,
// while an empty hint omits the block entirely.
func TestRenderBriefingUser_HintBlock(t *testing.T) {
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
	got, err := RenderBriefingUser(briefing, phase, "tighten the diff")
	if err != nil {
		t.Fatalf("RenderBriefingUser: %v", err)
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

	got2, err := RenderBriefingUser(briefing, phase, "")
	if err != nil {
		t.Fatalf("RenderBriefingUser: %v", err)
	}
	if strings.Contains(got2, "User hint") {
		t.Errorf("empty hint should omit the block:\n%s", got2)
	}
}

// TestRenderBriefingUser_FiltersToSubDAG verifies that when
// SubDAGTaskIDs is non-empty, the rendered prompt includes only those
// tasks and omits the rest.
func TestRenderBriefingUser_FiltersToSubDAG(t *testing.T) {
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
	got, err := RenderBriefingUser(briefing, phase, "")
	if err != nil {
		t.Fatalf("RenderBriefingUser: %v", err)
	}
	if strings.Contains(got, "First") {
		t.Errorf("sub-DAG filter leaked excluded task into prompt:\n%s", got)
	}
	if !strings.Contains(got, "Second") {
		t.Errorf("sub-DAG filter dropped target task:\n%s", got)
	}
}

// TestRenderBriefingUserView_NarrowedSubDAG verifies that
// taskIDsOverride replaces the briefing's SubDAGTaskIDs, so the
// rendered prompt contains only the override tasks regardless of what
// the briefing originally selected. Used by the loop on retry to
// narrow the prompt to still-incomplete tasks.
func TestRenderBriefingUserView_NarrowedSubDAG(t *testing.T) {
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
			{
				ID: "t3", Title: "Third", Intent: "third",
				Acceptance: []AcceptanceItem{{ID: "c", Description: "third ok", Evidence: EvidenceDiff}},
				Status:     TaskPending,
			},
		},
	}
	briefing := &Briefing{
		IterationID:   "p1-1",
		PhaseID:       "p1",
		SubDAGTaskIDs: []string{"t1", "t2", "t3"},
		SpecPath:      "/tmp/spec.md",
	}
	got, err := RenderBriefingUserView(briefing, phase, "", []string{"t2"}, nil)
	if err != nil {
		t.Fatalf("RenderBriefingUserView: %v", err)
	}
	for _, banned := range []string{"First", "Third"} {
		if strings.Contains(got, banned) {
			t.Errorf("override should drop %q from prompt:\n%s", banned, got)
		}
	}
	if !strings.Contains(got, "Second") {
		t.Errorf("override task missing from prompt:\n%s", got)
	}
}

// TestRenderBriefingUserView_FeedbackPerTask verifies that the
// per-task feedback map renders as a "Reviewer feedback (must
// address)" block under the matching task and only that task.
func TestRenderBriefingUserView_FeedbackPerTask(t *testing.T) {
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
		IterationID:   "p1-2",
		PhaseID:       "p1",
		SubDAGTaskIDs: []string{"t1", "t2"},
		SpecPath:      "/tmp/spec.md",
	}
	feedback := map[string]string{
		"t1": "fix off-by-one in loop",
		"t2": "missing nil check",
	}
	got, err := RenderBriefingUserView(briefing, phase, "", nil, feedback)
	if err != nil {
		t.Fatalf("RenderBriefingUserView: %v", err)
	}
	if !strings.Contains(got, "Reviewer feedback") {
		t.Errorf("retry prompt missing reviewer-feedback heading:\n%s", got)
	}
	if !strings.Contains(got, "fix off-by-one in loop") {
		t.Errorf("retry prompt missing t1 feedback:\n%s", got)
	}
	if !strings.Contains(got, "missing nil check") {
		t.Errorf("retry prompt missing t2 feedback:\n%s", got)
	}
	if !strings.Contains(got, "This is a retry.") {
		t.Errorf("retry prompt missing retry banner:\n%s", got)
	}

	// Feedback under t1 must appear before the t2 task header.
	t1Idx := strings.Index(got, "Reviewer feedback")
	t2Idx := strings.Index(got, "### Task t2")
	if t1Idx < 0 || t2Idx < 0 || t1Idx > t2Idx {
		t.Errorf("t1 feedback should be inside its task block, before t2 header (t1=%d t2=%d):\n%s", t1Idx, t2Idx, got)
	}
}

// Ensure the supervision package import is used (type aliases resolve at compile time,
// but the import is needed for the constant forwarding and AcceptanceItem).
var _ supervision.Briefing
