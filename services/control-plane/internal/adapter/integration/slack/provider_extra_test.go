package slack_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/slack"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// botFakeServer routes the four Slack Web API methods we exercise — auth.test,
// users.lookupByEmail, conversations.open, chat.postMessage — to canned
// responses. Each handler records its invocation so the test can assert on
// call order + the bearer token threaded through.
//
// All endpoints respond {ok: true, ...payload} unless a per-handler override
// is set via the methodOverrides map (used to drive error cases).
type botFakeServer struct {
	mu              sync.Mutex
	calls           []recordedCall
	methodOverrides map[string]http.HandlerFunc
	srv             *httptest.Server
}

type recordedCall struct {
	Path  string
	Token string
	Body  []byte
}

func newBotFakeServer(t *testing.T) *botFakeServer {
	b := &botFakeServer{methodOverrides: map[string]http.HandlerFunc{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth.test", b.wrap("auth.test", func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{
			"ok": true,
			"team_id": "T1",
			"team": "Acme",
			"user_id": "UB",
			"user": "nexis-bot"
		}`))
	}))
	mux.HandleFunc("/api/users.lookupByEmail", b.wrap("users.lookupByEmail", func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"ok": true, "user": {"id": "U42"}}`))
	}))
	mux.HandleFunc("/api/conversations.open", b.wrap("conversations.open", func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"ok": true, "channel": {"id": "D42"}}`))
	}))
	mux.HandleFunc("/api/chat.postMessage", b.wrap("chat.postMessage", func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"ok": true, "channel": "D42", "ts": "1700000000.000001"}`))
	}))
	b.srv = httptest.NewServer(mux)
	t.Cleanup(b.srv.Close)
	return b
}

func (b *botFakeServer) wrap(method string, ok func(http.ResponseWriter)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		b.mu.Lock()
		b.calls = append(b.calls, recordedCall{
			Path:  r.URL.Path,
			Token: r.Header.Get("Authorization"),
			Body:  body,
		})
		override := b.methodOverrides[method]
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if override != nil {
			override(w, r)
			return
		}
		ok(w)
	}
}

func (b *botFakeServer) recorded() []recordedCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]recordedCall, len(b.calls))
	copy(out, b.calls)
	return out
}

func (b *botFakeServer) callPaths() []string {
	calls := b.recorded()
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Path)
	}
	return out
}

func (b *botFakeServer) findCall(path string) (recordedCall, bool) {
	for _, c := range b.recorded() {
		if c.Path == path {
			return c, true
		}
	}
	return recordedCall{}, false
}

func newProviderBotMode(t *testing.T, b *botFakeServer) (*slack.Provider, *fakeRepo) {
	t.Helper()
	repo := newFakeRepo()
	client := slack.NewClientWithBaseURL(fastHTTPX(t, b.srv.Client()), b.srv.URL)
	p := slack.NewWithOAuth(repo, reversingKV{}, client, slack.OAuthConfig{
		ClientID:     "cid",
		ClientSecret: "csecret",
		RedirectURI:  "https://nexis.dev/cb",
	})
	return p, repo
}

// seedBotConnection inserts a bot-mode connection directly through the repo
// so the SendBlock / DMUserByEmail tests don't depend on the full OAuth flow.
func seedBotConnection(t *testing.T, repo *fakeRepo, orgID, channelID string) {
	t.Helper()
	enc, err := reversingKV{}.Encrypt(context.Background(), []byte("xoxb-test-bot-token"))
	require.NoError(t, err)
	require.NoError(t, repo.Upsert(context.Background(), orgID, domain.Connection{
		Provider:       domain.IntegrationSlack,
		Status:         domain.StatusConnected,
		InstallationID: "slack-T1",
		Metadata: map[string]any{
			"auth_mode":  "bot",
			"team_id":    "T1",
			"team_name":  "Acme",
			"channel_id": channelID,
		},
	}, enc))
}

