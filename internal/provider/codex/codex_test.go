package codex_test

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
	"github.com/fgmacedo/buchecha/internal/provider/codex"
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

func echoArgsOut(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv("ECHO_ARGS_OUT", path)
	return path
}

func drainEvents(t *testing.T, p *codex.Codex, req provider.SpawnRequest) (provider.SpawnResult, []agentcontract.AgentEvent, error) {
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

func TestSpawn_Name(t *testing.T) {
	c := codex.New(codex.Config{})
	if c.Name() != "codex" {
		t.Errorf("Name() = %q, want %q", c.Name(), "codex")
	}
}

func TestSpawn_StreamsEvents(t *testing.T) {
	c := codex.New(codex.Config{Binary: fixture(t, "fake-codex.sh")})
	res, got, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "go"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	wantKinds := []agentcontract.AgentEventKind{
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
	if res.Tokens.InputCached != 20 || res.Tokens.InputFresh != 80 || res.Tokens.Output != 10 {
		t.Errorf("Tokens mismatch: %+v", res.Tokens)
	}
}

func TestSpawn_PropagatesNonZeroExit(t *testing.T) {
	c := codex.New(codex.Config{Binary: fixture(t, "fake-codex-fail.sh")})
	res, _, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "x"})
	if err != nil {
		t.Fatalf("Spawn: unexpected error %v", err)
	}
	if res.ExitCode != 9 {
		t.Errorf("ExitCode = %d, want 9", res.ExitCode)
	}
	if !strings.Contains(res.StderrTail, "simulated quota exhausted") {
		t.Errorf("StderrTail = %q, want to contain simulated stderr", res.StderrTail)
	}
}

func TestSpawn_BinaryNotFound(t *testing.T) {
	c := codex.New(codex.Config{Binary: "/nonexistent/codex"})
	events := make(chan agentcontract.AgentEvent, 4)
	_, err := c.Spawn(context.Background(), provider.SpawnRequest{Prompt: "x", Events: events})
	if err == nil {
		t.Errorf("expected error for missing binary")
	}
}

