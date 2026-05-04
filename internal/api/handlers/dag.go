package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	"github.com/fgmacedo/buchecha/internal/services"
)

// dagInput captures the {id} path parameter that addresses the
// session whose DAG fragment the SPA wants to refetch.
type dagInput struct {
	ID string `path:"id" doc:"Session id (the directory name under .bcc/sessions/)." minLength:"1"`
}

// dagOutput streams the pre-marshaled JSON bytes of the DAG state.
// Body []byte is huma's escape hatch for handlers that need full
// control over the serialized payload: huma writes the bytes
// verbatim, skipping the SchemaLinkTransformer and the struct-driven
// marshaler, so dag.State's custom MarshalJSON is the one that
// produces the wire shape.
type dagOutput struct {
	Body []byte
}

// registerDAG wires GET /sessions/{id}/dag. The handler reuses
// SessionService.Snapshot rather than introducing a separate
// service method: the snapshot already deep-copies the live state,
// loads the archived dag.json, and returns the right error for
// unknown ids; we project off the DAG field and marshal it as JSON
// for the response.
func registerDAG(api huma.API, svc *services.Services, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-session-dag",
		Method:      "GET",
		Path:        "/sessions/{id}/dag",
		Summary:     "DAG fragment for one session",
		Description: "Returns only the DAG portion of the session snapshot, suitable for refetching after a seq_gone signal from the events stream without re-downloading the full bootstrap payload. Unknown ids return 404 with envelope code session_not_found.",
		Tags:        []string{"sessions"},
	}, func(ctx context.Context, in *dagInput) (*dagOutput, error) {
		if svc == nil {
			return nil, deps.HumaError(services.ErrInternal.WithMessage("dag: services not configured"))
		}
		snap, err := svc.Sessions.Snapshot(ctx, in.ID)
		if err != nil {
			return nil, deps.HumaError(err)
		}
		body, err := marshalDAG(snap.DAG)
		if err != nil {
			return nil, deps.HumaError(fmt.Errorf("dag: marshal: %w", err))
		}
		return &dagOutput{Body: body}, nil
	})
}

// marshalDAG serializes the dag.State pointer to JSON via its
// existing MarshalJSON method. A nil state surfaces as the JSON
// literal null so clients can disambiguate "no plan recorded yet"
// from an empty DAG.
func marshalDAG(state services.DAGSnapshot) ([]byte, error) {
	if state == nil {
		return []byte("null"), nil
	}
	return json.Marshal(state)
}
