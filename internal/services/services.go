// Package services is the application services layer. It is the only
// caller of the domain core (internal/supervision, internal/supervision/dag,
// internal/loop, internal/config) from above. Protocol adapters
// (internal/api, internal/mcp, internal/tui) consume Services rather
// than reaching into the core directly, so a change in the core does
// not ripple across every protocol surface.
//
// Layer rules:
//
//   - services depends on internal/supervision, internal/supervision/dag,
//     internal/loop, internal/loop/agentcontract, and internal/config
//     for value objects and behavior the services aggregate.
//   - services does NOT depend on any protocol adapter (internal/api,
//     internal/mcp, internal/tui) or any executor/git adapter.
//   - Constructors return concrete types so consumers narrow to their
//     own interfaces if they need to.
package services

import (
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/services/events"
	"github.com/fgmacedo/buchecha/internal/supervision/dag"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
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
	SessionStore *session.Store

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

	// EventsLogPath, when non-empty, is the absolute path the
	// EventService appends each post-enrichment SeqEvent to
	// (.bcc/sessions/<id>/events.ndjson). The same file is read back
	// by EventService.Replay for archived sessions. Empty disables
	// persistence; the live fan-out continues to ring-and-broadcast
	// in memory either way.
	EventsLogPath string

	// LiveAliasArchivedID, when non-empty, makes the LiveSessionAlias
	// resolve to this archived session id even when no SessionStore is
	// bound. bcc dev sets this so the SPA's default "live" id maps to
	// the replayed archived session for snapshot, get, and event reads;
	// SessionStore stays nil so events route through Replay.
	LiveAliasArchivedID string

	// ReplayInterEventDelay throttles EventService.Replay so each
	// emitted event is followed by this pause before the next one is
	// read. Zero (default) emits as fast as the channel can drain. bcc
	// dev sets a small delay so the SPA's timeline animates instead of
	// dumping every event in a single frame.
	ReplayInterEventDelay time.Duration
}

// Services is the aggregator handed to every protocol adapter. Each
// field is the V1 read-only service its name suggests; the aggregator
// itself does not own behavior.
type Services struct {
	Sessions  *SessionService
	Events    *events.EventService
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
		Sessions: newSessionService(deps),
		Events: events.New(events.Deps{
			LoopEvents:            deps.LoopEvents,
			SessionStore:          deps.SessionStore,
			SessionsBaseDir:       deps.SessionsBaseDir,
			EventsLogPath:         deps.EventsLogPath,
			LiveAliasArchivedID:   deps.LiveAliasArchivedID,
			ReplayInterEventDelay: deps.ReplayInterEventDelay,
		}),
		Briefings: newBriefingService(deps),
		Prompts:   newPromptService(deps),
		Audit:     newAudit(deps),
	}
}
