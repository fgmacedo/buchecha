// Package fake provides scripted Director adapters for tests.
//
// Each fake satisfies one of director's ports (Planner, Briefer,
// Reviewer) and routes calls to a caller-supplied function. Tests use
// these to drive the loop without spawning a real agent subprocess.
// Brief and Review fakes typically invoke the run-wide dag.Handler
// in-process so they emit briefings and per-task outcomes through the
// canonical wire (without an HTTP round-trip).
package fake

import (
	"context"
	"errors"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/supervision"
)

// Compile-time checks that the fakes satisfy the director ports.
var (
	_ supervision.Planner  = (*Planner)(nil)
	_ supervision.Briefer  = (*Briefer)(nil)
	_ supervision.Reviewer = (*Reviewer)(nil)
)

// Planner is a scripted supervision.Planner. Tests set PlanFn; calls to
// Plan delegate to it. When PlanFn is nil, Plan returns an error. The
// fake's PlanFn signature carries the events channel so tests that
// want to assert on streamed AgentEvents can inspect it.
type Planner struct {
	PlanFn func(ctx context.Context, in supervision.PlannerInput, events chan<- agentcontract.AgentEvent) (*supervision.Plan, *supervision.SpawnStats, error)
}

// Plan implements supervision.Planner.
func (p *Planner) Plan(ctx context.Context, in supervision.PlannerInput, events chan<- agentcontract.AgentEvent) (*supervision.Plan, *supervision.SpawnStats, error) {
	if p.PlanFn == nil {
		return nil, nil, errors.New("fake: Planner.PlanFn not set")
	}
	return p.PlanFn(ctx, in, events)
}

// Briefer is a scripted supervision.Briefer.
type Briefer struct {
	BriefFn func(ctx context.Context, in supervision.BrieferInput, events chan<- agentcontract.AgentEvent) (*supervision.SpawnStats, error)
}

// Brief implements supervision.Briefer.
func (b *Briefer) Brief(ctx context.Context, in supervision.BrieferInput, events chan<- agentcontract.AgentEvent) (*supervision.SpawnStats, error) {
	if b.BriefFn == nil {
		return nil, errors.New("fake: Briefer.BriefFn not set")
	}
	return b.BriefFn(ctx, in, events)
}

// Reviewer is a scripted supervision.Reviewer.
type Reviewer struct {
	ReviewFn func(ctx context.Context, in supervision.ReviewerInput, events chan<- agentcontract.AgentEvent) (*supervision.SpawnStats, error)
}

// Review implements supervision.Reviewer.
func (r *Reviewer) Review(ctx context.Context, in supervision.ReviewerInput, events chan<- agentcontract.AgentEvent) (*supervision.SpawnStats, error) {
	if r.ReviewFn == nil {
		return nil, errors.New("fake: Reviewer.ReviewFn not set")
	}
	return r.ReviewFn(ctx, in, events)
}
