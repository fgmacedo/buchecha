package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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

func TestProbe_HeadSHA_NoCommits(t *testing.T) {
	dir := initRepo(t)
	// no commits yet
	p := New(dir)
	_, err := p.HeadSHA(context.Background())
	if err == nil {
		t.Errorf("expected error when no commits exist")
	}
}
