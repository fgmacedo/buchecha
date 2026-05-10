package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/fgmacedo/buchecha/internal/config"
	"github.com/fgmacedo/buchecha/internal/executor/claude"
	gitcli "github.com/fgmacedo/buchecha/internal/git/cli"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/services"
	"github.com/fgmacedo/buchecha/internal/supervision"
	directorclaude "github.com/fgmacedo/buchecha/internal/supervision/claude"
	"github.com/fgmacedo/buchecha/internal/supervision/dag"
	"github.com/fgmacedo/buchecha/internal/supervision/journal"
	"github.com/fgmacedo/buchecha/internal/supervision/menu"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
	"github.com/fgmacedo/buchecha/internal/supervision/stats"
	"github.com/fgmacedo/buchecha/internal/tui"
	"github.com/fgmacedo/buchecha/internal/webui"
)

// errPlannerSkipped is returned by freshPlan / resolveDirectorPlan when
// the Planner declared the spec done by calling plan_skip. It is a
// success path: callers map it to ExitDone and surface a friendly
// "nothing to do" message instead of a fatal error.
type errPlannerSkipped struct {
	reason string
}

func (e errPlannerSkipped) Error() string {
	if e.reason == "" {
		return "director: planner skipped: nothing to do"
	}
	return "director: planner skipped: " + e.reason
}

// directorDeps groups the ports runDirector consumes. Production code
// builds Claude adapters; tests inject scripted fakes. Session
// resolution happens inside runDirectorWith from baseDir + the dio
// flags, unless a test pre-populates store, in which case that store
// is used as-is and no session helpers are consulted. newExecutor is
// a factory because the Executor is parameterized per-phase by the
// briefing prompt file path, which only exists after the Briefer has
// produced a Briefing.
type directorDeps struct {
	cfg         *config.Config
	planner     supervision.Planner
	briefer     supervision.Briefer
	reviewer    supervision.Reviewer
	registerFn  func(role dag.Role) (string, func(), error)
	baseDir     string
	store       *session.Store
	git         loop.GitProbe
	newExecutor func(args dag.RegisterArgs, renderSystem func(agentID string) (string, error), assignment *supervision.RoleAssignment) loop.Executor
	now         func() time.Time
	// handler, when non-nil, overrides the run-wide MCP handler the
	// loop receives in DirectorPorts. Tests inject one to drive the
	// loop without standing up the full MCP boot.
	handler *dag.Handler
	// boot is the run-wide MCP plumbing the cli wired in
	// runDirector. The deps struct keeps a back-reference so
	// runDirectorWith can call bindSession after session resolution
	// without re-constructing the boot. Tests leave this nil and skip
	// the bind step.
	boot *mcpBoot
	// stats, when non-nil, persists per-role spawn telemetry to
	// stats.jsonl in the session directory. Bound after session
	// resolution; tests typically leave nil to opt out.
	stats *stats.StatsLog
	// serviceEvents, when non-nil, is the long-lived loop.Event channel
	// the services aggregator consumes. Per-run tees forward every loop
	// event onto it (non-blocking) so /api/v1/sessions/{id}/events
	// subscribers see the live stream. Tests leave nil to opt out.
	serviceEvents chan<- loop.Event
	// svc, when non-nil, is the application services aggregator the TUI
	// subscribes to for its event stream instead of a raw channel.
	// Wired after services.New in runDirector; tests leave nil.
	svc *services.Services
	// webuiURL is the run-wide dashboard URL (with the session token)
	// the TUI's [w] keybinding launches the browser at. Empty disables
	// the binding; tests leave nil.
	webuiURL string
	// openBrowser is the platform-aware launcher the TUI's [w]
	// keybinding calls. Nil disables the binding; tests leave nil.
	openBrowser func(url string) error
	// prompt is the free-form user directive supplied via `bcc run --prompt`.
	// Threaded into PlannerInput.Prompt and persisted on Session.Prompt.
	prompt string
}

// directorIO captures the I/O surface so tests can drive escalation
// and resume flows without touching os.Stdin / os.Stderr. The session
// and resume flags drive how runDirectorWith resolves the per-run
// Store; the spec's first acceptance pins the matrix.
type directorIO struct {
	stdin   io.Reader
	stderr  io.Writer
	resume  bool
	session string
}

// runDirector is the entry point for the Director-driven loop, called
// from runSpec when [director].enabled is on. P4 wires planning,
// persistence, and user confirmation; the brief/execute/review pipeline
// lands in P5-P7.
func runDirector(ctx context.Context, cancel context.CancelFunc, specPath, prompt string, cfg *config.Config) error {
	dio := directorIO{
		stdin:   os.Stdin,
		stderr:  os.Stderr,
		resume:  runResume,
		session: runSessionID,
	}

	// Resolve (or create) the session and build the run-wide MCP boot
	// before the listener so the services aggregator the api.Server
	// consumes has a SessionStore and DAGHandler from t=0. Without this
	// the read-only API endpoints would respond "services not configured"
	// for the entire run.
	store, err := resolveDirectorSessionEarly(specPath, prompt, dio)
	if err != nil {
		ExitCode = loop.ExitInvalid
		return err
	}
	boot, err := newMCPBoot(nil)
	if err != nil {
		ExitCode = loop.ExitInvalid
		return err
	}

	// serviceEvents is the long-lived loop.Event channel the services
	// aggregator subscribes to. Per-loop-run tees forward every event
	// onto it without closing it so subscriber lifetimes are decoupled
	// from any single l.Run invocation. The bcc run's defer closes the
	// channel after the listener tears down.
	serviceEvents := make(chan loop.Event, 256)
	defer close(serviceEvents)

	svc := services.New(services.Deps{
		DAGHandler:      boot.handler,
		SessionStore:    store,
		SessionsBaseDir: filepath.Join(".bcc", "sessions"),
		AuditPath:       directorAuditPath(cfg, store),
		EventsLogPath:   directorEventsLogPath(cfg, store),
		LoopEvents:      serviceEvents,
	})

	webuiHandler := resolveWebUIHandler(runWebUIDev)
	listener, err := startRunListener(ctx, boot, svc, webuiHandler, runListenerBind())
	if err != nil {
		ExitCode = loop.ExitInvalid
		return err
	}
	defer func() {
		if cerr := listener.Stop(); cerr != nil {
			slog.Warn("cli: run listener stop", "err", cerr)
		}
	}()

	// --webui implies --api at the banner level: a webui run is by
	// definition api-enabled because the SPA depends on /api/v1/* on
	// the same listener. The banner already prefers webui over api when
	// both are set; promoting api here keeps the wiring honest if the
	// user opted into --webui without --api.
	apiBanner := runAPI || cfg.Webui.Enabled
	webuiBanner := cfg.Webui.Enabled
	printRunBanner(os.Stderr, listener.addr, listener.sessionToken, apiBanner, webuiBanner)

	if cfg.Webui.Open {
		// Best-effort browser launch: --webui-open is opt-in sugar; a
		// failure here must not derail the run. openBrowser logs a Warn
		// slog entry on its own; we discard the error after that.
		_ = openBrowser(dashboardURL(listener.addr, listener.sessionToken))
	}

	deps := defaultDirectorDeps(cfg, listener.boot)
	deps.boot = listener.boot
	deps.store = store
	deps.serviceEvents = serviceEvents
	deps.svc = svc
	deps.webuiURL = dashboardURL(listener.addr, listener.sessionToken)
	deps.openBrowser = openBrowser
	deps.prompt = prompt
	return runDirectorWith(ctx, cancel, specPath, cfg, deps, dio)
}

