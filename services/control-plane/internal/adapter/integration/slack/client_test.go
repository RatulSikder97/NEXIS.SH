package slack_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/slack"
)

// fastHTTPX returns an httpx.Client tuned for tests — high rate limit + no
// real backoff sleeps — so per-test runtime stays under 200ms even when
// retries kick in.
func fastHTTPX(t *testing.T, base *http.Client) *httpx.Client {
	t.Helper()
	if base == nil {
		base = &http.Client{}
	}
	return httpx.New(base, httpx.Config{
		RatePerSec:       1000,
		Burst:            100,
		MaxAttempts:      1,
		BreakerThreshold: 1000,
	})
}

// testServer is a chi-free test fixture: any path → response from handler.
// All Slack API endpoints we test POST application/json, so we don't dispatch
// on path/method here — each test sets the global handler.
func testServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func TestAuthTest_OK(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/auth.test", r.URL.Path)
		require.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
		require.Equal(t, "application/json; charset=utf-8", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"url": "https://acme.slack.com/",
			"team": "Acme",
			"user": "nexis-bot",
			"team_id": "T0001",
			"user_id": "U0001"
		}`))
	})
	c := slack.NewClientWithBaseURL(fastHTTPX(t, srv.Client()), srv.URL)
	info, err := c.AuthTest(context.Background(), "xoxb-test")
	require.NoError(t, err)
	require.Equal(t, "U0001", info.BotUserID)
	require.Equal(t, "T0001", info.TeamID)
	require.Equal(t, "Acme", info.TeamName)
	require.Equal(t, "nexis-bot", info.BotName)
}

func TestAuthTest_TypedError(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": false, "error": "invalid_auth"}`))
	})
	c := slack.NewClientWithBaseURL(fastHTTPX(t, srv.Client()), srv.URL)
	_, err := c.AuthTest(context.Background(), "xoxb-bad")

	var apiErr *slack.APIError
	require.True(t, errors.As(err, &apiErr), "want typed APIError, got %T", err)
	require.Equal(t, "invalid_auth", apiErr.Code)
}

func TestPostMessage_ReturnsTS(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/chat.postMessage", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"channel": "C0001",
			"ts": "1700000000.000001"
		}`))
	})
	c := slack.NewClientWithBaseURL(fastHTTPX(t, srv.Client()), srv.URL)
	ts, err := c.PostMessage(context.Background(), "xoxb-test", "C0001", []map[string]any{
		{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "hello"}},
	})
	require.NoError(t, err)
	require.Equal(t, "1700000000.000001", ts)
}

func TestLookupUserByEmail_NotFound(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/users.lookupByEmail", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": false, "error": "users_not_found"}`))
	})
	c := slack.NewClientWithBaseURL(fastHTTPX(t, srv.Client()), srv.URL)
	_, err := c.LookupUserByEmail(context.Background(), "xoxb-test", "ghost@acme.dev")

	var apiErr *slack.APIError
	require.True(t, errors.As(err, &apiErr), "want typed APIError, got %T", err)
	require.Equal(t, "users_not_found", apiErr.Code)
}

func TestLookupUserByEmail_OK(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"user": {"id": "U0002", "name": "alice"}
		}`))
	})
	c := slack.NewClientWithBaseURL(fastHTTPX(t, srv.Client()), srv.URL)
	id, err := c.LookupUserByEmail(context.Background(), "xoxb-test", "alice@acme.dev")
	require.NoError(t, err)
	require.Equal(t, "U0002", id)
}

func TestOpenIM_ReturnsChannelID(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/conversations.open", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"channel": {"id": "D0001"}
		}`))
	})
	c := slack.NewClientWithBaseURL(fastHTTPX(t, srv.Client()), srv.URL)
	ch, err := c.OpenIM(context.Background(), "xoxb-test", "U0002")
	require.NoError(t, err)
	require.Equal(t, "D0001", ch)
}

func TestClient_HTTPStatusErrorSurfaces(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	})
	c := slack.NewClientWithBaseURL(fastHTTPX(t, srv.Client()), srv.URL)
	_, err := c.AuthTest(context.Background(), "xoxb-test")

	var apiErr *slack.APIError
	require.True(t, errors.As(err, &apiErr), "want typed APIError, got %T", err)
	require.Equal(t, http.StatusForbidden, apiErr.Status)
	require.True(t, strings.Contains(apiErr.Code, "forbidden"),
		"code should carry the error field, got %q", apiErr.Code)
}
