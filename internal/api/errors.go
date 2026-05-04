package api

import (
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/fgmacedo/buchecha/internal/services"
)

// ErrorResponse is the canonical HTTP error envelope. It mirrors the
// shape declared by schemas/error.schema.json: code is the closed-enum
// value adapters key off; message is a short human-readable explanation;
// details, when non-nil, carries adapter-agnostic structured context.
type ErrorResponse struct {
	Code    services.ErrorCode `json:"code"`
	Message string             `json:"message,omitempty"`
	Details map[string]any     `json:"details,omitempty"`
}

// codeStatus is the deterministic mapping between every closed-enum
// services.ErrorCode and its HTTP status. New codes added in V2+
// extend this table additively.
var codeStatus = map[services.ErrorCode]int{
	services.CodeUnauthorized:    http.StatusUnauthorized,
	services.CodeForbidden:       http.StatusForbidden,
	services.CodeSessionNotFound: http.StatusNotFound,
	services.CodePhaseNotFound:   http.StatusNotFound,
	services.CodeTaskNotFound:    http.StatusNotFound,
	services.CodeAttemptNotFound: http.StatusNotFound,
	services.CodeRoleNotFound:    http.StatusNotFound,
	services.CodeSeqGone:         http.StatusGone,
	services.CodeNotImplemented:  http.StatusNotImplemented,
	services.CodeInvalidRequest:  http.StatusBadRequest,
	services.CodeConflict:        http.StatusConflict,
	services.CodeInternal:        http.StatusInternalServerError,
}

// statusFor returns the HTTP status mapped to code. Unknown codes
// fall back to 500 so the wire never carries an undocumented status.
func statusFor(code services.ErrorCode) int {
	if s, ok := codeStatus[code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// WriteError serializes err as the canonical JSON envelope and writes
// it with the right HTTP status. Non-services errors are sanitized to
// CodeInternal with a generic message; the original error is logged
// at warn level instead of leaking into the response body.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	svc, ok := services.AsServiceError(err)
	if !ok {
		slog.Warn("api: non-service error mapped to internal",
			"method", r.Method,
			"path", r.URL.Path,
			"err", err,
		)
		svc = services.ErrInternal
	}
	resp := ErrorResponse{
		Code:    svc.Code,
		Message: svc.Message,
		Details: svc.Details,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusFor(svc.Code))
	_ = json.NewEncoder(w).Encode(resp)
}

// humaServiceError adapts a *services.Error so it can be returned
// directly from a huma operation handler. huma serializes the body
// via MarshalJSON, so the wire envelope ends up identical to the
// one WriteError emits on the chi-mounted handlers.
type humaServiceError struct {
	err    *services.Error
	status int
}

// Error satisfies the error interface and the huma.StatusError check.
func (e *humaServiceError) Error() string { return e.err.Error() }

// GetStatus is the huma.StatusError contract: returning the mapped
// HTTP status from codeStatus keeps the wire response in lock-step
// with the WriteError table.
func (e *humaServiceError) GetStatus() int { return e.status }

// MarshalJSON emits the canonical envelope so the JSON body matches
// schemas/error.schema.json byte-for-byte regardless of which code
// path produced the error.
func (e *humaServiceError) MarshalJSON() ([]byte, error) {
	return json.Marshal(ErrorResponse{
		Code:    e.err.Code,
		Message: e.err.Message,
		Details: e.err.Details,
	})
}

// HumaServiceError lifts err into a huma.StatusError that, when
// returned from a huma operation handler, makes huma write the
// canonical envelope with the mapped HTTP status. Non-services
// errors are sanitized to CodeInternal; the original error is logged
// via WriteError instead of leaking into the response body. Handlers
// that flow through huma return HumaServiceError(err); chi-mounted
// handlers (openapi, schemas, sse) keep using WriteError directly.
func HumaServiceError(err error) huma.StatusError {
	svc, ok := services.AsServiceError(err)
	if !ok {
		slog.Warn("api: non-service error wrapped for huma response",
			"err", err,
		)
		svc = services.ErrInternal
	}
	return &humaServiceError{err: svc, status: statusFor(svc.Code)}
}

// RegisterErrorComponent ensures the OpenAPI document references the
// canonical Error envelope under #/components/schemas/Error so the
// document is non-empty even before any business operation is
// registered. The huma registry is responsible for emitting the JSON
// Schema; we only have to ask for it.
func RegisterErrorComponent(s *Server) {
	api := s.HumaAPI()
	if api == nil {
		_ = s.Routes()
		api = s.HumaAPI()
	}
	if api == nil {
		return
	}
	api.OpenAPI().Components.Schemas.Schema(reflect.TypeOf(ErrorResponse{}), true, "Error")
}

// requestIDHeader is the canonical request-id header name; the value
// is propagated verbatim when supplied by the caller so an upstream
// proxy can correlate logs across hops.
const requestIDHeader = "X-Request-Id"

// RequestContext is the root middleware mounted by Routes. It mints
// an X-Request-Id (ULID) when the request does not already carry one
// and stamps a Server header so every response, success or error,
// is identifiable.
func RequestContext(next http.Handler) http.Handler {
	server := "bcc/" + BinaryVersion()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get(requestIDHeader)
		if rid == "" {
			rid = newRequestID()
			r.Header.Set(requestIDHeader, rid)
		}
		w.Header().Set(requestIDHeader, rid)
		w.Header().Set("Server", server)
		next.ServeHTTP(w, r)
	})
}

