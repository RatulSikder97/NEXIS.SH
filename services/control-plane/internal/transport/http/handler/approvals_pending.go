package handler

// ApprovalsPending — list endpoint that powers the /console/approvals page
// + the sidebar "pending approvals" badge.
//
// Surface:
//
//	GET /v1/workspaces/{ws_id}/approvals/pending
//
// Owner|Admin only (mounted under the role-gated sub-group in server.go).
// Returns every approval_decisions row for the caller's org where
// decision='pending'. The {ws_id} URL param is honoured as a defensive
// filter so a workspace selector can scope the badge per-workspace —
// when present, only rows whose workspace_id matches are returned.
//
// Wire shape mirrors apps/web/lib/approvals.ts:PendingApproval:
//
//	{
//	  run_id, workspace_id, scenario, severity,
//	  awaiting_decision_since,
//	  agent_summaries: { Sentinel?, Pathfinder?, Synthesiser?, Validator? }
//	}
//
// agent_summaries is emitted as an empty object today — the workflow's
// per-agent confidence keys are not yet rolled up into the approval row,
// so we keep the shape stable while leaving room for a future enrichment
// pass (e.g. JOIN activity_events.payload->>'confidence').
//
// `awaiting_decision_since` is the approval row's created_at in RFC3339
// (matches the FE's relative-time renderer in /console/approvals).

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// PendingApprovalResp is the wire shape returned by
// GET /v1/workspaces/{ws_id}/approvals/pending. Aligned with the FE's
// PendingApproval type so the polling SDK in apps/web/lib/approvals.ts
// can render the table + sidebar badge without an SDK change.
type PendingApprovalResp struct {
	RunID                 string                 `json:"run_id"`
	WorkspaceID           string                 `json:"workspace_id"`
	Scenario              string                 `json:"scenario,omitempty"`
	Severity              string                 `json:"severity"`
	AwaitingDecisionSince string                 `json:"awaiting_decision_since"`
	AgentSummaries        map[string]interface{} `json:"agent_summaries"`
}

// ApprovalsPending wires GET /v1/workspaces/{ws_id}/approvals/pending. Reads
// every approval_decisions row for the caller's org where decision='pending'.
// The repo call is RLS-scoped (db.FromCtx) so cross-tenant rows never escape
// even if the SQL changes.
//
// Query params:
//
//	limit — 1..200, default 50 (matches ApprovalRepo.ListPending's own cap).
//
// Returns an always-array body (never null) so the polling SDK in
// apps/web/lib/approvals.ts can blindly call `.length`.
func ApprovalsPending(repo domain.ApprovalRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if repo == nil {
			// Defensive: if the route is mounted but no repo is wired
			// the polling client should see an empty array, not a 500.
			httpJSON(w, http.StatusOK, []PendingApprovalResp{})
			return
		}
		wsID := chi.URLParam(r, "ws_id")

		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				if n > 200 {
					n = 200
				}
				limit = n
			}
		}

		rows, err := repo.ListPending(r.Context(), princ.OrgID, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list failed")
			return
		}

		out := make([]PendingApprovalResp, 0, len(rows))
		for _, d := range rows {
			// Defence-in-depth tenancy filter + per-workspace narrowing.
			// The repo's WHERE already pins org_id, but the workspace
			// filter is enforced in the handler so the URL parameter
			// behaves as the FE expects.
			if d.OrgID != princ.OrgID {
				continue
			}
			if wsID != "" && d.WorkspaceID != wsID {
				continue
			}
			out = append(out, PendingApprovalResp{
				RunID:                 d.WorkflowRunID,
				WorkspaceID:           d.WorkspaceID,
				Scenario:              d.Scenario,
				Severity:              string(d.Severity),
				AwaitingDecisionSince: d.CreatedAt.UTC().Format(time.RFC3339),
				// agent_summaries is reserved for a future enrichment
				// pass that joins activity_events payloads. Keep the
				// key present + empty so the FE's optional-chain
				// access works without an SDK migration.
				AgentSummaries: map[string]interface{}{},
			})
		}
		httpJSON(w, http.StatusOK, out)
	}
}
