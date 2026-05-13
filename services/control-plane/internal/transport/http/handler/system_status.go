package handler

// Sidebar status pill — GET /v1/system-status.
//
// Compresses the dependency probe + integration / incidents / approvals
// counters into a single ~6-field response so the sidebar component can
// render an overall colour + a few badges without a fanout of its own.
//
// 30s in-memory cache per org keyed on the principal's OrgID. The
// dependency probe inside reuses the 15s system-health cache so the
// pill never triggers more probe work than the health page would.

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// SystemStatusDeps bundles the read paths the pill needs. AdminPool is
// optional — when nil the counters surface as 0 instead of 500ing.
type SystemStatusDeps struct {
	AdminPool *pgxpool.Pool
	Health    SystemHealthDeps
}

// statusCache caches the response keyed by orgID for 30s. Different orgs
// see different counters, so keying on the org is correct (a single global
// entry would leak counters across tenants).
var statusCache = newTTLCache[dto.SystemStatusResp](30 * time.Second)

// incidentsOpenWindow is the trailing window we treat as "open incidents"
// for the pill. The product UI surfaces this as "unresolved last 24h".
const incidentsOpenWindow = 24 * time.Hour

// SystemStatus wires GET /v1/system-status.
func SystemStatus(deps SystemStatusDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if cached, ok := statusCache.Get(princ.OrgID); ok {
			writeJSON(w, http.StatusOK, cached)
			return
		}

		// Health probe (cached for 15s).
		var health dto.SystemHealthResp
		if cached, ok := healthCache.Get("system-health"); ok {
			health = cached
		} else {
			health = runHealthProbe(r.Context(), deps.Health)
			healthCache.Set("system-health", health)
		}

		integ := countIntegrations(r.Context(), deps.AdminPool, princ.OrgID)
		incidentsOpen := countIncidentsOpen(r.Context(), deps.AdminPool, princ.OrgID, incidentsOpenWindow)
		approvalsPending := countApprovalsPending(r.Context(), deps.AdminPool, princ.OrgID)

		resp := dto.SystemStatusResp{
			Overall:               deriveOverall(health),
			IntegrationsConnected: integ.connected,
			IntegrationsTotal:     integ.total,
			IntegrationsDegraded:  integ.degraded,
			IncidentsOpen:         incidentsOpen,
			ApprovalsPending:      approvalsPending,
		}
		statusCache.Set(princ.OrgID, resp)
		writeJSON(w, http.StatusOK, resp)
	}
}

// deriveOverall reduces the SystemHealthResp into a tri-state for the pill.
// Any "down" → "down"; any "degraded" → "degraded"; all "healthy" /
// "disabled" → "healthy". Disabled deps don't count against the pill
// because they reflect deployment shape, not a fault.
func deriveOverall(h dto.SystemHealthResp) string {
	checks := []dto.SystemHealthCheck{
		h.ControlPlane, h.Postgres, h.Redis, h.Neo4j, h.MinIO, h.Temporal,
	}
	worst := "healthy"
	for _, c := range checks {
		switch c.Status {
		case "down":
			return "down"
		case "degraded":
			worst = "degraded"
		}
	}
	return worst
}

// integrationsCounters holds the rolled-up integration counts for the pill.
type integrationsCounters struct {
	connected int
	total     int
	degraded  int
}

// countIntegrations queries the integrations table for the caller's org and
// returns the (connected, total, degraded) tuple. A nil pool returns
// zero-valued counters so the dev path keeps rendering.
func countIntegrations(ctx context.Context, pool *pgxpool.Pool, orgID string) integrationsCounters {
	var out integrationsCounters
	if pool == nil {
		return out
	}
	// Single roundtrip — counts by status using FILTER.
	_ = pool.QueryRow(ctx, `
        SELECT count(*),
               count(*) FILTER (WHERE status='connected'),
               count(*) FILTER (WHERE status='error' OR status='pending')
        FROM integrations
        WHERE org_id=$1`,
		orgID,
	).Scan(&out.total, &out.connected, &out.degraded)
	return out
}

// countIncidentsOpen returns the recent incident count for the org. We
// approximate "open" as "received within the trailing window" because the
// incidents_raw table doesn't carry an explicit resolved_at column today.
func countIncidentsOpen(ctx context.Context, pool *pgxpool.Pool, orgID string, window time.Duration) int {
	if pool == nil {
		return 0
	}
	secs := int(window.Seconds())
	if secs < 1 {
		secs = 1
	}
	var n int
	_ = pool.QueryRow(ctx, `
        SELECT count(*) FROM incidents_raw
        WHERE org_id=$1 AND received_at > now() - make_interval(secs => $2)`,
		orgID, secs,
	).Scan(&n)
	return n
}

// countApprovalsPending returns the count of approval_decisions rows in
// the pending state for the org.
func countApprovalsPending(ctx context.Context, pool *pgxpool.Pool, orgID string) int {
	if pool == nil {
		return 0
	}
	var n int
	_ = pool.QueryRow(ctx,
		`SELECT count(*) FROM approval_decisions WHERE org_id=$1 AND decision='pending'`,
		orgID,
	).Scan(&n)
	return n
}
