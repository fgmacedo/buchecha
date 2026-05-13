package supervision

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/provider"
)

// ErrBudgetExceeded is returned by DirectorRoles when a spawn reports a
// per-call cost above DirectorConfig.MaxBudgetUSD. The loop treats it as
// a fail-closed signal: the affected role escalates rather than the run
// silently overspending.
var ErrBudgetExceeded = errors.New("supervision: per-call budget exceeded")

// ErrMissingAgentID is returned when a Director call arrives without an
// AgentID populated on the input. Callers register the agent with the
// run-wide registry before invoking the orchestrator and pass the
// assigned id back in.
var ErrMissingAgentID = errors.New("supervision: missing agent_id on input")

// DirectorConfig configures the DirectorRoles orchestrator. Run-wide
// knobs only: per-call values (provider, model, effort, prompt) come
// from each PlannerInput/BrieferInput/ReviewerInput.
type DirectorConfig struct {
	// MaxBudgetUSD, when > 0, caps the per-call USD cost. The
	// orchestrator returns ErrBudgetExceeded when a SpawnResult reports
	// CostUSD above the cap. Zero disables the check.
	MaxBudgetUSD float64

	// AllowedTools is the toolbox every Director role spawn is restricted
	// to. Empty falls back to the default ["Read","Bash","Grep","Glob"].
	AllowedTools []string

	// ExtraArgs are appended verbatim to every Director spawn's argv via
	// SpawnRequest.ExtraArgs. Empty leaves the provider's default.
	ExtraArgs []string
}

// defaultAllowedTools is the read-only toolbox every Director role
// spawn receives when DirectorConfig.AllowedTools is empty. Director
// roles never write to the working tree; they inspect it.
var defaultAllowedTools = []string{"Read", "Bash", "Grep", "Glob"}

// MCPProvider supplies the per-role MCP wiring (URL, bearer token,
// connection name) the orchestrator threads into each Director spawn so
// the agent subprocess can reach the run-wide MCP handler and call
// plan_emit / briefing_emit / task_approved / task_needs_fix. Returning
// a zero MCPSpec (empty URL) disables MCP wiring for that role; the
// closure is invoked per spawn so the cli can defer URL resolution until
// the listener has bound.
type MCPProvider func(role agentcontract.Role) provider.MCPSpec

// DirectorRoles is the vendor-agnostic orchestrator that implements
// Planner, Briefer, and Reviewer above the provider line. Each method
// composes the per-role prompt, looks up the assignment's provider in
// the registry, and runs a read-only spawn through provider.Provider.
//
// The orchestrator is built once per run and reused across every role
// call. SessionStore, LoopEvents, and the MCP provider are wired in
// after session resolution via SetSessionStore / SetLoopEvents /
// SetMCPProvider; all three are optional (tests typically leave them
// nil), but without an MCPProvider the Director-role subprocesses cannot
// reach the run-wide MCP handler and will fail to emit a plan / briefing
// / verdict.
type DirectorRoles struct {
	registry *provider.Registry
	cfg      DirectorConfig

	// sessionStore and loopEvents are opaque so the supervision root
	// stays free of internal/supervision/session and internal/loop. The
	// concrete types at runtime are *session.Store and chan<- loop.Event;
	// provider adapters type-assert them internally.
	sessionStore any
	loopEvents   any

	// mcp, when non-nil, is called per spawn to resolve the role's MCP
	// wiring. The result lands on SpawnRequest.MCP; the provider adapter
	// materialises it into whatever per-spawn shape the vendor CLI
	// requires.
	mcp MCPProvider
}

// Compile-time checks that *DirectorRoles satisfies the three Director
// ports. They keep the wire contract honest at build time so a
// signature drift on Planner/Briefer/Reviewer fails compilation here
// before it can fail at runtime in the cli.
var (
	_ Planner  = (*DirectorRoles)(nil)
	_ Briefer  = (*DirectorRoles)(nil)
	_ Reviewer = (*DirectorRoles)(nil)
)

