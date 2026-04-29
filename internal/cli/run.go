package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/fgmacedo/buchecha/internal/config"
	configloader "github.com/fgmacedo/buchecha/internal/configloader/toml"
	"github.com/fgmacedo/buchecha/internal/executor/claude"
	gitcli "github.com/fgmacedo/buchecha/internal/git/cli"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/spec"
	"github.com/fgmacedo/buchecha/internal/specreader/markdown"
	"github.com/fgmacedo/buchecha/internal/tui"
)

var (
	runSingleShot bool
	runMaxIter    int
	runEnvFlags   []string
	runExtra      string
	runConfigPath string
	runAllowDirty bool
	runOutput     string
	runVerbosity  string
	runNoColor    bool
)

var runCmd = &cobra.Command{
	Use:   "run <spec>",
	Short: "Run the loop on a spec",
	Long:  "Read a Markdown spec, invoke the configured agent in a phase-by-phase loop, and decide based on the journal entries.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		return runSpec(ctx, cancel, args[0])
	},
}

func init() {
	runCmd.Flags().BoolVar(&runSingleShot, "single-shot", false, "single-shot mode: one agent invocation tries all phases")
	runCmd.Flags().IntVar(&runMaxIter, "max-iterations", 0, "iteration cap (overrides .bcc.toml; 0 = use config)")
	runCmd.Flags().StringSliceVar(&runEnvFlags, "env", nil, "KEY=VALUE env override (repeatable; highest precedence)")
	runCmd.Flags().StringVar(&runExtra, "extra", "", "additional instructions appended to the prompt")
	runCmd.Flags().StringVar(&runConfigPath, "config", "", "path to .bcc.toml (overrides discovery)")
	runCmd.Flags().BoolVarP(&runAllowDirty, "allow-dirty", "d", false, "skip the pre-flight clean-tree check (the agent will see the dirty tree)")
	runCmd.Flags().StringVar(&runOutput, "output", OutputTUI, "render backend: tui|text|json")
	runCmd.Flags().StringVar(&runVerbosity, "verbosity", loop.LevelInfo.String(), "event level low-water mark: error|warn|info|debug|trace")
	runCmd.Flags().BoolVar(&runNoColor, "no-color", false, "disable color output (lipgloss styles render as plain text)")
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

	if runNoColor {
		tui.DisableColor()
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

	if runOutput == OutputTUI {
		return runWithTUI(ctx, cancel, l, specPath, branchHint, verbosity)
	}

	events, drained, err := dispatchEvents(runOutput, verbosity)
	if err != nil {
		ExitCode = loop.ExitInvalid
		return err
	}

	code, runErr := l.Run(ctx, events)
	<-drained
	ExitCode = code
	return runErr
}

// runWithTUI inverts the foreground/background relationship for TUI
// mode: bubbletea owns the main goroutine (it must, to read keys and
// drive renders), and the loop runs in a goroutine. The loop's events
// flow into the bubbletea program via the bridge tea.Cmd.
//
// The pause gate is wired here so a single source-of-truth lives in
// the TUI Model: the user toggles paused via the keyboard, the gate
// posts release tokens, and the Loop blocks on PauseGate before each
// iteration after the first.
func runWithTUI(ctx context.Context, cancel context.CancelFunc, l *loop.Loop, specPath, branch string, verbosity loop.Level) error {
	// The TUI ignores --verbosity per the Phase 2 spec ("TUI panels are
	// already curated"); raw is consumed directly. The verbosity arg is
	// kept on the function signature so callers do not branch.
	_ = verbosity
	raw := make(chan loop.Event, 256)

	gate := tui.NewGate()
	l.PauseGate = gate.Chan()

	specCfg := tui.SpecConfig{
		PlanHeading:    l.Config.Specs.PlanHeading,
		JournalHeading: l.Config.Specs.JournalHeading,
		ResultKeyword:  l.Config.Specs.ResultKeyword,
		ResultVocab: spec.ResultVocab{
			OK:      l.Config.Loop.Results.OK,
			Partial: l.Config.Loop.Results.Partial,
			Done:    l.Config.Loop.Results.Done,
			Blocked: l.Config.Loop.Results.Blocked,
			Review:  l.Config.Loop.Results.Review,
		},
	}

	gitProbeAdapter, _ := l.Git.(tui.GitProbe)
	specReaderAdapter, _ := l.SpecReader.(tui.SpecReader)

	model := tui.New(tui.Options{
		Events:     raw,
		Cancel:     cancel,
		Gate:       gate,
		SpecPath:   specPath,
		Branch:     branch,
		MaxIter:    l.Config.Loop.MaxIterations,
		SpecReader: specReaderAdapter,
		GitProbe:   gitProbeAdapter,
		GitCtx:     ctx,
		SpecConfig: specCfg,
	})
	// WithoutSignalHandler: signal.NotifyContext in RunE owns SIGINT /
	// SIGTERM; bubbletea must not install a competing handler.
	program := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	)

	// Belt-and-braces terminal restoration: program.Run() restores the
	// terminal during normal exit and bubbletea installs its own panic
	// handler for in-program panics, but neither protects against an
	// outer panic in this function before Run returns. The deferred
	// ReleaseTerminal undoes the alt-screen / raw mode in that path so
	// the user is not left staring at a broken terminal.
	defer func() {
		if r := recover(); r != nil {
			_ = program.ReleaseTerminal()
			fmt.Fprintf(os.Stderr, "bcc: panic in TUI host: %v\n%s\n", r, debug.Stack())
			panic(r)
		}
	}()

	type runResult struct {
		code int
		err  error
	}
	runCh := make(chan runResult, 1)
	go func() {
		// Convert a loop-goroutine panic into an error on runCh and
		// signal the program to quit. Without this, l.Run panicking
		// would crash the process before bubbletea restores the
		// terminal, leaving the user in alt-screen with no shell prompt.
		defer func() {
			if r := recover(); r != nil {
				program.Quit()
				runCh <- runResult{
					code: loop.ExitInvalid,
					err:  fmt.Errorf("loop panicked: %v\n%s", r, debug.Stack()),
				}
			}
		}()
		code, err := l.Run(ctx, raw)
		runCh <- runResult{code: code, err: err}
	}()

	if _, err := program.Run(); err != nil {
		cancel()
		<-runCh
		return err
	}

	res := <-runCh
	ExitCode = res.code
	return res.err
}

func validOutputMode(s string) bool {
	switch s {
	case OutputTUI, OutputText, OutputJSON:
		return true
	}
	return false
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
