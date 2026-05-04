// Package handlers hosts the read-only V1 HTTP API operations
// registered against the chi + huma server set up by internal/api.
// Each file in the package owns one resource (root catalog, openapi
// passthrough, schemas, sessions, snapshot, dag fragment, briefings,
// prompts, events). Register wires them all into a Server in one
// call so the composition root and the OpenAPI generator share the
// same surface.
package handlers

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/fgmacedo/buchecha/internal/services"
)

// Deps carries the values handlers need that are owned by the api
// package (and therefore cannot be imported here without breaking the
// layer rule). The api package builds Deps from its own constants and
// embedded assets and hands the bag to Register so handlers stay
// independent of api's concrete Server shape.
type Deps struct {
	// APIVersion is the wire-level prefix segment, e.g. "v1". Surfaced
	// in the root catalog response.
	APIVersion string
	// BinaryVersion is the bcc binary version. Surfaced in the root
	// catalog response.
	BinaryVersion string
	// OpenAPIJSON is the embedded OpenAPI 3.1 document the openapi.json
	// passthrough handler serves verbatim.
	OpenAPIJSON []byte
	// LoadSchema resolves a schema filename against the embedded
	// SchemaFS in the api package; the schemas handler delegates to
	// it instead of importing the embedded fs directly.
	LoadSchema func(name string) ([]byte, error)
	// WriteError serializes an error to the canonical envelope and
	// the right HTTP status. Provided by the api package so handlers
	// stay out of the circular import that would result from
	// reaching back into api directly.
	WriteError func(w http.ResponseWriter, r *http.Request, err error)
	// HumaError lifts a services error into a huma.StatusError so
	// huma operation handlers can return one and let huma render the
	// canonical envelope with the mapped HTTP status. Non-huma
	// handlers (openapi, schemas, sse) use WriteError instead.
	HumaError func(err error) huma.StatusError
}

// Register installs every V1 read-only operation against api and,
// where the operation cannot flow through huma (the SSE stream),
// against router. svc may be nil for the OpenAPI generator path:
// handlers that consume it perform a nil check at request time and
// surface a 500 envelope rather than panicking. Calling Register
// twice on the same api panics from huma's underlying duplicate-route
// detection; callers register exactly once per Server lifecycle.
func Register(api huma.API, router chi.Router, svc *services.Services, deps Deps) {
	registerRoot(api, deps)
	registerOpenAPI(router, deps.OpenAPIJSON)
	registerSchemas(router, deps)
	registerSessions(api, svc, deps)
	registerSnapshot(api, svc, deps)
}
