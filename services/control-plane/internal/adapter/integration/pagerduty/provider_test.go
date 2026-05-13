package pagerduty_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/pagerduty"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeRepo is a minimal in-memory IntegrationsRepo for the test, shaped
// identically to slack/sentry adapters' fakes so reviewers can compare
// straight across.
type fakeRepo struct {
	mu   sync.Mutex
	rows map[string]storedRow
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
// "decrypts" by stripping. Provides round-trip equality without depending on
// the adapter/keyvault package — same shape used in slack_test.
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

// fakeSink captures Insert calls so tests can assert on them.
type fakeSink struct {
	mu      sync.Mutex
	inserts []domain.RawIncident
}

func (s *fakeSink) Insert(_ context.Context, _ string, r domain.RawIncident) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inserts = append(s.inserts, r)
	return nil
}

// newWiredProvider stands up a Provider backed by a stub HTTP server, so the
// REST round-trip is exercised end-to-end. Returns (p, repo, sink, server,
// requestLog) — the requestLog records inbound requests so tests can assert
// on path/headers/body of the upstream call.
type recordedRequest struct {
	Path   string
	Method string
	Body   []byte
	Header http.Header
}

type requestLog struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (l *requestLog) record(r *http.Request) {
	body, _ := readAndReset(r)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, recordedRequest{
		Path: r.URL.Path, Method: r.Method, Body: body, Header: r.Header.Clone(),
	})
}

func readAndReset(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	b := new(bytes.Buffer)
	if _, err := b.ReadFrom(r.Body); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func newWiredProvider(t *testing.T, handler http.HandlerFunc) (
	*pagerduty.Provider, *fakeRepo, *fakeSink, *httptest.Server, *requestLog,
) {
	t.Helper()
	log := &requestLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	httpc := httpx.New(srv.Client(), httpx.Config{
		RatePerSec: 100, Burst: 100, MaxAttempts: 1, BreakerThreshold: 100,
	})
	client := pagerduty.NewClientWithBaseURL(httpc, srv.URL)

	repo := newFakeRepo()
	sink := &fakeSink{}
	p := pagerduty.New(repo, reversingKV{}, sink, "ops@example.com")
	p.SetClient(client)
	return p, repo, sink, srv, log
}

func TestConnect_ValidatesToken(t *testing.T) {
	p, repo, _, _, log := newWiredProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]any{"id": "PUSR1", "name": "Ada", "email": "ada@x.io", "role": "admin"},
		})
	})

	princ := domain.Principal{OrgID: "org-1", UserID: "u-1"}
	conn, err := p.Connect(context.Background(), princ, map[string]any{
		"api_token":            "real-token",
		"service_id":           "PSVC1",
		"escalation_policy_id": "POL1",
	})
	require.NoError(t, err)
	require.Equal(t, domain.IntegrationPagerDuty, conn.Provider)
	require.Equal(t, domain.StatusConnected, conn.Status)
	require.Equal(t, "PUSR1", conn.InstallationID)
	require.Equal(t, "PSVC1", conn.Metadata["service_id"])
	require.Equal(t, "POL1", conn.Metadata["escalation_policy_id"])
	require.Equal(t, "ada@x.io", conn.Metadata["user_email"])

	// The token must NOT leak into metadata anywhere.
	for k, v := range conn.Metadata {
		if s, ok := v.(string); ok {
			require.NotContains(t, s, "real-token", "metadata key %q leaked api_token", k)
		}
	}

	// Confirm token validation HTTP call landed on /users/me with the v2 Accept.
	require.Len(t, log.requests, 1)
	require.Equal(t, "/users/me", log.requests[0].Path)
	require.Equal(t, "Token token=real-token", log.requests[0].Header.Get("Authorization"))

	// Stored secret is encrypted JSON — the encryption prefix is there and
	// the token is not in plaintext metadata.
	_, raw, gerr := repo.Get(context.Background(), "org-1", domain.IntegrationPagerDuty)
	require.NoError(t, gerr)
	require.True(t, bytes.HasPrefix(raw, []byte("ENC:")))
	// Decoding the inner JSON should yield the full triple.
	plain := raw[4:]
	var seen struct {
		APIToken           string `json:"api_token"`
		ServiceID          string `json:"service_id"`
		EscalationPolicyID string `json:"escalation_policy_id"`
	}
	require.NoError(t, json.Unmarshal(plain, &seen))
	require.Equal(t, "real-token", seen.APIToken)
	require.Equal(t, "PSVC1", seen.ServiceID)
	require.Equal(t, "POL1", seen.EscalationPolicyID)
}

