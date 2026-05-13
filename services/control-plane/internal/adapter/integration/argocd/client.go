// Package argocd implements the ArgoCD IntegrationProvider. This file defines
// the REST client used by Provider — token-only auth against ArgoCD's HTTP API
// (we deliberately avoid the gRPC apiclient library, which would pull the
// entire argo-cd module tree into the control-plane binary just for sync +
// rollback).
//
// All requests flow through the shared httpx.Client so per-host rate limits,
// circuit breaker, retries-on-429, and authorization-header redaction apply
// uniformly across every adapter (github / sentry / slack / argocd).
package argocd

import (
	"bytes"
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

// Client talks to a single ArgoCD installation using a bearer token (typically
// a project-role token minted via /api/v1/projects/{p}/role/{r}/token). Safe
// for concurrent use — every call carves out a fresh *http.Request.
type Client struct {
	base  string
	token string
	http  *httpx.Client
}

// NewClient constructs a Client. base may include a trailing slash; it is
// trimmed so we can join segments unconditionally with "/". http MUST be
// non-nil — callers should pass the shared httpx wrapper so the same
// breaker/rate-limit state covers every adapter.
func NewClient(base, token string, http *httpx.Client) *Client {
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  http,
	}
}

// APIError is the typed error returned for any non-2xx response. Callers can
// inspect Status for 401/403/404 without string-matching the message.
type APIError struct {
	Status  int
	Message string
}

// Error implements error. Format mirrors the REST envelope so logs are useful
// when the underlying ArgoCD API returns a plain text message instead of JSON.
func (e *APIError) Error() string {
	return fmt.Sprintf("argocd: status=%d: %s", e.Status, e.Message)
}

// App is the trimmed-down view of an ArgoCD Application we expose upward. We
// pull only the fields we need for status reporting + sync/rollback; the full
// Application CRD has hundreds of fields and pinning to the upstream Go types
// would defeat the point of using the REST surface.
type App struct {
	Name         string
	Project      string
	SyncStatus   string
	HealthStatus string
	Revision     string
	Server       string
	Namespace    string
}

// SyncReq is the body of POST /applications/{name}/sync. Revision is optional
// — empty means "use whatever's at the tracking branch HEAD". Prune controls
// whether resources removed from the manifest are deleted from the cluster.
type SyncReq struct {
	Revision string
	Prune    bool
	DryRun   bool
}

// Operation mirrors operationState in the ArgoCD API. Phase is the high-level
// "Running" / "Succeeded" / "Failed" classification; the underlying API also
// emits "Error" and "Terminating" which we collapse into the same field.
type Operation struct {
	Phase      string
	Message    string
	StartedAt  time.Time
	FinishedAt *time.Time
}

// HistoryEntry is one entry of GET /applications/{name}/revisions. ID is the
// integer the rollback endpoint accepts as its history pointer.
type HistoryEntry struct {
	ID         int64
	Revision   string
	DeployedAt time.Time
}

// versionResp is the wire shape of GET /api/version. Only Version is exposed
// upward — the build/git info is useful for debugging but not for our flows.
type versionResp struct {
	Version string `json:"Version"`
}

// appResp is the slice of the Application CRD we deserialize. The full schema
// is enormous; embedding only the fields we read keeps the wire decode tight.
type appResp struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Project     string `json:"project"`
		Source      struct {
			TargetRevision string `json:"targetRevision"`
		} `json:"source"`
		Destination struct {
			Server    string `json:"server"`
			Namespace string `json:"namespace"`
		} `json:"destination"`
	} `json:"spec"`
	Status struct {
		Sync struct {
			Status   string `json:"status"`
			Revision string `json:"revision"`
		} `json:"sync"`
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
		OperationState *operationStateResp `json:"operationState"`
		History        []historyResp       `json:"history"`
	} `json:"status"`
}

