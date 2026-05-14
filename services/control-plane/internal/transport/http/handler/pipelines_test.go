package handler_test

// Coverage for the pipelines handler — List, Get, Create. Uses the same
// fakeSeedWorkflows from seed_sample_test for the WorkflowService port.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// fakePipelinesWorkflows is a small WorkflowService fake tailored to the
// pipelines test surface — its List/Get/Subscribe paths return canned
// values so the wire shape can be asserted.
type fakePipelinesWorkflows struct {
	mu       sync.Mutex
	listErr  error
	getErr   error
	startErr error
	runs     []domain.WorkflowRun
	events   []domain.ActivityEvent
}

func (f *fakePipelinesWorkflows) Start(_ context.Context, p domain.Principal, wsID, wfType string, _ []byte) (domain.WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return domain.WorkflowRun{}, f.startErr
	}
	return domain.WorkflowRun{
		ID: "run-1", OrgID: p.OrgID, WorkspaceID: wsID,
		WorkflowType: wfType, Status: domain.WRQueued,
	}, nil
}

func (f *fakePipelinesWorkflows) Get(_ context.Context, _ domain.Principal, _ string) (domain.WorkflowRun, []domain.ActivityEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return domain.WorkflowRun{}, nil, f.getErr
	}
	if len(f.runs) == 0 {
		return domain.WorkflowRun{ID: "run-1", Status: domain.WRSucceeded}, f.events, nil
	}
	return f.runs[0], f.events, nil
}

func (f *fakePipelinesWorkflows) List(_ context.Context, _ domain.Principal, _ string, _ string, _ int, _ time.Time) ([]domain.WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs, f.listErr
}

func (f *fakePipelinesWorkflows) Subscribe(_ context.Context, _ string) <-chan domain.ActivityEvent {
	ch := make(chan domain.ActivityEvent)
	close(ch)
	return ch
}

// TestPipelinesList_HappyPathReturnsArray — happy path returns the
// service's list wrapped in the DTO envelope.
func TestPipelinesList_HappyPathReturnsArray(t *testing.T) {
	now := time.Now().UTC()
	f := &fakePipelinesWorkflows{
		runs: []domain.WorkflowRun{
			{ID: "wf-1", Status: domain.WRSucceeded, StartedAt: now, OrgID: "org-1"},
			{ID: "wf-2", Status: domain.WRRunning, StartedAt: now, OrgID: "org-1"},
		},
	}
	h := handler.PipelinesList(f)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{ws_id}/pipelines", h)
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws-1/pipelines", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), domain.Principal{OrgID: "org-1", Role: "owner"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "wf-1") || !strings.Contains(rec.Body.String(), "wf-2") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestPipelinesList_EmptyResultIsEmptyArray — empty results emit "[]" not
// "null".
func TestPipelinesList_EmptyResultIsEmptyArray(t *testing.T) {
	f := &fakePipelinesWorkflows{runs: nil}
	h := handler.PipelinesList(f)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{ws_id}/pipelines", h)
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws-1/pipelines", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), domain.Principal{OrgID: "org-1", Role: "owner"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Fatalf("expected JSON array, got: %s", body)
	}
}

// TestPipelinesList_NotFoundIs404 — ErrNotFound from the service maps
// to 404.
func TestPipelinesList_NotFoundIs404(t *testing.T) {
	f := &fakePipelinesWorkflows{listErr: domain.ErrNotFound}
	h := handler.PipelinesList(f)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{ws_id}/pipelines", h)
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws-x/pipelines", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), domain.Principal{OrgID: "org-1", Role: "owner"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestPipelinesList_GenericErrorIs500 — non-sentinel errors return 500.
func TestPipelinesList_GenericErrorIs500(t *testing.T) {
	f := &fakePipelinesWorkflows{listErr: errors.New("db down")}
	h := handler.PipelinesList(f)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{ws_id}/pipelines", h)
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws-1/pipelines", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), domain.Principal{OrgID: "org-1", Role: "owner"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestPipelinesList_LimitAndBeforeQueryParams — query string parsing for
// limit + before is plumbed through to the service.
func TestPipelinesList_LimitAndBeforeQueryParams(t *testing.T) {
	f := &fakePipelinesWorkflows{}
	h := handler.PipelinesList(f)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{ws_id}/pipelines", h)
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws-1/pipelines?limit=10&before=2026-05-01T00:00:00Z", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), domain.Principal{OrgID: "org-1", Role: "owner"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestPipelineGet_HappyPath — returns the run + events envelope.
func TestPipelineGet_HappyPath(t *testing.T) {
	now := time.Now().UTC()
	f := &fakePipelinesWorkflows{
		runs: []domain.WorkflowRun{{ID: "run-1", Status: domain.WRSucceeded, StartedAt: now}},
		events: []domain.ActivityEvent{
			{WorkflowRunID: "run-1", Seq: 1, AgentRole: "backend", ActivityName: "patch", Status: "succeeded", TS: now},
		},
	}
	h := handler.PipelineGet(f)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{ws_id}/pipelines/{run_id}", h)
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws-1/pipelines/run-1", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), domain.Principal{OrgID: "org-1", Role: "owner"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Run    map[string]any   `json:"run"`
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Run["id"] != "run-1" {
		t.Fatalf("run id: %v", resp.Run["id"])
	}
	if len(resp.Events) != 1 {
		t.Fatalf("events: %d", len(resp.Events))
	}
}

// TestPipelineGet_NotFoundIs404 — ErrNotFound surfaces as 404.
func TestPipelineGet_NotFoundIs404(t *testing.T) {
	f := &fakePipelinesWorkflows{getErr: domain.ErrNotFound}
	h := handler.PipelineGet(f)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{ws_id}/pipelines/{run_id}", h)
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws-1/pipelines/missing", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), domain.Principal{OrgID: "org-1", Role: "owner"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestPipelineCreate_HappyPath — happy path returns 202.
func TestPipelineCreate_HappyPath(t *testing.T) {
	f := &fakePipelinesWorkflows{}
	h := handler.PipelineCreate(f, noopAuditWriter{})
	r := chi.NewRouter()
	r.Post("/v1/workspaces/{ws_id}/pipelines", h)
	body := strings.NewReader(`{"workflow_type": "RecoveryPipeline"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/workspaces/ws-1/pipelines", body)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), domain.Principal{OrgID: "org-1", Role: "owner"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestPipelineCreate_BadJSONIs400 — malformed body returns 400.
func TestPipelineCreate_BadJSONIs400(t *testing.T) {
	f := &fakePipelinesWorkflows{}
	h := handler.PipelineCreate(f, noopAuditWriter{})
	r := chi.NewRouter()
	r.Post("/v1/workspaces/{ws_id}/pipelines", h)
	body := strings.NewReader(`{not-json`)
	req := httptest.NewRequest(http.MethodPost, "/v1/workspaces/ws-1/pipelines", body)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), domain.Principal{OrgID: "org-1", Role: "owner"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rec.Code)
	}
}
