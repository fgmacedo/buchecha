// Package config defines the typed Config that maps to .bcc.toml.
//
// This package is stdlib-only: no TOML parser, no env libraries. Loading a
// Config from disk is the responsibility of internal/configloader/<format>.
// Env merging (precedence rules and process-environment application) lives
// here because it operates on an already-parsed Config and does not need
// the format-specific decoder.
package config

// Config mirrors the shape of .bcc.toml. The hierarchical layout matches
// the spec-format and agent adapter pattern: a global selector plus
// per-adapter subtables. Adapters with no Go counterpart yet (openspec,
// kiro, codex, gemini) may exist in the TOML as stubs; they decode into
// nothing and are written by `bcc init` for forward-compat.
type Config struct {
	Project  Project        `toml:"project"`
	Journal  Journal        `toml:"journal"`
	Agent    Agent          `toml:"agent"`
	Loop     Loop           `toml:"loop"`
	Git      Git            `toml:"git"`
	Env      Env            `toml:"env"`
	Director DirectorConfig `toml:"director"`
}

// Project holds top-level project settings.
type Project struct {
	Language string `toml:"language"`
}

// Journal selects the journal-storage hint passed to the agent's
// prompt. bcc never reads the journal; the agent owns the write side.
type Journal struct {
	Store string      `toml:"store"`
	File  JournalFile `toml:"file"`
}

// JournalFile carries options consumed when [journal].store == "file".
type JournalFile struct {
	Path string `toml:"path"`
}

// Agent selects the active executor adapter and carries per-adapter
// options.
type Agent struct {
	Name   string      `toml:"name"`
	Claude AgentClaude `toml:"claude"`
}

// AgentClaude configures the claude executor adapter.
type AgentClaude struct {
	Binary    string   `toml:"binary"`
	Model     string   `toml:"model"`
	ExtraArgs []string `toml:"extra_args"`

	// SkipPermissions, when true (the default), instructs the adapter to
	// suppress the agent's interactive permission prompts so the loop
	// can run end to end without human intervention. claude maps this
	// to --dangerously-skip-permissions.
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
func (a AgentClaude) ShouldSkipPermissions() bool {
	if a.SkipPermissions == nil {
		return true
	}
	return *a.SkipPermissions
}

// Loop configures the iteration loop.
type Loop struct {
	MaxIterations int `toml:"max_iterations"`
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

// DirectorConfig carries the global retry budget plus per-adapter
// subtables for the Director-driven loop.
//
// The wiring follows the same shape as Agent: the top-level knobs
// (RetryBudget) live here; per-adapter knobs sit in their own subtable.
// There is no adapter selector yet because only the Claude adapter is
// wired; future adapters add a sibling subtable and the runtime branches
// on which one is non-zero.
type DirectorConfig struct {
	RetryBudget int            `toml:"retry_budget"`
	Claude      DirectorClaude `toml:"claude"`

	// MCPAudit toggles the per-session mcp-log.jsonl handler audit
	// trail. Tristate via pointer: nil means "absent in TOML, use
	// default" (default: true). Setting `mcp_audit = false` is an
	// explicit opt-out, useful for very long runs where the JSONL
	// becomes inconveniently large.
	MCPAudit *bool `toml:"mcp_audit"`
}

// IsMCPAuditEnabled returns the effective value of the MCPAudit
// tristate, applying the default (true) when absent.
func (d DirectorConfig) IsMCPAuditEnabled() bool {
	if d.MCPAudit == nil {
		return true
	}
	return *d.MCPAudit
}

// DirectorClaude configures the Director's Claude adapter (P3+).
//
// MaxBudgetUSD == 0 disables the cost cap; > 0 maps to the binary's
// --max-budget-usd flag and the call fails fail-closed if exceeded.
type DirectorClaude struct {
	Binary       string   `toml:"binary"`
	Model        string   `toml:"model"`
	ExtraArgs    []string `toml:"extra_args"`
	MaxBudgetUSD float64  `toml:"max_budget_usd"`
}
