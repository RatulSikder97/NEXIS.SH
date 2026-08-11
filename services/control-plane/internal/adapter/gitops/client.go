// Package gitops is the control-plane-side HTTP client for the
// services/gitops sidecar. The GitOpsDeploy activity in workflow/recovery
// talks to the PR-opening endpoint through this client; the wire shape
// mirrors services/gitops/internal/domain/pr.go (separate Go module, so the
// structs are duplicated rather than imported — same pattern as the
// validator client).
package gitops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
// Opening a PR walks branch-create + commit + PR through the GitHub API, so
// the budget matches the validator client's 90s (network + cold start slack).
func New(cfg Config) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 90 * time.Second}
	}
	return &Client{cfg: cfg}
}

// compile-time conformance — Client satisfies recoverywf.GitOpsClient.
var _ recoverywf.GitOpsClient = (*Client)(nil)

// OpenPR issues POST /v1/gitops/open-pr with the supplied patch + repo
// coords and returns the opened PR's pointers. Only 201 is a success — the
// gitops service uses 400/412/500 for bad diffs, missing installations, and
// upstream failures respectively, all of which surface as errors here so the
// activity retry policy can decide what to do.
func (c *Client) OpenPR(ctx context.Context, in recoverywf.OpenPRRequest) (recoverywf.OpenPRResponse, error) {
	body, _ := json.Marshal(map[string]any{
		"org_id":          in.OrgID,
		"workspace_id":    in.WorkspaceID,
		"workflow_run_id": in.WorkflowRunID,
		"repo":            in.Repo,
		"branch_base":     in.BranchBase,
		"branch_name":     in.BranchName,
		"commit_message":  in.CommitMsg,
		"patch_diff":      in.PatchDiff,
		"pr_title":        in.PRTitle,
		"pr_body":         in.PRBody,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/gitops/open-pr", bytes.NewReader(body))
	if err != nil {
		return recoverywf.OpenPRResponse{}, fmt.Errorf("gitops: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return recoverywf.OpenPRResponse{}, fmt.Errorf("gitops: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		// Error bodies are plain-text http.Error output — fold the first
		// line into the error so the timeline frame explains the failure.
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return recoverywf.OpenPRResponse{}, fmt.Errorf("gitops: http %d: %s",
			resp.StatusCode, bytes.TrimSpace(msg))
	}

	var out struct {
		PRNumber int       `json:"pr_number"`
		PRURL    string    `json:"pr_url"`
		Branch   string    `json:"branch"`
		HeadSHA  string    `json:"head_sha"`
		OpenedAt time.Time `json:"opened_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return recoverywf.OpenPRResponse{}, fmt.Errorf("gitops: decode: %w", err)
	}
	return recoverywf.OpenPRResponse{
		PRNumber: out.PRNumber,
		PRURL:    out.PRURL,
		Branch:   out.Branch,
		HeadSHA:  out.HeadSHA,
		OpenedAt: out.OpenedAt,
	}, nil
}
