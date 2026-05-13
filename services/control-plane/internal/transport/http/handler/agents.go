package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// AgentInfo is one row of the /v1/workspaces/{ws}/agents response. The shape
// mirrors what the Phase 5+6 /console/agents page renders: who the agent is,
// what layer it belongs to, and per-org observability rolled up from
// activity_events + token_ledger over the last 7 days.
type AgentInfo struct {
	Name           string  `json:"name"`             // canonical AgentName (snake_case)
	Label          string  `json:"label"`            // human-readable
	Layer          string  `json:"layer"`            // "l1" | "l2" | "router" | "detector"
	Description    string  `json:"description"`
	Status         string  `json:"status"`           // "available" | "degraded" | "disabled"
	RecentRuns     int     `json:"recent_runs"`      // last 7 days
	LastSeenAt     string  `json:"last_seen_at,omitempty"`
	TotalTokensIn  int     `json:"total_tokens_in"`  // last 7 days, sum
	TotalTokensOut int     `json:"total_tokens_out"`
	TotalCostCents float64 `json:"total_cost_cents_exact"`
	P50DurationMs  int64   `json:"p50_duration_ms"`
	P95DurationMs  int64   `json:"p95_duration_ms"`
}

// agentCatalog is the fleet description. Status starts "available"; the
// handler downgrades to "degraded" when recent finishes have payload.degraded.
var agentCatalog = []AgentInfo{
	// L1
	{Name: "architect", Label: "Architect", Layer: "l1",
		Description: "Solution plan against the synthesised contracts. Picks which L1 specialists to dispatch."},
	{Name: "backend", Label: "Backend", Layer: "l1",
		Description: "Backend patch synthesis. Reads the codegraph + retrieval context, emits a unified diff."},
	{Name: "qa", Label: "QA", Layer: "l1",
		Description: "Unit + property test generation against the synthesised patch."},
	{Name: "devops", Label: "DevOps", Layer: "l1",
		Description: "Pipeline + CI/CD config changes (ArgoCD / GitHub Actions YAML)."},
	{Name: "data_engineer", Label: "Data Engineer", Layer: "l1",
		Description: "Schema migrations + data backfill plans."},

	// L2
	{Name: "sentinel", Label: "Sentinel", Layer: "detector",
		Description: "Streaming anomaly detector watching Sentry + OTel. Lives outside the workflow; emits IncidentDetected."},
	{Name: "pathfinder", Label: "Pathfinder", Layer: "l2",
		Description: "Causal RCA via the Neo4j codegraph + DoWhy estimands. Returns a root-cause hypothesis."},
	{Name: "synthesiser", Label: "Synthesiser", Layer: "l2",
		Description: "Picks the L1 fleet given root-cause + scenario. Routes the recovery plan."},
	{Name: "validator_l2", Label: "Validator (L2)", Layer: "l2",
		Description: "Sandbox + Hypothesis property-based tests. Confirms the patch doesn't regress invariants."},

	// Router
	{Name: "approval_gate", Label: "Approval Gate", Layer: "router",
		Description: "Severity router. Low → auto-merge, medium → 2-min countdown, high/unknown → human required."},
}

// AgentsList returns the fleet + per-org observability rolled up from
// activity_events + token_ledger. Read-only, any-auth route.
func AgentsList(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		since := time.Now().Add(-7 * 24 * time.Hour).UTC()
		stats := loadAgentStats(r.Context(), pool, princ.OrgID, since)

		out := make([]AgentInfo, len(agentCatalog))
		for i, a := range agentCatalog {
			a.Status = "available"
			if s, ok := stats[a.Name]; ok {
				a.RecentRuns = s.recentRuns
				a.TotalTokensIn = s.tokensIn
				a.TotalTokensOut = s.tokensOut
				a.TotalCostCents = s.costCents
				a.P50DurationMs = s.p50ms
				a.P95DurationMs = s.p95ms
				if !s.lastSeen.IsZero() {
					a.LastSeenAt = s.lastSeen.UTC().Format(time.RFC3339)
				}
				if s.recentDegraded > 0 {
					a.Status = "degraded"
				}
			}
			out[i] = a
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

type agentStat struct {
	recentRuns     int
	recentDegraded int
	tokensIn       int
	tokensOut      int
	costCents      float64
	p50ms          int64
	p95ms          int64
	lastSeen       time.Time
}

// loadAgentStats rolls up activity_events for the given org over the window.
// Uses the admin pool — there's no RLS principal in the cron-ish aggregate
// path. The org_id filter on the query is the tenancy gate.
func loadAgentStats(ctx context.Context, pool *pgxpool.Pool, orgID string, since time.Time) map[string]agentStat {
	out := map[string]agentStat{}
	if pool == nil {
		return out
	}

	// Recent runs + token totals from activity_events.
	// Each finish-frame carries the agent payload (tokens_in/tokens_out/cost_cents).
	rows, err := pool.Query(ctx, `
		SELECT agent_role,
		       COUNT(*) FILTER (WHERE kind='finish') AS finishes,
		       COUNT(*) FILTER (WHERE kind='finish' AND payload->>'degraded' = 'true') AS degraded,
		       COALESCE(SUM((payload->>'tokens_in')::int) FILTER (WHERE kind='finish'), 0) AS tokens_in,
		       COALESCE(SUM((payload->>'tokens_out')::int) FILTER (WHERE kind='finish'), 0) AS tokens_out,
		       COALESCE(SUM((payload->>'cost_cents')::numeric) FILTER (WHERE kind='finish'), 0) AS cost_cents,
		       MAX(ts) AS last_seen,
		       COALESCE(PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY (payload->>'duration_ms')::int) FILTER (WHERE kind='finish'), 0) AS p50,
		       COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY (payload->>'duration_ms')::int) FILTER (WHERE kind='finish'), 0) AS p95
		FROM activity_events ae
		JOIN workflow_runs wr ON wr.id = ae.workflow_run_id
		WHERE wr.org_id = $1 AND ae.ts >= $2
		GROUP BY agent_role`, orgID, since)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s agentStat
		var role string
		var p50, p95 float64
		if err := rows.Scan(&role, &s.recentRuns, &s.recentDegraded, &s.tokensIn, &s.tokensOut, &s.costCents, &s.lastSeen, &p50, &p95); err != nil {
			continue
		}
		s.p50ms = int64(p50)
		s.p95ms = int64(p95)
		out[role] = s
	}
	return out
}
