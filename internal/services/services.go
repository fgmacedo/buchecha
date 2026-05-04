// Package services is the application services layer. It is the only
// caller of the domain core (internal/director, internal/director/dag,
// internal/loop, internal/config) from above. Protocol adapters
// (internal/api, internal/mcp, internal/tui) consume Services rather
// than reaching into the core directly, so a change in the core does
// not ripple across every protocol surface.
//
// Layer rules:
//
//   - services depends on internal/director, internal/director/dag,
//     internal/loop, internal/loop/agentcontract, and internal/config
//     for value objects and behavior the services aggregate.
//   - services does NOT depend on any protocol adapter (internal/api,
//     internal/mcp, internal/tui) or any executor/git adapter.
//   - Constructors return concrete types so consumers narrow to their
//     own interfaces if they need to.
package services

import (
	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/director/dag"
	"github.com/fgmacedo/buchecha/internal/loop"
)

// Deps is the seam between the domain core and the application
// services. The composition root in internal/cli/ builds a Deps from
// existing constructors and hands it to New. Services stash the
// handles they need; the rest is unused.
//
// Every field is optional from the type system's point of view so
// tests can construct partial Deps with fakes for the surfaces they
// exercise. Production wiring populates every field.
type Deps struct {
	// LoopEvents is the loop-wide events channel. The EventService
	// fans it out to subscribers, assigning a monotonic Seq starting
	// at 1 to each event.
	LoopEvents <-chan loop.Event

	// DAGHandler is the in-memory DAG handler the SessionService
	// queries for live session snapshots. The service deep-copies
	// the snapshot before returning it so consumers cannot mutate
	// the source.
	DAGHandler *dag.Handler

	// SessionStore is the live session's persistence facade. It
	// describes where the session directory lives on disk and
	// exposes the manifest the live SessionService surfaces.
	SessionStore *director.Store

	// SessionsBaseDir is the parent directory under which every
	// session lives (.bcc/sessions/<id>/). The SessionService uses
	// it to resolve archived sessions by id without going through
	// SessionStore.
	SessionsBaseDir string

	// AuditPath, when non-empty, is the absolute path the Audit
	// service appends entries to (.bcc/sessions/<id>/audit.ndjson).
	// Empty disables audit log writing; the aggregator still
	// constructs an Audit so callers do not branch on nil.
	AuditPath string
}

// Services is the aggregator handed to every protocol adapter. Each
// field is the V1 read-only service its name suggests; the aggregator
// itself does not own behavior.
type Services struct {
	Sessions  *SessionService
	Events    *EventService
	Briefings *BriefingService
	Prompts   *PromptService
	Audit     *Audit
}

// New constructs every V1 service from Deps. The constructor wires
// each sub-service handle but performs no I/O: the live event fan-out
// goroutine starts on the first Subscribe, and on-disk reads happen
// per-method. nil-safe: a Deps with zero values returns a Services
// whose individual methods report errors when their dependencies are
// missing.
func New(deps Deps) *Services {
	return &Services{
		Sessions:  newSessionService(deps),
		Events:    newEventService(deps),
		Briefings: newBriefingService(deps),
		Prompts:   newPromptService(deps),
		Audit:     newAudit(deps),
	}
}
