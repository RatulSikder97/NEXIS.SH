// Package github — GitHub REST API client used by the Provider adapter.
//
// The client is a thin, opinionated wrapper around the shared httpx.Client:
//
//   - Authentication is two-tiered: every request is either signed with a
//     freshly-minted App JWT (Bearer) or with an installation token (token).
//     The two helpers MintAppJWT + ExchangeInstallationToken make the
//     boundary explicit so callers do not accidentally use the long-lived
//     App credentials on tenant-data endpoints.
//
//   - All requests carry the v2022-11-28 API-version header so a breaking
//     surface change upstream does not silently flip our payload shapes.
//
//   - 4xx responses are surfaced as a typed *APIError, letting callers
//     branch on Status (e.g. 404 → not connected, 422 → branch already
//     exists). 5xx is left to httpx's retry/breaker layer.
package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
)

// defaultAPIBaseURL is the GitHub REST API root. Tests override this via
// NewClientWithBaseURL to point at an httptest.Server.
const defaultAPIBaseURL = "https://api.github.com"

// AppCreds bundles the GitHub App credentials needed to mint App JWTs.
// PrivateKeyPEM is the raw PEM bytes (PKCS1 or PKCS8); decoding happens once
// inside NewClient so a malformed key fails at construction rather than on
// every Connect call.
type AppCreds struct {
	AppID         int64
	PrivateKeyPEM []byte
}

// PullRequestReq is the body of POST /repos/{owner}/{repo}/pulls. Field names
// map directly to GitHub's snake_case wire format.
type PullRequestReq struct {
	Title string
	Head  string // branch
	Base  string // usually "main"
	Body  string
	Draft bool
}

// APIError is returned for any non-2xx response. Callers can errors.As it to
// inspect Status (e.g. branch-already-exists → 422). The Message field carries
// the GitHub-supplied error message when present, otherwise the raw body.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github api: status %d: %s", e.Status, e.Message)
}

// Client is the GitHub REST API client. Construct with NewClient.
type Client struct {
	creds   AppCreds
	privKey *rsa.PrivateKey
	httpc   *httpx.Client
	baseURL string

	// nowFunc is the clock used for JWT iat/exp. Tests inject a fixed clock.
	nowFunc func() time.Time
}

// NewClient builds a Client wrapping the supplied *httpx.Client. The private
// key is parsed eagerly so a malformed PEM fails at boot. httpc must not be
// nil — every outbound call goes through the shared wrapper for rate
// limiting, retries, and breaker behaviour.
func NewClient(creds AppCreds, httpc *httpx.Client) (*Client, error) {
	if httpc == nil {
		return nil, errors.New("github: httpx.Client required")
	}
	if creds.AppID <= 0 {
		return nil, errors.New("github: AppID must be positive")
	}
	if len(creds.PrivateKeyPEM) == 0 {
		return nil, errors.New("github: PrivateKeyPEM is empty")
	}
	key, err := parseRSAPrivateKeyFromPEM(creds.PrivateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("github: parse private key: %w", err)
	}
	return &Client{
		creds:   creds,
		privKey: key,
		httpc:   httpc,
		baseURL: defaultAPIBaseURL,
		nowFunc: time.Now,
	}, nil
}

// NewClientWithBaseURL is the test-only constructor that lets *_test.go point
// at an httptest.Server URL. Keeping this on the exported type avoids leaking
// the baseURL field outside the package.
func NewClientWithBaseURL(creds AppCreds, httpc *httpx.Client, baseURL string) (*Client, error) {
	c, err := NewClient(creds, httpc)
	if err != nil {
		return nil, err
	}
	c.baseURL = baseURL
	return c, nil
}

