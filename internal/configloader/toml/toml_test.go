package toml

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// withOnlyBinaries sets PATH to a temp directory containing only the
// named executable stubs. Anything else (including the contributor's
// real claude/codex installs) becomes invisible to exec.LookPath for
// the test's duration, so availability assertions are deterministic.
func withOnlyBinaries(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		path := filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_FullExample(t *testing.T) {
	withOnlyBinaries(t, "claude")

	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	writeFile(t, path, `
[project]
language = "pt-BR"

[journal]
store = "markdown_inspec"

[providers.claude]
extra_args = ["--verbose"]
max_budget_usd = 2.5

[loop]
max_iterations = 10
retry_budget = 4

[git]
branch_prefix = "feat"
require_commit_per_iteration = true

[debug]
capture_subprocess_logs = true
capture_subprocess_stdout = false

[env]
files = [".env"]

[env.vars]
FOO = "bar"
`)

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Project.Language != "pt-BR" {
		t.Errorf("Language = %q", c.Project.Language)
	}
	claude := c.Providers["claude"]
	if len(claude.ExtraArgs) != 1 || claude.ExtraArgs[0] != "--verbose" {
		t.Errorf("Providers[claude].ExtraArgs = %v", claude.ExtraArgs)
	}
	if claude.MaxBudgetUSD != 2.5 {
		t.Errorf("Providers[claude].MaxBudgetUSD = %v, want 2.5", claude.MaxBudgetUSD)
	}
	if c.Loop.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d", c.Loop.MaxIterations)
	}
	if c.Loop.RetryBudget != 4 {
		t.Errorf("Loop.RetryBudget = %d, want 4", c.Loop.RetryBudget)
	}
	if c.Journal.Store != "markdown_inspec" {
		t.Errorf("Journal.Store = %q", c.Journal.Store)
	}
	if c.Env.Vars["FOO"] != "bar" {
		t.Errorf("Env.Vars[FOO] = %q", c.Env.Vars["FOO"])
	}
	if !c.Git.RequireCommitPerIteration {
		t.Errorf("Git.RequireCommitPerIteration = false")
	}
	if !c.Debug.IsCaptureSubprocessLogsEnabled() {
		t.Errorf("Debug.CaptureSubprocessLogs not honored from TOML")
	}
	if c.Debug.IsCaptureSubprocessStdoutEnabled() {
		t.Errorf("Debug.CaptureSubprocessStdout = true, want false (explicit)")
	}
}

func TestLoad_RolesPopulatedWithDefaults(t *testing.T) {
	withOnlyBinaries(t, "claude")
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	writeFile(t, path, `[project]
language = "en"
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for role, policy := range map[string]int{
		"planner":  len(c.Roles.Planner.Options),
		"briefer":  len(c.Roles.Briefer.Options),
		"executor": len(c.Roles.Executor.Options),
		"reviewer": len(c.Roles.Reviewer.Options),
	} {
		if policy == 0 {
			t.Errorf("role %s has no options after Load", role)
		}
	}
}

func TestLoad_ProviderNotInPathFiltersOptions(t *testing.T) {
	// No known provider on PATH. Defaults reference every known provider
	// (claude, codex), but the availability filter strips all of them,
	// leaving every role with an empty options list. Expect an error.
	withOnlyBinaries(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	writeFile(t, path, `[project]
language = "en"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected availability filter error, got nil")
	}
}

