// Package claude implements provider.Provider for Anthropic's claude CLI
// (claude --print with --output-format stream-json).
//
// The adapter is vendor-specific but role-agnostic: every cognitive role
// (executor, planner, briefer, reviewer) reaches claude through the same
// Spawn method, with role-specific shaping carried on SpawnRequest fields
// (Sandbox is ignored on claude; AllowedTools and SkipPermissions are the
// equivalent levers). MCP wiring, prompt persistence, and SpawnStarted /
// SpawnFinished event emission flow through internal/provider/spawnkit so
// every adapter speaks the same protocol-level dialect.
package claude

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/provider"
	"github.com/fgmacedo/buchecha/internal/provider/claude/streamjson"
	"github.com/fgmacedo/buchecha/internal/provider/spawnkit"
	"github.com/fgmacedo/buchecha/internal/supervision"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

// stderrCaptureBytes caps how much of claude's stderr the adapter holds
// in memory per spawn. Large enough to carry a useful tail (auth / quota
// / model errors), small enough to be safe under runaway output.
const stderrCaptureBytes = 8 * 1024

// defaultCancelGrace is the SIGINT -> SIGKILL escalation window used
// when Config.CancelGrace is zero.
const defaultCancelGrace = 5 * time.Second

// defaultMaxLineBytes is the per-line cap for the stream-json scanner
// when Config.MaxLineBytes is zero. Large enough to carry tool_result
// payloads that exceed bufio.Scanner's 64 KiB default.
const defaultMaxLineBytes = 8 * 1024 * 1024

// Compile-time check that *Claude satisfies provider.Provider.
var _ provider.Provider = (*Claude)(nil)

// Config configures the claude provider. Per-spawn values (model,
// effort, prompt, MCP spec, …) arrive on SpawnRequest; Config carries
// run-wide knobs that do not change between spawns.
type Config struct {
	// Binary is the path or PATH name of the claude binary. Empty
	// defaults to "claude".
	Binary string

	// ExtraArgs are appended to every argv after the per-spawn flags
	// (model, effort, MCP, allowed-tools) and before the positional
	// prompt. They concatenate with SpawnRequest.ExtraArgs.
	ExtraArgs []string

	// Stderr, when non-nil, receives the subprocess stderr verbatim in
	// addition to the in-memory tail. Default (nil) keeps only the
	// in-memory tail.
	Stderr io.Writer

	// Stdout, when non-nil, is teed from the subprocess stdout pipe
	// before the stream-json parser consumes it. Useful for raw line
	// captures alongside the parsed AgentEvents.
	Stdout io.Writer

	// CancelGrace is how long to wait after sending SIGINT before forcing
	// SIGKILL via exec.Cmd.WaitDelay. Zero uses defaultCancelGrace.
	CancelGrace time.Duration

	// MaxLineBytes caps a single stream-json line. Zero uses
	// defaultMaxLineBytes.
	MaxLineBytes int
}

// Claude implements provider.Provider for the claude CLI. Construct
// with New; the zero value is invalid.
type Claude struct {
	cfg Config
}

// New returns a Claude provider with the given Config. Zero-valued
// Binary, CancelGrace, and MaxLineBytes get their defaults.
func New(cfg Config) *Claude {
	if cfg.Binary == "" {
		cfg.Binary = "claude"
	}
	if cfg.CancelGrace == 0 {
		cfg.CancelGrace = defaultCancelGrace
	}
	if cfg.MaxLineBytes == 0 {
		cfg.MaxLineBytes = defaultMaxLineBytes
	}
	return &Claude{cfg: cfg}
}

// Name returns "claude". It matches the .bcc.toml [providers.<name>] key
// and the RoleAssignment.Provider string the Planner emits.
func (*Claude) Name() string { return "claude" }

