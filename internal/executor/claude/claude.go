// Package claude implements loop.Executor for Claude Code (claude CLI in
// print mode with stream-json output).
//
// MCP wiring is supplied externally: the run boot starts a single
// internal/mcp server with the dag.Handler attached, and passes the URL,
// bearer token, connection name, and per-spawn agent_id to the adapter
// via Config. The adapter writes a per-spawn mcp-config that points the
// agent at that server with the role's connection name carried in the
// X-BCC-Role header.
//
// The stream-json `tool_use` parser remains in place so the TUI and
// loop see agent activity in real time; per the migration spec the
// MCP handler is the protocol of record from P3 onward.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fgmacedo/buchecha/internal/executor/claude/streamjson"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// stderrCaptureBytes caps how much of claude's stderr the adapter
// holds in memory per call. Small enough to be safe under runaway
// output, large enough to carry a useful tail to the error message
// (typically a few exit reasons such as quota / auth / model errors).
const stderrCaptureBytes = 8 * 1024

// ringBuffer keeps the last N bytes written to it. Older bytes are
// dropped silently. Used to capture a small tail of the agent's
// stderr so a non-zero exit can surface a human-readable reason and
// the loop can render it on the dashboard.
type ringBuffer struct {
	cap int
	buf []byte
}

func newRingBuffer(cap int) *ringBuffer { return &ringBuffer{cap: cap} }

func (r *ringBuffer) Write(p []byte) (int, error) {
	if len(p) >= r.cap {
		r.buf = append(r.buf[:0], p[len(p)-r.cap:]...)
		return len(p), nil
	}
	if len(r.buf)+len(p) <= r.cap {
		r.buf = append(r.buf, p...)
		return len(p), nil
	}
	keep := r.cap - len(p)
	r.buf = append(r.buf[:0], r.buf[len(r.buf)-keep:]...)
	r.buf = append(r.buf, p...)
	return len(p), nil
}

func (r *ringBuffer) String() string { return string(r.buf) }

// Compile-time check that *Executor satisfies loop.Executor.
var _ loop.Executor = (*Executor)(nil)

// Config configures the Claude executor.
type Config struct {
	// Binary is the path or PATH name of the claude binary.
	Binary string

	// Model is passed via --model. Empty omits the flag.
	Model string

	// Effort is passed via --effort (claude CLI: low|medium|high|xhigh|max,
	// availability per model). Empty omits the flag. Set per-spawn from
	// the Phase's executor_assignment, falling back to the configured
	// default when the Planner does not attribute one.
	Effort string

	// ExtraArgs are appended to the command line after the protocol
	// flags, MCP wiring, --model, and before the prompt positional
	// argument. Reserve for ad-hoc additions.
	ExtraArgs []string

	// SkipPermissions, when true, adds --dangerously-skip-permissions to
	// the args so claude does not stall the loop with confirmation
	// prompts. This is the documented contract of bcc's autonomous mode.
	// When false (explicit user opt-out via .bcc.toml), the loop is
	// likely to hang on the first tool call; the user accepts that.
	SkipPermissions bool

	// SystemPromptFile, when non-empty, points to a file whose contents
	// claude loads as the system prompt via --system-prompt-file. Used by
	// Director-driven runs to ship the bcc contract (wire protocol,
	// absolute restrictions, working tree) as the durable system message
	// while the per-iteration briefing arrives as the user prompt via
	// stdin. When set, the prompt parameter passed to Run is fed to
	// claude on stdin instead of as a positional argument; an empty
	// prompt under SystemPromptFile is rejected because claude --print
	// requires user input. Default empty preserves the MVP behavior of
	// passing the prompt inline as a positional argument with no system
	// override.
	SystemPromptFile string

	// Stderr, when non-nil, receives the subprocess stderr verbatim.
	// Default (nil) discards stderr; callers wanting it should pipe to a
	// log file or os.Stderr explicitly.
	Stderr io.Writer

	// Stdout, when non-nil, is teed from the subprocess stdout pipe
	// before the stream-json parser consumes it. The raw line stream
	// lands in this writer in addition to being parsed into AgentEvents.
	// The caller owns the writer's lifetime; the adapter does not close
	// it.
	Stdout io.Writer

	// CancelGrace is how long to wait after sending SIGINT before forcing
	// SIGKILL. Defaults to 5 seconds when zero.
	CancelGrace time.Duration

	// MaxLineBytes caps a single stream-json line. Defaults to 8 MiB
	// when zero. Some tool_result payloads (large file reads) exceed
	// the default 64 KiB buffer of bufio.Scanner; oversize lines are
	// truncated and skipped rather than aborting the iteration.
	MaxLineBytes int

	// MCPURL is the http://127.0.0.1:port/mcp/ endpoint of the run-wide
	// MCP server. When empty, the adapter omits the --mcp-config wiring
	// entirely; useful for tests against fake-claude scripts that never
	// connect.
	MCPURL string

	// MCPToken is the bearer token the agent must present in
	// Authorization for every MCP request. Required when MCPURL is set.
	MCPToken string

	// MCPConnectionName names the role the agent is acting as on this
	// invocation: bcc-executor for ordinary work, bcc-planner for the
	// planning task, etc. The handler checks this against the
	// allowed-roles set per method.
	MCPConnectionName string

	// AgentID is the opaque per-spawn identifier the registry assigned
	// for this invocation. The adapter does not embed it in the
	// command line; it is the caller's responsibility to carry it into
	// the prompt or system prompt so the agent passes it back on every
	// MCP method call. Recorded here for symmetry with the role.
	AgentID string
}

