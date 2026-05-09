package dag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/fgmacedo/buchecha/internal/supervision"
)

// MCP method names. Each one has a JSON Schema embedded under
// internal/supervision/schemas/mcp/ that the handler validates inputs
// against, plus a per-method handler function attached at construction
// time.
const (
	MethodPlanEmit          = "bcc_plan_emit"
	MethodPlanSkip          = "bcc_plan_skip"
	MethodBriefingEmit      = "bcc_briefing_emit"
	MethodGetDAGSnapshot    = "bcc_get_dag_snapshot"
	MethodGetBriefing       = "bcc_get_briefing"
	MethodGetPendingTasks   = "bcc_get_pending_tasks"
	MethodTaskStarted       = "bcc_task_started"
	MethodTaskCompleted     = "bcc_task_completed"
	MethodIterationFinished = "bcc_iteration_finished"
	MethodGetBaseline       = "bcc_get_baseline"
	MethodGetJournalDelta   = "bcc_get_journal_delta"
	MethodTaskApproved      = "bcc_task_approved"
	MethodTaskNeedsFix      = "bcc_task_needs_fix"
	MethodReviewFinished    = "bcc_review_finished"
)

// PlanningTaskID is the well-known task id the Planner uses on the
// timeline pair (bcc_task_started, bcc_task_completed) so planning
// shows up alongside work tasks in the TUI without being part of the
// DAG. The handler treats this id as out-of-DAG: the calls succeed but
// no DAG mutation happens.
const PlanningTaskID = "planning"

// BriefingTaskID is the well-known task id the Briefer uses on the
// timeline pair (bcc_task_started, bcc_task_completed) so briefing
// shows up alongside work tasks. Same out-of-DAG semantics as
// PlanningTaskID: the calls are bookkeeping, not state mutation.
const BriefingTaskID = "briefing"

// ReviewingTaskID is the well-known task id the Reviewer uses on the
// timeline pair so reviewing shows up alongside work tasks. Same
// out-of-DAG semantics as PlanningTaskID and BriefingTaskID.
const ReviewingTaskID = "reviewing"

// PseudoTaskIDs is the set of well-known task IDs that are role
// bookkeeping rather than real DAG tasks. Consumers (TUI progress
// counters, exporters) should treat these as informational and not
// fold them into per-task work metrics.
var PseudoTaskIDs = map[string]struct{}{
	PlanningTaskID:  {},
	BriefingTaskID:  {},
	ReviewingTaskID: {},
}

// IsPseudoTaskID reports whether id is one of the well-known role
// bookkeeping task ids (planning, briefing). True means the id is
// out-of-DAG and progress consumers should not count it.
func IsPseudoTaskID(id string) bool {
	_, ok := PseudoTaskIDs[id]
	return ok
}

// methodSpec describes one entry in the dispatch table: which roles may
// call the method and the function that runs after agent identity and
// connection-name checks pass.
type methodSpec struct {
	allowedRoles map[Role]bool
	handle       func(*Handler, context.Context, AgentEntry, map[string]any) (string, error)
}

// HandlerObserver receives a notification after every successful
// dispatch through HandleCall, post-schema and post-scope checks. The
// handler invokes OnCall with no mutex held; observers must not block
// (HandleCall runs on MCP HTTP goroutines) and must not call back into
// the handler.
type HandlerObserver interface {
	OnCall(method string, agentID string, role Role, input map[string]any)
}

// briefingState holds per-briefing handler-side context: the Briefing
// itself (set by bcc_briefing_emit), plus the inputs needed to answer
// bcc_get_journal_delta. The loop populates the journal snapshots via
// the SetBriefingJournalSnapshots setter before the Reviewer is
// registered against the briefing.
type briefingState struct {
	briefing      *supervision.Briefing
	journalBefore []byte
	journalAfter  []byte
	reviewOutcome string
	reviewReason  string
	// iterSignal is the signal the Executor reported via
	// bcc_iteration_finished. Empty until the Executor exits cleanly;
	// the loop driver reads it once the Executor.Run returns to decide
	// whether to advance, retry, or terminate.
	iterSignal string
	// agents references the live agents bound to this briefing so the
	// handler can answer queries scoped per-agent. Today bcc spawns one
	// Executor and one Reviewer per briefing; the slice future-proofs
	// the parallel-shard case PRD 3 introduces.
	agents []AgentID
}

// Handler implements mcp.Handler against the DAG state and the agent
// registry. The dispatch table maps method names to per-method handler
// functions; each function performs schema validation, scope checks,
// and either mutates state or returns a query response.
type Handler struct {
	state    *State
	registry *AgentRegistry
	dispatch map[string]methodSpec
	schemas  map[string]*jsonschema.Schema

	head     HeadProvider
	journal  JournalDeltaProvider
	audit    *AuditLog
	observer HandlerObserver

	planStore     PlanPersister
	briefingStore BriefingPersister
	dagStore      DAGSnapshotPersister

	capabilityRegistry *supervision.CapabilityRegistry
	roleMenus          supervision.RoleMenus

	mu             sync.Mutex
	plan           *supervision.Plan
	planSkipped    bool
	planSkipReason string
	briefings      map[string]*briefingState
	phaseBaselines map[string]string
	now            func() time.Time
}

// HandlerOptions parameterizes NewHandler. Every field is optional;
// nil values disable the corresponding feature (no audit log, no
// persistence, no journal delta) so tests can construct a handler
// with only the inputs they need.
type HandlerOptions struct {
	Head               HeadProvider
	Journal            JournalDeltaProvider
	Audit              *AuditLog
	PlanStore          PlanPersister
	BriefingStore      BriefingPersister
	DAGSnapshotStore   DAGSnapshotPersister
	CapabilityRegistry *supervision.CapabilityRegistry
	RoleMenus          supervision.RoleMenus
	Now                func() time.Time
}

