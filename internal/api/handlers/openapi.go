package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// registerOpenAPI mounts the verbatim passthrough of the embedded
// OpenAPI 3.1 document under /openapi.json relative to the apiRouter
// (full path /api/v1/openapi.json). This handler does not flow
// through huma: huma's runtime serializer would reformat the document
// and inject SchemaLinkTransformer fields, so consumers comparing
// bytes against the regenerated openapi.json on disk would see drift.
//
// The asset is treated as immutable per build: the embedded slice
// only changes when the binary is rebuilt, so an aggressive cache
// header is correct.
func registerOpenAPI(router chi.Router, body []byte) {
	router.Get("/openapi.json", openAPIHandler(body))
}

// openAPIHandler returns the http.HandlerFunc that streams the
// embedded body. Bytes are served as-is with Content-Type
// application/json plus immutable cache headers; clients keying off
// Content-Length know the exact byte count up front.
func openAPIHandler(body []byte) http.HandlerFunc {
	length := strconv.Itoa(len(body))
	return func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "application/json")
		h.Set("Content-Length", length)
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}
