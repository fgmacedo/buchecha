package session

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fgmacedo/buchecha/internal/supervision"
)

// Store persists the Director's per-run artifacts (manifest, plan,
// briefings, verdicts) under a single session directory rooted at
// .bcc/sessions/<id>/. All reads return errors that wrap fs.ErrNotExist
// when the artifact has never been written, so callers can branch with
// errors.Is.
type Store struct {
	sessionDir string
	session    *Session
}

// SessionDir returns the directory the Store reads and writes under.
func (s *Store) SessionDir() string { return s.sessionDir }

// SpawnsDir returns the path <sessionDir>/spawns where per-spawn prompt
// files are written. The directory is NOT created here; the first writer
// must call os.MkdirAll(SpawnsDir(), 0o755) before writing.
func (s *Store) SpawnsDir() string {
	return filepath.Join(s.sessionDir, "spawns")
}

// Session returns a pointer to the Session manifest the Store was
// opened or created with. Callers must not mutate the returned struct;
// use Touch to update the persisted status.
func (s *Store) Session() *Session { return s.session }

const (
	manifestFile = "manifest.json"
	briefingsDir = "briefings"
	sessionsDir  = "sessions"
	planFile     = "plan.json"
	runsDir      = "runs"
	// PlannerRunsBucket is the sentinel iteration bucket for planner
	// spawns, which run outside any iteration. Used as the directory name
	// under runs/ so planner logs sit next to per-iteration logs without
	// colliding with a real iteration_id.
	PlannerRunsBucket = "_planner"
)

// SessionsRoot returns the parent directory that holds all session
// directories under baseDir, conventionally `.bcc/sessions/`. Tests and
// session helpers use it to walk and to construct child session paths.
func SessionsRoot(baseDir string) string {
	return filepath.Join(baseDir, sessionsDir)
}

// SessionDirFor returns the canonical filesystem path for a session id
// rooted under baseDir.
func SessionDirFor(baseDir, sessionID string) string {
	return filepath.Join(SessionsRoot(baseDir), sessionID)
}

// CreateSession allocates a fresh session id, writes the manifest under
// .bcc/sessions/<id>/, and returns a Store rooted at that directory.
// Refuses to overwrite an existing manifest, the freshly generated id
// would have to collide with one already on disk for that to happen,
// which is a sign of a corrupted entropy source rather than a normal
// race condition. The caller passes the absolute spec path and the
// already-computed spec hash so the manifest is self-describing without
// re-reading the spec file.
func CreateSession(baseDir, specPath, specHash string, now time.Time) (*Store, *Session, error) {
	if specPath == "" {
		return nil, nil, errors.New("director: create session: empty spec_path")
	}
	if specHash == "" {
		return nil, nil, errors.New("director: create session: empty spec_hash")
	}
	if now.IsZero() {
		now = time.Now()
	}
	id := NewSessionID(specPath, now, rand.Reader)
	dir := SessionDirFor(baseDir, id)
	if _, err := os.Stat(filepath.Join(dir, manifestFile)); err == nil {
		return nil, nil, fmt.Errorf("director: session %q already exists at %s", id, dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("director: mkdir %s: %w", dir, err)
	}
	sess := &Session{
		ID:        id,
		SpecPath:  specPath,
		SpecHash:  specHash,
		CreatedAt: now,
		UpdatedAt: now,
		Status:    SessionRunning,
	}
	if err := writeJSON(filepath.Join(dir, manifestFile), sess); err != nil {
		return nil, nil, err
	}
	return &Store{sessionDir: dir, session: sess}, sess, nil
}

// OpenSession reads an existing session manifest and returns a Store
// rooted at .bcc/sessions/<id>/. Returns ErrSessionNotFound (wrapping
// fs.ErrNotExist) when the directory or manifest is missing.
func OpenSession(baseDir, sessionID string) (*Store, error) {
	if sessionID == "" {
		return nil, errors.New("director: open session: empty id")
	}
	if !validSessionID(sessionID) {
		return nil, fmt.Errorf("director: open session: invalid id %q (want 12 hex chars)", sessionID)
	}
	dir := SessionDirFor(baseDir, sessionID)
	manifest := filepath.Join(dir, manifestFile)
	var sess Session
	if err := readJSON(manifest, &sess); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("director: session %q: %w", sessionID, ErrSessionNotFound)
		}
		return nil, err
	}
	if sess.ID != sessionID {
		return nil, fmt.Errorf("director: session %q manifest declares id %q", sessionID, sess.ID)
	}
	return &Store{sessionDir: dir, session: &sess}, nil
}

