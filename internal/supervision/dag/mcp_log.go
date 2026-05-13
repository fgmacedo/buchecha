package dag

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

// MCPLogEntry is one line in mcp-log.jsonl: a structured record of a
// single MCP call after agent identity has been resolved. Role and
// AgentID describe the caller; Method and Input describe the call;
// Result and Err describe the dispatch outcome (Err is empty on
// success).
type MCPLogEntry struct {
	At      time.Time      `json:"at"`
	Role    string         `json:"role"`
	AgentID string         `json:"agent_id"`
	Method  string         `json:"method"`
	Input   map[string]any `json:"input,omitempty"`
	Result  string         `json:"result,omitempty"`
	Err     string         `json:"error,omitempty"`
}

// MCPLog is the append-only writer the handler appends to after every
// successful or rejected MCP call. The file is opened lazily and held
// open for the run; writes are mutex-serialized so concurrent dispatch
// cannot interleave records mid-line.
type MCPLog struct {
	path string
	mu   sync.Mutex
	w    io.WriteCloser
}

// NewMCPLog returns an MCPLog rooted at path. The path's parent
// directory is created on first write. nil path returns nil so the
// handler treats logging as disabled without a special branch.
func NewMCPLog(path string) *MCPLog {
	if path == "" {
		return nil
	}
	return &MCPLog{path: path}
}

// Append writes one entry as a single JSON line. Returns an error if
// serialization or I/O fails; callers may log and continue, since the
// MCP log is informational and a write failure should not abort an
// otherwise valid MCP call.
func (a *MCPLog) Append(entry MCPLogEntry) error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.w == nil {
		if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
			return fmt.Errorf("dag mcp-log: mkdir %s: %w", filepath.Dir(a.path), err)
		}
		f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("dag mcp-log: open %s: %w", a.path, err)
		}
		a.w = f
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("dag mcp-log: marshal: %w", err)
	}
	line = append(line, '\n')
	if _, err := a.w.Write(line); err != nil {
		return fmt.Errorf("dag mcp-log: write %s: %w", a.path, err)
	}
	return nil
}

// AppendConnect records an HTTP-level MCP call: initialize, tools/list,
// tools/call, and any other method the transport layer dispatches before
// reaching the DAG handler. AgentID is omitted because the transport
// layer resolves only the role (X-BCC-Role header), not the registered
// agent_id. This complements the tool-level entries written by Append
// so the audit trail covers both the wire handshake and the actual
// method calls.
func (a *MCPLog) AppendConnect(role, method string) error {
	return a.Append(MCPLogEntry{
		At:     time.Now().UTC(),
		Role:   role,
		Method: method,
	})
}

// Close releases the MCPLog file handle. Idempotent. After Close,
// further Append calls will reopen the file lazily; the handler
// typically calls Close once on run shutdown.
func (a *MCPLog) Close() error {
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
