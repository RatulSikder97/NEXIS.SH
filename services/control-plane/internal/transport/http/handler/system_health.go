package handler

// System-health surface — one fanout endpoint that probes every wired
// dependency (Postgres, Redis, Neo4j, MinIO, Temporal) plus control-plane
// itself, and a smaller sidebar-pill endpoint that summarises the same
// data along with integration / incidents / approvals counters.
//
// Both endpoints cache aggressively (15s for system-health, 30s for
// system-status) because the dashboards poll them on a banner-level
// interval — we don't want the probe fanout to run on every keystroke.

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.temporal.io/sdk/client"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
)

// SystemHealthDeps bundles every reachability handle the probe needs. nil
// fields are tolerated — the corresponding check reports "disabled" so the
// operator UI can render the dependency in a neutral colour.
type SystemHealthDeps struct {
	AdminPool *pgxpool.Pool
	RedisAddr string // host:port; empty disables the redis probe
	Neo4j     neo4j.DriverWithContext
	MinIO     integration.MinIOConfig
	Temporal  client.Client
}

// probeTimeout caps every individual probe so a slow dependency cannot
// stall the whole response. The handler fans out probes in parallel so the
// caller sees roughly max(latencies) rather than sum.
const probeTimeout = 3 * time.Second

// degradedThreshold is the latency at which a successful probe is still
// flagged degraded so the operator sees the dependency cooling off before
// it actually fails.
const degradedThreshold = 500 * time.Millisecond

// runHealthProbe fans out all dependency probes and returns the assembled
// response. Used by both /v1/system-health (full result) and
// /v1/system-status (which compresses the result into pill-shape).
func runHealthProbe(ctx context.Context, deps SystemHealthDeps) dto.SystemHealthResp {
	now := time.Now().UTC().Format(time.RFC3339)
	resp := dto.SystemHealthResp{
		ControlPlane: dto.SystemHealthCheck{
			Status: "healthy", LatencyMs: 0, CheckedAt: now,
		},
	}

	var (
		wg      sync.WaitGroup
		pgRes   integration.ProbeResult
		rdRes   integration.ProbeResult
		neoRes  integration.ProbeResult
		minRes  integration.ProbeResult
		tempRes integration.ProbeResult
	)
	timed := func(probe func(ctx context.Context) integration.ProbeResult, out *integration.ProbeResult) {
		defer wg.Done()
		cctx, cancel := context.WithTimeout(ctx, probeTimeout)
		defer cancel()
		*out = probe(cctx)
	}
	wg.Add(5)
	go timed(func(ctx context.Context) integration.ProbeResult {
		return integration.ProbePostgres(ctx, deps.AdminPool)
	}, &pgRes)
	go timed(func(ctx context.Context) integration.ProbeResult { return integration.ProbeRedis(ctx, deps.RedisAddr) }, &rdRes)
	go timed(func(ctx context.Context) integration.ProbeResult { return integration.ProbeNeo4j(ctx, deps.Neo4j) }, &neoRes)
	go timed(func(ctx context.Context) integration.ProbeResult { return integration.ProbeMinIO(ctx, deps.MinIO) }, &minRes)
	go timed(func(ctx context.Context) integration.ProbeResult {
		return integration.ProbeTemporal(ctx, deps.Temporal)
	}, &tempRes)
	wg.Wait()

	resp.Postgres = toCheck(pgRes, now)
	resp.Redis = toCheck(rdRes, now)
	resp.Neo4j = toCheck(neoRes, now)
	resp.MinIO = toCheck(minRes, now)
	resp.Temporal = toCheck(tempRes, now)
	return resp
}

// toCheck folds a ProbeResult into a wire-shape check. The "disabled"
// status surfaces dependencies that were intentionally not wired
// (e.g. neo4j absent on the dev compose) without being conflated with a
// degraded dependency.
func toCheck(r integration.ProbeResult, checkedAt string) dto.SystemHealthCheck {
	c := dto.SystemHealthCheck{
		LatencyMs: r.Latency.Milliseconds(),
		CheckedAt: checkedAt,
	}
	if r.Err != nil {
		if r.Err.Error() == "disabled" {
			c.Status = "disabled"
			c.LastError = ""
			return c
		}
		c.Status = "down"
		c.LastError = r.Err.Error()
		return c
	}
	c.Status = r.HealthyWithin(degradedThreshold)
	return c
}

// healthCache caches the full SystemHealthResp for 15s.
var healthCache = newTTLCache[dto.SystemHealthResp](15 * time.Second)

// SystemHealth wires GET /v1/system-health. The fanout runs at most once
// per cache window even under bursty polling — see healthCache above.
//
// Auth: any authenticated principal. Routes that mount this handler should
// place it inside the RequireAuth group; the spec calls for the public+session
// guarantee (i.e. session-required, no role).
func SystemHealth(deps SystemHealthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cached, ok := healthCache.Get("system-health"); ok {
			writeJSON(w, http.StatusOK, cached)
			return
		}
		resp := runHealthProbe(r.Context(), deps)
		healthCache.Set("system-health", resp)
		writeJSON(w, http.StatusOK, resp)
	}
}
