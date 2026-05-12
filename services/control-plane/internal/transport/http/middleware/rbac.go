package middleware

import (
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// RequireRole gates the wrapped handler to principals whose role appears in
// allowed. Must come after RequireAuth in the middleware chain — without a
// principal in ctx the request is rejected with 401.
//
// The RBAC matrix lives in the spec (§8). Today we have three roles:
// owner > admin > member. The middleware is variadic so an endpoint can opt
// into "owner OR admin" without a per-pair helper.
func RequireRole(allowed ...domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			princ, ok := PrincipalFrom(r.Context())
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			for _, a := range allowed {
				if princ.Role == a {
					next.ServeHTTP(w, r)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"forbidden"}`))
		})
	}
}
