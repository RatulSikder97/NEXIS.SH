// Package slack — typed Slack Web API client used by the Provider adapter.
//
// The client is a thin opinionated wrapper around the shared httpx.Client. It
// covers only the surface needed by the Approval Gate's notifier + DM flow:
//
//   - oauth.v2.access — exchange the OAuth code for a bot token (in oauth.go).
//   - auth.test       — probe + identity for Status calls.
//   - chat.postMessage — post a Block Kit message to a channel or IM.
//   - users.lookupByEmail — resolve an approver's email to a Slack user id.
//   - conversations.open  — open a 1:1 IM with that user id.
//
// All requests are POSTed as application/json with a Bearer token; Slack's
// "Web API methods" surface accepts JSON bodies for every method we use. The
// response is always {ok: bool, error?: string, ...payload}; non-ok responses
// surface as a typed *APIError so callers can branch on Code (e.g.
// "users_not_found", "not_in_channel"). 5xx is left to httpx's retry/breaker.
//
// The base URL is overridable via NewClientWithBaseURL purely for tests; in
// production it is always https://slack.com.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
)

// defaultBaseURL is the Slack API root. The Web API methods live under /api/,
// the OAuth authorize page lives under /oauth/v2/authorize (handled in
// oauth.go's InstallURL).
const defaultBaseURL = "https://slack.com"

// APIError is returned by every Client method when Slack responds with
// {ok:false}. Status is the HTTP status (almost always 200 — Slack reports
// API-level failures inside the JSON envelope, not via HTTP codes), and Code
// is the literal "error" field returned by Slack (e.g. "users_not_found",
// "invalid_auth", "not_in_channel"). Callers can errors.As + branch on Code.
type APIError struct {
	Status int
	Code   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("slack api: status %d: %s", e.Status, e.Code)
}

// Client is the Slack Web API client. Construct with NewClient or
// NewClientWithBaseURL.
type Client struct {
	base  string
	httpc *httpx.Client
}

// NewClient builds a Client wrapping the supplied *httpx.Client. The wrapper
// is required — every outbound call goes through httpx so rate-limiting,
// retries, and breaker behaviour are uniform across adapters.
func NewClient(httpc *httpx.Client) *Client {
	return &Client{base: defaultBaseURL, httpc: httpc}
}

// NewClientWithBaseURL is the test-only constructor that lets *_test.go point
// at an httptest.Server URL. Keeping baseURL unexported on the type means
// nothing outside this package can rewrite the production endpoint.
func NewClientWithBaseURL(httpc *httpx.Client, baseURL string) *Client {
	return &Client{base: baseURL, httpc: httpc}
}

// AuthInfo is the slim view of Slack's auth.test response we keep — just
// enough to label a connection in the UI (team id + name) and resolve the
// bot's own identity (used to filter our own messages out of channel
// listeners in future phases).
type AuthInfo struct {
	BotUserID string
	BotName   string
	TeamID    string
	TeamName  string
}

// AuthTest calls https://api.slack.com/methods/auth.test. Used by Status to
// probe a stored bot token's liveness — the response is {ok, user_id, user,
// team_id, team, ...}. A token that has been revoked surfaces here as a
// {ok:false, error:"invalid_auth"} *APIError, which the caller renders as a
// "degraded" health pill.
func (c *Client) AuthTest(ctx context.Context, botToken string) (AuthInfo, error) {
	type respBody struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		UserID string `json:"user_id"`
		User   string `json:"user"`
		TeamID string `json:"team_id"`
		Team   string `json:"team"`
	}
	var out respBody
	if err := c.postJSON(ctx, "/api/auth.test", botToken, map[string]any{}, &out); err != nil {
		return AuthInfo{}, err
	}
	if !out.OK {
		return AuthInfo{}, &APIError{Status: http.StatusOK, Code: out.Error}
	}
	return AuthInfo{
		BotUserID: out.UserID,
		BotName:   out.User,
		TeamID:    out.TeamID,
		TeamName:  out.Team,
	}, nil
}