func TestConnect_RejectsMissingFields(t *testing.T) {
	p, _, _, _, _ := newWiredProvider(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be reached")
	})
	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"no token", map[string]any{"service_id": "S1"}, "api_token required"},
		{"no service", map[string]any{"api_token": "t"}, "service_id required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := p.Connect(context.Background(), domain.Principal{OrgID: "o"}, tc.cfg)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestConnect_RefusesOn401(t *testing.T) {
	p, repo, _, _, _ := newWiredProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad token"}}`))
	})
	_, err := p.Connect(context.Background(), domain.Principal{OrgID: "org-1"}, map[string]any{
		"api_token": "no-good", "service_id": "S1",
	})
	require.Error(t, err)
	// And no row should be persisted on validation failure.
	_, _, gerr := repo.Get(context.Background(), "org-1", domain.IntegrationPagerDuty)
	require.ErrorIs(t, gerr, domain.ErrNotFound)
}

func TestHandleWebhook_PublishesIncidentDetected(t *testing.T) {
	repo := newFakeRepo()
	sink := &fakeSink{}
	p := pagerduty.New(repo, reversingKV{}, sink, "ops@example.com")
	secret := []byte("rotating-secret-A")
	p.SetWebhookSecrets([][]byte{secret})

	body := []byte(`{
		"event": {
			"id": "EVT1",
			"event_type": "incident.triggered",
			"data": {
				"id": "INC-42",
				"title": "Disk full on api-01",
				"urgency": "high",
				"service": { "summary": "api" }
			}
		}
	}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := "v1=" + hex.EncodeToString(mac.Sum(nil))

	err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"X-PagerDuty-Signature": sig}, body)
	require.NoError(t, err)
	require.Len(t, sink.inserts, 1)
	got := sink.inserts[0]
	require.Equal(t, "pagerduty", got.Source)
	require.Equal(t, "INC-42", got.SourceEventID)
	require.Equal(t, "Disk full on api-01", got.Title)
	require.Equal(t, "high", got.Level)
	require.Equal(t, "api", got.Service)
}

func TestHandleWebhook_RotationAccepts2ndSecret(t *testing.T) {
	// Operator is mid-rotation: the new secret is in the rotation list and
	// the upstream is signing under it. The old secret is still configured
	// but the body is NOT signed by it. Verify ANY-match policy.
	repo := newFakeRepo()
	sink := &fakeSink{}
	p := pagerduty.New(repo, reversingKV{}, sink, "ops@example.com")
	old := []byte("old-secret")
	current := []byte("rotated-secret")
	p.SetWebhookSecrets([][]byte{old, current})

	body := []byte(`{"event":{"id":"e","event_type":"incident.triggered","data":{"id":"I"}}}`)
	mac := hmac.New(sha256.New, current)
	mac.Write(body)
	sig := "v1=" + hex.EncodeToString(mac.Sum(nil))

	require.NoError(t, p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"X-PagerDuty-Signature": sig}, body))
	require.Len(t, sink.inserts, 1)
}

func TestHandleWebhook_RejectsBadHMAC(t *testing.T) {
	repo := newFakeRepo()
	sink := &fakeSink{}
	p := pagerduty.New(repo, reversingKV{}, sink, "ops@example.com")
	p.SetWebhookSecrets([][]byte{[]byte("secret")})

	err := p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"X-PagerDuty-Signature": "v1=deadbeef"}, []byte(`{}`))
	require.Error(t, err)
	require.Len(t, sink.inserts, 0)
}

func TestHandleWebhook_IgnoresNonTriggerEvents(t *testing.T) {
	repo := newFakeRepo()
	sink := &fakeSink{}
	p := pagerduty.New(repo, reversingKV{}, sink, "ops@example.com")
	secret := []byte("k")
	p.SetWebhookSecrets([][]byte{secret})

	body := []byte(`{"event":{"id":"e","event_type":"incident.resolved","data":{"id":"I"}}}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := "v1=" + hex.EncodeToString(mac.Sum(nil))

	require.NoError(t, p.HandleWebhook(context.Background(), "org-1",
		map[string]string{"X-PagerDuty-Signature": sig}, body))
	require.Len(t, sink.inserts, 0)
}

func TestEscalate_TriggersWithDedup(t *testing.T) {
	// Sequence the upstream responses: first call is /users/me (Connect),
	// second is POST /incidents (Escalate).
	var callIdx int
	p, _, _, _, log := newWiredProvider(t, func(w http.ResponseWriter, r *http.Request) {
		callIdx++
		switch r.URL.Path {
		case "/users/me":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user": map[string]any{"id": "U1", "name": "Ada", "email": "ada@x.io"},
			})
		case "/incidents":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"incident":{"id":"INC-99"}}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	})

	princ := domain.Principal{OrgID: "org-1"}
	_, err := p.Connect(context.Background(), princ, map[string]any{
		"api_token": "tok", "service_id": "PSVC42",
	})
	require.NoError(t, err)

	id, err := p.Escalate(context.Background(), princ,
		"Sentinel: payment-api 5xx burst",
		"5xx rate > 2% for 10m on payment-api",
		"sentinel-payment-api-5xx",
	)
	require.NoError(t, err)
	require.Equal(t, "INC-99", id)

	// Find the POST /incidents request and assert dedup key + service id.
	var incReq *recordedRequest
	for i := range log.requests {
		if log.requests[i].Path == "/incidents" {
			incReq = &log.requests[i]
		}
	}
	require.NotNil(t, incReq, "expected POST /incidents")
	require.Equal(t, http.MethodPost, incReq.Method)
	require.Equal(t, "ops@example.com", incReq.Header.Get("From"))
	require.Equal(t, "Token token=tok", incReq.Header.Get("Authorization"))

	var got map[string]any
	require.NoError(t, json.Unmarshal(incReq.Body, &got))
	inc, _ := got["incident"].(map[string]any)
	require.NotNil(t, inc, "incident envelope missing")
	require.Equal(t, "sentinel-payment-api-5xx", inc["incident_key"])
	svc, _ := inc["service"].(map[string]any)
	require.Equal(t, "PSVC42", svc["id"])
	require.Equal(t, "high", inc["urgency"])
}

