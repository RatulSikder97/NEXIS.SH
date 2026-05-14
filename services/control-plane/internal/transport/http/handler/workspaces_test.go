package handler_test

// Wire-level coverage for the workspace handlers using a fake
// WorkspaceService so the tests stand alone (no Postgres, no SSE broker).

import (
	"bytes"
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

// fakeWS captures every call and lets tests inject errors per method.
type fakeWS struct {
	mu          sync.Mutex
	ws          domain.Workspace
	list        []domain.Workspace
	getErr      error
	createErr   error
	suspendErr  error
	listErr     error
	suspended   bool
	createCalls int
}

func (f *fakeWS) Create(_ context.Context, p domain.Principal, name, region string) (domain.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	if f.createErr != nil {
		return domain.Workspace{}, f.createErr
	}
	return domain.Workspace{
		ID: "ws-fake", OrgID: p.OrgID, Name: name, Slug: "slug",
		Region: region, Status: domain.WSProvisioning,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}, nil
}

func (f *fakeWS) Get(_ context.Context, _ domain.Principal, _ string) (domain.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return domain.Workspace{}, f.getErr
	}
	if f.ws.ID == "" {
		f.ws = domain.Workspace{
			ID: "ws-1", OrgID: "org-1", Name: "Test",
			Region: "us-east-1", Status: domain.WSReady,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
	}
	return f.ws, nil
}

func (f *fakeWS) List(_ context.Context, _ domain.Principal) ([]domain.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.list, f.listErr
}

func (f *fakeWS) Suspend(_ context.Context, _ domain.Principal, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suspended = true
	return f.suspendErr
}

func (f *fakeWS) Events(_ context.Context, _ string) <-chan domain.ProvisioningStep {
	ch := make(chan domain.ProvisioningStep)
	close(ch)
	return ch
}

// withWSPrincipal returns a request decorated with a Principal in the ctx
// for tests that bypass the middleware stack.
func withWSPrincipal(req *http.Request, orgID string) *http.Request {
	princ := domain.Principal{OrgID: orgID, UserID: "u-1", Role: "owner"}
	return req.WithContext(appmw.WithPrincipal(req.Context(), princ))
}

// TestWorkspaceRegions_ReturnsCatalog — the regions endpoint emits the
// static catalog with at least one entry.
func TestWorkspaceRegions_ReturnsCatalog(t *testing.T) {
	h := handler.WorkspaceRegions()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/workspaces/regions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var regions []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&regions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(regions) == 0 {
		t.Fatalf("expected at least one region")
	}
}

// TestWorkspacesList_HappyPathReturnsArray — happy path returns the
// service's list wrapped in the DTO shape.
func TestWorkspacesList_HappyPathReturnsArray(t *testing.T) {
	f := &fakeWS{
		list: []domain.Workspace{
			{ID: "ws-1", OrgID: "org-1", Name: "A", Slug: "a", Region: "us-east-1", Status: domain.WSReady, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
		},
	}
	h := handler.WorkspacesList(f)
	req := withWSPrincipal(httptest.NewRequest(http.MethodGet, "/v1/workspaces", nil), "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ws-1") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestWorkspacesList_ErrorIs500 — the underlying List failure surfaces
// as 500.
func TestWorkspacesList_ErrorIs500(t *testing.T) {
	f := &fakeWS{listErr: errors.New("db down")}
	h := handler.WorkspacesList(f)
	req := withWSPrincipal(httptest.NewRequest(http.MethodGet, "/v1/workspaces", nil), "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestWorkspaceCreate_HappyPathReturns202 — happy create returns Accepted
// with the workspace echoed.
func TestWorkspaceCreate_HappyPathReturns202(t *testing.T) {
	f := &fakeWS{}
	h := handler.WorkspaceCreate(f, noopAuditWriter{})
	body, _ := json.Marshal(map[string]string{
		"name": "Test Workspace", "region": "us-east-1",
	})
	req := withWSPrincipal(httptest.NewRequest(http.MethodPost, "/v1/workspaces", bytes.NewReader(body)), "org-1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createCalls != 1 {
		t.Fatalf("create calls: %d", f.createCalls)
	}
}

// TestWorkspaceCreate_MissingNameIs400 — empty body field returns 400.
func TestWorkspaceCreate_MissingNameIs400(t *testing.T) {
	f := &fakeWS{}
	h := handler.WorkspaceCreate(f, noopAuditWriter{})
	body, _ := json.Marshal(map[string]string{"region": "us-east-1"})
	req := withWSPrincipal(httptest.NewRequest(http.MethodPost, "/v1/workspaces", bytes.NewReader(body)), "org-1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "name required") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestWorkspaceCreate_MissingRegionIs400 — empty region returns 400.
func TestWorkspaceCreate_MissingRegionIs400(t *testing.T) {
	f := &fakeWS{}
	h := handler.WorkspaceCreate(f, noopAuditWriter{})
	body, _ := json.Marshal(map[string]string{"name": "x"})
	req := withWSPrincipal(httptest.NewRequest(http.MethodPost, "/v1/workspaces", bytes.NewReader(body)), "org-1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestWorkspaceCreate_ServiceErrorIs400 — service errors surface as 400
// with the error message (e.g. unsupported region).
func TestWorkspaceCreate_ServiceErrorIs400(t *testing.T) {
	f := &fakeWS{createErr: errors.New("region not allowed")}
	h := handler.WorkspaceCreate(f, noopAuditWriter{})
	body, _ := json.Marshal(map[string]string{"name": "x", "region": "mars"})
	req := withWSPrincipal(httptest.NewRequest(http.MethodPost, "/v1/workspaces", bytes.NewReader(body)), "org-1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestWorkspaceGet_HappyPath — returns 200 with the workspace shape.
func TestWorkspaceGet_HappyPath(t *testing.T) {
	f := &fakeWS{}
	h := handler.WorkspaceGet(f)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{id}", h)
	req := withWSPrincipal(httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws-1", nil), "org-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestWorkspaceGet_NotFoundIs404 — ErrNotFound maps to 404.
func TestWorkspaceGet_NotFoundIs404(t *testing.T) {
	f := &fakeWS{getErr: domain.ErrNotFound}
	h := handler.WorkspaceGet(f)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{id}", h)
	req := withWSPrincipal(httptest.NewRequest(http.MethodGet, "/v1/workspaces/missing", nil), "org-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestWorkspaceGet_GenericErrorIs500 — non-sentinel errors surface as 500.
func TestWorkspaceGet_GenericErrorIs500(t *testing.T) {
	f := &fakeWS{getErr: errors.New("db lost")}
	h := handler.WorkspaceGet(f)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{id}", h)
	req := withWSPrincipal(httptest.NewRequest(http.MethodGet, "/v1/workspaces/x", nil), "org-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestWorkspaceSuspend_HappyPath — 204 No Content + suspended flag set.
func TestWorkspaceSuspend_HappyPath(t *testing.T) {
	f := &fakeWS{}
	h := handler.WorkspaceSuspend(f, noopAuditWriter{})
	r := chi.NewRouter()
	r.Delete("/v1/workspaces/{id}", h)
	req := withWSPrincipal(httptest.NewRequest(http.MethodDelete, "/v1/workspaces/ws-1", nil), "org-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.suspended {
		t.Fatalf("suspend was not called")
	}
}

// TestWorkspaceSuspend_GetNotFoundIs404 — the pre-check Get failure path
// (ErrNotFound) maps to 404 without ever invoking Suspend.
func TestWorkspaceSuspend_GetNotFoundIs404(t *testing.T) {
	f := &fakeWS{getErr: domain.ErrNotFound}
	h := handler.WorkspaceSuspend(f, noopAuditWriter{})
	r := chi.NewRouter()
	r.Delete("/v1/workspaces/{id}", h)
	req := withWSPrincipal(httptest.NewRequest(http.MethodDelete, "/v1/workspaces/x", nil), "org-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestWorkspaceEvents_TerminalSendsSingleFrame — a workspace already in the
// "ready" state should emit one SSE frame and close the connection.
func TestWorkspaceEvents_TerminalSendsSingleFrame(t *testing.T) {
	f := &fakeWS{
		ws: domain.Workspace{
			ID: "ws-1", OrgID: "org-1", Status: domain.WSReady,
			ProvisioningStep: "complete",
		},
	}
	h := handler.WorkspaceEvents(f)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{id}/events", h)
	req := withWSPrincipal(httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws-1/events", nil), "org-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "data: ") {
		t.Fatalf("expected SSE data frame, got: %q", body)
	}
	if !strings.Contains(body, "ready") {
		t.Fatalf("expected 'ready' in terminal frame: %s", body)
	}
}