// resolveDirectorSessionEarly hashes the spec (when specPath is set) and
// resolves the session store before the run-wide HTTP listener binds.
// Pre-resolving the session means the services aggregator passed to api.New
// has a live SessionStore from t=0. When specPath is empty (prompt-only run),
// os.ReadFile is skipped and the hash is derived from the prompt alone.
func resolveDirectorSessionEarly(specPath, prompt string, dio directorIO) (*session.Store, error) {
	var content []byte
	if specPath != "" {
		var err error
		content, err = os.ReadFile(specPath)
		if err != nil {
			return nil, fmt.Errorf("director: read spec %s: %w", specPath, err)
		}
	}
	hash := supervision.ComputeSessionHash(content, prompt)
	deps := directorDeps{baseDir: ".bcc", now: time.Now, prompt: prompt}
	store, err := resolveDirectorSession(deps, dio, specPath, hash)
	if err != nil {
		return nil, err
	}
	return store, nil
}

// directorAuditPath returns the per-session MCP audit log path when the
// audit toggle is on, otherwise empty. Matches the path mcpBoot.bindSession
// uses so the services Audit and the dag handler agree on the destination.
func directorAuditPath(cfg *config.Config, store *session.Store) string {
	if cfg == nil || store == nil || !cfg.Debug.IsMCPAuditEnabled() {
		return ""
	}
	return filepath.Join(store.SessionDir(), "mcp-log.jsonl")
}

// directorEventsLogPath returns the per-session events.ndjson path when
// the persist_events_log toggle is on, otherwise empty. The same file
// is later read back by EventService.Replay for archived sessions and
// by bcc dev for replay-driven UI development.
func directorEventsLogPath(cfg *config.Config, store *session.Store) string {
	if cfg == nil || store == nil || !cfg.Debug.IsPersistEventsLogEnabled() {
		return ""
	}
	return filepath.Join(store.SessionDir(), "events.ndjson")
}

// teeLoopEvents reads every event written to src and forwards it to the
// transient consumer (TUI bridge, render dispatch) and to the persistent
// services channel. transient is closed when src closes so the per-run
// consumer's existing close-cascade keeps working; persistent stays open
// because services subscribers outlive any single l.Run. Sends are
// non-blocking so a slow consumer cannot stall its peer; the EventService
// already documents the same drop-on-pressure contract for its own ring
// buffer.
//
// persistent may be nil to opt out of services forwarding (tests).
func teeLoopEvents(src <-chan loop.Event, transient chan<- loop.Event, persistent chan<- loop.Event) {
	defer close(transient)
	for ev := range src {
		select {
		case transient <- ev:
		default:
			slog.Warn("cli: tui events channel full; dropping event")
		}
		if persistent == nil {
			continue
		}
		select {
		case persistent <- ev:
		default:
			slog.Warn("cli: service events channel full; dropping event")
		}
	}
}

// resolveWebUIHandler returns the http.Handler the run-wide listener
// mounts at /. The dashboard is always available so the user can reach
// it on demand (TUI keybinding or by visiting the URL directly), even
// when --webui / --webui-open were not passed; those flags now only
// govern the startup banner and the auto-open behavior. The dev flag
// (hidden from --help) selects between the production embedded bundle
// handler and the Vite reverse-proxy handler used during contributor
// work on the SPA.
func resolveWebUIHandler(dev bool) http.Handler {
	if dev {
		// bcc run uses the default Vite upstream (loopback:5173); the
		// configurable form lives on `bcc dev`. NewDev returns an error
		// only on parse failure of the constant default, which is a
		// programmer error and merits a panic.
		h, err := webui.NewDev(webui.DefaultDevUpstream)
		if err != nil {
			panic(fmt.Errorf("cli: build webui dev proxy: %w", err))
		}
		return h
	}
	return webui.New()
}

// runListenerBind returns the bind address used by startRunListener.
// The default is loopback with an OS-assigned port. P4 keeps the
// surface minimal; an explicit `--bind` flag lands once webui/api
// configuration grows in P5+.
func runListenerBind() string {
	return "127.0.0.1:0"
}

func defaultDirectorDeps(cfg *config.Config, boot *mcpBoot) directorDeps {
	// In TUI mode the alt-screen renderer owns the terminal: any byte the
	// agent subprocess writes to os.Stderr would be drawn on top of the
	// dashboard, scrambling panels. Discard the live tee in that mode and
	// rely on the adapter's internal ringBuffer for diagnostics; in
	// text/json mode forward stderr verbatim so users see auth/quota
	// errors as they happen.
	subprocessStderr := directorSubprocessStderr()
	// loopEvents is set later in runDirectorWith after session resolution;
	// the adapter pointer is kept so bindDirectorAdapterSession can patch it.
	claudeProvider := cfg.Providers["claude"]
	adapter := directorclaude.New(directorclaude.Config{
		Binary:       claudeProvider.Binary,
		ExtraArgs:    claudeProvider.ExtraArgs,
		MaxBudgetUSD: claudeProvider.MaxBudgetUSD,
		Stderr:       subprocessStderr,
		MCPURL:       boot.MCPURL(),
		MCPToken:     boot.token(),
	})
	registry := buildCapabilityRegistry()
	menus := buildRoleMenus(cfg)
	if boot != nil && boot.handler != nil {
		boot.handler.SetCapabilityRegistry(&registry)
		boot.handler.SetRoleMenus(menus)
	}
	return directorDeps{
		cfg:         cfg,
		planner:     adapter,
		briefer:     adapter,
		reviewer:    adapter,
		registerFn:  boot.registerDirectorAgent,
		baseDir:     ".bcc",
		git:         gitcli.New(""),
		newExecutor: makeNewExecutor(cfg, boot, subprocessStderr, nil, nil, nil),
		now:         time.Now,
	}
}

