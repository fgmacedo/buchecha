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

[executor]
agent = "claude"
binary = "/usr/bin/claude"
extra_args = ["--verbose"]

[specs]
dir = "specs"

[loop]
mode = "phase"
max_iterations = 10

[loop.results]
done = "feito"

[git]
branch_prefix = "feat"
require_commit_per_iteration = true

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
	if c.Executor.Binary != "/usr/bin/claude" {
		t.Errorf("Binary = %q", c.Executor.Binary)
	}
	if len(c.Executor.ExtraArgs) != 1 || c.Executor.ExtraArgs[0] != "--verbose" {
		t.Errorf("ExtraArgs = %v", c.Executor.ExtraArgs)
	}
	if c.Loop.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d", c.Loop.MaxIterations)
	}
	if c.Loop.Results.Done != "feito" {
		t.Errorf("Done = %q (expected explicit override)", c.Loop.Results.Done)
	}
	// Defaults are applied: pt-BR specs heading, partial="parcial".
	if c.Specs.PlanHeading != "## Plano de implementação" {
		t.Errorf("PlanHeading = %q (default not applied)", c.Specs.PlanHeading)
	}
	if c.Loop.Results.Partial != "parcial" {
		t.Errorf("Partial = %q (default not applied)", c.Loop.Results.Partial)
	}
	if c.Env.Vars["FOO"] != "bar" {
		t.Errorf("Env.Vars[FOO] = %q", c.Env.Vars["FOO"])
	}
	if !c.Git.RequireCommitPerIteration {
		t.Errorf("Git.RequireCommitPerIteration = false")
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
