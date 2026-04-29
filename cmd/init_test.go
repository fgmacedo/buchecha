package cmd

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
		Language:     "en",
		Agent:        "claude",
		Binary:       "/usr/bin/claude",
		Model:        "claude-opus-4-7",
		SpecsDir:     "docs/specs",
		Mode:         "phase",
		MaxIter:      15,
		BranchPrefix: "feat",
		EnvFiles:     []string{".env", ".env.local"},
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
	if cfg.Executor.Binary != "/usr/bin/claude" {
		t.Errorf("Binary = %q", cfg.Executor.Binary)
	}
	if cfg.Executor.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q", cfg.Executor.Model)
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
	if cfg.Specs.PlanHeading != "## Implementation Plan" {
		t.Errorf("PlanHeading default not applied: %q", cfg.Specs.PlanHeading)
	}
}

func TestWriteConfigTOML_OmitsModelWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".bcc.toml")
	in := initInput{
		Language:     "pt-BR",
		Agent:        "custom",
		Binary:       "/path/to/custom",
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
	if strings.Contains(string(b), "model =") {
		t.Errorf("model line should be omitted when Model is empty:\n%s", string(b))
	}

	cfg, err := configloader.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Specs.PlanHeading != "## Plano de implementação" {
		t.Errorf("pt-BR PlanHeading default not applied: %q", cfg.Specs.PlanHeading)
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
