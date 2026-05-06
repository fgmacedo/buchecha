package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/fgmacedo/buchecha/internal/director"
	"github.com/fgmacedo/buchecha/internal/services"
)

// planInput captures the {id} path parameter that addresses the
// session whose persisted plan.json the SPA wants to load.
type planInput struct {
	ID string `path:"id" doc:"Session id (the directory name under .bcc/sessions/)." minLength:"1"`
}

// planOutput returns the bare director.Plan JSON form. The structural
// fields (phases, tasks) are stable after the planner emits; the
// per-task status carried in this payload reflects the moment the
// loop persisted the plan, which is "pending" for every task right
// after planning. Live status changes flow through the events stream.
type planOutput struct {
	Body *director.Plan
}

// registerPlan wires GET /sessions/{id}/plan. The handler delegates
// to SessionService.Plan which reads .bcc/sessions/<id>/plan.json
// and resolves the live alias for dev mode. Unknown sessions return
// 404 session_not_found; sessions whose planner has not emitted yet
// return 404 plan_not_found.
func registerPlan(api huma.API, svc *services.Services, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-session-plan",
		Method:      "GET",
		Path:        "/sessions/{id}/plan",
		Summary:     "Persisted plan for one session",
		Description: "Returns the Director's persisted plan.json for the session: goal, success criteria, phases, and tasks with their structural fields. The status field on each task reflects the planner's emit moment (pending) and is not the live state; consumers tracking running progress merge in updates from the events stream.",
		Tags:        []string{"sessions"},
	}, func(ctx context.Context, in *planInput) (*planOutput, error) {
		if svc == nil {
			return nil, deps.HumaError(services.ErrInternal.WithMessage("plan: services not configured"))
		}
		plan, err := svc.Sessions.Plan(ctx, in.ID)
		if err != nil {
			return nil, deps.HumaError(err)
		}
		return &planOutput{Body: plan}, nil
	})
}
