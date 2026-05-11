// Package handler holds HTTP handler functions. Each file owns one route family.
package handler

import (
	"encoding/json"
	"net/http"
)

// Healthz returns a 200 with {service, status} JSON. Used for compose health checks.
func Healthz(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": service,
		})
	}
}
