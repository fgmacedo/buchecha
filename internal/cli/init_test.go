package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	configloader "github.com/fgmacedo/buchecha/internal/configloader/toml"
)

// withClaudeOnPath plants a fake claude binary in a temp PATH-only
// directory so configloader's host-availability filter passes deterministically.
func withClaudeOnPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestWriteConfigTOML_RoundTripEnglish(t *testing.T) {
	withClaudeOnPath(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:        "en",
		JournalStore:    "markdown_inspec",
		Binary:          "claude",
		MaxIter:         15,
		BranchPrefix:    "feat",
		EnvFiles:        []string{".env", ".env.local"},
		SkipPermissions: true,
	}
	if err := WriteConfigTOML(path, in); err != nil {
		t.Fatalf("WriteConfigTOML: %v", err)
	}
	cfg, err := configloader.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Project.Language != "en" {
		t.Errorf("Language = %q", cfg.Project.Language)
	}
	if cfg.Providers["claude"].Binary != "claude" {
		t.Errorf("Providers[claude].Binary = %q", cfg.Providers["claude"].Binary)
	}
	if cfg.Loop.MaxIterations != 15 {
		t.Errorf("MaxIterations = %d", cfg.Loop.MaxIterations)
	}
	if cfg.Loop.RetryBudget != 2 {
		t.Errorf("Loop.RetryBudget = %d, want 2", cfg.Loop.RetryBudget)
	}
	if cfg.Git.BranchPrefix != "feat" {
		t.Errorf("BranchPrefix = %q", cfg.Git.BranchPrefix)
	}
	if len(cfg.Env.Files) != 2 || cfg.Env.Files[0] != ".env" || cfg.Env.Files[1] != ".env.local" {
		t.Errorf("Env.Files = %v", cfg.Env.Files)
	}
	if !cfg.Providers["claude"].ShouldSkipPermissions() {
		t.Errorf("SkipPermissions should be true after round-trip")
	}
}

func TestWriteConfigTOML_SkipPermissionsFalseRoundTrips(t *testing.T) {
	withClaudeOnPath(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:        "en",
		JournalStore:    "markdown_inspec",
		Binary:          "claude",
		MaxIter:         5,
		BranchPrefix:    "feat",
		EnvFiles:        []string{".env"},
		SkipPermissions: false,
	}
	if err := WriteConfigTOML(path, in); err != nil {
		t.Fatalf("WriteConfigTOML: %v", err)
	}
	cfg, err := configloader.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Providers["claude"].ShouldSkipPermissions() {
		t.Errorf("SkipPermissions should be false after explicit opt-out")
	}
}

func TestWriteConfigTOML_JournalFileWritesPath(t *testing.T) {
	withClaudeOnPath(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:        "en",
		JournalStore:    "file",
		JournalFilePath: ".bcc/journal.ndjson",
		Binary:          "claude",
		MaxIter:         10,
		BranchPrefix:    "feat",
		EnvFiles:        []string{".env"},
	}
	if err := WriteConfigTOML(path, in); err != nil {
		t.Fatalf("WriteConfigTOML: %v", err)
	}
	cfg, err := configloader.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Journal.Store != "file" {
		t.Errorf("Journal.Store = %q, want file", cfg.Journal.Store)
	}
	if cfg.Journal.File.Path != ".bcc/journal.ndjson" {
		t.Errorf("Journal.File.Path = %q", cfg.Journal.File.Path)
	}
}

// TestWriteConfigTOML_WritesProvidersAndLoop verifies that `bcc init`
// emits the [providers.claude] and [loop] sections with the expected
// shape, since those are the new authoritative homes for binary,
// skip_permissions, retry_budget, and max_iterations.
func TestWriteConfigTOML_WritesProvidersAndLoop(t *testing.T) {
	withClaudeOnPath(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:        "en",
		JournalStore:    "markdown_inspec",
		Binary:          "claude",
		MaxIter:         10,
		BranchPrefix:    "feat",
		EnvFiles:        []string{".env"},
		SkipPermissions: true,
	}
	if err := WriteConfigTOML(path, in); err != nil {
		t.Fatalf("WriteConfigTOML: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[providers.claude]", "[loop]", "retry_budget = 2", "max_iterations = 10"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("init output missing %q:\n%s", want, string(b))
		}
	}

	cfg, err := configloader.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Loop.RetryBudget != 2 {
		t.Errorf("Loop.RetryBudget = %d, want 2", cfg.Loop.RetryBudget)
	}
	if cfg.Providers["claude"].Binary != "claude" {
		t.Errorf("Providers[claude].Binary = %q, want claude", cfg.Providers["claude"].Binary)
	}
}

func TestSplitTrim(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{".env", []string{".env"}},
		{".env, .env.local", []string{".env", ".env.local"}},
		{".env,,  .env.bcc  ", []string{".env", ".env.bcc"}},
		{"", nil},
	}
	for _, c := range cases {
		got := splitTrim(c.in, ",")
		if len(got) != len(c.want) {
			t.Errorf("splitTrim(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitTrim(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}
