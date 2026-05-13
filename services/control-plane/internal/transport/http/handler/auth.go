package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// WorkspaceChecker is the narrow interface the auth handlers use to fill
// AuthResp.HasWorkspace / MeResp.HasWorkspace. *repo.WorkspacesRepo satisfies
// it via structural typing. The handler tolerates a nil checker (treats as
// "no workspaces yet") so existing tests + the no-pool dev path keep working.
type WorkspaceChecker interface {
	HasAny(ctx context.Context, orgID string) (bool, error)
}

// hasWorkspace is the safe wrapper around WorkspaceChecker — nil is fine, DB
// errors get logged but never block the auth response.
func hasWorkspace(ctx context.Context, ws WorkspaceChecker, orgID string) bool {
	if ws == nil || orgID == "" {
		return false
	}
	ok, err := ws.HasAny(ctx, orgID)
	if err != nil {
		slog.Default().Error("workspace has-any", "org_id", orgID, "err", err)
		return false
	}
	return ok
}

// sessionCookieName is the canonical cookie name for browser sessions. It is
// duplicated from middleware/auth.go on purpose: handlers set; middleware
// reads. Keep both in sync.
const sessionCookieName = "nexis_session"

// writeJSON serialises v as JSON with the given status. Encoding errors are
// logged through slog.Default() because the response is already started.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Default().Error("encode response", "err", err)
	}
}

// writeError emits a canonical {"error": msg} envelope.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, dto.ErrorResp{Error: msg})
}

// setSessionCookie attaches the session JWT as an HttpOnly cookie. The Secure
// flag is only set outside of dev so local HTTP-only development still works.
func setSessionCookie(w http.ResponseWriter, cfg config.Config, tok domain.SessionToken) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    tok.Token,
		Path:     "/",
		Expires:  tok.ExpiresAt,
		HttpOnly: true,
		Secure:   cfg.AppEnv != "dev",
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie tells the browser to drop the session cookie. Used by
// Logout — the server-side session is also revoked via the AuthProvider.
func clearSessionCookie(w http.ResponseWriter, cfg config.Config) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cfg.AppEnv != "dev",
		SameSite: http.SameSiteLaxMode,
	})
}

