package config

import "testing"

func TestApplyDefaults_EnglishDefault(t *testing.T) {
	var c Config
	ApplyDefaults(&c)

	if c.Project.Language != "en" {
		t.Errorf("Language = %q, want en", c.Project.Language)
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
	if len(c.Env.Files) != 1 || c.Env.Files[0] != ".env" {
		t.Errorf("Env.Files = %v, want [.env]", c.Env.Files)
	}
}

func TestApplyDefaults_DoesNotOverwriteExplicit(t *testing.T) {
	c := Config{
		Project: Project{Language: "en"},
		Loop:    Loop{MaxIterations: 5},
	}
	ApplyDefaults(&c)
	if c.Loop.MaxIterations != 5 {
		t.Errorf("MaxIterations should not be overwritten, got %d", c.Loop.MaxIterations)
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

func TestApplyDefaults_DirectorDefaults(t *testing.T) {
	var c Config
	ApplyDefaults(&c)
	if c.Director.RetryBudget != 2 {
		t.Errorf("Director.RetryBudget = %d, want 2", c.Director.RetryBudget)
	}
	if c.Director.Claude.Binary != "claude" {
		t.Errorf("Director.Claude.Binary = %q, want claude", c.Director.Claude.Binary)
	}
	if c.Director.Claude.MaxBudgetUSD != 0 {
		t.Errorf("Director.Claude.MaxBudgetUSD = %v, want 0", c.Director.Claude.MaxBudgetUSD)
	}
}

func TestApplyDefaults_DirectorDoesNotOverwriteExplicit(t *testing.T) {
	c := Config{Director: DirectorConfig{
		RetryBudget: 5,
		Claude: DirectorClaude{
			Binary:       "/opt/claude",
			MaxBudgetUSD: 1.5,
		},
	}}
	ApplyDefaults(&c)
	if c.Director.RetryBudget != 5 {
		t.Errorf("explicit RetryBudget should not be overwritten, got %d", c.Director.RetryBudget)
	}
	if c.Director.Claude.Binary != "/opt/claude" {
		t.Errorf("explicit Director.Claude.Binary should not be overwritten, got %q", c.Director.Claude.Binary)
	}
	if c.Director.Claude.MaxBudgetUSD != 1.5 {
		t.Errorf("explicit MaxBudgetUSD should not be overwritten, got %v", c.Director.Claude.MaxBudgetUSD)
	}
}
