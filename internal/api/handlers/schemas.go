package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fgmacedo/buchecha/internal/services"
)

// registerSchemas mounts the GET /schemas/{name} handler on the
// apiRouter (full path /api/v1/schemas/{name}). It does not flow
// through huma: we serve the schema bytes verbatim with a
// schema-friendly Content-Type and the canonical not_implemented
// envelope on miss, neither of which fits cleanly into huma's
// struct-driven response model.
//
// The {name} parameter is the bare schema filename, e.g.
// "session.schema.json". Unknown names map to 501 with envelope
// code not_implemented because the surface is open-ended: a future
// release may publish an additional schema without breaking the
// closed-enum 404 codes.
func registerSchemas(router chi.Router, deps Deps) {
	router.Get("/schemas/{name}", schemasHandler(deps))
}

func schemasHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if name == "" {
			deps.WriteError(w, r, services.ErrInvalidRequest.WithMessage("schemas: empty name"))
			return
		}
		body, err := deps.LoadSchema(name)
		if err != nil {
			if errors.Is(err, services.ErrNotImplemented) {
				deps.WriteError(w, r, services.ErrNotImplemented.WithDetails(map[string]any{"schema": name}))
				return
			}
			deps.WriteError(w, r, err)
			return
		}
		h := w.Header()
		// application/schema+json is the IANA-registered media type
		// for JSON Schema documents (RFC 8927). Browsers fall back
		// to JSON behavior, and validators that key off the suffix
		// continue to work.
		h.Set("Content-Type", "application/schema+json")
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}
