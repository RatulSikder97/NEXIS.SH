// Package handler — user preferences (Phase 3 Stage 8).
//
// Two protected endpoints under /v1/me/preferences:
//
//   * GET  → returns the current user's preferences JSON object (or {} if
//     none have been set). Open to any authenticated principal.
//   * PATCH → overwrites preferences with the supplied JSON body.
//
// The endpoint validates the `theme` key when present — only "light", "dark",
// and "system" are accepted. Other keys pass through unchanged so future
// settings can be added without round-tripping schema changes.
//
// Implementation note: the AuthProvider port does not currently expose
// preferences directly; we type-assert on a narrow prefsUpdater interface
// so this handler degrades cleanly when wired against an adapter that
// doesn't implement it (returns 501).
package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// prefsUpdater is the minimal contract the auth provider must satisfy for
// the preferences endpoint to work. The local Provider implements it via
// its underlying Store.
type prefsUpdater interface {
	GetUserPreferences(ctx context.Context, userID string) (map[string]any, error)
	UpdateUserPreferences(ctx context.Context, userID string, prefs map[string]any) error
}

// GetPreferences wires GET /v1/me/preferences.
func GetPreferences(auth domain.AuthProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		u, ok := auth.(prefsUpdater)
		if !ok {
			httpJSON(w, http.StatusNotImplemented, map[string]string{"error": "preferences not supported"})
			return
		}
		p, err := u.GetUserPreferences(r.Context(), princ.UserID)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if p == nil {
			p = map[string]any{}
		}
		httpJSON(w, http.StatusOK, p)
	}
}

// PatchPreferences wires PATCH /v1/me/preferences. Body must be a JSON object;
// the entire object replaces the stored preferences (last-write-wins, the UI
// always sends the full preferences object — there is no field-level merge).
func PatchPreferences(auth domain.AuthProvider, audit domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		var p map[string]any
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
			return
		}
		// Soft-validate the theme key — anything else passes through.
		if t, ok := p["theme"].(string); ok && t != "light" && t != "dark" && t != "system" {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid theme"})
			return
		}
		u, ok := auth.(prefsUpdater)
		if !ok {
			httpJSON(w, http.StatusNotImplemented, map[string]string{"error": "preferences not supported"})
			return
		}
		if err := u.UpdateUserPreferences(r.Context(), princ.UserID, p); err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if audit != nil {
			_ = audit.Write(r.Context(), princ, "user.preferences_updated", princ.UserID, p)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
