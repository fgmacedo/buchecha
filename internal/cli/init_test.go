package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	configloader "github.com/fgmacedo/buchecha/internal/configloader/toml"
)

func TestWriteConfigTOML_RoundTripEnglish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:        "en",
		JournalStore:    "markdown_inspec",
		AgentName:       "claude",
		Binary:          "/usr/bin/claude",
		Model:           "claude-opus-4-7",
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
	if cfg.Agent.Name != "claude" {
		t.Errorf("Agent.Name = %q", cfg.Agent.Name)
	}
	if cfg.Agent.Claude.Binary != "/usr/bin/claude" {
		t.Errorf("Binary = %q", cfg.Agent.Claude.Binary)
	}
	if cfg.Agent.Claude.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q", cfg.Agent.Claude.Model)
	}
	if cfg.Loop.MaxIterations != 15 {
		t.Errorf("MaxIterations = %d", cfg.Loop.MaxIterations)
	}
	if cfg.Git.BranchPrefix != "feat" {
		t.Errorf("BranchPrefix = %q", cfg.Git.BranchPrefix)
	}
	if len(cfg.Env.Files) != 2 || cfg.Env.Files[0] != ".env" || cfg.Env.Files[1] != ".env.local" {
		t.Errorf("Env.Files = %v", cfg.Env.Files)
	}
	if !cfg.Agent.Claude.ShouldSkipPermissions() {
		t.Errorf("SkipPermissions should be true after round-trip")
	}
}

func TestWriteConfigTOML_SkipPermissionsFalseRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:        "en",
		JournalStore:    "markdown_inspec",
		AgentName:       "claude",
		Binary:          "/usr/bin/claude",
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
	if cfg.Agent.Claude.ShouldSkipPermissions() {
		t.Errorf("SkipPermissions should be false after explicit opt-out")
	}
}

func TestWriteConfigTOML_OmitsModelWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:     "pt-BR",
		JournalStore: "markdown_inspec",
		AgentName:    "claude",
		Binary:       "/path/to/claude",
		Model:        "",
		MaxIter:      20,
		BranchPrefix: "feat",
		EnvFiles:     []string{".env"},
	}
	if err := WriteConfigTOML(path, in); err != nil {
		t.Fatalf("WriteConfigTOML: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "model = \"\"") {
		t.Errorf("empty model should be omitted, not written as empty string:\n%s", string(b))
	}

	if _, err := configloader.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestWriteConfigTOML_JournalFileWritesPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:        "en",
		JournalStore:    "file",
		JournalFilePath: ".bcc/journal.ndjson",
		AgentName:       "claude",
		Binary:          "/usr/bin/claude",
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

// TestWriteConfigTOML_WritesDirectorSubtables verifies that `bcc init`
// emits the [director] and [director.claude] sections.
func TestWriteConfigTOML_WritesDirectorSubtables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:        "en",
		JournalStore:    "markdown_inspec",
		AgentName:       "claude",
		Binary:          "/usr/bin/claude",
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
	for _, want := range []string{"[director]", "retry_budget = 2", "[director.claude]", "max_budget_usd = 0"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("init output missing %q:\n%s", want, string(b))
		}
	}

	cfg, err := configloader.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Director.RetryBudget != 2 {
		t.Errorf("Director.RetryBudget = %d, want 2", cfg.Director.RetryBudget)
	}
	if cfg.Director.Claude.Binary != "claude" {
		t.Errorf("Director.Claude.Binary = %q, want claude", cfg.Director.Claude.Binary)
	}
	if cfg.Director.Claude.MaxBudgetUSD != 0 {
		t.Errorf("Director.Claude.MaxBudgetUSD = %v, want 0", cfg.Director.Claude.MaxBudgetUSD)
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