// NewHandler wires a Handler against a State and an AgentRegistry. The
// dispatch table and the per-method JSON Schemas are compiled once at
// construction; a malformed schema fails loudly here rather than on the
// first agent call.
func NewHandler(state *State, registry *AgentRegistry) *Handler {
	return NewHandlerWithOptions(state, registry, HandlerOptions{})
}

// NewHandlerWithOptions is the full constructor; NewHandler is the
// convenience wrapper for tests and the legacy non-Director run path.
func NewHandlerWithOptions(state *State, registry *AgentRegistry, opts HandlerOptions) *Handler {
	schemas, err := compileMethodSchemas()
	if err != nil {
		// A schema compile failure is a programming error: the schemas
		// are embedded at build time and validated by tests.
		panic(err)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	h := &Handler{
		state:              state,
		registry:           registry,
		schemas:            schemas,
		head:               opts.Head,
		journal:            opts.Journal,
		audit:              opts.Audit,
		planStore:          opts.PlanStore,
		briefingStore:      opts.BriefingStore,
		dagStore:           opts.DAGSnapshotStore,
		capabilityRegistry: opts.CapabilityRegistry,
		roleMenus:          opts.RoleMenus,
		briefings:          make(map[string]*briefingState),
		phaseBaselines:     make(map[string]string),
		now:                now,
	}
	h.dispatch = map[string]methodSpec{
		MethodPlanEmit: {
			allowedRoles: rolesSet(RolePlanner),
			handle:       (*Handler).handlePlanEmit,
		},
		MethodPlanSkip: {
			allowedRoles: rolesSet(RolePlanner),
			handle:       (*Handler).handlePlanSkip,
		},
		MethodBriefingEmit: {
			allowedRoles: rolesSet(RoleBriefer),
			handle:       (*Handler).handleBriefingEmit,
		},
		MethodGetDAGSnapshot: {
			allowedRoles: rolesSet(RoleBriefer, RoleExecutor, RoleReviewer),
			handle:       (*Handler).handleGetDAGSnapshot,
		},
		MethodGetBriefing: {
			allowedRoles: rolesSet(RoleExecutor, RoleReviewer),
			handle:       (*Handler).handleGetBriefing,
		},
		MethodGetPendingTasks: {
			allowedRoles: rolesSet(RoleExecutor),
			handle:       (*Handler).handleGetPendingTasks,
		},
		MethodTaskStarted: {
			allowedRoles: rolesSet(RolePlanner, RoleBriefer, RoleExecutor, RoleReviewer),
			handle:       (*Handler).handleTaskStarted,
		},
		MethodTaskCompleted: {
			allowedRoles: rolesSet(RolePlanner, RoleBriefer, RoleExecutor, RoleReviewer),
			handle:       (*Handler).handleTaskCompleted,
		},
		MethodIterationFinished: {
			allowedRoles: rolesSet(RoleExecutor),
			handle:       (*Handler).handleIterationFinished,
		},
		MethodGetBaseline: {
			allowedRoles: rolesSet(RoleReviewer),
			handle:       (*Handler).handleGetBaseline,
		},
		MethodGetJournalDelta: {
			allowedRoles: rolesSet(RoleReviewer),
			handle:       (*Handler).handleGetJournalDelta,
		},
		MethodTaskApproved: {
			allowedRoles: rolesSet(RoleReviewer),
			handle:       (*Handler).handleTaskApproved,
		},
		MethodTaskNeedsFix: {
			allowedRoles: rolesSet(RoleReviewer),
			handle:       (*Handler).handleTaskNeedsFix,
		},
		MethodReviewFinished: {
			allowedRoles: rolesSet(RoleReviewer),
			handle:       (*Handler).handleReviewFinished,
		},
	}
	return h
}

// Registry returns the underlying registry so callers (cmd/cli wiring,
// loop driver) can register and deregister agents around invocations.
func (h *Handler) Registry() *AgentRegistry { return h.registry }

// State returns the underlying DAG state. Returns nil before
// bcc_plan_emit lands the first plan.
func (h *Handler) State() *State { return h.state }

// SetState replaces the handler's DAG state. The loop calls this when
// it has built state from a Plan that did not flow through
// bcc_plan_emit (legacy planner adapters in tests, or a resumed
// session whose plan is already on disk).
func (h *Handler) SetState(s *State) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state = s
}

// SetPlan stores the most recently confirmed Plan on the handler so
// later queries (snapshots, briefings) see it. The loop calls this on
// the resumed-plan path. Setting a non-nil plan also clears any prior
// plan-skip state so the two terminals stay mutually exclusive.
func (h *Handler) SetPlan(p *supervision.Plan) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.plan = p
	if p != nil {
		h.planSkipped = false
		h.planSkipReason = ""
	}
}

// PlanSkipped reports whether the Planner declared the spec done by
// calling bcc_plan_skip. False before any planner terminal call lands
// or after a Plan was successfully emitted.
func (h *Handler) PlanSkipped() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.planSkipped
}

// PlanSkipReason returns the reason string the Planner attached to
// bcc_plan_skip, or empty when the planner did not skip.
func (h *Handler) PlanSkipReason() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.planSkipReason
}

// Plan returns the most recently emitted Plan, or nil before the
// Planner has called bcc_plan_emit successfully.
func (h *Handler) Plan() *supervision.Plan {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.plan
}

// Briefing returns the Briefing emitted under iterationID, or nil if
// no such briefing has been emitted yet.
func (h *Handler) Briefing(iterationID string) *supervision.Briefing {
	h.mu.Lock()
	defer h.mu.Unlock()
	bs := h.briefings[iterationID]
	if bs == nil {
		return nil
	}
	return bs.briefing
}

// AttachAudit binds an AuditLog to the handler. Late-binding is the
// production path: cli boot constructs the handler before the session
// directory is known, then attaches the audit log once the session is
// resolved. nil disables auditing.
func (h *Handler) AttachAudit(audit *AuditLog) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.audit = audit
}

// AttachObserver binds a HandlerObserver to the handler. Late-binding
// matches AttachAudit: the loop attaches its translator at the start
// of runDirector and detaches with nil at exit. Only one observer is
// active at a time; subsequent calls replace the previous binding.
func (h *Handler) AttachObserver(o HandlerObserver) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.observer = o
}

