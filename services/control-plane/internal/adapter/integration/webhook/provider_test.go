package webhook

// The signing secret is the whole trust boundary for this provider — there is
// no vendor API behind it — so these tests pin the signature path hard:
// a good signature lands exactly one incident, and every way of getting the
// signature wrong lands none.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// --- fakes ------------------------------------------------------------------

type fakeRepo struct {
	conn      domain.Connection
	secret    []byte
	getErr    error
	upsertErr error
	deleted   bool
}

func (f *fakeRepo) Upsert(_ context.Context, _ string, c domain.Connection, secret []byte) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.conn, f.secret = c, secret
	return nil
}

func (f *fakeRepo) Get(_ context.Context, _ string, _ domain.IntegrationProvider) (domain.Connection, []byte, error) {
	if f.getErr != nil {
		return domain.Connection{}, nil, f.getErr
	}
	return f.conn, f.secret, nil
}

func (f *fakeRepo) Delete(_ context.Context, _ string, _ domain.IntegrationProvider) error {
	f.deleted = true
	return nil
}

// plainKV is an identity KeyVault — the encryption itself is covered by the
// keyvault package's own tests; here it would only obscure the assertions.
type plainKV struct{}

func (plainKV) Encrypt(_ context.Context, b []byte) ([]byte, error) { return b, nil }
func (plainKV) Decrypt(_ context.Context, b []byte) ([]byte, error) { return b, nil }

type fakeSink struct {
	got []domain.RawIncident
}

func (f *fakeSink) Insert(_ context.Context, _ string, raw domain.RawIncident) error {
	f.got = append(f.got, raw)
	return nil
}

const testOrg = "11111111-2222-3333-4444-555555555555"

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func newProvider(secret string) (*Provider, *fakeRepo, *fakeSink) {
	r := &fakeRepo{secret: []byte(secret), conn: domain.Connection{
		Provider: domain.IntegrationWebhook, Status: domain.StatusConnected,
	}}
	s := &fakeSink{}
	return New(r, plainKV{}, s), r, s
}

// --- Connect ----------------------------------------------------------------

func TestConnect_GeneratesSecretAndReturnsItExactlyOnce(t *testing.T) {
	r := &fakeRepo{}
	p := New(r, plainKV{}, &fakeSink{})
	princ := domain.Principal{OrgID: testOrg}

	conn, err := p.Connect(context.Background(), princ, map[string]any{})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	secret, _ := conn.Metadata["signing_secret"].(string)
	if len(secret) != secretLen*2 { // hex encoding doubles the byte length
		t.Fatalf("generated secret length = %d, want %d", len(secret), secretLen*2)
	}
	if string(r.secret) != secret {
		t.Fatal("persisted secret differs from the one handed to the caller")
	}
	if conn.Status != domain.StatusConnected {
		t.Fatalf("status = %q, want connected", conn.Status)
	}
	if got, _ := conn.Metadata["webhook_path"].(string); got != "/v1/webhooks/webhook/"+testOrg {
		t.Fatalf("webhook_path = %q", got)
	}

	// Status must never echo the secret back.
	st, err := p.Status(context.Background(), princ)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if _, leaked := st.Metadata["signing_secret"]; leaked {
		t.Fatal("Status leaked signing_secret")
	}
}

func TestConnect_HonoursSuppliedSecretAndDoesNotEchoIt(t *testing.T) {
	r := &fakeRepo{}
	p := New(r, plainKV{}, &fakeSink{})
	const supplied = "an-operator-supplied-secret-value"

	conn, err := p.Connect(context.Background(), domain.Principal{OrgID: testOrg},
		map[string]any{"signing_secret": supplied, "label": "Checkout API"})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if string(r.secret) != supplied {
		t.Fatalf("persisted %q, want %q", r.secret, supplied)
	}
	if _, echoed := conn.Metadata["signing_secret"]; echoed {
		t.Fatal("Connect echoed an operator-supplied secret")
	}
	if got, _ := conn.Metadata["label"].(string); got != "Checkout API" {
		t.Fatalf("label = %q", got)
	}
}

func TestConnect_RejectsShortSecret(t *testing.T) {
	p := New(&fakeRepo{}, plainKV{}, &fakeSink{})
	_, err := p.Connect(context.Background(), domain.Principal{OrgID: testOrg},
		map[string]any{"signing_secret": "too-short"})
	if err == nil {
		t.Fatal("expected an error for a sub-16-character secret")
	}
}

// --- HandleWebhook ----------------------------------------------------------