// Executor invokes Claude Code in print mode and streams its stream-json
// events to a writer.
type Executor struct {
	cfg Config
}

// New returns a Claude Executor with cfg.
func New(cfg Config) *Executor {
	if cfg.CancelGrace == 0 {
		cfg.CancelGrace = 5 * time.Second
	}
	if cfg.MaxLineBytes == 0 {
		cfg.MaxLineBytes = 8 * 1024 * 1024
	}
	return &Executor{cfg: cfg}
}

// Run invokes the binary with print mode and stream-json output. For
// each line emitted on stdout the adapter parses it into zero or more
// agentcontract.AgentEvents and forwards each event on the events channel.
//
// On context cancellation the subprocess receives SIGINT first; if it
// fails to exit within CancelGrace it is killed via SIGKILL (handled by
// exec.Cmd via WaitDelay).
//
// Returns (ExecResult{ExitCode: 0}, nil) on natural completion. Returns
// (ExecResult{ExitCode: n}, nil) when the agent exits non-zero (a
// normal control-flow signal, not a Run failure). Returns (ExecResult,
// ctx.Err()) on cancellation. Returns (ExecResult{ExitCode: -1}, err)
// on invocation failure (binary missing, pipe setup error).
func (e *Executor) Run(ctx context.Context, prompt string, events chan<- agentcontract.AgentEvent) (loop.ExecResult, error) {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
	}

	// MCP wiring: when the run boot supplied an MCP URL, write a
	// per-spawn mcp-config pointing the agent at it with the role's
	// connection name carried via the X-BCC-Role header. The handler
	// (internal/director/dag) is the protocol of record; the
	// stream-json tool_use parser below stays in place for the TUI.
	if e.cfg.MCPURL != "" {
		path, cleanup, err := writeMCPConfig(e.cfg.MCPURL, e.cfg.MCPToken, e.cfg.MCPConnectionName)
		if err != nil {
			return loop.ExecResult{ExitCode: -1}, fmt.Errorf("mcp-config write: %w", err)
		}
		defer cleanup()
		args = append(args, "--mcp-config", path, "--strict-mcp-config")
	}

	// --system-prompt-file ships the bcc contract as the system message
	// under the Director. The per-iteration briefing arrives as the
	// user prompt via stdin (see below); the positional prompt argument
	// is omitted in that mode.
	if e.cfg.SystemPromptFile != "" {
		args = append(args, "--system-prompt-file", e.cfg.SystemPromptFile)
	}

	// --dangerously-skip-permissions is the precondition for autonomous
	// mode; without it claude prompts on every tool use. Users who set
	// skip_permissions=false in .bcc.toml accept that the loop will
	// stall on the first prompt.
	if e.cfg.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if e.cfg.Model != "" {
		args = append(args, "--model", e.cfg.Model)
	}
	if e.cfg.Effort != "" {
		args = append(args, "--effort", e.cfg.Effort)
	}
	args = append(args, e.cfg.ExtraArgs...)
	if e.cfg.SystemPromptFile == "" {
		args = append(args, prompt)
	} else if prompt == "" {
		return loop.ExecResult{ExitCode: -1}, errors.New("executor/claude: SystemPromptFile is set but prompt is empty; claude --print requires a user prompt")
	}

	cmd := exec.CommandContext(ctx, e.cfg.Binary, args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return loop.ExecResult{ExitCode: -1}, fmt.Errorf("stdout pipe: %w", err)
	}
	if e.cfg.SystemPromptFile != "" {
		// The contract lives in --system-prompt-file; the briefing is
		// the user prompt and goes on stdin so we are not bound by argv
		// length limits or escaping concerns.
		cmd.Stdin = strings.NewReader(prompt)
	}
	stderrTail := newRingBuffer(stderrCaptureBytes)
	if e.cfg.Stderr != nil {
		cmd.Stderr = io.MultiWriter(e.cfg.Stderr, stderrTail)
	} else {
		cmd.Stderr = stderrTail
	}
	cmd.Cancel = func() error {
		// Graceful interrupt; exec.Cmd escalates to SIGKILL after WaitDelay.
		return cmd.Process.Signal(syscall.SIGINT)
	}
	cmd.WaitDelay = e.cfg.CancelGrace

	if err := cmd.Start(); err != nil {
		return loop.ExecResult{ExitCode: -1}, fmt.Errorf("run %s: %w", e.cfg.Binary, err)
	}

	// Drain the stdout pipe in a goroutine. The pipe EOFs naturally when
	// the subprocess exits; on cancel, streamjson.Stream exits early via
	// ctx.Done so a slow consumer never blocks the cmd.Wait below.
	var src io.Reader = pipe
	if e.cfg.Stdout != nil {
		src = io.TeeReader(pipe, e.cfg.Stdout)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		streamjson.Stream(ctx, src, events, e.cfg.MaxLineBytes)
	}()

	// Per Cmd.StdoutPipe doc: callers must finish reading before Wait.
	wg.Wait()
	runErr := cmd.Wait()

	tail := strings.TrimSpace(stderrTail.String())

	if ctxErr := ctx.Err(); ctxErr != nil {
		exitCode := -1
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
		}
		return loop.ExecResult{ExitCode: exitCode, StderrTail: tail}, ctxErr
	}

	if runErr == nil {
		return loop.ExecResult{ExitCode: 0, StderrTail: tail}, nil
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		// Agent exited non-zero; that is a normal control-flow signal,
		// not a Run failure. Caller decides what to do; the captured
		// stderr tail rides along for diagnostics.
		return loop.ExecResult{ExitCode: ee.ExitCode(), StderrTail: tail}, nil
	}
	return loop.ExecResult{ExitCode: -1, StderrTail: tail}, fmt.Errorf("run %s: %w", e.cfg.Binary, runErr)
}

// writeMCPConfig persists a one-off mcp-config JSON pointing at the
// run-wide MCP server. The bearer token authenticates the connection;
// X-BCC-Role declares the role the agent is acting as so the handler's
// per-method allow-list can authorize each call. The file is created
// mode 0o600 in os.MkdirTemp; cleanup removes the directory.
func writeMCPConfig(url, token, connectionName string) (path string, cleanup func(), err error) {
	if connectionName == "" {
		return "", nil, errors.New("mcp-config: empty connection name")
	}
	dir, err := os.MkdirTemp("", "bcc-mcp-")
	if err != nil {
		return "", nil, err
	}
	path = filepath.Join(dir, "mcp-config.json")
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"bcc": map[string]any{
				"type": "http",
				"url":  url,
				"headers": map[string]string{
					"Authorization": "Bearer " + token,
					"X-BCC-Role":    connectionName,
				},
			},
		},
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	return path, cleanup, nil
}
