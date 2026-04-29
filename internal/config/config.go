// Package config defines the typed Config that maps to .bcc.toml.
//
// This package is stdlib-only: no TOML parser, no env libraries. Loading a
// Config from disk is the responsibility of internal/configloader/<format>.
// Env merging (precedence rules and process-environment application) lives
// here because it operates on an already-parsed Config and does not need
// the format-specific decoder.
package config

// Config mirrors the shape of .bcc.toml. Tags are TOML field names; the
// adapter in configloader/toml uses them to decode. Domain code (loop,
// spec, cmd) reads these as plain Go fields.
type Config struct {
	Project  Project  `toml:"project"`
	Executor Executor `toml:"executor"`
	Specs    Specs    `toml:"specs"`
	Loop     Loop     `toml:"loop"`
	Git      Git      `toml:"git"`
	Env      Env      `toml:"env"`
}

// Project holds top-level project settings.
type Project struct {
	Language string `toml:"language"`
}

// Executor configures the agent subprocess invoked once per iteration.
type Executor struct {
	Agent     string   `toml:"agent"`
	Binary    string   `toml:"binary"`
	Model     string   `toml:"model"`
	ExtraArgs []string `toml:"extra_args"`

	// SkipPermissions, when true (the default), instructs the adapter to
	// suppress the agent's interactive permission prompts so the loop
	// can run end to end without human intervention. Each adapter maps
	// this to its own flag (claude: --dangerously-skip-permissions;
	// codex/gemini: TBD).
	//
	// This is a tristate via pointer: nil means "absent in TOML, use
	// default"; the default is true. Setting `skip_permissions = false`
	// in .bcc.toml is an explicit opt-out and the user accepts that
	// the loop will stall on prompts.
	//
	// Use ShouldSkipPermissions() to read; never dereference directly.
	SkipPermissions *bool `toml:"skip_permissions"`
}

// ShouldSkipPermissions returns the effective value of the
// SkipPermissions tristate, applying the default (true) when absent.
func (e Executor) ShouldSkipPermissions() bool {
	if e.SkipPermissions == nil {
		return true
	}
	return *e.SkipPermissions
}

// Specs holds spec discovery and parsing keywords. Heading strings and
// the Result field name are localized; they default by Project.Language
// when zero.
type Specs struct {
	Dir            string `toml:"dir"`
	PlanHeading    string `toml:"plan_heading"`
	JournalHeading string `toml:"journal_heading"`
	ResultKeyword  string `toml:"result_keyword"`
}

// Loop configures the iteration loop and the localized Result vocabulary.
type Loop struct {
	Mode          string  `toml:"mode"`
	MaxIterations int     `toml:"max_iterations"`
	Results       Results `toml:"results"`
}

// Results is the localized vocabulary for the journal Result field.
type Results struct {
	OK      string `toml:"ok"`
	Partial string `toml:"partial"`
	Done    string `toml:"done"`
	Blocked string `toml:"blocked"`
}

// Git holds git-related settings.
type Git struct {
	BranchPrefix              string `toml:"branch_prefix"`
	RequireCommitPerIteration bool   `toml:"require_commit_per_iteration"`
}

// Env carries env loading settings.
type Env struct {
	Files []string          `toml:"files"`
	Vars  map[string]string `toml:"vars"`
}
