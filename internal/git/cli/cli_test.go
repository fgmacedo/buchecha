package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitLeakVars are environment variables that, when inherited from a parent
// git process (e.g. a pre-commit hook running on the outer worktree), make
// `git` invocations inside a brand-new temp repo discover the wrong
// repository or run the outer repo's hooks against a directory that does
// not have the matching config (notably .pre-commit-config.yaml). Unsetting
// them for the test process forces every child git, both the helper below
// and the production Probe under test, to bootstrap solely from cmd.Dir.
var gitLeakVars = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_COMMON_DIR",
	"GIT_INDEX_FILE",
	"GIT_PREFIX",
	"GIT_EXEC_PATH",
}

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
}

// initRepo creates a fresh git repo at t.TempDir() and configures author
// data. Returns the directory path.
func initRepo(t *testing.T) string {
	t.Helper()
	skipIfNoGit(t)
	// Unset leaked git env vars for the duration of this test. See
	// gitLeakVars: when the test process inherits GIT_DIR / GIT_WORK_TREE
	// etc. from an outer git invocation (e.g. a pre-commit hook running on
	// the buchecha worktree), every `git` subprocess, both the test helper
	// and the production Probe under test, would otherwise target the
	// outer repo instead of the temp dir. t.Setenv-with-empty is rejected
	// by git ("empty string is not a valid path"), so we Unsetenv and
	// restore the originals via t.Cleanup.
	for _, k := range gitLeakVars {
		orig, present := os.LookupEnv(k)
		os.Unsetenv(k)
		t.Cleanup(func() {
			if present {
				os.Setenv(k, orig)
			} else {
				os.Unsetenv(k)
			}
		})
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeAndCommit(t *testing.T, dir, file, content, msg string) {
	t.Helper()
	p := filepath.Join(dir, file)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	runGit(t, dir, "add", file)
	runGit(t, dir, "commit", "-m", msg)
}

func TestProbe_HeadSHA(t *testing.T) {
	dir := initRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello", "first")

	p := New(dir)
	sha, err := p.HeadSHA(context.Background())
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("sha = %q, want 40 chars", sha)
	}
}

func TestProbe_HeadSHA_Advances(t *testing.T) {
	dir := initRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello", "first")
	p := New(dir)
	first, err := p.HeadSHA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, dir, "b.txt", "world", "second")
	second, err := p.HeadSHA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Errorf("HeadSHA did not advance after commit: %q == %q", first, second)
	}
}

func TestProbe_CurrentBranch(t *testing.T) {
	dir := initRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello", "first")

	p := New(dir)
	br, err := p.CurrentBranch(context.Background())
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if br != "main" {
		t.Errorf("branch = %q, want main", br)
	}
}

func TestProbe_IsClean_True(t *testing.T) {
	dir := initRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello", "first")

	p := New(dir)
	clean, err := p.IsClean(context.Background())
	if err != nil {
		t.Fatalf("IsClean: %v", err)
	}
	if !clean {
		t.Errorf("expected clean working tree")
	}
}

func TestProbe_IsClean_FalseWithUntracked(t *testing.T) {
	dir := initRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello", "first")

	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("u"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := New(dir)
	clean, err := p.IsClean(context.Background())
	if err != nil {
		t.Fatalf("IsClean: %v", err)
	}
	if clean {
		t.Errorf("expected dirty after creating untracked file")
	}
}

func TestProbe_DirtyFileCount_Zero(t *testing.T) {
	dir := initRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello", "first")

	p := New(dir)
	n, err := p.DirtyFileCount(context.Background())
	if err != nil {
		t.Fatalf("DirtyFileCount: %v", err)
	}
	if n != 0 {
		t.Errorf("count = %d, want 0 on clean tree", n)
	}
}

func TestProbe_DirtyFileCount_CountsUntrackedAndModified(t *testing.T) {
	dir := initRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello", "first")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("u"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("u2"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := New(dir)
	n, err := p.DirtyFileCount(context.Background())
	if err != nil {
		t.Fatalf("DirtyFileCount: %v", err)
	}
	if n != 3 {
		t.Errorf("count = %d, want 3 (1 modified + 2 untracked)", n)
	}
}

func TestProbe_CommitsSince_ZeroOnSameSHA(t *testing.T) {
	dir := initRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello", "first")

	p := New(dir)
	sha, err := p.HeadSHA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n, err := p.CommitsSince(context.Background(), sha)
	if err != nil {
		t.Fatalf("CommitsSince: %v", err)
	}
	if n != 0 {
		t.Errorf("count = %d, want 0 when HEAD == baseline", n)
	}
}

func TestProbe_CommitsSince_CountsAdvances(t *testing.T) {
	dir := initRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello", "first")

	p := New(dir)
	baseline, err := p.HeadSHA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, dir, "b.txt", "world", "second")
	writeAndCommit(t, dir, "c.txt", "again", "third")

	n, err := p.CommitsSince(context.Background(), baseline)
	if err != nil {
		t.Fatalf("CommitsSince: %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2 commits since baseline", n)
	}
}

func TestProbe_CommitsSince_EmptySHARejected(t *testing.T) {
	dir := initRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello", "first")

	p := New(dir)
	if _, err := p.CommitsSince(context.Background(), ""); err == nil {
		t.Errorf("expected error for empty baseline sha")
	}
}

func TestProbe_HeadSHA_NoCommits(t *testing.T) {
	dir := initRepo(t)
	// no commits yet
	p := New(dir)
	_, err := p.HeadSHA(context.Background())
	if err == nil {
		t.Errorf("expected error when no commits exist")
	}
}
