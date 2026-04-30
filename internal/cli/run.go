package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
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
	// know what the agent will see in each iteration. BCC_ITERATION
	// varies per iteration; the names and meanings are fixed.
	branchHint := ""
	if br, gerr := gitProbe.CurrentBranch(ctx); gerr == nil {
		branchHint = br
	}
	fmt.Fprintf(os.Stderr,
		"bcc: agent subprocess will see: BCC_RUNNING=1, BCC_ITERATION=<n>, "+
			"BCC_MAX_ITERATIONS=%d, BCC_SPEC_PATH=%s, BCC_BRANCH=%s\n",
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
		buildLoop := func() *loop.Loop {
			return &loop.Loop{
				SpecPath:   specPath,
				Config:     cfg,
				Executor:   executor,
				Git:        gitProbe,
				SpecReader: specReader,
				Extra:      runExtra,
				SingleShot: runSingleShot,
			}
		}
		return runWithTUI(ctx, cancel, buildLoop, specPath, branchHint, verbosity, runNoColor)
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
//
// buildLoop is a factory that returns a fresh loop.Loop on every call.
// The first call drives the inner loop; subsequent calls fire when the
// user resumes the run from the post-loop session menu (P2.11). The
// factory captures all loop construction parameters in its closure so
// the host needs no other knowledge to spawn replacement runs.
func runWithTUI(ctx context.Context, cancel context.CancelFunc, buildLoop func() *loop.Loop, specPath, branch string, verbosity loop.Level, noColor bool) error {
	// The TUI ignores --verbosity per the Phase 2 spec ("TUI panels are
	// already curated"); raw is consumed directly. The verbosity arg is
	// kept on the function signature so callers do not branch.
	_ = verbosity

	first := buildLoop()
	gate := tui.NewGate()

	// In TUI mode, the loop's milestone slog calls (`loop start`,
	// `iter start`, ...) are duplicated as IterationStarted /
	// IterationFinished / LoopFinished events on the channel and
	// surfaced by panels. Pin a discard logger on the Loop so those
	// duplicate messages never reach stderr; otherwise they smear into
	// the alt-screen scrollback and become visible when the program
	// exits.
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	first.Logger = discard
	first.PauseGate = gate.Chan()

	specCfg := tui.SpecConfig{
		PlanHeading:    first.Config.Specs.PlanHeading,
		JournalHeading: first.Config.Specs.JournalHeading,
		ResultKeyword:  first.Config.Specs.ResultKeyword,
		ResultVocab: spec.ResultVocab{
			OK:      first.Config.Loop.Results.OK,
			Partial: first.Config.Loop.Results.Partial,
			Done:    first.Config.Loop.Results.Done,
			Blocked: first.Config.Loop.Results.Blocked,
			Review:  first.Config.Loop.Results.Review,
		},
	}

	gitProbeAdapter, _ := first.Git.(tui.GitProbe)
	specReaderAdapter, _ := first.SpecReader.(tui.SpecReader)

	type runResult struct {
		code int
		err  error
	}
	var (
		resultMu     sync.Mutex
		latestResult runResult
	)

	// runOne spawns one loop in a goroutine. The result is stashed in
	// latestResult under resultMu; the host returns it once the
	// bubbletea program exits (after [q] / Ctrl+C in session mode, or
	// after a "user cancelled" / "fatal" LoopFinished closes the
	// channel and Quit fires).
	runOne := func(l *loop.Loop, events chan<- loop.Event) {
		defer func() {
			if r := recover(); r != nil {
				resultMu.Lock()
				latestResult = runResult{
					code: loop.ExitInvalid,
					err:  fmt.Errorf("loop panicked: %v\n%s", r, debug.Stack()),
				}
				resultMu.Unlock()
			}
		}()
		code, err := l.Run(ctx, events)
		resultMu.Lock()
		latestResult = runResult{code: code, err: err}
		resultMu.Unlock()
	}

	// newEvents builds the next loop run on demand: invoked by the
	// Model when the user presses [r] in the session menu. Each call
	// produces a fresh events channel that the Model rebinds to its
	// bridge cmd.
	newEvents := func() <-chan loop.Event {
		ch := make(chan loop.Event, 256)
		l := buildLoop()
		l.Logger = discard
		l.PauseGate = gate.Chan()
		go runOne(l, ch)
		return ch
	}

	raw := make(chan loop.Event, 256)

	model := tui.New(tui.Options{
		Events:     raw,
		Cancel:     cancel,
		Gate:       gate,
		SpecPath:   specPath,
		Branch:     branch,
		MaxIter:    first.Config.Loop.MaxIterations,
		SpecReader: specReaderAdapter,
		GitProbe:   gitProbeAdapter,
		GitCtx:     ctx,
		SpecConfig: specCfg,
		NewEvents:  newEvents,
	})
	// WithoutSignalHandler: signal.NotifyContext in RunE owns SIGINT /
	// SIGTERM; bubbletea must not install a competing handler. Alt-screen
	// and mouse cell motion are now properties of the Model's tea.View
	// (set in Model.View), no longer ProgramOptions in v2. --no-color
	// becomes a colour-profile override on the Program: passing
	// colorprofile.NoTTY forces the renderer to strip every SGR escape
	// (colours and resets) so the output reaches the terminal as plain
	// text, matching the v1 behaviour of termenv.Ascii.
	progOpts := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	}
	if noColor {
		progOpts = append(progOpts, tea.WithColorProfile(colorprofile.NoTTY))
	}
	program := tea.NewProgram(model, progOpts...)
	// SetProgram wires the program reference into the model so the
	// session menu's [e] action can call ReleaseTerminal /
	// RestoreTerminal around the $EDITOR invocation.
	model.SetProgram(program)

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

	go runOne(first, raw)

	if _, err := program.Run(); err != nil {
		cancel()
		return err
	}

	resultMu.Lock()
	defer resultMu.Unlock()
	ExitCode = latestResult.code
	return latestResult.err
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
