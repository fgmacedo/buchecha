package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/services"
)

// TestWriteError_MapsCodesToStatuses covers the deterministic mapping
// table: every closed-enum services.ErrorCode must lift to its
// documented HTTP status. A non-services error falls back to 500
// with the canonical "internal" code so the wire never carries a
// raw error string.
func TestWriteError_MapsCodesToStatuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		err      error
		wantCode services.ErrorCode
		wantHTTP int
	}{
		{name: "unauthorized", err: services.ErrUnauthorized, wantCode: services.CodeUnauthorized, wantHTTP: http.StatusUnauthorized},
		{name: "forbidden", err: services.ErrForbidden, wantCode: services.CodeForbidden, wantHTTP: http.StatusForbidden},
		{name: "session not found", err: services.ErrSessionNotFound, wantCode: services.CodeSessionNotFound, wantHTTP: http.StatusNotFound},
		{name: "phase not found", err: services.ErrPhaseNotFound, wantCode: services.CodePhaseNotFound, wantHTTP: http.StatusNotFound},
		{name: "task not found", err: services.ErrTaskNotFound, wantCode: services.CodeTaskNotFound, wantHTTP: http.StatusNotFound},
		{name: "attempt not found", err: services.ErrAttemptNotFound, wantCode: services.CodeAttemptNotFound, wantHTTP: http.StatusNotFound},
		{name: "role not found", err: services.ErrRoleNotFound, wantCode: services.CodeRoleNotFound, wantHTTP: http.StatusNotFound},
		{name: "seq gone", err: services.ErrSeqGone, wantCode: services.CodeSeqGone, wantHTTP: http.StatusGone},
		{name: "not implemented", err: services.ErrNotImplemented, wantCode: services.CodeNotImplemented, wantHTTP: http.StatusNotImplemented},
		{name: "invalid request", err: services.ErrInvalidRequest, wantCode: services.CodeInvalidRequest, wantHTTP: http.StatusBadRequest},
		{name: "conflict", err: services.ErrConflict, wantCode: services.CodeConflict, wantHTTP: http.StatusConflict},
		{name: "internal", err: services.ErrInternal, wantCode: services.CodeInternal, wantHTTP: http.StatusInternalServerError},
		{name: "non-service error", err: errors.New("boom"), wantCode: services.CodeInternal, wantHTTP: http.StatusInternalServerError},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/probe", nil)
			WriteError(rec, req, tt.err)

			if rec.Code != tt.wantHTTP {
				t.Fatalf("status: got %d, want %d", rec.Code, tt.wantHTTP)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
				t.Errorf("content-type: got %q, want JSON", ct)
			}
			var body ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Code != tt.wantCode {
				t.Errorf("code: got %q, want %q", body.Code, tt.wantCode)
			}
		})
	}
}

// TestWriteError_DoesNotLeakNonServiceErrorMessage confirms the
// sanitization contract: when the source error is not *services.Error,
// the wire envelope carries the canonical "internal error" message
// rather than the raw err.Error() text.
func TestWriteError_DoesNotLeakNonServiceErrorMessage(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	WriteError(rec, req, errors.New("secret leak"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
	if got := rec.Body.String(); strings.Contains(got, "secret leak") {
		t.Fatalf("response leaked raw error string: %q", got)
	}
}

// TestRequestContext_StampsHeaders asserts every response carries
// X-Request-Id and Server. The request-id is preserved when supplied
// by the caller and minted otherwise.
func TestRequestContext_StampsHeaders(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := RequestContext(inner)

	t.Run("mints when missing", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		h.ServeHTTP(rec, req)

		got := rec.Header().Get("X-Request-Id")
		if len(got) != 26 {
			t.Errorf("minted request-id length: got %d (%q), want 26", len(got), got)
		}
		if !strings.HasPrefix(rec.Header().Get("Server"), "bcc/") {
			t.Errorf("server header: got %q, want bcc/<version>", rec.Header().Get("Server"))
		}
	})

	t.Run("propagates supplied request-id", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-Id", "01HABCDEFGHJKMNPQRSTVWXYZ0")
		h.ServeHTTP(rec, req)

		if got := rec.Header().Get("X-Request-Id"); got != "01HABCDEFGHJKMNPQRSTVWXYZ0" {
			t.Errorf("propagated request-id: got %q, want 01HABCDEFGHJKMNPQRSTVWXYZ0", got)
		}
	})
}

// TestNewRequestID_Properties checks the inline ULID generator emits
// 26 Crockford base32 characters and that two consecutive calls
// produce different ids.
func TestNewRequestID_Properties(t *testing.T) {
	t.Parallel()

	a := newRequestID()
	b := newRequestID()
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("ULID length: got %d/%d, want 26", len(a), len(b))
	}
	for _, ch := range a {
		if !strings.ContainsRune(crockford, ch) {
			t.Fatalf("ULID character not in Crockford alphabet: %q in %q", ch, a)
		}
	}
	if a == b {
		t.Errorf("two consecutive ULIDs collided: %q", a)
	}
}