// SetCapabilityRegistry installs the merged capability registry the
// handler uses to validate per-phase role assignments emitted with
// bcc_plan_emit. Plans land before the registry on cli boot ordering,
// so this is exposed as a setter rather than a constructor argument.
// nil disables capability validation; assignments are then accepted
// without checking model or effort against any registry.
func (h *Handler) SetCapabilityRegistry(reg *supervision.CapabilityRegistry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.capabilityRegistry = reg
}

// SetRoleMenus installs the per-role option menus the handler uses to
// fill missing assignments and validate the Planner's per-phase routing
// at bcc_plan_emit time. Filling at emit makes the persisted plan.json
// self-describing: every Phase carries the routing it actually ran
// with. Empty menus on a role accept any value the Planner emitted; in
// production every role has at least one option after defaults run.
func (h *Handler) SetRoleMenus(menus supervision.RoleMenus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.roleMenus = menus
}

// RoleMenus returns the role-options menus the handler is currently
// validating bcc_plan_emit against.
func (h *Handler) RoleMenus() supervision.RoleMenus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.roleMenus
}

// CapabilityRegistry returns the registry the handler validates
// bcc_plan_emit assignments against, or nil when none was attached.
func (h *Handler) CapabilityRegistry() *supervision.CapabilityRegistry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.capabilityRegistry
}

// AttachStores binds the per-session persistence ports to the handler.
// Any nil argument leaves the corresponding store untouched. Same
// late-binding rationale as AttachAudit.
func (h *Handler) AttachStores(plan PlanPersister, briefing BriefingPersister, dagSnap DAGSnapshotPersister) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if plan != nil {
		h.planStore = plan
	}
	if briefing != nil {
		h.briefingStore = briefing
	}
	if dagSnap != nil {
		h.dagStore = dagSnap
	}
}

// AttachProviders binds head and journal providers. Any nil argument
// leaves the corresponding provider untouched.
func (h *Handler) AttachProviders(head HeadProvider, journal JournalDeltaProvider) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if head != nil {
		h.head = head
	}
	if journal != nil {
		h.journal = journal
	}
}

// SetPhaseBaseline records the phase-scoped baseline SHA captured
// before the first attempt of phaseID. Stable across all attempts
// of the same phase.
func (h *Handler) SetPhaseBaseline(phaseID, sha string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.phaseBaselines == nil {
		h.phaseBaselines = make(map[string]string)
	}
	h.phaseBaselines[phaseID] = sha
}

// SetBriefingJournalSnapshots records the spec-content snapshots the
// handler diffs to compute bcc_get_journal_delta for the audited
// iterationID. Empty bytes are valid: an unchanged journal yields an
// empty delta.
func (h *Handler) SetBriefingJournalSnapshots(iterationID string, before, after []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	bs := h.briefings[iterationID]
	if bs == nil {
		bs = &briefingState{}
		h.briefings[iterationID] = bs
	}
	bs.journalBefore = append([]byte(nil), before...)
	bs.journalAfter = append([]byte(nil), after...)
}

// HandleCall implements mcp.Handler. The dispatch is:
//
//  1. Every bcc_* method goes through the dispatch table. The agent
//     identity is read from input["agent_id"]; the registry must know
//     it, and the registered role must match the connection name.
//  2. Unknown method names return a structured error.
//
// Every successful or rejected dispatch appends one record to the
// audit log when configured.
func (h *Handler) HandleCall(ctx context.Context, connectionName, methodName string, input map[string]any) (string, error) {
	spec, ok := h.dispatch[methodName]
	if !ok {
		return "", fmt.Errorf("dag: unknown method %q", methodName)
	}
	if !spec.allowedRoles[Role(connectionName)] {
		err := fmt.Errorf("dag: connection %q not allowed to call %q", connectionName, methodName)
		h.logCall(connectionName, "", methodName, input, "", err)
		return "", err
	}
	id, _ := input["agent_id"].(string)
	if id == "" {
		err := fmt.Errorf("dag: %s: missing agent_id", methodName)
		h.logCall(connectionName, "", methodName, input, "", err)
		return "", err
	}
	entry, ok := h.registry.Lookup(AgentID(id))
	if !ok {
		err := fmt.Errorf("dag: %s: unregistered agent_id %q", methodName, id)
		h.logCall(connectionName, id, methodName, input, "", err)
		return "", err
	}
	if string(entry.Role) != connectionName {
		err := fmt.Errorf("dag: %s: agent_id %q registered as %q, called from %q",
			methodName, id, string(entry.Role), connectionName)
		h.logCall(connectionName, id, methodName, input, "", err)
		return "", err
	}
	if methodName == MethodBriefingEmit {
		coerceStringifiedObject(input, "briefing", id, methodName)
	}
	if sch := h.schemas[methodName]; sch != nil {
		if err := sch.Validate(input); err != nil {
			wrapped := fmt.Errorf("dag: %s: schema validation: %w", methodName, err)
			h.logCall(connectionName, id, methodName, input, "", wrapped)
			return "", wrapped
		}
	}
	result, err := spec.handle(h, ctx, entry, input)
	if err == nil {
		h.mu.Lock()
		obs := h.observer
		h.mu.Unlock()
		if obs != nil {
			obs.OnCall(methodName, id, Role(connectionName), input)
		}
	}
	h.logCall(connectionName, id, methodName, input, result, err)
	return result, err
}

func (h *Handler) logCall(role, agentID, method string, input map[string]any, result string, err error) {
	if h.audit == nil {
		return
	}
	entry := AuditEntry{
		At:      h.now(),
		Role:    role,
		AgentID: agentID,
		Method:  method,
		Input:   input,
		Result:  result,
	}
	if err != nil {
		entry.Err = err.Error()
	}
	_ = h.audit.Append(entry)
}

func rolesSet(roles ...Role) map[Role]bool {
	out := make(map[Role]bool, len(roles))
	for _, r := range roles {
		out[r] = true
	}
	return out
}