func TestEscalate_RequiresConnection(t *testing.T) {
	p, _, _, _, _ := newWiredProvider(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be reached when no connection")
	})
	_, err := p.Escalate(context.Background(), domain.Principal{OrgID: "no-such"},
		"t", "b", "k")
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestOnCallFor_ReturnsConfiguredUser(t *testing.T) {
	p, _, _, _, _ := newWiredProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/me":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user": map[string]any{"id": "U1", "email": "ada@x.io"},
			})
		case "/oncalls":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"oncalls": []map[string]any{
					{"user": map[string]any{"id": "U-OC", "summary": "On-Call Ada"}},
				},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	})

	princ := domain.Principal{OrgID: "org-1"}
	_, err := p.Connect(context.Background(), princ, map[string]any{
		"api_token": "tok", "service_id": "S1", "escalation_policy_id": "POL1",
	})
	require.NoError(t, err)

	u, err := p.OnCallFor(context.Background(), princ)
	require.NoError(t, err)
	require.Equal(t, "U-OC", u.ID)
	require.Equal(t, "On-Call Ada", u.Name)
}

func TestOnCallFor_RequiresPolicyID(t *testing.T) {
	p, _, _, _, _ := newWiredProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]any{"id": "U1", "email": "ada@x.io"},
		})
	})
	princ := domain.Principal{OrgID: "org-1"}
	_, err := p.Connect(context.Background(), princ, map[string]any{
		"api_token": "tok", "service_id": "S1",
	})
	require.NoError(t, err)
	_, err = p.OnCallFor(context.Background(), princ)
	require.ErrorContains(t, err, "escalation_policy_id")
}

func TestDisconnect_RemovesRow(t *testing.T) {
	p, repo, _, _, _ := newWiredProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]any{"id": "U1", "email": "ada@x.io"},
		})
	})
	princ := domain.Principal{OrgID: "org-1"}
	_, err := p.Connect(context.Background(), princ, map[string]any{
		"api_token": "t", "service_id": "S1",
	})
	require.NoError(t, err)
	require.NoError(t, p.Disconnect(context.Background(), princ))
	_, _, err = repo.Get(context.Background(), "org-1", domain.IntegrationPagerDuty)
	require.ErrorIs(t, err, domain.ErrNotFound)
}
