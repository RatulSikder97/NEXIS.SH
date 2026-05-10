// Package probes provides standard /healthz and /readyz handlers.
package probes

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"nexis/backend/internal/platform/httpserver"
)

// Check is a readiness check function (returns nil if dep is healthy).
type Check func(context.Context) error

// Mount adds /healthz (always 200 if process alive) and /readyz (200 only if all checks pass).
func Mount(r chi.Router, checks ...Check) {
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpserver.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		failed := []string{}
		for i, c := range checks {
			if err := c(ctx); err != nil {
				failed = append(failed, indexedKey(i)+":"+err.Error())
			}
		}
		if len(failed) > 0 {
			httpserver.WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"status": "not_ready",
				"failed": failed,
			})
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
}

func indexedKey(i int) string {
	if i == 0 {
		return "check_0"
	}
	return "check_" + string(rune('0'+i))
}
