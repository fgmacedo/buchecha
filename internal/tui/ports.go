package tui

import (
	"context"

	"github.com/fgmacedo/buchecha/internal/spec"
)

// GitProbe is the read-only git view the TUI consumes for the "if you
// close now" panel. The cli adapter (internal/git/cli) implements this
// alongside loop.GitProbe; structural typing keeps both ports
// independent so neither package imports the other.
type GitProbe interface {
	DirtyFileCount(ctx context.Context) (int, error)
}

// SpecReader is the file-loader the TUI uses to re-parse the spec
// after each IterationFinished (progress + journal status). The
// markdown adapter satisfies it via plain os.ReadFile.
type SpecReader interface {
	Read(path string) (string, error)
}

// SpecConfig bundles the parser inputs derived from .bcc.toml so the
// TUI can re-parse the spec independently of the loop. Built by the
// caller in cmd/run from cfg.Specs and cfg.Loop.Results.
type SpecConfig struct {
	PlanHeading    string
	JournalHeading string
	ResultKeyword  string
	ResultVocab    spec.ResultVocab
}
