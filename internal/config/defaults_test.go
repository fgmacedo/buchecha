package config

import "testing"

func TestApplyDefaults_EnglishDefault(t *testing.T) {
	var c Config
	ApplyDefaults(&c)

	if c.Project.Language != "en" {
		t.Errorf("Language = %q, want en", c.Project.Language)
	}
	if c.Spec.Format != "markdown_bcc" {
		t.Errorf("Spec.Format = %q, want markdown_bcc", c.Spec.Format)
	}
	if c.Spec.MarkdownBCC.PlanHeading != "## Implementation Plan" {
		t.Errorf("PlanHeading = %q", c.Spec.MarkdownBCC.PlanHeading)
	}
	if c.Spec.MarkdownBCC.JournalHeading != "## Execution Journal" {
		t.Errorf("JournalHeading = %q", c.Spec.MarkdownBCC.JournalHeading)
	}
	if c.Journal.Store != "markdown_inspec" {
		t.Errorf("Journal.Store = %q, want markdown_inspec", c.Journal.Store)
	}
	if c.Agent.Name != "claude" {
		t.Errorf("Agent.Name = %q, want claude", c.Agent.Name)
	}
	if c.Agent.Claude.Binary != "claude" {
		t.Errorf("Agent.Claude.Binary = %q", c.Agent.Claude.Binary)
	}
	if !c.Agent.Claude.ShouldSkipPermissions() {
		t.Errorf("ShouldSkipPermissions = false, want true (default)")
	}
	if c.Loop.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d", c.Loop.MaxIterations)
	}
	if got, want := c.Spec.MarkdownBCC.Dir, "docs/specs"; got != want {
		t.Errorf("Spec.MarkdownBCC.Dir = %q, want %q", got, want)
	}
	if len(c.Env.Files) != 1 || c.Env.Files[0] != ".env" {
		t.Errorf("Env.Files = %v, want [.env]", c.Env.Files)
	}
}

func TestApplyDefaults_PtBR(t *testing.T) {
	c := Config{Project: Project{Language: "pt-BR"}}
	ApplyDefaults(&c)
	if c.Spec.MarkdownBCC.PlanHeading != "## Plano de implementação" {
		t.Errorf("PlanHeading = %q", c.Spec.MarkdownBCC.PlanHeading)
	}
	if c.Spec.MarkdownBCC.JournalHeading != "## Diário de execução" {
		t.Errorf("JournalHeading = %q", c.Spec.MarkdownBCC.JournalHeading)
	}
}

func TestApplyDefaults_DoesNotOverwriteExplicit(t *testing.T) {
	c := Config{
		Project: Project{Language: "en"},
		Spec:    Spec{MarkdownBCC: SpecMarkdownBCC{PlanHeading: "## Custom"}},
		Loop:    Loop{MaxIterations: 5},
	}
	ApplyDefaults(&c)
	if c.Spec.MarkdownBCC.PlanHeading != "## Custom" {
		t.Errorf("PlanHeading should not be overwritten")
	}
	if c.Loop.MaxIterations != 5 {
		t.Errorf("MaxIterations should not be overwritten, got %d", c.Loop.MaxIterations)
	}
}

func TestApplyDefaults_UnknownLanguageFallsBackToEn(t *testing.T) {
	c := Config{Project: Project{Language: "klingon"}}
	ApplyDefaults(&c)
	if c.Spec.MarkdownBCC.PlanHeading != "## Implementation Plan" {
		t.Errorf("unknown language should fall back to en, got %q", c.Spec.MarkdownBCC.PlanHeading)
	}
}

func TestApplyDefaults_SkipPermissionsExplicitFalseRespected(t *testing.T) {
	v := false
	c := Config{Agent: Agent{Claude: AgentClaude{SkipPermissions: &v}}}
	ApplyDefaults(&c)
	if c.Agent.Claude.ShouldSkipPermissions() {
		t.Errorf("explicit false should not be overwritten by defaults")
	}
}

func TestApplyDefaults_SkipPermissionsExplicitTrueRespected(t *testing.T) {
	v := true
	c := Config{Agent: Agent{Claude: AgentClaude{SkipPermissions: &v}}}
	ApplyDefaults(&c)
	if !c.Agent.Claude.ShouldSkipPermissions() {
		t.Errorf("explicit true should remain true")
	}
}
