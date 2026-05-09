package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/director/dag"
	"github.com/fgmacedo/buchecha/internal/executor/claude"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/mcp"
)

// mcpBoot is the run-wide MCP plumbing: a State (nil in legacy
// non-Director runs that have no Plan), a per-run AgentRegistry, the
// Handler that dispatches MCP method calls into both, the MCP request
// handler agents connect to via the shared API listener, and the
// freshly minted bearer token the path-scoped auth middleware enforces.
// cmd/cli wires one mcpBoot per `bcc run`. The composition root, not
// the boot, owns the listener: after api.Server.Listen binds, the
// composition root calls setBaseURL with the loopback address so per-
// spawn executorMCPConfig hands agents a /mcp/ URL on the live port.
type mcpBoot struct {
	server   *mcp.Server
	registry *dag.AgentRegistry
	handler  *dag.Handler
	state    *dag.State
	tok      string

	mu      sync.RWMutex
	baseURL string
}

// newMCPBoot builds the run-wide MCP plumbing with a Handler bound to
// (state, registry) and a 32-byte hex bearer token. state is nil
// before the Plan is confirmed; the loop seeds it from the Plan once
// the planner returns. The advertised tool list is the Director method
// surface (bcc_plan_emit, bcc_task_started, ...): any agent connected
// through the shared listener discovers them via tools/list and
// dispatches to handler.
//
// newMCPBoot does not start a listener; the composition root mounts
// boot.server.Routes() on the api.Server and invokes setBaseURL once
// the listener is bound so MCPURL returns a usable address.
func newMCPBoot(state *dag.State) (*mcpBoot, error) {
	registry := dag.NewAgentRegistry(nil)
	handler := dag.NewHandler(state, registry)
	descs, err := dag.Tools()
	if err != nil {
		return nil, fmt.Errorf("mcp boot: build director tools: %w", err)
	}
	tools := make([]mcp.Tool, len(descs))
	for i, d := range descs {
		tools[i] = mcp.ToolFromDescriptor(d)
	}
	srv, err := mcp.New(mcp.ServerConfig{
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
	tok, err := newMCPToken()
	if err != nil {
		return nil, fmt.Errorf("mcp boot: token: %w", err)
	}
	return &mcpBoot{
		server:   srv,
		registry: registry,
		handler:  handler,
		state:    state,
		tok:      tok,
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
// to answer bcc_get_journal_delta; pass the active spec format adapter.
// head, when non-nil, answers bcc_get_baseline. mcpAudit toggles the
// per-session JSONL audit log.
func (b *mcpBoot) bindSession(store *director.Store, mcpAudit bool, head dag.HeadProvider, journal dag.JournalDeltaProvider) {
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
	b.handler.AttachProviders(head, journal)
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

// Close releases registry-owned resources. The listener is owned by
// the composition root; boot does not start one and so does not stop
// one.
func (b *mcpBoot) Close() error {
	if b == nil {
		return nil
	}
	return nil
}

// setBaseURL records the bound listener address (e.g. "127.0.0.1:54321"
// or "http://127.0.0.1:54321") so subsequent MCPURL calls produce a
// /mcp/ URL agents can connect to. The composition root invokes this
// once the api.Server listener has bound. Pass empty to clear.
func (b *mcpBoot) setBaseURL(addr string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.baseURL = addr
}

// MCPURL returns the full http://host:port/mcp/ URL agents should be
// configured against, or empty when the listener has not bound yet.
// The trailing slash matters: chi mounts the MCP handler at /mcp and
// strips the prefix; agents must hit /mcp/ to land inside the mount.
func (b *mcpBoot) MCPURL() string {
	if b == nil {
		return ""
	}
	b.mu.RLock()
	addr := b.baseURL
	b.mu.RUnlock()
	if addr == "" {
		return ""
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	addr = strings.TrimRight(addr, "/")
	return addr + "/mcp/"
}

// token returns the run-wide bearer token agents present in the
// Authorization header. The path-scoped auth middleware in
// internal/api validates it before requests reach Routes().
func (b *mcpBoot) token() string {
	if b == nil {
		return ""
	}
	return b.tok
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
		MCPURL:            b.MCPURL(),
		MCPToken:          b.token(),
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
	inner         loop.Executor
	cleanup       func()
	agentID       string
	stderrLogPath string
}

func (d *deregisteringExecutor) Run(ctx context.Context, prompt string, events chan<- agentcontract.AgentEvent) (loop.ExecResult, error) {
	defer d.cleanup()
	res, err := d.inner.Run(ctx, prompt, events)
	if d.agentID != "" {
		res.AgentID = d.agentID
	}
	if d.stderrLogPath != "" {
		res.StderrLogPath = d.stderrLogPath
	}
	return res, err
}

// newMCPToken generates a 32-byte hex bearer token. The composition
// root supplies the value to both the boot (for inclusion in per-spawn
// mcp-config) and the path-scoped auth middleware (for validation on
// every /mcp/ request).
func newMCPToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
