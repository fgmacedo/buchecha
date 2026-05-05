package services

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/fgmacedo/buchecha/internal/director"
)

// Briefing is the rendered briefing markdown the BriefingService
// returns. It is a thin wrapper around the on-disk content so adapters
// can attach metadata (Content-Type, ETag) without re-reading the
// file.
type Briefing struct {
	SessionID   string `json:"session_id"`
	PhaseID     string `json:"phase_id"`
	Attempt     int    `json:"attempt"`
	IterationID string `json:"iteration_id"`
	Markdown    string `json:"markdown"`
}

// BriefingService reads rendered briefings under
// .bcc/sessions/<id>/runs/<iteration>/briefing.md. The service does
// not parse the markdown; the file shape is whatever the Briefer
// agent and the loop materialize at run time.
type BriefingService struct {
	deps Deps
}

func newBriefingService(deps Deps) *BriefingService {
	return &BriefingService{deps: deps}
}

// Get returns the Briefing for (sessionID, phaseID, attempt). The
// iteration directory name is derived from the persisted briefing
// JSON for the matching phase+attempt. Misses map to ErrPhaseNotFound
// (no briefing recorded against the phase) or ErrAttemptNotFound (a
// briefing exists for the phase but not at the requested attempt).
func (s *BriefingService) Get(ctx context.Context, sessionID, phaseID string, attempt int) (Briefing, error) {
	if err := ctx.Err(); err != nil {
		return Briefing{}, err
	}
	if sessionID == "" {
		return Briefing{}, ErrInvalidRequest.WithMessage("briefing service: empty session_id")
	}
	if phaseID == "" {
		return Briefing{}, ErrInvalidRequest.WithMessage("briefing service: empty phase_id")
	}
	if attempt < 1 {
		return Briefing{}, ErrInvalidRequest.WithMessage("briefing service: attempt must be >= 1")
	}
	sessionDir, err := s.sessionDir(sessionID)
	if err != nil {
		return Briefing{}, err
	}
	matches, err := briefingsForPhase(sessionDir, phaseID)
	if err != nil {
		return Briefing{}, fmt.Errorf("services: list briefings: %w", err)
	}
	if len(matches) == 0 {
		return Briefing{}, ErrPhaseNotFound.WithDetails(map[string]any{
			"session_id": sessionID,
			"phase_id":   phaseID,
		})
	}
	if attempt > len(matches) {
		return Briefing{}, ErrAttemptNotFound.WithDetails(map[string]any{
			"session_id":     sessionID,
			"phase_id":       phaseID,
			"attempt":        attempt,
			"max_attempt":    len(matches),
			"recorded_count": len(matches),
		})
	}
	chosen := matches[attempt-1]
	body, err := os.ReadFile(filepath.Join(sessionDir, "runs", chosen.IterationID, "briefing.md"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Briefing{}, ErrAttemptNotFound.WithDetails(map[string]any{
				"session_id":   sessionID,
				"phase_id":     phaseID,
				"attempt":      attempt,
				"iteration_id": chosen.IterationID,
			})
		}
		return Briefing{}, fmt.Errorf("services: read briefing.md: %w", err)
	}
	return Briefing{
		SessionID:   sessionID,
		PhaseID:     phaseID,
		Attempt:     attempt,
		IterationID: chosen.IterationID,
		Markdown:    string(body),
	}, nil
}

// sessionDir returns the absolute session directory for sessionID,
// preferring the live SessionStore when its manifest matches and
// falling back to OpenSession otherwise. Unknown ids return
// ErrSessionNotFound.
func (s *BriefingService) sessionDir(sessionID string) (string, error) {
	if s.deps.SessionStore != nil {
		if live := s.deps.SessionStore.Session(); live != nil && live.ID == sessionID {
			return s.deps.SessionStore.SessionDir(), nil
		}
	}
	if s.deps.SessionsBaseDir == "" {
		return "", ErrSessionNotFound.WithDetails(map[string]any{"id": sessionID})
	}
	store, err := director.OpenSession(s.deps.SessionsBaseDir, sessionID)
	if err != nil {
		if errors.Is(err, director.ErrSessionNotFound) || errors.Is(err, fs.ErrNotExist) {
			return "", ErrSessionNotFound.WithDetails(map[string]any{"id": sessionID})
		}
		return "", fmt.Errorf("services: open session %q: %w", sessionID, err)
	}
	return store.SessionDir(), nil
}

// briefingMatch is one record on disk: the iteration_id (used as the
// filename and the runs/ subdirectory) and the order in which the
// briefing was emitted. The slice returned by briefingsForPhase is
// ordered ascending by emit time so attempt N reads as matches[N-1].
type briefingMatch struct {
	IterationID string
}

// briefingsForPhase scans <sessionDir>/briefings/*.json and returns
// every briefing whose phase_id matches phaseID, ordered by file
// mtime ascending. The Briefer emits briefings monotonically per
// phase; the filesystem mtime is therefore a reliable proxy for the
// attempt index, and the canonical iteration_id format
// "<phase_id>-<unix_ns>" lets us sort lexicographically as a backup.
func briefingsForPhase(sessionDir, phaseID string) ([]briefingMatch, error) {
	briefingsDir := filepath.Join(sessionDir, "briefings")
	entries, err := os.ReadDir(briefingsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var found []briefingSortable
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(briefingsDir, e.Name())
		var brief struct {
			IterationID string `json:"iteration_id"`
			PhaseID     string `json:"phase_id"`
		}
		if err := readJSONFile(path, &brief); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if brief.PhaseID != phaseID {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		found = append(found, briefingSortable{
			match:     briefingMatch{IterationID: brief.IterationID},
			mtime:     info.ModTime().UnixNano(),
			iteration: brief.IterationID,
		})
	}
	// Stable order: mtime ascending; ties broken by iteration_id so
	// fast successive emits in tests still produce a deterministic
	// sequence.
	sortBriefings(found)
	out := make([]briefingMatch, len(found))
	for i, f := range found {
		out[i] = f.match
	}
	return out, nil
}

// briefingSortable is the in-memory pairing of a briefingMatch with
// the sort key extracted from the filesystem.
type briefingSortable struct {
	match     briefingMatch
	mtime     int64
	iteration string
}
