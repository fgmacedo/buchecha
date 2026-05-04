package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestErrorIs_MatchesSentinelByCode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		err      error
		sentinel *Error
		want     bool
	}{
		{
			name:     "exact sentinel matches itself",
			err:      ErrSessionNotFound,
			sentinel: ErrSessionNotFound,
			want:     true,
		},
		{
			name:     "decorated copy still matches sentinel by code",
			err:      ErrSessionNotFound.WithDetails(map[string]any{"id": "abc"}),
			sentinel: ErrSessionNotFound,
			want:     true,
		},
		{
			name:     "wrapped instance still matches sentinel by code",
			err:      fmt.Errorf("services: get session: %w", ErrSessionNotFound),
			sentinel: ErrSessionNotFound,
			want:     true,
		},
		{
			name:     "different code does not match",
			err:      ErrPhaseNotFound,
			sentinel: ErrSessionNotFound,
			want:     false,
		},
		{
			name:     "non-service error does not match",
			err:      errors.New("plain"),
			sentinel: ErrSessionNotFound,
			want:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := errors.Is(tc.err, tc.sentinel)
			if got != tc.want {
				t.Fatalf("errors.Is = %v, want %v (err=%v)", got, tc.want, tc.err)
			}
		})
	}
}

func TestErrorAs_ExtractsServiceError(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("services: ctx: %w", ErrInvalidRequest.WithDetails(map[string]any{"field": "role"}))
	svc, ok := AsServiceError(wrapped)
	if !ok {
		t.Fatal("AsServiceError returned ok=false")
	}
	if svc.Code != CodeInvalidRequest {
		t.Fatalf("Code = %q, want %q", svc.Code, CodeInvalidRequest)
	}
	if svc.Details["field"] != "role" {
		t.Fatalf("Details[field] = %v, want %q", svc.Details["field"], "role")
	}
}

func TestError_DetailsSerialization(t *testing.T) {
	t.Parallel()

	err := ErrSessionNotFound.WithDetails(map[string]any{
		"session_id": "abcd",
		"hint":       "list sessions to see live ids",
	})
	body, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("json.Marshal returned error: %v", marshalErr)
	}
	var round struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(body, &round); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if round.Code != string(CodeSessionNotFound) {
		t.Fatalf("Code = %q, want %q", round.Code, CodeSessionNotFound)
	}
	if round.Details["session_id"] != "abcd" {
		t.Fatalf("Details[session_id] = %v, want %q", round.Details["session_id"], "abcd")
	}
}

func TestError_MarshalJSONOmitsEmpty(t *testing.T) {
	t.Parallel()

	err := newError(CodeInternal, "")
	body, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("json.Marshal returned error: %v", marshalErr)
	}
	var round map[string]any
	if err := json.Unmarshal(body, &round); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if _, ok := round["message"]; ok {
		t.Fatalf("Message should be omitted when empty: %v", round)
	}
	if _, ok := round["details"]; ok {
		t.Fatalf("Details should be omitted when nil: %v", round)
	}
}

func TestError_NilSafeMethods(t *testing.T) {
	t.Parallel()

	var nilErr *Error
	if nilErr.Error() != "" {
		t.Fatalf("nil.Error() = %q, want empty", nilErr.Error())
	}
	if nilErr.WithDetails(map[string]any{"k": "v"}) != nil {
		t.Fatal("nil.WithDetails should remain nil")
	}
	if nilErr.WithMessage("x") != nil {
		t.Fatal("nil.WithMessage should remain nil")
	}
}

func TestError_WithMethodsDoNotMutateReceiver(t *testing.T) {
	t.Parallel()

	// Sentinels must remain stable across decoration; otherwise a
	// later errors.Is(svcErr, sentinel) check could fail because the
	// sentinel's identity drifted.
	originalMessage := ErrSessionNotFound.Message
	decorated := ErrSessionNotFound.WithDetails(map[string]any{"id": "x"})
	decorated.Message = "mutated"
	if ErrSessionNotFound.Message != originalMessage {
		t.Fatalf("sentinel message mutated: %q", ErrSessionNotFound.Message)
	}
	if ErrSessionNotFound.Details != nil {
		t.Fatalf("sentinel details should be nil, got %v", ErrSessionNotFound.Details)
	}
}
