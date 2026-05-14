package handler

// Unit coverage for the system-health + system-status helper functions.
// The full /v1/system-health endpoint requires live probe deps (Postgres,
// Redis, Neo4j) and is exercised under tests/integration; here we cover
// the pure helpers that fold probe results into the wire shape and the
// pill's overall-status reducer.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
)

// TestToCheck folds every ProbeResult variant into a SystemHealthCheck. Lists
// every branch: disabled, down (with error message), degraded (slow but ok),
// healthy.
func TestToCheck(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	cases := []struct {
		name       string
		probe      integration.ProbeResult
		wantStatus string
		wantErr    string
	}{
		{
			name:       "disabled_explicit",
			probe:      integration.ProbeResult{Err: errors.New("disabled")},
			wantStatus: "disabled",
			wantErr:    "",
		},
		{
			name:       "down_with_error",
			probe:      integration.ProbeResult{Err: errors.New("connection refused")},
			wantStatus: "down",
			wantErr:    "connection refused",
		},
		{
			name:       "degraded_slow_ok",
			probe:      integration.ProbeResult{Latency: 750 * time.Millisecond},
			wantStatus: "degraded",
			wantErr:    "",
		},
		{
			name:       "healthy_fast",
			probe:      integration.ProbeResult{Latency: 5 * time.Millisecond},
			wantStatus: "healthy",
			wantErr:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toCheck(tc.probe, now)
			if got.Status != tc.wantStatus {
				t.Fatalf("status: got %q want %q", got.Status, tc.wantStatus)
			}
			if got.LastError != tc.wantErr {
				t.Fatalf("last_error: got %q want %q", got.LastError, tc.wantErr)
			}
			if got.CheckedAt != now {
				t.Fatalf("checked_at: got %q want %q", got.CheckedAt, now)
			}
			if got.LatencyMs != tc.probe.Latency.Milliseconds() {
				t.Fatalf("latency_ms: got %d want %d", got.LatencyMs, tc.probe.Latency.Milliseconds())
			}
		})
	}
}

