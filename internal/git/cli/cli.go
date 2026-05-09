// Package cli implements loop.GitProbe by shelling out to the git binary.
//
// All operations are read-only. Mutations (commits, branch creation,
// pushes) are performed by the agent inside its iteration; the loop only
// observes state.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/fgmacedo/buchecha/internal/loop"
)

// Compile-time check that *Probe satisfies loop.GitProbe.
var _ loop.GitProbe = (*Probe)(nil)

// Probe runs git commands in Dir. Empty Dir means cwd.
type Probe struct {
	Dir string
}

// New returns a Probe rooted at dir. Empty dir means cwd.
func New(dir string) *Probe {
	return &Probe{Dir: dir}
}

// HeadSHA returns the SHA of HEAD as a 40-char string.
func (p *Probe) HeadSHA(ctx context.Context) (string, error) {
	return p.run(ctx, "rev-parse", "HEAD")
}

// CurrentBranch returns the name of the current branch (empty when in
// detached HEAD; the caller decides what to do with that).
func (p *Probe) CurrentBranch(ctx context.Context) (string, error) {
	return p.run(ctx, "branch", "--show-current")
}

// IsClean reports whether the working tree has no uncommitted changes
// and no untracked files (porcelain output is empty).
func (p *Probe) IsClean(ctx context.Context) (bool, error) {
	out, err := p.run(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// DirtyFileCount returns the number of entries in `git status --porcelain`,
// i.e. the count of files with uncommitted changes or untracked files. The
// TUI's "if you close now" panel reads this to surface what would be lost
// on a sudden exit. Equivalent to IsClean but quantitative.
func (p *Probe) DirtyFileCount(ctx context.Context) (int, error) {
	out, err := p.run(ctx, "status", "--porcelain")
	if err != nil {
		return 0, err
	}
	if out == "" {
		return 0, nil
	}
	return strings.Count(out, "\n") + 1, nil
}

// CommitsSince returns the number of commits between sha and HEAD,
// counted as `git rev-list --count <sha>..HEAD`. The TUI feeds this with
// the BaselineSHA from the first IterationStarted event so the "if you
// close now" panel can show how many commits the run produced. When
// HEAD == sha, the count is zero.
func (p *Probe) CommitsSince(ctx context.Context, sha string) (int, error) {
	if sha == "" {
		return 0, fmt.Errorf("git rev-list: empty baseline sha")
	}
	out, err := p.run(ctx, "rev-list", "--count", sha+"..HEAD")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("git rev-list --count %s..HEAD: parse %q: %w", sha, out, err)
	}
	return n, nil
}

func (p *Probe) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if p.Dir != "" {
		cmd.Dir = p.Dir
	}
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git %s: %w (stderr: %s)",
				strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
