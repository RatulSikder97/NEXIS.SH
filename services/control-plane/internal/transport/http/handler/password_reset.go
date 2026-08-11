package handler

// Phase 9 — password reset. Two public (unauthenticated) endpoints:
//
//	POST /v1/auth/password-reset/request  {email}                → 202
//	POST /v1/auth/password-reset/confirm  {token, new_password}  → 204
//
// The request endpoint is deliberately non-leaky: unknown emails return 202
// exactly like known ones (the provider swallows ErrNotFound internally), so
// the endpoint can't be used to probe for accounts — same contract as
// POST /v1/auth/magic. The confirm endpoint collapses every token failure
// (unknown / expired / already consumed) into one 401 so callers can't
// distinguish which precondition failed.

import (
	"errors"
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
)

// minResetPasswordLen mirrors the provider-side minimum (and the signup
// form's minLength=8). Validated here too so the caller gets a specific 400
// rather than the generic 401 the provider's wrapped error would map to.
const minResetPasswordLen = 8

// PasswordResetRequest wires POST /v1/auth/password-reset/request. Always
// 202 on a well-formed body — see the package comment on leak resistance.
func PasswordResetRequest(p domain.AuthProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req dto.PasswordResetRequestReq
		if !decodeBody(w, r, &req) {
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "email required")
			return
		}
		if err := p.RequestPasswordReset(r.Context(), req.Email); err != nil {
			mapAuthError(w, err, "password reset request")
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

// PasswordResetConfirm wires POST /v1/auth/password-reset/confirm. On success
// the credential is replaced and every session for the user is revoked, so
// there is no cookie to set — the client redirects to sign-in.
func PasswordResetConfirm(p domain.AuthProvider, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req dto.PasswordResetConfirmReq
		if !decodeBody(w, r, &req) {
			return
		}
		if req.Token == "" {
			writeError(w, http.StatusBadRequest, "token required")
			return
		}
		if len(req.NewPassword) < minResetPasswordLen {
			writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		if err := p.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
			// Collapse all token-shaped failures into one non-leaky 401;
			// everything else goes through the standard mapping (500 in
			// prod, cause surfaced in dev via safeErrorMessage).
			if errors.Is(err, domain.ErrInvalidCredentials) {
				writeError(w, http.StatusUnauthorized, "invalid or expired reset token")
				return
			}
			mapAuthErrorSafe(w, err, "password reset confirm", cfg)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
