package slack_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/slack"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// recordingClient implements slack.HTTPDoer + records every request that
// flows through it. Status defaults to 200; tests override per scenario.
type recordingClient struct {
	mu       sync.Mutex
	requests []recordedRequest
	status   int
	doErr    error
}

type recordedRequest struct {
	URL    string
	Body   []byte
	Header http.Header
}

func (c *recordingClient) Do(req *http.Request) (*http.Response, error) {
	if c.doErr != nil {
		return nil, c.doErr
	}
	body, _ := io.ReadAll(req.Body)
	c.mu.Lock()
	c.requests = append(c.requests, recordedRequest{
		URL:    req.URL.String(),
		Body:   body,
		Header: req.Header.Clone(),
	})
	c.mu.Unlock()
	st := c.status
	if st == 0 {
		st = 200
	}
	return &http.Response{
		StatusCode: st,
		Body:       io.NopCloser(bytes.NewReader([]byte("ok"))),
		Header:     http.Header{},
	}, nil
}

// fakeRepo is a minimal in-memory IntegrationsRepo for the test.
type fakeRepo struct {
	mu      sync.Mutex
	rows    map[string]storedRow
}

type storedRow struct {
	conn   domain.Connection
	secret []byte
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[string]storedRow{}} }

func (r *fakeRepo) key(orgID string, p domain.IntegrationProvider) string {
	return orgID + "|" + string(p)
}

func (r *fakeRepo) Upsert(_ context.Context, orgID string, c domain.Connection, secret []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[r.key(orgID, c.Provider)] = storedRow{conn: c, secret: secret}
	return nil
}

func (r *fakeRepo) Get(_ context.Context, orgID string, p domain.IntegrationProvider) (domain.Connection, []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.rows[r.key(orgID, p)]
	if !ok {
		return domain.Connection{}, nil, domain.ErrNotFound
	}
	return row.conn, row.secret, nil
}

func (r *fakeRepo) Delete(_ context.Context, orgID string, p domain.IntegrationProvider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, r.key(orgID, p))
	return nil
}

// reversingKV is a placeholder KeyVault that "encrypts" by prefixing and
// "decrypts" by reversing. Provides round-trip equality without depending
// on the local keyvault adapter.
type reversingKV struct{}

func (reversingKV) Encrypt(_ context.Context, p []byte) ([]byte, error) {
	out := make([]byte, len(p)+4)
	copy(out, []byte("ENC:"))
	copy(out[4:], p)
	return out, nil
}
func (reversingKV) Decrypt(_ context.Context, c []byte) ([]byte, error) {
	if !bytes.HasPrefix(c, []byte("ENC:")) {
		return nil, errors.New("bad ciphertext")
	}
	return c[4:], nil
}

func TestSlack_Connect_SendsProbeAndPersistsEncrypted(t *testing.T) {
	repo := newFakeRepo()
	rec := &recordingClient{status: 200}
	p := slack.NewWithClient(repo, reversingKV{}, rec)

	princ := domain.Principal{OrgID: "org-12345678", UserID: "u-1"}
	c, err := p.Connect(context.Background(), princ, map[string]any{
		"webhook_url":  "https://hooks.slack.com/services/T/B/X",
		"channel_name": "#alerts",
	})
	require.NoError(t, err)
	require.Equal(t, domain.IntegrationSlack, c.Provider)
	require.Equal(t, domain.StatusConnected, c.Status)
	require.Equal(t, "#alerts", c.Metadata["channel_name"])

	// Webhook URL must NOT leak into metadata.
	for k, v := range c.Metadata {
		s, _ := v.(string)
		require.False(t, strings.Contains(s, "hooks.slack.com"),
			"metadata key %q leaked webhook URL", k)
	}

	// Probe request landed.
	require.Len(t, rec.requests, 1)
	require.Equal(t, "https://hooks.slack.com/services/T/B/X", rec.requests[0].URL)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.requests[0].Body, &body))
	require.Equal(t, "Nexis connected", body["text"])

	// Persisted secret is the encrypted ciphertext.
	_, raw, err := repo.Get(context.Background(), "org-12345678", domain.IntegrationSlack)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(raw, []byte("ENC:")))
}

