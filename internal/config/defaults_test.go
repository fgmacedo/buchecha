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
	if c.Loop.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d", c.Loop.MaxIterations)
	}
	if c.Loop.RetryBudget != 2 {
		t.Errorf("Loop.RetryBudget = %d, want 2", c.Loop.RetryBudget)
	}
	if len(c.Env.Files) != 1 || c.Env.Files[0] != ".env" {
		t.Errorf("Env.Files = %v, want [.env]", c.Env.Files)
	}
}

func TestApplyDefaults_SeedsKnownProvider(t *testing.T) {
	var c Config
	ApplyDefaults(&c)

	claude, ok := c.Providers["claude"]
	if !ok {
		t.Fatalf("Providers[claude] missing after defaults")
	}
	if claude.Binary != "claude" {
		t.Errorf("claude binary = %q, want claude", claude.Binary)
	}
	if !claude.ShouldSkipPermissions() {
		t.Errorf("ShouldSkipPermissions = false, want true (default)")
	}
}

func TestApplyDefaults_DoesNotOverwriteExplicit(t *testing.T) {
	c := Config{
		Project: Project{Language: "en"},
		Loop:    Loop{MaxIterations: 5, RetryBudget: 7},
	}
	ApplyDefaults(&c)
	if c.Loop.MaxIterations != 5 {
		t.Errorf("MaxIterations should not be overwritten, got %d", c.Loop.MaxIterations)
	}
	if c.Loop.RetryBudget != 7 {
		t.Errorf("RetryBudget should not be overwritten, got %d", c.Loop.RetryBudget)
	}
}

func TestApplyDefaults_SkipPermissionsExplicitFalseRespected(t *testing.T) {
	v := false
	c := Config{Providers: map[string]Provider{
		"claude": {Binary: "claude", SkipPermissions: &v},
	}}
	ApplyDefaults(&c)
	if c.Providers["claude"].ShouldSkipPermissions() {
		t.Errorf("explicit false should not be overwritten by defaults")
	}
}

func TestApplyDefaults_SkipPermissionsExplicitTrueRespected(t *testing.T) {
	v := true
	c := Config{Providers: map[string]Provider{
		"claude": {Binary: "claude", SkipPermissions: &v},
	}}
	ApplyDefaults(&c)
	if !c.Providers["claude"].ShouldSkipPermissions() {
		t.Errorf("explicit true should remain true")
	}
}

func TestApplyDefaults_PopulatesDefaultRolesWhenAbsent(t *testing.T) {
	var c Config
	ApplyDefaults(&c)
	if len(c.Roles.Planner.Options) == 0 {
		t.Errorf("Planner options empty after defaults")
	}
	if len(c.Roles.Briefer.Options) == 0 {
		t.Errorf("Briefer options empty after defaults")
	}
	if len(c.Roles.Executor.Options) == 0 {
		t.Errorf("Executor options empty after defaults")
	}
	if len(c.Roles.Reviewer.Options) == 0 {
		t.Errorf("Reviewer options empty after defaults")
	}
}

func TestApplyDefaults_RolesNotOverwrittenWhenExplicit(t *testing.T) {
	custom := []RoleOption{
		{Provider: "claude", Model: "claude-haiku-4-5", Efforts: []string{"low"}},
	}
	c := Config{Roles: Roles{Executor: RolePolicy{Options: custom}}}
	ApplyDefaults(&c)
	if len(c.Roles.Executor.Options) != 1 || c.Roles.Executor.Options[0].Model != "claude-haiku-4-5" {
		t.Errorf("explicit Executor options were overwritten: %+v", c.Roles.Executor.Options)
	}
}

func TestApplyDefaults_PlannerDefaultsToFrontier(t *testing.T) {
	var c Config
	ApplyDefaults(&c)
	opt := c.Roles.Planner.Options[0]
	if opt.Provider != "claude" || opt.Model != "claude-opus-4-7" {
		t.Errorf("Planner default = %s/%s, want claude/claude-opus-4-7", opt.Provider, opt.Model)
	}
}