// coerceStringifiedObject mutates input in place when input[key] arrived
// as a JSON-stringified object literal instead of an object. It is a
// tolerance pass for a recurring LLM mistake on tools that take a large
// structured field: the model wraps the JSON in quotes and ships a
// string. When the string parses to a JSON object, we substitute the
// parsed value and emit a slog warning so the regression is visible.
// When the string does not parse, or the value is some other non-object
// type, the input is left untouched and downstream schema validation
// rejects it as before.
func coerceStringifiedObject(input map[string]any, key, agentID, method string) {
	raw, ok := input[key].(string)
	if !ok {
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return
	}
	input[key] = parsed
	slog.Warn("dag: agent sent stringified object, coerced",
		"method", method,
		"agent_id", agentID,
		"key", key,
	)
}

// handlePlanEmit accepts a Plan body, validates it (schema + structural
// invariants), replaces the in-memory DAG state, and persists the plan.
// On rejection the prior plan stays in place; the agent reads the
// structured error and re-emits.
func (h *Handler) handlePlanEmit(_ context.Context, _ AgentEntry, input map[string]any) (string, error) {
	planRaw, ok := input["plan"]
	if !ok {
		return "", errors.New("dag: bcc_plan_emit: missing plan")
	}
	if sch := h.schemas[planSchemaKey]; sch != nil {
		if err := sch.Validate(planRaw); err != nil {
			return "", fmt.Errorf("dag: bcc_plan_emit: plan schema: %w", err)
		}
	}
	body, err := json.Marshal(planRaw)
	if err != nil {
		return "", fmt.Errorf("dag: bcc_plan_emit: marshal plan: %w", err)
	}
	var plan supervision.Plan
	if err := json.Unmarshal(body, &plan); err != nil {
		return "", fmt.Errorf("dag: bcc_plan_emit: parse plan: %w", err)
	}
	if err := supervision.ValidatePlan(&plan); err != nil {
		return "", fmt.Errorf("dag: bcc_plan_emit: %w", err)
	}
	supervision.FillPlanFromMenus(&plan, h.roleMenus)
	if err := supervision.ValidatePlanAgainstMenus(&plan, h.roleMenus); err != nil {
		return "", fmt.Errorf("dag: bcc_plan_emit: %w", err)
	}
	newState := NewStateFromPlan(&plan)

	h.mu.Lock()
	if h.planSkipped {
		h.mu.Unlock()
		return "", errors.New("dag: bcc_plan_emit: plan was already skipped via bcc_plan_skip")
	}
	h.plan = &plan
	h.state = newState
	h.mu.Unlock()

	if h.planStore != nil {
		if err := h.planStore.WritePlan(&plan); err != nil {
			return "", fmt.Errorf("dag: bcc_plan_emit: persist plan: %w", err)
		}
	}
	if err := h.persistDAG(newState); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}

// handlePlanSkip records the Planner's "nothing to do" verdict. The
// loop reads PlanSkipped/PlanSkipReason after the planner adapter
// returns and exits cleanly with ExitDone instead of synthesising a
// Plan. Mutually exclusive with bcc_plan_emit.
func (h *Handler) handlePlanSkip(_ context.Context, _ AgentEntry, input map[string]any) (string, error) {
	reason, _ := input["reason"].(string)
	h.mu.Lock()
	if h.plan != nil {
		h.mu.Unlock()
		return "", errors.New("dag: bcc_plan_skip: plan was already emitted via bcc_plan_emit")
	}
	h.planSkipped = true
	h.planSkipReason = reason
	h.mu.Unlock()
	return `{"ok":true}`, nil
}

// handleBriefingEmit accepts a Briefing body, validates it against the
// current plan + DAG state, stores it, and returns a confirmation. The
// emitted briefing is later looked up by Executor and Reviewer agents
// whose registry entries point at its iteration_id.
func (h *Handler) handleBriefingEmit(_ context.Context, _ AgentEntry, input map[string]any) (string, error) {
	briefRaw, ok := input["briefing"]
	if !ok {
		return "", errors.New("dag: bcc_briefing_emit: missing briefing")
	}
	body, err := json.Marshal(briefRaw)
	if err != nil {
		return "", fmt.Errorf("dag: bcc_briefing_emit: marshal briefing: %w", err)
	}
	var brief supervision.Briefing
	if err := json.Unmarshal(body, &brief); err != nil {
		return "", fmt.Errorf("dag: bcc_briefing_emit: parse briefing: %w", err)
	}
	if brief.IterationID == "" {
		brief.IterationID = fmt.Sprintf("%s-%d", brief.PhaseID, h.now().UnixNano())
	}
	if err := h.storeValidatedBriefing(&brief, "dag: bcc_briefing_emit"); err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]any{
		"ok":           true,
		"iteration_id": brief.IterationID,
	})
	return string(out), nil
}

// RecordSyntheticBriefing stores a Briefing the Planner authored
// inline on the Phase (Phase.PreparedBriefing) so the loop can run an
// iteration without spawning a Briefer agent. The briefing is
// validated against the current plan with the same rules as
// bcc_briefing_emit, then persisted and recorded in the audit log
// under role "planner" so the timeline still shows where the briefing
// came from.
func (h *Handler) RecordSyntheticBriefing(brief supervision.Briefing) error {
	if brief.IterationID == "" {
		return errors.New("dag: RecordSyntheticBriefing: empty iteration_id")
	}
	briefCopy := brief
	if err := h.storeValidatedBriefing(&briefCopy, "dag: RecordSyntheticBriefing"); err != nil {
		return err
	}
	if h.audit != nil {
		_ = h.audit.Append(AuditEntry{
			At:     h.now(),
			Role:   "planner",
			Method: "synthetic_briefing",
			Input: map[string]any{
				"iteration_id":     brief.IterationID,
				"phase_id":         brief.PhaseID,
				"sub_dag_task_ids": stringSliceToAny(brief.SubDAGTaskIDs),
			},
			Result: `{"ok":true}`,
		})
	}
	return nil
}

