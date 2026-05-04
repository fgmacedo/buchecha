// Package claude implements the Director's Planner, Briefer, and
// Reviewer ports against the claude CLI.
//
// Each role is invoked as an interactive agent with a fixed read-only
// tool envelope:
//
//	claude -p --output-format stream-json --verbose
//	  --allowed-tools Read,Bash,Grep,Glob
//	  --dangerously-skip-permissions
//	  --mcp-config <per-spawn-tempfile> --strict-mcp-config
//	  [--model <m>] [--max-budget-usd <n>]
//	  <prompt>
//
// The prompt is the role's system prompt (plan.md / brief.md / review.md
// in internal/director/prompts), composed with the agentcontract
// partials and the per-role view data (AgentID, SpecPath, iteration
// metadata). Structured output never crosses stdout: the agent emits
// the Plan, Briefing, and per-task verdicts via MCP method calls, and
// bcc reads them from the run-wide handler. The adapter only watches
// the stream-json `result` event for cost stats and budget enforcement.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/director/dag"
	"github.com/fgmacedo/buchecha/internal/executor/claude/streamjson"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// stderrCaptureBytes caps how much of claude's stderr the adapter
// holds in memory per call. Small enough to be safe under runaway
// output, large enough to carry a useful tail to the error message
// (typically a few exit reasons such as quota / auth / model errors).
const stderrCaptureBytes = 8 * 1024

// ringBuffer keeps the last N bytes written to it. Older bytes are
// dropped silently. Used to capture a small tail of the agent's
// stderr so a non-zero exit can surface a human-readable reason.
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

// Compile-time checks that *Adapter satisfies the three Director ports.
var (
	_ director.Planner  = (*Adapter)(nil)
	_ director.Briefer  = (*Adapter)(nil)
	_ director.Reviewer = (*Adapter)(nil)
)

// ErrBudgetExceeded is returned when claude reports a per-call cost
// above MaxBudgetUSD. The loop treats this as a fail-closed signal: the
// affected role escalates rather than the run silently overspending.
var ErrBudgetExceeded = errors.New("director/claude: per-call budget exceeded")

// ErrMissingAgentID is returned when a Director call arrives without an
// AgentID populated on the input. The CLI/loop is expected to register
// the agent with the run-wide registry before invoking the adapter and
// pass the assigned id back in.
var ErrMissingAgentID = errors.New("director/claude: missing agent_id on input")

// Config configures the Director Claude adapter.
type Config struct {
	// Binary is the path or PATH name of the claude binary.
	Binary string

	// Model is passed via --model. Empty omits the flag. Per-phase
	// role assignments (briefer_assignment, reviewer_assignment) override
	// this on individual Brief/Review calls.
	Model string

	// Effort is the default effort level passed via --effort when no
	// per-call assignment overrides it. Empty omits the flag.
	Effort string

	// ExtraArgs are appended to the command line after the protocol
	// flags and --max-budget-usd, before the prompt positional argument.
	ExtraArgs []string

	// MaxBudgetUSD, when > 0, is passed to claude as --max-budget-usd
	// and also enforced on the bcc side: a result event reporting cost
	// above this cap aborts the call with ErrBudgetExceeded. Zero (the
	// default) disables both behaviors.
	MaxBudgetUSD float64

	// Stderr, when non-nil, receives the subprocess stderr verbatim.
	// Default (nil) discards stderr.
	Stderr io.Writer

	// StderrFactory, when non-nil, is called per spawn to obtain an
	// additional WriteCloser that receives the subprocess stderr alongside
	// the global Stderr and the in-memory tail. The adapter closes the
	// returned writer after Wait. Returning a nil writer with a nil error
	// is allowed and is equivalent to a no-op for that spawn.
	//
	// role identifies which Director role is being spawned. iterationID
	// is the briefing's iteration id when the spawn is in-iteration
	// (Brief, Review); empty for the Planner. agentID is the per-spawn
	// registry id.
	StderrFactory func(role, iterationID, agentID string) (io.WriteCloser, error)

	// StdoutFactory mirrors StderrFactory for the raw stream-json stdout
	// pipe. When set, the adapter tees the pipe into the returned writer
	// before parsing so the file contains the unmodified line stream.
	StdoutFactory func(role, iterationID, agentID string) (io.WriteCloser, error)

	// CancelGrace is how long to wait after sending SIGINT before
	// forcing SIGKILL. Defaults to 5 seconds when zero.
	CancelGrace time.Duration

	// MaxLineBytes caps a single stream-json line. Defaults to 8 MiB
	// when zero.
	MaxLineBytes int

	// MCPURL is the http://127.0.0.1:port/mcp/ endpoint of the run-wide
	// MCP handler mounted on the shared API listener. The adapter writes
	// a per-spawn mcp-config pointing at it verbatim with the role's
	// connection name carried in the X-BCC-Role header; the trailing
	// slash matters because chi mounts the handler at /mcp and strips
	// the prefix, so agents must hit /mcp/ to land inside the mount.
	// Empty disables the --mcp-config wiring; useful for tests against
	// fake-claude scripts that do not connect.
	MCPURL string

	// MCPToken is the bearer token the agent presents in Authorization
	// for every MCP request. Required when MCPURL is set.
	MCPToken string

	// now, when non-nil, replaces time.Now in tests for deterministic
	// stats timing. Always nil in production.
	now func() time.Time
}

