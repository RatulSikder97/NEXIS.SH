// Package datadog implements the Datadog IntegrationProvider — multi-site
// API client + signed-webhook receiver. The adapter has the same surface as
// every other provider (Name/Connect/Disconnect/Status/HandleWebhook) and
// stores its credentials encrypted at rest under the shared KeyVault.
//
// The client layer (this file) wraps the shared internal/httpx.Client so all
// Datadog REST calls inherit retry, breaker, per-host rate-limit and log
// redaction. We expose two typed methods:
//
//   - Validate    — GET /api/v1/validate to confirm api_key + app_key on
//                   Connect, and as the cheap probe used by Status.
//   - MonitorState — GET /api/v1/monitor and bucket the response by state so
//                   Status can surface "alerting / ok" counts in the UI.
//
// Auth headers are DD-API-KEY + DD-APPLICATION-KEY. The HTTP wrapper redacts
// Authorization but not these two, so callers must keep secrets out of slog
// fields explicitly.
package datadog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
)

// Supported Datadog sites. Anything outside this set is rejected at Connect
// time so we never emit a request to an unknown host.
const (
	SiteUS1    = "datadoghq.com"
	SiteUS3    = "us3.datadoghq.com"
	SiteUS5    = "us5.datadoghq.com"
	SiteEU1    = "datadoghq.eu"
	SiteUSGov  = "ddog-gov.com"
	defaultSite = SiteUS1
)

// validSite reports whether s is one of the five accepted Datadog site
// shorthands. Empty defaults to US1 at the call site.
func validSite(s string) bool {
	switch s {
	case SiteUS1, SiteUS3, SiteUS5, SiteEU1, SiteUSGov:
		return true
	}
	return false
}

// APIError is the typed error every client method returns when Datadog
// answers with a non-2xx status. Status is the HTTP code; Errors is the
// canonical {errors:[...]} array Datadog uses for both v1 and v2 surfaces.
type APIError struct {
	Status int
	Errors []string
}

func (e *APIError) Error() string {
	if len(e.Errors) == 0 {
		return fmt.Sprintf("datadog: http %d", e.Status)
	}
	return fmt.Sprintf("datadog: http %d: %v", e.Status, e.Errors)
}

// Unauthorized reports whether the response was 401/403 — used by Provider
// to decide whether to persist a degraded connection or reject Connect.
func (e *APIError) Unauthorized() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// Client is the typed wrapper around the shared httpx.Client. Construct via
// NewClient; safe for concurrent use.
type Client struct {
	site string
	http *httpx.Client
}

// NewClient returns a Client bound to a Datadog site. Empty site defaults
// to datadoghq.com so the dev/test path can call NewClient("", h). Unknown
// sites are accepted here so callers can still build a client for a custom
// staging endpoint; Validate will surface the error at request time.
func NewClient(site string, h *httpx.Client) *Client {
	if site == "" {
		site = defaultSite
	}
	return &Client{site: site, http: h}
}

// Site returns the configured site shorthand. Useful for tests and audit
// fields where we want to record which DC the connection targeted.
func (c *Client) Site() string { return c.site }

// baseURL returns the API base for the configured site. Datadog exposes
// the same paths under https://api.<site>/, so we keep this format string
// in one place and let the per-call code append /api/v{1,2}/<resource>.
func (c *Client) baseURL() string {
	return "https://api." + c.site
}

// Validate hits GET /api/v1/validate. Used as the cheap probe on Connect
// and Status; returns nil iff the response is a 200 with {valid:true}.
// 401/403 → typed *APIError so the caller can branch on Unauthorized().
func (c *Client) Validate(ctx context.Context, apiKey, appKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+"/api/v1/validate", nil)
	if err != nil {
		return fmt.Errorf("datadog: build validate: %w", err)
	}
	setAuth(req, apiKey, appKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("datadog: validate: %w", err)
	}
	defer drain(resp)
	if resp.StatusCode/100 != 2 {
		return decodeAPIError(resp)
	}
	var body struct {
		Valid bool `json:"valid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("datadog: decode validate: %w", err)
	}
	if !body.Valid {
		return &APIError{Status: resp.StatusCode, Errors: []string{"validate returned valid=false"}}
	}
	return nil
}

// monitor mirrors the slice of GET /api/v1/monitor we care about. Datadog
// returns dozens of fields per monitor; we only need the overall state to
// drive the alerting/ok health pill.
type monitor struct {
	OverallState string `json:"overall_state"`
}

// MonitorState fetches every monitor in the account and buckets the
// responses by overall_state. "Alert" + "Warn" → alerting; "OK" + "No Data"
// → ok. Anything else (e.g. "Ignored", "Skipped") is silently dropped — the
// UI only needs the two-state summary.
//
// This endpoint can return a very large body for big accounts. We rely on
// httpx's default request timeout (30s) to keep things bounded; future
// callers should switch to /monitor/search if Status latency becomes an
// issue.
func (c *Client) MonitorState(ctx context.Context, apiKey, appKey string) (alerting int, ok int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+"/api/v1/monitor", nil)
	if err != nil {
		return 0, 0, fmt.Errorf("datadog: build monitor: %w", err)
	}
	setAuth(req, apiKey, appKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("datadog: monitor: %w", err)
	}
	defer drain(resp)
	if resp.StatusCode/100 != 2 {
		return 0, 0, decodeAPIError(resp)
	}
	var monitors []monitor
	if err := json.NewDecoder(resp.Body).Decode(&monitors); err != nil {
		return 0, 0, fmt.Errorf("datadog: decode monitor: %w", err)
	}
	for _, m := range monitors {
		switch m.OverallState {
		case "Alert", "Warn":
			alerting++
		case "OK", "No Data":
			ok++
		}
	}
	return alerting, ok, nil
}

// setAuth attaches DD-API-KEY + DD-APPLICATION-KEY. Centralised so we never
// accidentally log them through req.Header in test output. (httpx redacts
// only the Authorization header.)
func setAuth(req *http.Request, apiKey, appKey string) {
	req.Header.Set("DD-API-KEY", apiKey)
	req.Header.Set("DD-APPLICATION-KEY", appKey)
	req.Header.Set("Accept", "application/json")
}

// decodeAPIError reads up to 64 KiB of the response body and parses
// Datadog's standard {errors:[...]} envelope. Always returns a non-nil
// *APIError so callers can errors.As against it.
func decodeAPIError(resp *http.Response) error {
	if resp == nil {
		return &APIError{Status: 0, Errors: []string{"nil response"}}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var env struct {
		Errors []string `json:"errors"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &env)
	}
	if len(env.Errors) == 0 {
		// Common shape on 401 from /validate is a bare {"errors":["Forbidden"]}
		// so this branch usually only fires on opaque proxy responses.
		// Preserve the raw body (truncated) so ops has *something* to read.
		if len(body) > 0 && len(body) < 200 {
			return &APIError{Status: resp.StatusCode, Errors: []string{string(body)}}
		}
		return &APIError{Status: resp.StatusCode}
	}
	return &APIError{Status: resp.StatusCode, Errors: env.Errors}
}

// drain closes the body after reading up to 64 KiB so the underlying TCP
// connection can be reused.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 64<<10)
	_ = resp.Body.Close()
}
