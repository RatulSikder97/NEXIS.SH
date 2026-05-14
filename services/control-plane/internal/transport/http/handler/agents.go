package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
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

// AgentRunSummary is one row of GET /v1/workspaces/{ws_id}/agents/{name}/runs.
// Each row aggregates every activity_events frame the named agent emitted
// inside a single workflow_run. The drill-down endpoint returns these
// summaries; the per-run events endpoint returns the raw frames.
type AgentRunSummary struct {
	RunID          string  `json:"run_id"`
	Scenario       string  `json:"scenario"`
	Severity       string  `json:"severity"`
	Status         string  `json:"status"` // succeeded | failed | running | degraded
	StartedAt      string  `json:"started_at"`
	FinishedAt     string  `json:"finished_at,omitempty"`
	DurationMs     int64   `json:"duration_ms"`
	TokensIn       int64   `json:"tokens_in"`
	TokensOut      int64   `json:"tokens_out"`
	CostCentsExact float64 `json:"cost_cents_exact"`
	EventCount     int     `json:"event_count"`
	Degraded       bool    `json:"degraded"`
	SummaryMessage string  `json:"summary_message,omitempty"`
}

// AgentRunsResp wraps the list response. `total` lets the dashboard paginate
// without re-issuing the COUNT(*) on every page.
type AgentRunsResp struct {
	Runs  []AgentRunSummary `json:"runs"`
	Total int               `json:"total"`
}

// AgentRunEventsResp wraps the per-run event timeline so the wire shape is
// `{events: [...]}` not a bare array — matches the rest of the v1 surface
// and leaves headroom for `total` / cursor fields when the timeline grows.
type AgentRunEventsResp struct {
	Events []dto.ActivityEventResp `json:"events"`
}

// validAgentName is the whitelist of agent_role values the drill-down route
// accepts. Keeps the URL impersonation-resistant + makes the SQL parameter
// safe even though it's already bound via $3.
var validAgentName = func() map[string]struct{} {
	m := map[string]struct{}{}
	for _, a := range agentCatalog {
		m[a.Name] = struct{}{}
	}
	return m
}()

// AgentRunsList wires GET /v1/workspaces/{ws_id}/agents/{name}/runs.
// Returns one row per workflow_run the named agent participated in, ordered
// by started_at DESC. The auth gate is the standard authenticated-principal
// check; the SQL filter (org_id + workspace_id) is the tenancy boundary.
//
// Pagination: `limit` (default 50, max 200) + `offset` (default 0). The
// total count lets clients render "page N of M" without an extra round-trip.
func AgentRunsList(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		wsID := chi.URLParam(r, "ws_id")
		name := chi.URLParam(r, "name")
		if wsID == "" || name == "" {
			writeError(w, http.StatusBadRequest, "workspace_id and name required")
			return
		}
		if _, ok := validAgentName[name]; !ok {
			writeError(w, http.StatusNotFound, "unknown agent")
			return
		}
		limit, offset := parseLimitOffset(r, 50, 200)

		runs, total, err := loadAgentRuns(r.Context(), pool, princ.OrgID, wsID, name, limit, offset)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		httpJSON(w, http.StatusOK, AgentRunsResp{Runs: runs, Total: total})
	}
}

// AgentRunEvents wires GET /v1/workspaces/{ws_id}/agents/{name}/runs/{run_id}/events.
// Returns every activity_events row for the given (workflow_run_id, agent_role),
// ordered by ts ASC. The full JSONB payload is surfaced so admins can inspect
// the input prompt, tool calls, decision rationale, and model output.
//
// Ownership is verified by joining workflow_runs ON workflow_run_id and
// filtering by (org_id, workspace_id, id) — any mismatch returns 404 so
// cross-tenant probing returns the same opaque response as a missing run.
func AgentRunEvents(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		wsID := chi.URLParam(r, "ws_id")
		name := chi.URLParam(r, "name")
		runID := chi.URLParam(r, "run_id")
		if wsID == "" || name == "" || runID == "" {
			writeError(w, http.StatusBadRequest, "workspace_id, name, run_id required")
			return
		}
		if _, ok := validAgentName[name]; !ok {
			writeError(w, http.StatusNotFound, "unknown agent")
			return
		}

		events, err := loadAgentRunEvents(r.Context(), pool, princ.OrgID, wsID, runID, name)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "run not found")
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		httpJSON(w, http.StatusOK, AgentRunEventsResp{Events: events})
	}
}

