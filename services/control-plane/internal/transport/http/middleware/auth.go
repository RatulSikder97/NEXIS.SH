// Package middleware holds Chi-compatible HTTP middlewares specific to the
// control-plane. The Auth middleware decorates every request with an optional
// Principal extracted from the Authorization header or session cookie; the
// RequireAuth middleware enforces presence and rejects unauthenticated calls
// with a JSON 401.
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// ctxKeyPrincipal is the unexported context key the Auth middleware uses to
// attach a verified Principal. Handlers read it via PrincipalFrom.
type ctxKeyPrincipal struct{}

// sessionCookieName is the canonical session-cookie name set by the handler
// layer and read by Auth. It matches the cookie attribute used by the web
// dashboard so a single name is honoured across HTTP and browser contexts.
const sessionCookieName = "nexis_session"

// apiKeyPrefix identifies tokens that should be verified as API keys rather
// than session JWTs. Keep in sync with the local adapter's apikey constant.
const apiKeyPrefix = "nx_live_"

// PrincipalFrom returns the verified Principal attached to ctx by Auth, plus a
// boolean indicating whether a principal was present. Handlers should call
// this after RequireAuth — that middleware guarantees a principal exists.
func PrincipalFrom(ctx context.Context) (domain.Principal, bool) {
	p, ok := ctx.Value(ctxKeyPrincipal{}).(domain.Principal)
	return p, ok
}

// WithPrincipal returns a context that carries the given principal. Exported
// because tests in other packages need to inject a principal directly without
// going through the Auth verification path.
func WithPrincipal(ctx context.Context, p domain.Principal) context.Context {
	return context.WithValue(ctx, ctxKeyPrincipal{}, p)
}

// Auth extracts a bearer token from the Authorization header or the
// nexis_session cookie, verifies it via the AuthProvider, and stores the
// resulting Principal in the request context. Auth never rejects on its own —
// missing or invalid credentials simply leave the principal absent. Use
// RequireAuth on routes that demand authentication.
func Auth(p domain.AuthProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			princ, err := verify(r.Context(), p, token)
			if err != nil {
				// Invalid token — fall through without a principal. RequireAuth
				// downstream will issue the 401; public routes still work.
				next.ServeHTTP(w, r)
				return
			}
			ctx := WithPrincipal(r.Context(), princ)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth rejects requests that lack a verified principal with a JSON 401.
// It must come after Auth in the middleware chain.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := PrincipalFrom(r.Context()); !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// extractToken pulls a bearer token from Authorization first, then falls back
// to the nexis_session cookie. Returns the empty string if neither is present.
func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		const bearer = "Bearer "
		if strings.HasPrefix(h, bearer) {
			return strings.TrimSpace(h[len(bearer):])
		}
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		return c.Value
	}
	return ""
}

// verify routes the token to the right AuthProvider method based on prefix.
// nx_live_* → VerifyAPIKey; everything else → VerifyToken.
func verify(ctx context.Context, p domain.AuthProvider, token string) (domain.Principal, error) {
	if strings.HasPrefix(token, apiKeyPrefix) {
		return p.VerifyAPIKey(ctx, token)
	}
	return p.VerifyToken(ctx, token)
}