// buildCapabilityRegistry merges the curated catalog of every known
// provider into a single CapabilityRegistry. Used by the run-wide
// handler for tier and summary lookups when validating plan_emit
// payloads and for renders that want a flat catalog (auditing, future
// API endpoints).
func buildCapabilityRegistry() menu.CapabilityRegistry {
	known := config.KnownProviderList()
	lists := make([][]menu.Capability, 0, len(known))
	for _, kp := range known {
		caps := make([]menu.Capability, 0, len(kp.Models))
		for _, m := range kp.Models {
			caps = append(caps, menu.Capability{
				Provider: m.Provider,
				Model:    m.Model,
				Tier:     m.Tier,
				Efforts:  append([]string(nil), m.DefaultEfforts...),
				Summary:  m.Summary,
			})
		}
		lists = append(lists, caps)
	}
	return menu.MergeCapabilityRegistries(lists...)
}

// buildRoleMenus converts the user-facing config menus into the
// director-side RoleMenus, enriching every option with curated tier and
// summary metadata when bcc has them in its built-in registry. Options
// with no curated entry render in the Planner prompt without tier or
// summary, but with the user's free-form note when present.
func buildRoleMenus(cfg *config.Config) menu.RoleMenus {
	if cfg == nil {
		return menu.RoleMenus{}
	}
	convert := func(policy config.RolePolicy) menu.RoleMenu {
		out := make([]menu.MenuOption, 0, len(policy.Options))
		for _, opt := range policy.Options {
			mo := menu.MenuOption{
				Provider: opt.Provider,
				Model:    opt.Model,
				Efforts:  append([]string(nil), opt.Efforts...),
				Note:     opt.Note,
			}
			if cap, ok := config.KnownModelByName(opt.Provider, opt.Model); ok {
				mo.Tier = cap.Tier
				mo.Summary = cap.Summary
			}
			out = append(out, mo)
		}
		return menu.RoleMenu{Options: out}
	}
	return menu.RoleMenus{
		Planner:  convert(cfg.Roles.Planner),
		Briefer:  convert(cfg.Roles.Briefer),
		Executor: convert(cfg.Roles.Executor),
		Reviewer: convert(cfg.Roles.Reviewer),
	}
}

// plannerAssignmentFor returns the (provider, model, effort) the bcc
// loop will run the Planner under, picked from the Planner's filtered
// menu. Falls back to a clearly-empty RoleAssignment when no options
// survived; the adapter then errors out at spawn time with a useful
// message.
func plannerAssignmentFor(cfg *config.Config) supervision.RoleAssignment {
	if cfg == nil || len(cfg.Roles.Planner.Options) == 0 {
		return supervision.RoleAssignment{}
	}
	opt := cfg.Roles.Planner.Options[0]
	a := supervision.RoleAssignment{Provider: opt.Provider, Model: opt.Model}
	if len(opt.Efforts) > 0 {
		a.Effort = opt.Efforts[0]
	}
	return a
}

// executorLogSinks is the cli's per-spawn debug-log allocator. The
// concrete sinks are opened once per call inside makeNewExecutor; the
// returned StderrLogPath is propagated onto the loop.ExecResult so the
// loop can name the capture file in error messages.
type executorLogSinks struct {
	StderrSink    io.WriteCloser
	StderrLogPath string
	StdoutSink    io.WriteCloser
}

// makeNewExecutor builds the per-iteration executor factory the loop
// calls. logSinks, when non-nil, opens optional per-spawn capture files
// and is invoked once per Run with the resolved agent_id and iteration
// id; the returned writers are wired into the inner adapter and closed
// when Run returns. Passing nil keeps the no-debug behavior. store and
// loopEvents, when non-nil, are forwarded to the executor Config for
// per-spawn prompt persistence and SpawnStarted event emission.
func makeNewExecutor(
	cfg *config.Config,
	boot *mcpBoot,
	subprocessStderr io.Writer,
	logSinks func(args dag.RegisterArgs, agentID string) (executorLogSinks, error),
	store *session.Store,
	loopEvents chan<- loop.Event,
) func(dag.RegisterArgs, func(string) (string, error), *supervision.RoleAssignment) loop.Executor {
	return func(args dag.RegisterArgs, renderSystem func(agentID string) (string, error), assignment *supervision.RoleAssignment) loop.Executor {
		mcpCfg, cleanup, err := boot.executorMCPConfig(dag.RoleExecutor, args)
		if err != nil {
			return &failingExecutor{err: fmt.Errorf("register executor agent: %w", err)}
		}
		systemPromptFile, err := renderSystem(mcpCfg.AgentID)
		if err != nil {
			cleanup()
			return &failingExecutor{err: fmt.Errorf("render executor system prompt: %w", err)}
		}
		if assignment == nil || assignment.Provider == "" || assignment.Model == "" {
			cleanup()
			return &failingExecutor{err: fmt.Errorf("executor spawn requires a complete RoleAssignment (provider, model)")}
		}
		if assignment.Provider != "claude" {
			cleanup()
			return &failingExecutor{err: fmt.Errorf("executor adapter does not support provider %q", assignment.Provider)}
		}
		provider := cfg.Providers[assignment.Provider]
		model := assignment.Model
		effort := assignment.Effort
		var sinks executorLogSinks
		if logSinks != nil {
			sinks, err = logSinks(args, mcpCfg.AgentID)
			if err != nil {
				cleanup()
				return &failingExecutor{err: fmt.Errorf("open executor log sinks: %w", err)}
			}
		}
		stderrWriter := subprocessStderr
		if sinks.StderrSink != nil {
			if subprocessStderr != nil && subprocessStderr != io.Discard {
				stderrWriter = io.MultiWriter(subprocessStderr, sinks.StderrSink)
			} else {
				stderrWriter = sinks.StderrSink
			}
		}
		inner := claude.New(claude.Config{
			Binary:            provider.Binary,
			Model:             model,
			Effort:            effort,
			ExtraArgs:         provider.ExtraArgs,
			SkipPermissions:   provider.ShouldSkipPermissions(),
			SystemPromptFile:  systemPromptFile,
			Stderr:            stderrWriter,
			Stdout:            sinks.StdoutSink,
			MCPURL:            mcpCfg.MCPURL,
			MCPToken:          mcpCfg.MCPToken,
			MCPConnectionName: mcpCfg.MCPConnectionName,
			AgentID:           mcpCfg.AgentID,
			PhaseID:           args.PhaseID,
			IterationID:       args.BriefingID,
			Attempt:           args.Attempt,
			SessionStore:      store,
			Events:            loopEvents,
		})
		return &deregisteringExecutor{
			inner:         inner,
			agentID:       mcpCfg.AgentID,
			stderrLogPath: sinks.StderrLogPath,
			cleanup: func() {
				if sinks.StdoutSink != nil {
					_ = sinks.StdoutSink.Close()
				}
				if sinks.StderrSink != nil {
					_ = sinks.StderrSink.Close()
				}
				cleanup()
			},
		}
	}
}

