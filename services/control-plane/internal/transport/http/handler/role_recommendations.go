// Package handler — intelligent role recommendation surface (Phase 9, FYP
// "Role Management & Access Control (Intelligent)").
//
// Four owner|admin-gated endpoints under /v1/orgs/{id}:
//
//	GET  /v1/orgs/{id}/members                                — real member list (email, role, login recency)
//	GET  /v1/orgs/{id}/role-recommendations                   — pending recommendations with rationale
//	POST /v1/orgs/{id}/role-recommendations/refresh           — run the analyser on demand for this org
//	POST /v1/orgs/{id}/role-recommendations/{rec_id}/decide   — {action: accept|dismiss}
//
// Accept applies the role change (guarded UPDATE on org_members inside the
// request's RLS tx) and stamps decided_by/decided_at; both outcomes are
// written to the audit log so the FYP's "full audit visibility" holds. The
// {id} URL param must match the caller's org — same defensive pattern as the
// invites surface.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
	"github.com/nexis-eco/nexis/services/control-plane/internal/usecase/rolerecommend"
)

// RoleRecommendationService is the narrow port these handlers need from the
// recommender store. *rolerecommend.Store satisfies it; tests can fake it.
type RoleRecommendationService interface {
	ListPending(ctx context.Context, orgID string) ([]rolerecommend.Recommendation, error)
	Refresh(ctx context.Context, orgID string) (int, error)
	Decide(ctx context.Context, orgID, recID string, accept bool, deciderUserID string) (rolerecommend.Recommendation, error)
	ListMembers(ctx context.Context, orgID string) ([]rolerecommend.Member, error)
}

// compile-time conformance: the production store implements the port.
var _ RoleRecommendationService = (*rolerecommend.Store)(nil)

// recToWire converts one store row to the JSON shape shared with
// apps/web/lib/roleRecommendations.ts.
func recToWire(r rolerecommend.Recommendation) map[string]any {
	return map[string]any{
		"id":               r.ID,
		"user_id":          r.UserID,
		"email":            r.Email,
		"current_role":     string(r.CurrentRole),
		"recommended_role": string(r.RecommendedRole),
		"rule":             r.Rule,
		"rationale":        r.Rationale,
		"status":           r.Status,
		"created_at":       r.CreatedAt.UTC().Format(time.RFC3339),
		"decided_at":       nullableTimeStr(r.DecidedAt),
		"decided_by":       r.DecidedBy,
	}
}

// orgFromRequest returns the principal + validates the {id} URL param pins
// the caller's own org. Shared by all four handlers in this file.
func orgFromRequest(w http.ResponseWriter, r *http.Request) (domain.Principal, bool) {
	princ, ok := appmw.PrincipalFrom(r.Context())
	if !ok {
		httpJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return domain.Principal{}, false
	}
	if chi.URLParam(r, "id") != princ.OrgID {
		httpJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return domain.Principal{}, false
	}
	return princ, true
}

// OrgMembersList wires GET /v1/orgs/{id}/members. Returns every org member
// with email, role, join date and last-login recency (always-array body).
func OrgMembersList(svc RoleRecommendationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := orgFromRequest(w, r)
		if !ok {
			return
		}
		rows, err := svc.ListMembers(r.Context(), princ.OrgID)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, m := range rows {
			out = append(out, map[string]any{
				"user_id":       m.UserID,
				"email":         m.Email,
				"role":          string(m.Role),
				"joined_at":     m.JoinedAt.UTC().Format(time.RFC3339),
				"last_login_at": nullableTimeStr(m.LastLoginAt),
			})
		}
		httpJSON(w, http.StatusOK, out)
	}
}

// RoleRecommendationsList wires GET /v1/orgs/{id}/role-recommendations.
// Pending rows only — accepted/dismissed history stays queryable via SQL and
// the audit log rather than this operator surface.
func RoleRecommendationsList(svc RoleRecommendationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := orgFromRequest(w, r)
		if !ok {
			return
		}
		rows, err := svc.ListPending(r.Context(), princ.OrgID)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, rec := range rows {
			out = append(out, recToWire(rec))
		}
		httpJSON(w, http.StatusOK, out)
	}
}

// RoleRecommendationsRefresh wires POST /v1/orgs/{id}/role-recommendations/refresh.
// Runs the analyser synchronously for the caller's org (member counts are
// small; the signal SQL is index-backed) and returns {"created": n}.
func RoleRecommendationsRefresh(svc RoleRecommendationService, audit domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := orgFromRequest(w, r)
		if !ok {
			return
		}
		n, err := svc.Refresh(r.Context(), princ.OrgID)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "refresh failed"})
			return
		}
		if audit != nil {
			_ = audit.Write(r.Context(), princ, "role_recommendation.refreshed", princ.OrgID,
				map[string]any{"created": n})
		}
		httpJSON(w, http.StatusOK, map[string]int{"created": n})
	}
}

// decideRoleRecReq is the body of POST .../role-recommendations/{rec_id}/decide.
type decideRoleRecReq struct {
	Action string `json:"action"` // accept | dismiss
}

// RoleRecommendationsDecide wires POST /v1/orgs/{id}/role-recommendations/{rec_id}/decide.
// Accept mutates org_members.role via the store's guarded update; a stale row
// (already decided, or the member's role changed since analysis) returns 409.
func RoleRecommendationsDecide(svc RoleRecommendationService, audit domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := orgFromRequest(w, r)
		if !ok {
			return
		}
		var req decideRoleRecReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
			return
		}
		if req.Action != "accept" && req.Action != "dismiss" {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be accept or dismiss"})
			return
		}
		rec, err := svc.Decide(r.Context(), princ.OrgID, chi.URLParam(r, "rec_id"),
			req.Action == "accept", princ.UserID)
		if err != nil {
			switch {
			case errors.Is(err, domain.ErrNotFound):
				httpJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			case errors.Is(err, domain.ErrConflict):
				httpJSON(w, http.StatusConflict, map[string]string{"error": "recommendation is stale or already decided"})
			default:
				httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "decide failed"})
			}
			return
		}
		if audit != nil {
			_ = audit.Write(r.Context(), princ, "role_recommendation.decided", rec.UserID, map[string]any{
				"action":           req.Action,
				"rule":             rec.Rule,
				"current_role":     string(rec.CurrentRole),
				"recommended_role": string(rec.RecommendedRole),
			})
		}
		httpJSON(w, http.StatusOK, recToWire(rec))
	}
}