func TestSpawn_ContextCancelInterrupts(t *testing.T) {
	c := codex.New(codex.Config{
		Binary:      fixture(t, "fake-codex-slow.sh"),
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

func TestSpawn_EmptyPromptRejected(t *testing.T) {
	c := codex.New(codex.Config{Binary: fixture(t, "fake-codex.sh")})
	res, _, err := drainEvents(t, c, provider.SpawnRequest{})
	if err == nil {
		t.Fatalf("expected error for empty prompt")
	}
	if res.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", res.ExitCode)
	}
}

func TestSpawn_ArgvAssembly_FullExecutorShape(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := codex.New(codex.Config{
		Binary:    fixture(t, "fake-codex-echo-args.sh"),
		ExtraArgs: []string{"--foo", "--bar"},
	})
	req := provider.SpawnRequest{
		Prompt:          "the prompt",
		Model:           "gpt-5.4-codex",
		Sandbox:         provider.SandboxWorkspaceWrite,
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
	want := []string{
		"-a", "never",
		"exec",
		"--json", "--ignore-user-config", "--skip-git-repo-check", "--ephemeral",
		"-c", `mcp_servers.bcc.url="http://127.0.0.1:1/mcp/"`,
		"-c", `mcp_servers.bcc.http_headers={Authorization="Bearer deadbeef", X-BCC-Role="bcc-executor"}`,
		"-c", `mcp_servers.bcc.default_tools_approval_mode="approve"`,
		"-s", "workspace-write",
		"-m", "gpt-5.4-codex",
		"--foo", "--bar",
		"-",
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("argv mismatch\n got: %v\nwant: %v", lines, want)
	}
}

func TestSpawn_OmitsMCPWhenURLEmpty(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := codex.New(codex.Config{Binary: fixture(t, "fake-codex-echo-args.sh")})
	if _, _, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "p"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	if strings.Contains(string(out), "mcp_servers.bcc.url") {
		t.Errorf("MCP overrides should be absent when MCP.URL is empty: %q", out)
	}
}

func TestSpawn_OmitsApprovalWhenSkipPermissionsFalse(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := codex.New(codex.Config{Binary: fixture(t, "fake-codex-echo-args.sh")})
	if _, _, err := drainEvents(t, c, provider.SpawnRequest{Prompt: "p"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	lines := readArgs(t, argsPath)
	if indexOf(lines, "-a") >= 0 {
		t.Errorf("-a should be omitted when SkipPermissions=false; got %v", lines)
	}
}

func TestSpawn_SandboxFlagsMapping(t *testing.T) {
	cases := []struct {
		name    string
		sandbox provider.Sandbox
		wantArg string
	}{
		{"read-only", provider.SandboxReadOnly, "read-only"},
		{"workspace-write", provider.SandboxWorkspaceWrite, "workspace-write"},
		{"danger-full-access", provider.SandboxDangerFullAccess, "danger-full-access"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argsPath := echoArgsOut(t)
			c := codex.New(codex.Config{Binary: fixture(t, "fake-codex-echo-args.sh")})
			req := provider.SpawnRequest{Prompt: "p", Sandbox: tc.sandbox}
			if _, _, err := drainEvents(t, c, req); err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			lines := readArgs(t, argsPath)
			i := indexOf(lines, "-s")
			if i < 0 || i+1 >= len(lines) {
				t.Fatalf("-s flag missing: %v", lines)
			}
			if lines[i+1] != tc.wantArg {
				t.Errorf("-s = %q, want %q", lines[i+1], tc.wantArg)
			}
		})
	}
}

func TestSpawn_EffortIsIgnored(t *testing.T) {
	argsPath := echoArgsOut(t)
	c := codex.New(codex.Config{Binary: fixture(t, "fake-codex-echo-args.sh")})
	req := provider.SpawnRequest{Prompt: "p", Effort: "high"}
	if _, _, err := drainEvents(t, c, req); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	out, _ := os.ReadFile(argsPath)
	if strings.Contains(string(out), "high") {
		t.Errorf("Effort must NOT be mapped on codex 0.130; got argv %q", out)
	}
}

func TestSpawn_StdinCarriesSystemAndUserPrompt(t *testing.T) {
	echoArgsOut(t)
	stdinPath := filepath.Join(t.TempDir(), "stdin.txt")
	t.Setenv("ECHO_STDIN_OUT", stdinPath)
	c := codex.New(codex.Config{Binary: fixture(t, "fake-codex-echo-args.sh")})
	req := provider.SpawnRequest{
		SystemPrompt: "# system contract",
		Prompt:       "iteration body",
	}
	if _, _, err := drainEvents(t, c, req); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	stdinBytes, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read stdin capture: %v", err)
	}
	got := string(stdinBytes)
	if !strings.HasPrefix(got, "# system contract") {
		t.Errorf("stdin must start with SystemPrompt; got %q", got)
	}
	if !strings.Contains(got, "iteration body") {
		t.Errorf("stdin must contain user Prompt; got %q", got)
	}
}

func newTestStore(t *testing.T) *session.Store {
	t.Helper()
	base := t.TempDir()
	store, _, err := session.CreateSession(base, "/tmp/spec.md", "deadbeef",
		time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC))
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
	c := codex.New(codex.Config{Binary: fixture(t, "fake-codex.sh")})
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
	if started.Provider != "codex" {
		t.Errorf("started.Provider = %q, want codex", started.Provider)
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
	if finished.Cost.USD != res.CostUSD {
		t.Errorf("Cost.USD %v != res.CostUSD %v", finished.Cost.USD, res.CostUSD)
	}
	if finished.Cost.Tokens != res.Tokens {
		t.Errorf("Cost.Tokens %+v != res.Tokens %+v", finished.Cost.Tokens, res.Tokens)
	}
}
