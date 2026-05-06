package webui

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// DefaultDevUpstream is the Vite dev server bcc expects to be running
// when --webui-dev is set with no override. Loopback-only by default:
// the dev proxy is a contributor convenience, not a generic proxy. The
// CLI exposes --webui-upstream / --webui-dev-upstream so power users
// can point bcc at a Vite running on another port (dev container, SSH
// tunnel, alternate process).
const DefaultDevUpstream = "http://127.0.0.1:5173"

// apiV1Prefix is the request-path prefix the api package owns on the
// shared listener. NewDev refuses to proxy paths under this prefix
// even though chi normally routes them itself; the defensive check
// keeps the proxy honest if the mount layout ever shifts.
const apiV1Prefix = "/api/v1/"

// NewDev returns an http.Handler that reverse-proxies every non-API
// request to the configured Vite dev server upstream. An empty
// upstream falls back to DefaultDevUpstream. Requests under /api/v1/
// short-circuit with 404 so a misconfigured mount cannot accidentally
// tunnel API calls through the dev server.
//
// The proxy is mounted at / on the api.Server alongside /api/v1/. In
// the production mount layout chi routes /api/v1/* to the API router
// before the WebUI handler ever sees the request, so the in-handler
// guard is defence in depth, not the primary boundary.
//
// The handler preserves the original method, headers, and request
// body. Director rewrites the upstream Host so Vite's request log
// reads naturally and so any host-aware Vite middleware (HMR's WS
// upgrade, in particular) sees a stable origin.
//
// NewDev returns an error when upstream cannot be parsed; callers
// surface it to the user instead of panicking.
func NewDev(upstream string) (http.Handler, error) {
	if upstream == "" {
		upstream = DefaultDevUpstream
	}
	target, err := url.Parse(upstream)
	if err != nil {
		return nil, fmt.Errorf("webui: parse dev upstream %q: %w", upstream, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("webui: dev upstream %q must include scheme and host", upstream)
	}
	return newDevProxy(target), nil
}

// newDevProxy is the constructor used by both NewDev (production
// behaviour) and the tests, which point it at an httptest upstream.
// Splitting the constructor keeps the public surface a no-arg
// function while still allowing the URL to be injected in tests.
func newDevProxy(target *url.URL) http.Handler {
	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
	}
	return &devProxyHandler{rp: rp}
}

// devProxyHandler short-circuits /api/v1/ paths to a 404 and forwards
// everything else through the inner ReverseProxy. The 404 envelope
// matches the production WebUI handler so consumers see a uniform
// shape for "the webui served this miss".
type devProxyHandler struct {
	rp *httputil.ReverseProxy
}

func (h *devProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, apiV1Prefix) {
		notFound(w)
		return
	}
	h.rp.ServeHTTP(w, r)
}
