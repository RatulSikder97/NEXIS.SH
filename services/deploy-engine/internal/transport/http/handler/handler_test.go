package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/deploy-engine/internal/deploy"
)

const testToken = "test-token"

// fakeDeployer scripts the Deployer surface for handler tests.
type fakeDeployer struct {
	res     deploy.Result
	err     error
	stopErr error
	stopped []string
}

func (f *fakeDeployer) Deploy(_ context.Context, in deploy.Request) (deploy.Result, error) {
	res := f.res
	res.DeploymentID = in.DeploymentID
	return res, f.err
}

func (f *fakeDeployer) Stop(_ context.Context, name string) error {
	f.stopped = append(f.stopped, name)
	return f.stopErr
}

func newRouter(f *fakeDeployer, store *deploy.Store) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := chi.NewRouter()
	r.Get("/healthz", Healthz("deploy-engine"))
	r.Post("/v1/deploy", Deploy(f, store, testToken, logger))
	r.Get("/v1/deploy/{deployment_id}", GetDeployment(store, testToken))
	r.Post("/v1/deploy/{deployment_id}/stop", StopDeployment(f, store, testToken, logger))
	return r
}

func doJSON(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const validBody = `{"deployment_id":"d-1","project_id":"p-1","org_id":"o-1","repo":"owner/name","branch":"main","github_token":""}`

func TestHealthz(t *testing.T) {
	h := newRouter(&fakeDeployer{}, deploy.NewStore())
	rec := doJSON(t, h, http.MethodGet, "/healthz", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "ok" || out["service"] != "deploy-engine" {
		t.Fatalf("unexpected body: %v", out)
	}
}

func TestDeploy_Unauthorized(t *testing.T) {
	h := newRouter(&fakeDeployer{}, deploy.NewStore())
	for _, tok := range []string{"", "wrong"} {
		rec := doJSON(t, h, http.MethodPost, "/v1/deploy", tok, validBody)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("token %q: code = %d, want 401", tok, rec.Code)
		}
		var out map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out["error"] == "" {
			t.Fatalf("401 must carry JSON error, got %q", rec.Body.String())
		}
	}
}

func TestDeploy_BadBody(t *testing.T) {
	h := newRouter(&fakeDeployer{}, deploy.NewStore())
	if rec := doJSON(t, h, http.MethodPost, "/v1/deploy", testToken, "{not json"); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed json: code = %d, want 400", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPost, "/v1/deploy", testToken, `{"repo":"o/r"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing fields: code = %d, want 400", rec.Code)
	}
}

func TestDeploy_Running200(t *testing.T) {
	url := "http://localhost:34567"
	port := 34567
	f := &fakeDeployer{res: deploy.Result{
		Status: deploy.StatusRunning, URL: &url, Port: &port,
		ImageTag: "nexis-preview-p-1:abc", DockerfileSource: "generated", DetectedStack: "static",
	}}
	store := deploy.NewStore()
	h := newRouter(f, store)

	rec := doJSON(t, h, http.MethodPost, "/v1/deploy", testToken, validBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out deploy.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != deploy.StatusRunning || out.URL == nil || *out.URL != url || out.DeploymentID != "d-1" {
		t.Fatalf("unexpected: %+v", out)
	}
	if _, ok := store.Get("d-1"); !ok {
		t.Fatal("deployment not tracked after success")
	}
}

func TestDeploy_Failed422_WithNullURLAndPort(t *testing.T) {
	f := &fakeDeployer{res: deploy.Result{
		Status: deploy.StatusFailed, Error: "docker build failed: exit status 1",
		DetectedStack: "node", DockerfileSource: "generated", BuildLog: "npm ERR!",
	}}
	store := deploy.NewStore()
	h := newRouter(f, store)

	rec := doJSON(t, h, http.MethodPost, "/v1/deploy", testToken, validBody)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422", rec.Code)
	}
	// Contract: url + port must serialize as JSON null on failure.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if v, present := raw["url"]; !present || v != nil {
		t.Fatalf("url = %v, want explicit null", v)
	}
	if v, present := raw["port"]; !present || v != nil {
		t.Fatalf("port = %v, want explicit null", v)
	}
	if raw["error"] == "" || raw["status"] != "failed" {
		t.Fatalf("unexpected failure body: %v", raw)
	}
	// Failed deployments are still tracked for GET.
	if _, ok := store.Get("d-1"); !ok {
		t.Fatal("failed deployment must still be tracked")
	}
}

func TestDeploy_InternalError500(t *testing.T) {
	f := &fakeDeployer{err: errors.New("mkdir temp: disk full")}
	h := newRouter(f, deploy.NewStore())
	rec := doJSON(t, h, http.MethodPost, "/v1/deploy", testToken, validBody)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out["error"] == "" {
		t.Fatalf("500 must carry JSON error, got %q", rec.Body.String())
	}
}

func TestGetDeployment(t *testing.T) {
	store := deploy.NewStore()
	h := newRouter(&fakeDeployer{}, store)

	if rec := doJSON(t, h, http.MethodGet, "/v1/deploy/nope", testToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: code = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodGet, "/v1/deploy/nope", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: code = %d, want 401", rec.Code)
	}

	store.Put("d-9", deploy.Record{Result: deploy.Result{DeploymentID: "d-9", Status: deploy.StatusRunning}})
	rec := doJSON(t, h, http.MethodGet, "/v1/deploy/d-9", testToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var out deploy.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.DeploymentID != "d-9" {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestStopDeployment(t *testing.T) {
	f := &fakeDeployer{}
	store := deploy.NewStore()
	h := newRouter(f, store)

	if rec := doJSON(t, h, http.MethodPost, "/v1/deploy/nope/stop", testToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: code = %d, want 404", rec.Code)
	}

	store.Put("d-2", deploy.Record{
		Result:        deploy.Result{DeploymentID: "d-2", Status: deploy.StatusRunning},
		ContainerName: "nexis-preview-d-2",
	})
	rec := doJSON(t, h, http.MethodPost, "/v1/deploy/d-2/stop", testToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out["status"] != "stopped" {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if len(f.stopped) != 1 || f.stopped[0] != "nexis-preview-d-2" {
		t.Fatalf("stop called with %v", f.stopped)
	}
	if got, _ := store.Get("d-2"); got.Result.Status != deploy.StatusStopped {
		t.Fatalf("status = %q, want stopped", got.Result.Status)
	}
}

func TestStopDeployment_EngineError500(t *testing.T) {
	f := &fakeDeployer{stopErr: errors.New("docker daemon unreachable")}
	store := deploy.NewStore()
	store.Put("d-3", deploy.Record{ContainerName: "c", Result: deploy.Result{Status: deploy.StatusRunning}})
	h := newRouter(f, store)
	rec := doJSON(t, h, http.MethodPost, "/v1/deploy/d-3/stop", testToken, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
	if got, _ := store.Get("d-3"); got.Result.Status == deploy.StatusStopped {
		t.Fatal("status must not flip to stopped when the engine errored")
	}
}
