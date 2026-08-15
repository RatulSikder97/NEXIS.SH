package handler

// DeploymentsCreate went from fully synchronous to fire-fast-return-then-
// finish-in-a-goroutine because the synchronous shape died against its own
// infrastructure: a cold build routinely exceeds the router's 60-second
// global Timeout middleware, and when it did, the handler's attempt to
// persist a "failed" row afterward used the already-canceled request
// context, so the attempt vanished from history entirely — not even an
// error row. These tests pin the async contract that replaced it:
//
//   - the HTTP response returns before the engine call resolves (proof it is
//     actually async, not just fast on the happy path)
//   - every terminal outcome — engine transport error, engine-reported build
//     failure, success — updates the SAME row the client was handed, so
//     nothing disappears
//   - a build/health-check failure still raises the self-healing incident

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
	"github.com/nexis-eco/nexis/services/control-plane/internal/usecase"
	recoverywf "github.com/nexis-eco/nexis/services/control-plane/internal/workflow/recovery"
)

// --- fakes -------------------------------------------------------------

// fakeDeployProjects implements the full ProjectsService port; only Get is
// exercised by DeploymentsCreate, so the rest are unreachable stubs.
type fakeDeployProjects struct{ project domain.Project }

func (f *fakeDeployProjects) Get(_ context.Context, _ domain.Principal, _ string) (domain.Project, error) {
	return f.project, nil
}

func (f *fakeDeployProjects) Create(context.Context, domain.Principal, usecase.CreateProjectInput) (domain.Project, error) {
	return domain.Project{}, nil
}

func (f *fakeDeployProjects) Update(context.Context, domain.Principal, string, usecase.UpdateProjectInput) (domain.Project, error) {
	return domain.Project{}, nil
}

func (f *fakeDeployProjects) Archive(context.Context, domain.Principal, string) error { return nil }

func (f *fakeDeployProjects) List(context.Context, domain.Principal, string) ([]domain.Project, error) {
	return nil, nil
}

func (f *fakeDeployProjects) UpdatePolicy(context.Context, domain.Principal, string, domain.RecoveryPolicy) (domain.Project, error) {
	return domain.Project{}, nil
}

type fakeDeployGitHub struct {
	token    string
	contents []byte
}

func (f *fakeDeployGitHub) MintInstallationToken(_ context.Context, _ int64) (string, error) {
	return f.token, nil
}

func (f *fakeDeployGitHub) GetFileContents(_ context.Context, _ int64, _, _, _, _ string) ([]byte, error) {
	return f.contents, nil
}

// fakeDeployEngine optionally blocks on `proceed` before returning, which is
// what lets a test prove the HTTP response does not wait for it.
type fakeDeployEngine struct {
	proceed chan struct{}
	resp    recoverywf.DeployResponse
	err     error
}

func (f *fakeDeployEngine) Deploy(ctx context.Context, _ recoverywf.DeployRequest) (recoverywf.DeployResponse, error) {
	if f.proceed != nil {
		select {
		case <-f.proceed:
		case <-ctx.Done():
			return recoverywf.DeployResponse{}, ctx.Err()
		}
	}
	return f.resp, f.err
}

func (f *fakeDeployEngine) Stop(context.Context, string) error { return nil }

// fakeDeployStore is a minimal in-memory DeploymentsStore. `updated` fires
// once per Update call so tests can wait for the goroutine to finish instead
// of racing it with a sleep.
type fakeDeployStore struct {
	mu      sync.Mutex
	rows    map[string]repo.Deployment
	updated chan string
}

func newFakeDeployStore() *fakeDeployStore {
	return &fakeDeployStore{rows: map[string]repo.Deployment{}, updated: make(chan string, 8)}
}

func (f *fakeDeployStore) Create(_ context.Context, d repo.Deployment) (repo.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[d.ID] = d
	return d, nil
}

func (f *fakeDeployStore) Get(_ context.Context, id string) (repo.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[id], nil
}

func (f *fakeDeployStore) List(context.Context, string, int) ([]repo.Deployment, error) {
	return nil, nil
}

