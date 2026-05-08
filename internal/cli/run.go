package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/fgmacedo/buchecha/internal/config"
	configloader "github.com/fgmacedo/buchecha/internal/configloader/toml"
	"github.com/fgmacedo/buchecha/internal/loop"
)

var (
	runEnvFlags        []string
	runConfigPath      string
	runAllowDirty      bool
	runOutput          string
	runVerbosity       string
	runNoColor         bool
	runAgentName       string
	runResume          bool
	runSessionID       string
	runDebugLogs       bool
	runDebugLogsStdout bool
	runNoEventsLog     bool
	runAPI             bool
	runWebUI           bool
	runWebUIOpen       bool
	runWebUIDev        bool
	runPrompt          string
)

var runCmd = &cobra.Command{
	Use:   "run [<spec>]",
	Short: "Run the loop on a spec",
	Long:  "Read a Markdown spec and drive the Director plan/brief/execute/review pipeline against it.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		specPath := ""
		if len(args) == 1 {
			specPath = args[0]
		}
		return runSpec(ctx, cancel, cmd, specPath, runPrompt)
	},
}

func init() {
	runCmd.Flags().StringSliceVar(&runEnvFlags, "env", nil, "KEY=VALUE env override (repeatable; highest precedence)")
	runCmd.Flags().StringVar(&runConfigPath, "config", "", "path to .bcc.toml (overrides discovery)")
	runCmd.Flags().BoolVarP(&runAllowDirty, "allow-dirty", "d", false, "skip the pre-flight clean-tree check (the agent will see the dirty tree)")
	runCmd.Flags().StringVar(&runOutput, "output", OutputTUI, "render backend: tui|text|json")
	runCmd.Flags().StringVar(&runVerbosity, "verbosity", loop.LevelInfo.String(), "event level low-water mark: error|warn|info|debug|trace")
	runCmd.Flags().BoolVar(&runNoColor, "no-color", false, "disable color output (lipgloss styles render as plain text)")
	runCmd.Flags().StringVar(&runAgentName, "agent", "", "active agent adapter (overrides [agent].name for this run)")
	runCmd.Flags().BoolVar(&runResume, "resume", false, "resume the most recent session that targets this spec; replan when the spec hash diverges")
	runCmd.Flags().StringVar(&runSessionID, "session", "", "resume the named session id (combine with --resume to resolve; without --resume, fails if the session does not exist)")
	runCmd.Flags().BoolVar(&runDebugLogs, "debug-logs", false, "capture per-spawn stderr of every Director role under .bcc/sessions/<id>/runs/ (overrides [debug].capture_subprocess_logs)")
	runCmd.Flags().BoolVar(&runDebugLogsStdout, "debug-logs-stdout", false, "also capture per-spawn stream-json stdout (heavier; implies --debug-logs; overrides [debug].capture_subprocess_stdout)")
	runCmd.Flags().BoolVar(&runNoEventsLog, "no-events-log", false, "skip persisting the SeqEvent stream to .bcc/sessions/<id>/events.ndjson (overrides [debug].persist_events_log)")
	runCmd.Flags().BoolVar(&runAPI, "api", false, "advertise the bcc HTTP API on stderr at startup (the listener always binds for MCP; this flag only controls the banner)")
	runCmd.Flags().BoolVarP(&runWebUI, "webui", "w", false, "serve the embedded web dashboard at the listener root and advertise it on stderr; takes precedence over --api when both are set")
	runCmd.Flags().BoolVarP(&runWebUIOpen, "webui-open", "W", false, "after the listener is up, launch the default browser at the dashboard URL (implies --webui)")
	runCmd.Flags().BoolVar(&runWebUIDev, "webui-dev", false, "reverse-proxy the SPA from the local Vite dev server (contributor mode)")
	runCmd.Flags().StringVarP(&runPrompt, "prompt", "p", "", "free-form user directive (the spec argument becomes optional; both compose when present)")
	if err := runCmd.Flags().MarkHidden("webui-dev"); err != nil {
		// Hidden marking only fails when the flag is missing. We just
		// registered it above, so a non-nil err here is a programmer
		// error worth surfacing in tests.
		panic(fmt.Errorf("cli: hide --webui-dev: %w", err))
	}
	rootCmd.AddCommand(runCmd)
}

// validateRunInputs returns an error when neither specPath nor prompt
// carries content. Both empty is always invalid; the user must provide
// at least one source of work.
func validateRunInputs(specPath, prompt string) error {
	if specPath == "" && strings.TrimSpace(prompt) == "" {
		return errors.New("provide a spec path, --prompt, or both")
	}
	return nil
}

