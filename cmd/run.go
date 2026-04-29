package cmd

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

	executor := claude.New(claude.Config{
		Binary:    cfg.Executor.Binary,
		Model:     cfg.Executor.Model,
		ExtraArgs: cfg.Executor.ExtraArgs,
		Stderr:    os.Stderr,
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
