package deployengine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	recoverywf "github.com/nexis-eco/nexis/services/control-plane/internal/workflow/recovery"
)

func TestClient_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/deploy" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var in recoverywf.DeployRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.DeploymentID != "d-1" || in.Repo != "o/r" {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deployment_id":     "d-1",
			"status":            "running",
			"url":               "http://localhost:34567",
			"port":              34567,
			"image_tag":         "nexis-preview-p-1:abc123def456",
			"dockerfile_source": "generated",
			"detected_stack":    "node",
			"build_log":         "ok",
			"container_log":     "listening",
			"started_at":        "2026-08-11T00:00:00Z",
			"finished_at":       "2026-08-11T00:00:30Z",
		})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "test-token"})
	res, err := c.Deploy(context.Background(), recoverywf.DeployRequest{
		DeploymentID: "d-1", ProjectID: "p-1", OrgID: "o-1",
		Repo: "o/r", Branch: "main",
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if res.Status != "running" || res.URL != "http://localhost:34567" || res.Port != 34567 {
		t.Fatalf("unexpected: %+v", res)
	}
	if res.StartedAt.IsZero() || res.FinishedAt.IsZero() {
		t.Fatalf("timestamps not parsed: %+v", res)
	}
}

func TestClient_Failed422IsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deployment_id":     "d-2",
			"status":            "failed",
			"url":               nil,
			"port":              nil,
			"detected_stack":    "unknown",
			"dockerfile_source": "",
			"error":             "no Dockerfile and no recognized stack marker (package.json/requirements.txt/go.mod/index.html) at repo root",
			"build_log":         "",
			"container_log":     "",
		})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t"})
	res, err := c.Deploy(context.Background(), recoverywf.DeployRequest{
		DeploymentID: "d-2", ProjectID: "p", Repo: "o/r", Branch: "main",
	})
	if err != nil {
		t.Fatalf("422 should not be an error; got %v", err)
	}
	// JSON nulls decode as zero values on the value-typed fields.
	if res.Status != "failed" || res.URL != "" || res.Port != 0 || res.Error == "" {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestClient_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "wrong"})
	if _, err := c.Deploy(context.Background(), recoverywf.DeployRequest{}); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestClient_StopHappyPath(t *testing.T) {
	stopped := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/deploy/d-1/stop" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		stopped = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "test-token"})
	if err := c.Stop(context.Background(), "d-1"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !stopped {
		t.Fatal("stop endpoint never hit")
	}
}

func TestClient_StopUntracked404WrapsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "deployment not tracked"})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t"})
	err := c.Stop(context.Background(), "gone")
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("404 must wrap domain.ErrNotFound; got %v", err)
	}
}
