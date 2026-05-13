// Package sentry — Sentry REST client.
//
// client.go is the typed wrapper over Sentry's `/api/0/` surface. Every call
// goes through the shared httpx.Client (rate limit + circuit breaker + retry
// with backoff), and every response that is not 2xx is surfaced as a typed
// *APIError so callers can match on Status / Code without parsing strings.
//
// Endpoints used by Phase 3:
//
//	GET /api/0/organizations/{org}/projects/        — token validation
//	GET /api/0/projects/{org}/{proj}/stats/         — events-per-hour gauge
//	GET /api/0/projects/{org}/{proj}/issues/        — backfill window
//
// The base URL is configurable so self-hosted Sentry deployments can swap in
// their own host without redeploying the control plane.
package sentry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
)

// Client is the typed Sentry REST wrapper. Construct via NewClient and pass
// the bearer token per-call rather than embedding it; per-tenant tokens live
// inside the KeyVault and are decrypted on demand.
type Client struct {
	base string
	http *httpx.Client
}

// NewClient builds a Client against the given base URL. base defaults to
// https://sentry.io when empty. The httpx.Client is the shared wrapper from
// the integrations subtree — never reach across to the inner *http.Client
// directly; that would bypass rate-limit/breaker bookkeeping.
func NewClient(base string, http *httpx.Client) *Client {
	if base == "" {
		base = "https://sentry.io"
	}
	return &Client{base: strings.TrimRight(base, "/"), http: http}
}

// APIError is the typed error every non-2xx Sentry response yields. Callers
// can errors.As to retrieve Status when they need to branch on it (most
// commonly 401 → re-prompt for token).
type APIError struct {
	Status int    // HTTP status code
	Body   string // response body, capped at 4 KiB
	Op     string // logical op name ("validate_token", "list_issues", ...)
}

// Error implements the error contract with a stable shape.
func (e *APIError) Error() string {
	return fmt.Sprintf("sentry: %s: http %d: %s", e.Op, e.Status, truncate(e.Body, 200))
}

// IsUnauthorized reports whether the error is a 401, used by Connect to map
// "token rejected by Sentry" to a typed UI signal.
func (e *APIError) IsUnauthorized() bool { return e.Status == http.StatusUnauthorized }

// ProjectRef is the subset of Sentry's project payload we keep. The slug is
// authoritative for URL building; id is opaque.
type ProjectRef struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// Issue is the subset of Sentry's issue payload we promote into incidents.
// Fingerprint is the dedupe key — Sentry calls this "id" on the issue
// resource (the issue id is stable across event volume, unlike event ids).
type Issue struct {
	ID          string         // Sentry issue id (used as fingerprint)
	Title       string         // human-readable title
	Culprit     string         // code locus (file + symbol)
	Level       string         // "error" | "fatal" | "warning" | ...
	Fingerprint string         // mirrors ID for callers
	LastSeen    time.Time      // most-recent occurrence timestamp
	Permalink   string         // UI deep link
	Metadata    map[string]any // free-form for downstream consumers
}

