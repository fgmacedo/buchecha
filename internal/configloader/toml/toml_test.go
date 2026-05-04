package toml

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

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
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	writeFile(t, path, `
[project]
language = "pt-BR"

[journal]
store = "markdown_inspec"

[agent]
name = "claude"

[agent.claude]
binary = "/usr/bin/claude"
extra_args = ["--verbose"]

[loop]
max_iterations = 10

[git]
branch_prefix = "feat"
require_commit_per_iteration = true

[director]
retry_budget = 4

[director.claude]
binary = "/usr/local/bin/claude"
model = "claude-opus-4-7"
extra_args = ["--verbose"]
max_budget_usd = 2.5

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
	if c.Agent.Name != "claude" {
		t.Errorf("Agent.Name = %q", c.Agent.Name)
	}
	if c.Agent.Claude.Binary != "/usr/bin/claude" {
		t.Errorf("Agent.Claude.Binary = %q", c.Agent.Claude.Binary)
	}
	if len(c.Agent.Claude.ExtraArgs) != 1 || c.Agent.Claude.ExtraArgs[0] != "--verbose" {
		t.Errorf("ExtraArgs = %v", c.Agent.Claude.ExtraArgs)
	}
	if c.Loop.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d", c.Loop.MaxIterations)
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
	if c.Director.RetryBudget != 4 {
		t.Errorf("Director.RetryBudget = %d, want 4", c.Director.RetryBudget)
	}
	if c.Director.Claude.Binary != "/usr/local/bin/claude" {
		t.Errorf("Director.Claude.Binary = %q", c.Director.Claude.Binary)
	}
	if c.Director.Claude.Model != "claude-opus-4-7" {
		t.Errorf("Director.Claude.Model = %q", c.Director.Claude.Model)
	}
	if len(c.Director.Claude.ExtraArgs) != 1 || c.Director.Claude.ExtraArgs[0] != "--verbose" {
		t.Errorf("Director.Claude.ExtraArgs = %v", c.Director.Claude.ExtraArgs)
	}
	if c.Director.Claude.MaxBudgetUSD != 2.5 {
		t.Errorf("Director.Claude.MaxBudgetUSD = %v, want 2.5", c.Director.Claude.MaxBudgetUSD)
	}
	if !c.Debug.IsCaptureSubprocessLogsEnabled() {
		t.Errorf("Debug.CaptureSubprocessLogs not honored from TOML")
	}
	if c.Debug.IsCaptureSubprocessStdoutEnabled() {
		t.Errorf("Debug.CaptureSubprocessStdout = true, want false (explicit)")
	}
}

func TestLoad_WebuiBlock(t *testing.T) {
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
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	writeFile(t, path, "this is not = valid = toml [")
	_, err := Load(path)
	if err == nil {
		t.Errorf("expected decode error")
	}
}

func TestDiscover_FoundUpThree(t *testing.T) {
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
	// A test running on a host where /tmp lacks .bcc.toml in any ancestor
	// of t.TempDir(). We rely on the conventional case; if a stray
	// .bcc.toml exists somewhere up, the test will surface it as a real
	// finding rather than an error.
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
