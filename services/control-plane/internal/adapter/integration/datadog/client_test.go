package datadog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
)

// newTestClient builds a Client that routes through an httptest.Server.
// We rewrite the request URL on every call so the production baseURL of
// https://api.<site> resolves to the test server. This keeps the
// production code's path-building under test rather than mocking it out.
func newTestClient(t *testing.T, srv *httptest.Server, site string) *Client {
	t.Helper()
	// Wrap the test server's transport so we transparently rewrite
	// https://api.<site> → srv.URL on the wire.
	inner := &http.Client{
		Transport: &rewriteTransport{base: srv.URL, inner: srv.Client().Transport},
	}
	h := httpx.New(inner, httpx.Config{
		RatePerSec:       1000,
		Burst:            1000,
		MaxAttempts:      1,
		BreakerThreshold: 100,
	})
	return NewClient(site, h)
}

// rewriteTransport sends requests to base instead of the URL's real host.
// We keep the path/query intact so we can still verify the production code
// builds /api/v1/validate / /api/v1/monitor.
type rewriteTransport struct {
	base  string
	inner http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	u := *clone.URL
	// Replace scheme + host with the test server's.
	base := rt.base
	if i := strings.Index(base, "://"); i >= 0 {
		u.Scheme = base[:i]
		u.Host = base[i+3:]
	}
	clone.URL = &u
	clone.Host = u.Host
	t := rt.inner
	if t == nil {
		t = http.DefaultTransport
	}
	return t.RoundTrip(clone)
}

func TestValidate_HappyPath(t *testing.T) {
	var gotPath, gotAPIKey, gotAppKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("DD-API-KEY")
		gotAppKey = r.Header.Get("DD-APPLICATION-KEY")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":true}`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, SiteUS1)
	if err := c.Validate(context.Background(), "api-key", "app-key"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if gotPath != "/api/v1/validate" {
		t.Errorf("path = %q, want /api/v1/validate", gotPath)
	}
	if gotAPIKey != "api-key" {
		t.Errorf("DD-API-KEY = %q, want api-key", gotAPIKey)
	}
	if gotAppKey != "app-key" {
		t.Errorf("DD-APPLICATION-KEY = %q, want app-key", gotAppKey)
	}
}

func TestValidate_401Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":["Forbidden"]}`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, SiteUS1)
	err := c.Validate(context.Background(), "bad", "bad")
	if err == nil {
		t.Fatal("want error for 401")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", apiErr.Status)
	}
	if !apiErr.Unauthorized() {
		t.Errorf("Unauthorized() = false for 401")
	}
	if len(apiErr.Errors) == 0 || apiErr.Errors[0] != "Forbidden" {
		t.Errorf("Errors = %v, want [Forbidden]", apiErr.Errors)
	}
}

func TestValidate_ValidFalseTreatedAsError(t *testing.T) {
	// Defensive — Datadog has historically returned 200 + {valid:false} for
	// some misconfigurations. Treat that as a validation failure rather
	// than swallowing it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":false}`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, SiteUS1)
	err := c.Validate(context.Background(), "k", "k")
	if err == nil {
		t.Fatal("want error when valid=false")
	}
}

func TestMonitorState_CountsByType(t *testing.T) {
	// 3 alerting (Alert, Warn, Alert), 2 ok (OK, No Data), 1 ignored
	body := `[
		{"overall_state":"Alert"},
		{"overall_state":"OK"},
		{"overall_state":"Warn"},
		{"overall_state":"No Data"},
		{"overall_state":"Alert"},
		{"overall_state":"Ignored"}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/monitor" {
			t.Errorf("path = %q, want /api/v1/monitor", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, SiteUS1)
	alerting, ok, err := c.MonitorState(context.Background(), "k", "k")
	if err != nil {
		t.Fatalf("MonitorState: %v", err)
	}
	if alerting != 3 {
		t.Errorf("alerting = %d, want 3", alerting)
	}
	if ok != 2 {
		t.Errorf("ok = %d, want 2", ok)
	}
}

func TestBaseURL_PerSite(t *testing.T) {
	cases := []struct {
		site string
		want string
	}{
		{SiteUS1, "https://api.datadoghq.com"},
		{SiteUS3, "https://api.us3.datadoghq.com"},
		{SiteUS5, "https://api.us5.datadoghq.com"},
		{SiteEU1, "https://api.datadoghq.eu"},
		{SiteUSGov, "https://api.ddog-gov.com"},
	}
	for _, tc := range cases {
		c := NewClient(tc.site, nil)
		if got := c.baseURL(); got != tc.want {
			t.Errorf("site=%s baseURL=%q, want %q", tc.site, got, tc.want)
		}
	}
	// Empty site defaults to US1.
	c := NewClient("", nil)
	if got := c.baseURL(); got != "https://api.datadoghq.com" {
		t.Errorf("default baseURL = %q, want US1", got)
	}
}

func TestValidate_Non2xxWithoutErrorsField(t *testing.T) {
	// Defensive — when Datadog answers with a non-retryable 4xx that doesn't
	// carry the standard {errors:[...]} envelope (e.g. a 400 from an
	// upstream proxy with an HTML body) we still want *APIError, not a
	// generic decode failure. We deliberately use a 4xx because 5xx is
	// retried + wrapped by httpx into a "max attempts reached" error —
	// that retry behaviour is owned by httpx and asserted in its own
	// tests, not here.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<html>bad request</html>`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, SiteUS1)
	err := c.Validate(context.Background(), "k", "k")
	if err == nil {
		t.Fatal("want error for 400")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err type = %T, want *APIError (chain: %v)", err, err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", apiErr.Status)
	}
}
