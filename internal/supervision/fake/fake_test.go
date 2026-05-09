package fake

import (
	"context"
	"testing"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/supervision"
)

func TestPlanner_DelegatesAndErrorsWhenUnset(t *testing.T) {
	p := &Planner{}
	if _, _, err := p.Plan(context.Background(), supervision.PlannerInput{}, nil); err == nil {
		t.Fatal("expected error when PlanFn unset")
	}
	wantPlan := &supervision.Plan{
		Goal: "x", SpecHash: "h",
		Phases: []supervision.Phase{{
			ID: "p1", Title: "t", Intent: "i",
			Tasks: []supervision.Task{{
				ID: "t1", Title: "tt", Intent: "ii",
				Acceptance: []supervision.AcceptanceItem{{ID: "a1", Description: "d", Evidence: supervision.EvidenceTest}},
				Status:     supervision.TaskPending,
			}},
		}},
	}
	p.PlanFn = func(ctx context.Context, in supervision.PlannerInput, _ chan<- agentcontract.AgentEvent) (*supervision.Plan, *supervision.SpawnStats, error) {
		return wantPlan, &supervision.SpawnStats{CostUSD: 0.01}, nil
	}
	got, stats, err := p.Plan(context.Background(), supervision.PlannerInput{}, nil)
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
	if _, err := b.Brief(context.Background(), supervision.BrieferInput{}, nil); err == nil {
		t.Fatal("expected error when BriefFn unset")
	}
	called := false
	b.BriefFn = func(ctx context.Context, in supervision.BrieferInput, _ chan<- agentcontract.AgentEvent) (*supervision.SpawnStats, error) {
		called = true
		return &supervision.SpawnStats{CostUSD: 0.02}, nil
	}
	stats, err := b.Brief(context.Background(), supervision.BrieferInput{PhaseID: "p1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !called || stats == nil || stats.CostUSD != 0.02 {
		t.Fatalf("called=%v stats=%+v", called, stats)
	}
}

func TestReviewer_DelegatesAndErrorsWhenUnset(t *testing.T) {
	r := &Reviewer{}
	if _, err := r.Review(context.Background(), supervision.ReviewerInput{}, nil); err == nil {
		t.Fatal("expected error when ReviewFn unset")
	}
	called := false
	r.ReviewFn = func(ctx context.Context, in supervision.ReviewerInput, _ chan<- agentcontract.AgentEvent) (*supervision.SpawnStats, error) {
		called = true
		return &supervision.SpawnStats{}, nil
	}
	if _, err := r.Review(context.Background(), supervision.ReviewerInput{}, nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("ReviewFn not invoked")
	}
}
