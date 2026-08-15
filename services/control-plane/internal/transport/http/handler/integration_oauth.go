// Package handler — integration OAuth install endpoints.
//
// Wires the four browser-redirect routes that complete the GitHub-App and
// Slack-OAuth-v2 installs that the Wave 2 adapter packages already support
// programmatically:
//
//	GET /v1/integrations/github/install           (session-gated)
//	GET /v1/integrations/github/install/callback  (public)
//	GET /v1/integrations/slack/install            (session-gated)
//	GET /v1/integrations/slack/callback           (public)
//
// CSRF model: every install endpoint mints a random 32-byte state and stashes
// it in a one-shot `nexis_oauth_state` cookie (Path=/, Max-Age=300, HttpOnly,
// SameSite=Lax, Secure in non-dev). The callback verifies that the cookie
// matches the state echoed back by the upstream provider and clears the cookie
// unconditionally so a replay is impossible regardless of which branch fails.
//
// The install endpoints REQUIRE a session — the install needs to attribute
// the resulting connection to the caller's org. The callbacks DO NOT require a
// session: a real GitHub/Slack install can take the user across browser tabs
// or sign-in walls, so we re-establish the principal from the session cookie
// inside the callback and 401 if it's been cleared.
package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// runWithTenantTx opens a tx on appPool, pins app.current_org_id to the
// principal's org_id, attaches the tx to ctx so repos pick it up, and runs
// the closure. Used by OAuth install callbacks which sit OUTSIDE the RLS
// middleware (they're mounted in the public group because GitHub/Slack
// redirects may strip the session cookie in some browser contexts).
//
// Commits on success, rolls back on any error. Returns any error from the
// closure, the BeginTx call, or the GUC-pin SQL.
func runWithTenantTx(ctx context.Context, appPool *pgxpool.Pool, princ domain.Principal, fn func(ctx context.Context) error) error {
	if appPool == nil {
		// Dev fallback — no RLS pool wired; run the closure directly so
		// the test harness still works.
		return fn(ctx)
	}
	tx, err := appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", princ.OrgID); err != nil {
		return err
	}
	if err := fn(db.WithTx(ctx, tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// errOAuthCallback is unused — kept to suppress linter warnings on imports.
var _ = errors.New

// oauthStateMaxAge bounds the lifetime of the nexis_oauth_state cookie. Five
// minutes is the same window the WorkOS callback applies — long enough to
// account for a sign-in detour in the upstream flow, short enough that a
// stolen cookie can't be replayed indefinitely.
const oauthStateMaxAge = 5 * 60

// installStateLen is the byte length of the random CSRF state token before
// base64-url-encoding. 32 bytes = 256 bits of entropy — same as the WorkOS
// flow.
const installStateLen = 32

// setInstallStateCookie writes the one-shot CSRF cookie. Shared between the
// GitHub and Slack install paths so the cookie attributes stay in sync.
func setInstallStateCookie(w http.ResponseWriter, cfg config.Config, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   oauthStateMaxAge,
		HttpOnly: true,
		Secure:   cfg.AppEnv != "dev",
		SameSite: http.SameSiteLaxMode,
	})
}

