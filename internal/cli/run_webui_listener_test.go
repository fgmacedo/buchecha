package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/config"
	"github.com/fgmacedo/buchecha/internal/webui"
)

// TestStartRunListener_WithRealWebUIHandler boots the run-wide listener
// against the production webui.New() handler and walks the three
// surfaces the composition root cares about: GET /healthz returns 200,
// GET / returns the embedded index.html bytes, GET /api/v1/ stays
// behind the API auth (chi routes it through the api server, not the
// webui mount).
func TestStartRunListener_WithRealWebUIHandler(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	listener, err := startRunListener(ctx, nil, nil, webui.New(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startRunListener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Stop() })

	// /healthz returns 200 OK from the webui handler.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+listener.addr+"/healthz", nil)
	req.Header.Set("Authorization", "Bearer "+listener.sessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("healthz do: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d, body = %s", resp.StatusCode, body)
	}
	if string(body) != "ok" {
		t.Errorf("/healthz body = %q, want ok", body)
	}

	// GET / returns the embedded index.html bytes.
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+listener.addr+"/", nil)
	req2.Header.Set("Authorization", "Bearer "+listener.sessionToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("/ do: %v", err)
	}
	rootBody, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("/ status = %d, body = %s", resp2.StatusCode, rootBody)
	}
	indexBytes, err := webui.BundleFS.ReadFile("web/dist/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	if !bytes.Equal(rootBody, indexBytes) {
		t.Errorf("/ body did not match embedded index.html\n got: %q\nwant: %q", rootBody, indexBytes)
	}

	// GET /api/v1/ still routes through chi to the API root catalog.
	// The webui handler must not shadow it.
	req3, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+listener.addr+"/api/v1", nil)
	req3.Header.Set("Authorization", "Bearer "+listener.sessionToken)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("/api/v1 do: %v", err)
	}
	apiBody, _ := io.ReadAll(resp3.Body)
	_ = resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("/api/v1 status = %d, body = %s", resp3.StatusCode, apiBody)
	}
	if bytes.Equal(apiBody, indexBytes) {
		t.Error("/api/v1 served the SPA index; chi routing is broken")
	}
	// Ack: the API root catalog returns JSON; a quick shape check.
	if !strings.Contains(string(apiBody), `"api_version"`) {
		t.Errorf("/api/v1 body missing api_version: %s", apiBody)
	}
}

// TestResolveWebUIHandler_FourCombinations exercises the four
// (api, webui) combinations called out by T5.6 acceptance: neither,
// api only, webui only, both. The handler-construction side and the
// banner side are independent; we drive them in parallel because the
// composition root in runDirector also drives them in parallel.
func TestResolveWebUIHandler_FourCombinations(t *testing.T) {
	t.Parallel()

	const (
		token = "tok"
		addr  = "127.0.0.1:54321"
	)

	cases := []struct {
		name        string
		api         bool
		webui       bool
		wantHandler bool
		// wantBanner is the line printRunBanner emits (excluding any
		// LAN warning, which the loopback test addr cannot trigger).
		// Empty means no banner line.
		wantBanner string
	}{
		{
			name:        "neither",
			wantHandler: false,
			wantBanner:  "",
		},
		{
			name:        "api only",
			api:         true,
			wantHandler: false,
			wantBanner:  "bcc: api at http://127.0.0.1:54321/api/v1\n",
		},
		{
			name:        "webui only",
			webui:       true,
			wantHandler: true,
			wantBanner:  "bcc: dashboard at http://127.0.0.1:54321/?t=" + token + "\n",
		},
		{
			name:        "both",
			api:         true,
			webui:       true,
			wantHandler: true,
			wantBanner:  "bcc: dashboard at http://127.0.0.1:54321/?t=" + token + "\n",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{Webui: config.Webui{Enabled: tt.webui}}
			h := resolveWebUIHandler(cfg, false)
			gotHandler := h != nil
			if gotHandler != tt.wantHandler {
				t.Errorf("resolveWebUIHandler returned non-nil = %v, want %v", gotHandler, tt.wantHandler)
			}

			// Banner: --webui implies --api at the banner level. Reflect
			// that promotion here so the assertion mirrors runDirector.
			apiBanner := tt.api || tt.webui
			webuiBanner := tt.webui

			var buf bytes.Buffer
			printRunBanner(&buf, addr, token, apiBanner, webuiBanner)
			if got := buf.String(); got != tt.wantBanner {
				t.Errorf("banner = %q, want %q", got, tt.wantBanner)
			}
		})
	}
}

// TestResolveWebUIHandler_DevSelectsProxy asserts the --webui-dev flag
// flips the constructor to NewDev. We cannot identity-compare because
// both constructors return distinct values per call; the proxy
// short-circuits /api/v1/ with 404 while the production handler does
// not see /api/v1/ at all (chi handles it). The proxy returns 404 for
// any non-API path it cannot reach (Vite is not running in tests),
// while the production handler returns the embedded index for /. That
// difference disambiguates them without coupling to internal types.
func TestResolveWebUIHandler_DevSelectsProxy(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Webui: config.Webui{Enabled: true}}
	prod := resolveWebUIHandler(cfg, false)
	dev := resolveWebUIHandler(cfg, true)
	if prod == nil || dev == nil {
		t.Fatalf("expected non-nil handlers, prod=%v dev=%v", prod, dev)
	}

	// Both serve /api/v1/ as 404 (the proxy explicitly, the prod
	// handler because it does not match the route). Probe / instead:
	// the prod handler returns 200 with the embedded index; the proxy
	// fails the upstream connect (Vite not running) and surfaces 502.
	probe := func(h http.Handler) int {
		req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
		rw := &captureRecorder{}
		h.ServeHTTP(rw, req)
		return rw.status
	}
	if got := probe(prod); got != http.StatusOK {
		t.Errorf("prod / status = %d, want 200", got)
	}
	// The proxy will fail to connect to 127.0.0.1:5173 in tests; any
	// non-200 (502 in practice) is fine, the point is to prove the
	// prod handler did not fire.
	if got := probe(dev); got == http.StatusOK {
		t.Errorf("dev / unexpectedly returned 200; production handler may have been wired")
	}
}

// captureRecorder is a minimal http.ResponseWriter that records the
// status code written. We avoid httptest.ResponseRecorder to keep the
// test free of /api/v1 routing concerns; this writer never serves a
// real response.
type captureRecorder struct {
	status int
	header http.Header
}

func (r *captureRecorder) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}
	return r.header
}

func (r *captureRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return len(p), nil
}

func (r *captureRecorder) WriteHeader(code int) {
	r.status = code
}
