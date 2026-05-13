// Package codex implements provider.Provider for OpenAI's codex CLI
// (`codex exec --json`).
//
// # codex-cli 0.130.0 wire details (relevant findings)
//
// The argv assembly below was grounded in `codex exec --help` on 0.130.0
// and in captured fixtures under jsonl/testdata/. Four details are worth
// recording because the codex spec was drafted before the 0.130 surface
// stabilised:
//
//   - `--ask-for-approval` (`-a`) is NOT an `exec` subcommand flag in
//     0.130; it lives at the top level. Any `-a` value must precede the
//     `exec` subcommand. Older drafts of the spec showed
//     `exec --ask-for-approval never`, which fails today with
//     "unexpected argument".
//
//   - `-a` controls approval for SHELL commands, not MCP tool calls.
//     MCP tool calls go through a SEPARATE per-server gate read from
//     `mcp_servers.<name>.default_tools_approval_mode` (AppToolApproval
//     enum: auto | prompt | approve). With the default (prompt) codex
//     cancels every MCP call with "user cancelled MCP tool call",
//     silently breaking the bcc wire (Planner cannot emit plan_emit,
//     Briefer cannot emit briefing_emit, Reviewer cannot emit
//     task_approved). The adapter therefore sets the bcc entry to
//     `default_tools_approval_mode="approve"` whenever MCP wiring is
//     present, which pre-approves every tool advertised by the bcc MCP
//     server while keeping the seatbelt sandbox active for shell
//     commands (preferable to --dangerously-bypass-approvals-and-sandbox
//     which would drop the sandbox too). Empirically validated
//     2026-05-13 against a smoke HTTP MCP server with codex 0.130.0:
//     with `-a never` alone the call is cancelled; with `-a never` plus
//     `mcp_servers.bcc.default_tools_approval_mode="approve"` codex
//     calls tools/call and the turn metadata reports `"sandbox":"seatbelt"`.
//
//   - codex does NOT consume a JSON mcp-config file. MCP servers are
//     configured exclusively through `~/.codex/config.toml`. For per-spawn
//     isolation the adapter passes `--ignore-user-config` and supplies the
//     entry via `-c` overrides (one per dotted key). spawnkit.WriteMCPConfig
//     stays claude-specific.
//
//   - Custom HTTP headers ARE supported via `-c
//     'mcp_servers.bcc.http_headers={X-BCC-Role="bcc-executor"}'`.
//     Confirmed by `codex mcp add bcc-test --url ...` followed by
//     inspecting the resulting [mcp_servers.bcc-test] block in
//     ~/.codex/config.toml: the schema accepts `url`,
//     `bearer_token_env_var`, `http_headers`, and `env_http_headers`.
//     The adapter routes BOTH the Authorization Bearer token AND the
//     X-BCC-Role label through a single `http_headers` inline table.
//     `bearer_token` is NOT supported for streamable_http in codex 0.130
//     (error: "uses unsupported bearer_token; set bearer_token_env_var");
//     using http_headers is the correct path for this transport type.
//
// # Effort flag
//
// codex 0.130 has no direct equivalent of bcc's per-spawn `Effort` knob
// (the closest match would be `model_reasoning_effort` in config.toml,
// which is a static run-wide setting). The adapter therefore IGNORES
// SpawnRequest.Effort. Future codex versions may add a CLI flag; until
// then the value is documented and dropped.
package codex

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/provider"
	"github.com/fgmacedo/buchecha/internal/provider/codex/jsonl"
	"github.com/fgmacedo/buchecha/internal/provider/spawnkit"
)

// stderrCaptureBytes caps how much of codex's stderr the adapter holds
// in memory per spawn. Large enough to carry a useful tail (auth /
// quota / model errors), small enough to be safe under runaway output.
const stderrCaptureBytes = 8 * 1024

// defaultCancelGrace is the SIGINT -> SIGKILL escalation window used
// when Config.CancelGrace is zero.
const defaultCancelGrace = 5 * time.Second

// defaultMaxLineBytes is the per-line cap for the JSONL scanner when
// Config.MaxLineBytes is zero. Generous because codex tool results can
// carry multi-KB aggregated_output payloads on a single line.
const defaultMaxLineBytes = 8 * 1024 * 1024

// Compile-time check that *Codex satisfies provider.Provider.
var _ provider.Provider = (*Codex)(nil)

