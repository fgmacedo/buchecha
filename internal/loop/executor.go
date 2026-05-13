package loop

import (
	"context"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/provider"
)

// ProviderExecutor adapts provider.Provider to loop.Executor. The Request
// field holds a template SpawnRequest pre-populated by the factory (all
// fields except Prompt and Events); Run clones the template, fills in the
// per-call fields, and delegates to Provider.Spawn.
//
// Callers should wrap ProviderExecutor with a deregisteringExecutor when the
// agent_id must be released from the run-wide registry after Run returns.
type ProviderExecutor struct {
	// Provider is the vendor-specific spawn implementation. Required.
	Provider provider.Provider
	// Request is the per-phase template. Prompt and Events are set by Run;
	// all other fields are populated by the factory before the executor is
	// handed to the loop. The struct is copied by value on each Run call so
	// concurrent calls do not share state.
	Request provider.SpawnRequest
}

// Compile-time check that *ProviderExecutor satisfies Executor.
var _ Executor = (*ProviderExecutor)(nil)

// Run satisfies loop.Executor. It clones the template Request, sets Prompt
// and Events for the current call, then calls Provider.Spawn. The returned
// ExecResult carries ExitCode, StderrTail, and SpawnID from the SpawnResult.
// ctx cancellation propagates to Provider.Spawn and from there to the
// subprocess.
func (p *ProviderExecutor) Run(ctx context.Context, prompt string, events chan<- agentcontract.AgentEvent) (ExecResult, error) {
	req := p.Request
	req.Prompt = prompt
	req.Events = events
	res, err := p.Provider.Spawn(ctx, req)
	return ExecResult{
		ExitCode:   res.ExitCode,
		StderrTail: res.StderrTail,
		SpawnID:    res.SpawnID,
	}, err
}
