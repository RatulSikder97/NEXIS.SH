// Package pagerduty — PagerDuty REST API v2 client used by the Provider
// adapter.
//
// The client is a thin, opinionated wrapper around the shared httpx.Client.
// Three boundaries make this client a little different from sentry/github:
//
//   - Authentication is PagerDuty's old-school `Authorization: Token token=<t>`
//     header (NOT the modern OAuth Bearer form). The Accept header pins us to
//     the v2 wire schema so PagerDuty cannot silently flip JSON shapes on us.
//
//   - The POST /incidents endpoint requires a `From: <user_email>` header that
//     names the PagerDuty user creating the incident on behalf of the bot.
//     The provider passes the operator's configured "from" email here; tests
//     assert it survives onto the wire.
//
//   - 4xx is surfaced as a typed *APIError so callers can branch on Status —
//     401 = bad token (Connect refuses to persist), 404 = wrong policy/service,
//     422 = validation. 5xx is left to httpx's retry/breaker layer.
package pagerduty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
)

// defaultAPIBaseURL is the PagerDuty REST API root. Tests override this via
// NewClientWithBaseURL so they can point at an httptest.Server.
const defaultAPIBaseURL = "https://api.pagerduty.com"

// acceptV2 is the version-pinning Accept header. PagerDuty's wire schema
// evolves under feature flags; pinning at v2 makes our parsers stable.
const acceptV2 = "application/vnd.pagerduty+json;version=2"

// APIError is returned for any non-2xx response. Callers can errors.As it to
// inspect Status (e.g. 401 → bad token, 404 → missing policy/service). The
// Message field carries the PagerDuty-supplied error message when present.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("pagerduty api: status %d: %s", e.Status, e.Message)
}

// User is the slim view of a PagerDuty user the Provider needs. The REST API
// returns many more fields; we drop everything else to keep the surface
// auditable.
type User struct {
	ID    string
	Name  string
	Email string
	Role  string
}

// TriggerReq is the input to TriggerIncident. Mirrors the subset of POST
// /incidents that Sentinel needs when it escalates a detection back to the
// customer's on-call rotation.
type TriggerReq struct {
	ServiceID   string // the PagerDuty service (NOT escalation policy) to page
	Title       string // incident summary, shown in PD app + email
	UrgencyHigh bool   // urgency=high|low — high pages the on-call now
	Body        string // free-form description, becomes the incident "details"
	DedupKey    string // dedup_key — at most one open incident per key
}

// Client is the PagerDuty REST API client. Construct with NewClient.
type Client struct {
	httpc   *httpx.Client
	baseURL string
}

// NewClient builds a Client wrapping the supplied *httpx.Client. httpc must
// not be nil — every outbound call goes through the shared wrapper for rate
// limiting, retries, and breaker behaviour.
func NewClient(httpc *httpx.Client) *Client {
	return &Client{httpc: httpc, baseURL: defaultAPIBaseURL}
}

// NewClientWithBaseURL is the test-only constructor that lets *_test.go point
// at an httptest.Server URL. Keeping this on the exported type avoids leaking
// the baseURL field outside the package.
func NewClientWithBaseURL(httpc *httpx.Client, baseURL string) *Client {
	return &Client{httpc: httpc, baseURL: baseURL}
}

