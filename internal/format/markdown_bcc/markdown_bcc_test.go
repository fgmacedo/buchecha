package markdown_bcc

import (
	"context"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/loop"
)

func TestNew_FillsDefaults(t *testing.T) {
	r := New(Config{})
	if r.cfg.PlanHeading != "## Implementation Plan" {
		t.Errorf("PlanHeading default = %q", r.cfg.PlanHeading)
	}
	if r.cfg.JournalHeading != "## Execution Journal" {
		t.Errorf("JournalHeading default = %q", r.cfg.JournalHeading)
	}
	if r.cfg.JournalStore != "markdown_inspec" {
		t.Errorf("JournalStore default = %q", r.cfg.JournalStore)
	}
}

func TestBuildPrompt_LoopMode_IncludesContractCore(t *testing.T) {
	r := New(Config{MaxIterations: 20})
	got, err := r.BuildPrompt(context.Background(), loop.BriefingInput{
		SpecPath:  "docs/specs/foo.md",
		Iteration: 3,
		Mode:      loop.ModeLoop,
	})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	for _, want := range []string{
		"docs/specs/foo.md",
		"`loop`",
		"`3` of `20`",
		"## Implementation Plan",
		"## Execution Journal",
		`"event":"task_started"`,
		`"event":"task_completed"`,
		`"event":"iteration_result"`,
		"`continue`",
		"`review`",
		"`done`",
		"`blocked`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildPrompt_SingleShotMode_DropsLoopProcedure(t *testing.T) {
	r := New(Config{})
	got, err := r.BuildPrompt(context.Background(), loop.BriefingInput{
		SpecPath: "x.md",
		Mode:     loop.ModeSingleShot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "single-shot") {
		t.Errorf("prompt missing 'single-shot'")
	}
	if strings.Contains(got, "implement **one pending work unit** per invocation") {
		t.Errorf("single-shot prompt should not include loop-mode procedure")
	}
}

func TestBuildPrompt_JournalNone_SuppressesJournalInstructions(t *testing.T) {
	r := New(Config{JournalStore: "none"})
	got, err := r.BuildPrompt(context.Background(), loop.BriefingInput{Mode: loop.ModeLoop})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Do not write a journal") {
		t.Errorf("none store should yield 'Do not write a journal'")
	}
	if strings.Contains(got, "Append a new entry") {
		t.Errorf("none store should not instruct journal appends")
	}
}

func TestBuildPrompt_JournalFile_MentionsPath(t *testing.T) {
	r := New(Config{JournalStore: "file", JournalPath: ".bcc/journal.ndjson"})
	got, err := r.BuildPrompt(context.Background(), loop.BriefingInput{Mode: loop.ModeLoop})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, ".bcc/journal.ndjson") {
		t.Errorf("file store should reference the configured path")
	}
}

func TestBuildPrompt_Extra_AppendedAtEnd(t *testing.T) {
	r := New(Config{})
	got, err := r.BuildPrompt(context.Background(), loop.BriefingInput{
		Mode:  loop.ModeLoop,
		Extra: "Fail the build if vendoring drifts.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Additional instructions from the invoker") {
		t.Errorf("extra should be framed as additional instructions")
	}
	if !strings.Contains(got, "Fail the build if vendoring drifts.") {
		t.Errorf("extra body missing")
	}
}

func TestBuildPrompt_Extra_OmittedWhenEmpty(t *testing.T) {
	r := New(Config{})
	got, err := r.BuildPrompt(context.Background(), loop.BriefingInput{Mode: loop.ModeLoop})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Additional instructions from the invoker") {
		t.Errorf("empty extra should not render the section header")
	}
}

func TestParseLine_TaskStarted(t *testing.T) {
	r := New(Config{})
	in := []byte(`{"type":"bcc_event","event":"task_started","id":"P1.2","summary":"start it"}`)
	got, ok := r.ParseLine(in)
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if got.Kind != loop.BccEventTaskStarted {
		t.Errorf("Kind = %v", got.Kind)
	}
	if got.ID != "P1.2" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Summary != "start it" {
		t.Errorf("Summary = %q", got.Summary)
	}
}

func TestParseLine_TaskCompleted(t *testing.T) {
	r := New(Config{})
	got, ok := r.ParseLine([]byte(`{"type":"bcc_event","event":"task_completed","id":"P1.2"}`))
	if !ok {
		t.Fatal("ok = false")
	}
	if got.Kind != loop.BccEventTaskCompleted || got.ID != "P1.2" {
		t.Errorf("got %+v", got)
	}
}

func TestParseLine_IterationResult_MapsValuesToSignal(t *testing.T) {
	cases := []struct {
		value string
		want  loop.Signal
	}{
		{"continue", loop.SignalContinue},
		{"review", loop.SignalReview},
		{"done", loop.SignalDone},
		{"blocked", loop.SignalBlocked},
		{"weird", loop.SignalUnknown},
	}
	r := New(Config{})
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			line := []byte(`{"type":"bcc_event","event":"iteration_result","value":"` + tc.value + `","summary":"x"}`)
			got, ok := r.ParseLine(line)
			if !ok {
				t.Fatalf("ok = false")
			}
			if got.Kind != loop.BccEventIterationResult {
				t.Errorf("Kind = %v", got.Kind)
			}
			if got.Signal != tc.want {
				t.Errorf("Signal = %v, want %v", got.Signal, tc.want)
			}
		})
	}
}

func TestParseLine_RejectsForeignLines(t *testing.T) {
	r := New(Config{})
	cases := [][]byte{
		[]byte(`not json`),
		[]byte(`{"type":"system"}`),
		[]byte(`{"type":"assistant","content":"hi"}`),
		[]byte(`{"type":"bcc_event","event":"unknown_kind"}`),
	}
	for i, line := range cases {
		_, ok := r.ParseLine(line)
		if ok {
			t.Errorf("case %d: ok=true, want false (line=%s)", i, line)
		}
	}
}