// Adapter is the Director Claude adapter. A zero-value Adapter is
// invalid; use New.
type Adapter struct {
	cfg Config
}

// New returns an Adapter with the given config.
func New(cfg Config) *Adapter {
	if cfg.CancelGrace == 0 {
		cfg.CancelGrace = 5 * time.Second
	}
	if cfg.MaxLineBytes == 0 {
		cfg.MaxLineBytes = 8 * 1024 * 1024
	}
	if cfg.Binary == "" {
		cfg.Binary = "claude"
	}
	return &Adapter{cfg: cfg}
}

// SetStderrFactory installs a per-spawn stderr sink factory after
// construction. nil disables the factory and reverts to the global
// Stderr only. Used by the cli to wire .bcc/sessions/<id>/runs/ capture
// once the session has been resolved.
func (a *Adapter) SetStderrFactory(fn func(role, iterationID, agentID string) (io.WriteCloser, error)) {
	a.cfg.StderrFactory = fn
}

// SetStdoutFactory installs a per-spawn stdout-tee factory after
// construction. nil disables the factory.
func (a *Adapter) SetStdoutFactory(fn func(role, iterationID, agentID string) (io.WriteCloser, error)) {
	a.cfg.StdoutFactory = fn
}

// planView, briefView, and reviewView are the per-role data the prompt
// templates render against. They are kept narrow so a future template
// edit cannot accidentally surface fields the role should not see.
type planView struct {
	AgentID  string
	SpecPath string
	Registry director.CapabilityRegistry
}

type briefView struct {
	AgentID     string
	SpecPath    string
	IterationID string
	PhaseID     string
	Attempt     int
}

type reviewView struct {
	AgentID  string
	SpecPath string
}