// enableDebugLogCapture wires per-spawn stderr (and optionally stdout)
// capture to .bcc/sessions/<id>/runs/ when the [debug] toggles request
// it. The session must already be resolved on deps.store. No-op when
// captures are off, when the directorclaude adapter shape is not what
// we expect (tests inject fakes), or when no Store is bound.
func enableDebugLogCapture(cfg *config.Config, deps *directorDeps) {
	if !cfg.Debug.IsCaptureSubprocessLogsEnabled() {
		return
	}
	if deps == nil || deps.store == nil {
		return
	}
	store := deps.store
	captureStdout := cfg.Debug.IsCaptureSubprocessStdoutEnabled()

	openLog := func(bucket, agentID, kind string) (io.WriteCloser, error) {
		path, err := store.RunLogPath(bucket, agentID, kind)
		if err != nil {
			return nil, err
		}
		return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	}
	bucketFor := func(role, iter string) string {
		if role == string(dag.RolePlanner) {
			return session.PlannerRunsBucket
		}
		return iter
	}

	if a, ok := deps.planner.(*directorclaude.Adapter); ok {
		a.SetStderrFactory(func(role, iter, agent string) (io.WriteCloser, error) {
			return openLog(bucketFor(role, iter), agent, "stderr.log")
		})
		if captureStdout {
			a.SetStdoutFactory(func(role, iter, agent string) (io.WriteCloser, error) {
				return openLog(bucketFor(role, iter), agent, "stdout.jsonl")
			})
		}
	}

	if deps.boot == nil {
		return
	}
	subprocessStderr := directorSubprocessStderr()
	deps.newExecutor = makeNewExecutor(cfg, deps.boot, subprocessStderr, func(args dag.RegisterArgs, agentID string) (executorLogSinks, error) {
		bucket := args.BriefingID
		stderrPath, err := store.RunLogPath(bucket, agentID, "stderr.log")
		if err != nil {
			return executorLogSinks{}, err
		}
		stderrSink, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return executorLogSinks{}, err
		}
		sinks := executorLogSinks{StderrSink: stderrSink, StderrLogPath: stderrPath}
		if captureStdout {
			stdoutSink, err := openLog(bucket, agentID, "stdout.jsonl")
			if err != nil {
				_ = stderrSink.Close()
				return executorLogSinks{}, err
			}
			sinks.StdoutSink = stdoutSink
		}
		return sinks, nil
	}, deps.store, deps.serviceEvents)
}

// bindExecutorSpawnContext re-builds the newExecutor factory after session
// resolution when debug log capture is NOT active. When debug capture is
// active, enableDebugLogCapture already calls makeNewExecutor with the
// store and events; this function handles the non-debug path so executor
// spawns always get prompt persistence and SpawnStarted emission.
func bindExecutorSpawnContext(cfg *config.Config, deps *directorDeps) {
	if cfg == nil || deps == nil || deps.store == nil || deps.boot == nil {
		return
	}
	if !cfg.Debug.IsCaptureSubprocessLogsEnabled() {
		subprocessStderr := directorSubprocessStderr()
		deps.newExecutor = makeNewExecutor(cfg, deps.boot, subprocessStderr, nil, deps.store, deps.serviceEvents)
	}
}

// bindDirectorAdapterSession wires the resolved session store and the
// serviceEvents channel into the director claude adapter after session
// resolution. This is a best-effort post-construction configuration: if
// the planner is not a *directorclaude.Adapter (test fakes), the call
// is a no-op.
func bindDirectorAdapterSession(deps *directorDeps) {
	if deps == nil || deps.store == nil {
		return
	}
	a, ok := deps.planner.(*directorclaude.Adapter)
	if !ok {
		return
	}
	a.SetSessionStore(deps.store)
	if deps.serviceEvents != nil {
		a.SetEvents(deps.serviceEvents)
	}
}

// directorSubprocessStderr returns the live stderr writer the adapters
// tee subprocess output to. Mirrors the choice in defaultDirectorDeps
// so debug capture, when re-wiring newExecutor, preserves the same TUI
// vs text-mode behavior.
func directorSubprocessStderr() io.Writer {
	if runOutput == OutputTUI {
		return io.Discard
	}
	return os.Stderr
}

// failingExecutor is the loop.Executor returned when registration fails
// inside the newExecutor closure. The Director loop surfaces the error
// from Run as a fatal abort.
type failingExecutor struct{ err error }

func (e *failingExecutor) Run(ctx context.Context, _ string, _ chan<- agentcontract.AgentEvent) (loop.ExecResult, error) {
	return loop.ExecResult{ExitCode: -1}, e.err
}

// runDirectorWith is the testable core: same signature as runDirector
// plus injected ports. All error paths set ExitCode before returning so
// the cobra wrapper exits with the right code.
func runDirectorWith(
	ctx context.Context,
	cancel context.CancelFunc,
	specPath string,
	cfg *config.Config,
	deps directorDeps,
	dio directorIO,
) error {
	var content []byte
	if specPath != "" {
		var err error
		content, err = os.ReadFile(specPath)
		if err != nil {
			ExitCode = loop.ExitInvalid
			return fmt.Errorf("director: read spec %s: %w", specPath, err)
		}
	}
	hash := supervision.ComputeSessionHash(content, deps.prompt)

	if deps.store == nil {
		store, sessErr := resolveDirectorSession(deps, dio, specPath, hash)
		if sessErr != nil {
			ExitCode = loop.ExitInvalid
			return sessErr
		}
		deps.store = store
	}
	fmt.Fprintf(dio.stderr, "bcc: session=%s status=%s\n", deps.store.Session().ID, deps.store.Session().Status)

	if deps.boot != nil {
		var headProvider dag.HeadProvider
		if g, ok := deps.git.(dag.HeadProvider); ok {
			headProvider = g
		}
		deps.boot.bindSession(deps.store, cfg.Debug.IsMCPAuditEnabled(), headProvider, journal.JournalDeltaProvider{})
	}
	if deps.store != nil && deps.stats == nil {
		deps.stats = stats.NewStatsLog(filepath.Join(deps.store.SessionDir(), "stats.jsonl"))
	}

	enableDebugLogCapture(cfg, &deps)
	bindDirectorAdapterSession(&deps)
	bindExecutorSpawnContext(cfg, &deps)

	if runOutput == OutputTUI {
		return runDirectorTUI(ctx, cancel, specPath, hash, cfg, deps, dio)
	}

	fmt.Fprintf(dio.stderr, "bcc: director enabled; planning %s (model thinking, typically 30s-3min)...\n", specPath)
	stopHeartbeat := startPlanningHeartbeat(ctx, dio.stderr, deps.now)
	plan, err := resolveDirectorPlan(ctx, specPath, hash, deps, dio, nil)
	stopHeartbeat()
	if err != nil {
		var skipped errPlannerSkipped
		if errors.As(err, &skipped) {
			return finishHeadlessNothingToDo(deps, dio, skipped.reason)
		}
		_ = deps.store.Touch(session.SessionAborted, deps.now())
		return err
	}

	escalation := stdinEscalationGate(ctx, dio.stdin)

	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Git:      deps.git,
		Director: &loop.DirectorPorts{
			Plan:        plan,
			Briefer:     deps.briefer,
			Reviewer:    deps.reviewer,
			Store:       deps.store,
			NewExecutor: deps.newExecutor,
			Handler:     directorEffectiveHandler(deps),
			Escalation:  escalation,
			Stats:       deps.stats,
		},
	}

	events, drained, derr := dispatchEvents(runOutput, loop.LevelInfo)
	if derr != nil {
		ExitCode = loop.ExitInvalid
		_ = deps.store.Touch(session.SessionAborted, deps.now())
		return derr
	}

	// Tee every loop event into the long-lived services channel so live
	// SSE subscribers see the headless run alongside the chosen render
	// backend.
	loopOut := make(chan loop.Event, 256)
	go teeLoopEvents(loopOut, events, deps.serviceEvents)

	code, runErr := l.Run(ctx, loopOut)
	<-drained
	ExitCode = code
	return runErr
}

