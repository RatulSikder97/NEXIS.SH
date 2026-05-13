// Package slack — OAuth v2 install + callback for the Slack app.
//
// Slack's "Sign in with Slack" / "Add to Slack" flow is OAuth 2.0 with a
// per-app client_id, client_secret, a list of bot scopes the workspace owner
// will grant, and a CSRF state token. The control-plane side has two halves:
//
//   - InstallURL builds the redirect URL that GET /v1/integrations/slack/install
//     issues to the browser. The state value comes from the surrounding handler
//     (it pins a per-session CSRF token into a signed cookie); we sign nothing
//     here.
//
//   - ExchangeCode swaps the code returned to the callback for a bot token
//     via POST https://slack.com/api/oauth.v2.access. Slack returns a JSON
//     envelope shape that is slightly different from every other Web API
//     method: there's no Authorization header, and the credentials go in as
//     form-encoded body fields (client_id, client_secret, code, redirect_uri).
//
// Granted scopes are space-separated; the slim view we keep on OAuthResult
// is sufficient for the Status panel and the audit row.
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// defaultScopes is the set of bot scopes the Slack app requests. Keep this
// in sync with the App Manifest under apps/slack-app/manifest.yaml — if the
// manifest drops a scope, Slack will silently grant a smaller token and the
// downstream call (e.g. users.lookupByEmail) will fail with "missing_scope".
//
// chat:write          — chat.postMessage to channels the bot has been invited to.
// im:write            — open IM channels with users (conversations.open).
// users:read          — base read of user list.
// users:read.email    — required for users.lookupByEmail to return a hit.
// incoming-webhook    — Slack still requires this for legacy webhook delivery
//
//	even on OAuth v2 — keeps the configured-channel handle alive for the UI.
var defaultScopes = []string{
	"chat:write",
	"im:write",
	"users:read",
	"users:read.email",
	"incoming-webhook",
}

// OAuthConfig bundles the three pieces of Slack-app metadata the install flow
// reads at runtime. Constructed in cmd/server/main.go from config.Config and
// passed into Provider.NewWithOAuth (see provider.go).
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

// OAuthResult is the slim view of the oauth.v2.access response we keep. The
// raw response from Slack includes the user-level token, refresh metadata,
// and the incoming-webhook handle; the recovery workflow only needs the bot
// token + team + bot user id, so we drop the rest.
type OAuthResult struct {
	BotToken  string // xoxb-...
	BotUserID string
	TeamID    string
	TeamName  string
	Scope     string // space-separated granted scopes
}

// InstallURL returns the URL the install handler redirects to. state must be
// a CSRF token bound to the caller's session (e.g. base64(rand(32)) stashed
// in a signed cookie); the callback handler verifies that the state echoed
// back by Slack matches the cookie before calling ExchangeCode.
//
// scopes is the bot-token scope list — pass nil to use defaultScopes. The
// "user_scope" parameter is intentionally omitted: this flow only requests
// bot-token scopes; per-user scopes are out of phase for the Approval Gate.
func (p *Provider) InstallURL(state string) string {
	q := url.Values{}
	q.Set("client_id", p.oauth.ClientID)
	q.Set("scope", strings.Join(defaultScopes, ","))
	q.Set("redirect_uri", p.oauth.RedirectURI)
	q.Set("state", state)
	return defaultBaseURL + "/oauth/v2/authorize?" + q.Encode()
}

// ExchangeCode posts to oauth.v2.access with the OAuth callback's code value
// and the app's client credentials. Returns the bot token + team metadata on
// success.
//
// Slack returns {ok, error?, app_id, authed_user{...}, scope, team{id,name},
// bot_user_id, access_token}. We extract the bot half; the authed_user half
// (user-level token) is discarded — the Approval Gate uses the bot identity
// for every API call.
func (p *Provider) ExchangeCode(ctx context.Context, code string) (OAuthResult, error) {
	if code == "" {
		return OAuthResult{}, errors.New("slack: code required")
	}
	if p.oauth.ClientID == "" || p.oauth.ClientSecret == "" {
		return OAuthResult{}, errors.New("slack: oauth client credentials not configured")
	}
	if p.client == nil {
		return OAuthResult{}, errors.New("slack: client not configured")
	}

	form := url.Values{}
	form.Set("client_id", p.oauth.ClientID)
	form.Set("client_secret", p.oauth.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", p.oauth.RedirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.client.base+"/api/oauth.v2.access", strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthResult{}, fmt.Errorf("slack: build oauth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.httpc.Do(req)
	if err != nil {
		return OAuthResult{}, fmt.Errorf("slack: oauth do: %w", err)
	}
	defer resp.Body.Close()

	// Cap the read so a pathological response can't blow memory; the real
	// payload is ~1 KiB.
	const maxBody = 64 << 10
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return OAuthResult{}, fmt.Errorf("slack: oauth read body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return OAuthResult{}, &APIError{Status: resp.StatusCode, Code: trimToCode(bodyBytes)}
	}

	var out struct {
		OK          bool   `json:"ok"`
		Error       string `json:"error"`
		AccessToken string `json:"access_token"`
		BotUserID   string `json:"bot_user_id"`
		Scope       string `json:"scope"`
		Team        struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
	}
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		return OAuthResult{}, fmt.Errorf("slack: oauth decode: %w", err)
	}
	if !out.OK {
		return OAuthResult{}, &APIError{Status: http.StatusOK, Code: out.Error}
	}
	if !strings.HasPrefix(out.AccessToken, "xoxb-") {
		// Defensive: oauth.v2.access on a manifest without bot-token scopes
		// returns a user token (xoxp-...) and we'd silently store the wrong
		// shape. Fail explicitly so the operator notices.
		return OAuthResult{}, fmt.Errorf("slack: expected bot token, got prefix %q", tokenPrefix(out.AccessToken))
	}
	return OAuthResult{
		BotToken:  out.AccessToken,
		BotUserID: out.BotUserID,
		TeamID:    out.Team.ID,
		TeamName:  out.Team.Name,
		Scope:     out.Scope,
	}, nil
}

// tokenPrefix returns the first segment of a token (up to the first '-') for
// inclusion in error messages. We never log the full token.
func tokenPrefix(t string) string {
	i := strings.IndexByte(t, '-')
	if i < 0 {
		if len(t) > 6 {
			return t[:6]
		}
		return t
	}
	return t[:i+1]
}
