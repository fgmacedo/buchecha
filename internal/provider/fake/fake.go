// Package fake provides a controllable provider.Provider implementation
// for tests. Callers configure a deterministic SpawnResult plus an
// optional error and read back every SpawnRequest the system under test
// emitted; the fake never spawns a subprocess.
package fake

import (
	"context"
	"sync"

	"github.com/fgmacedo/buchecha/internal/provider"
)

// Provider is a controllable provider.Provider used by tests. The zero
// value is valid: Spawn returns a zero SpawnResult and nil error. Set
// fields before the test exercises the system to control behavior.
type Provider struct {
	// ProviderName is returned by Name(). When empty defaults to "fake".
	ProviderName string

	// Result is returned by every Spawn call. Callers can mutate it
	// between calls to drive different paths.
	Result provider.SpawnResult

	// Err, when non-nil, is returned by Spawn after applying Result.
	// Useful to simulate provider errors.
	Err error

	// BlockUntilCtxDone, when true, causes Spawn to block until the
	// supplied context is cancelled. Used to exercise cancellation
	// propagation in DirectorRoles.
	BlockUntilCtxDone bool

	// SpawnFn, when non-nil, fully overrides Spawn. Use it when the
	// declarative knobs above are not enough (e.g. to inspect req and
	// branch on its fields). When set, Result/Err/BlockUntilCtxDone are
	// ignored.
	SpawnFn func(ctx context.Context, req provider.SpawnRequest) (provider.SpawnResult, error)

	mu       sync.Mutex
	requests []provider.SpawnRequest
}

// Compile-time check that *Provider satisfies provider.Provider.
var _ provider.Provider = (*Provider)(nil)

// Name returns the configured ProviderName or "fake" when unset.
func (p *Provider) Name() string {
	if p.ProviderName == "" {
		return "fake"
	}
	return p.ProviderName
}

// Spawn records req for inspection via Requests() and returns the
// configured Result/Err. When BlockUntilCtxDone is set Spawn waits for
// ctx.Done and returns ctx.Err() instead.
func (p *Provider) Spawn(ctx context.Context, req provider.SpawnRequest) (provider.SpawnResult, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()

	if p.SpawnFn != nil {
		return p.SpawnFn(ctx, req)
	}
	if p.BlockUntilCtxDone {
		<-ctx.Done()
		return p.Result, ctx.Err()
	}
	return p.Result, p.Err
}

// Requests returns a copy of every SpawnRequest captured so far. The
// slice is a fresh allocation so the caller may modify it without
// races against later Spawn calls.
func (p *Provider) Requests() []provider.SpawnRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]provider.SpawnRequest, len(p.requests))
	copy(out, p.requests)
	return out
}

// LastRequest returns the last SpawnRequest captured and true, or a
// zero request and false when no calls have been recorded.
func (p *Provider) LastRequest() (provider.SpawnRequest, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return provider.SpawnRequest{}, false
	}
	return p.requests[len(p.requests)-1], true
}