// resolveDirectorSession picks a session for this run based on the
// flag matrix in the migration spec acceptance:
//
//  1. --resume + --session <id>: resume the named session; spec must
//     match.
//  2. --resume only: pick the most recent session for this spec; if
//     none exists, create a fresh session and proceed.
//  3. --session <id> only (no --resume): require the session to exist;
//     do not silently overwrite by creating a fresh one.
//  4. neither: create a new session.
func resolveDirectorSession(deps directorDeps, dio directorIO, specPath, hash string) (*session.Store, error) {
	now := deps.now
	if now == nil {
		now = time.Now
	}
	// sessionSpecPath returns the effective spec path for session
	// persistence. When the user supplied no spec (prompt-only run), a
	// sentinel value disambiguates the session kind without conflating it
	// with a real file path.
	sessionSpecPath := specPath
	if sessionSpecPath == "" {
		sessionSpecPath = "--prompt"
	}

	// persistPrompt stamps the prompt onto a freshly created session
	// manifest so bcc sessions show reflects the user's directive.
	persistPrompt := func(store *session.Store) *session.Store {
		if deps.prompt == "" || store == nil {
			return store
		}
		store.Session().Prompt = deps.prompt
		if now == nil {
			now = time.Now
		}
		_ = store.Touch(session.SessionRunning, now())
		return store
	}

	switch {
	case dio.session != "":
		sess, err := session.ResolveSession(deps.baseDir, dio.session, specPath)
		if err != nil {
			return nil, fmt.Errorf("director: resolve session: %w", err)
		}
		store, err := session.OpenSession(deps.baseDir, sess.ID)
		if err != nil {
			return nil, fmt.Errorf("director: open session %s: %w", sess.ID, err)
		}
		return store, nil
	case dio.resume:
		matches, err := session.FindSessionsForSpec(deps.baseDir, sessionSpecPath)
		if err != nil {
			return nil, fmt.Errorf("director: list sessions: %w", err)
		}
		switch len(matches) {
		case 0:
			fmt.Fprintln(dio.stderr, "bcc: --resume requested but no session for this spec; creating a fresh one")
			store, _, cerr := session.CreateSession(deps.baseDir, sessionSpecPath, hash, now())
			if cerr != nil {
				return nil, fmt.Errorf("director: create session: %w", cerr)
			}
			return persistPrompt(store), nil
		case 1:
			store, err := session.OpenSession(deps.baseDir, matches[0].ID)
			if err != nil {
				return nil, fmt.Errorf("director: open session %s: %w", matches[0].ID, err)
			}
			return store, nil
		default:
			ids := make([]string, 0, len(matches))
			for _, m := range matches {
				ids = append(ids, m.ID)
			}
			return nil, fmt.Errorf("%w: candidates: %s",
				session.ErrSessionAmbiguous, strings.Join(ids, ", "))
		}
	default:
		store, _, err := session.CreateSession(deps.baseDir, sessionSpecPath, hash, now())
		if err != nil {
			return nil, fmt.Errorf("director: create session: %w", err)
		}
		return persistPrompt(store), nil
	}
}

// startPlanningHeartbeat prints "bcc: planner still working (Xs
// elapsed)" to stderr every 15s while the planner subprocess is
// blocking. The returned stop function cancels the ticker; safe to
// call multiple times. The first heartbeat fires at 15s, not
// immediately, so quick plans stay quiet.
//
// Without this the user sees nothing between the "planning..."
// banner and the eventual plan render, since claude --bare emits
// almost no stderr and we drain its stdout silently.
func startPlanningHeartbeat(ctx context.Context, w io.Writer, now func() time.Time) func() {
	stop := make(chan struct{})
	if now == nil {
		now = time.Now
	}
	start := now()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := now().Sub(start).Round(time.Second)
				fmt.Fprintf(w, "bcc: planner still working (%s elapsed)\n", elapsed)
			case <-ctx.Done():
				return
			case <-stop:
				return
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }
}

