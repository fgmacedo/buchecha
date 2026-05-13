package claude_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/provider"
	"github.com/fgmacedo/buchecha/internal/provider/claude"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("abs %s: %v", name, err)
	}
	return abs
}

// echoArgsOut sets ECHO_ARGS_OUT to a fresh file under t.TempDir() so
// fake-claude-echo-args.sh records argv there for the test to read.
func echoArgsOut(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv("ECHO_ARGS_OUT", path)
	return path
}

// drainEvents pumps the spawn's agent events into a slice.
func drainEvents(t *testing.T, p *claude.Claude, req provider.SpawnRequest) (provider.SpawnResult, []agentcontract.AgentEvent, error) {
	t.Helper()
	events := make(chan agentcontract.AgentEvent, 64)
	var got []agentcontract.AgentEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range events {
			got = append(got, ev)
		}
	}()
	req.Events = events
	res, err := p.Spawn(context.Background(), req)
	close(events)
	<-done
	return res, got, err
}

func TestSpawn_Name(t *testing.T) {
	c := claude.New(claude.Config{})
	if c.Name() != "claude" {
		t.Errorf("Name() = %q, want %q", c.Name(), "claude")
	}
}

func TestSpawn_StreamsEvents(t *testing.T) {
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude.sh")})
	res, got, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "test prompt"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	wantKinds := []agentcontract.AgentEventKind{
		agentcontract.KindInit,
		agentcontract.KindAssistantText,
		agentcontract.KindResultSummary,
	}
	if len(got) != len(wantKinds) {
		t.Fatalf("event count = %d, want %d: %+v", len(got), len(wantKinds), got)
	}
	for i, k := range wantKinds {
		if got[i].Kind != k {
			t.Errorf("event[%d].Kind = %q, want %q", i, got[i].Kind, k)
		}
	}
}

func TestSpawn_PropagatesNonZeroExit(t *testing.T) {
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-fail.sh")})
	res, got, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "x"})
	if err != nil {
		t.Fatalf("Spawn: unexpected error %v", err)
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
	if len(got) == 0 || got[0].Kind != agentcontract.KindInit {
		t.Errorf("partial stream lost; got=%+v", got)
	}
}

func TestSpawn_BinaryNotFound(t *testing.T) {
	c := claude.New(claude.Config{Binary: "/nonexistent/binary"})
	events := make(chan agentcontract.AgentEvent, 4)
	_, err := c.Spawn(context.Background(), provider.SpawnRequest{Prompt: "x", Events: events})
	if err == nil {
		t.Errorf("expected error for missing binary")
	}
}

func TestSpawn_ContextCancelInterrupts(t *testing.T) {
	c := claude.New(claude.Config{
		Binary:      fixture(t, "fake-claude-slow.sh"),
		CancelGrace: 1 * time.Second,
	})
	events := make(chan agentcontract.AgentEvent, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := c.Spawn(ctx, provider.SpawnRequest{Prompt: "x", Events: events})
	if err == nil {
		t.Fatalf("expected ctx error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want ctx.Err()", err)
	}
}

func TestSpawn_ArgvAssembly_FullExecutorShape(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := claude.New(claude.Config{
		Binary:    fixture(t, "fake-claude-echo-args.sh"),
		ExtraArgs: []string{"--foo", "--bar"},
	})
	req := provider.SpawnRequest{
		Prompt:          "the prompt",
		Model:           "test-model",
		SkipPermissions: true,
		MCP: provider.MCPSpec{
			URL:            "http://127.0.0.1:1/mcp/",
			Token:          "deadbeef",
			ConnectionName: "bcc-executor",
		},
	}
	if _, _, err := drainEvents(t, c, req); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	lines := readArgs(t, argsPath)

	mcpIdx := indexOf(lines, "--mcp-config")
	if mcpIdx < 0 {
		t.Fatalf("--mcp-config missing from args: %q", lines)
	}
	if mcpIdx+1 >= len(lines) || !strings.HasSuffix(lines[mcpIdx+1], "/mcp-config.json") {
		t.Errorf("arg after --mcp-config = %q, want path ending in /mcp-config.json", lines[mcpIdx+1])
	}
	if mcpIdx+2 >= len(lines) || lines[mcpIdx+2] != "--strict-mcp-config" {
		t.Errorf("--strict-mcp-config should follow --mcp-config <path>, got %q", lines[mcpIdx+2:])
	}
	stripped := slices.Clone(lines[:mcpIdx])
	stripped = append(stripped, lines[mcpIdx+3:]...)
	want := []string{
		"-p", "--output-format", "stream-json", "--verbose",
		"--dangerously-skip-permissions",
		"--model", "test-model",
		"--foo", "--bar",
		"the prompt",
	}
	if len(stripped) != len(want) {
		t.Fatalf("got %d args (after strip), want %d: %v", len(stripped), len(want), stripped)
	}
	for i := range want {
		if stripped[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, stripped[i], want[i])
		}
	}
}