// crockford is the Crockford base32 alphabet ULIDs encode against.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newRequestID returns a 26-character ULID built from a 48-bit
// millisecond timestamp followed by 80 bits of entropy. The output
// is sortable lexicographically by time and uniformly random within
// the same millisecond, which is enough for request correlation.
func newRequestID() string {
	var bytes [16]byte
	ms := uint64(time.Now().UnixMilli())
	bytes[0] = byte(ms >> 40)
	bytes[1] = byte(ms >> 32)
	bytes[2] = byte(ms >> 24)
	bytes[3] = byte(ms >> 16)
	bytes[4] = byte(ms >> 8)
	bytes[5] = byte(ms)
	if _, err := rand.Read(bytes[6:]); err != nil {
		// crypto/rand.Read on supported platforms only fails when the
		// kernel CSPRNG is unavailable, which is fatal for us anyway.
		// Fall back to the timestamp bytes so we never panic on the
		// hot path.
		for i := 6; i < len(bytes); i++ {
			bytes[i] = byte(ms >> uint((i-6)*8))
		}
	}
	out := make([]byte, 26)
	out[0] = crockford[(bytes[0]&224)>>5]
	out[1] = crockford[bytes[0]&31]
	out[2] = crockford[(bytes[1]&248)>>3]
	out[3] = crockford[((bytes[1]&7)<<2)|((bytes[2]&192)>>6)]
	out[4] = crockford[(bytes[2]&62)>>1]
	out[5] = crockford[((bytes[2]&1)<<4)|((bytes[3]&240)>>4)]
	out[6] = crockford[((bytes[3]&15)<<1)|((bytes[4]&128)>>7)]
	out[7] = crockford[(bytes[4]&124)>>2]
	out[8] = crockford[((bytes[4]&3)<<3)|((bytes[5]&224)>>5)]
	out[9] = crockford[bytes[5]&31]
	out[10] = crockford[(bytes[6]&248)>>3]
	out[11] = crockford[((bytes[6]&7)<<2)|((bytes[7]&192)>>6)]
	out[12] = crockford[(bytes[7]&62)>>1]
	out[13] = crockford[((bytes[7]&1)<<4)|((bytes[8]&240)>>4)]
	out[14] = crockford[((bytes[8]&15)<<1)|((bytes[9]&128)>>7)]
	out[15] = crockford[(bytes[9]&124)>>2]
	out[16] = crockford[((bytes[9]&3)<<3)|((bytes[10]&224)>>5)]
	out[17] = crockford[bytes[10]&31]
	out[18] = crockford[(bytes[11]&248)>>3]
	out[19] = crockford[((bytes[11]&7)<<2)|((bytes[12]&192)>>6)]
	out[20] = crockford[(bytes[12]&62)>>1]
	out[21] = crockford[((bytes[12]&1)<<4)|((bytes[13]&240)>>4)]
	out[22] = crockford[((bytes[13]&15)<<1)|((bytes[14]&128)>>7)]
	out[23] = crockford[(bytes[14]&124)>>2]
	out[24] = crockford[((bytes[14]&3)<<3)|((bytes[15]&224)>>5)]
	out[25] = crockford[bytes[15]&31]
	return string(out)
}