// Touch updates the session's Status and UpdatedAt and rewrites the
// manifest. The caller passes the new status; transitions are recorded
// for whatever observer (TUI, sessions list) is reading the manifest.
func (s *Store) Touch(status SessionStatus, now time.Time) error {
	if s.session == nil {
		return errors.New("director: touch session: nil session")
	}
	if !status.valid() {
		return fmt.Errorf("director: touch session: invalid status %q", string(status))
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.session.Status = status
	s.session.UpdatedAt = now
	return writeJSON(filepath.Join(s.sessionDir, manifestFile), s.session)
}

// SetIteration updates the session's IterationIndex and MaxIter and
// rewrites the manifest. Mirrors the Touch pattern.
func (s *Store) SetIteration(index, maxIter int, now time.Time) error {
	if s.session == nil {
		return errors.New("director: set iteration: nil session")
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.session.IterationIndex = index
	s.session.MaxIter = maxIter
	s.session.UpdatedAt = now
	return writeJSON(filepath.Join(s.sessionDir, manifestFile), s.session)
}

func (s *Store) planPath() string {
	return filepath.Join(s.sessionDir, planFile)
}

func (s *Store) briefingPath(iterationID string) string {
	return filepath.Join(s.sessionDir, briefingsDir, iterationID+".json")
}

// WritePlan serializes the Plan to <sessionDir>/plan.json.
func (s *Store) WritePlan(p *supervision.Plan) error {
	if p == nil {
		return errors.New("director: nil plan")
	}
	return writeJSON(s.planPath(), p)
}

// ReadPlan reads <sessionDir>/plan.json. Returns an error wrapping
// fs.ErrNotExist when no plan has been written.
func (s *Store) ReadPlan() (*supervision.Plan, error) {
	var p supervision.Plan
	if err := readJSON(s.planPath(), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// WriteBriefing serializes the Briefing under <sessionDir>/briefings/.
// IterationID and PhaseID on the Briefing must be set; the file name
// is derived from IterationID.
func (s *Store) WriteBriefing(b *supervision.Briefing) error {
	if b == nil {
		return errors.New("director: nil briefing")
	}
	if b.PhaseID == "" {
		return errors.New("director: briefing has empty phase_id")
	}
	if b.IterationID == "" {
		return errors.New("director: briefing has empty iteration_id")
	}
	return writeJSON(s.briefingPath(b.IterationID), b)
}

// RunLogPath returns the absolute path for a per-spawn capture file
// under <sessionDir>/runs/<bucket>/<agentID>.<kind>. bucket is the
// iterationID for in-iteration spawns (briefer, executor, reviewer) or
// PlannerRunsBucket for planner spawns. kind selects the artifact type
// (typically "stderr.log" or "stdout.jsonl"). The parent directory is
// created on demand. The function does not open the file; callers
// own the file handle and its lifetime.
func (s *Store) RunLogPath(bucket, agentID, kind string) (string, error) {
	if bucket == "" {
		bucket = PlannerRunsBucket
	}
	if agentID == "" {
		return "", errors.New("director: RunLogPath: empty agent_id")
	}
	if kind == "" {
		return "", errors.New("director: RunLogPath: empty kind")
	}
	dir := filepath.Join(s.sessionDir, runsDir, bucket)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("director: mkdir %s: %w", dir, err)
	}
	return filepath.Join(dir, agentID+"."+kind), nil
}

// ReadBriefing reads the briefing for an iteration id. Returns an
// error wrapping fs.ErrNotExist when missing.
func (s *Store) ReadBriefing(iterationID string) (*supervision.Briefing, error) {
	var b supervision.Briefing
	if err := readJSON(s.briefingPath(iterationID), &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("director: mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("director: marshal %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("director: write %s: %w", path, err)
	}
	return nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("director: read %s: %w", path, fs.ErrNotExist)
		}
		return fmt.Errorf("director: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("director: parse %s: %w", path, err)
	}
	return nil
}