// NewDirectorRoles builds a DirectorRoles bound to the given provider
// registry. cfg.AllowedTools defaults to defaultAllowedTools when empty.
// registry may be nil for tests that only call composition helpers, but
// is required before any role method spawns.
func NewDirectorRoles(registry *provider.Registry, cfg DirectorConfig) *DirectorRoles {
	if len(cfg.AllowedTools) == 0 {
		cfg.AllowedTools = append([]string(nil), defaultAllowedTools...)
	}
	return &DirectorRoles{registry: registry, cfg: cfg}
}

// SetSessionStore installs the per-session store the orchestrator
// passes through SpawnRequest.SessionStore so the provider can persist
// each spawn's prompt under <sessionDir>/spawns/. The value is opaque
// (any) to keep the supervision package free of an import of
// internal/supervision/session; the concrete type at runtime is
// *session.Store. Calling with nil disables prompt persistence.
func (d *DirectorRoles) SetSessionStore(store any) {
	if d == nil {
		return
	}
	d.sessionStore = store
}

// SetLoopEvents installs the loop-level events channel the orchestrator
// forwards through SpawnRequest.LoopEvents so providers can emit
// SpawnStarted / SpawnFinished. The value is opaque (any) here to keep
// the supervision package independent of internal/loop; provider
// adapters type-assert to chan<- loop.Event internally.
func (d *DirectorRoles) SetLoopEvents(events any) {
	if d == nil {
		return
	}
	d.loopEvents = events
}

// SetMCPProvider installs the per-role MCP wiring resolver. The cli
// calls this after the run listener binds (the MCP URL is only knowable
// then). Passing nil disables MCP wiring; the Director-role subprocesses
// then run with no MCP endpoint and cannot call plan_emit /
// briefing_emit / task_approved / task_needs_fix.
func (d *DirectorRoles) SetMCPProvider(fn MCPProvider) {
	if d == nil {
		return
	}
	d.mcp = fn
}

