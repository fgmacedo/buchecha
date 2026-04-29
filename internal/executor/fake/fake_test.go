package fake

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fgmacedo/buchecha/internal/loop"
)

func TestRun_ReplaysStepsInOrder(t *testing.T) {
	f := New(
		Step{Events: []loop.AgentEvent{{Kind: loop.KindInit}}, ExitCode: 0},
		Step{Events: []loop.AgentEvent{{Kind: loop.KindResultSummary}}, ExitCode: 7},
	)

	ch1 := make(chan loop.AgentEvent, 4)
	res1, err1 := f.Run(context.Background(), "first", ch1)
	close(ch1)
	if err1 != nil || res1.ExitCode != 0 {
		t.Errorf("step 1: code=%d err=%v", res1.ExitCode, err1)
	}
	if got := drain(ch1); len(got) != 1 || got[0].Kind != loop.KindInit {
		t.Errorf("step 1 events: %v", got)
	}

	ch2 := make(chan loop.AgentEvent, 4)
	res2, err2 := f.Run(context.Background(), "second", ch2)
	close(ch2)
	if err2 != nil || res2.ExitCode != 7 {
		t.Errorf("step 2: code=%d err=%v", res2.ExitCode, err2)
	}
	if got := drain(ch2); len(got) != 1 || got[0].Kind != loop.KindResultSummary {
		t.Errorf("step 2 events: %v", got)
	}

	if got := f.CallCount(); got != 2 {
		t.Errorf("CallCount = %d, want 2", got)
	}
	if got := f.Prompts(); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Errorf("Prompts = %v", got)
	}
}

func TestRun_OutOfStepsReturnsError(t *testing.T) {
	f := New(Step{ExitCode: 0})

	ch := make(chan loop.AgentEvent, 4)
	if _, err := f.Run(context.Background(), "p", ch); err != nil {
		t.Fatalf("first call: %v", err)
	}
	_, err := f.Run(context.Background(), "p2", ch)
	if err == nil {
		t.Errorf("expected error on call beyond steps")
	}
}

func TestRun_PropagatesScriptedError(t *testing.T) {
	wantErr := errors.New("scripted boom")
	f := New(Step{Err: wantErr})
	ch := make(chan loop.AgentEvent, 4)
	_, err := f.Run(context.Background(), "p", ch)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestRun_RawLogWrittenAtBCCJSONLPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.jsonl")
	t.Setenv("BCC_JSONL_PATH", path)

	f := New(Step{RawLog: `{"type":"hello"}` + "\n", ExitCode: 0})
	ch := make(chan loop.AgentEvent, 4)
	res, err := f.Run(context.Background(), "p", ch)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.LogPath != path {
		t.Errorf("LogPath = %q, want %q", res.LogPath, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if string(b) != `{"type":"hello"}`+"\n" {
		t.Errorf("log content = %q", string(b))
	}
}

func drain(ch <-chan loop.AgentEvent) []loop.AgentEvent {
	var out []loop.AgentEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}
