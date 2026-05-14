package handler

// Unit coverage for the webhooks helper functions. These are pure (no DB,
// no HTTP plumbing), so table-driven tests are sufficient. The handler-side
// integration cases (Webhook / WebhookByQuery happy/sad path, idempotency
// dedupe) require a Postgres pool and live under tests/integration.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// TestPayloadEventType walks every priority branch in the sniffer. The header
// path beats the body path; first-known header wins. The body fallbacks land
// when no provider header is present.
func TestPayloadEventType(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		headers  map[string]string
		body     []byte
		want     string
	}{
		{
			name:     "github_header",
			provider: "github",
			headers:  map[string]string{"X-Github-Event": "pull_request"},
			want:     "pull_request",
		},
		{
			name:     "gitlab_header",
			provider: "gitlab",
			headers:  map[string]string{"X-Gitlab-Event": "Push Hook"},
			want:     "Push Hook",
		},
		{
			name:     "sentry_header",
			provider: "sentry",
			headers:  map[string]string{"Sentry-Hook-Resource": "event_alert"},
			want:     "event_alert",
		},
		{
			name:     "pagerduty_header",
			provider: "pagerduty",
			headers:  map[string]string{"X-Pagerduty-Webhook-Type": "incident.triggered"},
			want:     "incident.triggered",
		},
		{
			name:     "datadog_header",
			provider: "datadog",
			headers:  map[string]string{"X-Datadog-Event-Type": "alert"},
			want:     "alert",
		},
		{
			name:     "body_event",
			provider: "unknown",
			body:     []byte(`{"event": "user.created"}`),
			want:     "user.created",
		},
		{
			name:     "body_type",
			provider: "unknown",
			body:     []byte(`{"type": "issue.opened"}`),
			want:     "issue.opened",
		},
		{
			name:     "body_action",
			provider: "unknown",
			body:     []byte(`{"action": "merged"}`),
			want:     "merged",
		},
		{
			name:     "fallback_provider_label",
			provider: "datadog",
			body:     []byte(``),
			want:     "datadog.webhook",
		},
		{
			name:     "fallback_unparseable_body",
			provider: "sentry",
			body:     []byte(`not-json-at-all`),
			want:     "sentry.webhook",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := payloadEventType(tc.provider, tc.headers, tc.body)
			if got != tc.want {
				t.Fatalf("payloadEventType: got %q want %q", got, tc.want)
			}
		})
	}
}

// TestClientIPFromRequest covers all three IP-derivation paths: the
// X-Forwarded-For last-entry path, the SplitHostPort fall-through, and the
// raw RemoteAddr fallback when SplitHostPort fails.
func TestClientIPFromRequest(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		header     string
		want       string
	}{
		{
			name:       "xff_single",
			header:     "1.2.3.4",
			remoteAddr: "10.0.0.1:5555",
			want:       "1.2.3.4",
		},
		{
			name:       "xff_chain_picks_last",
			header:     "1.2.3.4, 10.0.0.5, 192.168.1.1",
			remoteAddr: "10.0.0.1:5555",
			want:       "192.168.1.1",
		},
		{
			name:       "xff_chain_trailing_blank",
			header:     "1.2.3.4, ",
			remoteAddr: "10.0.0.1:5555",
			want:       "1.2.3.4",
		},
		{
			name:       "no_xff_splithostport",
			remoteAddr: "10.0.0.1:5555",
			want:       "10.0.0.1",
		},
		{
			name:       "no_xff_v6",
			remoteAddr: "[2001:db8::1]:9999",
			want:       "2001:db8::1",
		},
		{
			name:       "splithostport_fails",
			remoteAddr: "raw-not-host-port",
			want:       "raw-not-host-port",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/x", nil)
			if tc.header != "" {
				req.Header.Set("X-Forwarded-For", tc.header)
			}
			req.RemoteAddr = tc.remoteAddr
			got := clientIPFromRequest(req)
			if got != tc.want {
				t.Fatalf("clientIPFromRequest: got %q want %q", got, tc.want)
			}
		})
	}
}