// stdinEscalationGate spawns a goroutine that reads escalation
// answers from stdin and turns them into EscalationReply tokens. The
// goroutine never writes to out: the user-facing prompt for an
// escalation is the DirectorEscalation event, which the text/json
// drain renders, and the TUI overlay (P8) handles separately. Keeping
// the gate write-free avoids races between the goroutine and tests
// that inspect stderr after the loop terminates.
//
// Tests with no stdin lines (strings.NewReader("")) drive the
// goroutine to EOF immediately; ctx cancellation also exits the loop.
func stdinEscalationGate(ctx context.Context, in io.Reader) <-chan loop.EscalationReply {
	if in == nil {
		return nil
	}
	ch := make(chan loop.EscalationReply, 1)
	go func() {
		defer close(ch)
		br := bufio.NewReader(in)
		readLine := func() (string, bool) {
			line, err := br.ReadString('\n')
			if errors.Is(err, io.EOF) && line == "" {
				return "", false
			}
			if err != nil && !errors.Is(err, io.EOF) {
				return "", false
			}
			return line, true
		}
		for {
			line, ok := readLine()
			if !ok {
				return
			}
			var reply loop.EscalationReply
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "r", "resume":
				hintLine, hintOk := readLine()
				if !hintOk {
					return
				}
				reply = loop.EscalationReply{
					Kind: loop.EscalationResume,
					Hint: strings.TrimRight(strings.TrimRight(hintLine, "\n"), "\r"),
				}
			case "f", "force", "force-approve":
				reply = loop.EscalationReply{Kind: loop.EscalationForceApprove}
			case "s", "skip":
				reply = loop.EscalationReply{Kind: loop.EscalationSkip}
			case "a", "abort":
				reply = loop.EscalationReply{Kind: loop.EscalationAbort}
			default:
				continue
			}
			select {
			case ch <- reply:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// runDirectorTUI runs the Director under the bubbletea dashboard from
// t=0: the program enters alt-screen before the planner starts, the
// planner runs in a background goroutine that streams its AgentEvents
// into the TUI's raw channel as loop.AgentEventReceived (so the now /
// health / actions panels render planning activity in real time), and
// once the plan resolves the inner Loop kicks off in a second
// goroutine. The host returns the loop's ExitCode + error after the
// bubbletea program exits.
//
// Plan resolution failure: the planner goroutine emits a synthetic
// LoopFinished onto raw with reason "planner failed: <msg>"; the Model
// quits naturally on that signal.
func runDirectorTUI(ctx context.Context, cancel context.CancelFunc, specPath, hash string, cfg *config.Config, deps directorDeps, dio directorIO) error {
	gate := tui.NewGate()
	escalation := make(chan loop.EscalationReply, 1)

	gitProbeAdapter, _ := deps.git.(tui.GitProbe)
	branch := ""
	if br, gerr := deps.git.CurrentBranch(ctx); gerr == nil {
		branch = br
	}

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	// raw is the writer-side channel: loop.Loop.Run and the planner
	// orchestrator both publish onto it. A forwarding goroutine copies
	// every event from raw to deps.serviceEvents (non-blocking, keepalive
	// semantics: raw closing does not close serviceEvents so subscriber
	// lifetimes are decoupled from any single l.Run invocation). The TUI
	// reads from deps.svc.Events.Subscribe rather than a dedicated channel.
	raw := make(chan loop.Event, 256)
	go func() {
		for ev := range raw {
			if deps.serviceEvents == nil {
				continue
			}
			select {
			case deps.serviceEvents <- ev:
			default:
				slog.Warn("cli: serviceEvents channel full; dropping event")
			}
		}
	}()

	type runResult struct {
		code int
		err  error
	}
	var (
		resultMu sync.Mutex
		latest   runResult
	)
	setLatest := func(r runResult) {
		resultMu.Lock()
		defer resultMu.Unlock()
		latest = r
	}

	// runLoopOn spins up loop.Loop.Run against a freshly built events
	// channel. Used by both the first-run orchestrator and the session
	// Resume factory; loop.Loop.Run owns the channel lifecycle and emits
	// a terminal LoopFinished plus close on every exit path.
	runLoopOn := func(plan *supervision.Plan, ch chan loop.Event) {
		defer func() {
			if r := recover(); r != nil {
				setLatest(runResult{
					code: loop.ExitInvalid,
					err:  fmt.Errorf("loop panicked: %v\n%s", r, debug.Stack()),
				})
			}
		}()
		l := &loop.Loop{
			SpecPath: specPath,
			Config:   cfg,
			Git:      deps.git,
			Logger:   discard,
			Director: &loop.DirectorPorts{
				Plan:        plan,
				Briefer:     deps.briefer,
				Reviewer:    deps.reviewer,
				Store:       deps.store,
				NewExecutor: deps.newExecutor,
				Handler:     directorEffectiveHandler(deps),
				Escalation:  escalation,
				Stats:       deps.stats,
			},
		}
		code, err := l.Run(ctx, ch)
		setLatest(runResult{code: code, err: err})
	}

	model := tui.New(tui.Options{
		Services:        deps.svc,
		Cancel:          cancel,
		Gate:            gate,
		SpecPath:        specPath,
		Branch:          branch,
		SessionID:       deps.store.Session().ID,
		MaxIter:         cfg.Loop.MaxIterations,
		GitProbe:        gitProbeAdapter,
		GitCtx:          ctx,
		EscalationGate:  escalation,
		PlanningPending: true,
		WebUIURL:        deps.webuiURL,
		OpenBrowser:     deps.openBrowser,
	})
	progOpts := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	}
	if runNoColor {
		progOpts = append(progOpts, tea.WithColorProfile(colorprofile.NoTTY))
	}
	program := tea.NewProgram(model, progOpts...)
	model.SetProgram(program)

	defer func() {
		if r := recover(); r != nil {
			_ = program.ReleaseTerminal()
			fmt.Fprintf(os.Stderr, "bcc: panic in TUI host: %v\n%s\n", r, debug.Stack())
			panic(r)
		}
	}()

	// orchestrator drives plan resolution -> loop start. It owns the
	// raw channel: closes it via a synthetic LoopFinished on every
	// terminal path so the bubbletea bridge sees the close and the
	// program exits cleanly.
	orchestrate := func() {
		defer func() {
			if r := recover(); r != nil {
				setLatest(runResult{
					code: loop.ExitInvalid,
					err:  fmt.Errorf("director TUI orchestrator panicked: %v\n%s", r, debug.Stack()),
				})
				// Best-effort terminal signal. raw may already be
				// closed (loop.Loop.Run owns the close on the post-
				// planning path), so swallow any send/close-on-closed
				// secondary panic and rely on the prior LoopFinished
				// to drive the bridge.
				func() {
					defer func() { _ = recover() }()
					emitLoopFinished(raw, "fatal", loop.ExitInvalid)
					close(raw)
				}()
			}
		}()

		plan, perr := resolveDirectorPlanTUI(ctx, specPath, hash, deps, dio, raw)
		if perr != nil {
			var skipped errPlannerSkipped
			if errors.As(perr, &skipped) {
				_ = deps.store.Touch(session.SessionDone, deps.now())
				model.SignalPlanSkipped(skipped.reason)
				setLatest(runResult{code: loop.ExitDone, err: nil})
				emitLoopFinished(raw, LoopFinishedReasonNothingToDo, loop.ExitDone)
				close(raw)
				return
			}
			_ = deps.store.Touch(session.SessionAborted, deps.now())
			// Planner crashed without producing a terminal MCP call
			// (e.g. claude exited 1 because the account ran out of
			// credits). Keep the TUI alive in session mode so the
			// user reads the underlying error in the footer and
			// decides whether to retry, edit the spec, or quit.
			// The orchestrator returns nil here: the run "finished"
			// in the user-visible sense; the run-level ExitCode is
			// still ExitInvalid so headless callers see the same
			// failure.
			model.SignalPlanFailed(perr.Error())
			setLatest(runResult{code: loop.ExitInvalid, err: nil})
			emitLoopFinished(raw, LoopFinishedReasonPlannerFailed, loop.ExitInvalid)
			close(raw)
			return
		}

		// Plan resolved. Latch it onto the dashboard, drop the
		// planning placeholder, and start the loop straight away.
		// bcc is autonomous by design: there is no user gate here.
		model.SignalPlanReady(plan)
		runLoopOn(plan, raw)
		// loop.Loop.Run owns raw for the duration of Run: it emits a
		// final LoopFinished and closes the channel via its own defer,
		// even on panic. Closing here would double-close.
	}

	go orchestrate()

	if _, err := program.Run(); err != nil {
		cancel()
		printSessionResumeHint(dio.stderr, deps.store.Session().ID, specPath)
		return err
	}
	printSessionResumeHint(dio.stderr, deps.store.Session().ID, specPath)
	resultMu.Lock()
	defer resultMu.Unlock()
	ExitCode = latest.code
	return latest.err
}

// printSessionResumeHint writes a one-line stderr message after the TUI
// alt-screen has been released so the user sees the session id and the
// exact command needed to resume the run. Headless callers (text/json)
// already print session=<id> at startup; the TUI counterpart was lost
// to the alt-screen, so this restores it on exit.
func printSessionResumeHint(w io.Writer, sessionID, specPath string) {
	if w == nil || sessionID == "" {
		return
	}
	fmt.Fprintf(w, "bcc: session=%s (resume with: bcc run %s --resume --session %s)\n",
		sessionID, specPath, sessionID)
}

// LoopFinishedReasonNothingToDo is the canonical Reason carried on the
// LoopFinished event when the Planner declared the spec done via
// plan_skip. Headless render backends (text/json) emit this reason
// so consumers can branch deterministically; the TUI maps the same
// reason to a friendly terminal screen.
const LoopFinishedReasonNothingToDo = "nothing_to_do"

// LoopFinishedReasonPlannerFailed is the canonical Reason emitted when
// the planner subprocess exited with no terminal MCP call (no
// plan_emit, no plan_skip). The TUI keeps the dashboard alive
// in session mode so the user can read the underlying error in the
// session footer and decide whether to resume / edit / quit.
const LoopFinishedReasonPlannerFailed = "planner_failed"

// finishHeadlessNothingToDo runs the post-skip terminal sequence for
// the text/json output paths: stamps the session as done, emits a
// structured LoopFinished event through the chosen render backend so
// JSON consumers see one terminal line, prints a friendly stderr
// message in text mode, and exits with ExitDone (0). The TUI path has
// its own handler so the dashboard stays alive.
func finishHeadlessNothingToDo(deps directorDeps, dio directorIO, reason string) error {
	now := deps.now
	if now == nil {
		now = time.Now
	}
	_ = deps.store.Touch(session.SessionDone, now())

	events, drained, derr := dispatchEvents(runOutput, loop.LevelInfo)
	if derr != nil {
		ExitCode = loop.ExitInvalid
		return derr
	}
	events <- loop.LoopFinished{
		Reason:   LoopFinishedReasonNothingToDo,
		ExitCode: loop.ExitDone,
		At:       now(),
	}
	close(events)
	<-drained

	msg := reason
	if msg == "" {
		msg = "spec is already complete"
	}
	fmt.Fprintf(dio.stderr, "bcc: nothing to do; %s\n", msg)
	ExitCode = loop.ExitDone
	return nil
}

// emitLoopFinished sends a synthetic LoopFinished onto the events
// channel. Used by the TUI orchestrator for terminal paths that bypass
// loop.Loop.Run (planner failure, user abort at confirmation, fatal
// orchestrator panic). The Model treats this as a normal end-of-run
// signal and the program quits.
func emitLoopFinished(events chan<- loop.Event, reason string, exit int) {
	events <- loop.LoopFinished{Reason: reason, ExitCode: exit, At: time.Now()}
}

// resolveDirectorPlanTUI is the TUI-mode counterpart of
// resolveDirectorPlan. The signature differs only in that it takes a
// raw events channel onto which the planner's stream-json envelopes
// are forwarded as loop.AgentEventReceived; the rest of the resume /
// fresh / re-plan logic is shared via resolveDirectorPlan(events).
func resolveDirectorPlanTUI(
	ctx context.Context,
	specPath, hash string,
	deps directorDeps,
	dio directorIO,
	raw chan<- loop.Event,
) (*supervision.Plan, error) {
	// resolveDirectorPlan accepts an events sink that, when non-nil,
	// receives the planner's AgentEvents already wrapped as
	// loop.AgentEventReceived; the existing flow is otherwise unchanged.
	return resolveDirectorPlan(ctx, specPath, hash, deps, dio, raw)
}

// resolveDirectorPlan returns the Plan to run, handling --resume:
//
//   - --resume + persisted plan + matching SpecHash: reuse the plan,
//     skip the planner entirely.
//   - --resume + persisted plan + diverging SpecHash: call the planner,
//     compute a PlanDiff against the stored plan, render the diff for
//     the user's information, and persist the new plan.
//   - --resume + no persisted plan: fall through to the fresh path.
//   - no --resume: plan, persist, and run.
//
// bcc runs autonomously: the loop starts as soon as a plan is in
// hand. There is no user gate. All error paths set ExitCode before
// returning so the cobra wrapper exits with the right code.
//
// When raw is non-nil the planner's stream telemetry is forwarded onto
// raw as loop.AgentEventReceived (TUI mode); when raw is nil the
// planner runs silently and the caller is responsible for any progress
// indicator (text/json mode uses startPlanningHeartbeat).
func resolveDirectorPlan(
	ctx context.Context,
	specPath string,
	hash string,
	deps directorDeps,
	dio directorIO,
	raw chan<- loop.Event,
) (*supervision.Plan, error) {
	if dio.resume {
		existing, readErr := deps.store.ReadPlan()
		if readErr == nil {
			if existing.SpecHash == hash {
				if raw == nil {
					RenderPlan(existing, dio.stderr)
					fmt.Fprintln(dio.stderr, "\nbcc: --resume; spec hash unchanged; resuming from persisted plan")
				}
				if err := loadPersistedDAGState(deps, existing); err != nil {
					ExitCode = loop.ExitInvalid
					return nil, err
				}
				return existing, nil
			}
			return rePlanFlow(ctx, specPath, hash, existing, deps, dio, raw)
		}
		if !errors.Is(readErr, fs.ErrNotExist) {
			ExitCode = loop.ExitInvalid
			return nil, fmt.Errorf("director: read persisted plan: %w", readErr)
		}
		if raw == nil {
			fmt.Fprintln(dio.stderr, "bcc: session has no persisted plan; planning from scratch")
		}
	}

	plan, err := freshPlan(ctx, specPath, hash, deps, raw)
	if err != nil {
		return nil, err
	}
	if err := deps.store.WritePlan(plan); err != nil {
		ExitCode = loop.ExitInvalid
		return nil, fmt.Errorf("director: persist plan: %w", err)
	}
	if raw == nil {
		RenderPlan(plan, dio.stderr)
	}
	return plan, nil
}

// loadPersistedDAGState restores the DAG state captured under the
// session directory so a `--resume` run continues from the prior
// progress instead of treating every task as pending. Called only when
// the persisted plan's SpecHash matches the current spec; on re-plan
// the persisted state is stale and the loop builds a fresh one from
// the new plan via NewStateFromPlan.
//
// A missing dag.json is not an error: the session may have been
// created but never advanced past planning. In that case the loop
// initializes the state from the plan as usual.
func loadPersistedDAGState(deps directorDeps, plan *supervision.Plan) error {
	handler := directorEffectiveHandler(deps)
	if handler == nil || deps.store == nil {
		return nil
	}
	dagPath := filepath.Join(deps.store.SessionDir(), "dag.json")
	state, err := dag.LoadStateFile(dagPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("director: load persisted dag state: %w", err)
	}
	handler.SetState(state)
	handler.SetPlan(plan)
	return nil
}

// freshPlan calls the planner, normalises the bcc-owned fields, and
// validates. It is the shared kernel used by both the fresh path and
// rePlanFlow; neither writes to disk before validation. The planner is
// invoked under a freshly-registered Director agent_id so the run-wide
// MCP handler can scope its emit and audit log entries.
//
// When raw is non-nil, the planner's AgentEvents are forwarded onto
// raw as loop.AgentEventReceived for the TUI to render in real time.
func freshPlan(ctx context.Context, specPath string, hash string, deps directorDeps, raw chan<- loop.Event) (*supervision.Plan, error) {
	agentID, deregister, err := registerDirectorAgent(deps, dag.RolePlanner)
	if err != nil {
		ExitCode = loop.ExitInvalid
		return nil, err
	}
	defer deregister()

	var agentEvents chan agentcontract.AgentEvent
	pumpDone := make(chan struct{})
	if raw != nil {
		agentEvents = make(chan agentcontract.AgentEvent, 256)
		go func() {
			defer close(pumpDone)
			for ae := range agentEvents {
				select {
				case raw <- loop.AgentEventReceived{Event: ae}:
				case <-ctx.Done():
					return
				}
			}
		}()
	} else {
		close(pumpDone)
	}

	var (
		plannerRegistry menu.CapabilityRegistry
		plannerMenus    menu.RoleMenus
	)
	if h := directorEffectiveHandler(deps); h != nil {
		if reg := h.CapabilityRegistry(); reg != nil {
			plannerRegistry = *reg
		}
		plannerMenus = h.RoleMenus()
	}
	plan, plannerStats, runErr := deps.planner.Plan(ctx, supervision.PlannerInput{
		AgentID:    agentID,
		SpecPath:   specPath,
		SpecHash:   hash,
		Registry:   plannerRegistry,
		Prompt:     deps.prompt,
		Assignment: plannerAssignmentFor(deps.cfg),
		Menus:      plannerMenus,
	}, agentEvents)
	if agentEvents != nil {
		close(agentEvents)
	}
	<-pumpDone
	if plannerStats != nil && deps.stats != nil {
		if err := deps.stats.Append(stats.StatsEntry{
			At:         deps.now(),
			Role:       string(dag.RolePlanner),
			DurationMS: plannerStats.DurationMS,
			CostUSD:    plannerStats.CostUSD,
			Tokens:     plannerStats.Tokens,
		}); err != nil {
			slog.Warn("director stats append planner", "err", err)
		}
	}

	// The Plan flows through the MCP handler via plan_emit; the
	// adapter return is nil by design. Handler state is authoritative:
	// inspect it before honouring runErr so that a misbehaving agent
	// that crashed after calling plan_emit / plan_skip still has
	// its terminal call respected. A non-zero exit only becomes fatal
	// when the planner left no terminal state behind.
	if h := directorEffectiveHandler(deps); h != nil {
		if hp := h.Plan(); hp != nil {
			plan = hp
		} else if h.PlanSkipped() {
			return nil, errPlannerSkipped{reason: h.PlanSkipReason()}
		}
	}
	if plan == nil {
		ExitCode = loop.ExitInvalid
		if runErr != nil {
			return nil, fmt.Errorf("director: plan: %w", runErr)
		}
		return nil, errors.New("director: planner exited without emitting a plan or calling plan_skip")
	}
	plan.SpecHash = hash
	if plan.PlannedAt.IsZero() {
		plan.PlannedAt = deps.now()
	}
	if err := supervision.ValidatePlan(plan); err != nil {
		ExitCode = loop.ExitInvalid
		return nil, err
	}
	return plan, nil
}

// registerDirectorAgent obtains an agent_id from the run-wide registry
// for the given Director role. When deps.registerFn is unset (test
// fakes drive the loop without the real MCP boot), it returns a stable
// stub id so input.AgentID is non-empty.
func registerDirectorAgent(deps directorDeps, role dag.Role) (string, func(), error) {
	if deps.registerFn == nil {
		return "fake-" + string(role), func() {}, nil
	}
	return deps.registerFn(role)
}

// rePlanFlow handles the --resume hash-mismatch branch: replan,
// render the diff for the user (text/json mode), and persist the new
// plan. Autonomous by design, no user gate.
func rePlanFlow(
	ctx context.Context,
	specPath string,
	hash string,
	old *supervision.Plan,
	deps directorDeps,
	dio directorIO,
	raw chan<- loop.Event,
) (*supervision.Plan, error) {
	if raw == nil {
		fmt.Fprintln(dio.stderr, "bcc: --resume; spec hash diverged from persisted plan; replanning")
	}
	newPlan, err := freshPlan(ctx, specPath, hash, deps, raw)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		diff := supervision.ComputePlanDiff(old, newPlan)
		supervision.RenderPlanDiff(diff, dio.stderr)
	}

	if err := deps.store.WritePlan(newPlan); err != nil {
		ExitCode = loop.ExitInvalid
		return nil, fmt.Errorf("director: persist replanned plan: %w", err)
	}
	return newPlan, nil
}