// userResp mirrors the slice of GET /users/me we care about. PagerDuty wraps
// the user object in an envelope {"user": {...}}; the rest of the response
// (e.g. teams, contact methods) is discarded.
type userResp struct {
	User struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"user"`
}

// MeOK validates a REST API token by calling GET /users/me. The Provider
// uses this from Connect to refuse persisting a token that cannot read the
// caller's own user record — typically a 401 from a revoked token or a 403
// from an over-restricted REST key.
func (c *Client) MeOK(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, errors.New("pagerduty: token required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/users/me", nil)
	if err != nil {
		return User{}, fmt.Errorf("pagerduty: build req: %w", err)
	}
	setAuthHeaders(req, token)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return User{}, fmt.Errorf("pagerduty: users/me: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return User{}, decodeAPIError(resp)
	}
	var body userResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return User{}, fmt.Errorf("pagerduty: decode users/me: %w", err)
	}
	return User{
		ID:    body.User.ID,
		Name:  body.User.Name,
		Email: body.User.Email,
		Role:  body.User.Role,
	}, nil
}

// oncallsResp mirrors the slim view of GET /oncalls we care about. PagerDuty
// returns a richer object that includes the schedule + escalation rule; we
// only need the user reference so the approval-gate display can name a
// human.
type oncallsResp struct {
	Oncalls []struct {
		User struct {
			ID      string `json:"id"`
			Summary string `json:"summary"`
		} `json:"user"`
	} `json:"oncalls"`
}

// WhoIsOnCall returns the user currently on call for the supplied escalation
// policy. We ask PagerDuty to project just the first match (limit=1) so the
// call stays cheap even for orgs with hundreds of rotations.
//
// The user object PagerDuty returns under /oncalls is a "user reference" and
// only carries {id, summary}. We map summary into User.Name; Email and Role
// are left blank — callers needing those should follow up with MeOK against
// the user's own token (we cannot call /users/{id} without escalated scopes).
func (c *Client) WhoIsOnCall(ctx context.Context, token, escalationPolicyID string) (User, error) {
	if token == "" {
		return User{}, errors.New("pagerduty: token required")
	}
	if escalationPolicyID == "" {
		return User{}, errors.New("pagerduty: escalation policy id required")
	}
	url := fmt.Sprintf("%s/oncalls?escalation_policy_ids[]=%s&limit=1",
		c.baseURL, escalationPolicyID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return User{}, fmt.Errorf("pagerduty: build req: %w", err)
	}
	setAuthHeaders(req, token)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return User{}, fmt.Errorf("pagerduty: oncalls: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return User{}, decodeAPIError(resp)
	}
	var body oncallsResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return User{}, fmt.Errorf("pagerduty: decode oncalls: %w", err)
	}
	if len(body.Oncalls) == 0 {
		return User{}, &APIError{Status: http.StatusNotFound, Message: "no on-call user for policy"}
	}
	first := body.Oncalls[0].User
	return User{ID: first.ID, Name: first.Summary}, nil
}

// triggerIncidentWire is the snake_case body POSTed to /incidents. PagerDuty
// requires the outer "incident" envelope and a "type":"incident" tag — both
// are baked in here so callers cannot accidentally omit them.
type triggerIncidentWire struct {
	Incident struct {
		Type    string `json:"type"`
		Title   string `json:"title"`
		Service struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"service"`
		Urgency string `json:"urgency"`
		Body    struct {
			Type    string `json:"type"`
			Details string `json:"details"`
		} `json:"body"`
		IncidentKey string `json:"incident_key,omitempty"`
	} `json:"incident"`
}

// triggerIncidentResp captures only the incident id we surface to callers.
type triggerIncidentResp struct {
	Incident struct {
		ID string `json:"id"`
	} `json:"incident"`
}

// TriggerIncident POSTs /incidents with the supplied service + dedup key.
// The fromEmail argument names a PagerDuty user the request is being made on
// behalf of — PagerDuty requires this in the `From` header and 400s without
// it. The Provider wires the operator-configured "from" email through here.
//
// On success returns the new incident's id. On 4xx returns a typed
// *APIError. The dedup_key (incident_key in v2 wire) bounds the blast radius
// of repeated escalations: PagerDuty merges any subsequent trigger with the
// same key into the existing open incident.
func (c *Client) TriggerIncident(ctx context.Context, token, fromEmail string, in TriggerReq) (string, error) {
	if token == "" {
		return "", errors.New("pagerduty: token required")
	}
	if fromEmail == "" {
		return "", errors.New("pagerduty: from email required")
	}
	if in.ServiceID == "" {
		return "", errors.New("pagerduty: service id required")
	}
	if in.Title == "" {
		return "", errors.New("pagerduty: title required")
	}

	var wire triggerIncidentWire
	wire.Incident.Type = "incident"
	wire.Incident.Title = in.Title
	wire.Incident.Service.ID = in.ServiceID
	wire.Incident.Service.Type = "service_reference"
	if in.UrgencyHigh {
		wire.Incident.Urgency = "high"
	} else {
		wire.Incident.Urgency = "low"
	}
	wire.Incident.Body.Type = "incident_body"
	wire.Incident.Body.Details = in.Body
	wire.Incident.IncidentKey = in.DedupKey

	bodyBytes, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("pagerduty: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/incidents",
		bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("pagerduty: build req: %w", err)
	}
	setAuthHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("From", fromEmail)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("pagerduty: trigger incident: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", decodeAPIError(resp)
	}
	var out triggerIncidentResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("pagerduty: decode incident: %w", err)
	}
	return out.Incident.ID, nil
}

// setAuthHeaders applies the v2 Accept header + the legacy `Token token=<t>`
// Authorization scheme PagerDuty still uses. Pulled into a helper so we only
// have one place to fix when PagerDuty finally migrates to Bearer.
func setAuthHeaders(req *http.Request, token string) {
	req.Header.Set("Accept", acceptV2)
	req.Header.Set("Authorization", "Token token="+token)
}

// decodeAPIError turns a non-2xx response into a typed *APIError. We try to
// pull PagerDuty's nested {"error":{"message":"..."}} shape; on parse failure
// we fall back to the raw body so the operator sees something useful in logs.
func decodeAPIError(resp *http.Response) error {
	const maxBody = 64 << 10 // 64 KiB — PagerDuty errors are tiny
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	msg := ""
	var asJSON struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &asJSON) == nil && asJSON.Error.Message != "" {
		msg = asJSON.Error.Message
	} else {
		msg = string(raw)
	}
	return &APIError{Status: resp.StatusCode, Message: msg}
}