// clearInstallStateCookie tells the browser to drop the one-shot CSRF cookie.
// Called unconditionally from every callback branch.
func clearInstallStateCookie(w http.ResponseWriter, cfg config.Config) {
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

// newOAuthState mints a base64-url-encoded random state token. Returns the
// empty string on RNG failure — callers MUST treat that as a hard error
// because a deterministic state would defeat the CSRF cookie check.
func newOAuthState() string {
	b := make([]byte, installStateLen)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// verifyInstallState compares the state query value against the one-shot
// cookie. Returns true only when both are non-empty and equal. The caller is
// responsible for clearing the cookie before this function runs (matches the
// WorkOSCallback pattern — clear-then-compare so a mismatch can't survive a
// replay).
func verifyInstallState(r *http.Request, state string) bool {
	c, err := r.Cookie(oauthStateCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	return state != "" && c.Value == state
}

// installRedirectURL builds the post-callback dashboard URL with
// `?installed=<provider>` query. Same pattern as GitHubMockInstall so the
// console can render a success banner without keeping handler-specific paths
// in the frontend.
func installRedirectURL(base, provider string) string {
	root := strings.TrimRight(base, "/")
	if root == "" {
		root = "http://localhost:3000"
	}
	return root + "/console/integrations?installed=" + url.QueryEscape(provider)
}

// installErrorURL is the destination for OAuth callback failures (state
// mismatch, exchange failure, etc.). Mirrors the WorkOSCallback shape so the
// console can surface a banner on /console/integrations.
func installErrorURL(base, provider, code string) string {
	root := strings.TrimRight(base, "/")
	if root == "" {
		root = "http://localhost:3000"
	}
	q := url.Values{}
	q.Set("install_error", code)
	q.Set("provider", provider)
	return root + "/console/integrations?" + q.Encode()
}

// GitHubInstallStart wires GET /v1/integrations/github/install. Mints a CSRF
// state, sets the one-shot cookie, and 302s to the GitHub App install URL.
// Session-gated — the caller must be logged in so the callback can attribute
// the install to their org.
//
// The install URL embeds the slug from cfg.GitHubAppSlug; absent that, the
// handler 503s rather than redirect to a malformed URL — operators see this
// in the response and know to wire GITHUB_APP_SLUG.
func GitHubInstallStart(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := appmw.PrincipalFrom(r.Context()); !ok {
			httpJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if cfg.GitHubAppSlug == "" {
			httpJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "github app slug not configured"})
			return
		}
		state := newOAuthState()
		if state == "" {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "rng failure"})
			return
		}
		setInstallStateCookie(w, cfg, state)
		q := url.Values{}
		q.Set("state", state)
		dest := "https://github.com/apps/" + url.PathEscape(cfg.GitHubAppSlug) + "/installations/new?" + q.Encode()
		http.Redirect(w, r, dest, http.StatusFound)
	}
}

// GitHubInstallCallback wires GET /v1/integrations/github/install/callback.
// Reads {installation_id, state} from the query, verifies the CSRF cookie,
// calls the GitHub provider's Connect to validate the install, and 302s to
// the console with `?installed=github`. Failures redirect to the same console
// path with `?install_error=<code>`.
//
// The cookie is cleared on every branch so a replay attempt cannot reuse the
// same state value even if the handler bails for a different reason.
func GitHubInstallCallback(reg *integration.Registry, aud domain.AuditWriter, cfg config.Config, appPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		clearInstallStateCookie(w, cfg)
		if !ok {
			http.Redirect(w, r, installErrorURL(cfg.AppBaseURL, "github", "unauthorized"), http.StatusFound)
			return
		}
		state := r.URL.Query().Get("state")
		if !verifyInstallState(r, state) {
			// Two different failures land here and the operator needs to
			// tell them apart: an install started somewhere other than the
			// console (GitHub's own "Install app" button never carries our
			// state) versus a genuine mismatch. Both are refused — the
			// state cookie is the only CSRF defence on this route — but the
			// console renders different guidance per code.
			code := "state_mismatch"
			if state == "" {
				code = "state_missing"
			}
			slog.Default().Warn("github install callback state rejected",
				"code", code, "setup_action", r.URL.Query().Get("setup_action"))
			http.Redirect(w, r, installErrorURL(cfg.AppBaseURL, "github", code), http.StatusFound)
			return
		}
		installID := r.URL.Query().Get("installation_id")
		if installID == "" {
			// GitHub omits installation_id when the user lands here after a
			// permissions *update* on an install we never recorded, or when
			// the App's Setup URL fires on a cancelled flow.
			http.Redirect(w, r, installErrorURL(cfg.AppBaseURL, "github", "missing_installation_id"), http.StatusFound)
			return
		}
		p, ok := reg.Get(domain.IntegrationGitHub)
		if !ok {
			http.Redirect(w, r, installErrorURL(cfg.AppBaseURL, "github", "provider_unavailable"), http.StatusFound)
			return
		}
		// Wrap Connect in a tx that pins app.current_org_id so the
		// integrations.WITH-CHECK RLS policy (migration 0025) accepts
		// the INSERT. The callback sits outside the RLS middleware
		// because it's mounted in the public group.
		var c domain.Connection
		err := runWithTenantTx(r.Context(), appPool, princ, func(ctx context.Context) error {
			out, err := p.Connect(ctx, princ, map[string]any{"installation_id": installID})
			c = out
			return err
		})
		if err != nil {
			slog.Default().Error("github install callback connect", "err", err, "org_id", princ.OrgID)
			http.Redirect(w, r, installErrorURL(cfg.AppBaseURL, "github", "connect_failed"), http.StatusFound)
			return
		}
		if aud != nil {
			meta := map[string]any{"installation_id": c.InstallationID, "via": "github_app_install"}
			if err := aud.Write(r.Context(), princ, "integration.connected", "github", meta); err != nil {
				slog.Default().Error("audit write", "action", "integration.connected", "err", err)
			}
		}
		http.Redirect(w, r, installRedirectURL(cfg.AppBaseURL, "github"), http.StatusFound)
	}
}

