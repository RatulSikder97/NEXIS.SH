// Package validator is the control-plane-side HTTP client for the
// services/validator service. The activity in workflow/recovery talks to the
// sandbox through this client; Phase 7 will swap the validator endpoint for
// Modal without changing callers.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	recoverywf "github.com/nexis-eco/nexis/services/control-plane/internal/workflow/recovery"
)

// Config bundles the dial-string + auth + optional http client.
type Config struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// Client is the HTTP wrapper. Zero value is unusable; construct via New.
type Client struct{ cfg Config }

// New returns a Client with a sensible default timeout if none is supplied.
// The validator service can run pytest for up to ~60s; the client adds slack
// for network + cold start.
func New(cfg Config) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 90 * time.Second}
	}
	return &Client{cfg: cfg}
}

// compile-time conformance — Client satisfies recoverywf.ValidatorClient.
var _ recoverywf.ValidatorClient = (*Client)(nil)

// Validate issues POST /v1/validate with the supplied patch + repo SHA and
// returns the structured result. Treats 200 + 422 as non-error responses
// (422 means the sandbox ran but tests failed; the caller still wants the
// summary). Any other status is surfaced as an error.
func (c *Client) Validate(ctx context.Context, in recoverywf.ValidateRequest) (recoverywf.ValidateResponse, error) {
	body, _ := json.Marshal(map[string]any{
		"repo_sha":   in.RepoSHA,
		"patch_diff": in.PatchDiff,
		"image":      in.Image,
		"timeout_ms": in.TimeoutMs,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/validate", bytes.NewReader(body))
	if err != nil {
		return recoverywf.ValidateResponse{}, fmt.Errorf("validator: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return recoverywf.ValidateResponse{}, fmt.Errorf("validator: do: %w", err)
	}
	defer resp.Body.Close()

	var out struct {
		TestsPassed bool    `json:"tests_passed"`
		TestCount   int     `json:"test_count"`
		FailCount   int     `json:"fail_count"`
		Coverage    float64 `json:"coverage"`
		DurationMs  int64   `json:"duration_ms"`
		Logs        string  `json:"logs"`

		PatchApplied bool   `json:"patch_applied"`
		PatchError   string `json:"patch_error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return recoverywf.ValidateResponse{}, fmt.Errorf("validator: decode: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnprocessableEntity {
		return recoverywf.ValidateResponse{
			Logs:       out.Logs,
			DurationMs: out.DurationMs,
		}, fmt.Errorf("validator: http %d", resp.StatusCode)
	}
	return recoverywf.ValidateResponse{
		TestsPassed: out.TestsPassed,
		TestCount:   out.TestCount,
		FailCount:   out.FailCount,
		Coverage:    out.Coverage,
		DurationMs:   out.DurationMs,
		Logs:         out.Logs,
		PatchApplied: out.PatchApplied,
		PatchError:   out.PatchError,
	}, nil
}