// parseLimitOffset is a small URL-query helper. Defaults: 50 / 0. Limit is
// clamped to [1, max]; offset clamps to >=0.
func parseLimitOffset(r *http.Request, defaultLimit, maxLimit int) (int, int) {
	limit := defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > maxLimit {
				n = maxLimit
			}
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

// loadAgentRuns aggregates activity_events into one row per workflow_run for
// the named agent in (org_id, workspace_id). Returns the page + total count.
// Uses the admin pool to match loadAgentStats — the (org_id, workspace_id)
// filter is the tenancy boundary.
//
// SQL shape:
//
//   - Inner CTE rolls the events into per-(run, agent) aggregates.
//   - Outer query joins with workflow_runs for status + scenario + severity
//     fallback (scenario lives in wr.input JSON; severity is sniffed from
//     the approval_gate finish frame payload when present).
//   - Status comes from the agent's frames first: any 'failed' row →
//     "failed"; any 'started' without matching 'succeeded' → "running";
//     any degraded finish → "degraded"; otherwise "succeeded".
func loadAgentRuns(ctx context.Context, pool *pgxpool.Pool, orgID, workspaceID, name string, limit, offset int) ([]AgentRunSummary, int, error) {
	if pool == nil {
		return []AgentRunSummary{}, 0, nil
	}

	// Total count (unfiltered by limit/offset) so clients can paginate.
	var total int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT ae.workflow_run_id)
		FROM activity_events ae
		JOIN workflow_runs wr ON wr.id = ae.workflow_run_id
		WHERE wr.org_id=$1 AND wr.workspace_id=$2 AND ae.agent_role=$3`,
		orgID, workspaceID, name).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []AgentRunSummary{}, 0, nil
	}

	rows, err := pool.Query(ctx, `
		WITH agent_rollup AS (
		  SELECT
		    ae.workflow_run_id                                         AS run_id,
		    MIN(ae.ts)                                                 AS started_at,
		    MAX(ae.ts) FILTER (WHERE ae.status='succeeded' OR ae.status='failed' OR ae.status='timed_out') AS finished_at,
		    COUNT(*)                                                   AS event_count,
		    BOOL_OR(ae.status='failed' OR ae.status='timed_out')       AS any_failed,
		    BOOL_OR(ae.status='started')                               AS any_started,
		    BOOL_OR(ae.status='succeeded')                             AS any_succeeded,
		    BOOL_OR(COALESCE((ae.payload->>'degraded')::boolean,false)
		            AND ae.status='succeeded')                         AS any_degraded,
		    COALESCE(SUM((ae.payload->>'tokens_in')::bigint)
		             FILTER (WHERE ae.status='succeeded'), 0)          AS tokens_in,
		    COALESCE(SUM((ae.payload->>'tokens_out')::bigint)
		             FILTER (WHERE ae.status='succeeded'), 0)          AS tokens_out,
		    COALESCE(SUM((ae.payload->>'cost_cents')::numeric)
		             FILTER (WHERE ae.status='succeeded'), 0)          AS cost_cents,
		    COALESCE(MAX((ae.payload->>'duration_ms')::bigint)
		             FILTER (WHERE ae.status='succeeded'), 0)          AS duration_ms,
		    (ARRAY_AGG(ae.message ORDER BY ae.ts DESC)
		      FILTER (WHERE ae.status='succeeded' OR ae.status='failed'))[1]  AS summary_message
		  FROM activity_events ae
		  JOIN workflow_runs wr ON wr.id = ae.workflow_run_id
		  WHERE wr.org_id=$1 AND wr.workspace_id=$2 AND ae.agent_role=$3
		  GROUP BY ae.workflow_run_id
		),
		scenario AS (
		  SELECT id AS run_id,
		         COALESCE(input->>'scenario', '')                      AS scenario
		  FROM workflow_runs
		),
		severity AS (
		  SELECT workflow_run_id AS run_id,
		         (payload->>'severity')                                AS severity
		  FROM activity_events
		  WHERE agent_role='approval_gate' AND status='succeeded'
		)
		SELECT
		  ar.run_id::text, ar.started_at, ar.finished_at, ar.event_count,
		  ar.any_failed, ar.any_started, ar.any_succeeded, ar.any_degraded,
		  ar.tokens_in, ar.tokens_out, ar.cost_cents, ar.duration_ms,
		  COALESCE(ar.summary_message, ''),
		  COALESCE(s.scenario, ''),
		  COALESCE(sv.severity, '')
		FROM agent_rollup ar
		LEFT JOIN scenario s  ON s.run_id  = ar.run_id
		LEFT JOIN severity sv ON sv.run_id = ar.run_id
		ORDER BY ar.started_at DESC
		LIMIT $4 OFFSET $5`, orgID, workspaceID, name, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]AgentRunSummary, 0, limit)
	for rows.Next() {
		var (
			runID                                            string
			startedAt                                        time.Time
			finishedAt                                       *time.Time
			eventCount                                       int
			anyFailed, anyStarted, anySucceeded, anyDegraded bool
			tokensIn, tokensOut                              int64
			costCents                                        float64
			durationMs                                       int64
			summaryMessage, scenario, severity               string
		)
		if err := rows.Scan(
			&runID, &startedAt, &finishedAt, &eventCount,
			&anyFailed, &anyStarted, &anySucceeded, &anyDegraded,
			&tokensIn, &tokensOut, &costCents, &durationMs,
			&summaryMessage, &scenario, &severity,
		); err != nil {
			return nil, 0, err
		}
		row := AgentRunSummary{
			RunID:          runID,
			Scenario:       scenario,
			Severity:       severity,
			Status:         deriveAgentRunStatus(anyFailed, anyStarted, anySucceeded, anyDegraded),
			StartedAt:      startedAt.UTC().Format(time.RFC3339Nano),
			DurationMs:     durationMs,
			TokensIn:       tokensIn,
			TokensOut:      tokensOut,
			CostCentsExact: costCents,
			EventCount:     eventCount,
			Degraded:       anyDegraded,
			SummaryMessage: summaryMessage,
		}
		if finishedAt != nil {
			row.FinishedAt = finishedAt.UTC().Format(time.RFC3339Nano)
			// Fallback: when the agent finish frame didn't carry a
			// duration_ms (older runs / stub fallbacks pre-enrichment),
			// compute it from started_at → finished_at.
			if row.DurationMs == 0 {
				row.DurationMs = finishedAt.Sub(startedAt).Milliseconds()
			}
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// deriveAgentRunStatus maps the boolean rollup of activity_events.status
// into one of succeeded | failed | running | degraded.
//
//   - failed     → any frame with status='failed' or 'timed_out'
//   - running    → started without a succeeded counterpart (and not failed)
//   - degraded   → succeeded but with payload.degraded=true
//   - succeeded  → otherwise
//
// Order matters: failed > running > degraded > succeeded.
func deriveAgentRunStatus(anyFailed, anyStarted, anySucceeded, anyDegraded bool) string {
	switch {
	case anyFailed:
		return "failed"
	case anyStarted && !anySucceeded:
		return "running"
	case anyDegraded:
		return "degraded"
	default:
		return "succeeded"
	}
}

// loadAgentRunEvents returns every activity_event for the given
// (workflow_run_id, agent_role) pair, ordered by ts ASC. The principal's
// (org_id, workspace_id) is verified before the events are read so
// cross-tenant access returns ErrNotFound.
func loadAgentRunEvents(ctx context.Context, pool *pgxpool.Pool, orgID, workspaceID, runID, name string) ([]dto.ActivityEventResp, error) {
	if pool == nil {
		return []dto.ActivityEventResp{}, nil
	}

	// Verify the run belongs to (org_id, workspace_id) before we surface
	// any rows from it.
	var ownerOrg, ownerWs string
	err := pool.QueryRow(ctx,
		`SELECT org_id::text, workspace_id::text FROM workflow_runs WHERE id=$1`,
		runID,
	).Scan(&ownerOrg, &ownerWs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if ownerOrg != orgID || ownerWs != workspaceID {
		return nil, domain.ErrNotFound
	}

	rows, err := pool.Query(ctx, `
		SELECT workflow_run_id::text, seq, agent_role, activity_name,
		       status, attempt, COALESCE(message,''), payload, ts
		FROM activity_events
		WHERE workflow_run_id=$1 AND agent_role=$2
		ORDER BY ts ASC, seq ASC`, runID, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]dto.ActivityEventResp, 0, 16)
	for rows.Next() {
		var (
			wrID, role, activityName, status, message string
			seq, attempt                              int
			payload                                   []byte
			ts                                        time.Time
		)
		if err := rows.Scan(&wrID, &seq, &role, &activityName, &status, &attempt, &message, &payload, &ts); err != nil {
			return nil, err
		}
		resp := dto.ActivityEventResp{
			WorkflowRunID: wrID,
			Seq:           seq,
			AgentRole:     role,
			ActivityName:  activityName,
			Status:        status,
			Attempt:       attempt,
			Message:       message,
			TS:            ts.UTC().Format(time.RFC3339Nano),
		}
		if len(payload) > 0 {
			// Truncate huge payloads (model output >8KB) so the admin UI
			// stays responsive. We never drop tool_calls / input_summary —
			// only the raw model body, which already has output_summary
			// alongside it.
			var raw map[string]any
			if err := json.Unmarshal(payload, &raw); err == nil {
				truncateLargeOutput(raw, 8*1024)
				resp.Payload = raw
			}
		}
		out = append(out, resp)
	}
	return out, rows.Err()
}

// truncateLargeOutput shrinks oversize string fields in the activity payload
// so the admin UI never has to render an 80KB hex blob. We only touch raw
// content keys ("content", "raw_output") — structured + tool_calls + summary
// fields stay intact.
func truncateLargeOutput(payload map[string]any, maxBytes int) {
	rawKeys := []string{"content", "raw_output", "model_output", "raw_content", "system_prompt", "user_prompt"}
	for _, k := range rawKeys {
		v, ok := payload[k]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		if len(s) > maxBytes {
			payload[k] = s[:maxBytes] + "... [truncated]"
			payload[k+"_truncated"] = true
			payload[k+"_original_bytes"] = len(s)
		}
	}
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
	// Note: activity_events has a `status` column (started|succeeded|failed|
	// timed_out), not `kind`. A "finish" frame is any row with
	// status='succeeded' — the same predicate loadAgentRuns uses.
	rows, err := pool.Query(ctx, `
		SELECT agent_role,
		       COUNT(*) FILTER (WHERE status='succeeded') AS finishes,
		       COUNT(*) FILTER (WHERE status='succeeded' AND payload->>'degraded' = 'true') AS degraded,
		       COALESCE(SUM((payload->>'tokens_in')::int) FILTER (WHERE status='succeeded'), 0) AS tokens_in,
		       COALESCE(SUM((payload->>'tokens_out')::int) FILTER (WHERE status='succeeded'), 0) AS tokens_out,
		       COALESCE(SUM((payload->>'cost_cents')::numeric) FILTER (WHERE status='succeeded'), 0) AS cost_cents,
		       MAX(ts) AS last_seen,
		       COALESCE(PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY (payload->>'duration_ms')::int) FILTER (WHERE status='succeeded'), 0) AS p50,
		       COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY (payload->>'duration_ms')::int) FILTER (WHERE status='succeeded'), 0) AS p95
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
