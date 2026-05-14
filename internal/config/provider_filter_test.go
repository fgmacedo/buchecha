package config

import (
	"strings"
	"testing"
)

func makeCfg() *Config {
	return &Config{
		Roles: Roles{
			Planner: RolePolicy{Options: []RoleOption{
				{Provider: "claude", Model: "claude-opus", Efforts: []string{"high"}},
				{Provider: "codex", Model: "gpt-5-codex", Efforts: []string{"medium"}},
			}},
			Briefer: RolePolicy{Options: []RoleOption{
				{Provider: "claude", Model: "claude-sonnet", Efforts: []string{"medium"}},
				{Provider: "codex", Model: "gpt-5", Efforts: []string{"medium"}},
			}},
			Executor: RolePolicy{Options: []RoleOption{
				{Provider: "claude", Model: "claude-sonnet", Efforts: []string{"medium"}},
				{Provider: "codex", Model: "gpt-5-codex", Efforts: []string{"medium"}},
			}},
			Reviewer: RolePolicy{Options: []RoleOption{
				{Provider: "claude", Model: "claude-sonnet", Efforts: []string{"medium"}},
				{Provider: "codex", Model: "gpt-5", Efforts: []string{"medium"}},
			}},
		},
	}
}

func providersOf(opts []RoleOption) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.Provider)
	}
	return out
}

func TestFilterProviders_NoOp(t *testing.T) {
	c := makeCfg()
	if err := FilterProviders(c, "", ""); err != nil {
		t.Fatalf("FilterProviders no-op: unexpected err: %v", err)
	}
	if got := len(c.Roles.Planner.Options); got != 2 {
		t.Errorf("Planner options = %d, want 2 (untouched)", got)
	}
}

func TestFilterProviders_GlobalAppliesToAllRoles(t *testing.T) {
	c := makeCfg()
	if err := FilterProviders(c, "codex", ""); err != nil {
		t.Fatalf("FilterProviders global codex: unexpected err: %v", err)
	}
	for _, tc := range []struct {
		role string
		opts []RoleOption
	}{
		{"planner", c.Roles.Planner.Options},
		{"briefer", c.Roles.Briefer.Options},
		{"executor", c.Roles.Executor.Options},
		{"reviewer", c.Roles.Reviewer.Options},
	} {
		if len(tc.opts) != 1 {
			t.Errorf("%s options = %d, want 1", tc.role, len(tc.opts))
		}
		if len(tc.opts) > 0 && tc.opts[0].Provider != "codex" {
			t.Errorf("%s[0].Provider = %q, want codex", tc.role, tc.opts[0].Provider)
		}
	}
}

func TestFilterProviders_PlannerOverridesGlobal(t *testing.T) {
	c := makeCfg()
	if err := FilterProviders(c, "claude", "codex"); err != nil {
		t.Fatalf("FilterProviders mixed: unexpected err: %v", err)
	}
	if got := providersOf(c.Roles.Planner.Options); len(got) != 1 || got[0] != "codex" {
		t.Errorf("Planner providers = %v, want [codex]", got)
	}
	for _, role := range []struct {
		name string
		opts []RoleOption
	}{
		{"briefer", c.Roles.Briefer.Options},
		{"executor", c.Roles.Executor.Options},
		{"reviewer", c.Roles.Reviewer.Options},
	} {
		if got := providersOf(role.opts); len(got) != 1 || got[0] != "claude" {
			t.Errorf("%s providers = %v, want [claude]", role.name, got)
		}
	}
}

func TestFilterProviders_PlannerOnly(t *testing.T) {
	c := makeCfg()
	if err := FilterProviders(c, "", "codex"); err != nil {
		t.Fatalf("FilterProviders planner-only: unexpected err: %v", err)
	}
	if got := providersOf(c.Roles.Planner.Options); len(got) != 1 || got[0] != "codex" {
		t.Errorf("Planner providers = %v, want [codex]", got)
	}
	if got := len(c.Roles.Briefer.Options); got != 2 {
		t.Errorf("Briefer options = %d, want 2 (untouched by --planner)", got)
	}
}

func TestFilterProviders_UnknownProvider_ErrorsOnFirstEmptyRole(t *testing.T) {
	c := makeCfg()
	err := FilterProviders(c, "gemini", "")
	if err == nil {
		t.Fatal("FilterProviders gemini: want error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "planner") {
		t.Errorf("error should mention the offending role; got %q", msg)
	}
	if !strings.Contains(msg, "gemini") {
		t.Errorf("error should mention the offending provider; got %q", msg)
	}
}

func TestFilterProviders_PlannerOnly_RoleEmpty(t *testing.T) {
	c := makeCfg()
	err := FilterProviders(c, "", "gemini")
	if err == nil {
		t.Fatal("FilterProviders --planner=gemini: want error, got nil")
	}
	if !strings.Contains(err.Error(), "planner") {
		t.Errorf("error should mention planner; got %q", err)
	}
}

func TestFilterProviders_NilSafe(t *testing.T) {
	if err := FilterProviders(nil, "codex", "claude"); err != nil {
		t.Errorf("nil cfg should be no-op, got err: %v", err)
	}
}
