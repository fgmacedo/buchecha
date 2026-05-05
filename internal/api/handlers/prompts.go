package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/fgmacedo/buchecha/internal/services"
)

// promptsInput captures the addressing pair for one rendered prompt.
type promptsInput struct {
	ID   string `path:"id" doc:"Session id (the directory name under .bcc/sessions/)." minLength:"1"`
	Role string `path:"role" doc:"One of planner, briefer, executor, reviewer." minLength:"1"`
}

// promptsOutput streams the prompt markdown verbatim with the
// matching content type, mirroring briefingsOutput. The shape and
// content-type override pattern is identical because the Prompts and
// Briefings tabs in the SPA render through the same react-markdown
// pipeline.
type promptsOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

// spawnPromptsInput captures the addressing pair for one spawn prompt.
type spawnPromptsInput struct {
	ID      string `path:"id" doc:"Session id (the directory name under .bcc/sessions/)." minLength:"1"`
	SpawnID string `path:"spawnId" doc:"Spawn id (ULID-shaped, 16-32 lowercase hex chars)." minLength:"1"`
}

// registerPrompts wires GET /sessions/{id}/prompts/{role} and
// GET /sessions/{id}/spawns/{spawnId}/prompt. Invalid roles and spawn
// IDs map to invalid_request before any filesystem call; unknown
// sessions and missing prompt files map to session_not_found and
// role_not_found respectively.
func registerPrompts(api huma.API, svc *services.Services, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-session-prompt",
		Method:      "GET",
		Path:        "/sessions/{id}/prompts/{role}",
		Summary:     "Rendered system prompt for one role",
		Description: "Returns the rendered system prompt the run boot materialized for the requested role under .bcc/sessions/<id>/prompts/<role>.md. Roles outside the closed set planner|briefer|executor|reviewer return 400 with envelope code invalid_request; unknown sessions return 404 with session_not_found; missing prompt files return 404 with role_not_found.",
		Tags:        []string{"prompts"},
	}, func(ctx context.Context, in *promptsInput) (*promptsOutput, error) {
		if svc == nil {
			return nil, deps.HumaError(services.ErrInternal.WithMessage("prompts: services not configured"))
		}
		got, err := svc.Prompts.Get(ctx, in.ID, in.Role)
		if err != nil {
			return nil, deps.HumaError(err)
		}
		return &promptsOutput{
			ContentType: "text/markdown; charset=utf-8",
			Body:        []byte(got.Markdown),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-session-spawn-prompt",
		Method:      "GET",
		Path:        "/sessions/{id}/spawns/{spawnId}/prompt",
		Summary:     "Resolved prompt for one spawn",
		Description: "Returns the exact markdown prompt the specified spawn received before execution, persisted under .bcc/sessions/<id>/spawns/<spawnId>.md. Spawn IDs are ULID-shaped (16-32 lowercase hex chars). Malformed spawn IDs return 400 with envelope code invalid_request; unknown sessions return 404 with session_not_found; missing spawn files return 404 with role_not_found.",
		Tags:        []string{"prompts"},
	}, func(ctx context.Context, in *spawnPromptsInput) (*promptsOutput, error) {
		if svc == nil {
			return nil, deps.HumaError(services.ErrInternal.WithMessage("prompts: services not configured"))
		}
		got, err := svc.Prompts.GetSpawn(ctx, in.ID, in.SpawnID)
		if err != nil {
			return nil, deps.HumaError(err)
		}
		return &promptsOutput{
			ContentType: "text/markdown; charset=utf-8",
			Body:        []byte(got.Markdown),
		}, nil
	})
}