// TestBodyAsJSONForAudit covers the three branches: valid JSON → map,
// non-JSON → synthetic envelope with raw_head_utf8, oversize body → truncated
// flag set.
func TestBodyAsJSONForAudit(t *testing.T) {
	t.Run("valid_json", func(t *testing.T) {
		got := bodyAsJSONForAudit([]byte(`{"foo":"bar","n":1}`))
		if got["foo"] != "bar" {
			t.Fatalf("expected foo=bar in result: %+v", got)
		}
		if _, exists := got["raw_head_utf8"]; exists {
			t.Fatalf("raw_head_utf8 should not appear on valid JSON: %+v", got)
		}
	})
	t.Run("invalid_json", func(t *testing.T) {
		got := bodyAsJSONForAudit([]byte(`not really json`))
		if got["raw_head_utf8"] != "not really json" {
			t.Fatalf("raw_head_utf8 missing: %+v", got)
		}
		if got["truncated"] != false {
			t.Fatalf("truncated should be false for short body: %+v", got)
		}
	})
	t.Run("oversize_truncated", func(t *testing.T) {
		big := make([]byte, maxWebhookPayloadBytes+512)
		for i := range big {
			big[i] = 'A'
		}
		got := bodyAsJSONForAudit(big)
		if got["truncated"] != true {
			t.Fatalf("truncated must be true for oversize body: %+v", got)
		}
		head, ok := got["raw_head_utf8"].(string)
		if !ok || len(head) != maxWebhookPayloadBytes {
			t.Fatalf("raw_head_utf8 length: ok=%v got=%d want=%d", ok, len(head), maxWebhookPayloadBytes)
		}
	})
	t.Run("null_value_in_json", func(t *testing.T) {
		got := bodyAsJSONForAudit([]byte(`null`))
		// JSON `null` unmarshals to a nil map → fallback envelope.
		if got["raw_head_utf8"] != "null" {
			t.Fatalf("null body should fall through to envelope: %+v", got)
		}
	})
}

// TestExtractProviderEventID covers every provider's id source. For each
// provider we check the success path (id present) AND one fallback path
// (no id → empty string return so the caller knows to skip dedupe).
func TestExtractProviderEventID(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		hdrs     map[string]string
		body     []byte
		want     string
	}{
		{
			name:     "github_header",
			provider: "github",
			hdrs:     map[string]string{"X-Github-Delivery": "deliv-xyz-1"},
			want:     "deliv-xyz-1",
		},
		{
			name:     "github_missing",
			provider: "github",
			hdrs:     map[string]string{},
			want:     "",
		},
		{
			name:     "datadog_header",
			provider: "datadog",
			hdrs:     map[string]string{"X-Datadog-Request-Id": "dd-req-001"},
			want:     "dd-req-001",
		},
		{
			name:     "datadog_body_fallback",
			provider: "datadog",
			hdrs:     map[string]string{},
			body:     []byte(`{"request_id":"dd-from-body"}`),
			want:     "dd-from-body",
		},
		{
			name:     "sentry_body_event_id",
			provider: "sentry",
			body:     []byte(`{"event_id":"evt-sentry-1"}`),
			want:     "evt-sentry-1",
		},
		{
			name:     "slack_body_event_id",
			provider: "slack",
			body:     []byte(`{"event_id":"evt-slack-1"}`),
			want:     "evt-slack-1",
		},
		{
			name:     "pagerduty_body_id",
			provider: "pagerduty",
			body:     []byte(`{"id":"pd-event-1"}`),
			want:     "pd-event-1",
		},
		{
			name:     "unknown_provider",
			provider: "unknown",
			body:     []byte(`{"id":"x"}`),
			want:     "",
		},
		{
			name:     "id_with_whitespace_trimmed",
			provider: "pagerduty",
			body:     []byte(`{"id":"  trim-me  "}`),
			want:     "trim-me",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractProviderEventID(tc.provider, tc.hdrs, tc.body)
			if got != tc.want {
				t.Fatalf("extractProviderEventID: got %q want %q", got, tc.want)
			}
		})
	}
}

// TestStashIdempotencyKey — nil header maps and empty event ids are no-ops.
// Non-empty event ids stamp the canonical Idempotency-Key entry.
func TestStashIdempotencyKey(t *testing.T) {
	t.Run("nil_headers_no_panic", func(t *testing.T) {
		stashIdempotencyKey(nil, "deliv-1")
	})
	t.Run("empty_event_id_no_op", func(t *testing.T) {
		h := map[string]string{}
		stashIdempotencyKey(h, "")
		if _, ok := h["Idempotency-Key"]; ok {
			t.Fatalf("empty event id should NOT set the key")
		}
	})
	t.Run("stamps_key", func(t *testing.T) {
		h := map[string]string{"X-Foo": "bar"}
		stashIdempotencyKey(h, "deliv-1")
		if h["Idempotency-Key"] != "deliv-1" {
			t.Fatalf("idempotency key: %+v", h)
		}
		if h["X-Foo"] != "bar" {
			t.Fatalf("existing header dropped: %+v", h)
		}
	})
}

