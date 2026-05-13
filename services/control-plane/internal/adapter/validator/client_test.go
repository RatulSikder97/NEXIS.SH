package validator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	recoverywf "github.com/nexis-eco/nexis/services/control-plane/internal/workflow/recovery"
)

func TestClient_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tests_passed": true,
			"test_count":   12,
			"fail_count":   0,
			"coverage":     0.0,
			"duration_ms":  4321,
			"logs":         "ok",
		})
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "test-token"})
	res, err := c.Validate(context.Background(), recoverywf.ValidateRequest{
		RepoSHA: "deadbeef", PatchDiff: "",
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.TestsPassed || res.TestCount != 12 {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestClient_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "wrong"})
	if _, err := c.Validate(context.Background(), recoverywf.ValidateRequest{}); err == nil {
		t.Fatalf("expected error on 401")
	}
}

func TestClient_TestsFailed_422(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tests_passed": false,
			"test_count":   12,
			"fail_count":   3,
		})
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: ""})
	res, err := c.Validate(context.Background(), recoverywf.ValidateRequest{})
	if err != nil {
		t.Fatalf("422 should not be an error; got %v", err)
	}
	if res.TestsPassed || res.FailCount != 3 {
		t.Fatalf("unexpected: %+v", res)
	}
}
