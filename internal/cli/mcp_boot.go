package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/director/dag"
	"github.com/fgmacedo/buchecha/internal/executor/claude"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/mcp"
)

// mcpBoot is the run-wide MCP plumbing: a State (nil in legacy
// non-Director runs that have no Plan), a per-run AgentRegistry, the
// Handler that dispatches MCP method calls into both, and the live MCP
// HTTP server agents connect to. cmd/cli wires one mcpBoot per `bcc
// run` and tears it down via Close.
type mcpBoot struct {
	server   *mcp.Server
	registry *dag.AgentRegistry
	handler  *dag.Handler
	state    *dag.State
}

// startMCPBoot brings up the run-wide MCP server with a Handler bound
// to (state, registry). state is nil before the Plan is confirmed; the
// loop seeds it from the Plan once the planner returns. The advertised
// tool list is the Director method surface (bcc_plan_emit,
// bcc_task_started, ...): any agent connected to this server
// discovers them via tools/list and dispatches to handler.
func startMCPBoot(state *dag.State) (*mcpBoot, error) {
	registry := dag.NewAgentRegistry(nil)
	handler := dag.NewHandler(state, registry)
	tools, err := dag.Tools()
	if err != nil {
		return nil, fmt.Errorf("mcp boot: build director tools: %w", err)
	}
	srv, err := mcp.Start(mcp.ServerConfig{
		Tools:   tools,
		Handler: handler,
		ConnectionNames: []string{
			string(dag.RolePlanner),
			string(dag.RoleBriefer),
			string(dag.RoleExecutor),
			string(dag.RoleReviewer),
			string(dag.RoleLoop),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("mcp boot: %w", err)
	}
	return &mcpBoot{
		server:   srv,
		registry: registry,
		handler:  handler,
		state:    state,
	}, nil
}

// dagSnapshotPersister wraps a session directory in the
// dag.DAGSnapshotPersister contract: every mutation triggers an atomic
// rewrite of <sessionDir>/dag.json. The wrapper holds the path so the
// dag package never imports session-aware code.
type dagSnapshotPersister struct{ path string }

func (p *dagSnapshotPersister) WriteDAGSnapshot(s *dag.State) error {
	return dag.SaveStateFile(s, p.path)
}

// bindSession late-binds session-scoped persistence and audit to the
// run-wide handler. Called from the Director cli once the session
// directory is resolved; the legacy non-Director path skips this.
//
// markdownAdapter, when non-nil, becomes the JournalDeltaProvider used
// to answer bcc_get_journal_delta; pass the active spec format
// adapter. git, when non-nil, answers bcc_get_diff. mcpAudit toggles
// the per-session JSONL audit log.
func (b *mcpBoot) bindSession(store *director.Store, mcpAudit bool, git dag.GitDiffProvider, journal dag.JournalDeltaProvider) {
	if b == nil || b.handler == nil || store == nil {
		return
	}
	dagPath := filepath.Join(store.SessionDir(), "dag.json")
	b.handler.AttachStores(
		store,
		store,
		&dagSnapshotPersister{path: dagPath},
	)
	if mcpAudit {
		b.handler.AttachAudit(dag.NewAuditLog(filepath.Join(store.SessionDir(), "mcp-log.jsonl")))
	}
	b.handler.AttachProviders(git, journal)
}

// directorEffectiveHandler picks the run-wide handler, preferring the
// test-supplied deps.handler when set; otherwise it returns the one
// the MCP boot constructed.
func directorEffectiveHandler(deps directorDeps) *dag.Handler {
	if deps.handler != nil {
		return deps.handler
	}
	if deps.boot == nil {
		return nil
	}
	return deps.boot.handler
}

func (b *mcpBoot) Close() error {
	if b == nil || b.server == nil {
		return nil
	}
	return b.server.Close()
}

// url returns the live MCP server's URL or empty when the boot is nil
// or its server has been closed.
func (b *mcpBoot) url() string {
	if b == nil || b.server == nil {
		return ""
	}
	return b.server.URL()
}

// token returns the live MCP server's bearer token, or empty when the
// boot is nil.
func (b *mcpBoot) token() string {
	if b == nil || b.server == nil {
		return ""
	}
	return b.server.Token()
}

// registerDirectorAgent registers a fresh Director agent (Planner,
// Briefer, or Reviewer) with the run-wide registry and returns the
// assigned agent_id plus a deregister cleanup. Director agents have no
// briefing/sub-DAG scope at registration time; scope-bound roles
// (Reviewer auditing a specific Executor's briefing) are wired through
// the loop's per-iteration registration helper.
func (b *mcpBoot) registerDirectorAgent(role dag.Role) (string, func(), error) {
	id, err := b.registry.Register(role, dag.RegisterArgs{})
	if err != nil {
		return "", func() {}, fmt.Errorf("register director agent: %w", err)
	}
	cleanup := func() { b.registry.Deregister(id) }
	return string(id), cleanup, nil
}

// executorMCPConfig fills the MCP-related fields on a claude.Config so
// the executor adapter wires its per-spawn mcp-config against this run's
// MCP server, registering a fresh agent_id under the given role. The
// returned cleanup deregisters the id when the executor invocation
// completes.
func (b *mcpBoot) executorMCPConfig(role dag.Role, args dag.RegisterArgs) (claude.Config, func(), error) {
	id, err := b.registry.Register(role, args)
	if err != nil {
		return claude.Config{}, func() {}, err
	}
	cfg := claude.Config{
		MCPURL:            b.server.URL(),
		MCPToken:          b.server.Token(),
		MCPConnectionName: string(role),
		AgentID:           string(id),
	}
	cleanup := func() { b.registry.Deregister(id) }
	return cfg, cleanup, nil
}

// deregisteringExecutor wraps a loop.Executor so its agent_id is
// deregistered from the run's registry once Run returns. The Director
// loop calls deps.NewExecutor once per phase attempt; without this
// wrapper the registry would leak entries across iterations.
type deregisteringExecutor struct {
	inner   loop.Executor
	cleanup func()
}

func (d *deregisteringExecutor) Run(ctx context.Context, prompt string, events chan<- agentcontract.AgentEvent) (loop.ExecResult, error) {
	defer d.cleanup()
	return d.inner.Run(ctx, prompt, events)
}
