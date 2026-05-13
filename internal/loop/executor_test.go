package loop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/provider"
	"github.com/fgmacedo/buchecha/internal/provider/fake"
)

func TestProviderExecutor_ExitCode(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		wantCode int
	}{
		{name: "zero on success", exitCode: 0, wantCode: 0},
		{name: "non-zero on failure", exitCode: 1, wantCode: 1},
		{name: "high exit code", exitCode: 42, wantCode: 42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := &fake.Provider{
				Result: provider.SpawnResult{ExitCode: tt.exitCode},
			}
			exec := &loop.ProviderExecutor{
				Provider: fp,
				Request:  provider.SpawnRequest{},
			}
			events := make(chan agentcontract.AgentEvent, 1)
			res, err := exec.Run(t.Context(), "some prompt", events)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.ExitCode != tt.wantCode {
				t.Errorf("ExitCode = %d, want %d", res.ExitCode, tt.wantCode)
			}
		})
	}
}

func TestProviderExecutor_StderrTailForwarding(t *testing.T) {
	wantTail := "error: something went wrong\nstack trace follows"
	fp := &fake.Provider{
		Result: provider.SpawnResult{
			ExitCode:   1,
			StderrTail: wantTail,
		},
	}
	exec := &loop.ProviderExecutor{
		Provider: fp,
		Request:  provider.SpawnRequest{},
	}
	events := make(chan agentcontract.AgentEvent, 1)
	res, _ := exec.Run(t.Context(), "prompt", events)
	if res.StderrTail != wantTail {
		t.Errorf("StderrTail = %q, want %q", res.StderrTail, wantTail)
	}
}

func TestProviderExecutor_SpawnIDForwarding(t *testing.T) {
	wantSpawnID := "abc-123-spawn"
	fp := &fake.Provider{
		Result: provider.SpawnResult{SpawnID: wantSpawnID},
	}
	exec := &loop.ProviderExecutor{
		Provider: fp,
		Request:  provider.SpawnRequest{},
	}
	events := make(chan agentcontract.AgentEvent, 1)
	res, err := exec.Run(t.Context(), "prompt", events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SpawnID != wantSpawnID {
		t.Errorf("SpawnID = %q, want %q", res.SpawnID, wantSpawnID)
	}
}

func TestProviderExecutor_PromptSetOnRequest(t *testing.T) {
	wantPrompt := "do the thing"
	fp := &fake.Provider{}
	exec := &loop.ProviderExecutor{
		Provider: fp,
		Request:  provider.SpawnRequest{Model: "claude-3-5-sonnet"},
	}
	events := make(chan agentcontract.AgentEvent, 1)
	_, _ = exec.Run(t.Context(), wantPrompt, events)

	req, ok := fp.LastRequest()
	if !ok {
		t.Fatal("no request recorded")
	}
	if req.Prompt != wantPrompt {
		t.Errorf("Prompt = %q, want %q", req.Prompt, wantPrompt)
	}
	// template field preserved
	if req.Model != "claude-3-5-sonnet" {
		t.Errorf("Model = %q, want claude-3-5-sonnet", req.Model)
	}
}

func TestProviderExecutor_ErrorPropagation(t *testing.T) {
	sentinel := errors.New("spawn failed")
	fp := &fake.Provider{Err: sentinel}
	exec := &loop.ProviderExecutor{Provider: fp, Request: provider.SpawnRequest{}}
	events := make(chan agentcontract.AgentEvent, 1)
	_, err := exec.Run(t.Context(), "prompt", events)
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want sentinel error", err)
	}
}

func TestProviderExecutor_CtxCancellationReachesProvider(t *testing.T) {
	fp := &fake.Provider{BlockUntilCtxDone: true}
	exec := &loop.ProviderExecutor{Provider: fp, Request: provider.SpawnRequest{}}

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan agentcontract.AgentEvent, 1)

	done := make(chan error, 1)
	go func() {
		_, err := exec.Run(ctx, "prompt", events)
		done <- err
	}()

	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestProviderExecutor_TemplateNotMutated(t *testing.T) {
	// Verify that consecutive Run calls each see their own prompt (template
	// is copied by value, not shared).
	fp := &fake.Provider{}
	exec := &loop.ProviderExecutor{
		Provider: fp,
		Request:  provider.SpawnRequest{SystemPrompt: "system"},
	}
	events := make(chan agentcontract.AgentEvent, 1)
	_, _ = exec.Run(t.Context(), "first", events)
	_, _ = exec.Run(t.Context(), "second", events)

	reqs := fp.Requests()
	if len(reqs) != 2 {
		t.Fatalf("got %d requests, want 2", len(reqs))
	}
	if reqs[0].Prompt != "first" {
		t.Errorf("first call Prompt = %q, want %q", reqs[0].Prompt, "first")
	}
	if reqs[1].Prompt != "second" {
		t.Errorf("second call Prompt = %q, want %q", reqs[1].Prompt, "second")
	}
	// template field preserved in both
	if reqs[0].SystemPrompt != "system" || reqs[1].SystemPrompt != "system" {
		t.Error("SystemPrompt not preserved from template")
	}
}
