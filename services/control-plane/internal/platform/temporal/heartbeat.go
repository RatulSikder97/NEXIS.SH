package temporal

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"go.temporal.io/sdk/client"
)

// Heartbeat is the in-process liveness tracker the temporal healthz endpoint
// (Phase 8) reads. The control-plane spins up a single goroutine on boot that
// pings the Temporal frontend service on a ticker and updates this struct's
// atomic timestamp on each successful ping. The /v1/healthz/temporal endpoint
// returns 200 when LastSeen() is within the staleness window, 503 otherwise.
//
// Using a heartbeat goroutine rather than checking on every healthz request
// keeps the endpoint cheap (the BetterStack probe hits it every 30s) and
// decouples the staleness window from per-request latency to the Temporal
// frontend.
type Heartbeat struct {
	lastSeenUnixNano atomic.Int64
}

// NewHeartbeat returns a fresh heartbeat tracker. LastSeen() returns the zero
// time until the first successful ping completes.
func NewHeartbeat() *Heartbeat {
	return &Heartbeat{}
}

// LastSeen returns the wall-clock time of the most recent successful ping, or
// the zero time when no ping has yet succeeded.
func (h *Heartbeat) LastSeen() time.Time {
	n := h.lastSeenUnixNano.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

// IsHealthy reports whether the last ping landed within window. The healthz
// handler uses 60s by default; callers may tighten/loosen the window without
// changing this method.
func (h *Heartbeat) IsHealthy(window time.Duration) bool {
	ls := h.LastSeen()
	if ls.IsZero() {
		return false
	}
	return time.Since(ls) <= window
}

// Touch records a successful ping at the current wall-clock instant. Exported
// so an in-process worker registration path can mark itself live without
// going through the network ping path (useful in tests / dev where the SDK
// returns a healthy worker that the server can trust).
func (h *Heartbeat) Touch() {
	h.lastSeenUnixNano.Store(time.Now().UnixNano())
}

// StartHeartbeat launches a goroutine that pings the Temporal frontend every
// interval and updates h on success. ctx cancellation stops the goroutine
// cleanly. The ping uses CheckHealth which is the SDK's lightweight
// frontend-service readiness probe.
//
// Returns immediately; the goroutine runs until ctx is done.
func StartHeartbeat(ctx context.Context, c client.Client, h *Heartbeat, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	// Initial ping so the healthz endpoint flips to OK quickly on boot rather
	// than waiting one tick.
	if err := ping(ctx, c); err == nil {
		h.Touch()
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := ping(ctx, c); err != nil {
					logger.Warn("temporal.heartbeat.ping_failed", "err", err)
					continue
				}
				h.Touch()
			}
		}
	}()
}

// ping is the actual SDK call. CheckHealth on Temporal Go SDK >= 1.21 talks to
// the frontend service's Health API; for older SDKs the workflow service
// describe namespace fallback works equivalently.
func ping(ctx context.Context, c client.Client) error {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// CheckHealth exists on the v1.21+ Go SDK. If the SDK version pinned here
	// lacks it the linker will fail; we'll fall back to a workflow-service
	// describe instead by switching to client.WorkflowService().GetClusterInfo.
	_, err := c.CheckHealth(pingCtx, &client.CheckHealthRequest{})
	return err
}
