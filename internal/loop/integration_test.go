package loop_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/config"
	"github.com/fgmacedo/buchecha/internal/executor/fake"
	gitcli "github.com/fgmacedo/buchecha/internal/git/cli"
	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/specreader/markdown"
)

// initIntegrationRepo creates a fresh git repo + initial spec, returns
// (repoDir, specPath). Skips if git is missing.
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
	if err := os.WriteFile(specPath, []byte(specWith([]string{"[ ]", "[ ]"}, "")), 0o644); err != nil {
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

// editingExec wraps a fake.Executor with a hook that mutates the spec
// file and commits between iterations, simulating an agent's edits and
// HEAD advance.
type editingExec struct {
	fake     *fake.Executor
	specPath string
	repoDir  string
	t        *testing.T
	updates  []func() // updates[i] runs before iteration i+1
	idx      int
}

func (e *editingExec) Run(ctx context.Context, prompt string, w io.Writer) (int, error) {
	if e.idx < len(e.updates) {
		e.updates[e.idx]()
		e.idx++
	}
	return e.fake.Run(ctx, prompt, w)
}

func TestIntegration_TwoIterToDone(t *testing.T) {
	dir, specPath := initIntegrationRepo(t)

	// Apply iteration-1 update: agent marks first item, sets Result: ok.
	iter1 := func() {
		_ = os.WriteFile(specPath, []byte(specWith([]string{"[x]", "[ ]"}, "ok")), 0o644)
		runShell(t, dir, "git", "add", ".")
		runShell(t, dir, "git", "commit", "-m", "iter1")
	}
	// Apply iteration-2 update: agent finishes, sets Result: done.
	iter2 := func() {
		_ = os.WriteFile(specPath, []byte(specWith([]string{"[x]", "[x]"}, "done")), 0o644)
		runShell(t, dir, "git", "add", ".")
		runShell(t, dir, "git", "commit", "-m", "iter2")
	}

	exec := &editingExec{
		fake: fake.New(
			fake.Step{JSONL: `{"type":"system"}` + "\n", ExitCode: 0},
			fake.Step{JSONL: `{"type":"result"}` + "\n", ExitCode: 0},
		),
		specPath: specPath,
		repoDir:  dir,
		t:        t,
		updates:  []func(){iter1, iter2},
	}

	cfg := newTestConfig()
	jsonlDir := t.TempDir()

	l := &loop.Loop{
		SpecPath:   specPath,
		Config:     cfg,
		Executor:   exec,
		Git:        gitcli.New(dir),
		SpecReader: markdown.New(),
		JSONLDir:   jsonlDir,
	}

	code, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != loop.ExitDone {
		t.Errorf("exit = %d, want %d (ExitDone)", code, loop.ExitDone)
	}
	if exec.fake.CallCount() != 2 {
		t.Errorf("executor calls = %d, want 2", exec.fake.CallCount())
	}

	// JSONL files should exist for both iterations.
	matches, err := filepath.Glob(filepath.Join(jsonlDir, "spec-iter*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 jsonl files, got %d: %v", len(matches), matches)
	}
	// Iter 1 JSONL must contain the scripted system event.
	iter1JSONL, err := os.ReadFile(filepath.Join(jsonlDir, "spec-iter1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(iter1JSONL), `"system"`) {
		t.Errorf("iter1 jsonl missing scripted content: %q", string(iter1JSONL))
	}
}

func TestIntegration_HEADStuckOnNoCommit(t *testing.T) {
	dir, specPath := initIntegrationRepo(t)

	// Agent runs but does NOT commit anything; spec file is unchanged.
	exec := &editingExec{
		fake: fake.New(fake.Step{JSONL: `{"type":"system"}` + "\n", ExitCode: 0}),
		// No updates → HEAD does not advance.
		updates:  nil,
		specPath: specPath,
		repoDir:  dir,
		t:        t,
	}

	cfg := newTestConfig()

	l := &loop.Loop{
		SpecPath:   specPath,
		Config:     cfg,
		Executor:   exec,
		Git:        gitcli.New(dir),
		SpecReader: markdown.New(),
		JSONLDir:   t.TempDir(),
	}

	code, err := l.Run(context.Background())
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

// Reuse the loop_test.go helper specWith if visible; otherwise inline.
// (specWith is package-local in loop_test.go and visible here because we
// share the loop_test package.)

// Helper to pretty-print specs in test failures.
func dumpSpec(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Logf("dump %s: %v", path, err)
		return
	}
	t.Logf("--- %s ---\n%s\n--- end ---", path, b)
}

// silenceUnused: make sure dumpSpec / configWithDefaults compile even
// when not invoked in a particular subtest. Cheap way to keep them
// available without tripping the linter.
var (
	_ = dumpSpec
	_ = configWithDefaults
	_ = fmt.Sprintf
)
