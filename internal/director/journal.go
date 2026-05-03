package director

import (
	"bufio"
	"bytes"
	"context"
	"strings"
)

// JournalHeading is the canonical H2 the Director's review pipeline
// expects in the spec: the section the Executor appends entries to and
// the section bcc reads to compute the per-iteration journal delta. The
// markdown_bcc adapter localizes this for the agent's prompt at the
// format layer; the director domain pins one canonical heading because
// the Reviewer must agree with bcc on where to look.
const JournalHeading = "## Execution Journal"

// GatherJournalDelta returns the text appended to the Execution Journal
// section between the before and after snapshots of a spec. When the
// journal is unchanged or absent in both, the result is empty.
//
// "Appended" is defined as the new prefix of the after-section that is
// not present at the start of the before-section. Director runs in a
// "new entry on top" convention; entries pushed earlier in time stay
// at the bottom and never change.
//
// The function is a pure text op: no I/O, no allocations beyond the
// returned string. Callers feed bytes from disk; we never read the
// filesystem here.
func GatherJournalDelta(specBefore, specAfter []byte) string {
	before := journalSection(specBefore)
	after := journalSection(specAfter)
	if after == "" || after == before {
		return ""
	}
	if before == "" {
		return after
	}
	if strings.HasSuffix(after, before) {
		return strings.TrimRight(strings.TrimSuffix(after, before), "\n")
	}
	return after
}

// journalSection returns the body of the Execution Journal section,
// excluding the heading line itself, terminating at the next H2 (or
// EOF). The returned string preserves internal newlines.
func journalSection(spec []byte) string {
	if len(spec) == 0 {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(spec))
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var (
		body   strings.Builder
		inside bool
	)
	for sc.Scan() {
		line := sc.Text()
		if !inside {
			if strings.TrimSpace(line) == JournalHeading {
				inside = true
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	return body.String()
}

// GitDiffer is the read-only working-tree probe the Director's review
// pipeline needs to capture the diff between two SHAs. The loop's
// GitProbe satisfies this structurally; defining it here keeps the
// director domain free of an internal/loop import.
type GitDiffer interface {
	Diff(ctx context.Context, baseSHA, headSHA string) (string, error)
}

// GatherDiff returns the unified diff between baseSHA and headSHA via
// git. It is a thin wrapper over GitDiffer.Diff; callers route through
// this entry point so the review pipeline always speaks the same shape
// to the Reviewer.
func GatherDiff(ctx context.Context, git GitDiffer, baseSHA, headSHA string) (string, error) {
	return git.Diff(ctx, baseSHA, headSHA)
}
