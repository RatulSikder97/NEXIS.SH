// CORS middleware: lets the browser-side web app at cfg.AppBaseURL exchange
// credentialed requests with the control-plane. We do NOT use a wildcard
// origin — `Access-Control-Allow-Credentials: true` is incompatible with
// `Access-Control-Allow-Origin: *`. Instead we echo back the configured
// origin only when the request's Origin header matches it. Anything else
// gets the headers omitted so the browser refuses the response.
//
// The set of allowed methods/headers is intentionally narrow: this is a
// JSON API. Preflights (OPTIONS) short-circuit with 204 before chi tries
// to match a real route — chi otherwise returns 405 for OPTIONS, which
// the browser treats as a CORS failure.
package middleware

import "net/http"

// CORS returns middleware that allows credentialed requests from the given
// allowed origin (typically the web app's APP_BASE_URL). When `allowed` is
// empty the middleware is a no-op — useful for tests that don't care about
// CORS and for production deployments that front the API on the same origin
// as the web app.
func CORS(allowed string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if allowed != "" && origin == allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "300")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
