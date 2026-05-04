package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fgmacedo/buchecha/internal/services"
)

// SessionCookieName is the cookie the API sets and reads to carry
// the per-run session token. It is intentionally namespaced so other
// cookies on the same origin do not collide.
const SessionCookieName = "__bcc_api"

// QueryTokenParam is the one-shot URL parameter clients use to
// bootstrap the session cookie when a CLI prints a deep-link URL.
// Successful presentation produces a 302 to the same URL stripped
// of this parameter so the token does not stick in browser history.
const QueryTokenParam = "t"

// bearerPrefix is the case-sensitive prefix the Authorization header
// must begin with for the bearer path to match.
const bearerPrefix = "Bearer "

// NewSessionToken returns a fresh 64-character hexadecimal session
// token sourced from crypto/rand. The CLI mints one token per
// `bcc run` and shares it between the API and the SPA on the same
// origin. The function deliberately takes no arguments: token minting
// happens once at startup and does not need a clock or a custom rand
// source.
func NewSessionToken() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand only fails when the kernel CSPRNG is broken,
		// which is fatal for any auth flow: refusing to mint a
		// token forces the caller to abort startup loudly rather
		// than continue with a predictable value.
		panic("api: NewSessionToken: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(raw[:])
}

// SessionAuth returns a middleware that gates the wrapped handler
// behind the per-run session token. Acceptance order on each request:
//
//  1. ?t=<token> matches: set the session cookie and respond 302
//     to the same URL with the t parameter stripped so the token
//     never sticks in the browser address bar or referer chain.
//  2. Cookie or Authorization: Bearer matches: pass through.
//  3. Neither: write the canonical 401 error envelope and stop.
//
// Token comparisons run through crypto/subtle.ConstantTimeCompare to
// keep the middleware closed to timing oracles.
//
// Token values are never logged; the middleware logs at debug level
// with the request method and path only.
func SessionAuth(token string) func(http.Handler) http.Handler {
	want := []byte(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if t := r.URL.Query().Get(QueryTokenParam); t != "" {
				if tokenMatches(t, want) {
					setSessionCookie(w, t)
					http.Redirect(w, r, redirectURLWithoutToken(r), http.StatusFound)
					return
				}
				slog.Debug("api auth: query token rejected",
					"method", r.Method,
					"path", r.URL.Path,
				)
				WriteError(w, r, services.ErrUnauthorized)
				return
			}
			if c, err := r.Cookie(SessionCookieName); err == nil && tokenMatches(c.Value, want) {
				next.ServeHTTP(w, r)
				return
			}
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, bearerPrefix) {
				if tokenMatches(strings.TrimPrefix(h, bearerPrefix), want) {
					next.ServeHTTP(w, r)
					return
				}
			}
			slog.Debug("api auth: missing or invalid credential",
				"method", r.Method,
				"path", r.URL.Path,
			)
			WriteError(w, r, services.ErrUnauthorized)
		})
	}
}

// tokenMatches compares got against want in constant time. Length
// mismatches return false up front so we do not leak the expected
// length through the timing channel.
func tokenMatches(got string, want []byte) bool {
	gotBytes := []byte(got)
	if len(gotBytes) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare(gotBytes, want) == 1
}

// setSessionCookie installs the session cookie carrying token. The
// cookie is HttpOnly + SameSite=Strict and rooted at /; we do not
// set Secure because V1 is bound to loopback only.
func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// redirectURLWithoutToken returns the request URL with the t query
// parameter removed. Other query parameters and the path are
// preserved so deep links keep their per-page state.
func redirectURLWithoutToken(r *http.Request) string {
	cleaned := *r.URL
	q := cleaned.Query()
	q.Del(QueryTokenParam)
	cleaned.RawQuery = q.Encode()
	if cleaned.Path == "" {
		cleaned.Path = "/api/" + APIVersion
	}
	return cleaned.RequestURI()
}
