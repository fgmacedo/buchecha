package config

import "testing"

func TestApplyDefaults_EnglishDefault(t *testing.T) {
	var c Config
	ApplyDefaults(&c)

	if c.Project.Language != "en" {
		t.Errorf("Language = %q, want en", c.Project.Language)
	}
	if c.Specs.PlanHeading != "## Implementation Plan" {
		t.Errorf("PlanHeading = %q", c.Specs.PlanHeading)
	}
	if c.Specs.JournalHeading != "## Execution Journal" {
		t.Errorf("JournalHeading = %q", c.Specs.JournalHeading)
	}
	if c.Specs.ResultKeyword != "Result" {
		t.Errorf("ResultKeyword = %q", c.Specs.ResultKeyword)
	}
	if c.Loop.Results.OK != "ok" {
		t.Errorf("Results.OK = %q", c.Loop.Results.OK)
	}
	if c.Loop.Results.Done != "done" {
		t.Errorf("Results.Done = %q", c.Loop.Results.Done)
	}
	if c.Executor.Agent != "claude" {
		t.Errorf("Executor.Agent = %q", c.Executor.Agent)
	}
	if c.Loop.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d", c.Loop.MaxIterations)
	}
	if got, want := c.Specs.Dir, "docs/specs"; got != want {
		t.Errorf("Specs.Dir = %q, want %q", got, want)
	}
	if len(c.Env.Files) != 1 || c.Env.Files[0] != ".env" {
		t.Errorf("Env.Files = %v, want [.env]", c.Env.Files)
	}
}

func TestApplyDefaults_PtBR(t *testing.T) {
	c := Config{Project: Project{Language: "pt-BR"}}
	ApplyDefaults(&c)
	if c.Specs.PlanHeading != "## Plano de implementação" {
		t.Errorf("PlanHeading = %q", c.Specs.PlanHeading)
	}
	if c.Specs.JournalHeading != "## Diário de execução" {
		t.Errorf("JournalHeading = %q", c.Specs.JournalHeading)
	}
	if c.Specs.ResultKeyword != "Resultado" {
		t.Errorf("ResultKeyword = %q", c.Specs.ResultKeyword)
	}
	if c.Loop.Results.Partial != "parcial" {
		t.Errorf("Partial = %q", c.Loop.Results.Partial)
	}
	if c.Loop.Results.Done != "finalizado" {
		t.Errorf("Done = %q", c.Loop.Results.Done)
	}
	if c.Loop.Results.Blocked != "bloqueado" {
		t.Errorf("Blocked = %q", c.Loop.Results.Blocked)
	}
}

func TestApplyDefaults_DoesNotOverwriteExplicit(t *testing.T) {
	c := Config{
		Project: Project{Language: "en"},
		Specs:   Specs{PlanHeading: "## Custom"},
		Loop: Loop{
			MaxIterations: 5,
			Results:       Results{OK: "yep"},
		},
	}
	ApplyDefaults(&c)
	if c.Specs.PlanHeading != "## Custom" {
		t.Errorf("PlanHeading should not be overwritten")
	}
	if c.Loop.MaxIterations != 5 {
		t.Errorf("MaxIterations should not be overwritten, got %d", c.Loop.MaxIterations)
	}
	if c.Loop.Results.OK != "yep" {
		t.Errorf("OK should not be overwritten")
	}
	if c.Loop.Results.Partial != "partial" {
		t.Errorf("Partial should be defaulted to en value, got %q", c.Loop.Results.Partial)
	}
}

func TestApplyDefaults_UnknownLanguageFallsBackToEn(t *testing.T) {
	c := Config{Project: Project{Language: "klingon"}}
	ApplyDefaults(&c)
	if c.Specs.PlanHeading != "## Implementation Plan" {
		t.Errorf("unknown language should fall back to en, got %q", c.Specs.PlanHeading)
	}
}
