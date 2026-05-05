package services

import (
	"encoding/json"
	"errors"
)

// ErrorCode is the closed enum every protocol adapter maps to its own
// wire format (HTTP status code for the API, MCP error code for MCP,
// human-readable label for the TUI). The set is sealed: adding a new
// code is a deliberate edit here and a corresponding change in every
// adapter that maps codes to wire values.
type ErrorCode string

// Closed enum of canonical error codes. Names are lowercase
// underscore-separated so the JSON wire form (the result of
// Error.MarshalJSON) round-trips without case folding surprises.
const (
	CodeUnauthorized    ErrorCode = "unauthorized"
	CodeForbidden       ErrorCode = "forbidden"
	CodeSessionNotFound ErrorCode = "session_not_found"
	CodePhaseNotFound   ErrorCode = "phase_not_found"
	CodeTaskNotFound    ErrorCode = "task_not_found"
	CodeAttemptNotFound ErrorCode = "attempt_not_found"
	CodeRoleNotFound    ErrorCode = "role_not_found"
	CodeSeqGone         ErrorCode = "seq_gone"
	CodeNotImplemented  ErrorCode = "not_implemented"
	CodeInvalidRequest  ErrorCode = "invalid_request"
	CodeConflict        ErrorCode = "conflict"
	CodeInternal        ErrorCode = "internal"
)

// Error is the canonical service error. Code is the closed-enum value
// adapters key off; Message is a short human-readable explanation;
// Details, when non-nil, carries adapter-agnostic structured context
// (offending ids, requested seq, etc.) that wire envelopes can include
// verbatim.
//
// Error implements errors.Is by code: errors.Is(err, ErrSessionNotFound)
// is true when err's code matches CodeSessionNotFound, regardless of
// message or details. Wrapping with fmt.Errorf("%w", svcErr) preserves
// this property since fmt.Errorf threads the chain through Unwrap.
type Error struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// Error implements the error interface. The code prefixes the message
// so logs read consistently even when the message is empty.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

// Is reports whether target carries the same code as e. errors.Is
// walks the wrap chain via Unwrap, so a wrapped *Error compares equal
// to a sentinel with matching code.
func (e *Error) Is(target error) bool {
	if e == nil {
		return target == nil
	}
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// MarshalJSON serializes the error with stable field ordering so the
// wire form is reproducible across runs. Empty Message and nil Details
// are omitted so adapters can include the JSON in their envelopes
// without trailing nulls.
func (e *Error) MarshalJSON() ([]byte, error) {
	if e == nil {
		return []byte("null"), nil
	}
	type alias Error
	return json.Marshal((*alias)(e))
}

// newError constructs a fresh *Error so sentinels and call-site
// errors share the same shape.
func newError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WithDetails returns a copy of the receiver with details merged in.
// The receiver is not mutated so a sentinel remains usable as the
// canonical comparison target after the caller decorates a fresh
// instance with per-call context.
func (e *Error) WithDetails(details map[string]any) *Error {
	if e == nil {
		return nil
	}
	out := &Error{Code: e.Code, Message: e.Message}
	if len(details) == 0 {
		return out
	}
	out.Details = make(map[string]any, len(details))
	for k, v := range details {
		out.Details[k] = v
	}
	return out
}

// WithMessage returns a copy of the receiver with the message
// replaced. Sentinels stay constant; per-call errors carry the
// flavored message.
func (e *Error) WithMessage(msg string) *Error {
	if e == nil {
		return nil
	}
	out := *e
	out.Message = msg
	if e.Details != nil {
		out.Details = make(map[string]any, len(e.Details))
		for k, v := range e.Details {
			out.Details[k] = v
		}
	}
	return &out
}

// Sentinel errors. Each one carries the canonical code with no message
// or details so callers compare with errors.Is and decorate with
// WithMessage / WithDetails when constructing a per-call instance.
var (
	ErrUnauthorized    = newError(CodeUnauthorized, "unauthorized")
	ErrForbidden       = newError(CodeForbidden, "forbidden")
	ErrSessionNotFound = newError(CodeSessionNotFound, "session not found")
	ErrPhaseNotFound   = newError(CodePhaseNotFound, "phase not found")
	ErrTaskNotFound    = newError(CodeTaskNotFound, "task not found")
	ErrAttemptNotFound = newError(CodeAttemptNotFound, "attempt not found")
	ErrRoleNotFound    = newError(CodeRoleNotFound, "role not found")
	ErrSeqGone         = newError(CodeSeqGone, "sequence number predates the ring buffer")
	ErrNotImplemented  = newError(CodeNotImplemented, "not implemented")
	ErrInvalidRequest  = newError(CodeInvalidRequest, "invalid request")
	ErrConflict        = newError(CodeConflict, "conflict")
	ErrInternal        = newError(CodeInternal, "internal error")
)

// AsServiceError extracts the *Error from err's wrap chain. Returns
// nil and false when no *Error is present. Adapters use it to render
// the wire envelope without case-matching every sentinel.
func AsServiceError(err error) (*Error, bool) {
	var svc *Error
	if errors.As(err, &svc) {
		return svc, true
	}
	return nil, false
}
