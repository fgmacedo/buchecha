package fake

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestRun_ReplaysStepsInOrder(t *testing.T) {
	f := New(
		Step{JSONL: `{"type":"a"}` + "\n", ExitCode: 0},
		Step{JSONL: `{"type":"b"}` + "\n", ExitCode: 7},
	)

	var buf1, buf2 bytes.Buffer
	code1, err1 := f.Run(context.Background(), "first", &buf1)
	if err1 != nil || code1 != 0 || buf1.String() != `{"type":"a"}`+"\n" {
		t.Errorf("step 1 mismatch: code=%d err=%v out=%q", code1, err1, buf1.String())
	}

	code2, err2 := f.Run(context.Background(), "second", &buf2)
	if err2 != nil || code2 != 7 || buf2.String() != `{"type":"b"}`+"\n" {
		t.Errorf("step 2 mismatch: code=%d err=%v out=%q", code2, err2, buf2.String())
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

	if _, err := f.Run(context.Background(), "p", new(bytes.Buffer)); err != nil {
		t.Fatalf("first call: %v", err)
	}
	_, err := f.Run(context.Background(), "p2", new(bytes.Buffer))
	if err == nil {
		t.Errorf("expected error on third call beyond steps")
	}
}

func TestRun_PropagatesScriptedError(t *testing.T) {
	wantErr := errors.New("scripted boom")
	f := New(Step{Err: wantErr})
	_, err := f.Run(context.Background(), "p", new(bytes.Buffer))
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}
