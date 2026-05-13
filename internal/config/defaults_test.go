package config

import (
	"slices"
	"testing"
)

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
	if c.Loop.RetryBudget != 3 {
		t.Errorf("Loop.RetryBudget = %d, want 3", c.Loop.RetryBudget)
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

// TestApplyDefaults_DefaultsDerivedFromKnownProviders pins the contract
// that defaults are derived from knownProviders × defaultRoleTiers.
// When more than one provider has a model at the same tier (claude and
// codex both expose balanced), both must appear in the role menu, in
// registry order. The test fails loudly if a provider is added to
// knownProviders without being picked up here.
func TestApplyDefaults_DefaultsDerivedFromKnownProviders(t *testing.T) {
	var c Config
	ApplyDefaults(&c)

	t.Run("planner: one entry per provider with a frontier model, registry order", func(t *testing.T) {
		got := c.Roles.Planner.Options
		want := []RoleOption{
			{Provider: "claude", Model: "claude-opus-4-7", Efforts: []string{"high"}},
			{Provider: "codex", Model: "gpt-5.5", Efforts: []string{"high"}},
		}
		assertRoleOptions(t, got, want)
	})

	t.Run("briefer: tie at balanced includes both providers with curated efforts", func(t *testing.T) {
		got := c.Roles.Briefer.Options
		want := []RoleOption{
			{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: []string{"medium"}},
			{Provider: "codex", Model: "gpt-5.3-codex", Efforts: []string{"medium"}},
		}
		assertRoleOptions(t, got, want)
	})

	t.Run("reviewer: tie at balanced includes both providers with curated efforts", func(t *testing.T) {
		got := c.Roles.Reviewer.Options
		want := []RoleOption{
			{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: []string{"medium"}},
			{Provider: "codex", Model: "gpt-5.3-codex", Efforts: []string{"medium"}},
		}
		assertRoleOptions(t, got, want)
	})

	t.Run("executor: balanced→frontier→fast across providers with widened efforts", func(t *testing.T) {
		got := c.Roles.Executor.Options
		wide := []string{"low", "medium", "high"}
		want := []RoleOption{
			{Provider: "claude", Model: "claude-sonnet-4-6", Efforts: wide},
			{Provider: "codex", Model: "gpt-5.3-codex", Efforts: wide},
			{Provider: "claude", Model: "claude-opus-4-7", Efforts: wide},
			{Provider: "codex", Model: "gpt-5.5", Efforts: wide},
			{Provider: "claude", Model: "claude-haiku-4-5", Efforts: wide},
			{Provider: "codex", Model: "gpt-5.4-mini", Efforts: wide},
		}
		assertRoleOptions(t, got, want)
	})
}

func assertRoleOptions(t *testing.T, got, want []RoleOption) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d\ngot:  %+v\nwant: %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i].Provider != want[i].Provider || got[i].Model != want[i].Model {
			t.Errorf("[%d] = %s/%s, want %s/%s",
				i, got[i].Provider, got[i].Model, want[i].Provider, want[i].Model)
		}
		if !slices.Equal(got[i].Efforts, want[i].Efforts) {
			t.Errorf("[%d] %s/%s efforts = %v, want %v",
				i, got[i].Provider, got[i].Model, got[i].Efforts, want[i].Efforts)
		}
	}
}
