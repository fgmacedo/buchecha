package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// rawLog wraps the per-iteration raw stream-json log file. The adapter
// opens it from BCC_JSONL_PATH at the start of Run, writes one line per
// event as the parser reads it, optionally appends an interrupted
// terminator on cancellation, and closes the file on return.
//
// All methods are safe to call on a nil receiver. When BCC_JSONL_PATH
// is empty, openRawLog returns (nil, nil) and the adapter degrades to
// "no audit log".
type rawLog struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

// openRawLog creates the file at path (and any missing parent
// directories) and returns a rawLog handle. Empty path returns
// (nil, nil); the adapter treats the absent log as opt-out from
// persistence.
func openRawLog(path string) (*rawLog, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create log %s: %w", path, err)
	}
	return &rawLog{f: f, path: path}, nil
}

// writeLine appends line followed by a terminating newline. Best-effort:
// write errors are ignored because the audit log is not critical to loop
// progress and the file may already be closed (e.g., after an
// interrupt).
func (r *rawLog) writeLine(line []byte) {
	if r == nil || r.f == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.f.Write(line)
	_, _ = r.f.Write([]byte{'\n'})
}

// writeInterruptedTerminator appends a synthetic event marking abnormal
// end-of-stream so downstream parsers can distinguish a canceled
// iteration from a clean one.
func (r *rawLog) writeInterruptedTerminator() {
	if r == nil || r.f == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.f.Write([]byte(`{"type":"interrupted"}` + "\n"))
}

func (r *rawLog) close() error {
	if r == nil || r.f == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

// Path returns the log path or "" when no log was opened.
func (r *rawLog) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}
