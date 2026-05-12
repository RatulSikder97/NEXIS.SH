// Package handler — invite surface (Phase 3).
//
// Three protected endpoints under /v1/orgs/{id}/invites: issue, list, revoke.
// Two public endpoints under /v1/invites/{token}: get (landing page) and claim
// (sets session cookie). RBAC: issue + revoke are owner-only; list is admin or
// owner. The token in the URL is the raw 32-byte base64url value emailed to
// the invitee; it is hashed before any DB lookup.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// issueInviteReq is the body of POST /v1/orgs/{id}/invites.
type issueInviteReq struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// InviteIssue mints an invite for the caller's org. The {id} URL param must
// match the caller's org_id — we don't trust cross-org issuance even from an
// owner. Returns 201 with token_prefix (first 8 chars of the raw token) — the
// full token only lands in the email so the prefix is safe to show in toasts.
func InviteIssue(auth domain.AuthProvider, audit domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		if chi.URLParam(r, "id") != princ.OrgID {
			httpJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		var req issueInviteReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
			return
		}
		role := domain.Role(req.Role)
		if role != domain.RoleAdmin && role != domain.RoleMember {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
			return
		}
		token, err := auth.IssueInvite(r.Context(), princ, req.Email, role)
		if err != nil {
			if errors.Is(err, domain.ErrConflict) {
				httpJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if audit != nil {
			_ = audit.Write(r.Context(), princ, "invite.issued", req.Email, map[string]any{"role": req.Role})
		}
		prefix := token
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		httpJSON(w, http.StatusCreated, map[string]string{"token_prefix": prefix})
	}
}

// InviteList wires GET /v1/orgs/{id}/invites. Includes claimed + expired rows
// — the UI badges them; the caller does not need to know about each row's
// lifecycle state to render the table.
func InviteList(auth domain.AuthProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		if chi.URLParam(r, "id") != princ.OrgID {
			httpJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		rows, err := auth.ListInvites(r.Context(), princ)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, i := range rows {
			out = append(out, map[string]any{
				"token_hash": toHex(i.TokenHash),
				"email":      i.Email,
				"role":       string(i.Role),
				"expires_at": i.ExpiresAt.UTC().Format(time.RFC3339),
				"claimed_at": nullableTimeStr(i.ClaimedAt),
			})
		}
		httpJSON(w, http.StatusOK, out)
	}
}

// InviteRevoke wires DELETE /v1/orgs/{id}/invites/{token_hash}. token_hash is
// the hex string surfaced by the list endpoint.
func InviteRevoke(auth domain.AuthProvider, audit domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		if chi.URLParam(r, "id") != princ.OrgID {
			httpJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		tokenHashHex := chi.URLParam(r, "token_hash")
		if err := auth.RevokeInvite(r.Context(), princ, tokenHashHex); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				httpJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if audit != nil {
			_ = audit.Write(r.Context(), princ, "invite.revoked", tokenHashHex, nil)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// InviteGet wires GET /v1/invites/{token} (public). Returns the org + role +
// inviter email so the claim landing page can render a coherent prompt.
func InviteGet(auth domain.AuthProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := auth.GetInviteInfo(r.Context(), chi.URLParam(r, "token"))
		if err != nil {
			httpJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		httpJSON(w, http.StatusOK, map[string]any{
			"org": map[string]string{
				"id":   info.OrgID,
				"name": info.OrgName,
				"slug": info.OrgSlug,
			},
			"role":          string(info.Role),
			"inviter_email": info.InviterEmail,
		})
	}
}

// claimReq is the body of POST /v1/invites/{token}/claim. Password is required
// only for brand-new users; for existing users the field is ignored.
type claimReq struct {
	Password string `json:"password"`
}

// InviteClaim wires POST /v1/invites/{token}/claim (public). On success the
// session cookie is set so the invitee lands on the dashboard already signed in.
func InviteClaim(auth domain.AuthProvider, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req claimReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		st, err := auth.ClaimInvite(r.Context(), chi.URLParam(r, "token"), req.Password)
		if err != nil {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		setSessionCookie(w, cfg, st)
		w.WriteHeader(http.StatusOK)
	}
}
