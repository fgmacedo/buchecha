package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntry is one line in audit.ndjson: a structured record of one
// service-level action. Timestamp is filled in by Record when zero;
// every other field is supplied by the caller. Result encodes either
// the success indicator (Code empty, ResultCode "success") or an
// error code drawn from the canonical Error enum.
type AuditEntry struct {
	At     time.Time   `json:"at"`
	Actor  AuditActor  `json:"actor"`
	Method string      `json:"method"`
	Target AuditTarget `json:"target,omitzero"`
	Result AuditResult `json:"result"`
}

// AuditActor identifies the caller. Role is the canonical role name
// for agent-driven calls (planner, briefer, executor, reviewer) or
// "user" for human-driven calls; AgentID is non-empty only when an
// agent issued the call.
type AuditActor struct {
	Role    string `json:"role"`
	AgentID string `json:"agent_id,omitempty"`
}

// AuditTarget names the session, phase, and task the call addressed.
// Empty fields are omitted from the wire form; per-method records
// populate only the fields they care about.
type AuditTarget struct {
	SessionID string `json:"session_id,omitempty"`
	PhaseID   string `json:"phase_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
}

// AuditResult records the outcome. Status is "success" or "error";
// Code, when non-empty on errors, carries the canonical ErrorCode
// (the same closed enum protocol adapters key off).
type AuditResult struct {
	Status string    `json:"status"`
	Code   ErrorCode `json:"code,omitempty"`
}

// AuditStatusSuccess and AuditStatusError are the two values
// AuditResult.Status takes. The closed set keeps log analyzers from
// guessing.
const (
	AuditStatusSuccess = "success"
	AuditStatusError   = "error"
)

// Audit writes audit entries to a single ndjson file under the
// session directory. V1 read-only services do not call Record; the
// service is staged so V2+ mutating endpoints have a stable home.
//
// The struct is goroutine-safe: every Record call is mutex-serialized
// so concurrent writers never interleave records mid-line. The file
// handle is opened lazily on the first successful write so a Services
// constructed without an AuditPath never touches disk.
type Audit struct {
	path   string
	logger *slog.Logger

	mu sync.Mutex
	w  io.WriteCloser
}

func newAudit(deps Deps) *Audit {
	return &Audit{
		path:   deps.AuditPath,
		logger: slog.Default(),
	}
}

// SetLogger replaces the slog logger Audit emits structured Info
// entries to. Tests inject a fixed logger so log assertions are not
// flaky; production wiring leaves the default.
func (a *Audit) SetLogger(l *slog.Logger) {
	if l == nil {
		l = slog.Default()
	}
	a.logger = l
}

// Path returns the audit file path. Empty when audit logging is
// disabled.
func (a *Audit) Path() string { return a.path }

// Record appends one entry to audit.ndjson and emits a structured
// slog Info line carrying the same fields. Empty AuditPath disables
// the file write but the slog mirror still fires so the audit is
// never silently dropped. A nil receiver is a no-op so callers that
// stash a service handle without checking Audit can call through
// freely.
func (a *Audit) Record(ctx context.Context, entry AuditEntry) error {
	if a == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	if entry.Result.Status == "" {
		if entry.Result.Code != "" {
			entry.Result.Status = AuditStatusError
		} else {
			entry.Result.Status = AuditStatusSuccess
		}
	}
	a.logSlog(entry)
	if a.path == "" {
		return nil
	}
	return a.appendFile(entry)
}

// logSlog mirrors the entry into the structured logger so tail -f on
// stderr shows the audit trail in real time. The values are
// non-secret by construction (ids, methods, codes); never log
// arbitrary user input here.
func (a *Audit) logSlog(entry AuditEntry) {
	logger := a.logger
	if logger == nil {
		logger = slog.Default()
	}
	attrs := []slog.Attr{
		slog.String("actor_role", entry.Actor.Role),
		slog.String("method", entry.Method),
		slog.String("status", entry.Result.Status),
	}
	if entry.Actor.AgentID != "" {
		attrs = append(attrs, slog.String("actor_agent_id", entry.Actor.AgentID))
	}
	if entry.Target.SessionID != "" {
		attrs = append(attrs, slog.String("session_id", entry.Target.SessionID))
	}
	if entry.Target.PhaseID != "" {
		attrs = append(attrs, slog.String("phase_id", entry.Target.PhaseID))
	}
	if entry.Target.TaskID != "" {
		attrs = append(attrs, slog.String("task_id", entry.Target.TaskID))
	}
	if entry.Result.Code != "" {
		attrs = append(attrs, slog.String("error_code", string(entry.Result.Code)))
	}
	logger.LogAttrs(context.Background(), slog.LevelInfo, "services audit", attrs...)
}

// appendFile writes entry as a JSON line to AuditPath. The file is
// opened lazily on the first successful write and held open for the
// process. Concurrent Record calls serialize on a.mu so no two
// records interleave mid-line.
func (a *Audit) appendFile(entry AuditEntry) error {
	body, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("services audit: marshal: %w", err)
	}
	body = append(body, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.w == nil {
		dir := filepath.Dir(a.path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("services audit: mkdir %s: %w", dir, err)
		}
		f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("services audit: open %s: %w", a.path, err)
		}
		a.w = f
	}
	if _, err := a.w.Write(body); err != nil {
		return fmt.Errorf("services audit: write %s: %w", a.path, err)
	}
	return nil
}

// Close releases the audit file handle. Idempotent. After Close,
// further Record calls reopen the file lazily.
func (a *Audit) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.w == nil {
		return nil
	}
	err := a.w.Close()
	a.w = nil
	if err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return nil
}