// RecordSyntheticApproval marks every sub-DAG task bound to
// iterationID as done and records the synthetic approval under role
// "planner" in the audit log. The loop calls this when a Phase has
// SkipReview=true so the Reviewer agent is bypassed entirely. The
// recorded review outcome is "approve" so the decider's existing
// advance-on-approve path runs unchanged.
func (h *Handler) RecordSyntheticApproval(iterationID string) error {
	if iterationID == "" {
		return errors.New("dag: RecordSyntheticApproval: empty iteration_id")
	}
	h.mu.Lock()
	bs := h.briefings[iterationID]
	state := h.state
	h.mu.Unlock()
	if bs == nil || bs.briefing == nil {
		return fmt.Errorf("dag: RecordSyntheticApproval: unknown iteration %q", iterationID)
	}
	if state == nil {
		return errors.New("dag: RecordSyntheticApproval: no DAG state")
	}
	phaseID := PhaseID(bs.briefing.PhaseID)
	phase := state.Phase(phaseID)
	if phase == nil {
		return fmt.Errorf("dag: RecordSyntheticApproval: phase %q unknown", phaseID)
	}
	approved := make([]string, 0, len(bs.briefing.SubDAGTaskIDs))
	for _, tid := range bs.briefing.SubDAGTaskIDs {
		t, ok := phase.Tasks[TaskID(tid)]
		if !ok {
			return fmt.Errorf("dag: RecordSyntheticApproval: task %q not in phase %q", tid, phaseID)
		}
		if t.Status == supervision.TaskDone {
			continue
		}
		if err := state.SetTaskStatus(phaseID, TaskID(tid), supervision.TaskDone); err != nil {
			return fmt.Errorf("dag: RecordSyntheticApproval: %w", err)
		}
		approved = append(approved, tid)
	}
	if err := h.persistDAG(state); err != nil {
		return err
	}

	h.mu.Lock()
	bs.reviewOutcome = "approve"
	bs.reviewReason = "planner: skip_review on this phase"
	h.mu.Unlock()

	if h.audit != nil {
		input := map[string]any{
			"iteration_id": iterationID,
			"phase_id":     string(phaseID),
		}
		if len(approved) > 0 {
			input["approved_task_ids"] = stringSliceToAny(approved)
		}
		_ = h.audit.Append(AuditEntry{
			At:     h.now(),
			Role:   "planner",
			Method: "synthetic_approval",
			Input:  input,
			Result: `{"ok":true}`,
		})
	}
	return nil
}

// storeValidatedBriefing checks the structural invariants both the
// Briefer-emitted and Planner-prepared briefings must satisfy and
// stores the result on the handler. errPrefix flavors the messages so
// callers (the bcc_briefing_emit handler vs. RecordSyntheticBriefing)
// remain distinguishable in the audit log and logs.
func (h *Handler) storeValidatedBriefing(brief *supervision.Briefing, errPrefix string) error {
	h.mu.Lock()
	state := h.state
	h.mu.Unlock()
	if state == nil {
		return fmt.Errorf("%s: no plan emitted yet", errPrefix)
	}
	phase := state.Phase(PhaseID(brief.PhaseID))
	if phase == nil {
		return fmt.Errorf("%s: unknown phase %q", errPrefix, brief.PhaseID)
	}
	if !phaseIsEligible(state, PhaseID(brief.PhaseID)) {
		return fmt.Errorf("%s: phase %q not eligible (deps not done)", errPrefix, brief.PhaseID)
	}
	if len(brief.SubDAGTaskIDs) == 0 {
		return fmt.Errorf("%s: empty sub_dag_task_ids", errPrefix)
	}
	subSet := make(map[string]bool, len(brief.SubDAGTaskIDs))
	for _, tid := range brief.SubDAGTaskIDs {
		t, ok := phase.Tasks[TaskID(tid)]
		if !ok {
			return fmt.Errorf("%s: task %q not in phase %q", errPrefix, tid, brief.PhaseID)
		}
		if t.Status != supervision.TaskPending && t.Status != supervision.TaskNeedsFix {
			return fmt.Errorf("%s: task %q status %q is not pending/needs_fix",
				errPrefix, tid, string(t.Status))
		}
		subSet[tid] = true
	}
	for _, tid := range brief.SubDAGTaskIDs {
		t := phase.Tasks[TaskID(tid)]
		for _, dep := range t.DependsOn {
			if subSet[string(dep)] {
				continue
			}
			depTask := phase.Tasks[dep]
			if depTask == nil || depTask.Status != supervision.TaskDone {
				return fmt.Errorf("%s: task %q depends on %q which is neither in sub-DAG nor done",
					errPrefix, tid, dep)
			}
		}
	}

	h.mu.Lock()
	bs := h.briefings[brief.IterationID]
	if bs == nil {
		bs = &briefingState{}
		h.briefings[brief.IterationID] = bs
	}
	briefCopy := *brief
	bs.briefing = &briefCopy
	h.mu.Unlock()

	if h.briefingStore != nil {
		if err := h.briefingStore.WriteBriefing(brief); err != nil {
			return fmt.Errorf("%s: persist briefing: %w", errPrefix, err)
		}
	}
	return nil
}

