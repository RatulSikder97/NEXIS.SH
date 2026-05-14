package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
)

// BodyLimit returns a middleware that wraps r.Body in http.MaxBytesReader so
// any handler that reads the body sees io.EOF after maxBytes and the
// MaxBytesError is propagated up. Without this the control-plane is
// vulnerable to memory pressure from a malicious upstream pushing a multi-GB
// request body — the existing /v1 surface only checks ContentLength on a
// handful of routes and Decode/io.ReadAll otherwise streams the full body
// into memory.
//
// The wrapper returns 413 Request Entity Too Large with a structured JSON
// envelope so callers can distinguish "your payload is too big" from a
// generic 400. Other body-read errors (premature EOF, etc.) fall through to
// the handler so existing error-path tests aren't broken.
//
// maxBytes is the soft cap measured against r.ContentLength PLUS the
// MaxBytesReader's running counter. A negative ContentLength (chunked
// transfer) is still capped because MaxBytesReader tracks bytes consumed.
//
// Stacking is safe: Chi mounts middleware as a stack; multiple BodyLimit
// instances with different limits compose as the tightest cap that the
// innermost handler observes.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Early-reject when ContentLength explicitly declares an oversize
			// body — saves the upstream socket from streaming a payload we'll
			// only throw away.
			if r.ContentLength > maxBytes {
				writeBodyLimitError(w)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// writeBodyLimitError emits the canonical 413 envelope. The body matches the
// rest of the surface's "error" shape so dashboards / SDKs can rely on a
// uniform key for grouping.
func writeBodyLimitError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusRequestEntityTooLarge)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "request_body_too_large"})
}

// IsBodyLimitError reports whether err originates from a MaxBytesReader cap
// firing. Handlers that read the body manually (io.ReadAll, json.Decoder)
// can call this to short-circuit and emit the canonical 413 themselves
// without leaking the underlying error.
func IsBodyLimitError(err error) bool {
	if err == nil {
		return false
	}
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
