package services

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/fgmacedo/buchecha/internal/supervision"
	"github.com/fgmacedo/buchecha/internal/supervision/dag"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

// SessionMeta is the per-session metadata every adapter renders into
// its own wire format. Fields the persistence layer does not yet carry
// (BaselineSHA, IterationIndex, MaxIter) come back zero-valued for
// V1; later wiring populates them without breaking the contract.
type SessionMeta struct {
	ID             string    `json:"id"`
	SpecPath       string    `json:"spec_path"`
	BaselineSHA    string    `json:"baseline_sha"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at,omitzero"`
	Status         string    `json:"status"`
	IterationIndex int       `json:"iteration_index"`
	MaxIter        int       `json:"max_iter"`
}

// DAGSnapshot is a deep-copied snapshot of the DAG state owned by the
// caller. The alias keeps dag.State's canonical JSON form intact so
// adapters can serialize without wrapping; consumers must not retain
// the pointer beyond the call boundary if they want to mutate it.
type DAGSnapshot = *dag.State

// PhaseBriefedRef points at the most recent (PhaseID, Attempt) the
// Briefer materialized so a UI can deep-link into the briefing
// without walking the event stream.
type PhaseBriefedRef struct {
	PhaseID     string `json:"phase_id"`
	Attempt     int    `json:"attempt"`
	IterationID string `json:"iteration_id"`
}

// Snapshot is the SessionService.Snapshot return shape: enough state
// for an SPA to bootstrap a session view in one request without
// follow-ups for the dag and the latest briefing pointer.
type Snapshot struct {
	Session          SessionMeta      `json:"session"`
	DAG              DAGSnapshot      `json:"dag"`
	LastPhaseBriefed *PhaseBriefedRef `json:"last_phase_briefed,omitempty"`
}

// LiveSessionAlias is the reserved id callers pass when they want the
// service to resolve the currently bound live session. The SPA defaults
// to this alias on first load, before it has a real session id from
// /sessions, so the dashboard can render without bcc injecting the id at
// build time. Methods that accept an id (Get, Snapshot, plus the
// EventService) translate this token to the live session's real id; an
// alias lookup with no live session bound returns ErrSessionNotFound.
const LiveSessionAlias = "live"

// SessionService exposes live and archived session metadata and
// snapshots. The live session, when configured, is read from the
// in-memory dag handler and the live store; archived sessions are
// read from .bcc/sessions/<id>/.
type SessionService struct {
	deps Deps
}

func newSessionService(deps Deps) *SessionService {
	return &SessionService{deps: deps}
}

// List returns metadata for every session known under
// Deps.SessionsBaseDir, ordered by UpdatedAt descending so the most
// recently touched session appears first. The live session, when one
// is bound to the service via Deps.SessionStore, takes precedence
// over its archived counterpart in case of ID collision so callers
// always see the freshest status.
func (s *SessionService) List(ctx context.Context) ([]SessionMeta, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.deps.SessionsBaseDir == "" {
		return nil, ErrInternal.WithMessage("session service: no sessions base dir configured")
	}
	stored, err := session.ListSessions(s.deps.SessionsBaseDir)
	if err != nil {
		return nil, fmt.Errorf("services: list sessions: %w", err)
	}
	live := s.liveSession()
	out := make([]SessionMeta, 0, len(stored))
	for _, sess := range stored {
		if live != nil && sess.ID == live.ID {
			out = append(out, sessionMetaFrom(*live))
			continue
		}
		out = append(out, sessionMetaFrom(sess))
	}
	if live != nil {
		found := false
		for _, m := range out {
			if m.ID == live.ID {
				found = true
				break
			}
		}
		if !found {
			out = append([]SessionMeta{sessionMetaFrom(*live)}, out...)
		}
	}
	return out, nil
}

// Get returns the metadata for one session id. The live session takes
// precedence over the archived manifest when both exist for the same
// id. The reserved id LiveSessionAlias resolves to whichever session is
// bound as live; with no live session bound it returns ErrSessionNotFound.
// Unknown ids return ErrSessionNotFound.
func (s *SessionService) Get(ctx context.Context, id string) (SessionMeta, error) {
	if err := ctx.Err(); err != nil {
		return SessionMeta{}, err
	}
	if id == "" {
		return SessionMeta{}, ErrInvalidRequest.WithMessage("session service: empty id")
	}
	if id == LiveSessionAlias {
		if live := s.liveSession(); live != nil {
			return sessionMetaFrom(*live), nil
		}
		if s.deps.LiveAliasArchivedID != "" {
			id = s.deps.LiveAliasArchivedID
		} else {
			return SessionMeta{}, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
		}
	}
	if live := s.liveSession(); live != nil && live.ID == id {
		return sessionMetaFrom(*live), nil
	}
	if s.deps.SessionsBaseDir == "" {
		return SessionMeta{}, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
	}
	store, err := session.OpenSession(s.deps.SessionsBaseDir, id)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) || errors.Is(err, fs.ErrNotExist) {
			return SessionMeta{}, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
		}
		return SessionMeta{}, fmt.Errorf("services: get session %q: %w", id, err)
	}
	return sessionMetaFrom(*store.Session()), nil
}

