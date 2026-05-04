package webui

import (
	"bytes"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// distRoot is the prefix every embedded path carries inside BundleFS.
// The handler trims request paths against this root so the SPA's
// dist/ layout is the source of truth for served content.
const distRoot = "web/dist"

// indexPath is the embedded path of the SPA shell served at "/".
const indexPath = distRoot + "/index.html"

// assetsPrefix is the request-path prefix under which the SPA's
// hashed static assets live. Everything below it is served with the
// long-lived immutable cache policy because the build pipeline
// guarantees content-hashed filenames.
const assetsPrefix = "/assets/"

// New returns an http.Handler that serves the embedded SPA bundle
// rooted at web/dist/. It is mounted at "/" by the api server via
// Mounts.WebUI. The handler is self-contained: it routes against
// http.ServeMux semantics without any third-party router so the webui
// package depends on stdlib only.
//
// Routes:
//
//   - GET  /            -> serves web/dist/index.html
//   - GET  /assets/...  -> serves web/dist/assets/...
//   - GET  /healthz     -> 200 OK liveness probe
//   - any other path    -> 404 plain text
//
// HEAD is accepted everywhere GET is, courtesy of http.ServeContent
// for static paths and an explicit allowance for /healthz. Other
// methods produce 405 Method Not Allowed with Allow: GET, HEAD.
//
// SPA history-mode fallback (rewriting unknown paths to index.html)
// is intentionally not implemented here. The P7 SPA introduces
// client-side routes such as /archived/{id} that, on direct load or
// reload, need the server to return index.html so React Router can
// take over.
//
// TODO(P7): when the SPA gains client-side routes, add a history-mode
// fallback that serves index.html for unknown GETs whose Accept
// header includes text/html, while still returning 404 for asset
// misses and unknown subtrees outside the SPA.
func New() http.Handler {
	return &handler{}
}

// handler implements http.Handler for the embedded SPA bundle. It is
// stateless; one instance is shared across all requests.
type handler struct{}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch {
	case r.URL.Path == "/healthz":
		serveHealthz(w, r)
	case r.URL.Path == "/" || r.URL.Path == "/index.html":
		serveIndex(w, r)
	case strings.HasPrefix(r.URL.Path, assetsPrefix):
		serveAsset(w, r)
	default:
		notFound(w)
	}
}

// serveHealthz answers liveness probes without consulting the embed
// FS. The body is the literal token "ok" so reverse proxies can
// pattern-match without parsing.
func serveHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte("ok"))
}

// serveIndex serves the embedded SPA shell with no-cache so reloads
// always fetch the freshest hashed asset references.
func serveIndex(w http.ResponseWriter, r *http.Request) {
	body, err := fs.ReadFile(BundleFS, indexPath)
	if err != nil {
		// The stub index.html is committed and required for
		// go build to succeed; reaching this branch means the
		// build pipeline shipped without a bundle.
		http.Error(w, "spa bundle missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(body))
}

// serveAsset serves a single file from web/dist/assets/. The
// Content-Type is derived from the filename extension via
// mime.TypeByExtension and falls back to application/octet-stream
// when the extension is unrecognised. Long-lived immutable caching
// is safe because the SPA build pipeline emits content-hashed
// filenames.
func serveAsset(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/")
	// Defence in depth: reject traversal attempts and absolute
	// re-paths. fs.ReadFile already rejects ".." but we surface a
	// 404 (not 500) for these and avoid even probing the FS.
	if !fs.ValidPath(rel) {
		notFound(w)
		return
	}
	embedPath := distRoot + "/" + rel
	body, err := fs.ReadFile(BundleFS, embedPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			notFound(w)
			return
		}
		http.Error(w, "asset read failed", http.StatusInternalServerError)
		return
	}

	ct := mime.TypeByExtension(path.Ext(embedPath))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, path.Base(embedPath), time.Time{}, bytes.NewReader(body))
}

// notFound writes a uniform 404 envelope. The body is plain text so
// the SPA's client-side fallback (added in P7) can distinguish a
// real miss from a same-origin API failure.
func notFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("not found"))
}
