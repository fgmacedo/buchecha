package loop_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/config"
	"github.com/fgmacedo/buchecha/internal/executor/fake"
	gitcli "github.com/fgmacedo/buchecha/internal/git/cli"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// initIntegrationRepo creates a fresh git repo + initial spec file.
// Returns (repoDir, specPath). Skips when git is missing.
func initIntegrationRepo(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	runShell(t, dir, "git", "init", "-b", "main")
	runShell(t, dir, "git", "config", "user.email", "test@test.com")
	runShell(t, dir, "git", "config", "user.name", "Test")
	runShell(t, dir, "git", "config", "commit.gpgsign", "false")

	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("# spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runShell(t, dir, "git", "add", ".")
	runShell(t, dir, "git", "commit", "-m", "init spec")
	return dir, specPath
}

func runShell(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

// committingExec wraps a fake.Executor with a hook that performs a real
// git commit on the repo before each iteration's events are emitted,
// simulating an agent that does work and commits.
type committingExec struct {
	fake     *fake.Executor
	specPath string
	repoDir  string
	t        *testing.T
	commits  []bool // commits[i]=true means iteration i+1 makes a commit
	idx      int
}

func (e *committingExec) Run(ctx context.Context, prompt string, events chan<- loop.AgentEvent) (loop.ExecResult, error) {
	if e.idx < len(e.commits) && e.commits[e.idx] {
		// Touch the spec, stage, commit.
		_ = os.WriteFile(e.specPath, []byte(fmt.Sprintf("# spec iter %d\n", e.idx+1)), 0o644)
		runShell(e.t, e.repoDir, "git", "add", ".")
		runShell(e.t, e.repoDir, "git", "commit", "-m", fmt.Sprintf("iter%d", e.idx+1))
	}
	e.idx++
	return e.fake.Run(ctx, prompt, events)
}

func TestIntegration_TwoIterToDone(t *testing.T) {
	dir, specPath := initIntegrationRepo(t)

	// Two iterations: first emits "continue" + commits, second emits "done"
	// + commits.
	executor := &committingExec{
		fake: fake.New(
			fake.Step{Events: []loop.AgentEvent{
				{Kind: loop.KindBccEvent, At: time.Now(), Bcc: &agentcontract.BccEvent{
					Kind: agentcontract.BccEventIterationResult, Signal: agentcontract.SignalContinue,
				}},
			}},
			fake.Step{Events: []loop.AgentEvent{
				{Kind: loop.KindBccEvent, At: time.Now(), Bcc: &agentcontract.BccEvent{
					Kind: agentcontract.BccEventIterationResult, Signal: agentcontract.SignalDone,
				}},
			}},
		),
		specPath: specPath, repoDir: dir, t: t,
		commits: []bool{true, true},
	}

	cfg := newTestConfig()

	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Executor: executor,
		Git:      gitcli.New(dir),
		Briefing: fakeBriefing{},
	}

	events := make(chan loop.Event, 1024)
	code, err := l.Run(context.Background(), events)
	for range events {
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want %d (ExitDone)", code, loop.ExitDone)
	}
	if executor.fake.CallCount() != 2 {
		t.Errorf("executor calls = %d, want 2", executor.fake.CallCount())
	}
}

func TestIntegration_HEADStuckOnNoCommit(t *testing.T) {
	dir, specPath := initIntegrationRepo(t)

	// Agent emits "continue" but does NOT commit anything.
	executor := &committingExec{
		fake: fake.New(fake.Step{Events: []loop.AgentEvent{
			{Kind: loop.KindBccEvent, At: time.Now(), Bcc: &agentcontract.BccEvent{
				Kind: agentcontract.BccEventIterationResult, Signal: agentcontract.SignalContinue,
			}},
		}}),
		specPath: specPath, repoDir: dir, t: t,
		commits: []bool{false},
	}

	cfg := newTestConfig()

	l := &loop.Loop{
		SpecPath: specPath,
		Config:   cfg,
		Executor: executor,
		Git:      gitcli.New(dir),
		Briefing: fakeBriefing{},
	}

	events := make(chan loop.Event, 1024)
	code, err := l.Run(context.Background(), events)
	for range events {
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitHEADStuck {
		t.Errorf("exit = %d, want %d (ExitHEADStuck)", code, loop.ExitHEADStuck)
	}
}

// configWith returns a Config with defaults applied; alias to keep the
// integration test self-contained.
func configWithDefaults() *config.Config {
	c := &config.Config{}
	config.ApplyDefaults(c)
	return c
}

// dumpSpec is a debug helper retained for ad-hoc test diagnosis.
func dumpSpec(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Logf("dump %s: %v", path, err)
		return
	}
	t.Logf("--- %s ---\n%s\n--- end ---", path, b)
}

var (
	_ = dumpSpec
	_ = configWithDefaults
)
