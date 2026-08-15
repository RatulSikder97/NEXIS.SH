// Package deployengine is the control-plane-side HTTP client for the
// services/deploy-engine sidecar (preview deployments: clone → build → run →
// health check). Mirrors internal/adapter/validator: a Config{BaseURL,
// Token, HTTPClient} + New(cfg) constructor, bearer auth, and calls that
// treat 200 and 422 as structured (non-transport-error) results.
//
// deploy-engine is a separate Go module, so the wire structs are carried in
// workflow/recovery (DeployRequest/DeployResponse) rather than imported —
// same pattern as the validator + gitops clients.
package deployengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
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
// A deploy clones + builds a docker image + boots a container; the request
// budget the caller asks the engine to honour server-side is
// handler.deployTimeoutMs (8 minutes as of this comment), so the client
// timeout carries real slack on top of that rather than racing it — a
// http.Client.Timeout firing first would produce a bare "context deadline
// exceeded" instead of the engine's own, more specific error.
func New(cfg Config) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
	}
	return &Client{cfg: cfg}
}

// compile-time conformance — Client satisfies recoverywf.DeployEngineClient.
var _ recoverywf.DeployEngineClient = (*Client)(nil)

// Deploy issues POST /v1/deploy with the supplied repo coords + clone token
// and returns the structured result. Treats 200 + 422 as non-error responses
// (422 means the pipeline ran but the build/health check failed; the caller
// still wants the logs — same convention as the validator client's handling
// of /v1/validate). Any other status is surfaced as an error so retry
// policies can decide what to do.
func (c *Client) Deploy(ctx context.Context, in recoverywf.DeployRequest) (recoverywf.DeployResponse, error) {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/deploy", bytes.NewReader(body))
	if err != nil {
		return recoverywf.DeployResponse{}, fmt.Errorf("deployengine: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return recoverywf.DeployResponse{}, fmt.Errorf("deployengine: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnprocessableEntity {
		return recoverywf.DeployResponse{}, fmt.Errorf("deployengine: http %d: %s",
			resp.StatusCode, readErrorBody(resp.Body))
	}
	var out recoverywf.DeployResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return recoverywf.DeployResponse{}, fmt.Errorf("deployengine: decode: %w", err)
	}
	return out, nil
}

// Stop issues POST /v1/deploy/{deployment_id}/stop — the engine docker-rm's
// the container. A 404 (this engine instance doesn't track the deployment,
// e.g. after a restart) wraps domain.ErrNotFound so callers can treat
// "already gone" as idempotent success; any other non-200 surfaces as a
// plain error.
func (c *Client) Stop(ctx context.Context, deploymentID string) error {
	url := c.cfg.BaseURL + "/v1/deploy/" + deploymentID + "/stop"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("deployengine: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("deployengine: do: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("deployengine: stop %s: %w", deploymentID, domain.ErrNotFound)
	default:
		return fmt.Errorf("deployengine: http %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
}

// readErrorBody folds a transport-error response body into a short string
// for the wrapped error. JSON bodies contribute their "error" field; raw
// bodies are truncated to 512 bytes so a proxy's HTML error page doesn't
// flood the log line.
func readErrorBody(r io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(r, 512))
	var asJSON struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &asJSON) == nil && asJSON.Error != "" {
		return asJSON.Error
	}
	return string(bytes.TrimSpace(raw))
}
