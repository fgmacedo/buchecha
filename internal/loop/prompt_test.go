package loop

import (
	"strings"
	"testing"
)

func englishPromptInput() PromptInput {
	return PromptInput{
		SpecPath:       "docs/specs/foo.md",
		GuidePath:      "docs/guides/autonomous-execution.md",
		PlanHeading:    "## Implementation Plan",
		JournalHeading: "## Execution Journal",
		ResultKeyword:  "Result",
		ResultOK:       "ok",
		ResultPartial:  "partial",
		ResultDone:     "done",
		ResultBlocked:  "blocked",
	}
}

func TestBuildPromptLoop_IncludesAllVocab(t *testing.T) {
	out, err := BuildPromptLoop(englishPromptInput())
	if err != nil {
		t.Fatalf("BuildPromptLoop: %v", err)
	}
	for _, want := range []string{
		"docs/specs/foo.md",
		"docs/guides/autonomous-execution.md",
		"## Implementation Plan",
		"## Execution Journal",
		"**Result**",
		"ok", "partial", "done", "blocked",
		"loop-by-phase",
		"ONE pending phase",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", want, out)
		}
	}
}

func TestBuildPromptLoop_OmitsExtraBlockWhenEmpty(t *testing.T) {
	out, err := BuildPromptLoop(englishPromptInput())
	if err != nil {
		t.Fatalf("BuildPromptLoop: %v", err)
	}
	if strings.Contains(out, "Additional instructions") {
		t.Errorf("prompt should not include extra block when Extra is empty")
	}
}

func TestBuildPromptLoop_WithExtra(t *testing.T) {
	in := englishPromptInput()
	in.Extra = "use TDD; ignore F1"
	out, err := BuildPromptLoop(in)
	if err != nil {
		t.Fatalf("BuildPromptLoop: %v", err)
	}
	if !strings.Contains(out, "Additional instructions") {
		t.Errorf("prompt should include extra block when Extra is set")
	}
	if !strings.Contains(out, "use TDD; ignore F1") {
		t.Errorf("prompt should include extra text verbatim")
	}
}

func TestBuildPromptLoop_PortugueseLocalized(t *testing.T) {
	in := PromptInput{
		SpecPath:       "x",
		GuidePath:      "g",
		PlanHeading:    "## Plano de implementação",
		JournalHeading: "## Diário de execução",
		ResultKeyword:  "Resultado",
		ResultOK:       "ok",
		ResultPartial:  "parcial",
		ResultDone:     "finalizado",
		ResultBlocked:  "bloqueado",
	}
	out, err := BuildPromptLoop(in)
	if err != nil {
		t.Fatalf("BuildPromptLoop: %v", err)
	}
	for _, want := range []string{
		"## Plano de implementação",
		"## Diário de execução",
		"**Resultado**",
		"parcial", "finalizado", "bloqueado",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", want, out)
		}
	}
	// English keywords should not leak.
	if strings.Contains(out, "**Result**") {
		t.Errorf("English Result keyword leaked into pt-BR prompt")
	}
}

func TestBuildPromptSingleShot(t *testing.T) {
	out, err := BuildPromptSingleShot(englishPromptInput())
	if err != nil {
		t.Fatalf("BuildPromptSingleShot: %v", err)
	}
	if !strings.Contains(out, "single-shot") {
		t.Errorf("prompt missing single-shot mention")
	}
	if !strings.Contains(out, "## Execution Journal") {
		t.Errorf("prompt missing journal heading")
	}
	if strings.Contains(out, "ONE pending phase") {
		t.Errorf("single-shot prompt leaked loop-mode phrasing")
	}
}

func TestBuildPromptSingleShot_WithExtra(t *testing.T) {
	in := englishPromptInput()
	in.Extra = "test it well"
	out, err := BuildPromptSingleShot(in)
	if err != nil {
		t.Fatalf("BuildPromptSingleShot: %v", err)
	}
	if !strings.Contains(out, "Additional instructions") || !strings.Contains(out, "test it well") {
		t.Errorf("extra block missing in single-shot prompt:\n%s", out)
	}
}
