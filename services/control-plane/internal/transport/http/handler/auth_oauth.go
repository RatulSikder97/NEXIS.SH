// Package handler — WorkOS OAuth callback (Phase 7).
//
// The dashboard's sign-in button redirects to WorkOS AuthKit with
// `redirect_uri=${origin}/v1/auth/workos/callback&state=<csrf>` and a
// `nexis_oauth_state` cookie holding the same csrf value. WorkOS sends the
// browser back to this handler with `?code=<grant>&state=<csrf>`; we:
//
//   1. compare the cookie state to the query state (CSRF defence),
//   2. exchange the code via the AuthProvider's ConsumeOAuthCode method,
//   3. mint a nexis_session cookie,
//   4. clear the one-shot oauth state cookie,
//   5. 302 to `return_to` (default `/console`).
//
// The route is mounted in the public group — the browser has no session yet.

package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// oauthStateCookieName is the canonical one-shot CSRF cookie the dashboard
// sets before redirecting to WorkOS AuthKit. Keep in sync with the frontend.
const oauthStateCookieName = "nexis_oauth_state"

// defaultReturnTo is where the browser lands after a successful sign-in if no
// ?return_to= query was supplied. /console matches the dashboard's authed
// home route — same destination Login + Verify already use.
const defaultReturnTo = "/console"

// WorkOSCallback wires GET /v1/auth/workos/callback. The handler is
// intentionally minimal: state check, code exchange, session cookie, redirect.
// All persistence + session-mint logic lives behind the AuthProvider port.
func WorkOSCallback(p domain.AuthProvider, aud domain.AuditWriter, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		if code == "" {
			redirectWithError(w, r, cfg, "missing_code")
			return
		}

		// CSRF defence: compare the one-shot cookie to the state query param.
		// Missing cookie → 400 (the dashboard MUST set it before redirecting).
		// Mismatch → 400. Both clear the cookie either way.
		cookie, err := r.Cookie(oauthStateCookieName)
		clearOAuthStateCookie(w, cfg)
		if err != nil || cookie.Value == "" {
			redirectWithError(w, r, cfg, "state_missing")
			return
		}
		if state == "" || cookie.Value != state {
			redirectWithError(w, r, cfg, "state_mismatch")
			return
		}

		res, err := p.ConsumeOAuthCode(r.Context(), code)
		if err != nil {
			if errors.Is(err, domain.ErrNotImplemented) {
				redirectWithError(w, r, cfg, "oauth_not_implemented")
				return
			}
			redirectWithError(w, r, cfg, "exchange_failed")
			return
		}

		setSessionCookie(w, cfg, res.Session)
		auditWrite(r, aud, domain.Principal{
			UserID: res.User.ID,
			OrgID:  res.Org.ID,
			Role:   domain.RoleOwner,
		}, "user.oauth_consumed", res.User.ID, map[string]any{
			"provider": p.Name(),
			"email":    res.User.Email,
			"org":      res.Org.ID,
		})

		dest := resolveReturnTo(r, cfg)
		http.Redirect(w, r, dest, http.StatusFound)
	}
}

// resolveReturnTo picks the destination URL for the post-callback redirect.
// Query string `return_to` wins when supplied AND safe; otherwise we fall
// back to `${AppBaseURL}/console`. Safe means same-origin (no protocol-relative
// `//host` or absolute URLs to other origins) — defence against open-redirect
// abuse via the callback.
func resolveReturnTo(r *http.Request, cfg config.Config) string {
	dest := cfg.AppBaseURL + defaultReturnTo
	rt := strings.TrimSpace(r.URL.Query().Get("return_to"))
	if rt == "" {
		return dest
	}
	// Allow only same-origin relative paths starting with a single slash.
	// "//evil.com/x" is rejected because url.Parse would set Host.
	parsed, err := url.Parse(rt)
	if err != nil {
		return dest
	}
	if parsed.IsAbs() || parsed.Host != "" {
		// Permit absolute URLs only when they point at the configured app
		// base URL. Anything else is treated as untrusted.
		if cfg.AppBaseURL != "" && strings.HasPrefix(rt, cfg.AppBaseURL) {
			return rt
		}
		return dest
	}
	if !strings.HasPrefix(parsed.Path, "/") {
		return dest
	}
	return cfg.AppBaseURL + parsed.RequestURI()
}

// redirectWithError sends the browser back to the dashboard with a short
// `oauth_error=<code>` query param so the sign-in page can surface a banner
// without baking server-side templating into this handler.
func redirectWithError(w http.ResponseWriter, r *http.Request, cfg config.Config, code string) {
	q := url.Values{}
	q.Set("oauth_error", code)
	dest := cfg.AppBaseURL + "/sign-in?" + q.Encode()
	http.Redirect(w, r, dest, http.StatusFound)
}

// clearOAuthStateCookie tells the browser to drop the one-shot CSRF cookie.
// Called unconditionally so a replay attempt with an old cookie value can't
// be reused even if the handler bails for a different reason.
func clearOAuthStateCookie(w http.ResponseWriter, cfg config.Config) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cfg.AppEnv != "dev",
		SameSite: http.SameSiteLaxMode,
	})
}
