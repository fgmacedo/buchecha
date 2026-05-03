package director

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGatherJournalDelta(t *testing.T) {
	cases := []struct {
		name   string
		before string
		after  string
		want   string
	}{
		{
			name:   "no journal in either",
			before: "# spec\n\n## Implementation Plan\n",
			after:  "# spec\n\n## Implementation Plan\n",
			want:   "",
		},
		{
			name:   "journal added from scratch",
			before: "# spec\n\n## Implementation Plan\n",
			after:  "# spec\n\n## Implementation Plan\n\n## Execution Journal\n\n### entry one\n\nnew text\n",
			want:   "\n### entry one\n\nnew text\n",
		},
		{
			name:   "entry prepended on top",
			before: "# spec\n\n## Execution Journal\n\n### old entry\n\nold text\n",
			after:  "# spec\n\n## Execution Journal\n\n### new entry\n\nfresh text\n\n### old entry\n\nold text\n",
			want:   "\n### new entry\n\nfresh text",
		},
		{
			name:   "no change",
			before: "## Execution Journal\n\n### a\n\nbody\n",
			after:  "## Execution Journal\n\n### a\n\nbody\n",
			want:   "",
		},
		{
			name:   "journal followed by another H2",
			before: "## Execution Journal\n\n### a\n\nbody\n\n## References\nlinks\n",
			after:  "## Execution Journal\n\n### b\n\nnew\n\n### a\n\nbody\n\n## References\nlinks\n",
			want:   "\n### b\n\nnew",
		},
		{
			name:   "empty inputs",
			before: "",
			after:  "",
			want:   "",
		},
		{
			name:   "after has reordered body, not a clean prepend",
			before: "## Execution Journal\n\n### a\n\nbody\n",
			after:  "## Execution Journal\n\n### a\n\nedited\n",
			want:   "\n### a\n\nedited\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GatherJournalDelta([]byte(tc.before), []byte(tc.after))
			if got != tc.want {
				t.Errorf("delta mismatch\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// fakeGitDiffer is a hand-rolled stub for GatherDiff tests that does
// not need its own package: it captures arguments and returns scripted
// outputs.
type fakeGitDiffer struct {
	diff string
	err  error
	gotB string
	gotH string
}

func (f *fakeGitDiffer) Diff(_ context.Context, base, head string) (string, error) {
	f.gotB = base
	f.gotH = head
	return f.diff, f.err
}

func TestGatherDiff_PassthroughAndArguments(t *testing.T) {
	d := &fakeGitDiffer{diff: "diff body\n"}
	got, err := GatherDiff(context.Background(), d, "BASE", "HEAD")
	if err != nil {
		t.Fatalf("GatherDiff: %v", err)
	}
	if got != "diff body\n" {
		t.Errorf("diff = %q, want %q", got, "diff body\n")
	}
	if d.gotB != "BASE" || d.gotH != "HEAD" {
		t.Errorf("forwarded args = (%q,%q), want (BASE,HEAD)", d.gotB, d.gotH)
	}
}

func TestGatherDiff_PropagatesError(t *testing.T) {
	want := errors.New("git boom")
	d := &fakeGitDiffer{err: want}
	_, err := GatherDiff(context.Background(), d, "a", "b")
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// guard against typo in the canonical journal heading; specs and the
// markdown_bcc adapter share this string verbatim.
func TestJournalHeading_Canonical(t *testing.T) {
	if !strings.HasPrefix(JournalHeading, "## ") {
		t.Errorf("JournalHeading must be an H2 heading, got %q", JournalHeading)
	}
	if JournalHeading != "## Execution Journal" {
		t.Errorf("JournalHeading drifted: %q", JournalHeading)
	}
}