func TestSendBlock_UsesClient(t *testing.T) {
	b := newBotFakeServer(t)
	p, repo := newProviderBotMode(t, b)
	seedBotConnection(t, repo, "org-1", "C-CHANNEL")

	code, err := p.SendBlock(context.Background(), "org-1", []map[string]any{
		{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "approval"}},
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	call, ok := b.findCall("/api/chat.postMessage")
	require.True(t, ok, "chat.postMessage was not called")
	require.Equal(t, "Bearer xoxb-test-bot-token", call.Token)

	var body map[string]any
	require.NoError(t, json.Unmarshal(call.Body, &body))
	require.Equal(t, "C-CHANNEL", body["channel"])
	blocks, ok := body["blocks"].([]any)
	require.True(t, ok, "blocks not an array, got %T", body["blocks"])
	require.Len(t, blocks, 1)
}

func TestDMUserByEmail_RoundTrip(t *testing.T) {
	b := newBotFakeServer(t)
	p, repo := newProviderBotMode(t, b)
	seedBotConnection(t, repo, "org-1", "C-IGNORED")

	ts, err := p.DMUserByEmail(context.Background(),
		domain.Principal{OrgID: "org-1", UserID: "u-1"},
		"alice@acme.dev",
		[]map[string]any{{"type": "section"}},
	)
	require.NoError(t, err)
	require.Equal(t, "1700000000.000001", ts)

	// The three calls must land in the documented order: lookup → openIM → post.
	paths := b.callPaths()
	require.GreaterOrEqual(t, len(paths), 3, "want >=3 calls, got %v", paths)
	require.Equal(t, []string{
		"/api/users.lookupByEmail",
		"/api/conversations.open",
		"/api/chat.postMessage",
	}, paths[:3])

	// And the post lands on the IM channel returned by conversations.open.
	postCall, ok := b.findCall("/api/chat.postMessage")
	require.True(t, ok)
	var body map[string]any
	require.NoError(t, json.Unmarshal(postCall.Body, &body))
	require.Equal(t, "D42", body["channel"])
}

func TestDMUserByEmail_PropagatesLookupError(t *testing.T) {
	b := newBotFakeServer(t)
	b.methodOverrides["users.lookupByEmail"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": false, "error": "users_not_found"}`))
	}
	p, repo := newProviderBotMode(t, b)
	seedBotConnection(t, repo, "org-1", "C-X")

	_, err := p.DMUserByEmail(context.Background(),
		domain.Principal{OrgID: "org-1"},
		"ghost@acme.dev",
		[]map[string]any{{"type": "section"}},
	)
	require.ErrorContains(t, err, "users_not_found")

	// We must NOT have called conversations.open or chat.postMessage after
	// a lookup failure.
	for _, path := range b.callPaths() {
		require.NotEqual(t, "/api/conversations.open", path)
		require.NotEqual(t, "/api/chat.postMessage", path)
	}
}

func TestStatus_BotMode_ProbesAuthTest(t *testing.T) {
	b := newBotFakeServer(t)
	p, repo := newProviderBotMode(t, b)
	seedBotConnection(t, repo, "org-1", "C-X")

	c, err := p.Status(context.Background(), domain.Principal{OrgID: "org-1"})
	require.NoError(t, err)
	require.Equal(t, domain.StatusConnected, c.Status)
	require.Equal(t, "T1", c.Metadata["team_id"])
	require.Equal(t, "Acme", c.Metadata["team_name"])
	require.Equal(t, "UB", c.Metadata["bot_user_id"])
	_, hasLatency := c.Metadata["latency_ms"]
	require.True(t, hasLatency, "Status should record latency_ms")
}

func TestStatus_BotMode_SurfacesAuthError(t *testing.T) {
	b := newBotFakeServer(t)
	b.methodOverrides["auth.test"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": false, "error": "invalid_auth"}`))
	}
	p, repo := newProviderBotMode(t, b)
	seedBotConnection(t, repo, "org-1", "C-X")

	c, err := p.Status(context.Background(), domain.Principal{OrgID: "org-1"})
	require.NoError(t, err)
	require.Equal(t, domain.StatusError, c.Status)
	require.True(t, strings.Contains(c.LastError, "invalid_auth"),
		"LastError should mention invalid_auth, got %q", c.LastError)
}

func TestConnect_BotToken_ValidatesViaAuthTest(t *testing.T) {
	b := newBotFakeServer(t)
	p, repo := newProviderBotMode(t, b)

	c, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-2"}, map[string]any{
		"bot_token":    "xoxb-manual-entry",
		"channel_name": "#alerts",
	})
	require.NoError(t, err)
	require.Equal(t, "bot", c.Metadata["auth_mode"])
	require.Equal(t, "T1", c.Metadata["team_id"])

	// The Connect call must NOT leak the bot token into metadata.
	for k, v := range c.Metadata {
		s, _ := v.(string)
		require.False(t, strings.Contains(s, "xoxb-"),
			"metadata key %q leaked bot token", k)
	}

	// And the persisted secret round-trips through KV — encrypted on disk,
	// decrypted matches the original.
	_, raw, err := repo.Get(context.Background(), "org-2", domain.IntegrationSlack)
	require.NoError(t, err)
	plain, err := reversingKV{}.Decrypt(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, "xoxb-manual-entry", string(plain))
}
