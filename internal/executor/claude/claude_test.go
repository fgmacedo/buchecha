package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("abs %s: %v", name, err)
	}
	return abs
}

// withLogPath sets BCC_JSONL_PATH to a fresh file in t.TempDir() and
// returns the path. The env var is restored at test end via t.Setenv.
func withLogPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stream.jsonl")
	t.Setenv("BCC_JSONL_PATH", path)
	return path
}

func TestRun_StreamsJSONL(t *testing.T) {
	logPath := withLogPath(t)
	e := New(Config{Binary: fixture(t, "fake-claude.sh")})
	events := make(chan loop.AgentEvent, 4)
	res, err := e.Run(context.Background(), "test prompt", events)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", res.ExitCode)
	}
	if res.LogPath != logPath {
		t.Errorf("LogPath = %q, want %q", res.LogPath, logPath)
	}
	out, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(out), `"type":"system"`) {
		t.Errorf("log missing system event: %q", out)
	}
	if !strings.Contains(string(out), `"type":"result"`) {
		t.Errorf("log missing result event: %q", out)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines of JSONL, got %d (out=%q)", len(lines), out)
	}
}

func TestRun_PropagatesNonZeroExit(t *testing.T) {
	logPath := withLogPath(t)
	e := New(Config{Binary: fixture(t, "fake-claude-fail.sh")})
	events := make(chan loop.AgentEvent, 4)
	res, err := e.Run(context.Background(), "x", events)
	if err != nil {
		t.Fatalf("Run: unexpected error %v", err)
	}
	if res.ExitCode != 7 {
		t.Errorf("exit code = %d, want 7", res.ExitCode)
	}
	out, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(out), `"type":"system"`) {
		t.Errorf("partial stdout was lost: %q", out)
	}
}

func TestRun_BinaryNotFound(t *testing.T) {
	withLogPath(t)
	e := New(Config{Binary: "/nonexistent/binary"})
	events := make(chan loop.AgentEvent, 4)
	_, err := e.Run(context.Background(), "x", events)
	if err == nil {
		t.Errorf("expected error for missing binary")
	}
}

func TestRun_ContextCancelInterrupts(t *testing.T) {
	logPath := withLogPath(t)
	e := New(Config{
		Binary:      fixture(t, "fake-claude-slow.sh"),
		CancelGrace: 1 * time.Second,
	})
	events := make(chan loop.AgentEvent, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := e.Run(ctx, "x", events)
	if err == nil {
		t.Fatalf("expected ctx error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want ctx.Err()", err)
	}
	out, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(out), `"interrupted"`) {
		t.Errorf("log missing interrupted terminator: %q", out)
	}
}

func TestRun_PromptIsLastArg(t *testing.T) {
	logPath := withLogPath(t)
	e := New(Config{
		Binary:          fixture(t, "fake-claude-echo-args.sh"),
		Model:           "test-model",
		ExtraArgs:       []string{"--foo", "--bar"},
		SkipPermissions: true,
	})
	events := make(chan loop.AgentEvent, 4)
	if _, err := e.Run(context.Background(), "the prompt", events); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	trimmed := strings.TrimRight(string(out), "\n")
	lines := strings.Split(trimmed, "\n")

	want := []string{
		"-p", "--output-format", "stream-json", "--verbose",
		"--dangerously-skip-permissions",
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
	logPath := withLogPath(t)
	e := New(Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	events := make(chan loop.AgentEvent, 4)
	if _, err := e.Run(context.Background(), "p", events); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out, _ := os.ReadFile(logPath)
	if strings.Contains(string(out), "--model") {
		t.Errorf("--model should be omitted when Model is empty: %q", out)
	}
}

func TestRun_OmitsSkipPermissionsWhenFalse(t *testing.T) {
	logPath := withLogPath(t)
	e := New(Config{
		Binary:          fixture(t, "fake-claude-echo-args.sh"),
		SkipPermissions: false,
	})
	events := make(chan loop.AgentEvent, 4)
	if _, err := e.Run(context.Background(), "p", events); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out, _ := os.ReadFile(logPath)
	if strings.Contains(string(out), "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions should be omitted when SkipPermissions=false: %q", out)
	}
}

func TestRun_AddsSkipPermissionsWhenTrue(t *testing.T) {
	logPath := withLogPath(t)
	e := New(Config{
		Binary:          fixture(t, "fake-claude-echo-args.sh"),
		SkipPermissions: true,
	})
	events := make(chan loop.AgentEvent, 4)
	if _, err := e.Run(context.Background(), "p", events); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out, _ := os.ReadFile(logPath)
	if !strings.Contains(string(out), "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions should appear when SkipPermissions=true: %q", out)
	}
}

func TestRun_StreamsEventsFromFixture(t *testing.T) {
	logPath := withLogPath(t)
	e := New(Config{Binary: fixture(t, "fake-claude-fixture.sh")})

	events := make(chan loop.AgentEvent, 64)
	var got []loop.AgentEvent
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for ev := range events {
			got = append(got, ev)
		}
	}()

	res, err := e.Run(context.Background(), "p", events)
	close(events)
	<-drained
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", res.ExitCode)
	}
	if res.LogPath != logPath {
		t.Errorf("LogPath = %q, want %q", res.LogPath, logPath)
	}

	wantKinds := []loop.AgentEventKind{
		loop.KindInit,
		loop.KindRateLimit,
		loop.KindThinking,
		loop.KindToolUse,
		loop.KindToolResult,
		loop.KindToolUse,
		loop.KindToolResult,
		loop.KindAssistantText,
		loop.KindResultSummary,
	}
	if len(got) != len(wantKinds) {
		t.Fatalf("event count = %d, want %d:\n got=%+v", len(got), len(wantKinds), got)
	}
	for i, k := range wantKinds {
		if got[i].Kind != k {
			t.Errorf("event[%d].Kind = %q, want %q", i, got[i].Kind, k)
		}
	}

	// Raw log should mirror the fixture line-for-line.
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	wantBytes, err := os.ReadFile(fixture(t, "full-iter.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !strings.EqualFold(string(logBytes), string(wantBytes)) {
		t.Errorf("raw log differs from fixture:\n got=%q\nwant=%q", logBytes, wantBytes)
	}
}
