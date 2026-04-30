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

// echoArgsOut sets ECHO_ARGS_OUT to a fresh file in t.TempDir() so the
// fake-claude-echo-args.sh helper can record argv for the test to read.
func echoArgsOut(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv("ECHO_ARGS_OUT", path)
	return path
}

func collectEvents(t *testing.T, e *Executor, prompt string) (loop.ExecResult, []loop.AgentEvent, error) {
	t.Helper()
	events := make(chan loop.AgentEvent, 64)
	var got []loop.AgentEvent
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for ev := range events {
			got = append(got, ev)
		}
	}()
	res, err := e.Run(context.Background(), prompt, events)
	close(events)
	<-drained
	return res, got, err
}

func TestRun_StreamsEvents(t *testing.T) {
	e := New(Config{Binary: fixture(t, "fake-claude.sh")})
	res, got, err := collectEvents(t, e, "test prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", res.ExitCode)
	}
	wantKinds := []loop.AgentEventKind{
		loop.KindInit,
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
}

func TestRun_PropagatesNonZeroExit(t *testing.T) {
	e := New(Config{Binary: fixture(t, "fake-claude-fail.sh")})
	res, got, err := collectEvents(t, e, "x")
	if err != nil {
		t.Fatalf("Run: unexpected error %v", err)
	}
	if res.ExitCode != 7 {
		t.Errorf("exit code = %d, want 7", res.ExitCode)
	}
	// Partial stdout before exit must still be parsed and forwarded.
	if len(got) == 0 || got[0].Kind != loop.KindInit {
		t.Errorf("partial stream lost; got=%+v", got)
	}
}

func TestRun_BinaryNotFound(t *testing.T) {
	e := New(Config{Binary: "/nonexistent/binary"})
	events := make(chan loop.AgentEvent, 4)
	_, err := e.Run(context.Background(), "x", events)
	if err == nil {
		t.Errorf("expected error for missing binary")
	}
}

func TestRun_ContextCancelInterrupts(t *testing.T) {
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
}

func TestRun_PromptIsLastArg(t *testing.T) {
	argsPath := echoArgsOut(t)
	e := New(Config{
		Binary:          fixture(t, "fake-claude-echo-args.sh"),
		Model:           "test-model",
		ExtraArgs:       []string{"--foo", "--bar"},
		SkipPermissions: true,
	})
	if _, _, err := collectEvents(t, e, "the prompt"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read argv: %v", err)
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
	argsPath := echoArgsOut(t)
	e := New(Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	if _, _, err := collectEvents(t, e, "p"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	if strings.Contains(string(out), "--model") {
		t.Errorf("--model should be omitted when Model is empty: %q", out)
	}
}

func TestRun_OmitsSkipPermissionsWhenFalse(t *testing.T) {
	argsPath := echoArgsOut(t)
	e := New(Config{
		Binary:          fixture(t, "fake-claude-echo-args.sh"),
		SkipPermissions: false,
	})
	if _, _, err := collectEvents(t, e, "p"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	if strings.Contains(string(out), "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions should be omitted when SkipPermissions=false: %q", out)
	}
}

func TestRun_AddsSkipPermissionsWhenTrue(t *testing.T) {
	argsPath := echoArgsOut(t)
	e := New(Config{
		Binary:          fixture(t, "fake-claude-echo-args.sh"),
		SkipPermissions: true,
	})
	if _, _, err := collectEvents(t, e, "p"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	if !strings.Contains(string(out), "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions should appear when SkipPermissions=true: %q", out)
	}
}

func TestRun_StreamsEventsFromFixture(t *testing.T) {
	e := New(Config{Binary: fixture(t, "fake-claude-fixture.sh")})
	res, got, err := collectEvents(t, e, "p")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", res.ExitCode)
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
}