// TestIsHMACError matches the canonical signature-failure strings every
// adapter returns. Anything else returns false so a generic 500 still maps to
// 500 instead of 401.
func TestIsHMACError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"hmac_mismatch", errors.New("github: HMAC mismatch"), true},
		{"missing_xhub", errors.New("missing X-Hub-Signature header"), true},
		{"missing_sentry_sig", errors.New("missing Sentry-Hook-Signature"), true},
		{"missing_argocd_sig", errors.New("missing argocd webhook signature: dropped"), true},
		{"missing_datadog_sig", errors.New("missing X-Datadog-Signature header"), true},
		{"missing_generic", errors.New("missing webhook signature payload"), true},
		{"missing_pagerduty", errors.New("missing X-PagerDuty-Signature value"), true},
		{"unrelated", errors.New("postgres conn refused"), false},
		{"empty", errors.New(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isHMACError(tc.err)
			if got != tc.want {
				t.Fatalf("isHMACError: got %v want %v err=%q", got, tc.want, tc.err.Error())
			}
		})
	}
}

// TestContainsAny / TestIndexOfSub — the substring helpers. Both are nil-safe
// on empty targets and handle the empty-string edge case.
func TestContainsAny(t *testing.T) {
	if !containsAny("hello world", []string{"foo", "world"}) {
		t.Fatalf("containsAny: expected world to match")
	}
	if containsAny("hello world", []string{"foo", "bar"}) {
		t.Fatalf("containsAny: expected no match")
	}
	if containsAny("hello", []string{""}) {
		t.Fatalf("empty target must be skipped")
	}
}

func TestIndexOfSub(t *testing.T) {
	if idx := indexOfSub("hello world", "world"); idx != 6 {
		t.Fatalf("indexOfSub: got %d want 6", idx)
	}
	if idx := indexOfSub("hello", "world"); idx != -1 {
		t.Fatalf("indexOfSub miss: got %d want -1", idx)
	}
	if idx := indexOfSub("abc", "abcd"); idx != -1 {
		t.Fatalf("longer sub must miss: got %d want -1", idx)
	}
}

// TestValidateOrgID_NilPoolFallsBackToUUID — when no admin pool is wired
// (dev/no-DB boot path), validateOrgID accepts any well-formed UUID and
// rejects malformed strings.
func TestValidateOrgID_NilPoolFallsBackToUUID(t *testing.T) {
	t.Run("valid_uuid_accepted", func(t *testing.T) {
		if !validateOrgID(nil, nil, "550e8400-e29b-41d4-a716-446655440000") {
			t.Fatalf("valid uuid must pass when pool is nil")
		}
	})
	t.Run("malformed_rejected", func(t *testing.T) {
		if validateOrgID(nil, nil, "not-a-uuid") {
			t.Fatalf("malformed uuid must be rejected")
		}
	})
	t.Run("empty_rejected", func(t *testing.T) {
		if validateOrgID(nil, nil, "") {
			t.Fatalf("empty org id must be rejected")
		}
	})
}

// TestWebhookByQuery_RejectsInvalidUUIDOrgID is a wire-level sanity check
// that the early validation gate fires when the query org_id is malformed.
// We can run this without a registry — the validator runs before the
// registry lookup.
//
// We don't have a *pgxpool.Pool here so the path falls back to UUID-only
// validation (pool=nil branch in validateOrgID).
func TestWebhookByQuery_RejectsInvalidUUIDOrgID(t *testing.T) {
	// Build a request to the WebhookByQuery handler with a bogus org id.
	// The handler is registered with a nil registry — that path is reached
	// only on a successful org id check, so this exercises the early-reject
	// path explicitly.
	h := WebhookByQuery("datadog", nil, nil, nil, config.Config{})
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/datadog?org=not-a-uuid", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("WebhookByQuery invalid uuid: status=%d body=%s want=400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_org_id") {
		t.Fatalf("expected invalid_org_id in body, got %s", rec.Body.String())
	}
}

// TestWebhookByQuery_MissingOrgIs404 — the documented contract says a missing
// `?org=` returns 404 (look like a routing miss to the upstream so it
// stops retrying).
func TestWebhookByQuery_MissingOrgIs404(t *testing.T) {
	h := WebhookByQuery("datadog", nil, nil, nil, config.Config{})
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/datadog", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("WebhookByQuery missing org: status=%d body=%s want=404", rec.Code, rec.Body.String())
	}
}
