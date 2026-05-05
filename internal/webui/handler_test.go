package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandler_Routes drives the New() handler against the same set of
// well-known routes a mounted dashboard would receive in production.
// httptest.NewRecorder is used directly against the http.Handler so
// the test does not depend on a live listener.
func TestHandler_Routes(t *testing.T) {
	h := New()

	cases := []struct {
		name             string
		method           string
		path             string
		wantStatus       int
		wantContentType  string
		wantCacheControl string
		wantBodyContains string
		wantAllow        string
	}{
		{
			name:             "index served at root",
			method:           http.MethodGet,
			path:             "/",
			wantStatus:       http.StatusOK,
			wantContentType:  "text/html; charset=utf-8",
			wantCacheControl: "no-cache",
			wantBodyContains: "<title>bcc dashboard</title>",
		},
		{
			name:             "index served at /index.html",
			method:           http.MethodGet,
			path:             "/index.html",
			wantStatus:       http.StatusOK,
			wantContentType:  "text/html; charset=utf-8",
			wantCacheControl: "no-cache",
			wantBodyContains: `id="root"`,
		},
		{
			name:             "asset served from /assets",
			method:           http.MethodGet,
			path:             "/assets/app.css",
			wantStatus:       http.StatusOK,
			wantContentType:  "text/css; charset=utf-8",
			wantCacheControl: "public, max-age=31536000, immutable",
			wantBodyContains: "color-scheme: dark",
		},
		{
			name:             "healthz returns ok",
			method:           http.MethodGet,
			path:             "/healthz",
			wantStatus:       http.StatusOK,
			wantContentType:  "text/plain; charset=utf-8",
			wantCacheControl: "no-store",
			wantBodyContains: "ok",
		},
		{
			name:             "unknown path is 404",
			method:           http.MethodGet,
			path:             "/does-not-exist",
			wantStatus:       http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantBodyContains: "not found",
		},
		{
			name:             "missing asset is 404",
			method:           http.MethodGet,
			path:             "/assets/missing.js",
			wantStatus:       http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantBodyContains: "not found",
		},
		{
			name:             "font served from /fonts",
			method:           http.MethodGet,
			path:             "/fonts/geist/geist-latin-400-normal.woff2",
			wantStatus:       http.StatusOK,
			wantContentType:  "font/woff2",
			wantCacheControl: "public, max-age=31536000, immutable",
		},
		{
			name:             "missing font is 404",
			method:           http.MethodGet,
			path:             "/fonts/missing-font.woff2",
			wantStatus:       http.StatusNotFound,
			wantContentType:  "text/plain; charset=utf-8",
			wantBodyContains: "not found",
		},
		{
			name:       "favicon returns 204 No Content",
			method:     http.MethodGet,
			path:       "/favicon.ico",
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "post is 405",
			method:     http.MethodPost,
			path:       "/",
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  "GET, HEAD",
		},
		{
			name:       "put is 405",
			method:     http.MethodPut,
			path:       "/assets/app.css",
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  "GET, HEAD",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantContentType != "" {
				got := rec.Header().Get("Content-Type")
				if got != tc.wantContentType {
					t.Errorf("Content-Type: got %q, want %q", got, tc.wantContentType)
				}
			}
			if tc.wantCacheControl != "" {
				got := rec.Header().Get("Cache-Control")
				if got != tc.wantCacheControl {
					t.Errorf("Cache-Control: got %q, want %q", got, tc.wantCacheControl)
				}
			}
			if tc.wantAllow != "" {
				got := rec.Header().Get("Allow")
				if got != tc.wantAllow {
					t.Errorf("Allow: got %q, want %q", got, tc.wantAllow)
				}
			}
			if tc.wantBodyContains != "" {
				body, err := io.ReadAll(rec.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if !strings.Contains(string(body), tc.wantBodyContains) {
					t.Errorf("body missing %q\n--- got ---\n%s", tc.wantBodyContains, string(body))
				}
			}
		})
	}
}

// TestHandler_HeadRequests asserts HEAD is treated like GET for the
// same set of routes, covering the explicit branch in serveHealthz
// and the implicit handling in http.ServeContent for static paths.
func TestHandler_HeadRequests(t *testing.T) {
	h := New()

	cases := []struct {
		name string
		path string
	}{
		{"head index", "/"},
		{"head asset", "/assets/app.css"},
		{"head healthz", "/healthz"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodHead, tc.path, nil)
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status: got %d, want 200", rec.Code)
			}
			body, err := io.ReadAll(rec.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if len(body) != 0 {
				t.Errorf("HEAD body should be empty, got %d bytes", len(body))
			}
		})
	}
}
