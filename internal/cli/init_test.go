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
		SpecFormat:      "markdown_bcc",
		JournalStore:    "markdown_inspec",
		AgentName:       "claude",
		Binary:          "/usr/bin/claude",
		Model:           "claude-opus-4-7",
		SpecsDir:        "docs/specs",
		Mode:            "phase",
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
	if cfg.Spec.Format != "markdown_bcc" {
		t.Errorf("Spec.Format = %q", cfg.Spec.Format)
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
	// Defaults are applied during Load: en plan heading should be filled.
	if cfg.Spec.MarkdownBCC.PlanHeading != "## Implementation Plan" {
		t.Errorf("PlanHeading default not applied: %q", cfg.Spec.MarkdownBCC.PlanHeading)
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
		SpecFormat:      "markdown_bcc",
		JournalStore:    "markdown_inspec",
		AgentName:       "claude",
		Binary:          "/usr/bin/claude",
		SpecsDir:        "docs/specs",
		Mode:            "phase",
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
		SpecFormat:   "markdown_bcc",
		JournalStore: "markdown_inspec",
		AgentName:    "claude",
		Binary:       "/path/to/claude",
		Model:        "",
		SpecsDir:     "docs/specs",
		Mode:         "phase",
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
	// The active agent block writes binary + extra_args + skip_permissions
	// but should omit `model =` when empty.
	if strings.Contains(string(b), "model = \"\"") {
		t.Errorf("empty model should be omitted, not written as empty string:\n%s", string(b))
	}

	cfg, err := configloader.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Spec.MarkdownBCC.PlanHeading != "## Plano de implementação" {
		t.Errorf("pt-BR PlanHeading default not applied: %q", cfg.Spec.MarkdownBCC.PlanHeading)
	}
}

func TestWriteConfigTOML_JournalFileWritesPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:        "en",
		SpecFormat:      "markdown_bcc",
		JournalStore:    "file",
		JournalFilePath: ".bcc/journal.ndjson",
		AgentName:       "claude",
		Binary:          "/usr/bin/claude",
		SpecsDir:        "docs/specs",
		Mode:            "phase",
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
