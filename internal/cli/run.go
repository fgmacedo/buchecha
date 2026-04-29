package cli

import (
	"context"
	"errors"
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
	"github.com/fgmacedo/buchecha/internal/spec"
	"github.com/fgmacedo/buchecha/internal/specreader/markdown"
)

var (
	runSingleShot bool
	runMaxIter    int
	runEnvFlags   []string
	runExtra      string
	runConfigPath string
	runAllowDirty bool
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
	runCmd.Flags().BoolVarP(&runAllowDirty, "allow-dirty", "d", false, "skip the pre-flight clean-tree check (the agent will see the dirty tree)")
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

	gitProbe := gitcli.New("")
	specReader := markdown.New()

	// Pre-flight: refuse to run on a dirty working tree unless the user
	// explicitly allowed it. The agent assumes a clean entry tree to
	// commit only its own work; mixing in the user's unrelated changes
	// would either contaminate the iteration commits or force the agent
	// to hand-curate `git add` paths every iteration.
	if !runAllowDirty {
		clean, gerr := gitProbe.IsClean(ctx)
		if gerr != nil {
			ExitCode = loop.ExitInvalid
			return fmt.Errorf("check working tree: %w", gerr)
		}
		if !clean {
			ExitCode = loop.ExitInvalid
			return errors.New(
				"working tree is not clean (uncommitted changes or untracked files).\n" +
					"  commit or stash before running, or pass --allow-dirty / -d to override.",
			)
		}
	}

	// Print the BCC_* contract once at startup so the user (and observer)
	// know what the agent will see in each iteration. Per-iteration values
	// (BCC_ITERATION, BCC_JSONL_PATH) vary; the names and meanings are fixed.
	branchHint := ""
	if br, gerr := gitProbe.CurrentBranch(ctx); gerr == nil {
		branchHint = br
	}
	fmt.Fprintf(os.Stderr,
		"bcc: agent subprocess will see: BCC_RUNNING=1, BCC_ITERATION=<n>, "+
			"BCC_MAX_ITERATIONS=%d, BCC_SPEC_PATH=%s, BCC_JSONL_PATH=<per-iter>, BCC_BRANCH=%s\n",
		cfg.Loop.MaxIterations, specPath, branchHint,
	)

	// Print the previous Result (if any) so a re-run after stop has clear
	// context. Useful when re-triggering after Result: review.
	if prev := readPreviousResult(specPath, cfg, specReader); prev != "" {
		fmt.Fprintf(os.Stderr, "bcc: previous run stopped on Result: %s\n", prev)
	}

	executor := claude.New(claude.Config{
		Binary:          cfg.Executor.Binary,
		Model:           cfg.Executor.Model,
		ExtraArgs:       cfg.Executor.ExtraArgs,
		SkipPermissions: skip,
		Stderr:          os.Stderr,
	})

	l := &loop.Loop{
		SpecPath:   specPath,
		Config:     cfg,
		Executor:   executor,
		Git:        gitProbe,
		SpecReader: specReader,
		Extra:      runExtra,
		SingleShot: runSingleShot,
	}

	// Drain events in a goroutine so the loop never blocks on a
	// full channel. P2.3 will replace this no-op drain with the
	// tui/text/json render backends.
	events := make(chan loop.Event, 256)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range events {
		}
	}()

	code, runErr := l.Run(ctx, events)
	<-drained
	ExitCode = code
	return runErr
}

// readPreviousResult returns the raw Result string of the latest journal
// entry in the spec, or empty string when the journal has no entry yet
// (first run) or the parse fails (malformed journal). Best-effort; not
// fatal.
func readPreviousResult(specPath string, cfg *config.Config, reader loop.SpecReader) string {
	content, err := reader.Read(specPath)
	if err != nil {
		return ""
	}
	vocab := spec.ResultVocab{
		OK:      cfg.Loop.Results.OK,
		Partial: cfg.Loop.Results.Partial,
		Done:    cfg.Loop.Results.Done,
		Blocked: cfg.Loop.Results.Blocked,
		Review:  cfg.Loop.Results.Review,
	}
	latest, err := spec.ParseLatestResult(content, cfg.Specs.JournalHeading, cfg.Specs.ResultKeyword, vocab)
	if err != nil {
		return ""
	}
	return latest.Raw
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