// PostMessage calls https://api.slack.com/methods/chat.postMessage with a
// Block Kit message body. channelID may be a channel name ("#alerts"), a
// channel id ("C0123"), or a DM channel id from OpenIM ("D0123") — Slack
// resolves all three uniformly.
//
// Returns the message timestamp ts on success. ts uniquely identifies the
// message within the channel and is the handle the Approval Gate would use
// later to update an in-flight approval block.
func (c *Client) PostMessage(ctx context.Context, botToken, channelID string, blocks []map[string]any) (string, error) {
	if channelID == "" {
		return "", errors.New("slack: channel id required")
	}
	type respBody struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	}
	body := map[string]any{
		"channel": channelID,
		"blocks":  blocks,
	}
	var out respBody
	if err := c.postJSON(ctx, "/api/chat.postMessage", botToken, body, &out); err != nil {
		return "", err
	}
	if !out.OK {
		return "", &APIError{Status: http.StatusOK, Code: out.Error}
	}
	return out.TS, nil
}

// LookupUserByEmail calls https://api.slack.com/methods/users.lookupByEmail.
// The "users:read.email" scope is required on the bot token; absent the scope
// Slack returns {ok:false, error:"missing_scope"} which surfaces here as a
// typed *APIError so the caller (DMUserByEmail) can render a helpful 4xx
// upstream rather than a generic 500.
//
// Email lookup returns "users_not_found" for unknown emails — the caller
// branches on that to render an "approver not in workspace" error.
func (c *Client) LookupUserByEmail(ctx context.Context, botToken, email string) (string, error) {
	if email == "" {
		return "", errors.New("slack: email required")
	}
	type respBody struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	body := map[string]any{"email": email}
	var out respBody
	if err := c.postJSON(ctx, "/api/users.lookupByEmail", botToken, body, &out); err != nil {
		return "", err
	}
	if !out.OK {
		return "", &APIError{Status: http.StatusOK, Code: out.Error}
	}
	return out.User.ID, nil
}

// OpenIM calls https://api.slack.com/methods/conversations.open with a single
// user id. Returns the resulting IM channel id (always starts with "D"), which
// the caller then passes to PostMessage to deliver a 1:1 message.
//
// conversations.open is idempotent — calling it twice for the same user
// returns the same channel id, so the caller does not need to cache the value.
func (c *Client) OpenIM(ctx context.Context, botToken, userID string) (string, error) {
	if userID == "" {
		return "", errors.New("slack: user id required")
	}
	type respBody struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	body := map[string]any{"users": userID}
	var out respBody
	if err := c.postJSON(ctx, "/api/conversations.open", botToken, body, &out); err != nil {
		return "", err
	}
	if !out.OK {
		return "", &APIError{Status: http.StatusOK, Code: out.Error}
	}
	return out.Channel.ID, nil
}

// postJSON is the shared POST helper. Every Slack Web API call we make is a
// POST with a Bearer token, application/json content, and a JSON-encoded body.
// out must be a pointer to a struct that decodes the {ok, error, ...} envelope.
//
// HTTP-level non-2xx (rare from Slack — they prefer ok:false) surfaces as a
// status-tagged *APIError so callers don't have to inspect raw bodies. The
// 5xx retry path lives in httpx; by the time we get here a 5xx has already
// been exhausted.
func (c *Client) postJSON(ctx context.Context, path, botToken string, body any, out any) error {
	if c.httpc == nil {
		return errors.New("slack: httpx.Client required")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("slack: marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("slack: build request: %w", err)
	}
	if botToken != "" {
		req.Header.Set("Authorization", "Bearer "+botToken)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("slack: do: %w", err)
	}
	defer resp.Body.Close()

	// Limit the read to 1 MiB — Slack responses for the methods we call are
	// tiny (a few hundred bytes); anything larger means an upstream error page
	// we don't want to buffer fully into memory.
	const maxBody = 1 << 20
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("slack: read body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return &APIError{Status: resp.StatusCode, Code: trimToCode(bodyBytes)}
	}
	if err := json.Unmarshal(bodyBytes, out); err != nil {
		return fmt.Errorf("slack: decode response: %w", err)
	}
	return nil
}

// trimToCode tries to extract Slack's "error" field from a non-2xx body so
// the *APIError carries something meaningful. Falls back to the raw body when
// the response is not JSON (e.g. Cloudflare HTML error page).
func trimToCode(b []byte) string {
	var asJSON struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &asJSON) == nil && asJSON.Error != "" {
		return asJSON.Error
	}
	const maxFallback = 256
	if len(b) > maxFallback {
		return string(b[:maxFallback])
	}
	return string(b)
}
