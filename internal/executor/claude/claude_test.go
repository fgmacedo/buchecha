package claude

import (
	"context"
	"errors"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
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

func collectEvents(t *testing.T, e *Executor, prompt string) (loop.ExecResult, []agentcontract.AgentEvent, error) {
	t.Helper()
	events := make(chan agentcontract.AgentEvent, 64)
	var got []agentcontract.AgentEvent
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
	wantKinds := []agentcontract.AgentEventKind{
		agentcontract.KindInit,
		agentcontract.KindAssistantText,
		agentcontract.KindResultSummary,
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
	if len(got) == 0 || got[0].Kind != agentcontract.KindInit {
		t.Errorf("partial stream lost; got=%+v", got)
	}
}

func TestRun_BinaryNotFound(t *testing.T) {
	e := New(Config{Binary: "/nonexistent/binary"})
	events := make(chan agentcontract.AgentEvent, 4)
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
	events := make(chan agentcontract.AgentEvent, 4)
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
		Binary:            fixture(t, "fake-claude-echo-args.sh"),
		Model:             "test-model",
		ExtraArgs:         []string{"--foo", "--bar"},
		SkipPermissions:   true,
		MCPURL:            "http://127.0.0.1:1/mcp",
		MCPToken:          "deadbeef",
		MCPConnectionName: "bcc-executor",
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

	// --mcp-config carries a temp path generated per Run; assert its
	// presence and shape, then strip it before checking the rest of
	// the args verbatim.
	mcpIdx := -1
	for i, arg := range lines {
		if arg == "--mcp-config" {
			mcpIdx = i
			break
		}
	}
	if mcpIdx < 0 {
		t.Fatalf("--mcp-config missing from args: %q", lines)
	}
	if mcpIdx+1 >= len(lines) || !strings.HasSuffix(lines[mcpIdx+1], "/mcp-config.json") {
		t.Errorf("arg after --mcp-config = %q, want path ending in /mcp-config.json", lines[mcpIdx+1])
	}
	if mcpIdx+2 >= len(lines) || lines[mcpIdx+2] != "--strict-mcp-config" {
		t.Errorf("--strict-mcp-config should follow --mcp-config <path>, got %q", lines[mcpIdx+2:])
	}
	stripped := append([]string{}, lines[:mcpIdx]...)
	stripped = append(stripped, lines[mcpIdx+3:]...)

	want := []string{
		"-p", "--output-format", "stream-json", "--verbose",
		"--dangerously-skip-permissions",
		"--model", "test-model",
		"--foo", "--bar",
		"the prompt",
	}
	if len(stripped) != len(want) {
		t.Fatalf("got %d args (after strip), want %d (output=%q)", len(stripped), len(want), out)
	}
	for i := range want {
		if stripped[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, stripped[i], want[i])
		}
	}
}

// TestRun_SystemPromptFile_FeedsStdin pins the Director-mode contract:
// when Config.SystemPromptFile is set, the adapter passes
// --system-prompt-file <path>, omits the positional prompt, and pipes
// the prompt parameter into the subprocess via stdin. The contract
// (system) and the briefing (user) ride on different channels so claude
// --print sees a non-empty user input.
func TestRun_SystemPromptFile_FeedsStdin(t *testing.T) {
	argsPath := echoArgsOut(t)
	stdinPath := filepath.Join(t.TempDir(), "stdin.txt")
	t.Setenv("ECHO_STDIN_OUT", stdinPath)
	systemPath := filepath.Join(t.TempDir(), "contract.system.md")
	if err := os.WriteFile(systemPath, []byte("# contract\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Config{
		Binary:           fixture(t, "fake-claude-echo-args.sh"),
		SystemPromptFile: systemPath,
	})
	const briefingBody = "iteration body delivered via stdin"
	if _, _, err := collectEvents(t, e, briefingBody); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	args := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	flagIdx := -1
	for i, a := range args {
		if a == "--system-prompt-file" {
			flagIdx = i
			break
		}
	}
	if flagIdx < 0 {
		t.Fatalf("--system-prompt-file missing from args: %q", args)
	}
	if flagIdx+1 >= len(args) || args[flagIdx+1] != systemPath {
		t.Errorf("arg after --system-prompt-file = %q, want %q", args[flagIdx+1], systemPath)
	}
	for _, a := range args {
		if a == briefingBody {
			t.Errorf("briefing body must arrive via stdin, not as a positional arg; got args %q", args)
		}
	}
	stdinBytes, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read stdin capture: %v", err)
	}
	if string(stdinBytes) != briefingBody {
		t.Errorf("stdin = %q, want %q", string(stdinBytes), briefingBody)
	}
}

// TestRun_SystemPromptFile_RejectsEmptyPrompt ensures the adapter
// rejects an empty prompt under --system-prompt-file before launching
// claude: claude --print requires a user prompt and the historical
// failure mode (silent stall with stderr leaking into the TUI) was the
// reason this branch exists.
func TestRun_SystemPromptFile_RejectsEmptyPrompt(t *testing.T) {
	systemPath := filepath.Join(t.TempDir(), "contract.system.md")
	if err := os.WriteFile(systemPath, []byte("# contract\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Config{
		Binary:           fixture(t, "fake-claude-echo-args.sh"),
		SystemPromptFile: systemPath,
	})
	res, _, err := collectEvents(t, e, "")
	if err == nil {
		t.Fatalf("expected error when prompt is empty under SystemPromptFile; got nil")
	}
	if res.ExitCode != -1 {
		t.Errorf("expected ExitCode -1 (invocation rejected); got %d", res.ExitCode)
	}
}

// TestRun_OmitsMCPConfigWhenURLEmpty verifies the test-friendly default:
// without an MCPURL the adapter omits the --mcp-config flags so fake
// scripts that do not implement MCP can still drive the executor.
func TestRun_OmitsMCPConfigWhenURLEmpty(t *testing.T) {
	argsPath := echoArgsOut(t)
	e := New(Config{
		Binary: fixture(t, "fake-claude-echo-args.sh"),
	})
	if _, _, err := collectEvents(t, e, "p"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	if strings.Contains(string(out), "--mcp-config") || strings.Contains(string(out), "--strict-mcp-config") {
		t.Errorf("MCP flags should be absent when MCPURL is empty: %q", out)
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

	// Mirrors testdata/full-iter.jsonl: every tool_use envelope (now
	// uniformly KindToolUse since legacy wire routing is gone) is
	// followed by its tool_result envelope, plus init / rate_limit /
	// thinking / assistant_text framing and the terminal result.
	wantKinds := []agentcontract.AgentEventKind{
		agentcontract.KindInit,
		agentcontract.KindRateLimit,
		agentcontract.KindThinking,
		agentcontract.KindToolUse,
		agentcontract.KindToolResult,
		agentcontract.KindToolUse,
		agentcontract.KindToolResult,
		agentcontract.KindToolUse,
		agentcontract.KindToolResult,
		agentcontract.KindToolUse,
		agentcontract.KindToolResult,
		agentcontract.KindAssistantText,
		agentcontract.KindToolUse,
		agentcontract.KindToolResult,
		agentcontract.KindResultSummary,
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
