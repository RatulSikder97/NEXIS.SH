package slack_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/slack"
)

// newProviderWithOAuthServer constructs a Provider pointed at the supplied
// httptest.Server and wired with the supplied OAuth credentials. The repo is
// the package-level fakeRepo from provider_test.go.
func newProviderWithOAuthServer(t *testing.T, srv *httptest.Server, oauth slack.OAuthConfig) *slack.Provider {
	t.Helper()
	repo := newFakeRepo()
	client := slack.NewClientWithBaseURL(fastHTTPX(t, srv.Client()), srv.URL)
	return slack.NewWithOAuth(repo, reversingKV{}, client, oauth)
}

func TestExchangeCode_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/oauth.v2.access", r.URL.Path)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		s := string(body)
		// All four form fields must be present; the order is arbitrary.
		for _, expected := range []string{
			"client_id=cid-1",
			"client_secret=csecret-1",
			"code=oauth-code-xyz",
			"redirect_uri=",
		} {
			require.True(t, strings.Contains(s, expected),
				"body %q missing %q", s, expected)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"access_token": "xoxb-team-bot-token",
			"bot_user_id": "U0001",
			"scope": "chat:write,im:write,users:read.email",
			"team": {"id": "T0001", "name": "Acme"}
		}`))
	}))
	t.Cleanup(srv.Close)

	p := newProviderWithOAuthServer(t, srv, slack.OAuthConfig{
		ClientID:     "cid-1",
		ClientSecret: "csecret-1",
		RedirectURI:  "https://nexis.dev/v1/integrations/slack/callback",
	})
	res, err := p.ExchangeCode(context.Background(), "oauth-code-xyz")
	require.NoError(t, err)
	require.Equal(t, "xoxb-team-bot-token", res.BotToken)
	require.Equal(t, "U0001", res.BotUserID)
	require.Equal(t, "T0001", res.TeamID)
	require.Equal(t, "Acme", res.TeamName)
	require.Contains(t, res.Scope, "chat:write")
}

func TestExchangeCode_InvalidGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": false, "error": "invalid_code"}`))
	}))
	t.Cleanup(srv.Close)

	p := newProviderWithOAuthServer(t, srv, slack.OAuthConfig{
		ClientID:     "cid-1",
		ClientSecret: "csecret-1",
		RedirectURI:  "https://nexis.dev/cb",
	})
	_, err := p.ExchangeCode(context.Background(), "stale-code")

	var apiErr *slack.APIError
	require.True(t, errors.As(err, &apiErr), "want typed APIError, got %T", err)
	require.Equal(t, "invalid_code", apiErr.Code)
}

func TestExchangeCode_RejectsUserToken(t *testing.T) {
	// oauth.v2.access on a manifest without bot-token scopes returns a
	// user token (xoxp-...). The Provider must refuse to persist a non-bot
	// token rather than silently storing the wrong shape.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"access_token": "xoxp-user-token-no-bot",
			"team": {"id": "T0001", "name": "Acme"}
		}`))
	}))
	t.Cleanup(srv.Close)

	p := newProviderWithOAuthServer(t, srv, slack.OAuthConfig{
		ClientID:     "cid-1",
		ClientSecret: "csecret-1",
		RedirectURI:  "https://nexis.dev/cb",
	})
	_, err := p.ExchangeCode(context.Background(), "code")
	require.ErrorContains(t, err, "expected bot token")
}

func TestInstallURL_IncludesScopesAndState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)

	p := newProviderWithOAuthServer(t, srv, slack.OAuthConfig{
		ClientID:     "client-abc",
		ClientSecret: "secret-xyz",
		RedirectURI:  "https://nexis.dev/v1/integrations/slack/callback",
	})
	got := p.InstallURL("csrf-state-1")
	require.Contains(t, got, "/oauth/v2/authorize")
	require.Contains(t, got, "client_id=client-abc")
	require.Contains(t, got, "state=csrf-state-1")
	require.Contains(t, got, "redirect_uri=https%3A%2F%2Fnexis.dev%2Fv1%2Fintegrations%2Fslack%2Fcallback")
	// Scopes: comma-separated, URL-encoded as %2C.
	require.Contains(t, got, "chat%3Awrite")
	require.Contains(t, got, "users%3Aread.email")
}
