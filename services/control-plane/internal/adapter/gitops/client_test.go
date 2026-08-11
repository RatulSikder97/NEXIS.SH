package gitops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	recoverywf "github.com/nexis-eco/nexis/services/control-plane/internal/workflow/recovery"
)

func TestClient_HappyPath(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/gitops/open-pr" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"pr_number": 7,
			"pr_url":    "https://github.com/acme/orders/pull/7",
			"branch":    "nexis/recovery-run-1",
			"head_sha":  "abc123",
		})
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "test-token"})
	res, err := c.OpenPR(context.Background(), recoverywf.OpenPRRequest{
		OrgID: "org-1", WorkflowRunID: "run-1",
		Repo: "acme/orders", BranchBase: "main", BranchName: "nexis/recovery-run-1",
		CommitMsg: "fix: x", PatchDiff: "diff --git a/x b/x", PRTitle: "t", PRBody: "b",
	})
	if err != nil {
		t.Fatalf("open pr: %v", err)
	}
	if res.PRNumber != 7 || res.PRURL != "https://github.com/acme/orders/pull/7" {
		t.Fatalf("unexpected: %+v", res)
	}
	// Wire shape must match the gitops service's PROpenRequest JSON tags.
	if gotBody["repo"] != "acme/orders" || gotBody["commit_message"] != "fix: x" ||
		gotBody["patch_diff"] != "diff --git a/x b/x" || gotBody["branch_base"] != "main" {
		t.Fatalf("wire body mismatch: %+v", gotBody)
	}
}

func TestClient_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bearer mismatch", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "wrong"})
	if _, err := c.OpenPR(context.Background(), recoverywf.OpenPRRequest{}); err == nil {
		t.Fatalf("expected error on 401")
	}
}

func TestClient_NoInstallation_412(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "github installation not connected", http.StatusPreconditionFailed)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "t"})
	_, err := c.OpenPR(context.Background(), recoverywf.OpenPRRequest{})
	if err == nil {
		t.Fatalf("expected error on 412")
	}
	// The upstream reason must surface so the timeline frame explains it.
	if want := "installation not connected"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q must contain %q", err.Error(), want)
	}
}