// Config configures the codex provider. Per-spawn values (model, prompt,
// MCP spec, ...) arrive on SpawnRequest; Config carries run-wide knobs
// that do not change between spawns.
type Config struct {
	// Binary is the path or PATH name of the codex binary. Empty
	// defaults to "codex".
	Binary string

	// ExtraArgs are appended to every argv after the per-spawn flags
	// and before the trailing stdin sentinel. They concatenate with
	// SpawnRequest.ExtraArgs.
	ExtraArgs []string

	// Stderr, when non-nil, receives the subprocess stderr verbatim in
	// addition to the in-memory tail. Default (nil) keeps only the
	// in-memory tail.
	Stderr io.Writer

	// Stdout, when non-nil, is teed from the subprocess stdout pipe
	// before the JSONL parser consumes it. Useful for raw line captures
	// alongside the parsed AgentEvents.
	Stdout io.Writer

	// CancelGrace is how long to wait after sending SIGINT before forcing
	// SIGKILL via exec.Cmd.WaitDelay. Zero uses defaultCancelGrace.
	CancelGrace time.Duration

	// MaxLineBytes caps a single JSONL line. Zero uses defaultMaxLineBytes.
	MaxLineBytes int
}

// Codex implements provider.Provider for the codex CLI. Construct with
// New; the zero value is invalid.
type Codex struct {
	cfg Config
}

// New returns a Codex provider with the given Config. Zero-valued
// Binary, CancelGrace, and MaxLineBytes get their defaults.
func New(cfg Config) *Codex {
	if cfg.Binary == "" {
		cfg.Binary = "codex"
	}
	if cfg.CancelGrace == 0 {
		cfg.CancelGrace = defaultCancelGrace
	}
	if cfg.MaxLineBytes == 0 {
		cfg.MaxLineBytes = defaultMaxLineBytes
	}
	return &Codex{cfg: cfg}
}

// Name returns "codex". It matches the .bcc.toml [providers.<name>] key
// and the RoleAssignment.Provider string the Planner emits.
func (*Codex) Name() string { return "codex" }

