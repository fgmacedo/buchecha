// Package config defines the typed Config that maps to .bcc.toml.
//
// This package is stdlib-only: no TOML parser, no env libraries. Loading a
// Config from disk is the responsibility of internal/configloader/<format>.
// Env merging (precedence rules and process-environment application) lives
// here because it operates on an already-parsed Config and does not need
// the format-specific decoder.
package config

// Config mirrors the shape of .bcc.toml. The hierarchical layout splits
// the two orthogonal dimensions of the run:
//
//   - [providers.<name>] is a vendor adapter: how to invoke the CLI of one
//     LLM provider (binary, flags, auth, budget cap). One section per
//     provider; each section is shared across every role that uses it.
//   - [roles.<name>] is a per-role menu of options the Planner picks from.
//     Roles are planner / briefer / executor / reviewer.
//
// Defaults filled by ApplyDefaults cover the common case so an empty
// .bcc.toml still produces a working configuration. Per-provider entries
// for known vendors (today: claude) are auto-populated even when absent
// from the TOML; user declarations only override.
type Config struct {
	Project   Project             `toml:"project"`
	Journal   Journal             `toml:"journal"`
	Providers map[string]Provider `toml:"providers"`
	Roles     Roles               `toml:"roles"`
	Loop      Loop                `toml:"loop"`
	Git       Git                 `toml:"git"`
	Env       Env                 `toml:"env"`
	Debug     DebugConfig         `toml:"debug"`
	Webui     Webui               `toml:"webui"`
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

// Provider configures one LLM CLI vendor adapter (claude, codex,
// gemini). The same Provider entry is shared by every role that picks
// this vendor in its options list, so binary, extra args, permission
// model, and budget cap live here once instead of being duplicated per
// role.
type Provider struct {
	// Binary is the path or PATH name of the vendor's CLI.
	Binary string `toml:"binary"`

	// ExtraArgs are appended verbatim to every spawn this provider
	// runs, regardless of role. Useful for vendor-wide flags
	// (--strict-mcp-config, token-saving knobs).
	ExtraArgs []string `toml:"extra_args"`

	// SkipPermissions, when true (the default), instructs the adapter
	// to suppress the agent's interactive permission prompts so the
	// loop can run end to end without human intervention. Vendor
	// adapters map this to their own flag (claude:
	// --dangerously-skip-permissions).
	//
	// Tristate via pointer: nil means "absent in TOML, use default";
	// the default is true. Setting `skip_permissions = false` is an
	// explicit opt-out.
	SkipPermissions *bool `toml:"skip_permissions"`

	// MaxBudgetUSD, when > 0, caps the cost of each Director-role
	// spawn this provider runs (mapped to the binary's
	// --max-budget-usd flag and enforced bcc-side as a fail-closed
	// check). Zero (the default) disables both behaviors.
	MaxBudgetUSD float64 `toml:"max_budget_usd"`
}

// ShouldSkipPermissions returns the effective value of the
// SkipPermissions tristate, applying the default (true) when absent.
func (p Provider) ShouldSkipPermissions() bool {
	if p.SkipPermissions == nil {
		return true
	}
	return *p.SkipPermissions
}

// Roles carries the per-role option menu. Each role is independent; the
// Planner picks from each role's filtered menu when it emits a Plan
// (briefer/executor/reviewer) or is itself driven by options[0] (planner).
type Roles struct {
	Planner  RolePolicy `toml:"planner"`
	Briefer  RolePolicy `toml:"briefer"`
	Executor RolePolicy `toml:"executor"`
	Reviewer RolePolicy `toml:"reviewer"`
}

// RolePolicy is a role's ordered list of options. Order is the user's
// preference: best to cheapest. The runtime filters out options whose
// provider is not available (binary not in PATH); for the Planner the
// first surviving entry runs, for other roles the surviving entries
// form the menu the Planner picks from per phase.
type RolePolicy struct {
	Options []RoleOption `toml:"options"`
}

// RoleOption is one row in a role's menu: a (provider, model, efforts)
// triple. efforts lists the effort levels the Planner is allowed to
// pair with this model on this role; the Planner picks one effort per
// phase from this list. note is free-form user guidance rendered in the
// Planner prompt (e.g. "only for schema migrations on this project").
type RoleOption struct {
	Provider string   `toml:"provider"`
	Model    string   `toml:"model"`
	Efforts  []string `toml:"efforts"`
	Note     string   `toml:"note,omitempty"`
}

// Loop configures the iteration loop.
type Loop struct {
	// MaxIterations bounds the per-phase iteration count.
	MaxIterations int `toml:"max_iterations"`

	// RetryBudget is the floor the Director honors when no per-task
	// override is set on the Plan. The Planner may raise it per task
	// for work it expects to be brittle.
	RetryBudget int `toml:"retry_budget"`
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

// DebugConfig toggles diagnostic captures. Off by default to keep the
// happy-path session footprint small. The CLI may override individual
// fields via flags; see internal/cli/run.go.
type DebugConfig struct {
	// MCPAudit toggles the per-session mcp-log.jsonl handler audit
	// trail. Tristate via pointer: nil means "absent in TOML, use
	// default" (default: true). Setting `mcp_audit = false` is an
	// explicit opt-out, useful for very long runs where the JSONL
	// becomes inconveniently large.
	MCPAudit *bool `toml:"mcp_audit"`

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

// IsMCPAuditEnabled returns the effective value of the MCPAudit
// tristate, applying the default (true) when absent.
func (d DebugConfig) IsMCPAuditEnabled() bool {
	if d.MCPAudit == nil {
		return true
	}
	return *d.MCPAudit
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
