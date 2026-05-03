package dag

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// AgentID is the opaque per-spawn identifier bcc assigns to every
// Director or Executor invocation. The agent receives it inline in its
// prompt and passes it back on every MCP call.
type AgentID string

// Role names the cognitive role an agent plays for the run. The MCP
// connection name (X-BCC-Role header) must agree with the role the
// agent was registered under, otherwise the handler rejects the call.
type Role string

const (
	RolePlanner  Role = "bcc-planner"
	RoleBriefer  Role = "bcc-briefer"
	RoleExecutor Role = "bcc-executor"
	RoleReviewer Role = "bcc-reviewer"
	// RoleLoop is reserved for internal calls bcc itself makes against
	// the handler (force-approve in P7). Agents never see this name.
	RoleLoop Role = "bcc-loop"
)

func (r Role) valid() bool {
	switch r {
	case RolePlanner, RoleBriefer, RoleExecutor, RoleReviewer, RoleLoop:
		return true
	}
	return false
}

// AgentEntry is the registry record for one live agent. BriefingID and
// PhaseID, when non-empty, scope what the agent may query and mutate;
// SubDAG lists the task ids the agent is allowed to act on.
type AgentEntry struct {
	ID           AgentID
	Role         Role
	BriefingID   string
	PhaseID      PhaseID
	SubDAG       []TaskID
	RegisteredAt time.Time
}

// RegisterArgs carries optional scope to attach to a new agent. Empty
// fields are kept empty in the registry; the handler enforces scope
// only on methods that consult these fields.
type RegisterArgs struct {
	BriefingID string
	PhaseID    PhaseID
	SubDAG     []TaskID
}

// AgentRegistry tracks every live agent_id for the run. The registry is
// in-memory only; resume regenerates ids for the next round of spawns.
type AgentRegistry struct {
	mu      sync.Mutex
	entries map[AgentID]AgentEntry
	now     func() time.Time
}

// NewAgentRegistry returns an empty registry. nowFn is the time source
// for RegisteredAt; pass nil to use time.Now (production); tests inject
// a fixed clock for deterministic timestamps.
func NewAgentRegistry(nowFn func() time.Time) *AgentRegistry {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &AgentRegistry{
		entries: make(map[AgentID]AgentEntry),
		now:     nowFn,
	}
}

// Register allocates a fresh AgentID for the role and stores the entry.
// The id has the shape "<role>-<8-hex>" and is unique per registry by
// construction (the registry rejects accidental id collisions).
func (r *AgentRegistry) Register(role Role, args RegisterArgs) (AgentID, error) {
	if !role.valid() {
		return "", fmt.Errorf("dag: invalid role %q", string(role))
	}
	id := AgentID(string(role) + "-" + randomHex(4))
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[id]; exists {
		return "", fmt.Errorf("dag: id collision %q", string(id))
	}
	r.entries[id] = AgentEntry{
		ID:           id,
		Role:         role,
		BriefingID:   args.BriefingID,
		PhaseID:      args.PhaseID,
		SubDAG:       append([]TaskID(nil), args.SubDAG...),
		RegisteredAt: r.now(),
	}
	return id, nil
}

// Lookup returns the entry for id, or false if no agent is registered
// under that id (or it has been deregistered).
func (r *AgentRegistry) Lookup(id AgentID) (AgentEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return AgentEntry{}, false
	}
	out := e
	out.SubDAG = append([]TaskID(nil), e.SubDAG...)
	return out, true
}

// Deregister removes the entry for id. Subsequent Lookups return false;
// subsequent MCP calls carrying id are rejected. Idempotent.
func (r *AgentRegistry) Deregister(id AgentID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, id)
}

// Live returns a snapshot of every live agent entry. Order is not
// guaranteed; callers that need stability should sort the result.
func (r *AgentRegistry) Live() []AgentEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AgentEntry, 0, len(r.entries))
	for _, e := range r.entries {
		copyE := e
		copyE.SubDAG = append([]TaskID(nil), e.SubDAG...)
		out = append(out, copyE)
	}
	return out
}

func randomHex(nBytes int) string {
	if nBytes <= 0 {
		return ""
	}
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure on a desktop OS is exceptional; the
		// registry would still produce a usable but predictable id from
		// the zeroed buffer.
		_ = errors.New("dag: rand read failed")
	}
	return hex.EncodeToString(b)
}
