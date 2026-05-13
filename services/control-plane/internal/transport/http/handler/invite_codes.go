// Package handler — invite-codes HTTP surface (Phase 8 — public beta).
//
// Four endpoints under /v1/invite-codes (owner-only) + signup-side redemption
// in auth.go.
//
//   POST   /v1/invite-codes              → mint N codes
//   GET    /v1/invite-codes              → list non-revoked codes
//   DELETE /v1/invite-codes/{code}       → revoke (set expires_at = now())
//
// Redemption is wired into the existing POST /v1/auth/signup via the
// ?invite=<code> query param. When SIGNUP_REQUIRES_INVITE=1 the redemption is
// mandatory; otherwise it's best-effort and ignored on missing/invalid codes.
package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// createInviteCodesReq is the body of POST /v1/invite-codes. All fields are
// optional; defaults are documented below.
type createInviteCodesReq struct {
	// MaxUses is the per-code cap. Defaults to 1.
	MaxUses int `json:"max_uses"`
	// ExpiresAt is RFC3339. Empty/zero = never expires.
	ExpiresAt string `json:"expires_at"`
	// Count is the number of codes to mint. Capped at 50 per call so an
	// owner can't accidentally fill the table.
	Count int `json:"count"`
}

// createInviteCodeRespEntry is one item in the create-response array.
type createInviteCodeRespEntry struct {
	Code      string `json:"code"`
	MaxUses   int    `json:"max_uses"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// InviteCodesCreate wires POST /v1/invite-codes. Owner-only.
func InviteCodesCreate(repoInst *repo.InviteCodesRepo, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		var req createInviteCodesReq
		if !decodeBody(w, r, &req) {
			return
		}
		if req.MaxUses < 1 {
			req.MaxUses = 1
		}
		if req.Count < 1 {
			req.Count = 1
		}
		if req.Count > 50 {
			req.Count = 50
		}
		var expires *time.Time
		if req.ExpiresAt != "" {
			t, err := time.Parse(time.RFC3339, req.ExpiresAt)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid expires_at")
				return
			}
			expires = &t
		}

		userID := princ.UserID
		out := make([]createInviteCodeRespEntry, 0, req.Count)
		for i := 0; i < req.Count; i++ {
			code, err := repo.GenerateCode()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "code gen failed")
				return
			}
			ic := domain.InviteCode{
				Code:      code,
				MaxUses:   req.MaxUses,
				ExpiresAt: expires,
				CreatedBy: &userID,
			}
			if err := repoInst.Create(r.Context(), ic); err != nil {
				slog.Default().Error("invite_codes: create", "err", err)
				writeError(w, http.StatusInternalServerError, "create failed")
				return
			}
			entry := createInviteCodeRespEntry{
				Code:    code,
				MaxUses: req.MaxUses,
			}
			if expires != nil {
				entry.ExpiresAt = expires.UTC().Format(time.RFC3339)
			}
			out = append(out, entry)
		}
		auditWrite(r, aud, princ, "invite_codes.created", "", map[string]any{
			"count":    req.Count,
			"max_uses": req.MaxUses,
		})
		writeJSON(w, http.StatusCreated, out)
	}
}

// inviteCodeListEntry is one row in the list response.
type inviteCodeListEntry struct {
	Code       string `json:"code"`
	MaxUses    int    `json:"max_uses"`
	UsedCount  int    `json:"used_count"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	CreatedBy  string `json:"created_by,omitempty"`
	CreatedAt  string `json:"created_at"`
	Active     bool   `json:"active"`
}

// InviteCodesList wires GET /v1/invite-codes. Returns every code newest-first.
// The Active flag lets the dashboard grey out expired/exhausted rows without
// re-deriving the predicate client-side.
func InviteCodesList(repoInst *repo.InviteCodesRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := repoInst.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list failed")
			return
		}
		now := time.Now().UTC()
		out := make([]inviteCodeListEntry, 0, len(rows))
		for _, c := range rows {
			entry := inviteCodeListEntry{
				Code:      c.Code,
				MaxUses:   c.MaxUses,
				UsedCount: c.UsedCount,
				CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
				Active:    c.Active(now),
			}
			if c.ExpiresAt != nil {
				entry.ExpiresAt = c.ExpiresAt.UTC().Format(time.RFC3339)
			}
			if c.CreatedBy != nil {
				entry.CreatedBy = *c.CreatedBy
			}
			out = append(out, entry)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// InviteCodesRevoke wires DELETE /v1/invite-codes/{code}. Sets expires_at =
// now() rather than deleting the row.
func InviteCodesRevoke(repoInst *repo.InviteCodesRepo, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		code := repo.NormalizeCode(chi.URLParam(r, "code"))
		if code == "" {
			writeError(w, http.StatusBadRequest, "code required")
			return
		}
		if err := repoInst.Revoke(r.Context(), code); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "revoke failed")
			return
		}
		auditWrite(r, aud, princ, "invite_codes.revoked", code, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

// applyInviteCode is the signup-side hook called from the Signup handler. The
// behaviour is parameterised on `required` so a single helper serves both
// modes:
//
//   - required=true (env SIGNUP_REQUIRES_INVITE=1) — missing/invalid code
//     blocks signup with 403.
//   - required=false — best-effort. Missing or invalid is ignored; a valid
//     code is consumed (used_count incremented).
//
// The function returns a (consumed, err) pair so the caller can distinguish
// "no code provided" from "redemption failed". When required=false the err
// is always nil for any client-facing failure mode.
func applyInviteCode(r *http.Request, codes *repo.InviteCodesRepo, required bool) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("invite"))
	if raw == "" {
		if required {
			return false, errors.New("invite code required")
		}
		return false, nil
	}
	if codes == nil {
		if required {
			return false, errors.New("invite code subsystem unavailable")
		}
		return false, nil
	}
	code := repo.NormalizeCode(raw)
	if err := codes.Redeem(r.Context(), code); err != nil {
		if required {
			return false, err
		}
		return false, nil
	}
	return true, nil
}
