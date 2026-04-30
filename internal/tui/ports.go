package tui

import (
	"context"
)

// GitProbe is the read-only git view the TUI consumes for the "if you
// close now" panel. The cli adapter (internal/git/cli) implements this
// alongside loop.GitProbe; structural typing keeps both ports
// independent so neither package imports the other.
//
// CommitsSince returns the number of commits between the given baseline
// SHA and HEAD. The TUI feeds it with the BaselineSHA captured from the
// first IterationStarted event so the panel can show how many commits
// the bcc run itself has produced.
type GitProbe interface {
	DirtyFileCount(ctx context.Context) (int, error)
	CommitsSince(ctx context.Context, sha string) (int, error)
}