// stringSliceToAny converts a []string to []any for audit log inputs.
// The audit serializer is JSON-driven and accepts heterogenous slices
// only as []any.
func stringSliceToAny(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

// handleGetDAGSnapshot returns the live DAG state. The Briefer sees
// the full state; Executor and Reviewer agents see only the phase they
// are registered against.
func (h *Handler) handleGetDAGSnapshot(_ context.Context, entry AgentEntry, _ map[string]any) (string, error) {
	h.mu.Lock()
	state := h.state
	h.mu.Unlock()
	if state == nil {
		return "", errors.New("dag: bcc_get_dag_snapshot: no plan emitted yet")
	}
	if entry.Role == RoleBriefer {
		body, err := json.Marshal(state)
		if err != nil {
			return "", fmt.Errorf("dag: bcc_get_dag_snapshot: marshal: %w", err)
		}
		return string(body), nil
	}
	if entry.PhaseID == "" {
		return "", errors.New("dag: bcc_get_dag_snapshot: agent has no phase scope")
	}
	phase := state.Phase(entry.PhaseID)
	if phase == nil {
		return "", fmt.Errorf("dag: bcc_get_dag_snapshot: phase %q unknown", entry.PhaseID)
	}
	body, err := json.Marshal(map[string]any{
		"phase": phase,
	})
	if err != nil {
		return "", fmt.Errorf("dag: bcc_get_dag_snapshot: marshal phase: %w", err)
	}
	return string(body), nil
}

// handleGetBriefing returns the Briefing the calling agent is bound
// to, identified by entry.BriefingID assigned at registration time.
func (h *Handler) handleGetBriefing(_ context.Context, entry AgentEntry, _ map[string]any) (string, error) {
	if entry.BriefingID == "" {
		return "", errors.New("dag: bcc_get_briefing: agent has no briefing scope")
	}
	h.mu.Lock()
	bs := h.briefings[entry.BriefingID]
	h.mu.Unlock()
	if bs == nil || bs.briefing == nil {
		return "", fmt.Errorf("dag: bcc_get_briefing: briefing %q not emitted", entry.BriefingID)
	}
	body, err := json.Marshal(bs.briefing)
	if err != nil {
		return "", fmt.Errorf("dag: bcc_get_briefing: marshal: %w", err)
	}
	return string(body), nil
}

// handleGetPendingTasks returns the calling agent's SubDAG members
// whose status is still pending or needs_fix.
func (h *Handler) handleGetPendingTasks(_ context.Context, entry AgentEntry, _ map[string]any) (string, error) {
	if len(entry.SubDAG) == 0 {
		return "", errors.New("dag: bcc_get_pending_tasks: agent has no sub-DAG scope")
	}
	if entry.PhaseID == "" {
		return "", errors.New("dag: bcc_get_pending_tasks: agent has no phase scope")
	}
	h.mu.Lock()
	state := h.state
	h.mu.Unlock()
	if state == nil {
		return "", errors.New("dag: bcc_get_pending_tasks: no plan emitted yet")
	}
	phase := state.Phase(entry.PhaseID)
	if phase == nil {
		return "", fmt.Errorf("dag: bcc_get_pending_tasks: phase %q unknown", entry.PhaseID)
	}
	out := make([]TaskID, 0, len(entry.SubDAG))
	for _, tid := range entry.SubDAG {
		t := phase.Tasks[tid]
		if t == nil {
			continue
		}
		if t.Status == supervision.TaskPending || t.Status == supervision.TaskNeedsFix {
			out = append(out, tid)
		}
	}
	body, err := json.Marshal(map[string]any{"pending": out})
	if err != nil {
		return "", fmt.Errorf("dag: bcc_get_pending_tasks: marshal: %w", err)
	}
	return string(body), nil
}

// handleTaskStarted marks the requested task as in_progress. The
// Planner uses the well-known PlanningTaskID and bypasses DAG state.
func (h *Handler) handleTaskStarted(_ context.Context, entry AgentEntry, input map[string]any) (string, error) {
	id, _ := input["id"].(string)
	if entry.Role == RolePlanner {
		if id != PlanningTaskID {
			return "", fmt.Errorf("dag: bcc_task_started: planner must use id=%q, got %q", PlanningTaskID, id)
		}
		return `{"ok":true}`, nil
	}
	if entry.Role == RoleBriefer {
		if id != BriefingTaskID {
			return "", fmt.Errorf("dag: bcc_task_started: briefer must use id=%q, got %q", BriefingTaskID, id)
		}
		return `{"ok":true}`, nil
	}
	if entry.Role == RoleReviewer {
		if id != ReviewingTaskID {
			return "", fmt.Errorf("dag: bcc_task_started: reviewer must use id=%q, got %q", ReviewingTaskID, id)
		}
		return `{"ok":true}`, nil
	}
	if err := h.assertExecutorScope(entry, id); err != nil {
		return "", err
	}
	if err := h.state.SetTaskStatus(entry.PhaseID, id, supervision.TaskInProgress); err != nil {
		return "", fmt.Errorf("dag: bcc_task_started: %w", err)
	}
	if err := h.persistDAG(h.state); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}

// handleTaskCompleted marks the requested task as done.
func (h *Handler) handleTaskCompleted(_ context.Context, entry AgentEntry, input map[string]any) (string, error) {
	id, _ := input["id"].(string)
	if entry.Role == RolePlanner {
		if id != PlanningTaskID {
			return "", fmt.Errorf("dag: bcc_task_completed: planner must use id=%q, got %q", PlanningTaskID, id)
		}
		return `{"ok":true}`, nil
	}
	if entry.Role == RoleBriefer {
		if id != BriefingTaskID {
			return "", fmt.Errorf("dag: bcc_task_completed: briefer must use id=%q, got %q", BriefingTaskID, id)
		}
		return `{"ok":true}`, nil
	}
	if entry.Role == RoleReviewer {
		if id != ReviewingTaskID {
			return "", fmt.Errorf("dag: bcc_task_completed: reviewer must use id=%q, got %q", ReviewingTaskID, id)
		}
		return `{"ok":true}`, nil
	}
	if err := h.assertExecutorScope(entry, id); err != nil {
		return "", err
	}
	if err := h.state.SetTaskStatus(entry.PhaseID, id, supervision.TaskDone); err != nil {
		return "", fmt.Errorf("dag: bcc_task_completed: %w", err)
	}
	if err := h.persistDAG(h.state); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}

// handleIterationFinished records the Executor's exit signal under the
// briefing the agent is scoped to so the loop driver can read it once
// the Executor.Run subprocess returns. Last-write-wins: a misbehaving
// Executor that emits more than one terminal signal contributes only
// the final one.
func (h *Handler) handleIterationFinished(_ context.Context, entry AgentEntry, input map[string]any) (string, error) {
	signal, _ := input["signal"].(string)
	if entry.BriefingID == "" {
		return "", errors.New("dag: bcc_iteration_finished: agent has no briefing scope")
	}
	h.mu.Lock()
	bs := h.briefings[entry.BriefingID]
	if bs == nil {
		bs = &briefingState{}
		h.briefings[entry.BriefingID] = bs
	}
	bs.iterSignal = signal
	h.mu.Unlock()
	out, _ := json.Marshal(map[string]any{
		"ok":     true,
		"signal": signal,
	})
	return string(out), nil
}

// IterationSignal returns the signal the Executor reported via
// bcc_iteration_finished for the given briefing, or the empty string
// when no signal was observed (Executor exited without calling the
// terminal method, or the briefing is unknown). Callers (the loop
// driver) treat the empty value as SignalUnknown.
func (h *Handler) IterationSignal(briefingID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	bs := h.briefings[briefingID]
	if bs == nil {
		return ""
	}
	return bs.iterSignal
}

// handleGetBaseline returns the phase-scoped baseline SHA (stable
// across all attempts of the phase) and the current HEAD SHA so the
// Reviewer can run git diff/log/show via Bash.
func (h *Handler) handleGetBaseline(ctx context.Context, entry AgentEntry, _ map[string]any) (string, error) {
	phaseID := string(entry.PhaseID)
	if phaseID == "" {
		return "", errors.New("dag: bcc_get_baseline: agent has no phase scope")
	}
	h.mu.Lock()
	baseSHA, ok := h.phaseBaselines[phaseID]
	h.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("dag: bcc_get_baseline: no baseline recorded for phase %q", phaseID)
	}
	if h.head == nil {
		return "", errors.New("dag: bcc_get_baseline: no head provider configured")
	}
	headSHA, err := h.head.HeadSHA(ctx)
	if err != nil {
		return "", fmt.Errorf("dag: bcc_get_baseline: %w", err)
	}
	body, _ := json.Marshal(map[string]any{
		"phase_id":           phaseID,
		"phase_baseline_sha": baseSHA,
		"current_head_sha":   headSHA,
	})
	return string(body), nil
}