// MintAppJWT returns a 10-minute RS256 JWT signed with the App's private key.
// iss=AppID, iat=now-60s (clock-skew buffer recommended by GitHub),
// exp=now+10min. The token is used as Bearer credentials when calling the
// /app/* endpoints.
func (c *Client) MintAppJWT() (string, error) {
	now := c.nowFunc()
	claims := jwt.MapClaims{
		"iss": fmt.Sprintf("%d", c.creds.AppID),
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := tok.SignedString(c.privKey)
	if err != nil {
		return "", fmt.Errorf("github: sign jwt: %w", err)
	}
	return signed, nil
}

// installationTokenResp mirrors the response body of POST
// /app/installations/{id}/access_tokens.
type installationTokenResp struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ExchangeInstallationToken posts to /app/installations/{id}/access_tokens
// using the App JWT and returns a 1-hour installation access token. The
// returned expiresAt is what GitHub reports, NOT a local now+1h estimate —
// downstream cache TTLs use it directly minus a small buffer.
func (c *Client) ExchangeInstallationToken(ctx context.Context, installationID int64) (string, time.Time, error) {
	jwtTok, err := c.MintAppJWT()
	if err != nil {
		return "", time.Time{}, err
	}
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.baseURL, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("github: build req: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwtTok)
	c.setCommonHeaders(req)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("github: exchange token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return "", time.Time{}, decodeAPIError(resp)
	}
	var body installationTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", time.Time{}, fmt.Errorf("github: decode token: %w", err)
	}
	return body.Token, body.ExpiresAt, nil
}

// installationRepoListResp mirrors the response of GET /installation/repositories.
// We only need full_name for the Status payload's repos[:5] preview; everything
// else GitHub returns gets dropped.
type installationRepoListResp struct {
	TotalCount   int `json:"total_count"`
	Repositories []struct {
		FullName string `json:"full_name"`
	} `json:"repositories"`
}

// ListInstallationRepos calls GET /installation/repositories with the
// installation token. Returns repo full_names (e.g. "owner/repo"). The first
// page is capped to 30 (GitHub's default) — pagination is not exercised
// because the Status caller only needs a few names for the UI preview.
func (c *Client) ListInstallationRepos(ctx context.Context, instToken string) ([]string, error) {
	url := c.baseURL + "/installation/repositories?per_page=30"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build req: %w", err)
	}
	req.Header.Set("Authorization", "token "+instToken)
	c.setCommonHeaders(req)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: list repos: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return nil, decodeAPIError(resp)
	}
	var body installationRepoListResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("github: decode repo list: %w", err)
	}
	names := make([]string, 0, len(body.Repositories))
	for _, r := range body.Repositories {
		names = append(names, r.FullName)
	}
	return names, nil
}

// pullRequestRespMin is the slim view of the PR creation response we care
// about — just the HTML URL for surfacing to the user.
type pullRequestRespMin struct {
	HTMLURL string `json:"html_url"`
}

// pullRequestWire is the snake_case wire shape POSTed to /repos/.../pulls.
type pullRequestWire struct {
	Title string `json:"title"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Body  string `json:"body,omitempty"`
	Draft bool   `json:"draft,omitempty"`
}

// OpenPullRequest POSTs /repos/{owner}/{repo}/pulls and returns the new PR's
// HTML URL on success. On 422 (validation error — typically "branch already
// has a PR") the caller receives a typed *APIError so the higher layer can
// decide whether to skip vs surface the conflict.
func (c *Client) OpenPullRequest(ctx context.Context, instToken, owner, repo string, in PullRequestReq) (string, error) {
	if owner == "" || repo == "" {
		return "", errors.New("github: owner and repo required")
	}
	if in.Title == "" || in.Head == "" || in.Base == "" {
		return "", errors.New("github: title/head/base required")
	}
	url := fmt.Sprintf("%s/repos/%s/%s/pulls", c.baseURL, owner, repo)
	wire := pullRequestWire{
		Title: in.Title,
		Head:  in.Head,
		Base:  in.Base,
		Body:  in.Body,
		Draft: in.Draft,
	}
	bodyBytes, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("github: marshal pr: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("github: build req: %w", err)
	}
	req.Header.Set("Authorization", "token "+instToken)
	req.Header.Set("Content-Type", "application/json")
	c.setCommonHeaders(req)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: open pr: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return "", decodeAPIError(resp)
	}
	var body pullRequestRespMin
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("github: decode pr: %w", err)
	}
	return body.HTMLURL, nil
}

// setCommonHeaders applies the Accept + API-Version pair every GitHub call
// should carry. Pulled into a helper so we only have one place to bump the
// version when GitHub publishes a new one.
func (c *Client) setCommonHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

// decodeAPIError turns a non-2xx response into a typed *APIError. We try to
// pull the "message" field GitHub returns; on parse failure we fall back to
// the raw body so the operator sees something useful in logs.
func decodeAPIError(resp *http.Response) error {
	const maxBody = 64 << 10 // 64 KiB — GitHub errors are tiny
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	msg := ""
	var asJSON struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &asJSON) == nil && asJSON.Message != "" {
		msg = asJSON.Message
	} else {
		msg = string(raw)
	}
	return &APIError{Status: resp.StatusCode, Message: msg}
}

// parseRSAPrivateKeyFromPEM accepts both PKCS1 ("RSA PRIVATE KEY") and PKCS8
// ("PRIVATE KEY") PEM blocks. GitHub-supplied .pem files have historically
// used PKCS1, but App installers exporting from `openssl genrsa` may produce
// either, so we try both before erroring.
func parseRSAPrivateKeyFromPEM(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS1/PKCS8: %w", err)
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("PEM block is not an RSA private key")
	}
	return rsaKey, nil
}
