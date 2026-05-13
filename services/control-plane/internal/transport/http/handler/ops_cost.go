package handler

// Per-org cost rollup — GET /v1/orgs/{org_id}/cost.
//
// Reads token_ledger (Phase 5) for the caller's org and aggregates three
// ways: MTD totals, by-agent_role, and by-day. Cache hit rate is the
// (cached_tokens / tokens_in) ratio across the same MTD window.
//
// The endpoint is org-scoped by the principal (the URL {org_id} is ignored
// by design — see ops_activity.go for the rationale).

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// OrgCost wires GET /v1/orgs/{org_id}/cost. Runs four queries against
// token_ledger: aggregate totals, cache-hit numerator/denominator, by-agent
// rollup, and by-day rollup. All four are MTD-bounded.
//
// Admin pool — the operator endpoint authenticates via session middleware
// and the principal's OrgID is the tenancy boundary at the SQL layer.
func OrgCost(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if pool == nil {
			writeJSON(w, http.StatusOK, dto.OrgCostResp{
				ByAgentRole: []dto.CostByAgentRole{},
				ByDay:       []dto.CostByDay{},
			})
			return
		}
		// MTD lower bound — first day of the current month, UTC.
		now := time.Now().UTC()
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

		var resp dto.OrgCostResp
		resp.ByAgentRole = []dto.CostByAgentRole{}
		resp.ByDay = []dto.CostByDay{}

		// Aggregate totals + cache numerator.
		var cachedTokens int64
		err := pool.QueryRow(r.Context(), `
            SELECT COALESCE(SUM(cost_cents)::float8, 0),
                   COALESCE(SUM(tokens_in), 0),
                   COALESCE(SUM(tokens_out), 0),
                   COALESCE(SUM(cached_tokens), 0)
            FROM token_ledger
            WHERE org_id=$1 AND recorded_at >= $2`,
			princ.OrgID, monthStart,
		).Scan(&resp.MTDTotalCentsExact, &resp.TokensIn, &resp.TokensOut, &cachedTokens)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if resp.TokensIn > 0 {
			resp.CacheHitRate = float64(cachedTokens) / float64(resp.TokensIn)
		}

		// By agent_role.
		rows, err := pool.Query(r.Context(), `
            SELECT agent,
                   COALESCE(SUM(tokens_in), 0),
                   COALESCE(SUM(tokens_out), 0),
                   COALESCE(SUM(cost_cents)::float8, 0)
            FROM token_ledger
            WHERE org_id=$1 AND recorded_at >= $2
            GROUP BY agent
            ORDER BY SUM(cost_cents) DESC`,
			princ.OrgID, monthStart)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		for rows.Next() {
			var row dto.CostByAgentRole
			if err := rows.Scan(&row.AgentRole, &row.TokensIn, &row.TokensOut, &row.CostCents); err != nil {
				rows.Close()
				httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			resp.ByAgentRole = append(resp.ByAgentRole, row)
		}
		rows.Close()

		// By day (UTC date_trunc).
		dayRows, err := pool.Query(r.Context(), `
            SELECT date_trunc('day', recorded_at)::date::text AS day,
                   COALESCE(SUM(cost_cents)::float8, 0),
                   COALESCE(SUM(tokens_in + tokens_out), 0)
            FROM token_ledger
            WHERE org_id=$1 AND recorded_at >= $2
            GROUP BY 1
            ORDER BY 1 ASC`,
			princ.OrgID, monthStart)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer dayRows.Close()
		for dayRows.Next() {
			var row dto.CostByDay
			if err := dayRows.Scan(&row.Day, &row.CostCents, &row.Tokens); err != nil {
				httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			resp.ByDay = append(resp.ByDay, row)
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