// Spawn runs the codex CLI per the supplied request, streams JSONL
// telemetry into req.Events as agentcontract.AgentEvents, emits
// loop.SpawnStarted / SpawnFinished onto req.LoopEvents (typed assertion
// to chan<- loop.Event), and returns the spawn result.
//
// The argv is assembled as:
//
//	codex [-a never]
//	  exec --json --ignore-user-config --skip-git-repo-check --ephemeral
//	  [-c 'mcp_servers.bcc.url="..."']
//	  [-c 'mcp_servers.bcc.http_headers={Authorization="Bearer ...", X-BCC-Role="..."}']
//	  [-s read-only|workspace-write|danger-full-access]
//	  [-m <model>]
//	  <Config.ExtraArgs...> <req.ExtraArgs...>
//	  -
//
// The trailing `-` instructs codex to read the prompt from stdin; the
// adapter sets Stdin to SystemPrompt + Prompt so the system instructions
// land before the user turn body. SkipPermissions maps to top-level
// `-a never`; Effort is ignored on codex 0.130 (documented).
//
// Cancellation: ctx propagates via exec.CommandContext; Cancel sends
// SIGINT first and WaitDelay (CancelGrace) escalates to SIGKILL when the
// process refuses to exit.
func (c *Codex) Spawn(ctx context.Context, req provider.SpawnRequest) (provider.SpawnResult, error) {
	args := assembleArgs(c.cfg, req)

	fullPrompt := composePrompt(req)
	if fullPrompt == "" {
		return provider.SpawnResult{ExitCode: -1}, errors.New("provider/codex: empty prompt; codex exec - requires stdin input")
	}

	cmd := exec.CommandContext(ctx, c.cfg.Binary, args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return provider.SpawnResult{ExitCode: -1}, fmt.Errorf("provider/codex: stdout pipe: %w", err)
	}
	cmd.Stdin = strings.NewReader(fullPrompt)
	stderrTail := spawnkit.NewRingBuffer(stderrCaptureBytes)
	if c.cfg.Stderr != nil {
		cmd.Stderr = io.MultiWriter(c.cfg.Stderr, stderrTail)
	} else {
		cmd.Stderr = stderrTail
	}
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGINT)
	}
	cmd.WaitDelay = c.cfg.CancelGrace

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
	spawnID := spawnkit.NewSpawnID()
	promptPath, persisted, perr := spawnkit.PersistPromptFromAny(req.SessionStore, spawnID, fullPrompt)
	if perr != nil {
		return provider.SpawnResult{ExitCode: -1}, perr
	}
	if persisted {
		info.SpawnID = spawnID
		spawnkit.EmitSpawnStartedAny(req.LoopEvents, info, promptPath, time.Now().UTC())
	}

	spawnStartedAt := time.Now()
	if startErr := cmd.Start(); startErr != nil {
		return provider.SpawnResult{ExitCode: -1, SpawnID: info.SpawnID},
			fmt.Errorf("provider/codex: run %s: %w", c.cfg.Binary, startErr)
	}

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
			evs, _ := jsonl.ParseLine(line, time.Now())
			for _, ev := range evs {
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
	if summary, ok := jsonl.LastResultSummary(parsedEvents); ok {
		result.CostUSD = summary.TotalCostUSD
		result.Tokens = summary.Tokens
		// codex turn.completed has no duration_ms today; leave the
		// wall-clock DurationMS measured above untouched when the
		// summary's value is zero.
		if summary.DurationMS > 0 {
			result.DurationMS = summary.DurationMS
		}
	}

	if info.SpawnID != "" {
		spawnkit.EmitSpawnFinishedAny(req.LoopEvents, info, result, time.Now().UTC())
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
	return result, fmt.Errorf("provider/codex: run %s: %w", c.cfg.Binary, runErr)
}

// assembleArgs builds the codex argv per the documented mapping. Kept as
// a free function so unit tests can exercise it directly via the stub
// binary path.
func assembleArgs(cfg Config, req provider.SpawnRequest) []string {
	args := make([]string, 0, 32)
	// Top-level codex flags BEFORE the exec subcommand. -a controls
	// shell-command approval only; MCP tool approval is configured
	// per-server via default_tools_approval_mode below.
	if req.SkipPermissions {
		args = append(args, "-a", "never")
	}
	args = append(args,
		"exec",
		"--json",
		"--ignore-user-config",
		"--skip-git-repo-check",
		"--ephemeral",
	)
	if req.MCP.URL != "" {
		args = append(args,
			"-c", fmt.Sprintf(`mcp_servers.bcc.url=%q`, req.MCP.URL),
		)
		// codex 0.130 streamable_http does not support the bearer_token field;
		// route both the Authorization token and the X-BCC-Role label through
		// http_headers so the MCP auth middleware accepts the request.
		var headerParts []string
		if req.MCP.Token != "" {
			headerParts = append(headerParts,
				fmt.Sprintf(`Authorization=%q`, "Bearer "+req.MCP.Token),
			)
		}
		if req.MCP.ConnectionName != "" {
			headerParts = append(headerParts,
				fmt.Sprintf(`X-BCC-Role=%q`, req.MCP.ConnectionName),
			)
		}
		if len(headerParts) > 0 {
			args = append(args,
				"-c", fmt.Sprintf(`mcp_servers.bcc.http_headers={%s}`, strings.Join(headerParts, ", ")),
			)
		}
		// Pre-approve every tool advertised by this MCP server so codex
		// does not stall on its elicitation gate. The per-server
		// AppToolApproval enum (auto | prompt | approve) is read from
		// mcp_servers.<name>.default_tools_approval_mode in
		// codex-rs/core/src/mcp_tool_call.rs:custom_mcp_tool_approval_mode.
		// Without this, MCP calls cancel with "user cancelled MCP tool
		// call" even when SkipPermissions is true and `-a never` is set,
		// because `-a` only governs shell-command approvals.
		args = append(args,
			"-c", `mcp_servers.bcc.default_tools_approval_mode="approve"`,
		)
	}
	switch req.Sandbox {
	case provider.SandboxReadOnly:
		args = append(args, "-s", "read-only")
	case provider.SandboxWorkspaceWrite:
		args = append(args, "-s", "workspace-write")
	case provider.SandboxDangerFullAccess:
		args = append(args, "-s", "danger-full-access")
	}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	// req.Effort is intentionally NOT mapped on codex 0.130; see package doc.
	args = append(args, cfg.ExtraArgs...)
	args = append(args, req.ExtraArgs...)
	// Trailing `-` tells codex to read the prompt from stdin even when
	// it has positional args earlier.
	args = append(args, "-")
	return args
}

// composePrompt concatenates SystemPrompt and Prompt with a visible
// separator so the system instructions arrive before the user turn body
// on stdin. The separator is a markdown horizontal rule which codex
// passes verbatim to the model; the model treats prose before the rule
// as scaffolding.
func composePrompt(req provider.SpawnRequest) string {
	switch {
	case req.SystemPrompt == "":
		return req.Prompt
	case req.Prompt == "":
		return req.SystemPrompt
	default:
		return req.SystemPrompt + "\n\n---\n\n" + req.Prompt
	}
}

// resolveOriginRole returns the canonical agentcontract.Role for use in
// AgentEvent.WithOrigin given the caller-supplied req.Role string. The
// SpawnStarted / SpawnFinished events carry the raw req.Role verbatim;
// this helper maps the short form (e.g. "executor") to the agent-side
// canonical Role ("bcc-executor") when recognised, otherwise returns
// whatever the caller supplied unchanged.
func resolveOriginRole(s string) agentcontract.Role {
	if r := agentcontract.Role(s); r.Valid() {
		return r
	}
	if r := agentcontract.Role("bcc-" + s); r.Valid() {
		return r
	}
	return agentcontract.Role(s)
}