func (f *fakeDeployStore) Update(_ context.Context, id string, fields map[string]any) (repo.Deployment, error) {
	f.mu.Lock()
	row := f.rows[id]
	if v, ok := fields["status"].(string); ok {
		row.Status = v
	}
	if v, ok := fields["url"].(string); ok {
		row.URL = v
	}
	if v, ok := fields["error"].(string); ok {
		row.Error = v
	}
	if v, ok := fields["build_log"].(string); ok {
		row.BuildLog = v
	}
	if v, ok := fields["dockerfile_source"].(string); ok {
		row.DockerfileSource = v
	}
	f.rows[id] = row
	f.mu.Unlock()

	select {
	case f.updated <- id:
	default:
	}
	return row, nil
}

func (f *fakeDeployStore) snapshot(id string) repo.Deployment {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[id]
}

// waitUpdated blocks until fakeDeployStore.Update has fired at least once,
// or fails the test after a bound well under Go's own test timeout.
func waitUpdated(t *testing.T, ch chan string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the async deploy to update its row")
	}
}

type fakeDeployIncidents struct {
	mu     sync.Mutex
	rows   []domain.RawIncident
	signal chan struct{}
}

func (f *fakeDeployIncidents) Insert(_ context.Context, _ string, raw domain.RawIncident) error {
	f.mu.Lock()
	f.rows = append(f.rows, raw)
	f.mu.Unlock()
	if f.signal != nil {
		select {
		case f.signal <- struct{}{}:
		default:
		}
	}
	return nil
}

func (f *fakeDeployIncidents) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

// --- test scaffolding ----------------------------------------------------

func testProject() domain.Project {
	return domain.Project{
		ID:    "proj-1",
		OrgID: "org-1",
		Selectors: domain.ProjectSelectors{
			GitHubRepo:           "nexis-sh/fixture",
			GitHubInstallationID: 42,
			GitHubDefaultBranch:  "main",
		},
	}
}

func deployRequest(t *testing.T) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/proj-1/deploy", nil)
	princ := domain.Principal{OrgID: "org-1", UserID: "u-1", Role: "owner"}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "proj-1")
	req = req.WithContext(context.WithValue(appmw.WithPrincipal(req.Context(), princ), chi.RouteCtxKey, rctx))
	return req
}

// --- tests -----------------------------------------------------------------

func TestDeploymentsCreate_RespondsBeforeTheEngineFinishes(t *testing.T) {
	store := newFakeDeployStore()
	gate := make(chan struct{}) // never closed during the assertion below
	deps := DeploymentsDeps{
		Projects:    &fakeDeployProjects{project: testProject()},
		GitHub:      &fakeDeployGitHub{token: "tok"},
		Engine:      &fakeDeployEngine{proceed: gate, resp: recoverywf.DeployResponse{Status: repo.DeploymentStatusRunning}},
		Deployments: store,
	}

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		DeploymentsCreate(deps)(rec, deployRequest(t))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return — it is blocking on the engine call, which is exactly the bug this test guards against")
	}
	close(gate) // let the goroutine finish so it doesn't leak past the test

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	var out deploymentResp
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Status != repo.DeploymentStatusBuilding {
		t.Errorf("status in body = %q, want %q", out.Status, repo.DeploymentStatusBuilding)
	}
	if out.ID == "" {
		t.Error("response carries no deployment id — the client has nothing to poll")
	}
}

func TestDeploymentsCreate_Success_UpdatesTheSameRowToRunning(t *testing.T) {
	store := newFakeDeployStore()
	deps := DeploymentsDeps{
		Projects: &fakeDeployProjects{project: testProject()},
		GitHub:   &fakeDeployGitHub{token: "tok"},
		Engine: &fakeDeployEngine{resp: recoverywf.DeployResponse{
			Status: repo.DeploymentStatusRunning, URL: "http://localhost:5555", ImageTag: "img:abc",
		}},
		Deployments: store,
	}

	rec := httptest.NewRecorder()
	DeploymentsCreate(deps)(rec, deployRequest(t))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var accepted deploymentResp
	_ = json.Unmarshal(rec.Body.Bytes(), &accepted)

	waitUpdated(t, store.updated)
	row := store.snapshot(accepted.ID)
	if row.Status != repo.DeploymentStatusRunning {
		t.Errorf("final status = %q, want running", row.Status)
	}
	if row.URL != "http://localhost:5555" {
		t.Errorf("url = %q", row.URL)
	}
}

