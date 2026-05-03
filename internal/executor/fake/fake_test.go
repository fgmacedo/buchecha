package fake

import (
	"context"
	"errors"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"testing"
)

func TestRun_ReplaysStepsInOrder(t *testing.T) {
	f := New(
		Step{Events: []agentcontract.AgentEvent{{Kind: agentcontract.KindInit}}, ExitCode: 0},
		Step{Events: []agentcontract.AgentEvent{{Kind: agentcontract.KindResultSummary}}, ExitCode: 7},
	)

	ch1 := make(chan agentcontract.AgentEvent, 4)
	res1, err1 := f.Run(context.Background(), "first", ch1)
	close(ch1)
	if err1 != nil || res1.ExitCode != 0 {
		t.Errorf("step 1: code=%d err=%v", res1.ExitCode, err1)
	}
	if got := drain(ch1); len(got) != 1 || got[0].Kind != agentcontract.KindInit {
		t.Errorf("step 1 events: %v", got)
	}

	ch2 := make(chan agentcontract.AgentEvent, 4)
	res2, err2 := f.Run(context.Background(), "second", ch2)
	close(ch2)
	if err2 != nil || res2.ExitCode != 7 {
		t.Errorf("step 2: code=%d err=%v", res2.ExitCode, err2)
	}
	if got := drain(ch2); len(got) != 1 || got[0].Kind != agentcontract.KindResultSummary {
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

	ch := make(chan agentcontract.AgentEvent, 4)
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
	ch := make(chan agentcontract.AgentEvent, 4)
	_, err := f.Run(context.Background(), "p", ch)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func drain(ch <-chan agentcontract.AgentEvent) []agentcontract.AgentEvent {
	var out []agentcontract.AgentEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}