// Plan implements director.Planner. It renders the planner prompt, runs
// claude with the planner connection name, and returns once the agent
// exits cleanly. The adapter never returns the Plan itself: the agent
// emits it via bcc_plan_emit, and the run-wide handler stores it.
// Callers read the Plan from the dag handler/store after Plan returns.
//
// events, when non-nil, receives the planner's stream telemetry
// (thinking, tool calls, assistant text, rate-limit, result summary).
// The adapter never closes events; the caller owns it.
func (a *Adapter) Plan(ctx context.Context, in director.PlannerInput, events chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
	if in.AgentID == "" {
		return nil, nil, ErrMissingAgentID
	}
	prompt, err := composePrompt(director.PlanPromptTemplate(), planView{
		AgentID:  in.AgentID,
		SpecPath: in.SpecPath,
		Registry: in.Registry,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("director/claude: compose plan prompt: %w", err)
	}
	stats, err := a.runRole(ctx, dag.RolePlanner, in.AgentID, "", prompt, events, "", "")
	return nil, stats, err
}

// Brief implements director.Briefer. Same shape as Plan: render, run,
// return. The Briefing lands in the dag handler via bcc_briefing_emit.
// in.Assignment, when non-nil, overrides the configured Model and
// Effort on this call so per-phase capability routing flows through to
// the spawned claude process.
func (a *Adapter) Brief(ctx context.Context, in director.BrieferInput, events chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
	if in.AgentID == "" {
		return nil, ErrMissingAgentID
	}
	prompt, err := composePrompt(director.BriefPromptTemplate(), briefView{
		AgentID:     in.AgentID,
		SpecPath:    in.SpecPath,
		IterationID: in.IterationID,
		PhaseID:     in.PhaseID,
		Attempt:     in.Attempt,
	})
	if err != nil {
		return nil, fmt.Errorf("director/claude: compose brief prompt: %w", err)
	}
	model, effort := assignmentOverride(in.Assignment)
	return a.runRole(ctx, dag.RoleBriefer, in.AgentID, in.IterationID, prompt, events, model, effort)
}

// Review implements director.Reviewer. The Reviewer's work is recorded
// as DAG mutations (bcc_task_approved / bcc_task_needs_fix) plus a
// final bcc_review_finished outcome; the handler is the source of
// truth for the resulting state. in.Assignment, when non-nil, overrides
// the configured Model and Effort on this call.
func (a *Adapter) Review(ctx context.Context, in director.ReviewerInput, events chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
	if in.AgentID == "" {
		return nil, ErrMissingAgentID
	}
	prompt, err := composePrompt(director.ReviewPromptTemplate(), reviewView{
		AgentID:  in.AgentID,
		SpecPath: "",
	})
	if err != nil {
		return nil, fmt.Errorf("director/claude: compose review prompt: %w", err)
	}
	model, effort := assignmentOverride(in.Assignment)
	return a.runRole(ctx, dag.RoleReviewer, in.AgentID, in.IterationID, prompt, events, model, effort)
}

// assignmentOverride extracts the per-call (model, effort) pair from a
// RoleAssignment. nil or empty fields produce empty strings, which
// runRole reads as "use the configured default".
func assignmentOverride(a *director.RoleAssignment) (string, string) {
	if a == nil {
		return "", ""
	}
	return a.Model, a.Effort
}

// composePrompt expands a role's prompt template with the agentcontract
// partials and the per-role view data. Pure text; no I/O.
func composePrompt(promptTpl string, view any) (string, error) {
	t := agentcontract.Partials()
	if _, err := t.New("role").Parse(promptTpl); err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "role", view); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

// runRole spawns claude with the Director envelope for the given role
// and waits for it to exit. The agent emits structured output via MCP;
// the adapter parses stream-json into agentcontract.AgentEvents,
// forwards them to events when non-nil, and derives DirectorCallStats
// from the terminal KindResultSummary so the cost panel and budget
// check share a single source of truth.
// runRole spawns claude for one role invocation. modelOverride and
// effortOverride, when non-empty, replace the adapter's configured
// values for this single spawn so per-phase capability assignments
// flow into the CLI flags. Empty strings preserve the configured
// defaults.
func (a *Adapter) runRole(ctx context.Context, role dag.Role, agentID, iterationID, prompt string, events chan<- agentcontract.AgentEvent, modelOverride, effortOverride string) (*director.DirectorCallStats, error) {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--allowed-tools", "Read,Bash,Grep,Glob",
		"--dangerously-skip-permissions",
	}

	if a.cfg.MCPURL != "" {
		path, cleanup, err := writeMCPConfig(a.cfg.MCPURL, a.cfg.MCPToken, string(role))
		if err != nil {
			return nil, fmt.Errorf("director/claude: write mcp-config: %w", err)
		}
		defer cleanup()
		args = append(args, "--mcp-config", path, "--strict-mcp-config")
	}

	model := a.cfg.Model
	if modelOverride != "" {
		model = modelOverride
	}
	effort := a.cfg.Effort
	if effortOverride != "" {
		effort = effortOverride
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	if a.cfg.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(a.cfg.MaxBudgetUSD, 'f', -1, 64))
	}
	args = append(args, a.cfg.ExtraArgs...)
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, a.cfg.Binary, args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("director/claude: stdout pipe: %w", err)
	}
	var stdoutSink io.WriteCloser
	if a.cfg.StdoutFactory != nil {
		stdoutSink, err = a.cfg.StdoutFactory(string(role), iterationID, agentID)
		if err != nil {
			return nil, fmt.Errorf("director/claude: open stdout sink: %w", err)
		}
	}
	defer func() {
		if stdoutSink != nil {
			_ = stdoutSink.Close()
		}
	}()
	stderrTail := newRingBuffer(stderrCaptureBytes)
	stderrWriters := []io.Writer{stderrTail}
	if a.cfg.Stderr != nil {
		stderrWriters = append(stderrWriters, a.cfg.Stderr)
	}
	var stderrSink io.WriteCloser
	if a.cfg.StderrFactory != nil {
		stderrSink, err = a.cfg.StderrFactory(string(role), iterationID, agentID)
		if err != nil {
			return nil, fmt.Errorf("director/claude: open stderr sink: %w", err)
		}
		if stderrSink != nil {
			stderrWriters = append(stderrWriters, stderrSink)
		}
	}
	defer func() {
		if stderrSink != nil {
			_ = stderrSink.Close()
		}
	}()
	cmd.Stderr = io.MultiWriter(stderrWriters...)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGINT)
	}
	cmd.WaitDelay = a.cfg.CancelGrace

	start := a.timeNow()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("director/claude: run %s: %w", a.cfg.Binary, err)
	}

	var (
		mu     sync.Mutex
		stats  director.DirectorCallStats
		latest *agentcontract.ResultSummaryInfo
	)

	parseDone := make(chan struct{})
	go func() {
		defer close(parseDone)
		var src io.Reader = pipe
		if stdoutSink != nil {
			src = io.TeeReader(pipe, stdoutSink)
		}
		sc := bufio.NewScanner(src)
		sc.Buffer(make([]byte, 64*1024), a.cfg.MaxLineBytes)
		for sc.Scan() {
			line := slices.Clone(sc.Bytes())
			for _, ev := range streamjson.ParseLine(line, a.timeNow()) {
				if ev.Kind == agentcontract.KindResultSummary && ev.Done != nil {
					mu.Lock()
					done := *ev.Done
					latest = &done
					mu.Unlock()
				}
				if events != nil {
					select {
					case events <- ev:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	<-parseDone
	runErr := cmd.Wait()
	stats.DurationMS = a.timeNow().Sub(start).Milliseconds()

	if latest != nil {
		stats.CostUSD = latest.TotalCostUSD
		stats.InputTokens = latest.InputTokens
		stats.OutputTokens = latest.OutputTokens
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return &stats, ctxErr
	}
	if runErr != nil {
		tail := strings.TrimSpace(stderrTail.String())
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			if tail != "" {
				return &stats, fmt.Errorf("director/claude: %s exited %d: %s", a.cfg.Binary, ee.ExitCode(), tail)
			}
			return &stats, fmt.Errorf("director/claude: %s exited %d", a.cfg.Binary, ee.ExitCode())
		}
		if tail != "" {
			return &stats, fmt.Errorf("director/claude: run %s: %w: %s", a.cfg.Binary, runErr, tail)
		}
		return &stats, fmt.Errorf("director/claude: run %s: %w", a.cfg.Binary, runErr)
	}

	if a.cfg.MaxBudgetUSD > 0 && stats.CostUSD > a.cfg.MaxBudgetUSD {
		return &stats, fmt.Errorf("%w: cost=%.4f cap=%.4f", ErrBudgetExceeded, stats.CostUSD, a.cfg.MaxBudgetUSD)
	}
	return &stats, nil
}

func (a *Adapter) timeNow() time.Time {
	if a.cfg.now != nil {
		return a.cfg.now()
	}
	return time.Now()
}