// ValidateToken hits GET /api/0/organizations/{org}/projects/ — the cheapest
// authenticated call that proves (1) the token is live and (2) it has org
// scope. Returns the list of visible projects so AssertProjectExists can
// follow up without a second round trip.
func (c *Client) ValidateToken(ctx context.Context, token, orgSlug string) ([]ProjectRef, error) {
	if token == "" {
		return nil, errors.New("sentry: empty token")
	}
	if orgSlug == "" {
		return nil, errors.New("sentry: empty organization_slug")
	}
	path := fmt.Sprintf("/api/0/organizations/%s/projects/", url.PathEscape(orgSlug))
	var out []ProjectRef
	if err := c.getJSON(ctx, "validate_token", token, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AssertProjectExists confirms the configured project_slug is reachable
// through the token. Sentry's tokens can be scoped narrower than the org, so
// "token is valid" is not the same as "token can see this project".
func (c *Client) AssertProjectExists(ctx context.Context, token, orgSlug, projectSlug string) error {
	projects, err := c.ValidateToken(ctx, token, orgSlug)
	if err != nil {
		return err
	}
	for _, p := range projects {
		if p.Slug == projectSlug {
			return nil
		}
	}
	return fmt.Errorf("sentry: project_slug=%q not visible under organization_slug=%q", projectSlug, orgSlug)
}

// ProjectStats returns the last-1h event count for the (org, project) pair.
// Used by Status() as a freshness / health gauge — non-zero events_1h is the
// best proof that the project is wired up correctly without us minting a
// synthetic event.
//
// Sentry returns [[ts, count], ...]; we sum every bucket since we ask for a
// 1-hour window with resolution=10s.
func (c *Client) ProjectStats(ctx context.Context, token, orgSlug, projectSlug string) (int, error) {
	q := url.Values{}
	q.Set("stat", "received")
	q.Set("resolution", "10s")
	q.Set("since", strconv.FormatInt(time.Now().Add(-1*time.Hour).Unix(), 10))
	q.Set("until", strconv.FormatInt(time.Now().Unix(), 10))
	path := fmt.Sprintf("/api/0/projects/%s/%s/stats/?%s",
		url.PathEscape(orgSlug), url.PathEscape(projectSlug), q.Encode())

	var buckets [][2]int64 // [[unix_ts, count], ...]
	if err := c.getJSON(ctx, "project_stats", token, path, &buckets); err != nil {
		return 0, err
	}
	total := 0
	for _, b := range buckets {
		total += int(b[1])
	}
	return total, nil
}

// ListRecentIssues fetches issues for the (org, project) seen since the
// supplied timestamp. The query mirrors Sentry's documented "is:unresolved"
// filter; statsPeriod is set from `since` so the upstream does the window
// trim for us and we do not have to depend on local clock drift.
//
// The list returned is bounded by Sentry's pagination default (100); the
// backfill cron tolerates this because it polls every 5 minutes and emits
// each fingerprint at most once.
func (c *Client) ListRecentIssues(ctx context.Context, token, orgSlug, projectSlug string, since time.Time) ([]Issue, error) {
	q := url.Values{}
	q.Set("query", "is:unresolved")
	q.Set("statsPeriod", statsPeriodSince(since))
	q.Set("sort", "date")
	path := fmt.Sprintf("/api/0/projects/%s/%s/issues/?%s",
		url.PathEscape(orgSlug), url.PathEscape(projectSlug), q.Encode())

	var raw []struct {
		ID        string         `json:"id"`
		Title     string         `json:"title"`
		Culprit   string         `json:"culprit"`
		Level     string         `json:"level"`
		LastSeen  time.Time      `json:"lastSeen"`
		Permalink string         `json:"permalink"`
		Metadata  map[string]any `json:"metadata"`
	}
	if err := c.getJSON(ctx, "list_issues", token, path, &raw); err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(raw))
	for _, r := range raw {
		// Trust `since` over Sentry's statsPeriod rounding — Sentry rounds
		// statsPeriod to the minute, which can leak slightly older issues
		// into the response. Drop anything strictly before `since` so the
		// dedupe set stays tight.
		if !since.IsZero() && r.LastSeen.Before(since) {
			continue
		}
		out = append(out, Issue{
			ID:          r.ID,
			Title:       r.Title,
			Culprit:     r.Culprit,
			Level:       r.Level,
			Fingerprint: r.ID,
			LastSeen:    r.LastSeen,
			Permalink:   r.Permalink,
			Metadata:    r.Metadata,
		})
	}
	return out, nil
}

// getJSON issues a GET with the supplied bearer token and decodes the body
// into `out`. Non-2xx responses become typed *APIError.
func (c *Client) getJSON(ctx context.Context, op, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("sentry: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sentry: %s: %w", op, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return &APIError{Status: resp.StatusCode, Body: string(body), Op: op}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("sentry: %s: decode: %w", op, err)
	}
	return nil
}

// statsPeriodSince converts an absolute timestamp into Sentry's compact
// statsPeriod form. Sentry accepts "Nm" / "Nh" / "Nd"; we pick the smallest
// unit that fits to keep the window tight.
func statsPeriodSince(since time.Time) string {
	if since.IsZero() {
		return "1h"
	}
	d := time.Since(since)
	if d < time.Minute {
		return "1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes())+1)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours())+1)
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24)+1)
}

// truncate caps a string at n bytes for log/error embedding.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