// handleGetJournalDelta computes the per-iteration journal delta via
// the configured JournalDeltaProvider.
func (h *Handler) handleGetJournalDelta(_ context.Context, entry AgentEntry, _ map[string]any) (string, error) {
	if h.journal == nil {
		return "", errors.New("dag: bcc_get_journal_delta: no journal provider configured")
	}
	if entry.BriefingID == "" {
		return "", errors.New("dag: bcc_get_journal_delta: agent has no briefing scope")
	}
	h.mu.Lock()
	bs := h.briefings[entry.BriefingID]
	var before, after []byte
	if bs != nil {
		before = append([]byte(nil), bs.journalBefore...)
		after = append([]byte(nil), bs.journalAfter...)
	}
	h.mu.Unlock()
	if bs == nil {
		return "", fmt.Errorf("dag: bcc_get_journal_delta: no journal snapshots recorded for briefing %q", entry.BriefingID)
	}
	delta := h.journal.JournalDelta(before, after)
	body, _ := json.Marshal(map[string]any{"delta": delta})
	return string(body), nil
}

// handleTaskApproved marks the task as done within the Reviewer's
// audited sub-DAG.
func (h *Handler) handleTaskApproved(_ context.Context, entry AgentEntry, input map[string]any) (string, error) {
	id, _ := input["id"].(string)
	if err := h.assertReviewerScope(entry, id); err != nil {
		return "", err
	}
	if err := h.state.SetTaskStatus(entry.PhaseID, id, supervision.TaskDone); err != nil {
		return "", fmt.Errorf("dag: bcc_task_approved: %w", err)
	}
	if err := h.persistDAG(h.state); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}

// handleTaskNeedsFix returns the task to needs_fix.
func (h *Handler) handleTaskNeedsFix(_ context.Context, entry AgentEntry, input map[string]any) (string, error) {
	id, _ := input["id"].(string)
	if err := h.assertReviewerScope(entry, id); err != nil {
		return "", err
	}
	if err := h.state.SetTaskStatus(entry.PhaseID, id, supervision.TaskNeedsFix); err != nil {
		return "", fmt.Errorf("dag: bcc_task_needs_fix: %w", err)
	}
	if err := h.persistDAG(h.state); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}

// handleReviewFinished enforces the cross-method invariants on outcome
// vs. per-task DAG state and returns a confirmation.
func (h *Handler) handleReviewFinished(_ context.Context, entry AgentEntry, input map[string]any) (string, error) {
	outcome, _ := input["outcome"].(string)
	reasoning, _ := input["reasoning"].(string)
	if entry.BriefingID == "" {
		return "", errors.New("dag: bcc_review_finished: agent has no briefing scope")
	}
	state := h.state
	if state == nil {
		return "", errors.New("dag: bcc_review_finished: no DAG state")
	}
	phase := state.Phase(entry.PhaseID)
	if phase == nil {
		return "", fmt.Errorf("dag: bcc_review_finished: phase %q unknown", entry.PhaseID)
	}
	doneAll := true
	anyNeedsFix := false
	for _, tid := range entry.SubDAG {
		t := phase.Tasks[tid]
		if t == nil {
			return "", fmt.Errorf("dag: bcc_review_finished: task %q missing from phase", tid)
		}
		if t.Status != supervision.TaskDone {
			doneAll = false
		}
		if t.Status == supervision.TaskNeedsFix {
			anyNeedsFix = true
		}
	}
	switch outcome {
	case "approve":
		if !doneAll {
			return "", errors.New("dag: bcc_review_finished: approve requires every sub-DAG task done")
		}
	case "revise":
		if !anyNeedsFix {
			return "", errors.New("dag: bcc_review_finished: revise requires at least one needs_fix")
		}
	case "escalate":
		if reasoning == "" {
			return "", errors.New("dag: bcc_review_finished: escalate requires reasoning")
		}
	default:
		return "", fmt.Errorf("dag: bcc_review_finished: invalid outcome %q", outcome)
	}
	h.mu.Lock()
	bs := h.briefings[entry.BriefingID]
	if bs == nil {
		bs = &briefingState{}
		h.briefings[entry.BriefingID] = bs
	}
	bs.reviewOutcome = outcome
	bs.reviewReason = reasoning
	h.mu.Unlock()
	body, _ := json.Marshal(map[string]any{
		"ok":      true,
		"outcome": outcome,
	})
	return string(body), nil
}

