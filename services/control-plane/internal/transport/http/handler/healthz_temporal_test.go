package handler_test

// Coverage for the temporal-liveness endpoint. Wires the heartbeat
// tracker directly without a Temporal client.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/temporal"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
)

// TestHealthzTemporal_NilHeartbeatReturns503 — when the heartbeat isn't
// wired, the endpoint returns 503 + status "unknown".
func TestHealthzTemporal_NilHeartbeatReturns503(t *testing.T) {
	h := handler.HealthzTemporal(nil, 60*time.Second)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/healthz/temporal", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp struct {
		Status     string `json:"status"`
		WindowSecs int    `json:"window_secs"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "unknown" {
		t.Fatalf("status: %q", resp.Status)
	}
	if resp.WindowSecs != 60 {
		t.Fatalf("window: %d", resp.WindowSecs)
	}
}

// TestHealthzTemporal_HealthyReturns200 — a recently-touched heartbeat
// produces 200 + status "ok" with a non-empty last_seen.
func TestHealthzTemporal_HealthyReturns200(t *testing.T) {
	hb := temporal.NewHeartbeat()
	hb.Touch()
	h := handler.HealthzTemporal(hb, 60*time.Second)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/healthz/temporal", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp struct {
		Status   string `json:"status"`
		LastSeen string `json:"last_seen"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status: %q", resp.Status)
	}
	if resp.LastSeen == "" {
		t.Fatalf("last_seen must be non-empty after Touch")
	}
}

// TestHealthzTemporal_StaleReturns503 — a never-touched heartbeat is
// stale → 503.
func TestHealthzTemporal_StaleReturns503(t *testing.T) {
	hb := temporal.NewHeartbeat() // never touched
	h := handler.HealthzTemporal(hb, 60*time.Second)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/healthz/temporal", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "stale" {
		t.Fatalf("status: %q", resp.Status)
	}
}

// TestHealthzTemporal_DefaultWindowSecs — passing window <= 0 falls back
// to 60s.
func TestHealthzTemporal_DefaultWindowSecs(t *testing.T) {
	hb := temporal.NewHeartbeat()
	hb.Touch()
	h := handler.HealthzTemporal(hb, 0) // 0 → default 60s
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/healthz/temporal", nil))
	var resp struct {
		WindowSecs int `json:"window_secs"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.WindowSecs != 60 {
		t.Fatalf("default window: %d", resp.WindowSecs)
	}
}
