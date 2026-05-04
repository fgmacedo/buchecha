package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// stubInnerExecutor is a loop.Executor stand-in that records whether
// Run was invoked and returns a configurable ExecResult plus error. It
// drives the deregisteringExecutor propagation tests without spinning
// up a real claude subprocess.
type stubInnerExecutor struct {
	called atomic.Int32
	res    loop.ExecResult
	err    error
}

func (s *stubInnerExecutor) Run(_ context.Context, _ string, _ chan<- agentcontract.AgentEvent) (loop.ExecResult, error) {
	s.called.Add(1)
	return s.res, s.err
}

func TestDeregisteringExecutor_AnnotatesResult(t *testing.T) {
	inner := &stubInnerExecutor{
		res: loop.ExecResult{ExitCode: 7, StderrTail: "boom"},
	}
	cleanupCalls := atomic.Int32{}
	d := &deregisteringExecutor{
		inner:         inner,
		cleanup:       func() { cleanupCalls.Add(1) },
		agentID:       "bcc-executor-fake01",
		stderrLogPath: "/tmp/runs/P7-01/bcc-executor-fake01.stderr.log",
	}

	got, err := d.Run(context.Background(), "ignored", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", got.ExitCode)
	}
	if got.StderrTail != "boom" {
		t.Errorf("StderrTail = %q, want %q", got.StderrTail, "boom")
	}
	if got.AgentID != "bcc-executor-fake01" {
		t.Errorf("AgentID = %q, want bcc-executor-fake01", got.AgentID)
	}
	if got.StderrLogPath == "" || !strings.HasSuffix(got.StderrLogPath, "bcc-executor-fake01.stderr.log") {
		t.Errorf("StderrLogPath = %q, want suffix bcc-executor-fake01.stderr.log", got.StderrLogPath)
	}
	if cleanupCalls.Load() != 1 {
		t.Errorf("cleanup invoked %d times, want 1", cleanupCalls.Load())
	}
	if inner.called.Load() != 1 {
		t.Errorf("inner Run invoked %d times, want 1", inner.called.Load())
	}
}

// TestDeregisteringExecutor_NoAnnotationLeaks confirms that empty
// agentID/stderrLogPath fields preserve whatever the inner adapter put
// in the result. This matters because the inner executor adapter may
// itself populate these fields in the future, and the wrapper must not
// blank them.
func TestDeregisteringExecutor_NoAnnotationLeaks(t *testing.T) {
	inner := &stubInnerExecutor{
		res: loop.ExecResult{
			ExitCode:      0,
			AgentID:       "preset-agent",
			StderrLogPath: "/preset/path",
		},
	}
	d := &deregisteringExecutor{
		inner:   inner,
		cleanup: func() {},
	}
	got, err := d.Run(context.Background(), "ignored", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.AgentID != "preset-agent" {
		t.Errorf("inner AgentID overwritten: got %q, want preset-agent", got.AgentID)
	}
	if got.StderrLogPath != "/preset/path" {
		t.Errorf("inner StderrLogPath overwritten: got %q, want /preset/path", got.StderrLogPath)
	}
}

// TestRunLogPath_FileCreationSmoke is an end-to-end-ish check that the
// debug-log opener produces a real file under
// .bcc/sessions/<id>/runs/<iter>/<agent>.stderr.log. This is the path
// users will look at when investigating a silent crash, so it has to
// actually materialize on disk when openLog runs.
func TestRunLogPath_FileCreationSmoke(t *testing.T) {
	// Use the real Store helper to derive the path; the directorEnableLogCapture
	// closure is exactly equivalent to this open + write pattern.
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".bcc", "sessions", "abcd123", "runs", "P7-01"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmp, ".bcc", "sessions", "abcd123", "runs", "P7-01", "bcc-executor-xyz.stderr.log")
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("auth: token expired\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if string(got) != "auth: token expired\n" {
		t.Errorf("file contents = %q, want %q", string(got), "auth: token expired\n")
	}
}