// Plan implements Planner. It renders the planner prompt and runs a
// read-only spawn under the assignment's provider. The Plan itself
// flows back through MCP (plan_emit); the returned *Plan is always nil
// by design and the caller reads the authoritative Plan from the
// handler.
func (d *DirectorRoles) Plan(ctx context.Context, in PlannerInput, events chan<- agentcontract.AgentEvent) (*Plan, *SpawnStats, error) {
	if in.AgentID == "" {
		return nil, nil, ErrMissingAgentID
	}
	prompt, err := composePrompt(PlanPromptTemplate(), planView{
		Role:     "planner",
		AgentID:  in.AgentID,
		SpecPath: in.SpecPath,
		Registry: in.Registry,
		Menus:    renderMenus(in.Menus),
		Prompt:   in.Prompt,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("supervision: compose plan prompt: %w", err)
	}
	provName, model, effort, err := resolveAssignment("plan", &in.Assignment)
	if err != nil {
		return nil, nil, err
	}
	stats, err := d.spawn(ctx, spawnRequest{
		role:        agentcontract.RolePlanner,
		agentID:     in.AgentID,
		iterationID: "",
		phaseID:     "",
		attempt:     0,
		prompt:      prompt,
		provider:    provName,
		model:       model,
		effort:      effort,
	}, events)
	return nil, stats, err
}

// Brief implements Briefer. The Briefing lands on the handler via
// briefing_emit; the orchestrator only reports cost/timing stats.
func (d *DirectorRoles) Brief(ctx context.Context, in BrieferInput, events chan<- agentcontract.AgentEvent) (*SpawnStats, error) {
	if in.AgentID == "" {
		return nil, ErrMissingAgentID
	}
	prompt, err := composePrompt(BriefPromptTemplate(), briefView{
		Role:        "briefer",
		AgentID:     in.AgentID,
		SpecPath:    in.SpecPath,
		IterationID: in.IterationID,
		PhaseID:     in.PhaseID,
	})
	if err != nil {
		return nil, fmt.Errorf("supervision: compose brief prompt: %w", err)
	}
	provName, model, effort, err := resolveAssignment("brief", in.Assignment)
	if err != nil {
		return nil, err
	}
	return d.spawn(ctx, spawnRequest{
		role:        agentcontract.RoleBriefer,
		agentID:     in.AgentID,
		iterationID: in.IterationID,
		phaseID:     in.PhaseID,
		attempt:     in.Attempt,
		prompt:      prompt,
		provider:    provName,
		model:       model,
		effort:      effort,
	}, events)
}

// Review implements Reviewer. Per-task outcomes flow through
// task_approved / task_needs_fix; the orchestrator only reports stats.
func (d *DirectorRoles) Review(ctx context.Context, in ReviewerInput, events chan<- agentcontract.AgentEvent) (*SpawnStats, error) {
	if in.AgentID == "" {
		return nil, ErrMissingAgentID
	}
	prompt, err := composePrompt(ReviewPromptTemplate(), reviewView{
		Role:    "reviewer",
		AgentID: in.AgentID,
	})
	if err != nil {
		return nil, fmt.Errorf("supervision: compose review prompt: %w", err)
	}
	provName, model, effort, err := resolveAssignment("review", in.Assignment)
	if err != nil {
		return nil, err
	}
	return d.spawn(ctx, spawnRequest{
		role:        agentcontract.RoleReviewer,
		agentID:     in.AgentID,
		iterationID: in.IterationID,
		phaseID:     in.PhaseID,
		attempt:     in.Attempt,
		prompt:      prompt,
		provider:    provName,
		model:       model,
		effort:      effort,
	}, events)
}

// spawnRequest groups the per-call values shared by Plan/Brief/Review
// so the spawn implementation works against a single struct instead of
// a long parameter list.
type spawnRequest struct {
	role        agentcontract.Role
	agentID     string
	iterationID string
	phaseID     string
	attempt     int
	prompt      string
	provider    string
	model       string
	effort      string
}

// spawn looks up the provider in the registry and runs a single
// read-only spawn under the supplied request. Cost above
// MaxBudgetUSD escalates to ErrBudgetExceeded.
func (d *DirectorRoles) spawn(ctx context.Context, r spawnRequest, events chan<- agentcontract.AgentEvent) (*SpawnStats, error) {
	if d.registry == nil {
		return nil, fmt.Errorf("supervision: %s spawn: nil provider registry", r.role)
	}
	p, ok := d.registry.Get(r.provider)
	if !ok {
		return nil, fmt.Errorf("supervision: %s spawn: unknown provider %q", r.role, r.provider)
	}

	var mcp provider.MCPSpec
	if d.mcp != nil {
		mcp = d.mcp(r.role)
	}

	req := provider.SpawnRequest{
		Role:            string(r.role),
		Prompt:          r.prompt,
		Model:           r.model,
		Effort:          r.effort,
		Sandbox:         provider.SandboxReadOnly,
		AllowedTools:    append([]string(nil), d.cfg.AllowedTools...),
		SkipPermissions: true,
		ExtraArgs:       append([]string(nil), d.cfg.ExtraArgs...),
		MCP:             mcp,
		AgentID:         r.agentID,
		PhaseID:         r.phaseID,
		IterationID:     r.iterationID,
		Attempt:         r.attempt,
		SessionStore:    d.sessionStore,
		Events:          events,
		LoopEvents:      d.loopEvents,
	}

	res, runErr := p.Spawn(ctx, req)
	stats := &SpawnStats{
		DurationMS: res.DurationMS,
		CostUSD:    res.CostUSD,
		Tokens:     res.Tokens,
	}

	if d.cfg.MaxBudgetUSD > 0 && res.CostUSD > d.cfg.MaxBudgetUSD {
		return stats, fmt.Errorf("%w: cost=%.4f cap=%.4f", ErrBudgetExceeded, res.CostUSD, d.cfg.MaxBudgetUSD)
	}
	if runErr != nil {
		return stats, fmt.Errorf("supervision: %s spawn via %s: %w", r.role, r.provider, runErr)
	}
	return stats, nil
}

// resolveAssignment unpacks the RoleAssignment for a call. Returns an
// error when the assignment is nil or missing provider/model; the loop
// guarantees a populated assignment via FillPlanFromMenus, so a missing
// one is a programmer error.
//
// The Planner's own assignment is passed by value (PlannerInput.Assignment);
// Briefer/Reviewer pass *RoleAssignment. The helper accepts a pointer to
// share one impl; callers pass &in.Assignment for the value form.
func resolveAssignment(call string, a *RoleAssignment) (string, string, string, error) {
	if a == nil {
		return "", "", "", fmt.Errorf("supervision: %s requires assignment", call)
	}
	if a.Provider == "" || a.Model == "" {
		return "", "", "", fmt.Errorf("supervision: %s assignment missing provider or model", call)
	}
	return a.Provider, a.Model, a.Effort, nil
}

// composePrompt expands a role's prompt template with the agentcontract
// partials and the per-role view data. Pure text; no I/O.
func composePrompt(promptTpl string, view any) (string, error) {
	t := agentcontract.Partials()
	if _, err := t.New("role").Parse(promptTpl); err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "role", view); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

// planView, briefView, and reviewView are the per-role data the prompt
// templates render against. They are kept narrow so a future template
// edit cannot accidentally surface fields the role should not see.
type planView struct {
	Role     string
	AgentID  string
	SpecPath string
	Registry CapabilityRegistry
	Menus    menusView
	Prompt   string
}

// menusView and roleMenuRow describe the per-role cardápio rendered
// into the Planner prompt: only the roles whose assignments the Planner
// is expected to attribute (briefer, executor, reviewer). The Planner's
// own role is omitted because it cannot reroute itself.
type menusView struct {
	Briefer  []roleMenuRow
	Executor []roleMenuRow
	Reviewer []roleMenuRow
}

type roleMenuRow struct {
	Provider string
	Model    string
	Efforts  []string
	Note     string
	Tier     string
	Summary  string
}

type briefView struct {
	Role        string
	AgentID     string
	SpecPath    string
	IterationID string
	PhaseID     string
}

type reviewView struct {
	Role    string
	AgentID string
}

// renderMenus converts the loop-side RoleMenus into the per-role view
// the Planner prompt template iterates over. Producer-side rendering
// keeps the template syntax small and avoids leaking config types into
// the prompt context.
func renderMenus(menus RoleMenus) menusView {
	return menusView{
		Briefer:  renderRoleMenu(menus.Briefer),
		Executor: renderRoleMenu(menus.Executor),
		Reviewer: renderRoleMenu(menus.Reviewer),
	}
}

func renderRoleMenu(menu RoleMenu) []roleMenuRow {
	rows := make([]roleMenuRow, 0, len(menu.Options))
	for _, opt := range menu.Options {
		rows = append(rows, roleMenuRow{
			Provider: opt.Provider,
			Model:    opt.Model,
			Efforts:  append([]string(nil), opt.Efforts...),
			Note:     opt.Note,
			Tier:     opt.Tier,
			Summary:  opt.Summary,
		})
	}
	return rows
}

// EffortsString joins efforts with ", " for prompt rendering. Returns
// "n/a" when the row exposes no effort knob so the Planner table reads
// cleanly.
func (r roleMenuRow) EffortsString() string {
	if len(r.Efforts) == 0 {
		return "n/a"
	}
	return strings.Join(r.Efforts, ", ")
}
