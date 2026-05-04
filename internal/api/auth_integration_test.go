package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/fgmacedo/buchecha/internal/services"
)

// TestAuthIntegration_ThroughChiRouter exercises the full Routes()
// stack with auth applied. A test-only huma operation registers a
// 204 probe under /api/v1/probe so the cookie and bearer paths have
// something downstream to hit; the operation is scoped to the test
// and never leaks into production code (the server is rebuilt on
// every subtest).
func TestAuthIntegration_ThroughChiRouter(t *testing.T) {
	t.Parallel()

	const token = "cafef00dcafef00dcafef00dcafef00dcafef00dcafef00dcafef00dcafef00d"

	build := func() *httptest.Server {
		s := New(nil).WithAuth(token)
		handler := s.Routes()
		// Register a probe operation against the captured huma.API
		// so authenticated requests have something to dispatch to.
		// Calling huma.Register after Routes() works because the
		// adapter mutates the underlying chi router directly.
		huma.Register(s.HumaAPI(), huma.Operation{
			Method:        http.MethodGet,
			Path:          "/probe",
			DefaultStatus: http.StatusNoContent,
		}, func(_ context.Context, _ *struct{}) (*struct{}, error) {
			return nil, nil
		})
		return httptest.NewServer(handler)
	}

	noFollow := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	t.Run("cookie path reaches probe", func(t *testing.T) {
		t.Parallel()
		srv := build()
		t.Cleanup(srv.Close)

		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/probe", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })

		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status: got %d, want 204", resp.StatusCode)
		}
		if rid := resp.Header.Get("X-Request-Id"); rid == "" {
			t.Errorf("X-Request-Id missing on 204")
		}
		if srv := resp.Header.Get("Server"); !strings.HasPrefix(srv, "bcc/") {
			t.Errorf("Server header: got %q, want bcc/<version>", srv)
		}
	})

	t.Run("bearer path reaches probe", func(t *testing.T) {
		t.Parallel()
		srv := build()
		t.Cleanup(srv.Close)

		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/probe", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })

		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status: got %d, want 204", resp.StatusCode)
		}
	})

	t.Run("query token redirects with cookie", func(t *testing.T) {
		t.Parallel()
		srv := build()
		t.Cleanup(srv.Close)

		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/probe?t="+token+"&keep=1", nil)
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })

		if resp.StatusCode != http.StatusFound {
			t.Fatalf("status: got %d, want 302", resp.StatusCode)
		}
		var found *http.Cookie
		for _, c := range resp.Cookies() {
			if c.Name == SessionCookieName {
				found = c
				break
			}
		}
		if found == nil {
			t.Fatalf("session cookie missing")
		}
		if found.Value != token {
			t.Errorf("cookie value: got %q, want token", found.Value)
		}
		if !found.HttpOnly || found.SameSite != http.SameSiteStrictMode || found.Path != "/" {
			t.Errorf("cookie attributes: got HttpOnly=%v SameSite=%v Path=%q",
				found.HttpOnly, found.SameSite, found.Path)
		}
		loc := resp.Header.Get("Location")
		if strings.Contains(loc, "t=") {
			t.Errorf("Location must strip t param: %q", loc)
		}
		if !strings.Contains(loc, "keep=1") {
			t.Errorf("Location must preserve other query params: %q", loc)
		}
	})

	t.Run("missing credential returns 401 envelope", func(t *testing.T) {
		t.Parallel()
		srv := build()
		t.Cleanup(srv.Close)

		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/probe", nil)
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "json") {
			t.Errorf("content-type: got %q, want JSON", ct)
		}
		var body ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Code != services.CodeUnauthorized {
			t.Errorf("code: got %q, want %q", body.Code, services.CodeUnauthorized)
		}
	})
}