// SlackInstallStart wires GET /v1/integrations/slack/install. Mints a CSRF
// state, sets the cookie, and 302s to the URL returned by the Slack adapter's
// InstallURL. Session-gated for the same reason as GitHubInstallStart.
//
// The adapter's InstallURL embeds the configured client_id + redirect_uri.
// If neither has been wired (Slack OAuth disabled), InstallURL produces a URL
// without a client_id; we 503 instead so the user doesn't get a half-built
// authorize page that Slack will reject.
func SlackInstallStart(reg *integration.Registry, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := appmw.PrincipalFrom(r.Context()); !ok {
			httpJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if reg == nil || reg.Slack == nil {
			httpJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "slack provider not configured"})
			return
		}
		if cfg.SlackClientID == "" {
			httpJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "slack oauth not configured"})
			return
		}
		state := newOAuthState()
		if state == "" {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "rng failure"})
			return
		}
		setInstallStateCookie(w, cfg, state)
		dest := reg.Slack.InstallURL(state)
		http.Redirect(w, r, dest, http.StatusFound)
	}
}

// SlackInstallCallback wires GET /v1/integrations/slack/callback. Reads
// {code, state} from the query, verifies the CSRF cookie, calls the Slack
// provider's Connect with {code, state} (it runs the oauth.v2.access
// exchange + bot-token persistence path), and 302s to the console.
//
// The state cookie is cleared on every branch — same shape as
// GitHubInstallCallback above.
//
// appPool is the application-role pool used to open a tenant-pinned tx so
// the integrations.WITH-CHECK RLS policy (migration 0025) accepts the
// INSERT. This mirrors GitHubInstallCallback — the callback sits OUTSIDE
// the RLS middleware (mounted in the public group because Slack's OAuth
// redirect may strip the session cookie in some browser contexts) so we
// rebuild the GUC binding inline before calling Connect.
func SlackInstallCallback(reg *integration.Registry, aud domain.AuditWriter, cfg config.Config, appPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		clearInstallStateCookie(w, cfg)
		if !ok {
			http.Redirect(w, r, installErrorURL(cfg.AppBaseURL, "slack", "unauthorized"), http.StatusFound)
			return
		}
		state := r.URL.Query().Get("state")
		if !verifyInstallState(r, state) {
			http.Redirect(w, r, installErrorURL(cfg.AppBaseURL, "slack", "state_mismatch"), http.StatusFound)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Redirect(w, r, installErrorURL(cfg.AppBaseURL, "slack", "missing_code"), http.StatusFound)
			return
		}
		p, ok := reg.Get(domain.IntegrationSlack)
		if !ok {
			http.Redirect(w, r, installErrorURL(cfg.AppBaseURL, "slack", "provider_unavailable"), http.StatusFound)
			return
		}
		// Wrap Connect in a tx that pins app.current_org_id so the
		// integrations.WITH-CHECK RLS policy (migration 0025) accepts
		// the INSERT — same shape as GitHubInstallCallback above.
		var c domain.Connection
		err := runWithTenantTx(r.Context(), appPool, princ, func(ctx context.Context) error {
			out, err := p.Connect(ctx, princ, map[string]any{
				"code":  code,
				"state": state,
			})
			c = out
			return err
		})
		if err != nil {
			slog.Default().Error("slack install callback connect", "err", err, "org_id", princ.OrgID)
			http.Redirect(w, r, installErrorURL(cfg.AppBaseURL, "slack", "connect_failed"), http.StatusFound)
			return
		}
		if aud != nil {
			meta := map[string]any{"installation_id": c.InstallationID, "via": "slack_oauth_v2"}
			if err := aud.Write(r.Context(), princ, "integration.connected", "slack", meta); err != nil {
				slog.Default().Error("audit write", "action", "integration.connected", "err", err)
			}
		}
		http.Redirect(w, r, installRedirectURL(cfg.AppBaseURL, "slack"), http.StatusFound)
	}
}
