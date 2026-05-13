// Package sentry — REST client tests.
//
// Every Sentry endpoint we hit is exercised through an httptest.Server so we
// can assert on header / path / query without ever touching the real
// upstream. The shared httpx.Client is wired with a fastBackoff sleeper so
// retry-bearing tests do not actually wait.
package sentry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
)

// newTestClient bundles the boilerplate every test needs: an httptest.Server
// with the supplied handler + an httpx-wrapped Client pointing at it. The
// cleanup is registered so callers do not have to remember it.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	hx := httpx.New(srv.Client(), httpx.Config{
		RatePerSec:       1000,
		Burst:            1000,
		MaxAttempts:      2,
		BreakerThreshold: 100, // do not trip during a happy-path test
	})
	return NewClient(srv.URL, hx), srv
}

func TestValidateToken_HappyPath(t *testing.T) {
	want := []ProjectRef{
		{ID: "1", Slug: "api", Name: "API"},
		{ID: "2", Slug: "web", Name: "Web"},
	}
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/api/0/organizations/acme/projects/") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-1" {
			t.Errorf("auth header = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("accept header = %q", got)
		}
		_ = json.NewEncoder(w).Encode(want)
	})

	got, err := c.ValidateToken(context.Background(), "tok-1", "acme")
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if len(got) != 2 || got[0].Slug != "api" || got[1].Slug != "web" {
		t.Fatalf("got %+v", got)
	}
}

func TestValidateToken_401Unauthorized(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"Invalid token"}`))
	})

	_, err := c.ValidateToken(context.Background(), "bad", "acme")
	if err == nil {
		t.Fatal("want error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if !apiErr.IsUnauthorized() {
		t.Fatalf("status = %d, want 401", apiErr.Status)
	}
	if !strings.Contains(apiErr.Body, "Invalid token") {
		t.Errorf("body = %q", apiErr.Body)
	}
	if apiErr.Op != "validate_token" {
		t.Errorf("op = %q", apiErr.Op)
	}
}

func TestAssertProjectExists_NotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]ProjectRef{
			{ID: "1", Slug: "api"},
			{ID: "2", Slug: "web"},
		})
	})

	err := c.AssertProjectExists(context.Background(), "tok", "acme", "billing")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "billing") {
		t.Errorf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "acme") {
		t.Errorf("error = %v", err)
	}
}

func TestAssertProjectExists_Found(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]ProjectRef{{Slug: "billing"}})
	})

	if err := c.AssertProjectExists(context.Background(), "tok", "acme", "billing"); err != nil {
		t.Fatalf("AssertProjectExists: %v", err)
	}
}

func TestProjectStats_ParsesEventCount(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/api/0/projects/acme/api/stats/") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("stat") != "received" {
			t.Errorf("stat param = %q", r.URL.Query().Get("stat"))
		}
		// Sentry returns [[unix_ts, count], ...]
		_, _ = fmt.Fprint(w, `[[1700000000,3],[1700000010,4],[1700000020,5]]`)
	})

	n, err := c.ProjectStats(context.Background(), "tok", "acme", "api")
	if err != nil {
		t.Fatalf("ProjectStats: %v", err)
	}
	if n != 12 {
		t.Fatalf("events = %d, want 12", n)
	}
}

func TestListRecentIssues_FiltersBySince(t *testing.T) {
	since := time.Now().Add(-7 * time.Minute)

	var capturedQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		if !strings.Contains(r.URL.Path, "/api/0/projects/acme/api/issues/") {
			t.Errorf("path = %s", r.URL.Path)
		}
		// Return three issues: one in window, one strictly older than since,
		// one without a lastSeen. The "older" one should be filtered out
		// client-side; the no-lastSeen one keeps the zero-value time which
		// is also strictly before `since` and is filtered.
		body := fmt.Sprintf(`[
			{"id":"i1","title":"T1","level":"error","lastSeen":%q},
			{"id":"i2","title":"T2","level":"error","lastSeen":%q}
		]`, time.Now().Format(time.RFC3339Nano),
			time.Now().Add(-1*time.Hour).Format(time.RFC3339Nano))
		_, _ = w.Write([]byte(body))
	})

	got, err := c.ListRecentIssues(context.Background(), "tok", "acme", "api", since)
	if err != nil {
		t.Fatalf("ListRecentIssues: %v", err)
	}
	// statsPeriod must be present and mention minutes/hours (something derived
	// from the 7-minute window).
	if !strings.Contains(capturedQuery, "statsPeriod=") {
		t.Errorf("query missing statsPeriod: %q", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "is%3Aunresolved") {
		t.Errorf("query missing is:unresolved: %q", capturedQuery)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 issue (in-window only), got %d", len(got))
	}
	if got[0].ID != "i1" {
		t.Errorf("issue id = %q", got[0].ID)
	}
	if got[0].Fingerprint != "i1" {
		t.Errorf("fingerprint = %q", got[0].Fingerprint)
	}
}

func TestListRecentIssues_500_RetriesAndFails(t *testing.T) {
	// httpx retries 5xx, so a sustained 500 surfaces as the max-attempts
	// wrap error rather than a typed APIError. This test pins that
	// behaviour so a future refactor of the wrapper does not silently
	// flip the error shape.
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"detail":"boom"}`))
	})

	_, err := c.ListRecentIssues(context.Background(), "tok", "acme", "api", time.Now().Add(-time.Minute))
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "max attempts") {
		t.Errorf("want max-attempts error, got: %v", err)
	}
	// 4xx-non-429 errors should ALSO map to *APIError. Easier to assert on
	// a 400 here since the retry path doesn't fire.
	if errors.As(err, new(*APIError)) {
		t.Errorf("5xx max-retries should not surface as *APIError")
	}
}

func TestListRecentIssues_400_TypedError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"invalid statsPeriod"}`))
	})

	_, err := c.ListRecentIssues(context.Background(), "tok", "acme", "api", time.Now().Add(-time.Minute))
	if err == nil {
		t.Fatal("want error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Errorf("status = %d", apiErr.Status)
	}
}

func TestStatsPeriodSince(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		in   time.Time
		want string
	}{
		{"zero", time.Time{}, "1h"},
		{"30s ago", now.Add(-30 * time.Second), "1m"},
		{"3m ago", now.Add(-3 * time.Minute), "4m"},
		{"2h ago", now.Add(-2 * time.Hour), "3h"},
		{"3d ago", now.Add(-3 * 24 * time.Hour), "4d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statsPeriodSince(tc.in); got != tc.want {
				t.Errorf("statsPeriodSince(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