// Snapshot returns the per-session bootstrap payload: metadata, DAG
// (deep-copied), and the most recent PhaseBriefed reference when
// known. Live sessions read DAG state from Deps.DAGHandler.Snapshot;
// archived sessions read the persisted dag.json under the session
// directory. The returned DAG pointer is independent of the live
// state so consumers cannot mutate the source.
func (s *SessionService) Snapshot(ctx context.Context, id string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if id == "" {
		return Snapshot{}, ErrInvalidRequest.WithMessage("session service: empty id")
	}
	if id == LiveSessionAlias {
		if live := s.liveSession(); live != nil {
			return s.liveSnapshot(*live)
		}
		if s.deps.LiveAliasArchivedID != "" {
			id = s.deps.LiveAliasArchivedID
		} else {
			return Snapshot{}, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
		}
	}
	if live := s.liveSession(); live != nil && live.ID == id {
		return s.liveSnapshot(*live)
	}
	if s.deps.SessionsBaseDir == "" {
		return Snapshot{}, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
	}
	store, err := session.OpenSession(s.deps.SessionsBaseDir, id)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) || errors.Is(err, fs.ErrNotExist) {
			return Snapshot{}, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
		}
		return Snapshot{}, fmt.Errorf("services: snapshot session %q: %w", id, err)
	}
	state, err := loadArchivedDAG(store.SessionDir())
	if err != nil {
		return Snapshot{}, fmt.Errorf("services: snapshot session %q: %w", id, err)
	}
	return Snapshot{
		Session: sessionMetaFrom(*store.Session()),
		DAG:     state,
	}, nil
}

// Plan reads <sessionDir>/plan.json for the session id and returns the
// Director's persisted plan. The structural fields (phases, tasks,
// acceptance) are stable after the planner emits; status fields on
// each task track the disk write at the moment the loop persisted the
// plan, which is "pending" for every task right after planning. Live
// status changes flow over the events stream; consumers that need the
// running DAG snapshot still go through Snapshot. LiveSessionAlias
// resolves to the bound live session, then to LiveAliasArchivedID for
// dev mode; ErrSessionNotFound covers unknown ids and ErrPlanNotFound
// covers a session whose planner has not emitted yet.
func (s *SessionService) Plan(ctx context.Context, id string) (*supervision.Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, ErrInvalidRequest.WithMessage("session service: empty id")
	}
	if id == LiveSessionAlias {
		if live := s.liveSession(); live != nil {
			id = live.ID
		} else if s.deps.LiveAliasArchivedID != "" {
			id = s.deps.LiveAliasArchivedID
		} else {
			return nil, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
		}
	}
	if s.deps.SessionStore != nil {
		if live := s.deps.SessionStore.Session(); live != nil && live.ID == id {
			plan, err := s.deps.SessionStore.ReadPlan()
			return resolvePlanReadError(plan, err, id)
		}
	}
	if s.deps.SessionsBaseDir == "" {
		return nil, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
	}
	store, err := session.OpenSession(s.deps.SessionsBaseDir, id)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) || errors.Is(err, fs.ErrNotExist) {
			return nil, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
		}
		return nil, fmt.Errorf("services: plan session %q: %w", id, err)
	}
	plan, err := store.ReadPlan()
	return resolvePlanReadError(plan, err, id)
}

func resolvePlanReadError(plan *supervision.Plan, err error, id string) (*supervision.Plan, error) {
	if err == nil {
		return plan, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrPlanNotFound.WithDetails(map[string]any{"id": id})
	}
	return nil, fmt.Errorf("services: read plan %q: %w", id, err)
}

func (s *SessionService) liveSession() *session.Session {
	if s.deps.SessionStore == nil {
		return nil
	}
	return s.deps.SessionStore.Session()
}

func (s *SessionService) liveSnapshot(live session.Session) (Snapshot, error) {
	if s.deps.DAGHandler == nil {
		return Snapshot{
			Session: sessionMetaFrom(live),
		}, nil
	}
	state := s.deps.DAGHandler.State()
	var snap *dag.State
	if state != nil {
		snap = state.Snapshot()
	}
	return Snapshot{
		Session: sessionMetaFrom(live),
		DAG:     snap,
	}, nil
}

// loadArchivedDAG reads the persisted dag.json under sessionDir and
// returns the reconciled in-memory state. A missing file returns nil
// state with a nil error so a session whose plan was never emitted
// is still snapshottable.
func loadArchivedDAG(sessionDir string) (*dag.State, error) {
	path := filepath.Join(sessionDir, "dag.json")
	state, err := dag.LoadStateFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return state, nil
}

// sessionMetaFrom projects session.Session to SessionMeta, mapping
// the lifecycle status to the wire form (a plain string) and using
// CreatedAt as StartedAt. FinishedAt is non-zero only when the
// session reached a terminal status; otherwise the field is left
// zero so consumers know the run is still in flight.
func sessionMetaFrom(sess session.Session) SessionMeta {
	meta := SessionMeta{
		ID:             sess.ID,
		SpecPath:       sess.SpecPath,
		StartedAt:      sess.CreatedAt,
		Status:         string(sess.Status),
		IterationIndex: sess.IterationIndex,
		MaxIter:        sess.MaxIter,
	}
	if sess.Status != session.SessionRunning && !sess.UpdatedAt.IsZero() {
		meta.FinishedAt = sess.UpdatedAt
	}
	return meta
}
