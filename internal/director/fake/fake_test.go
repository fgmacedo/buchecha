package fake

import (
	"context"
	"testing"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
)

func TestPlanner_DelegatesAndErrorsWhenUnset(t *testing.T) {
	p := &Planner{}
	if _, _, err := p.Plan(context.Background(), director.PlannerInput{}, nil); err == nil {
		t.Fatal("expected error when PlanFn unset")
	}
	wantPlan := &director.Plan{
		Goal: "x", SpecHash: "h",
		Phases: []director.Phase{{
			ID: "p1", Title: "t", Intent: "i",
			Tasks: []director.Task{{
				ID: "t1", Title: "tt", Intent: "ii",
				Acceptance: []director.AcceptanceItem{{ID: "a1", Description: "d", Evidence: director.EvidenceTest}},
				Status:     director.TaskPending,
			}},
		}},
	}
	p.PlanFn = func(ctx context.Context, in director.PlannerInput, _ chan<- agentcontract.AgentEvent) (*director.Plan, *director.DirectorCallStats, error) {
		return wantPlan, &director.DirectorCallStats{CostUSD: 0.01}, nil
	}
	got, stats, err := p.Plan(context.Background(), director.PlannerInput{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantPlan {
		t.Fatalf("got %+v, want %+v", got, wantPlan)
	}
	if stats == nil || stats.CostUSD != 0.01 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestBriefer_DelegatesAndErrorsWhenUnset(t *testing.T) {
	b := &Briefer{}
	if _, err := b.Brief(context.Background(), director.BrieferInput{}, nil); err == nil {
		t.Fatal("expected error when BriefFn unset")
	}
	called := false
	b.BriefFn = func(ctx context.Context, in director.BrieferInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
		called = true
		return &director.DirectorCallStats{CostUSD: 0.02}, nil
	}
	stats, err := b.Brief(context.Background(), director.BrieferInput{PhaseID: "p1", Attempt: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !called || stats == nil || stats.CostUSD != 0.02 {
		t.Fatalf("called=%v stats=%+v", called, stats)
	}
}

func TestReviewer_DelegatesAndErrorsWhenUnset(t *testing.T) {
	r := &Reviewer{}
	if _, err := r.Review(context.Background(), director.ReviewerInput{}, nil); err == nil {
		t.Fatal("expected error when ReviewFn unset")
	}
	called := false
	r.ReviewFn = func(ctx context.Context, in director.ReviewerInput, _ chan<- agentcontract.AgentEvent) (*director.DirectorCallStats, error) {
		called = true
		return &director.DirectorCallStats{}, nil
	}
	if _, err := r.Review(context.Background(), director.ReviewerInput{}, nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("ReviewFn not invoked")
	}
}