// Spawn runs the claude CLI per the supplied request, streams stream-json
// telemetry into req.Events as agentcontract.AgentEvents, emits
// loop.SpawnStarted / SpawnFinished onto req.LoopEvents (typed assertion
// to chan<- loop.Event), and returns the spawn result.
//
// The argv is assembled as:
//
//	claude -p --output-format stream-json --verbose
//	  [--mcp-config <tempfile> --strict-mcp-config]
//	  [--system-prompt-file <tempfile>]
//	  [--dangerously-skip-permissions]
//	  [--allowed-tools <csv>]
//	  [--model <m>] [--effort <e>] [--max-budget-usd <n>]
//	  <Config.ExtraArgs...> <req.ExtraArgs...>
//	  [<prompt>]
//
// When SystemPrompt is set, the user prompt is piped to stdin instead of
// being appended as a positional argument. Sandbox is ignored on claude:
// access control on this vendor is expressed via AllowedTools plus
// SkipPermissions.
//
// Cancellation: ctx propagates to the subprocess via exec.CommandContext;
// Cancel sends SIGINT first and WaitDelay (CancelGrace) escalates to
// SIGKILL when the process refuses to exit.
func (c *Claude) Spawn(ctx context.Context, req provider.SpawnRequest) (provider.SpawnResult, error) {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
	}

	// MCP wiring: per-spawn mcp-config file consumed by claude. The temp
	// directory holds only the config file; cleanup removes it whole.
	var mcpDir string
	if req.MCP.URL != "" {
		var err error
		mcpDir, err = os.MkdirTemp("", "bcc-mcp-")
		if err != nil {
			return provider.SpawnResult{ExitCode: -1}, fmt.Errorf("provider/claude: mcp tempdir: %w", err)
		}
		defer func() { _ = os.RemoveAll(mcpDir) }()
		path, _, err := spawnkit.WriteMCPConfig(mcpDir, req.MCP)
		if err != nil {
			return provider.SpawnResult{ExitCode: -1}, fmt.Errorf("provider/claude: mcp-config write: %w", err)
		}
		args = append(args, "--mcp-config", path, "--strict-mcp-config")
	}

	// --system-prompt-file: claude consumes a file path, not inline
	// content. Materialise the SystemPrompt to a tempfile and let the
	// user prompt ride on stdin.
	var systemPromptPath string
	if req.SystemPrompt != "" {
		path, cleanup, err := writeSystemPromptFile(req.SystemPrompt)
		if err != nil {
			return provider.SpawnResult{ExitCode: -1}, fmt.Errorf("provider/claude: system-prompt write: %w", err)
		}
		defer cleanup()
		systemPromptPath = path
		args = append(args, "--system-prompt-file", path)
	}

	if req.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if len(req.AllowedTools) > 0 {
		args = append(args, "--allowed-tools", strings.Join(req.AllowedTools, ","))
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Effort != "" {
		args = append(args, "--effort", req.Effort)
	}
	if req.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", strconvFloat(req.MaxBudgetUSD))
	}
	args = append(args, c.cfg.ExtraArgs...)
	args = append(args, req.ExtraArgs...)
	if systemPromptPath == "" {
		if req.Prompt == "" {
			return provider.SpawnResult{ExitCode: -1}, errors.New("provider/claude: empty prompt with no system-prompt-file; claude --print requires input")
		}
		args = append(args, req.Prompt)
	} else if req.Prompt == "" {
		return provider.SpawnResult{ExitCode: -1}, errors.New("provider/claude: system-prompt-file set with empty prompt; claude --print requires user input")
	}

	cmd := exec.CommandContext(ctx, c.cfg.Binary, args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return provider.SpawnResult{ExitCode: -1}, fmt.Errorf("provider/claude: stdout pipe: %w", err)
	}
	if systemPromptPath != "" {
		cmd.Stdin = strings.NewReader(req.Prompt)
	}
	stderrTail := spawnkit.NewRingBuffer(stderrCaptureBytes)
	if c.cfg.Stderr != nil {
		cmd.Stderr = io.MultiWriter(c.cfg.Stderr, stderrTail)
	} else {
		cmd.Stderr = stderrTail
	}
	cmd.Cancel = func() error {
		// Graceful interrupt; exec.Cmd escalates to SIGKILL after WaitDelay.
		return cmd.Process.Signal(syscall.SIGINT)
	}
	cmd.WaitDelay = c.cfg.CancelGrace

	// Persist the prompt and emit SpawnStarted before the subprocess
	// starts so observers see exactly what the spawn was given.
	loopEvents, _ := req.LoopEvents.(chan<- loop.Event)
	info := spawnkit.SpawnInfo{
		Role:        req.Role,
		AgentID:     req.AgentID,
		PhaseID:     req.PhaseID,
		IterationID: req.IterationID,
		Attempt:     req.Attempt,
		Provider:    c.Name(),
		Model:       req.Model,
		Effort:      req.Effort,
	}
	if store, ok := req.SessionStore.(*session.Store); ok && store != nil {
		info.SpawnID = supervision.NewSpawnID()
		// Capture the system-prompt body alongside the user prompt so the
		// spawn file reflects the full context the agent saw.
		content := req.Prompt
		if req.SystemPrompt != "" {
			content = req.SystemPrompt + "\n\n" + req.Prompt
		}
		promptPath, perr := spawnkit.PersistPrompt(store, info.SpawnID, content)
		if perr != nil {
			return provider.SpawnResult{ExitCode: -1, SpawnID: info.SpawnID}, perr
		}
		spawnkit.EmitSpawnStarted(loopEvents, info, promptPath, time.Now().UTC())
	}

	spawnStartedAt := time.Now()
	if startErr := cmd.Start(); startErr != nil {
		return provider.SpawnResult{ExitCode: -1, SpawnID: info.SpawnID}, fmt.Errorf("provider/claude: run %s: %w", c.cfg.Binary, startErr)
	}

	// Drain the stdout pipe in a goroutine. parsedEvents stays in scope
	// so LastResultSummary can extract cost / tokens after Wait. The
	// caller's events channel still receives every event so the TUI
	// keeps rendering live.
	var src io.Reader = pipe
	if c.cfg.Stdout != nil {
		src = io.TeeReader(pipe, c.cfg.Stdout)
	}
	var (
		wg           sync.WaitGroup
		parsedEvents []agentcontract.AgentEvent
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(src)
		sc.Buffer(make([]byte, 64*1024), c.cfg.MaxLineBytes)
		originRole := resolveOriginRole(req.Role)
		for sc.Scan() {
			line := slices.Clone(sc.Bytes())
			for _, ev := range streamjson.ParseLine(line, time.Now()) {
				ev = ev.WithOrigin(req.AgentID, originRole, req.PhaseID, req.IterationID, req.Attempt)
				parsedEvents = append(parsedEvents, ev)
				if req.Events == nil {
					continue
				}
				select {
				case req.Events <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	wg.Wait()
	runErr := cmd.Wait()
	tail := strings.TrimSpace(stderrTail.Tail())

	exitCode := 0
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		exitCode = ee.ExitCode()
	} else if runErr != nil {
		exitCode = -1
	}

	result := provider.SpawnResult{
		SpawnID:    info.SpawnID,
		ExitCode:   exitCode,
		StderrTail: tail,
		DurationMS: time.Since(spawnStartedAt).Milliseconds(),
	}
	if summary, ok := streamjson.LastResultSummary(parsedEvents); ok {
		result.CostUSD = summary.TotalCostUSD
		result.Tokens = summary.Tokens
	}

	if info.SpawnID != "" {
		spawnkit.EmitSpawnFinished(loopEvents, info, result, time.Now().UTC())
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	if runErr == nil {
		return result, nil
	}
	if errors.As(runErr, &ee) {
		// Agent exited non-zero. That is a normal control-flow signal,
		// not a Spawn failure; the caller decides what to do.
		return result, nil
	}
	return result, fmt.Errorf("provider/claude: run %s: %w", c.cfg.Binary, runErr)
}

// writeSystemPromptFile persists body in a fresh tempfile (mode 0o600)
// so claude can load it via --system-prompt-file. The returned cleanup
// removes the tempdir.
func writeSystemPromptFile(body string) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "bcc-sysprompt-")
	if err != nil {
		return "", func() {}, err
	}
	path = dir + string(os.PathSeparator) + "system.md"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", func() {}, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	return path, cleanup, nil
}

// strconvFloat is a tiny helper kept in this file (instead of pulling
// strconv) because the value comes from a small, well-formed budget;
// rendering with %g matches the legacy CLI behaviour.
func strconvFloat(f float64) string { return fmt.Sprintf("%g", f) }

// resolveOriginRole returns the canonical agentcontract.Role for use in
// AgentEvent.WithOrigin given the caller-supplied req.Role string. The
// SpawnStarted / SpawnFinished events carry the raw req.Role verbatim
// (short form like "executor" for wire-format compatibility); this
// helper maps it to the agent-side canonical Role ("bcc-executor")
// when the short form is recognised, otherwise returns whatever the
// caller supplied unchanged. The function never panics; an unknown
// role propagates through as a typed Role value.
func resolveOriginRole(s string) agentcontract.Role {
	if r := agentcontract.Role(s); r.Valid() {
		return r
	}
	if r := agentcontract.Role("bcc-" + s); r.Valid() {
		return r
	}
	return agentcontract.Role(s)
}