func TestLoad_DefaultsCoverEveryKnownProviderOnPath(t *testing.T) {
	// Inverse of TestLoad_ProviderNotInPathFiltersOptions: with codex
	// alone on PATH and no explicit role menus, Load succeeds because
	// the default role options now include codex via the tier-driven
	// derivation in internal/config/defaults.go.
	withOnlyBinaries(t, "codex")
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	writeFile(t, path, `[project]
language = "en"
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Roles.Planner.Options) == 0 {
		t.Fatalf("Planner has no options with codex-only PATH")
	}
	if got := c.Roles.Planner.Options[0].Provider; got != "codex" {
		t.Errorf("Planner Options[0].Provider = %q, want codex", got)
	}
}

func TestLoad_WebuiBlock(t *testing.T) {
	withOnlyBinaries(t, "claude")
	cases := []struct {
		name        string
		toml        string
		wantEnabled bool
		wantOpen    bool
	}{
		{
			name:        "block absent defaults to off",
			toml:        "[project]\nlanguage = \"en\"\n",
			wantEnabled: false,
			wantOpen:    false,
		},
		{
			name: "explicit true honored",
			toml: `[webui]
enabled = true
open = true
`,
			wantEnabled: true,
			wantOpen:    true,
		},
		{
			name: "partial set leaves the other field at zero",
			toml: `[webui]
enabled = true
`,
			wantEnabled: true,
			wantOpen:    false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".bcc.toml")
			writeFile(t, path, tt.toml)
			c, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if c.Webui.Enabled != tt.wantEnabled {
				t.Errorf("Webui.Enabled = %v, want %v", c.Webui.Enabled, tt.wantEnabled)
			}
			if c.Webui.Open != tt.wantOpen {
				t.Errorf("Webui.Open = %v, want %v", c.Webui.Open, tt.wantOpen)
			}
		})
	}
}

func TestLoad_DebugDefaultsOff(t *testing.T) {
	withOnlyBinaries(t, "claude")
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	writeFile(t, path, `[project]
language = "en"
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Debug.IsCaptureSubprocessLogsEnabled() {
		t.Errorf("default Debug.CaptureSubprocessLogs should be false")
	}
	if c.Debug.IsCaptureSubprocessStdoutEnabled() {
		t.Errorf("default Debug.CaptureSubprocessStdout should be false")
	}
}

func TestLoad_NotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/.bcc.toml")
	if err == nil {
		t.Fatalf("expected error on missing file")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err should wrap fs.ErrNotExist, got: %v", err)
	}
}

func TestLoad_InvalidTOML(t *testing.T) {
	withOnlyBinaries(t, "claude")
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	writeFile(t, path, "this is not = valid = toml [")
	_, err := Load(path)
	if err == nil {
		t.Errorf("expected decode error")
	}
}

func TestDiscover_FoundUpThree(t *testing.T) {
	withOnlyBinaries(t, "claude")
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, ".bcc.toml")
	writeFile(t, cfgPath, "[project]\nlanguage = \"en\"\n")

	c, found, err := Discover(sub)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if found != cfgPath {
		t.Errorf("found = %q, want %q", found, cfgPath)
	}
	if c.Project.Language != "en" {
		t.Errorf("Language = %q", c.Project.Language)
	}
}

func TestDiscover_PicksClosestAncestor(t *testing.T) {
	withOnlyBinaries(t, "claude")
	root := t.TempDir()
	mid := filepath.Join(root, "mid")
	leaf := filepath.Join(mid, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	// Both have configs; expect mid wins (closest to leaf).
	writeFile(t, filepath.Join(root, ".bcc.toml"), "[project]\nlanguage = \"en\"\n")
	writeFile(t, filepath.Join(mid, ".bcc.toml"), "[project]\nlanguage = \"pt-BR\"\n")

	c, found, err := Discover(leaf)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if want := filepath.Join(mid, ".bcc.toml"); found != want {
		t.Errorf("found = %q, want %q (closest)", found, want)
	}
	if c.Project.Language != "pt-BR" {
		t.Errorf("Language = %q, want pt-BR", c.Project.Language)
	}
}

func TestDiscover_NotFoundReturnsDefaults(t *testing.T) {
	withOnlyBinaries(t, "claude")
	dir := t.TempDir()
	c, _, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// Defaults must be applied either way.
	if c.Project.Language == "" {
		t.Errorf("Language is empty; defaults not applied")
	}
}
