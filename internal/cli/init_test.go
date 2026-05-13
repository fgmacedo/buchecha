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
	if cfg.Loop.RetryBudget != 3 {
		t.Errorf("Loop.RetryBudget = %d, want 3", cfg.Loop.RetryBudget)
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
	for _, want := range []string{"[providers.claude]", "[loop]", "retry_budget = 3", "max_iterations = 10"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("init output missing %q:\n%s", want, string(b))
		}
	}

	cfg, err := configloader.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Loop.RetryBudget != 3 {
		t.Errorf("Loop.RetryBudget = %d, want 3", cfg.Loop.RetryBudget)
	}
	if cfg.Providers["claude"].Binary != "claude" {
		t.Errorf("Providers[claude].Binary = %q, want claude", cfg.Providers["claude"].Binary)
	}
}

// withCodexOnPath plants a fake codex binary in a temp PATH-only
// directory alongside claude so exec.LookPath("codex") succeeds.
func withCodexOnPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		bin := filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

// TestWriteConfigTOML_CodexEnabled verifies that when UseCodex is true,
// WriteConfigTOML emits [providers.codex] and a [[roles.executor.options]]
// entry with provider = "codex".
func TestWriteConfigTOML_CodexEnabled(t *testing.T) {
	withCodexOnPath(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:             "en",
		JournalStore:         "markdown_inspec",
		Binary:               "claude",
		MaxIter:              20,
		BranchPrefix:         "feat",
		EnvFiles:             []string{".env"},
		SkipPermissions:      true,
		UseCodex:             true,
		CodexBinary:          "codex",
		CodexSkipPermissions: true,
	}
	if err := WriteConfigTOML(path, in); err != nil {
		t.Fatalf("WriteConfigTOML: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	for _, want := range []string{
		"[providers.codex]",
		`binary = "codex"`,
		`provider = "codex"`,
		`model = "gpt-5.3-codex"`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("init output missing %q:\n%s", want, content)
		}
	}

	cfg, err := configloader.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	codexCfg, ok := cfg.Providers["codex"]
	if !ok {
		t.Fatal("cfg.Providers[codex] not present after round-trip")
	}
	if codexCfg.Binary != "codex" {
		t.Errorf("Providers[codex].Binary = %q, want codex", codexCfg.Binary)
	}
	if !codexCfg.ShouldSkipPermissions() {
		t.Error("Providers[codex].SkipPermissions should be true")
	}
	hasCodexExecutorOption := false
	for _, opt := range cfg.Roles.Executor.Options {
		if opt.Provider == "codex" {
			hasCodexExecutorOption = true
			break
		}
	}
	if !hasCodexExecutorOption {
		t.Error("roles.executor.options has no entry with provider=codex")
	}
}

// TestWriteConfigTOML_CodexAbsent verifies that when UseCodex is false,
// WriteConfigTOML does not emit an active [providers.codex] section.
func TestWriteConfigTOML_CodexAbsent(t *testing.T) {
	withClaudeOnPath(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:        "en",
		JournalStore:    "markdown_inspec",
		Binary:          "claude",
		MaxIter:         20,
		BranchPrefix:    "feat",
		EnvFiles:        []string{".env"},
		SkipPermissions: true,
		UseCodex:        false,
	}
	if err := WriteConfigTOML(path, in); err != nil {
		t.Fatalf("WriteConfigTOML: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	// The section must appear only as a comment, not as an active heading.
	if strings.Contains(content, "\n[providers.codex]") {
		t.Errorf("init output must not contain active [providers.codex] when UseCodex=false:\n%s", content)
	}
}

// TestRunInitWizard_CodexOfferedWhenOnPath exercises the wizard with
// simulated stdin confirming codex when codex is on PATH.
func TestRunInitWizard_CodexOfferedWhenOnPath(t *testing.T) {
	withCodexOnPath(t)
	dir := t.TempDir()
	target := filepath.Join(dir, ".bcc.toml")

	// Answers: language=en, binary=claude, journal=markdown_inspec,
	// maxiter=20, branchprefix=feat, envfiles=.env, skip=yes, codex=yes,
	// codexbinary=codex, codexskip=yes.
	stdin := strings.NewReader("en\nclaude\nmarkdown_inspec\n20\nfeat\n.env\nyes\nyes\ncodex\nyes\n")
	var out strings.Builder
	if err := runInitWizard(stdin, &out, target); err != nil {
		t.Fatalf("runInitWizard: %v", err)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, "[providers.codex]") {
		t.Errorf("expected [providers.codex] in generated TOML:\n%s", content)
	}
	if !strings.Contains(content, `provider = "codex"`) {
		t.Errorf("expected executor option with provider=codex in generated TOML:\n%s", content)
	}
}

// TestRunInitWizard_CodexNotOfferedWhenAbsent exercises the wizard when
// codex is NOT on PATH; the user is never prompted and no codex section appears.
func TestRunInitWizard_CodexNotOfferedWhenAbsent(t *testing.T) {
	withClaudeOnPath(t) // PATH has only claude
	dir := t.TempDir()
	target := filepath.Join(dir, ".bcc.toml")

	// Answers: language, binary, journal, maxiter, branchprefix, envfiles, skip.
	// No codex prompt should appear.
	stdin := strings.NewReader("en\nclaude\nmarkdown_inspec\n20\nfeat\n.env\nyes\n")
	var out strings.Builder
	if err := runInitWizard(stdin, &out, target); err != nil {
		t.Fatalf("runInitWizard: %v", err)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if strings.Contains(content, "\n[providers.codex]") {
		t.Errorf("codex section must not appear when codex is not on PATH:\n%s", content)
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