func TestSpawn_OmitsMCPConfigWhenURLEmpty(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	if _, _, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "p"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	if strings.Contains(string(out), "--mcp-config") || strings.Contains(string(out), "--strict-mcp-config") {
		t.Errorf("MCP flags should be absent when MCP.URL is empty: %q", out)
	}
}

func TestSpawn_OmitsModelWhenEmpty(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	if _, _, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "p"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	if strings.Contains(string(out), "--model") {
		t.Errorf("--model should be omitted when Model is empty: %q", out)
	}
}

func TestSpawn_PassesEffortWhenSet(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	if _, _, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "p", Effort: "high"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	lines := readArgs(t, argsPath)
	if i := indexOf(lines, "--effort"); i < 0 || i+1 >= len(lines) || lines[i+1] != "high" {
		t.Errorf("--effort high missing in args: %v", lines)
	}
}

func TestSpawn_OmitsEffortWhenEmpty(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	if _, _, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "p"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	if strings.Contains(string(out), "--effort") {
		t.Errorf("--effort should be omitted when Effort is empty: %q", out)
	}
}

func TestSpawn_AddsSkipPermissionsWhenTrue(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	if _, _, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "p", SkipPermissions: true}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	if !strings.Contains(string(out), "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions missing when SkipPermissions=true: %q", out)
	}
}

func TestSpawn_OmitsSkipPermissionsWhenFalse(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	if _, _, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "p"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	if strings.Contains(string(out), "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions should be absent when SkipPermissions=false: %q", out)
	}
}

func TestSpawn_AllowedToolsCSV(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	req := provider.SpawnRequest{
		Prompt:       "p",
		AllowedTools: []string{"Read", "Bash", "Grep", "Glob"},
	}
	if _, _, err := drainEvents(t, c, req); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	lines := readArgs(t, argsPath)
	i := indexOf(lines, "--allowed-tools")
	if i < 0 || i+1 >= len(lines) {
		t.Fatalf("--allowed-tools missing or trailing: %v", lines)
	}
	if got, want := lines[i+1], "Read,Bash,Grep,Glob"; got != want {
		t.Errorf("--allowed-tools value = %q, want %q", got, want)
	}
}

func TestSpawn_MaxBudgetUSDWhenSet(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	if _, _, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "p", MaxBudgetUSD: 1.5}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	lines := readArgs(t, argsPath)
	i := indexOf(lines, "--max-budget-usd")
	if i < 0 || i+1 >= len(lines) {
		t.Fatalf("--max-budget-usd missing: %v", lines)
	}
	if lines[i+1] != "1.5" {
		t.Errorf("--max-budget-usd value = %q, want %q", lines[i+1], "1.5")
	}
}

func TestSpawn_SystemPromptFile_FeedsStdin(t *testing.T) {
	argsPath := echoArgsOut(t)
	stdinPath := filepath.Join(t.TempDir(), "stdin.txt")
	t.Setenv("ECHO_STDIN_OUT", stdinPath)
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	const briefing = "iteration body delivered via stdin"
	req := provider.SpawnRequest{Prompt: briefing, SystemPrompt: "# contract\n"}
	if _, _, err := drainEvents(t, c, req); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	lines := readArgs(t, argsPath)
	i := indexOf(lines, "--system-prompt-file")
	if i < 0 {
		t.Fatalf("--system-prompt-file missing from args: %v", lines)
	}
	if i+1 >= len(lines) || !strings.HasSuffix(lines[i+1], "system.md") {
		t.Errorf("--system-prompt-file path = %q, want suffix system.md", lines[i+1])
	}
	for _, a := range lines {
		if a == briefing {
			t.Errorf("briefing must arrive via stdin, not as positional arg; got %v", lines)
		}
	}
	stdinBytes, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read stdin capture: %v", err)
	}
	if string(stdinBytes) != briefing {
		t.Errorf("stdin = %q, want %q", string(stdinBytes), briefing)
	}
}

