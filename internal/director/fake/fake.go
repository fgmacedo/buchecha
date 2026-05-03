// Package fake provides scripted Director adapters for tests.
//
// Each fake satisfies one of director's ports (Planner, Briefer,
// Reviewer) and routes calls to a caller-supplied function. Tests use
// these to drive the loop without spawning claude. Brief and Review
// fakes typically invoke the run-wide dag.Handler in-process so they
// emit briefings and per-task outcomes through the canonical wire
// (without an HTTP round-trip).
package fake

import (
	"context"
	"errors"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

// Compile-time checks that the fakes satisfy the director ports.
var (
	_ director.Planner  = (*Planner)(nil)
	_ director.Briefer  = (*Briefer)(nil)
	_ director.Reviewer = (*Reviewer)(nil)
)

// Planner is a scripted director.Planner. Tests set PlanFn; calls to
// Plan delegate to it. When PlanFn is nil, Plan returns an error. The
// fake's PlanFn signature carries the events channel so tests that
// want to assert on streamed AgentEvents can inspect it.
type Planner struct {
	PlanFn func(ctx context.Context, in director.PlannerInput, events chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error)
}

// Plan implements director.Planner.
func (p *Planner) Plan(ctx context.Context, in director.PlannerInput, events chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
	if p.PlanFn == nil {
		return nil, nil, errors.New("fake: Planner.PlanFn not set")
	}
	return p.PlanFn(ctx, in, events)
}

// Briefer is a scripted director.Briefer.
type Briefer struct {
	BriefFn func(ctx context.Context, in director.BrieferInput, events chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error)
}

// Brief implements director.Briefer.
func (b *Briefer) Brief(ctx context.Context, in director.BrieferInput, events chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
	if b.BriefFn == nil {
		return nil, errors.New("fake: Briefer.BriefFn not set")
	}
	return b.BriefFn(ctx, in, events)
}

// Reviewer is a scripted director.Reviewer.
type Reviewer struct {
	ReviewFn func(ctx context.Context, in director.ReviewerInput, events chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error)
}

// Review implements director.Reviewer.
func (r *Reviewer) Review(ctx context.Context, in director.ReviewerInput, events chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
	if r.ReviewFn == nil {
		return nil, errors.New("fake: Reviewer.ReviewFn not set")
	}
	return r.ReviewFn(ctx, in, events)
}
