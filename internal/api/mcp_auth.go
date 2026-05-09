package api

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fgmacedo/buchecha/internal/services"
)

// mcpRoleHeader names the request header MCP agents set so the
// composition root can decide which role is calling. Mirrors
// internal/mcp.RoleHeader; duplicated here so the api package does
// not need to import internal/mcp (which would violate layer rules).
const mcpRoleHeader = "X-BCC-Role"

// MCPAuth returns a chi middleware that gates the wrapped handler
// behind the run-wide MCP bearer token plus the role allow-list. A
// request must carry both:
//
//  1. Authorization: Bearer <token> matching the run-wide MCP token,
//  2. X-BCC-Role: <role> matching one of the registered connection
//     names.
//
// Either failure produces the canonical unauthorized envelope with
// the same code (CodeUnauthorized) callers see on the /api/v1/*
// subtree, so consumers can distinguish "not authenticated" from
// other states by code alone. The token comparison runs through
// crypto/subtle.ConstantTimeCompare to avoid leaking the expected
// length through the timing channel.
//
// Token values are never logged; the middleware logs at debug level
// with the request method and path only.
//
// The api package does not import internal/supervision/dag or
// internal/mcp; it receives the allowed roles as a closed list from
// the composition root.
func MCPAuth(token string, allowedRoles []string) func(http.Handler) http.Handler {
	want := []byte(token)
	roleSet := make(map[string]struct{}, len(allowedRoles))
	for _, name := range allowedRoles {
		roleSet[name] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !mcpBearerMatches(r, want) {
				slog.Debug("api auth: mcp bearer rejected",
					"method", r.Method,
					"path", r.URL.Path,
				)
				WriteError(w, r, services.ErrUnauthorized)
				return
			}
			role := r.Header.Get(mcpRoleHeader)
			if _, ok := roleSet[role]; !ok {
				slog.Debug("api auth: mcp role rejected",
					"method", r.Method,
					"path", r.URL.Path,
					"role_present", role != "",
				)
				WriteError(w, r, services.ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// mcpBearerMatches inspects the Authorization header for a Bearer
// token equal to want in constant time. Empty want or empty header
// returns false up front so neither side leaks length.
func mcpBearerMatches(r *http.Request, want []byte) bool {
	if len(want) == 0 {
		return false
	}
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, bearerPrefix) {
		return false
	}
	got := []byte(strings.TrimPrefix(h, bearerPrefix))
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