func runSpec(ctx context.Context, cancel context.CancelFunc, cmd *cobra.Command, specPath, prompt string) error {
	if err := validateRunInputs(specPath, prompt); err != nil {
		ExitCode = loop.ExitInvalid
		return err
	}

	verbosity, err := loop.ParseLevel(runVerbosity)
	if err != nil {
		ExitCode = loop.ExitInvalid
		return err
	}
	if !validOutputMode(runOutput) {
		ExitCode = loop.ExitInvalid
		return fmt.Errorf("unknown --output %q (want tui|text|json)", runOutput)
	}

	// In text mode the user expects events at their level on stderr.
	// Reconfigure the default slog handler so debug/trace events are
	// not swallowed; loop diagnostics share the same handler.
	if runOutput == OutputText {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slogLevelOf(verbosity),
		})))
	}

	if specPath != "" {
		if _, err := os.Stat(specPath); err != nil {
			ExitCode = loop.ExitInvalid
			return fmt.Errorf("spec %s: %w", specPath, err)
		}
	}

	cfg, foundCfg, err := loadConfigForRun()
	if err != nil {
		ExitCode = loop.ExitInvalid
		return err
	}

	if runAgentName != "" {
		cfg.Agent.Name = runAgentName
	}
	// --webui-open implies --webui: passing -W alone is sugar for "open
	// the dashboard right away", which is meaningless without the
	// dashboard mounted. Promote the flag here so all downstream reads
	// (config override, banner, handler construction) see a single
	// truthy value.
	if runWebUIOpen {
		runWebUI = true
	}
	mergeWebuiFlags(cfg, webuiOverride{
		webuiChanged:     cmd.Flags().Changed("webui") || runWebUIOpen,
		webuiOpenChanged: cmd.Flags().Changed("webui-open"),
		webui:            runWebUI,
		webuiOpen:        runWebUIOpen,
	})
	if cmd.Flags().Changed("debug-logs") {
		cfg.Debug.CaptureSubprocessLogs = boolPtr(runDebugLogs)
	}
	if cmd.Flags().Changed("debug-logs-stdout") {
		cfg.Debug.CaptureSubprocessStdout = boolPtr(runDebugLogsStdout)
		// --debug-logs-stdout has no effect without stderr capture
		// (the runs/ directory only materializes when stderr capture is
		// on). Imply it so the flag is self-sufficient.
		if runDebugLogsStdout && !cfg.Debug.IsCaptureSubprocessLogsEnabled() {
			cfg.Debug.CaptureSubprocessLogs = boolPtr(true)
		}
	}
	if cmd.Flags().Changed("no-events-log") {
		// --no-events-log only matters when set: it forces the tristate
		// to false. The default (true) is left alone so the events.ndjson
		// is persisted out of the box.
		cfg.Debug.PersistEventsLog = boolPtr(!runNoEventsLog)
	}

	if err := cfg.ApplyEnv(runEnvFlags); err != nil {
		ExitCode = loop.ExitInvalid
		return fmt.Errorf("apply env: %w", err)
	}

	if specPath != "" {
		if foundCfg != "" {
			fmt.Fprintf(os.Stderr, "bcc: spec=%s config=%s\n", specPath, foundCfg)
		} else {
			fmt.Fprintf(os.Stderr, "bcc: spec=%s config=<defaults; no .bcc.toml found>\n", specPath)
		}
	} else {
		if foundCfg != "" {
			fmt.Fprintf(os.Stderr, "bcc: prompt-only run config=%s\n", foundCfg)
		} else {
			fmt.Fprintf(os.Stderr, "bcc: prompt-only run config=<defaults; no .bcc.toml found>\n")
		}
	}

	return runDirector(ctx, cancel, specPath, prompt, cfg)
}

func boolPtr(b bool) *bool { return &b }

// webuiOverride captures the inputs mergeWebuiFlags needs to apply the
// CLI-wins-over-TOML rule for [webui]. The two *Changed fields mirror
// cobra's cmd.Flags().Changed semantics: true means the user passed
// the flag explicitly, false means leave the TOML value alone.
type webuiOverride struct {
	webuiChanged     bool
	webuiOpenChanged bool
	webui            bool
	webuiOpen        bool
}

// mergeWebuiFlags applies the [webui] override rule: CLI flags win
// when the user set them, otherwise the TOML value (cfg.Webui as
// loaded from disk) stands. Pulled out of runSpec so the matrix is
// unit-testable without booting cobra.
func mergeWebuiFlags(cfg *config.Config, o webuiOverride) {
	if cfg == nil {
		return
	}
	if o.webuiChanged {
		cfg.Webui.Enabled = o.webui
	}
	if o.webuiOpenChanged {
		cfg.Webui.Open = o.webuiOpen
	}
}

func validOutputMode(s string) bool {
	switch s {
	case OutputTUI, OutputText, OutputJSON:
		return true
	}
	return false
}

// loadConfigForRun returns the loaded Config and the path it was found at
// (empty when discover did not find anything and defaults were used).
func loadConfigForRun() (*config.Config, string, error) {
	if runConfigPath != "" {
		c, err := configloader.Load(runConfigPath)
		if err != nil {
			return nil, "", fmt.Errorf("load config %s: %w", runConfigPath, err)
		}
		return c, runConfigPath, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("getwd: %w", err)
	}
	c, found, err := configloader.Discover(cwd)
	if err != nil {
		return nil, "", fmt.Errorf("discover config: %w", err)
	}
	return c, found, nil
}
