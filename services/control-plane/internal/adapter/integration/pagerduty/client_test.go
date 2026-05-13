package pagerduty

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
)

// newTestClient wraps an httptest.Server's URL into a real Client backed by
// a real httpx.Client. The httpx config is squeezed (high rate limit, single
// attempt, fast backoff) so the tests stay deterministic and quick.
func newTestClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	httpc := httpx.New(srv.Client(), httpx.Config{
		RatePerSec:       100,
		Burst:            100,
		MaxAttempts:      1,
		BreakerThreshold: 100,
	})
	return NewClientWithBaseURL(httpc, srv.URL), srv
}

func TestMeOK_HappyPath(t *testing.T) {
	var seenAuth, seenAccept string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/me" {
			t.Errorf("path = %q", r.URL.Path)
		}
		seenAuth = r.Header.Get("Authorization")
		seenAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]any{
				"id":    "PUSER1",
				"name":  "Ada Lovelace",
				"email": "ada@example.com",
				"role":  "admin",
			},
		})
	}))

	user, err := c.MeOK(context.Background(), "token123")
	if err != nil {
		t.Fatalf("MeOK: %v", err)
	}
	if user.ID != "PUSER1" || user.Name != "Ada Lovelace" {
		t.Errorf("user = %+v", user)
	}
	if user.Email != "ada@example.com" || user.Role != "admin" {
		t.Errorf("user contact/role = %+v", user)
	}
	if seenAuth != "Token token=token123" {
		t.Errorf("Authorization = %q, want Token token=token123", seenAuth)
	}
	if !strings.Contains(seenAccept, "vnd.pagerduty+json;version=2") {
		t.Errorf("Accept = %q, want v2 pin", seenAccept)
	}
}

func TestMeOK_401Unauthorized(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Authentication failed"}}`))
	}))

	_, err := c.MeOK(context.Background(), "bad-token")
	if err == nil {
		t.Fatal("MeOK: want error, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.Status != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", apiErr.Status)
	}
	if !strings.Contains(apiErr.Message, "Authentication failed") {
		t.Errorf("Message = %q, want 'Authentication failed'", apiErr.Message)
	}
}

func TestMeOK_RejectsEmptyToken(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be reached for empty token")
	}))
	_, err := c.MeOK(context.Background(), "")
	if err == nil {
		t.Fatal("MeOK: want error for empty token")
	}
}

func TestWhoIsOnCall_ReturnsFirstUser(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oncalls" {
			t.Errorf("path = %q", r.URL.Path)
		}
		// PagerDuty packs ids into repeated query params; with our URL builder
		// the bracketed form ends up url-decoded as `escalation_policy_ids[]`.
		q := r.URL.Query()
		if got := q.Get("escalation_policy_ids[]"); got != "POLICY1" {
			t.Errorf("policy query = %q, want POLICY1", got)
		}
		if got := q.Get("limit"); got != "1" {
			t.Errorf("limit = %q, want 1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"oncalls": []map[string]any{
				{"user": map[string]any{"id": "PUSR-FIRST", "summary": "First Responder"}},
				{"user": map[string]any{"id": "PUSR-SECOND", "summary": "Backup"}},
			},
		})
	}))

	user, err := c.WhoIsOnCall(context.Background(), "tok", "POLICY1")
	if err != nil {
		t.Fatalf("WhoIsOnCall: %v", err)
	}
	if user.ID != "PUSR-FIRST" {
		t.Errorf("ID = %q, want PUSR-FIRST", user.ID)
	}
	if user.Name != "First Responder" {
		t.Errorf("Name = %q, want First Responder", user.Name)
	}
}

func TestWhoIsOnCall_EmptyOncalls(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"oncalls": []}`))
	}))
	_, err := c.WhoIsOnCall(context.Background(), "tok", "POLICY1")
	if err == nil {
		t.Fatal("want error when no on-call user")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Errorf("err = %v, want APIError 404", err)
	}
}

func TestTriggerIncident_DedupKey(t *testing.T) {
	var seenFrom, seenAuth, seenContentType string
	var seenBody []byte
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		if r.URL.Path != "/incidents" {
			t.Errorf("path = %q", r.URL.Path)
		}
		seenFrom = r.Header.Get("From")
		seenAuth = r.Header.Get("Authorization")
		seenContentType = r.Header.Get("Content-Type")
		seenBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"incident":{"id":"INC-42"}}`))
	}))

	id, err := c.TriggerIncident(context.Background(), "tok", "ops@example.com", TriggerReq{
		ServiceID:   "PSVC1",
		Title:       "API saturated",
		UrgencyHigh: true,
		Body:        "rps > 2000 for 5m",
		DedupKey:    "saturation-2026-05-13",
	})
	if err != nil {
		t.Fatalf("TriggerIncident: %v", err)
	}
	if id != "INC-42" {
		t.Errorf("id = %q, want INC-42", id)
	}
	if seenFrom != "ops@example.com" {
		t.Errorf("From = %q, want ops@example.com", seenFrom)
	}
	if seenAuth != "Token token=tok" {
		t.Errorf("Authorization = %q", seenAuth)
	}
	if seenContentType != "application/json" {
		t.Errorf("Content-Type = %q", seenContentType)
	}
	// Verify body carries the dedup key + urgency + service envelope.
	var got map[string]any
	if err := json.Unmarshal(seenBody, &got); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	inc, _ := got["incident"].(map[string]any)
	if inc == nil {
		t.Fatalf("body missing 'incident' envelope: %s", seenBody)
	}
	if inc["incident_key"] != "saturation-2026-05-13" {
		t.Errorf("incident_key = %v, want dedup key", inc["incident_key"])
	}
	if inc["urgency"] != "high" {
		t.Errorf("urgency = %v, want high", inc["urgency"])
	}
	if inc["type"] != "incident" {
		t.Errorf("type = %v, want incident", inc["type"])
	}
	svc, _ := inc["service"].(map[string]any)
	if svc == nil || svc["id"] != "PSVC1" || svc["type"] != "service_reference" {
		t.Errorf("service envelope = %v", inc["service"])
	}
}

func TestTriggerIncident_RejectsMissingFrom(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be reached")
	}))
	_, err := c.TriggerIncident(context.Background(), "tok", "", TriggerReq{
		ServiceID: "PSVC1", Title: "t",
	})
	if err == nil {
		t.Fatal("want error for missing From")
	}
}

func TestTriggerIncident_LowUrgency(t *testing.T) {
	// Confirms UrgencyHigh=false flips the wire to "low" — important so
	// non-critical Sentinel signals do not page the on-call at 3am.
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		inc, _ := got["incident"].(map[string]any)
		if inc["urgency"] != "low" {
			t.Errorf("urgency = %v, want low", inc["urgency"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"incident":{"id":"X"}}`))
	}))

	if _, err := c.TriggerIncident(context.Background(), "tok", "ops@example.com", TriggerReq{
		ServiceID: "PSVC1", Title: "t", UrgencyHigh: false,
	}); err != nil {
		t.Fatalf("TriggerIncident: %v", err)
	}
}

// _ keeps the time import live in case future tests need timed mocks.
var _ = time.Second
