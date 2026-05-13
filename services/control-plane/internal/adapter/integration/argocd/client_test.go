package argocd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
)

// newClient builds a Client backed by an httptest server. fastBackoff is used
// so the per-test execution stays well under a second even when retries kick
// in (which they should not for these happy/sad-path cases).
func newClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	hx := httpx.New(srv.Client(), httpx.Config{
		RatePerSec:  1000,
		Burst:       1000,
		MaxAttempts: 1,
	})
	return NewClient(srv.URL, "test-token", hx), srv
}

func TestVersion_HappyPath(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			t.Errorf("path = %s, want /api/version", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header = %q, want Bearer test-token", got)
		}
		_, _ = w.Write([]byte(`{"Version":"v2.10.1"}`))
	})

	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "v2.10.1" {
		t.Fatalf("version = %q, want v2.10.1", v)
	}
}

func TestGetApplication_ParsesStatus(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1/applications/") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("project"); got != "demo" {
			t.Errorf("project query = %q, want demo", got)
		}
		_, _ = w.Write([]byte(`{
			"metadata": {"name": "nexis-demo"},
			"spec": {
				"project": "demo",
				"source": {"targetRevision": "main"},
				"destination": {"server": "https://kubernetes.default.svc", "namespace": "prod"}
			},
			"status": {
				"sync":   {"status": "Synced", "revision": "abc123"},
				"health": {"status": "Healthy"}
			}
		}`))
	})

	app, err := c.GetApplication(context.Background(), "demo", "nexis-demo")
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if app.Name != "nexis-demo" {
		t.Errorf("Name = %q", app.Name)
	}
	if app.Project != "demo" {
		t.Errorf("Project = %q", app.Project)
	}
	if app.SyncStatus != "Synced" {
		t.Errorf("SyncStatus = %q", app.SyncStatus)
	}
	if app.HealthStatus != "Healthy" {
		t.Errorf("HealthStatus = %q", app.HealthStatus)
	}
	if app.Revision != "abc123" {
		t.Errorf("Revision = %q", app.Revision)
	}
	if app.Namespace != "prod" {
		t.Errorf("Namespace = %q", app.Namespace)
	}
}

func TestGetApplication_404(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"application 'nope' not found","message":"app missing"}`))
	})

	_, err := c.GetApplication(context.Background(), "demo", "nope")
	if err == nil {
		t.Fatal("want error on 404")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", apiErr.Status)
	}
	if !strings.Contains(apiErr.Message, "not found") {
		t.Errorf("message = %q, want substring 'not found'", apiErr.Message)
	}
}

func TestSyncApp_ReturnsOperation(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/sync") {
			t.Errorf("path = %s, want suffix /sync", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if got["name"] != "nexis-demo" {
			t.Errorf("body.name = %v", got["name"])
		}
		if got["revision"] != "deadbeef" {
			t.Errorf("body.revision = %v", got["revision"])
		}
		if got["prune"] != true {
			t.Errorf("body.prune = %v", got["prune"])
		}

		_, _ = w.Write([]byte(`{
			"status": {
				"sync": {"status": "OutOfSync"},
				"health": {"status": "Progressing"},
				"operationState": {
					"phase": "Running",
					"message": "syncing",
					"startedAt": "2026-05-13T10:00:00Z"
				}
			}
		}`))
	})

	op, err := c.SyncApp(context.Background(), "demo", "nexis-demo", SyncReq{
		Revision: "deadbeef",
		Prune:    true,
	})
	if err != nil {
		t.Fatalf("SyncApp: %v", err)
	}
	if op.Phase != "Running" {
		t.Errorf("phase = %q, want Running", op.Phase)
	}
	if op.Message != "syncing" {
		t.Errorf("message = %q, want syncing", op.Message)
	}
	if op.StartedAt.IsZero() {
		t.Error("StartedAt is zero")
	}
}

func TestRollback_TargetsHistoryID(t *testing.T) {
	var receivedBody map[string]any
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/rollback") {
			t.Errorf("path = %s, want suffix /rollback", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{
			"phase": "Running",
			"message": "rolling back",
			"startedAt": "2026-05-13T10:05:00Z"
		}`))
	})

	op, err := c.Rollback(context.Background(), "demo", "nexis-demo", 42)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	// JSON numbers decode as float64 in map[string]any — accept either form.
	switch v := receivedBody["id"].(type) {
	case float64:
		if int64(v) != 42 {
			t.Errorf("body.id = %v, want 42", v)
		}
	case int64:
		if v != 42 {
			t.Errorf("body.id = %v, want 42", v)
		}
	default:
		t.Errorf("body.id has unexpected type %T (value %v)", receivedBody["id"], receivedBody["id"])
	}
	if op.Phase != "Running" {
		t.Errorf("phase = %q, want Running", op.Phase)
	}
	if op.Message != "rolling back" {
		t.Errorf("message = %q", op.Message)
	}
}

func TestRollback_AcceptsApplicationShape(t *testing.T) {
	// Older ArgoCD versions return the full Application from /rollback; the
	// client must handle that fallback shape without losing the operation
	// state.
	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"metadata": {"name": "nexis-demo"},
			"spec": {"project": "demo"},
			"status": {
				"sync": {"status": "OutOfSync"},
				"health": {"status": "Progressing"},
				"operationState": {
					"phase": "Succeeded",
					"message": "rollback complete",
					"startedAt": "2026-05-13T10:05:00Z",
					"finishedAt": "2026-05-13T10:06:00Z"
				}
			}
		}`))
	})

	op, err := c.Rollback(context.Background(), "demo", "nexis-demo", 7)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if op.Phase != "Succeeded" {
		t.Errorf("phase = %q, want Succeeded", op.Phase)
	}
	if op.FinishedAt == nil {
		t.Fatal("FinishedAt nil")
	}
}

func TestListHistory_ParsesArray(t *testing.T) {
	deployed := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"metadata": {"name": "nexis-demo"},
			"spec": {"project": "demo"},
			"status": {
				"sync": {"status": "Synced"},
				"health": {"status": "Healthy"},
				"history": [
					{"id": 1, "revision": "aaaaaaa", "deployedAt": "2026-05-13T10:00:00Z"},
					{"id": 2, "revision": "bbbbbbb", "deployedAt": "2026-05-13T10:01:00Z"},
					{"id": 3, "revision": "ccccccc", "deployedAt": "2026-05-13T10:02:00Z"}
				]
			}
		}`))
	})

	history, err := c.ListHistory(context.Background(), "demo", "nexis-demo")
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("len(history) = %d, want 3", len(history))
	}
	if history[0].ID != 1 || history[2].ID != 3 {
		t.Errorf("history ids: %d, %d, %d", history[0].ID, history[1].ID, history[2].ID)
	}
	if !history[0].DeployedAt.Equal(deployed) {
		t.Errorf("history[0].DeployedAt = %v, want %v", history[0].DeployedAt, deployed)
	}
}

func TestAPIError_NonJSONBody(t *testing.T) {
	// ArgoCD returns 401/403 on auth failures as plain text in some
	// configurations (e.g. when an admin proxy intercepts the request before
	// it reaches the API server). The client must surface the body verbatim
	// instead of "argocd: status=401: " with an empty message.
	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("missing bearer token"))
	})

	_, err := c.Version(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Status != http.StatusUnauthorized {
		t.Errorf("status = %d", apiErr.Status)
	}
	if !strings.Contains(apiErr.Message, "missing bearer token") {
		t.Errorf("message = %q", apiErr.Message)
	}
}
