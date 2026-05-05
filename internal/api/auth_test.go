package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/services"
)

// TestNewSessionToken_Properties asserts the mint helper produces a
// 64-character lowercase hex string and that two consecutive calls
// produce different values. Hex is the wire form because it is
// URL-safe and trivial to log redactedly without character classes.
func TestNewSessionToken_Properties(t *testing.T) {
	t.Parallel()

	a := NewSessionToken()
	b := NewSessionToken()
	if len(a) != 64 || len(b) != 64 {
		t.Fatalf("token length: got %d/%d, want 64", len(a), len(b))
	}
	for _, r := range a {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			t.Fatalf("token character not hex: %q in %q", r, a)
		}
	}
	if a == b {
		t.Errorf("two consecutive tokens collided: %q", a)
	}
}

// TestSessionAuth_AcceptsValidCredentials walks every accepted entry
// path: cookie, bearer header, and the bootstrap query parameter
// (which sets the cookie and redirects). Missing credentials must
// return 401 with the canonical JSON envelope and code
// "unauthorized".
func TestSessionAuth_AcceptsValidCredentials(t *testing.T) {
	t.Parallel()

	const token = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := SessionAuth(token)(probe)

	t.Run("cookie", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status: got %d, want 204", rec.Code)
		}
	})

	t.Run("bearer", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status: got %d, want 204", rec.Code)
		}
	})

	t.Run("query token sets cookie and redirects", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1?t="+token+"&keep=1", nil)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("status: got %d, want 302", rec.Code)
		}
		got := rec.Result().Cookies()
		if len(got) == 0 {
			t.Fatalf("expected Set-Cookie, got none")
		}
		var found *http.Cookie
		for _, c := range got {
			if c.Name == SessionCookieName {
				found = c
				break
			}
		}
		if found == nil {
			t.Fatalf("session cookie %q not set", SessionCookieName)
		}
		if found.Value != token {
			t.Errorf("cookie value: got %q, want token", found.Value)
		}
		if !found.HttpOnly {
			t.Error("session cookie must be HttpOnly")
		}
		if found.SameSite != http.SameSiteStrictMode {
			t.Errorf("SameSite: got %v, want Strict", found.SameSite)
		}
		if found.Path != "/" {
			t.Errorf("Path: got %q, want \"/\"", found.Path)
		}
		loc := rec.Header().Get("Location")
		if strings.Contains(loc, "t=") {
			t.Errorf("Location must strip t param: %q", loc)
		}
		if !strings.Contains(loc, "keep=1") {
			t.Errorf("Location must preserve other query params: %q", loc)
		}
	})

	t.Run("missing credential returns unauthorized envelope", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", rec.Code)
		}
		var body ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Code != services.CodeUnauthorized {
			t.Errorf("code: got %q, want %q", body.Code, services.CodeUnauthorized)
		}
	})

	t.Run("invalid bearer rejected", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
		req.Header.Set("Authorization", "Bearer not-the-token")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", rec.Code)
		}
	})

	t.Run("invalid query token rejected", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1?t=wrong", nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", rec.Code)
		}
	})
}
