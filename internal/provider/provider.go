// Package provider defines the vendor-agnostic Provider port and the
// value objects that flow across it. Adapters (internal/provider/claude,
// internal/provider/codex, …) implement this interface; orchestrators
// (internal/supervision, internal/loop) consume it.
//
// The package imports only stdlib and internal/loop/agentcontract. No
// adapter-specific dependencies live here. Session persistence and
// loop-event channel forwarding flow through opaque `any` fields on
// SpawnRequest so the port stays decoupled from internal/supervision
// (and so supervision-side orchestrators can hold a SpawnRequest
// template without dragging session typing into the supervision root).
package provider

import (
	"context"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// Sandbox names the permission level the provider should enforce for a spawn.
// Each vendor maps these to its own CLI flags; adapters that have no sandbox
// concept (e.g. claude, which uses allowed-tools + skip-permissions instead)
// are free to ignore the value.
type Sandbox int

const (
	// SandboxReadOnly restricts the spawn to read-only filesystem access.
	SandboxReadOnly Sandbox = iota
	// SandboxWorkspaceWrite allows the spawn to write inside the project
	// working directory.
	SandboxWorkspaceWrite
	// SandboxDangerFullAccess places no sandbox restriction on the spawn.
	SandboxDangerFullAccess
)

// MCPSpec carries the per-spawn MCP connection parameters the adapter
// writes into a provider-specific config file before starting the
// subprocess.
type MCPSpec struct {
	// URL is the http://127.0.0.1:port/mcp/ endpoint of the run-wide MCP
	// handler. The trailing slash is significant for chi's prefix strip.
	// Empty disables MCP wiring for the spawn.
	URL string
	// Token is the bearer token the agent presents in Authorization on
	// every MCP request. Required when URL is set.
	Token string
	// ConnectionName is the role label carried in the X-BCC-Role header so
	// the handler's per-method allow-list can authorise each call.
	ConnectionName string
}

// SpawnRequest is the input to Provider.Spawn. All fields are optional
// unless the individual adapter documents a requirement; adapters fail
// fast on missing required fields.
type SpawnRequest struct {
	// Role is the cognitive role for this spawn (e.g. "bcc-executor").
	// Used when constructing system prompts and SpawnStarted events.
	Role string
	// Prompt is the user-turn content delivered to the agent.
	Prompt string
	// SystemPrompt, when non-empty, is delivered as the system message.
	// Adapters materialise it as a file or pass it directly depending on
	// their CLI capabilities.
	SystemPrompt string
	// Model is passed to the agent CLI via its model flag. Empty lets the
	// adapter use its built-in default.
	Model string
	// Effort maps to the agent's effort/reasoning-level flag. Empty omits
	// the flag.
	Effort string
	// Sandbox names the permission level the adapter should request.
	Sandbox Sandbox
	// AllowedTools is the comma-separated or slice-form list of tools the
	// agent is permitted to call. Empty means no restriction is expressed
	// (the adapter's default applies).
	AllowedTools []string
	// SkipPermissions, when true, instructs the adapter to pass its
	// "skip all permission prompts" flag so the agent runs autonomously.
	SkipPermissions bool
	// MaxBudgetUSD, when > 0, is passed as a per-call cost cap. Adapters
	// that support it pass it to the CLI; the caller may also enforce it
	// post-spawn against SpawnResult.CostUSD.
	MaxBudgetUSD float64
	// ExtraArgs are appended verbatim to the adapter's command line after
	// all other flags.
	ExtraArgs []string
	// MCP carries the run-wide MCP endpoint the adapter should wire the
	// agent to for each spawn.
	MCP MCPSpec
	// AgentID is the per-spawn registry id assigned before the spawn.
	// Adapters embed it in system prompts so the agent passes it back on
	// every MCP call.
	AgentID string
	// PhaseID is the phase the spawn belongs to. Empty for spawns that
	// run outside any phase (e.g. the Planner).
	PhaseID string
	// IterationID is the briefing iteration id for in-phase spawns.
	// Empty for the Planner.
	IterationID string
	// Attempt is the 1-based retry counter within the phase iteration.
	// Zero for the Planner.
	Attempt int
	// SessionStore, when non-nil, is used to resolve the spawns directory
	// for per-spawn prompt persistence and SpawnStarted emission. When nil
	// prompt persistence is skipped. The concrete type at runtime is
	// *internal/supervision/session.Store; typed as any here to keep this
	// package free of an import of internal/supervision. Adapters use the
	// spawnkit helpers that perform the type assertion internally.
	SessionStore any
	// Events, when non-nil, receives the stream-json parsed AgentEvents
	// produced by the adapter as the subprocess runs. The adapter never
	// closes this channel; the caller owns it.
	Events chan<- agentcontract.AgentEvent
	// LoopEvents, when non-nil, receives loop-level lifecycle events
	// (SpawnStarted, SpawnFinished). The concrete type at runtime is
	// chan<- loop.Event; typed as any here to keep this package free of an
	// import of internal/loop. Adapters use spawnkit.EmitSpawnStarted /
	// spawnkit.EmitSpawnFinished which perform the type assertion
	// internally.
	LoopEvents any
}

// SpawnResult is the output of Provider.Spawn. CostUSD and Tokens are
// best-effort: adapters that cannot reliably extract them leave them zero.
type SpawnResult struct {
	// SpawnID is the per-spawn identifier generated by the adapter and
	// used as the basename (without .md) of the prompt file under
	// <sessionDir>/spawns/. Empty when prompt persistence was skipped.
	SpawnID string
	// ExitCode is the agent subprocess exit code. 0 on clean completion.
	ExitCode int
	// StderrTail is the last few KiB of the subprocess stderr captured by
	// the adapter. Useful for surfacing a human-readable error reason on
	// non-zero exits.
	StderrTail string
	// DurationMS is the wall-clock duration of the spawn in milliseconds,
	// measured from cmd.Start to cmd.Wait.
	DurationMS int64
	// CostUSD is the provider-reported dollar cost for the spawn. Zero
	// when the adapter could not extract it from the output stream.
	CostUSD float64
	// Tokens holds the per-bucket token usage reported by the agent.
	// Zero-valued buckets are left as zero when unavailable.
	Tokens agentcontract.TokenUsage
}

// Provider is the vendor-agnostic interface for spawning an agent
// subprocess. Each vendor (claude, codex, …) provides exactly one
// implementation. The interface is intentionally small: a single Spawn
// method covers planning, briefing, executing, and reviewing because the
// differences between those roles are expressed through SpawnRequest
// fields, not through distinct method signatures.
type Provider interface {
	// Name returns the canonical lower-case provider identifier (e.g.
	// "claude", "codex"). It must match the key used in .bcc.toml
	// [providers.<name>] and in RoleAssignment.Provider.
	Name() string
	// Spawn starts the agent subprocess with the given request, streams
	// its output, and returns once the subprocess exits. Context
	// cancellation must propagate to the subprocess with a graceful
	// interrupt before escalating to a forced kill.
	Spawn(ctx context.Context, req SpawnRequest) (SpawnResult, error)
}
