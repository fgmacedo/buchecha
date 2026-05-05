package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/fgmacedo/buchecha/internal/services"
)

// briefingsInput captures the three path parameters that address one
// rendered briefing: session id, phase id, and 1-based attempt index.
type briefingsInput struct {
	ID      string `path:"id" doc:"Session id (the directory name under .bcc/sessions/)." minLength:"1"`
	Phase   string `path:"phase" doc:"Phase id matching the plan emitted at session start." minLength:"1"`
	Attempt int    `path:"attempt" doc:"1-based attempt index for the requested phase." minimum:"1"`
}

// briefingsOutput carries the briefing markdown body verbatim with
// the matching content type. Body []byte is huma's escape hatch for
// non-JSON payloads; the ContentType header field overrides the
// negotiated default so clients receive text/markdown directly.
type briefingsOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

// registerBriefings wires GET /sessions/{id}/briefings/{phase}/{attempt}.
// On success it streams Briefing.Markdown as text/markdown; on miss
// it lifts ErrPhaseNotFound or ErrAttemptNotFound into the canonical
// envelope via deps.HumaError. ErrSessionNotFound also lands here
// because the underlying service routes through the same path lookup.
func registerBriefings(api huma.API, svc *services.Services, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-session-briefing",
		Method:      "GET",
		Path:        "/sessions/{id}/briefings/{phase}/{attempt}",
		Summary:     "Rendered briefing markdown for one (phase, attempt)",
		Description: "Returns the rendered briefing markdown the Briefer materialized for the requested attempt. Misses map to 404 with envelope code phase_not_found (no briefing recorded against the phase) or attempt_not_found (briefings exist but not at the requested attempt). Unknown sessions return session_not_found.",
		Tags:        []string{"briefings"},
	}, func(ctx context.Context, in *briefingsInput) (*briefingsOutput, error) {
		if svc == nil {
			return nil, deps.HumaError(services.ErrInternal.WithMessage("briefings: services not configured"))
		}
		got, err := svc.Briefings.Get(ctx, in.ID, in.Phase, in.Attempt)
		if err != nil {
			return nil, deps.HumaError(err)
		}
		return &briefingsOutput{
			ContentType: "text/markdown; charset=utf-8",
			Body:        []byte(got.Markdown),
		}, nil
	})
}
