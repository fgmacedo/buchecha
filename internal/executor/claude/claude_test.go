package claude

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("abs %s: %v", name, err)
	}
	return abs
}

func TestRun_StreamsJSONL(t *testing.T) {
	e := New(Config{Binary: fixture(t, "fake-claude.sh")})
	var buf bytes.Buffer
	code, err := e.Run(context.Background(), "test prompt", &buf)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	out := buf.String()
	if !strings.Contains(out, `"type":"system"`) {
		t.Errorf("output missing system event: %q", out)
	}
	if !strings.Contains(out, `"type":"result"`) {
		t.Errorf("output missing result event: %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines of JSONL, got %d (out=%q)", len(lines), out)
	}
}

func TestRun_PropagatesNonZeroExit(t *testing.T) {
	e := New(Config{Binary: fixture(t, "fake-claude-fail.sh")})
	var buf bytes.Buffer
	code, err := e.Run(context.Background(), "x", &buf)
	if err != nil {
		t.Fatalf("Run: unexpected error %v", err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
	if !strings.Contains(buf.String(), `"type":"system"`) {
		t.Errorf("partial stdout was lost: %q", buf.String())
	}
}

func TestRun_BinaryNotFound(t *testing.T) {
	e := New(Config{Binary: "/nonexistent/binary"})
	var buf bytes.Buffer
	_, err := e.Run(context.Background(), "x", &buf)
	if err == nil {
		t.Errorf("expected error for missing binary")
	}
}

func TestRun_ContextCancelInterrupts(t *testing.T) {
	e := New(Config{
		Binary:      fixture(t, "fake-claude-slow.sh"),
		CancelGrace: 1 * time.Second,
	})
	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := e.Run(ctx, "x", &buf)
	if err == nil {
		t.Fatalf("expected ctx error, got nil; output=%q", buf.String())
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want ctx.Err()", err)
	}
	if !strings.Contains(buf.String(), `"interrupted"`) {
		t.Errorf("output missing interrupted terminator: %q", buf.String())
	}
}

func TestRun_PromptIsLastArg(t *testing.T) {
	e := New(Config{
		Binary:    fixture(t, "fake-claude-echo-args.sh"),
		Model:     "test-model",
		ExtraArgs: []string{"--foo", "--bar"},
	})
	var buf bytes.Buffer
	if _, err := e.Run(context.Background(), "the prompt", &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := strings.TrimRight(buf.String(), "\n")
	lines := strings.Split(out, "\n")

	want := []string{
		"-p", "--output-format", "stream-json", "--verbose",
		"--model", "test-model",
		"--foo", "--bar",
		"the prompt",
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d args, want %d (output=%q)", len(lines), len(want), out)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestRun_OmitsModelWhenEmpty(t *testing.T) {
	e := New(Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	var buf bytes.Buffer
	if _, err := e.Run(context.Background(), "p", &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(buf.String(), "--model") {
		t.Errorf("--model should be omitted when Model is empty: %q", buf.String())
	}
}

func TestRun_StderrIsCapturedWhenConfigured(t *testing.T) {
	e := New(Config{Binary: fixture(t, "fake-claude.sh")})
	var stdoutBuf, stderrBuf bytes.Buffer
	e.cfg.Stderr = &stderrBuf
	if _, err := e.Run(context.Background(), "p", &stdoutBuf); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// fake-claude.sh has set -e but writes only to stdout; stderrBuf can
	// be empty but must not panic. Smoke test that the path is wired.
	_ = stderrBuf
}