// mapAuthError converts a domain error to an HTTP status + user-safe message.
// Unknown errors are logged and reported as 500.
func mapAuthError(w http.ResponseWriter, err error, op string) {
	switch {
	case errors.Is(err, domain.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid credentials")
	case errors.Is(err, domain.ErrMFARequired):
		writeError(w, http.StatusBadRequest, "mfa required")
	case errors.Is(err, domain.ErrMFAInvalid):
		writeError(w, http.StatusBadRequest, "mfa invalid")
	case errors.Is(err, domain.ErrSessionRevoked):
		writeError(w, http.StatusUnauthorized, "session revoked")
	case errors.Is(err, domain.ErrSessionExpired):
		writeError(w, http.StatusUnauthorized, "session expired")
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	default:
		slog.Default().Error("auth handler", "op", op, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// auditWrite invokes the AuditWriter and logs any error. Audit failures don't
// abort the response — the cookie/header may already be set by the caller —
// but for protected endpoints the failed INSERT poisons the RLS tx, which
// means the surrounding mutation is rolled back at commit time. Treating
// audit as best-effort at the HTTP boundary but tx-atomic underneath is the
// right balance: we never silently lose an audit row for a successful
// mutation, while a transient audit DB hiccup still surfaces in logs.
func auditWrite(r *http.Request, aud domain.AuditWriter, p domain.Principal, action, target string, metadata map[string]any) {
	if aud == nil {
		return
	}
	if err := aud.Write(r.Context(), p, action, target, metadata); err != nil {
		slog.Default().Error("audit write", "action", action, "target", target, "err", err)
	}
}

// decodeBody decodes the JSON request body into v. On failure it writes a 400
// envelope and returns false so the caller can return early.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// Signup wires POST /v1/auth/signup. Returns 201 + session cookie on success.
//
// On success, an audit row is appended with the fresh principal we just
// minted — the request hasn't carried one yet, so we synthesise it from the
// signup result. Audit Write here runs OUTSIDE the RLS tx (the signup route
// is unauthenticated), so the writer falls through to its owning pool.
func Signup(p domain.AuthProvider, aud domain.AuditWriter, cfg config.Config, ws WorkspaceChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req dto.SignupReq
		if !decodeBody(w, r, &req) {
			return
		}
		res, err := p.Signup(r.Context(), domain.SignupInput{
			Email:    req.Email,
			Password: req.Password,
			OrgName:  req.OrgName,
		})
		if err != nil {
			mapAuthError(w, err, "signup")
			return
		}
		setSessionCookie(w, cfg, res.Session)
		auditWrite(r, aud, domain.Principal{
			UserID: res.User.ID,
			OrgID:  res.Org.ID,
			Role:   domain.RoleOwner,
		}, "user.signup", res.User.ID, map[string]any{
			"email": res.User.Email,
			"org":   res.Org.ID,
		})
		writeJSON(w, http.StatusCreated, dto.AuthResp{
			UserID:       res.User.ID,
			OrgID:        res.Org.ID,
			ExpiresAt:    res.Session.ExpiresAt.UTC().Format(time.RFC3339),
			HasWorkspace: hasWorkspace(r.Context(), ws, res.Org.ID),
		})
	}
}

// Login wires POST /v1/auth/login. Returns 200 + session cookie on success.
//
// The audit row is only written when we can resolve the freshly-issued token
// back to a Principal — otherwise we'd have no org id to bind to. A failed
// VerifyToken here is unusual (we just signed it) but if it does happen we
// skip the audit append rather than blocking the login.
func Login(p domain.AuthProvider, aud domain.AuditWriter, cfg config.Config, ws WorkspaceChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req dto.LoginReq
		if !decodeBody(w, r, &req) {
			return
		}
		tok, err := p.Login(r.Context(), domain.LoginInput{
			Email:    req.Email,
			Password: req.Password,
			MFACode:  req.MFACode,
		})
		if err != nil {
			mapAuthError(w, err, "login")
			return
		}
		setSessionCookie(w, cfg, tok)
		// Resolve principal so we can return user/org ids in the body. If this
		// lookup fails we still treat the login as successful — the cookie is
		// what the client needs.
		princ, err := p.VerifyToken(r.Context(), tok.Token)
		body := dto.AuthResp{ExpiresAt: tok.ExpiresAt.UTC().Format(time.RFC3339)}
		if err == nil {
			body.UserID = princ.UserID
			body.OrgID = princ.OrgID
			body.HasWorkspace = hasWorkspace(r.Context(), ws, princ.OrgID)
			auditWrite(r, aud, princ, "user.login", princ.UserID, map[string]any{
				"email": req.Email,
			})
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// Magic wires POST /v1/auth/magic. The endpoint is intentionally non-leaky:
// unknown emails return 202 the same as known ones.
func Magic(p domain.AuthProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req dto.MagicReq
		if !decodeBody(w, r, &req) {
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "email required")
			return
		}
		purpose := req.Purpose
		if purpose == "" {
			purpose = "login"
		}
		if err := p.IssueMagicLink(r.Context(), req.Email, purpose); err != nil {
			mapAuthError(w, err, "magic")
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

// Verify wires GET /v1/auth/verify?token=... — the magic-link landing page.
// The token query value is the base64url-encoded plaintext emitted by
// IssueMagicLink. We forward it verbatim to ConsumeMagicLink, which hashes
// the string and looks up the persisted hash. On success the session cookie
// is set and the browser is 302'd to the configured dashboard URL.
//
// We audit the magic-link consumption with the principal we just minted —
// see the Login handler for the same pattern. The query string carries the
// purpose only as a hint; ConsumeMagicLink doesn't return it, so we record
// the raw query param.
func Verify(p domain.AuthProvider, aud domain.AuditWriter, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			writeError(w, http.StatusBadRequest, "missing token")
			return
		}
		tok, err := p.ConsumeMagicLink(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		setSessionCookie(w, cfg, tok)
		if princ, err := p.VerifyToken(r.Context(), tok.Token); err == nil {
			auditWrite(r, aud, princ, "user.magic_consumed", princ.UserID, map[string]any{
				"purpose": r.URL.Query().Get("purpose"),
			})
		}
		dest := cfg.AppBaseURL + "/dashboard"
		http.Redirect(w, r, dest, http.StatusFound)
	}
}

// Logout wires POST /v1/auth/logout. RequireAuth guarantees a principal is
// present in ctx.
func Logout(p domain.AuthProvider, aud domain.AuditWriter, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		if princ.SessionID != "" {
			if err := p.Logout(r.Context(), princ.SessionID); err != nil {
				mapAuthError(w, err, "logout")
				return
			}
		}
		clearSessionCookie(w, cfg)
		auditWrite(r, aud, princ, "user.logout", princ.UserID, map[string]any{})
		w.WriteHeader(http.StatusNoContent)
	}
}

// MFAEnroll wires POST /v1/auth/mfa/enroll. Returns the QR as a data URL.
func MFAEnroll(p domain.AuthProvider, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		qr, secret, err := p.EnrollMFA(r.Context(), princ.UserID)
		if err != nil {
			mapAuthError(w, err, "mfa enroll")
			return
		}
		dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(qr)
		// secret is also embedded inside the otpauth URL of the QR PNG; we
		// surface it explicitly here so deterministic E2E tests can generate
		// a TOTP code without decoding a PNG. Do not log this value.
		auditWrite(r, aud, princ, "user.mfa_enroll_started", princ.UserID, map[string]any{})
		writeJSON(w, http.StatusOK, dto.MFAEnrollResp{
			QRDataURL:     dataURL,
			Secret:        secret,
			RecoveryCodes: []string{}, // reserved for Phase 3
		})
	}
}

// MFAVerify wires POST /v1/auth/mfa/verify.
func MFAVerify(p domain.AuthProvider, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req dto.MFAVerifyReq
		if !decodeBody(w, r, &req) {
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		if err := p.VerifyMFA(r.Context(), princ.UserID, req.Code); err != nil {
			mapAuthError(w, err, "mfa verify")
			return
		}
		auditWrite(r, aud, princ, "user.mfa_enabled", princ.UserID, map[string]any{})
		w.WriteHeader(http.StatusNoContent)
	}
}

// MFADisable wires DELETE /v1/auth/mfa.
func MFADisable(p domain.AuthProvider, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		if err := p.DisableMFA(r.Context(), princ.UserID); err != nil {
			mapAuthError(w, err, "mfa disable")
			return
		}
		auditWrite(r, aud, princ, "user.mfa_disabled", princ.UserID, map[string]any{})
		w.WriteHeader(http.StatusNoContent)
	}
}

// APIKeyCreate wires POST /v1/apikeys.
func APIKeyCreate(p domain.AuthProvider, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req dto.CreateAPIKeyReq
		if !decodeBody(w, r, &req) {
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name required")
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		created, err := p.CreateAPIKey(r.Context(), princ, req.Name, req.Scopes)
		if err != nil {
			mapAuthError(w, err, "apikey create")
			return
		}
		scopes := created.Key.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		auditWrite(r, aud, princ, "apikey.created", created.Key.ID, map[string]any{
			"name":   req.Name,
			"scopes": scopes,
		})
		writeJSON(w, http.StatusCreated, dto.APIKeyCreatedResp{
			ID:        created.Key.ID,
			Prefix:    created.Key.Prefix,
			Name:      created.Key.Name,
			Scopes:    scopes,
			Plaintext: created.Plaintext,
		})
	}
}

// APIKeyList wires GET /v1/apikeys.
func APIKeyList(p domain.AuthProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		keys, err := p.ListAPIKeys(r.Context(), princ)
		if err != nil {
			mapAuthError(w, err, "apikey list")
			return
		}
		out := make([]dto.APIKeyResp, 0, len(keys))
		for _, k := range keys {
			scopes := k.Scopes
			if scopes == nil {
				scopes = []string{}
			}
			lastUsed := ""
			if k.LastUsedAt != nil {
				lastUsed = k.LastUsedAt.UTC().Format(time.RFC3339)
			}
			out = append(out, dto.APIKeyResp{
				ID:         k.ID,
				Prefix:     k.Prefix,
				Name:       k.Name,
				Scopes:     scopes,
				CreatedAt:  k.CreatedAt.UTC().Format(time.RFC3339),
				LastUsedAt: lastUsed,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// APIKeyRevoke wires DELETE /v1/apikeys/{id}.
func APIKeyRevoke(p domain.AuthProvider, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		if err := p.RevokeAPIKey(r.Context(), princ, id); err != nil {
			mapAuthError(w, err, "apikey revoke")
			return
		}
		auditWrite(r, aud, princ, "apikey.revoked", id, map[string]any{})
		w.WriteHeader(http.StatusNoContent)
	}
}

// Me wires GET /v1/me. RequireAuth guarantees a principal is in ctx; we then
// fetch the User + Organization records to enrich the response. Phase 3.5
// includes HasWorkspace so the dashboard can decide whether to bounce the
// caller into the onboarding flow.
func Me(p domain.AuthProvider, ws WorkspaceChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		user, err := p.GetUser(r.Context(), princ.UserID)
		if err != nil {
			mapAuthError(w, err, "me get user")
			return
		}
		org, err := p.GetOrg(r.Context(), princ.OrgID)
		if err != nil {
			mapAuthError(w, err, "me get org")
			return
		}
		writeJSON(w, http.StatusOK, dto.MeResp{
			User:         dto.MeUser{ID: user.ID, Email: user.Email},
			Org:          dto.MeOrg{ID: org.ID, Name: org.Name, Slug: org.Slug},
			Role:         string(princ.Role),
			HasWorkspace: hasWorkspace(r.Context(), ws, princ.OrgID),
		})
	}
}
