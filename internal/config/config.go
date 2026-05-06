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
	Debug    DebugConfig    `toml:"debug"`
	Webui    Webui          `toml:"webui"`
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
	Binary string `toml:"binary"`
	Model  string `toml:"model"`
	// Effort is the default --effort level claude uses when the
	// Planner does not attribute one on the Phase via
	// executor_assignment. Empty omits the flag. Allowed values depend
	// on the model (low|medium|high|xhigh|max); the loop validates
	// per-phase overrides against the capability registry, so an
	// invalid default here is only caught at spawn time by the CLI.
	Effort    string   `toml:"effort"`
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

// DebugConfig toggles diagnostic captures. Off by default to keep the
// happy-path session footprint small. The CLI may override individual
// fields via flags; see internal/cli/run.go.
type DebugConfig struct {
	// CaptureSubprocessLogs persists the full stderr of every Director
	// role spawn (planner, briefer, executor, reviewer) under
	// .bcc/sessions/<id>/runs/. Tristate via pointer; default false.
	// When set to true, the adapter teams the subprocess stderr to a
	// per-spawn file in addition to the existing in-memory tail.
	CaptureSubprocessLogs *bool `toml:"capture_subprocess_logs"`

	// CaptureSubprocessStdout persists the raw stream-json stdout of
	// every spawn alongside the stderr file. Tristate via pointer;
	// default false. Heavier than CaptureSubprocessLogs since stream-json
	// can be large; opt-in independently for cases where the user wants
	// to reconstruct the model narrative offline.
	CaptureSubprocessStdout *bool `toml:"capture_subprocess_stdout"`

	// PersistEventsLog persists the canonical SeqEvent stream to
	// .bcc/sessions/<id>/events.ndjson, the same format the
	// EventService.Replay reads back. The file is the source of truth
	// for replay-driven dev modes (bcc dev) and for SSE clients that
	// reconnect after the live ring buffer rolled over. Tristate via
	// pointer; default true. Set to false (CLI: --no-events-log) when
	// disk pressure outweighs the diagnostic value, e.g. on long
	// runs.
	PersistEventsLog *bool `toml:"persist_events_log"`
}

// IsCaptureSubprocessLogsEnabled returns the effective value of the
// CaptureSubprocessLogs tristate, applying the default (false) when
// absent.
func (d DebugConfig) IsCaptureSubprocessLogsEnabled() bool {
	if d.CaptureSubprocessLogs == nil {
		return false
	}
	return *d.CaptureSubprocessLogs
}

// IsCaptureSubprocessStdoutEnabled returns the effective value of the
// CaptureSubprocessStdout tristate, applying the default (false) when
// absent.
func (d DebugConfig) IsCaptureSubprocessStdoutEnabled() bool {
	if d.CaptureSubprocessStdout == nil {
		return false
	}
	return *d.CaptureSubprocessStdout
}

// IsPersistEventsLogEnabled returns the effective value of the
// PersistEventsLog tristate, applying the default (true) when absent.
func (d DebugConfig) IsPersistEventsLogEnabled() bool {
	if d.PersistEventsLog == nil {
		return true
	}
	return *d.PersistEventsLog
}

// Webui carries the embedded web dashboard surface toggles, mapped to
// the [webui] TOML block. Both fields default to false; CLI flags
// (--webui / --webui-open) take precedence when explicitly set in
// internal/cli/run.go.
//
// The block intentionally has no bind field: the API and WebUI share
// the run-wide listener owned by internal/api, and bind belongs to a
// future [api] block. Adding it here would split listener configuration
// across two siblings.
type Webui struct {
	// Enabled mirrors --webui: when true, the composition root builds
	// a webui.New() handler and mounts it at / on the api listener.
	Enabled bool `toml:"enabled"`
	// Open mirrors --webui-open: when true, the run boot launches the
	// platform default browser at the dashboard URL after the listener
	// is up. Implies Enabled (the CLI promotes runWebUI when -W is set).
	Open bool `toml:"open"`
}

// DirectorClaude configures the Director's Claude adapter (P3+).
//
// MaxBudgetUSD == 0 disables the cost cap; > 0 maps to the binary's
// --max-budget-usd flag and the call fails fail-closed if exceeded.
type DirectorClaude struct {
	Binary string `toml:"binary"`
	Model  string `toml:"model"`
	// Effort is the default --effort level for Director roles
	// (Planner, Briefer, Reviewer) when the Planner did not attribute
	// one on the Phase via briefer_assignment / reviewer_assignment.
	// Empty omits the flag. Same allowed values as AgentClaude.Effort.
	Effort       string   `toml:"effort"`
	ExtraArgs    []string `toml:"extra_args"`
	MaxBudgetUSD float64  `toml:"max_budget_usd"`
}
