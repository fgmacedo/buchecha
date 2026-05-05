package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// TestRoutes_RegistersAPIv1Subtree confirms the /api/v1 subtree is
// always registered, even when no optional Mounts are configured.
// We verify by walking the chi router and matching against the
// expected mount prefixes; the huma adapter installs its own routes
// under /api/v1, including /api/v1/openapi.json.
func TestRoutes_RegistersAPIv1Subtree(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		mounts    Mounts
		wantPaths []string
	}{
		{
			name:      "bare server registers only api/v1",
			mounts:    Mounts{},
			wantPaths: []string{"/api/v1"},
		},
		{
			name: "with mcp mount",
			mounts: Mounts{
				MCP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			},
			wantPaths: []string{"/api/v1", "/mcp"},
		},
		{
			name: "with webui mount",
			mounts: Mounts{
				WebUI: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			},
			wantPaths: []string{"/api/v1", "/"},
		},
		{
			name: "with both mounts",
			mounts: Mounts{
				MCP:   http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
				WebUI: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			},
			wantPaths: []string{"/api/v1", "/mcp", "/"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := New(nil).WithMounts(tt.mounts)
			h := s.Routes()
			r, ok := h.(chi.Router)
			if !ok {
				t.Fatalf("Routes() did not return a chi.Router: %T", h)
			}

			seen := map[string]bool{}
			err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
				for _, want := range tt.wantPaths {
					if strings.HasPrefix(route, want) {
						seen[want] = true
					}
				}
				return nil
			})
			if err != nil {
				t.Fatalf("chi.Walk: %v", err)
			}
			for _, want := range tt.wantPaths {
				if !seen[want] {
					t.Errorf("expected route prefix %q to be registered, walk did not find it", want)
				}
			}
		})
	}
}

// TestRoutes_OpenAPIDocumentServed verifies the huma adapter
// publishes a 3.1 OpenAPI document at /api/v1/openapi.json. This is
// the discovery endpoint clients hit; if it disappears, every
// downstream consumer breaks.
func TestRoutes_OpenAPIDocumentServed(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(New(nil).Routes())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/openapi.json")
	if err != nil {
		t.Fatalf("get openapi.json: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("openapi.json status: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("openapi.json content-type: got %q, want JSON", ct)
	}
}

// TestRoutes_MountsDispatch verifies the optional Mounts handlers
// receive requests at the documented mount points.
func TestRoutes_MountsDispatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		buildSrv func(hits *int32) *Server
		path     string
		wantHits int32
		wantCode int
	}{
		{
			name: "mcp mount receives /mcp/",
			buildSrv: func(hits *int32) *Server {
				return New(nil).WithMounts(Mounts{
					MCP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						atomic.AddInt32(hits, 1)
						w.WriteHeader(http.StatusNoContent)
					}),
				})
			},
			path:     "/mcp/",
			wantHits: 1,
			wantCode: http.StatusNoContent,
		},
		{
			name: "webui mount receives /",
			buildSrv: func(hits *int32) *Server {
				return New(nil).WithMounts(Mounts{
					WebUI: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						atomic.AddInt32(hits, 1)
						w.WriteHeader(http.StatusTeapot)
					}),
				})
			},
			path:     "/",
			wantHits: 1,
			wantCode: http.StatusTeapot,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hits int32
			srv := httptest.NewServer(tt.buildSrv(&hits).Routes())
			t.Cleanup(srv.Close)

			resp, err := http.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatalf("get %s: %v", tt.path, err)
			}
			t.Cleanup(func() { _ = resp.Body.Close() })

			if got := atomic.LoadInt32(&hits); got != tt.wantHits {
				t.Errorf("mount handler hits: got %d, want %d", got, tt.wantHits)
			}
			if resp.StatusCode != tt.wantCode {
				t.Errorf("status: got %d, want %d", resp.StatusCode, tt.wantCode)
			}
		})
	}
}

// TestListen_StopsOnContextCancel asserts that Listen returns
// promptly after ctx is cancelled and surfaces no error for a clean
// shutdown.
func TestListen_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	s := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		// bind to :0 so the OS picks a free port; we do not need
		// to talk to the listener for this test.
		errCh <- s.Listen(ctx, "127.0.0.1:0")
	}()

	// Give the goroutine enough time to bind before we cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Listen returned unexpected error: %v", err)
		}
	case <-time.After(shutdownGracePeriod + 2*time.Second):
		t.Fatal("Listen did not return within grace period after ctx cancel")
	}
}

// TestListen_ReportsBindError covers the failure path: when the
// requested bind address is invalid, Listen returns a wrapped error
// rather than blocking.
func TestListen_ReportsBindError(t *testing.T) {
	t.Parallel()

	s := New(nil)
	err := s.Listen(context.Background(), "127.0.0.1:-1")
	if err == nil {
		t.Fatal("expected bind error, got nil")
	}
	// Sanity-check: the wrapped error preserves the listen
	// failure rather than swallowing it.
	if errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("bind error should not be ErrServerClosed: %v", err)
	}
}