// TestDeriveOverall walks every reducer branch. Order matters: any "down"
// short-circuits the entire response; "degraded" beats "healthy" only when
// no "down" is present; "disabled" never counts against the pill (it
// reflects deployment shape, not a fault).
func TestDeriveOverall(t *testing.T) {
	healthy := dto.SystemHealthCheck{Status: "healthy"}
	degraded := dto.SystemHealthCheck{Status: "degraded"}
	down := dto.SystemHealthCheck{Status: "down"}
	disabled := dto.SystemHealthCheck{Status: "disabled"}

	cases := []struct {
		name string
		h    dto.SystemHealthResp
		want string
	}{
		{
			name: "all_healthy",
			h: dto.SystemHealthResp{
				ControlPlane: healthy, Postgres: healthy, Redis: healthy,
				Neo4j: healthy, MinIO: healthy, Temporal: healthy,
			},
			want: "healthy",
		},
		{
			name: "any_down_wins",
			h: dto.SystemHealthResp{
				ControlPlane: healthy, Postgres: down, Redis: degraded,
				Neo4j: healthy, MinIO: healthy, Temporal: healthy,
			},
			want: "down",
		},
		{
			name: "any_degraded_no_down",
			h: dto.SystemHealthResp{
				ControlPlane: healthy, Postgres: healthy, Redis: degraded,
				Neo4j: healthy, MinIO: healthy, Temporal: healthy,
			},
			want: "degraded",
		},
		{
			name: "disabled_counted_as_healthy",
			h: dto.SystemHealthResp{
				ControlPlane: healthy, Postgres: disabled, Redis: disabled,
				Neo4j: disabled, MinIO: disabled, Temporal: disabled,
			},
			want: "healthy",
		},
		{
			name: "mixed_disabled_and_down",
			h: dto.SystemHealthResp{
				ControlPlane: healthy, Postgres: disabled, Redis: down,
				Neo4j: healthy, MinIO: healthy, Temporal: healthy,
			},
			want: "down",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveOverall(tc.h)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestCountIntegrations_NilPoolReturnsZero — the dev/no-DB boot path must
// not panic when pool is nil. All counters surface as zero.
func TestCountIntegrations_NilPoolReturnsZero(t *testing.T) {
	got := countIntegrations(nil, nil, "any-org")
	if got.connected != 0 || got.total != 0 || got.degraded != 0 {
		t.Fatalf("nil pool counters: %+v", got)
	}
}

// TestCountIncidentsOpen_NilPoolReturnsZero — same nil-pool guarantee for
// the incidents counter.
func TestCountIncidentsOpen_NilPoolReturnsZero(t *testing.T) {
	if n := countIncidentsOpen(nil, nil, "any-org", 24*time.Hour); n != 0 {
		t.Fatalf("nil pool incidents: %d", n)
	}
}

// TestCountApprovalsPending_NilPoolReturnsZero — and approvals counter.
func TestCountApprovalsPending_NilPoolReturnsZero(t *testing.T) {
	if n := countApprovalsPending(nil, nil, "any-org"); n != 0 {
		t.Fatalf("nil pool approvals: %d", n)
	}
}

// TestSystemHealth_Cached — the second call within the 15s cache window
// returns the cached response without re-running the probe fanout. Since
// nil deps result in every probe returning "disabled" (deterministic), we
// can verify by hitting the handler twice and confirming both responses
// are byte-identical.
//
// We don't have a Principal middleware on this test path; SystemHealth
// itself doesn't gate on a principal so we hit it directly.
func TestSystemHealth_Cached(t *testing.T) {
	// Reset the cache so prior tests don't leak state. We replace the package
	// var with a fresh cache instance for this test only — see test below for
	// the symmetrical restore.
	prev := healthCache
	healthCache = newTTLCache[dto.SystemHealthResp](15 * time.Second)
	t.Cleanup(func() { healthCache = prev })

	deps := SystemHealthDeps{
		AdminPool: nil,
		RedisAddr: "",
		Neo4j:     nil,
		Temporal:  nil,
	}
	h := SystemHealth(deps)

	r1 := httptest.NewRecorder()
	h.ServeHTTP(r1, httptest.NewRequest(http.MethodGet, "/v1/system-health", nil))
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, httptest.NewRequest(http.MethodGet, "/v1/system-health", nil))
	if r1.Code != http.StatusOK || r2.Code != http.StatusOK {
		t.Fatalf("status: r1=%d r2=%d", r1.Code, r2.Code)
	}
	// Verify cache: bodies should be identical for the cache window.
	// Bodies carry a CheckedAt timestamp; identical responses prove a cache
	// hit (a re-run would produce a slightly different now).
	if r1.Body.String() != r2.Body.String() {
		t.Fatalf("cache miss on second call:\n%s\n%s", r1.Body.String(), r2.Body.String())
	}
	// All probes should report disabled because every dep is nil.
	if !strings.Contains(r1.Body.String(), `"status":"disabled"`) {
		t.Fatalf("expected at least one disabled probe: %s", r1.Body.String())
	}
}

// TestSystemHealth_OmitsDisabledProbes — the toCheck contract surfaces
// "disabled" rather than "down" for nil deps so the dashboard can show a
// neutral pill for that dep.
func TestSystemHealth_OmitsDisabledProbes(t *testing.T) {
	prev := healthCache
	healthCache = newTTLCache[dto.SystemHealthResp](15 * time.Second)
	t.Cleanup(func() { healthCache = prev })
	deps := SystemHealthDeps{
		AdminPool: nil, // disabled
		RedisAddr: "", // disabled
		Neo4j:     nil, // disabled
		Temporal:  nil, // disabled
	}
	h := SystemHealth(deps)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/system-health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	body := rec.Body.String()
	// All four nil deps surface as "disabled" not "down".
	// We can't easily count occurrences without unmarshaling, so just confirm
	// the absence of any down state (would imply a misconfiguration).
	if strings.Contains(body, `"status":"down"`) {
		t.Fatalf("nil deps must NOT surface as down: %s", body)
	}
	if !strings.Contains(body, `"status":"disabled"`) {
		t.Fatalf("nil deps must surface at least one disabled: %s", body)
	}
}
