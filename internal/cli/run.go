package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/fgmacedo/buchecha/internal/config"
	configloader "github.com/fgmacedo/buchecha/internal/configloader/toml"
	"github.com/fgmacedo/buchecha/internal/loop"
)

var (
	runEnvFlags    []string
	runConfigPath  string
	runAllowDirty  bool
	runOutput      string
	runVerbosity   string
	runNoColor     bool
	runAgentName   string
	runAutoProceed bool
	runResume      bool
	runSessionID   string
)

var runCmd = &cobra.Command{
	Use:   "run <spec>",
	Short: "Run the loop on a spec",
	Long:  "Read a Markdown spec and drive the Director plan/brief/execute/review pipeline against it.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		return runSpec(ctx, cancel, args[0])
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
	runCmd.Flags().BoolVar(&runAutoProceed, "auto-proceed", false, "skip the post-plan confirmation prompt")
	runCmd.Flags().BoolVar(&runResume, "resume", false, "resume the most recent session that targets this spec; replan with a diff prompt when the spec hash diverges")
	runCmd.Flags().StringVar(&runSessionID, "session", "", "resume the named session id (combine with --resume to resolve; without --resume, fails if the session does not exist)")
	rootCmd.AddCommand(runCmd)
}

func runSpec(ctx context.Context, cancel context.CancelFunc, specPath string) error {
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

	if _, err := os.Stat(specPath); err != nil {
		ExitCode = loop.ExitInvalid
		return fmt.Errorf("spec %s: %w", specPath, err)
	}

	cfg, foundCfg, err := loadConfigForRun()
	if err != nil {
		ExitCode = loop.ExitInvalid
		return err
	}

	if runAgentName != "" {
		cfg.Agent.Name = runAgentName
	}

	if err := cfg.ApplyEnv(runEnvFlags); err != nil {
		ExitCode = loop.ExitInvalid
		return fmt.Errorf("apply env: %w", err)
	}

	if foundCfg != "" {
		fmt.Fprintf(os.Stderr, "bcc: spec=%s config=%s\n", specPath, foundCfg)
	} else {
		fmt.Fprintf(os.Stderr, "bcc: spec=%s config=<defaults; no .bcc.toml found>\n", specPath)
	}

	return runDirector(ctx, cancel, specPath, cfg)
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
