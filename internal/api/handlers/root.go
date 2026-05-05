package handlers

import (
	"context"
	"slices"

	"github.com/danielgtaylor/huma/v2"
)

// rootCatalog mirrors schemas/root.schema.json. The fields surface
// enough metadata for clients introspecting the API at runtime to
// resolve every other V1 entry point without hardcoded assumptions.
type rootCatalog struct {
	APIVersion    string   `json:"api_version" doc:"Wire-level API version segment, e.g. v1." example:"v1"`
	BinaryVersion string   `json:"binary_version" doc:"bcc binary version reported at build time." example:"dev"`
	OpenAPIURL    string   `json:"openapi_url" doc:"Absolute path of the OpenAPI 3.1 document." example:"/api/v1/openapi.json"`
	SchemasURL    string   `json:"schemas_url" doc:"Absolute base path of the schemas directory; append a name to fetch one." example:"/api/v1/schemas"`
	Endpoints     []string `json:"endpoints" doc:"Every V1 endpoint path the server registers, one entry per route, with parameter placeholders preserved."`
}

// rootOutput is the huma response wrapper. Body carries the JSON
// payload; huma derives the response schema from the rootCatalog
// struct tags above.
type rootOutput struct {
	Body rootCatalog
}

// v1Endpoints is the canonical list of paths the read-only V1 surface
// exposes under /api/v1. The order matches the spec's per-task layout
// so a client walking the slice sees discovery first, then the per-
// session resources, and finally the live event stream.
var v1Endpoints = []string{
	"/api/v1",
	"/api/v1/openapi.json",
	"/api/v1/schemas/{name}",
	"/api/v1/sessions",
	"/api/v1/sessions/{id}",
	"/api/v1/sessions/{id}/snapshot",
	"/api/v1/sessions/{id}/dag",
	"/api/v1/sessions/{id}/briefings/{phase}/{attempt}",
	"/api/v1/sessions/{id}/prompts/{role}",
	"/api/v1/sessions/{id}/events",
}

// registerRoot wires the GET /api/v1 root catalog operation. The
// handler is pure: it composes the response from constants supplied
// in deps, performs no I/O, and never returns an error.
func registerRoot(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-api-root",
		Method:      "GET",
		Path:        "/",
		Summary:     "API root catalog",
		Description: "Returns discoverable metadata about the bcc HTTP API: wire-level version, binary version, the absolute OpenAPI document URL, the schemas base URL, and the list of registered V1 endpoints. Tools that introspect the API at runtime use this as their entry point.",
		Tags:        []string{"meta"},
	}, func(_ context.Context, _ *struct{}) (*rootOutput, error) {
		return &rootOutput{Body: rootCatalog{
			APIVersion:    deps.APIVersion,
			BinaryVersion: deps.BinaryVersion,
			OpenAPIURL:    "/api/" + deps.APIVersion + "/openapi.json",
			SchemasURL:    "/api/" + deps.APIVersion + "/schemas",
			Endpoints:     slices.Clone(v1Endpoints),
		}}, nil
	})
}
