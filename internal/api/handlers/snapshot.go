package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/fgmacedo/buchecha/internal/services"
)

// snapshotInput captures the {id} path parameter that addresses the
// session whose bootstrap state the SPA wants to load.
type snapshotInput struct {
	ID string `path:"id" doc:"Session id (the directory name under .bcc/sessions/)." minLength:"1"`
}

// snapshotOutput returns the bare services.Snapshot JSON form. The
// dag field is allowed to be null when no plan has been recorded for
// the session yet, matching schemas/snapshot.schema.json.
type snapshotOutput struct {
	Body services.Snapshot
}

// registerSnapshot wires GET /sessions/{id}/snapshot. The handler
// delegates to SessionService.Snapshot which already covers both the
// live and the archived path; consumers do not need to know which
// branch handled the request.
func registerSnapshot(api huma.API, svc *services.Services, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-session-snapshot",
		Method:      "GET",
		Path:        "/sessions/{id}/snapshot",
		Summary:     "Bootstrap snapshot for one session",
		Description: "Returns enough state for an SPA to render a session view in one request: SessionMeta, the DAG state (deep-copied from the live handler or read from .bcc/sessions/<id>/dag.json for archived runs), and the most recent PhaseBriefed reference when known. Unknown ids return 404 with envelope code session_not_found.",
		Tags:        []string{"sessions"},
	}, func(ctx context.Context, in *snapshotInput) (*snapshotOutput, error) {
		if svc == nil {
			return nil, deps.HumaError(services.ErrInternal.WithMessage("snapshot: services not configured"))
		}
		snap, err := svc.Sessions.Snapshot(ctx, in.ID)
		if err != nil {
			return nil, deps.HumaError(err)
		}
		return &snapshotOutput{Body: snap}, nil
	})
}