func TestDeploymentsCreate_EngineTransportError_RowEndsFailedNotMissing(t *testing.T) {
	store := newFakeDeployStore()
	deps := DeploymentsDeps{
		Projects:    &fakeDeployProjects{project: testProject()},
		GitHub:      &fakeDeployGitHub{token: "tok"},
		Engine:      &fakeDeployEngine{err: context.DeadlineExceeded},
		Deployments: store,
	}

	rec := httptest.NewRecorder()
	DeploymentsCreate(deps)(rec, deployRequest(t))
	var accepted deploymentResp
	_ = json.Unmarshal(rec.Body.Bytes(), &accepted)

	// Before the async fix, this exact scenario (the outer context already
	// gone by the time the engine call returns) meant the failed-row Create
	// itself failed, and the attempt left no trace. Now it's an Update
	// against a row that was already persisted synchronously, on a fresh
	// context — it must land regardless of what happened to the request.
	waitUpdated(t, store.updated)
	row := store.snapshot(accepted.ID)
	if row.Status != repo.DeploymentStatusFailed {
		t.Fatalf("status = %q, want failed — the attempt must not vanish from history", row.Status)
	}
	if row.Error == "" {
		t.Error("failed row carries no error message")
	}
}

func TestDeploymentsCreate_BuildFailure_UpdatesRowAndRaisesIncident(t *testing.T) {
	store := newFakeDeployStore()
	incidents := &fakeDeployIncidents{signal: make(chan struct{}, 1)}
	deps := DeploymentsDeps{
		Projects: &fakeDeployProjects{project: testProject()},
		GitHub:   &fakeDeployGitHub{token: "tok"},
		Engine: &fakeDeployEngine{resp: recoverywf.DeployResponse{
			Status: repo.DeploymentStatusFailed, Error: "health check never became healthy", BuildLog: "step 4/7 ...",
		}},
		Deployments: store,
		Incidents:   incidents,
	}

	rec := httptest.NewRecorder()
	DeploymentsCreate(deps)(rec, deployRequest(t))
	var accepted deploymentResp
	_ = json.Unmarshal(rec.Body.Bytes(), &accepted)

	waitUpdated(t, store.updated)
	row := store.snapshot(accepted.ID)
	if row.Status != repo.DeploymentStatusFailed {
		t.Fatalf("status = %q, want failed", row.Status)
	}
	if row.Error != "health check never became healthy" {
		t.Errorf("error = %q", row.Error)
	}

	select {
	case <-incidents.signal:
	case <-time.After(2 * time.Second):
		t.Fatal("no incident was raised for the failed deploy")
	}
	if incidents.count() != 1 {
		t.Errorf("incidents raised = %d, want 1", incidents.count())
	}
}

func TestDeploymentUpdateFields_MapsOntoTheAllowedColumnsOnly(t *testing.T) {
	finished := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	row := repo.Deployment{
		Status: repo.DeploymentStatusRunning, URL: "http://x", Port: 8080,
		ImageTag: "img:1", DockerfileSource: "repo", DetectedStack: "node",
		BuildLog: "b", ContainerLog: "c", Error: "", FinishedAt: &finished,
	}
	fields := deploymentUpdateFields(row)
	for _, key := range []string{"status", "url", "port", "image_tag", "dockerfile_source", "detected_stack", "build_log", "container_log", "error", "finished_at"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("deploymentUpdateFields missing key %q", key)
		}
	}
	if _, present := fields["started_at"]; present {
		t.Error("deploymentUpdateFields must not touch started_at — that stays the original request time")
	}
	if fields["finished_at"] != finished {
		t.Errorf("finished_at = %v, want %v", fields["finished_at"], finished)
	}
}

func TestDeploymentUpdateFields_DefaultsFinishedAtWhenEngineOmitsIt(t *testing.T) {
	fields := deploymentUpdateFields(repo.Deployment{Status: repo.DeploymentStatusFailed})
	ts, ok := fields["finished_at"].(time.Time)
	if !ok || time.Since(ts) > 5*time.Second {
		t.Errorf("finished_at not defaulted to now: %v", fields["finished_at"])
	}
}