// operationStateResp is the operationState field of an Application or the
// direct body of the sync/rollback response. We accept both — the sync
// endpoint returns the full Application with operationState nested, while
// rollback returns only the operationState object.
type operationStateResp struct {
	Phase      string     `json:"phase"`
	Message    string     `json:"message"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// historyResp is one entry of Application.Status.History.
type historyResp struct {
	ID         int64     `json:"id"`
	Revision   string    `json:"revision"`
	DeployedAt time.Time `json:"deployedAt"`
}

// errorResp is the JSON envelope ArgoCD returns on 4xx/5xx. We try this first;
// if decoding fails the raw body is propagated verbatim in APIError.Message so
// operators still get a useful message in audit logs.
type errorResp struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// Version probes /api/version. Used at Connect time to validate reachability
// and surfaced through Status for the integrations health pill.
func (c *Client) Version(ctx context.Context) (string, error) {
	var out versionResp
	if err := c.do(ctx, http.MethodGet, "/api/version", nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// GetApplication returns the named application. project is optional but
// recommended — ArgoCD allows multiple apps with the same name in different
// projects, and the project filter narrows authz to that project's bound
// token.
func (c *Client) GetApplication(ctx context.Context, project, name string) (App, error) {
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	path := "/api/v1/applications/" + url.PathEscape(name)
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out appResp
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return App{}, err
	}
	return App{
		Name:         out.Metadata.Name,
		Project:      out.Spec.Project,
		SyncStatus:   out.Status.Sync.Status,
		HealthStatus: out.Status.Health.Status,
		Revision:     out.Status.Sync.Revision,
		Server:       out.Spec.Destination.Server,
		Namespace:    out.Spec.Destination.Namespace,
	}, nil
}

// SyncApp triggers a sync. The returned Operation reflects the *initial* state
// reported by the server — typically "Running" — not the terminal phase.
// Callers wanting to wait for completion should poll GetApplication's
// operationState (Phase 6's Approval Gate does this).
func (c *Client) SyncApp(ctx context.Context, project, name string, req SyncReq) (Operation, error) {
	body := map[string]any{
		"name":     name,
		"prune":    req.Prune,
		"dryRun":   req.DryRun,
	}
	if req.Revision != "" {
		body["revision"] = req.Revision
	}
	if project != "" {
		body["project"] = project
	}
	var out appResp
	path := "/api/v1/applications/" + url.PathEscape(name) + "/sync"
	if err := c.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return Operation{}, err
	}
	if out.Status.OperationState == nil {
		// ArgoCD always returns operationState on a successful sync POST; an
		// absent field is a contract violation that we surface so the caller
		// can decide whether to retry.
		return Operation{}, errors.New("argocd: sync response missing operationState")
	}
	return toOperation(out.Status.OperationState), nil
}

// Rollback reverts the application to the supplied history id (obtained via
// ListHistory). ArgoCD's rollback endpoint returns operationState directly,
// not the full Application — we accept both shapes for forward compatibility.
func (c *Client) Rollback(ctx context.Context, project, name string, historyID int64) (Operation, error) {
	body := map[string]any{
		"id": historyID,
	}
	if project != "" {
		body["project"] = project
	}
	if name != "" {
		body["name"] = name
	}
	path := "/api/v1/applications/" + url.PathEscape(name) + "/rollback"
	raw, err := c.doRaw(ctx, http.MethodPost, path, body)
	if err != nil {
		return Operation{}, err
	}
	// Try the operationState-only shape first (current ArgoCD); fall back to
	// the full Application shape (older versions).
	var direct operationStateResp
	if err := json.Unmarshal(raw, &direct); err == nil && direct.Phase != "" {
		return toOperation(&direct), nil
	}
	var app appResp
	if err := json.Unmarshal(raw, &app); err != nil {
		return Operation{}, fmt.Errorf("argocd: decode rollback: %w", err)
	}
	if app.Status.OperationState == nil {
		return Operation{}, errors.New("argocd: rollback response missing operationState")
	}
	return toOperation(app.Status.OperationState), nil
}

// ListHistory returns the deploy history embedded in the application status,
// most-recent last. ArgoCD does not expose a dedicated history endpoint; we
// surface the slice on Application.Status.History under a focused name so the
// caller does not have to know that detail.
func (c *Client) ListHistory(ctx context.Context, project, name string) ([]HistoryEntry, error) {
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	path := "/api/v1/applications/" + url.PathEscape(name)
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out appResp
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if len(out.Status.History) == 0 {
		return nil, nil
	}
	entries := make([]HistoryEntry, 0, len(out.Status.History))
	for _, h := range out.Status.History {
		entries = append(entries, HistoryEntry{
			ID:         h.ID,
			Revision:   h.Revision,
			DeployedAt: h.DeployedAt,
		})
	}
	return entries, nil
}

// do executes a JSON request and decodes a JSON response into out (pass nil
// for endpoints that only matter for their status code). 2xx → out is
// populated; non-2xx → returns *APIError.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	raw, err := c.doRaw(ctx, method, path, body)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("argocd: decode %s %s: %w", method, path, err)
	}
	return nil
}

// doRaw returns the response body bytes on success. Splitting raw + typed
// decode lets Rollback try two response shapes against the same bytes without
// a second network round-trip.
func (c *Client) doRaw(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("argocd: marshal request: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("argocd: build request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("argocd: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Bound the read at 1 MiB. ArgoCD application docs cap at ~256 KiB in
	// practice; this guard catches a misconfigured upstream from exhausting
	// memory.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("argocd: read body: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	return nil, toAPIError(resp.StatusCode, raw)
}

// toAPIError converts a non-2xx body into the typed *APIError. We prefer
// ArgoCD's JSON envelope (`error` / `message` keys) but fall back to the raw
// body so a plain-text 502 from a reverse proxy still produces a readable
// audit log.
func toAPIError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if len(body) > 0 {
		var env errorResp
		if err := json.Unmarshal(body, &env); err == nil {
			if env.Error != "" {
				msg = env.Error
			} else if env.Message != "" {
				msg = env.Message
			}
		}
	}
	if msg == "" {
		msg = http.StatusText(status)
		if msg == "" {
			msg = "status " + strconv.Itoa(status)
		}
	}
	return &APIError{Status: status, Message: msg}
}

// toOperation flattens an operationStateResp into our exported Operation.
// Centralising the conversion keeps the field-renaming logic in one place.
func toOperation(s *operationStateResp) Operation {
	return Operation{
		Phase:      s.Phase,
		Message:    s.Message,
		StartedAt:  s.StartedAt,
		FinishedAt: s.FinishedAt,
	}
}
