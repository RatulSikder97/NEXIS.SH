package handler

// Phase 9 — session management. Two authenticated endpoints under /v1/me:
//
//	GET    /v1/me/sessions       → the caller's live sessions, newest first
//	DELETE /v1/me/sessions/{id}  → revoke one of the caller's own sessions
//
// Both run behind RequireAuth, so a Principal is always in ctx. The provider
// scopes every operation to the principal's own user id — a session id
// belonging to someone else comes back as 404, never 403, so the endpoint
// can't be used to confirm foreign session ids.

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// SessionsList wires GET /v1/me/sessions. IsCurrent is computed here (not in
// the provider) by comparing each row to the requesting principal's session.
func SessionsList(p domain.AuthProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		sessions, err := p.ListSessions(r.Context(), princ.UserID)
		if err != nil {
			mapAuthError(w, err, "sessions list")
			return
		}
		out := make([]dto.SessionResp, 0, len(sessions))
		for _, s := range sessions {
			lastSeen := ""
			if s.LastSeenAt != nil {
				lastSeen = s.LastSeenAt.UTC().Format(time.RFC3339)
			}
			out = append(out, dto.SessionResp{
				ID:         s.ID,
				CreatedAt:  s.CreatedAt.UTC().Format(time.RFC3339),
				LastSeenAt: lastSeen,
				UserAgent:  s.UserAgent,
				IP:         s.IP,
				IsCurrent:  s.ID == princ.SessionID,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// SessionRevoke wires DELETE /v1/me/sessions/{id}. Revoking the current
// session is allowed (it's just a logout the hard way); the web UI hides the
// button for the current row but the API stays permissive.
func SessionRevoke(p domain.AuthProvider, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		if err := p.RevokeSession(r.Context(), princ.UserID, id); err != nil {
			mapAuthError(w, err, "session revoke")
			return
		}
		auditWrite(r, aud, princ, "session.revoked", id, map[string]any{})
		w.WriteHeader(http.StatusNoContent)
	}
}
