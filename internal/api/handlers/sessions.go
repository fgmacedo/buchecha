package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/fgmacedo/buchecha/internal/services"
)

// listSessionsOutput wraps the sessions slice in an object so the
// response stays extensible. A bare array would lock us out of adding
// metadata (cursor, total count) without breaking clients.
type listSessionsOutput struct {
	Body struct {
		Sessions []services.SessionMeta `json:"sessions"`
	}
}

// getSessionInput captures the {id} path parameter.
type getSessionInput struct {
	ID string `path:"id" doc:"Session id (the directory name under .bcc/sessions/)." minLength:"1"`
}

// getSessionOutput returns the bare SessionMeta. Wrapping it here
// would force every client to peel an envelope for a single
// resource; the canonical pattern in REST APIs is the bare object.
type getSessionOutput struct {
	Body services.SessionMeta
}

// getSessionCostInput captures the {id} path parameter for the cost
// endpoint. Distinct from getSessionInput so each handler reads only
// the parameters it needs.
type getSessionCostInput struct {
	ID string `path:"id" doc:"Session id (the directory name under .bcc/sessions/)." minLength:"1"`
}

// getSessionCostOutput returns the bare CostAggregate. The total_tokens
// field is the vendor-neutral 5-bucket TokenUsage; total_usd is the
// provider-reported scalar; by_role groups SpawnFinished costs by
// emitter (planner/briefer/executor/reviewer).
type getSessionCostOutput struct {
	Body services.CostAggregate
}

// registerSessions wires the list and get operations under the
// shared sessions resource. Both share the same SessionService and
// the same error-to-envelope mapping; they live together so the
// reviewer can audit one file per resource.
func registerSessions(api huma.API, svc *services.Services, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "list-sessions",
		Method:      "GET",
		Path:        "/sessions",
		Summary:     "List every session known to the runtime",
		Description: "Returns the live session (when one is active) plus every archived session under .bcc/sessions/, ordered with the most recent first.",
		Tags:        []string{"sessions"},
	}, func(ctx context.Context, _ *struct{}) (*listSessionsOutput, error) {
		if svc == nil {
			return nil, deps.HumaError(services.ErrInternal.WithMessage("sessions: services not configured"))
		}
		got, err := svc.Sessions.List(ctx)
		if err != nil {
			return nil, deps.HumaError(err)
		}
		out := &listSessionsOutput{}
		out.Body.Sessions = got
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-session",
		Method:      "GET",
		Path:        "/sessions/{id}",
		Summary:     "Get one session's metadata by id",
		Description: "Returns the SessionMeta for the live session (when its id matches) or the archived session under .bcc/sessions/<id>/. Unknown ids return 404 with envelope code session_not_found.",
		Tags:        []string{"sessions"},
	}, func(ctx context.Context, in *getSessionInput) (*getSessionOutput, error) {
		if svc == nil {
			return nil, deps.HumaError(services.ErrInternal.WithMessage("sessions: services not configured"))
		}
		meta, err := svc.Sessions.Get(ctx, in.ID)
		if err != nil {
			return nil, deps.HumaError(err)
		}
		return &getSessionOutput{Body: meta}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-session-cost",
		Method:      "GET",
		Path:        "/sessions/{id}/cost",
		Summary:     "Get the per-session cost and token aggregate",
		Description: "Returns the cumulative spawn cost and 5-bucket TokenUsage for the session, grouped by role. Live sessions read from the in-memory accumulator; archived sessions read .bcc/sessions/<id>/cost.json (rebuilt from events.ndjson when missing).",
		Tags:        []string{"sessions"},
	}, func(ctx context.Context, in *getSessionCostInput) (*getSessionCostOutput, error) {
		if svc == nil {
			return nil, deps.HumaError(services.ErrInternal.WithMessage("sessions: services not configured"))
		}
		agg, err := svc.Sessions.Cost(ctx, in.ID)
		if err != nil {
			return nil, deps.HumaError(err)
		}
		return &getSessionCostOutput{Body: agg}, nil
	})
}
