package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/fgmacedo/buchecha/internal/config"
	configloader "github.com/fgmacedo/buchecha/internal/configloader/toml"
	"github.com/fgmacedo/buchecha/internal/executor/claude"
	gitcli "github.com/fgmacedo/buchecha/internal/git/cli"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/specreader/markdown"
)

var (
	runSingleShot bool
	runMaxIter    int
	runEnvFlags   []string
	runExtra      string
	runConfigPath string
)

var runCmd = &cobra.Command{
	Use:   "run <spec>",
	Short: "Run the loop on a spec",
	Long:  "Read a Markdown spec, invoke the configured agent in a phase-by-phase loop, and decide based on the journal entries.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		return runSpec(ctx, args[0])
	},
}

func init() {
	runCmd.Flags().BoolVar(&runSingleShot, "single-shot", false, "single-shot mode: one agent invocation tries all phases")
	runCmd.Flags().IntVar(&runMaxIter, "max-iterations", 0, "iteration cap (overrides .bcc.toml; 0 = use config)")
	runCmd.Flags().StringSliceVar(&runEnvFlags, "env", nil, "KEY=VALUE env override (repeatable; highest precedence)")
	runCmd.Flags().StringVar(&runExtra, "extra", "", "additional instructions appended to the prompt")
	runCmd.Flags().StringVar(&runConfigPath, "config", "", "path to .bcc.toml (overrides discovery)")
	rootCmd.AddCommand(runCmd)
}

func runSpec(ctx context.Context, specPath string) error {
	if _, err := os.Stat(specPath); err != nil {
		ExitCode = loop.ExitInvalid
		return fmt.Errorf("spec %s: %w", specPath, err)
	}

	cfg, foundCfg, err := loadConfigForRun()
	if err != nil {
		ExitCode = loop.ExitInvalid
		return err
	}

	if runMaxIter > 0 {
		cfg.Loop.MaxIterations = runMaxIter
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

	skip := cfg.Executor.ShouldSkipPermissions()
	if skip {
		fmt.Fprintf(os.Stderr,
			"bcc: WARNING: agent permission prompts are SUPPRESSED (executor.skip_permissions=true).\n"+
				"  The agent will read, write, edit, and run shell commands without confirmation.\n"+
				"  This is required for autonomous mode. To opt out, set skip_permissions=false\n"+
				"  in .bcc.toml; the loop will likely degrade or stall on the first tool use.\n",
		)
	} else {
		fmt.Fprintf(os.Stderr,
			"bcc: WARNING: skip_permissions=false. The agent runs in -p mode without\n"+
				"  --dangerously-skip-permissions. Tool calls that require user approval will\n"+
				"  abort or be skipped silently; the loop is unlikely to converge. Use this only\n"+
				"  for dry-runs or when the agent does not require permission prompts.\n",
		)
	}

	executor := claude.New(claude.Config{
		Binary:          cfg.Executor.Binary,
		Model:           cfg.Executor.Model,
		ExtraArgs:       cfg.Executor.ExtraArgs,
		SkipPermissions: skip,
		Stderr:          os.Stderr,
	})
	gitProbe := gitcli.New("")
	specReader := markdown.New()

	l := &loop.Loop{
		SpecPath:   specPath,
		Config:     cfg,
		Executor:   executor,
		Git:        gitProbe,
		SpecReader: specReader,
		Extra:      runExtra,
		SingleShot: runSingleShot,
	}

	code, runErr := l.Run(ctx)
	ExitCode = code
	return runErr
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