// ForceApprovePending marks every still-pending or needs_fix task in
// the sub-DAG bound to iterationID as done, persists the snapshot, and
// appends a synthetic audit entry recording the user's force-approve
// decision. The audit role is "user" and the method name is
// "bcc_force_approve" so the entry is distinguishable from agent-driven
// task approvals. Returns an error if the briefing is unknown or the
// phase the briefing targets is missing from state.
func (h *Handler) ForceApprovePending(iterationID, hint string) error {
	if iterationID == "" {
		return errors.New("dag: ForceApprovePending: empty iteration_id")
	}
	h.mu.Lock()
	bs := h.briefings[iterationID]
	state := h.state
	h.mu.Unlock()
	if bs == nil || bs.briefing == nil {
		return fmt.Errorf("dag: ForceApprovePending: unknown iteration %q", iterationID)
	}
	if state == nil {
		return errors.New("dag: ForceApprovePending: no DAG state")
	}
	phaseID := PhaseID(bs.briefing.PhaseID)
	phase := state.Phase(phaseID)
	if phase == nil {
		return fmt.Errorf("dag: ForceApprovePending: phase %q unknown", phaseID)
	}
	approved := make([]string, 0, len(bs.briefing.SubDAGTaskIDs))
	for _, tid := range bs.briefing.SubDAGTaskIDs {
		t, ok := phase.Tasks[tid]
		if !ok {
			return fmt.Errorf("dag: ForceApprovePending: task %q not in phase %q", tid, phaseID)
		}
		if t.Status == supervision.TaskDone {
			continue
		}
		if err := state.SetTaskStatus(phaseID, tid, supervision.TaskDone); err != nil {
			return fmt.Errorf("dag: ForceApprovePending: %w", err)
		}
		approved = append(approved, string(tid))
	}
	if err := h.persistDAG(state); err != nil {
		return err
	}
	if h.audit != nil {
		input := map[string]any{
			"iteration_id": iterationID,
		}
		if hint != "" {
			input["hint"] = hint
		}
		if len(approved) > 0 {
			ids := make([]any, len(approved))
			for i, id := range approved {
				ids[i] = id
			}
			input["approved_task_ids"] = ids
		}
		_ = h.audit.Append(AuditEntry{
			At:     h.now(),
			Role:   "user",
			Method: "bcc_force_approve",
			Input:  input,
			Result: `{"ok":true}`,
		})
	}
	return nil
}

// LastReviewOutcome returns the outcome string the Reviewer reported on
// bcc_review_finished for iterationID, or "" when no review has landed.
func (h *Handler) LastReviewOutcome(iterationID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	bs := h.briefings[iterationID]
	if bs == nil {
		return ""
	}
	return bs.reviewOutcome
}

// LastReviewReasoning mirrors LastReviewOutcome for the prose reasoning
// the Reviewer attached to bcc_review_finished.
func (h *Handler) LastReviewReasoning(iterationID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	bs := h.briefings[iterationID]
	if bs == nil {
		return ""
	}
	return bs.reviewReason
}

// ResetReviewOutcome clears the sticky review verdict on the
// briefingState for iterationID, so the next Reviewer attempt
// starts from an empty outcome. No-op when no briefing state is
// registered for that id.
func (h *Handler) ResetReviewOutcome(iterationID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	bs := h.briefings[iterationID]
	if bs == nil {
		return
	}
	bs.reviewOutcome = ""
	bs.reviewReason = ""
}

// assertExecutorScope rejects task ids outside the Executor's
// registered SubDAG. PhaseID must agree.
func (h *Handler) assertExecutorScope(entry AgentEntry, taskID string) error {
	if entry.PhaseID == "" {
		return errors.New("dag: agent has no phase scope")
	}
	if !contains(entry.SubDAG, taskID) {
		return fmt.Errorf("dag: task %q not in agent's sub-DAG", taskID)
	}
	if h.state == nil {
		return errors.New("dag: no DAG state")
	}
	return nil
}

// assertReviewerScope mirrors assertExecutorScope. Reviewer agents are
// scoped to the same SubDAG as the Executor they audit.
func (h *Handler) assertReviewerScope(entry AgentEntry, taskID string) error {
	if entry.PhaseID == "" {
		return errors.New("dag: reviewer has no phase scope")
	}
	if !contains(entry.SubDAG, taskID) {
		return fmt.Errorf("dag: task %q not in reviewer's sub-DAG", taskID)
	}
	if h.state == nil {
		return errors.New("dag: no DAG state")
	}
	return nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// persistDAG writes the current state snapshot via the configured
// DAGSnapshotPersister, if any. nil persister keeps state in-memory
// only and returns nil so tests need not wire one up.
func (h *Handler) persistDAG(s *State) error {
	if h.dagStore == nil || s == nil {
		return nil
	}
	if err := h.dagStore.WriteDAGSnapshot(s); err != nil {
		return fmt.Errorf("dag: persist snapshot: %w", err)
	}
	return nil
}

// phaseIsEligible mirrors State.EligiblePhases for one phase id. A
// phase is eligible when each phase-level dependency is fully done.
func phaseIsEligible(s *State, id PhaseID) bool {
	ps := s.Phase(id)
	if ps == nil {
		return false
	}
	for _, dep := range ps.DependsOn {
		dp := s.Phase(dep)
		if dp == nil {
			return false
		}
		for _, t := range dp.Tasks {
			if t.Status != supervision.TaskDone {
				return false
			}
		}
	}
	return true
}
