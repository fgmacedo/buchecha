package director

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"
)

// Session is the per-run record persisted under
// .bcc/sessions/<id>/manifest.json. Every state artifact the Director
// writes during a run, plan, briefings, DAG snapshots, MCP audit log,
// is rooted at the session directory; sessions therefore replace the
// global .bcc/ layout and let two runs against different specs coexist
// without overwriting each other.
type Session struct {
	ID        string        `json:"id"`
	SpecPath  string        `json:"spec_path"`
	SpecHash  string        `json:"spec_hash"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Status    SessionStatus `json:"status"`
}

// SessionStatus tracks the lifecycle of a Session. The closed set is
// enforced at JSON boundaries so the on-disk manifest cannot drift to
// values the loop does not know how to handle on resume.
type SessionStatus string

const (
	SessionRunning          SessionStatus = "running"
	SessionDone             SessionStatus = "done"
	SessionAborted          SessionStatus = "aborted"
	SessionEscalatedPending SessionStatus = "escalated_pending"
)

func (s SessionStatus) valid() bool {
	switch s {
	case SessionRunning, SessionDone, SessionAborted, SessionEscalatedPending:
		return true
	}
	return false
}

// MarshalJSON refuses the zero value so a half-initialised manifest is
// rejected at the boundary instead of silently losing the status field.
func (s SessionStatus) MarshalJSON() ([]byte, error) {
	if !s.valid() {
		return nil, fmt.Errorf("director: invalid session status %q", string(s))
	}
	return json.Marshal(string(s))
}

// UnmarshalJSON enforces the closed set so a manifest from an unknown
// version of bcc surfaces as a parse error rather than as a session
// in an undefined state.
func (s *SessionStatus) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("director: parse session status: %w", err)
	}
	candidate := SessionStatus(raw)
	if !candidate.valid() {
		return fmt.Errorf("director: invalid session status %q", raw)
	}
	*s = candidate
	return nil
}

// NewSessionID derives a 12-hex-char id from the spec path, the current
// time, and 16 fresh random bytes drawn from randSource. The randomness
// is what keeps two consecutive runs from colliding even when both
// arrive in the same nanosecond against the same spec; injecting the
// reader makes it deterministic in tests.
func NewSessionID(specPath string, now time.Time, randSource io.Reader) string {
	if randSource == nil {
		randSource = rand.Reader
	}
	var entropy [16]byte
	if _, err := io.ReadFull(randSource, entropy[:]); err != nil {
		panic(fmt.Errorf("director: read session entropy: %w", err))
	}
	h := sha256.New()
	h.Write([]byte(specPath))
	h.Write([]byte{0})
	var nanos [8]byte
	binary.BigEndian.PutUint64(nanos[:], uint64(now.UnixNano()))
	h.Write(nanos[:])
	h.Write([]byte{0})
	h.Write(entropy[:])
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// validSessionID returns true when id matches the 12-lowercase-hex
// shape NewSessionID produces. Used by ResolveSession to fail fast on
// obvious typos before the filesystem lookup.
func validSessionID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	_, err := strconv.ParseUint(id, 16, 64)
	return err == nil
}
