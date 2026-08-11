// Package handler — Temporal liveness HTTP surface (Phase 8 — public beta).
//
// One endpoint: GET /v1/healthz/temporal. Returns 200 with the last-seen
// timestamp when the in-process heartbeat goroutine pinged the Temporal
// frontend within the staleness window (60s by default); 503 otherwise.
//
// Intentionally unauthenticated — BetterStack pulls this every 30s. The
// response body is intentionally minimal so a probe can grep status fields
// without an SDK.
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/temporal"
)

// healthzTemporalResp is the body of /v1/healthz/temporal. The status field is
// one of "ok" / "stale" / "unknown". Timestamp is RFC3339; "" when no ping has
// ever succeeded.
type healthzTemporalResp struct {
	Status     string `json:"status"`
	LastSeen   string `json:"last_seen,omitempty"`
	WindowSecs int    `json:"window_secs"`
}

// HealthzTemporal wires GET /v1/healthz/temporal. Window is the staleness
// budget; the Phase 8 spec calls for 60s.
func HealthzTemporal(hb *temporal.Heartbeat, window time.Duration) http.HandlerFunc {
	if window <= 0 {
		window = 60 * time.Second
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := healthzTemporalResp{WindowSecs: int(window / time.Second)}
		if hb == nil {
			resp.Status = "unknown"
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		ls := hb.LastSeen()
		if !ls.IsZero() {
			resp.LastSeen = ls.Format(time.RFC3339)
		}
		if hb.IsHealthy(window) {
			resp.Status = "ok"
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		resp.Status = "stale"
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(resp)
	}
}
