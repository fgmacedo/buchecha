package director

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// StatsEntry is one line in stats.jsonl: telemetry for a single agent
// spawn (Planner, Briefer, Executor, or Reviewer). It is persisted so
// post-hoc analysis can attribute cost and tokens to a role and a
// particular phase iteration without scraping logs. Fields are
// optional when they do not apply: Planner spawns leave PhaseID and
// IterationID empty, and Briefer/Reviewer spawns leave Attempt zero
// (the executor retry index is meaningful only for Executor entries).
type StatsEntry struct {
	At           time.Time `json:"at"`
	Role         string    `json:"role"`
	PhaseID      string    `json:"phase_id,omitempty"`
	IterationID  string    `json:"iteration_id,omitempty"`
	Attempt      int       `json:"attempt,omitempty"`
	DurationMS   int64     `json:"duration_ms"`
	CostUSD      float64   `json:"cost_usd"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
}

// StatsLog is the append-only writer the loop appends to after every
// agent spawn returns its DirectorCallStats (or, for the Executor, the
// final ResultSummary on the agent event stream). The file is opened
// lazily and held open for the run; writes are mutex-serialized so
// concurrent goroutines cannot interleave records mid-line. Mirrors
// the AuditLog shape on purpose; the two are separate writers because
// stats are spawn telemetry, not MCP dispatch records.
type StatsLog struct {
	path string
	mu   sync.Mutex
	w    io.WriteCloser
}

// NewStatsLog returns a StatsLog rooted at path. Empty path returns
// nil so callers treat persistence as disabled without a special
// branch (the Append method also treats a nil receiver as a no-op).
func NewStatsLog(path string) *StatsLog {
	if path == "" {
		return nil
	}
	return &StatsLog{path: path}
}

// Append writes one entry as a single JSON line. A nil receiver and a
// zero-valued entry are both no-ops; otherwise serialization or I/O
// failures are returned. Callers may log and continue, since stats
// are informational and a write failure should not abort the run.
func (s *StatsLog) Append(entry StatsEntry) error {
	if s == nil {
		return nil
	}
	if entry.Role == "" {
		return errors.New("director stats: entry missing role")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
			return fmt.Errorf("director stats: mkdir %s: %w", filepath.Dir(s.path), err)
		}
		f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("director stats: open %s: %w", s.path, err)
		}
		s.w = f
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("director stats: marshal: %w", err)
	}
	line = append(line, '\n')
	if _, err := s.w.Write(line); err != nil {
		return fmt.Errorf("director stats: write %s: %w", s.path, err)
	}
	return nil
}

// Close releases the file handle. Idempotent. After Close, further
// Append calls reopen lazily.
func (s *StatsLog) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return nil
	}
	err := s.w.Close()
	s.w = nil
	if err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return nil
}