func TestSlack_Connect_RejectsMissingURL(t *testing.T) {
	repo := newFakeRepo()
	p := slack.NewWithClient(repo, reversingKV{}, &recordingClient{status: 200})

	_, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-1"}, map[string]any{})
	require.ErrorContains(t, err, "webhook_url required")
}

func TestSlack_Connect_FailsOnProbe4xx(t *testing.T) {
	repo := newFakeRepo()
	rec := &recordingClient{status: 404}
	p := slack.NewWithClient(repo, reversingKV{}, rec)

	_, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-1"},
		map[string]any{"webhook_url": "https://hooks.slack.com/services/T/B/X"})
	require.ErrorContains(t, err, "probe failed")
	// No row persisted on probe failure.
	_, _, gerr := repo.Get(context.Background(), "org-1", domain.IntegrationSlack)
	require.ErrorIs(t, gerr, domain.ErrNotFound)
}

func TestSlack_SendBlock_PostsBlocksAndReportsStatus(t *testing.T) {
	repo := newFakeRepo()
	rec := &recordingClient{status: 200}
	p := slack.NewWithClient(repo, reversingKV{}, rec)

	// Seed a connection first.
	_, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-1"},
		map[string]any{"webhook_url": "https://hooks.slack.com/services/W"})
	require.NoError(t, err)
	rec.requests = nil // discard the probe

	code, err := p.SendBlock(context.Background(), "org-1", []map[string]any{
		{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "hi"}},
	})
	require.NoError(t, err)
	require.Equal(t, 200, code)
	require.Len(t, rec.requests, 1)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.requests[0].Body, &body))
	blocks, _ := body["blocks"].([]any)
	require.Len(t, blocks, 1)
}

func TestSlack_SendBlock_PropagatesHTTP5xx(t *testing.T) {
	repo := newFakeRepo()
	rec := &recordingClient{status: 503}
	p := slack.NewWithClient(repo, reversingKV{}, rec)
	// Seed connection — but the probe is gated on 200. Use a different
	// recordingClient for the probe by re-seeding via the repo directly.
	enc, _ := reversingKV{}.Encrypt(context.Background(), []byte("https://hooks.slack.com/services/W"))
	require.NoError(t, repo.Upsert(context.Background(), "org-1", domain.Connection{
		Provider: domain.IntegrationSlack, Status: domain.StatusConnected,
	}, enc))

	code, err := p.SendBlock(context.Background(), "org-1", []map[string]any{{"type": "section"}})
	require.ErrorContains(t, err, "http 503")
	require.Equal(t, 503, code)
}

func TestSlack_HandleWebhook_RejectsAsUnsupported(t *testing.T) {
	p := slack.NewWithClient(newFakeRepo(), reversingKV{}, &recordingClient{})
	err := p.HandleWebhook(context.Background(), "org-1", map[string]string{}, []byte(`{}`))
	require.ErrorContains(t, err, "not supported")
}

func TestSlack_Status_ReturnsConnectionWithoutSecret(t *testing.T) {
	repo := newFakeRepo()
	rec := &recordingClient{status: 200}
	p := slack.NewWithClient(repo, reversingKV{}, rec)
	_, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-1"},
		map[string]any{"webhook_url": "https://hooks.slack.com/services/W", "channel_name": "#x"})
	require.NoError(t, err)

	c, err := p.Status(context.Background(), domain.Principal{OrgID: "org-1"})
	require.NoError(t, err)
	require.Equal(t, domain.IntegrationSlack, c.Provider)
	require.Equal(t, "#x", c.Metadata["channel_name"])
}

func TestSlack_Disconnect_RemovesRow(t *testing.T) {
	repo := newFakeRepo()
	p := slack.NewWithClient(repo, reversingKV{}, &recordingClient{status: 200})
	_, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-1"},
		map[string]any{"webhook_url": "https://hooks.slack.com/services/W"})
	require.NoError(t, err)
	require.NoError(t, p.Disconnect(context.Background(), domain.Principal{OrgID: "org-1"}))
	_, _, err = repo.Get(context.Background(), "org-1", domain.IntegrationSlack)
	require.ErrorIs(t, err, domain.ErrNotFound)
}