func TestHandleWebhook_ValidSignatureLandsOneFatalIncident(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	p, _, sink := newProvider(secret)

	body, _ := json.Marshal(map[string]any{
		"title":      "ZeroDivisionError: division by zero in unit_price",
		"service":    "checkout-api",
		"event_id":   "evt-1",
		"stacktrace": "Traceback…\n  File \"src/nexis_fixture/pricing.py\", line 14",
		"logs":       "ts=… level=error msg=unhandled_exception",
		"metadata":   map[string]any{"root_cause_node": "nexis_fixture.pricing.unit_price"},
	})

	for _, hdr := range []map[string]string{
		{SignatureHeader: sign(secret, body)},
		{SignatureHeader: "sha256=" + sign(secret, body)},      // prefixed form
		{SignatureHeader: strings.ToUpper(sign(secret, body))}, // upper-case hex
	} {
		sink.got = nil
		if err := p.HandleWebhook(context.Background(), testOrg, hdr, body); err != nil {
			t.Fatalf("HandleWebhook(%v): %v", hdr, err)
		}
		if len(sink.got) != 1 {
			t.Fatalf("incidents inserted = %d, want 1", len(sink.got))
		}
		raw := sink.got[0]
		if raw.Level != "fatal" {
			t.Errorf("level = %q, want fatal (Sentinel's per-row rule keys off it)", raw.Level)
		}
		if raw.Source != "webhook" {
			t.Errorf("source = %q, want webhook", raw.Source)
		}
		if raw.Service != "checkout-api" || raw.Environment != "production" {
			t.Errorf("service/env = %q/%q", raw.Service, raw.Environment)
		}
		if raw.SourceEventID != "evt-1" {
			t.Errorf("source_event_id = %q", raw.SourceEventID)
		}
	}
}

func TestHandleWebhook_RejectsBadSignatures(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	body := []byte(`{"title":"boom"}`)

	cases := map[string]map[string]string{
		"no signature header": {},
		"empty signature":     {SignatureHeader: ""},
		"wrong secret":        {SignatureHeader: sign("not-the-secret-not-the-secret", body)},
		"not hex":             {SignatureHeader: "zzzz"},
		"truncated":           {SignatureHeader: sign(secret, body)[:32]},
	}
	for name, hdr := range cases {
		t.Run(name, func(t *testing.T) {
			p, _, sink := newProvider(secret)
			err := p.HandleWebhook(context.Background(), testOrg, hdr, body)
			if err == nil {
				t.Fatal("expected rejection, got nil error")
			}
			if len(sink.got) != 0 {
				t.Fatalf("inserted %d incidents on a rejected delivery", len(sink.got))
			}
		})
	}
}

// A signature over different bytes than were sent must not pass — this is the
// replay/tamper case, not just a typo case.
func TestHandleWebhook_SignatureIsOverTheExactBody(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	p, _, sink := newProvider(secret)

	signed := []byte(`{"title":"harmless"}`)
	sent := []byte(`{"title":"tampered"}`)
	err := p.HandleWebhook(context.Background(), testOrg,
		map[string]string{SignatureHeader: sign(secret, signed)}, sent)
	if err == nil || !strings.Contains(err.Error(), "HMAC mismatch") {
		t.Fatalf("err = %v, want an HMAC mismatch", err)
	}
	if len(sink.got) != 0 {
		t.Fatal("tampered body was accepted")
	}
}

func TestHandleWebhook_RequiresTitleAndValidJSON(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	for name, body := range map[string][]byte{
		"missing title": []byte(`{"service":"api"}`),
		"blank title":   []byte(`{"title":"   "}`),
		"not json":      []byte(`{nope`),
	} {
		t.Run(name, func(t *testing.T) {
			p, _, sink := newProvider(secret)
			err := p.HandleWebhook(context.Background(), testOrg,
				map[string]string{SignatureHeader: sign(secret, body)}, body)
			if err == nil {
				t.Fatal("expected an error")
			}
			if len(sink.got) != 0 {
				t.Fatal("inserted an incident for an invalid payload")
			}
		})
	}
}

func TestHandleWebhook_DefaultsAndOverrides(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	p, _, sink := newProvider(secret)

	body := []byte(`{"title":"slow query","level":"warning","environment":"staging"}`)
	if err := p.HandleWebhook(context.Background(), testOrg,
		map[string]string{SignatureHeader: sign(secret, body)}, body); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	raw := sink.got[0]
	if raw.Level != "warning" {
		t.Errorf("level = %q, want the supplied warning", raw.Level)
	}
	if raw.Environment != "staging" {
		t.Errorf("environment = %q", raw.Environment)
	}
	if raw.Service != "unknown" {
		t.Errorf("service = %q, want the unknown default", raw.Service)
	}
}

func TestHandleWebhook_NoConnectionForOrg(t *testing.T) {
	r := &fakeRepo{} // secret nil → no connection stored
	p := New(r, plainKV{}, &fakeSink{})
	body := []byte(`{"title":"boom"}`)
	err := p.HandleWebhook(context.Background(), testOrg,
		map[string]string{SignatureHeader: sign("whatever-secret-here", body)}, body)
	if err == nil || !strings.Contains(err.Error(), "no connection") {
		t.Fatalf("err = %v, want a no-connection error", err)
	}
}

func TestName(t *testing.T) {
	if got := New(&fakeRepo{}, plainKV{}, nil).Name(); got != domain.IntegrationWebhook {
		t.Fatalf("Name() = %q", got)
	}
}