func TestSpawn_SystemPromptFile_RejectsEmptyPrompt(t *testing.T) {
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-echo-args.sh")})
	res, _, err := drainEvents(t, c, provider.SpawnRequest{SystemPrompt: "# contract"})
	if err == nil {
		t.Fatalf("expected error for empty prompt with SystemPrompt set")
	}
	if res.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", res.ExitCode)
	}
}

func newTestStore(t *testing.T) *session.Store {
	t.Helper()
	base := t.TempDir()
	store, _, err := session.CreateSession(base, "/tmp/spec.md", "deadbeef",
		time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return store
}

func collectLoopEvents(ch <-chan loop.Event) []loop.Event {
	var out []loop.Event
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}

func TestSpawn_PersistsPromptAndEmitsLifecycleEvents(t *testing.T) {
	store := newTestStore(t)
	loopEvents := make(chan loop.Event, 16)
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude.sh")})
	req := provider.SpawnRequest{
		Role:         "bcc-executor",
		Prompt:       "implement the spec",
		PhaseID:      "P1",
		IterationID:  "P1-01",
		Attempt:      1,
		SessionStore: store,
		LoopEvents:   chan<- loop.Event(loopEvents),
	}
	res, _, err := drainEvents(t, c, req)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if res.SpawnID == "" {
		t.Fatalf("SpawnID is empty")
	}
	evs := collectLoopEvents(loopEvents)
	var started *loop.SpawnStarted
	var finished *loop.SpawnFinished
	for i := range evs {
		switch e := evs[i].(type) {
		case loop.SpawnStarted:
			s := e
			started = &s
		case loop.SpawnFinished:
			s := e
			finished = &s
		}
	}
	if started == nil {
		t.Fatalf("no SpawnStarted emitted; got %v", evs)
	}
	if finished == nil {
		t.Fatalf("no SpawnFinished emitted; got %v", evs)
	}
	if started.SpawnID != res.SpawnID || finished.SpawnID != res.SpawnID {
		t.Errorf("SpawnID mismatch: started=%q finished=%q res=%q",
			started.SpawnID, finished.SpawnID, res.SpawnID)
	}
	if started.Provider != "claude" {
		t.Errorf("started.Provider = %q, want claude", started.Provider)
	}
	if started.PhaseID != "P1" {
		t.Errorf("PhaseID = %q, want P1", started.PhaseID)
	}
	got, err := os.ReadFile(started.PromptPath)
	if err != nil {
		t.Fatalf("read prompt file: %v", err)
	}
	if !strings.Contains(string(got), "implement the spec") {
		t.Errorf("prompt file content = %q", got)
	}
}

func TestSpawn_CostExtractedFromResultSummary(t *testing.T) {
	store := newTestStore(t)
	loopEvents := make(chan loop.Event, 16)
	c := claude.New(claude.Config{Binary: fixture(t, "fake-claude-fixture.sh")})
	req := provider.SpawnRequest{
		Role:         "bcc-executor",
		Prompt:       "p",
		SessionStore: store,
		LoopEvents:   chan<- loop.Event(loopEvents),
	}
	res, _, err := drainEvents(t, c, req)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if res.CostUSD == 0 && res.Tokens == (agentcontract.TokenUsage{}) {
		t.Errorf("expected non-zero cost/tokens from fixture, got %+v", res)
	}
	evs := collectLoopEvents(loopEvents)
	var finished *loop.SpawnFinished
	for i := range evs {
		if f, ok := evs[i].(loop.SpawnFinished); ok {
			finished = &f
			break
		}
	}
	if finished == nil {
		t.Fatalf("no SpawnFinished emitted")
	}
	if finished.Cost.USD != res.CostUSD {
		t.Errorf("Cost.USD %v != res.CostUSD %v", finished.Cost.USD, res.CostUSD)
	}
	if finished.Cost.Tokens != res.Tokens {
		t.Errorf("Cost.Tokens %+v != res.Tokens %+v", finished.Cost.Tokens, res.Tokens)
	}
}

// readArgs reads the argv-capture file produced by fake-claude-echo-args.sh
// and returns it as a slice of lines.
func readArgs(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func indexOf(slice []string, v string) int {
	for i, s := range slice {
		if s == v {
			return i
		}
	}
	return -1
}
