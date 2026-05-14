package handler_test

// Wire-level coverage for SeedSample. We mount the handler inside the real
// chi server (so the auth + RequireRole middleware fires), drive it with an
// in-memory auth provider, and use a fake WorkflowService so the test never
// touches Temporal or Postgres.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

// fakeSeedWorkflows records every Start call so tests can assert the call
// surface. Falls behind the WorkflowService port — only Start is wired; the
// other methods are zero stubs so this fake compiles against the wider port.
type fakeSeedWorkflows struct {
	mu          sync.Mutex
	startCalls  []seedStartCall
	startErr    error
	returnRunID string
}

type seedStartCall struct {
	WorkspaceID  string
	WorkflowType string
	Input        []byte
	PrincipalOrg string
}

func (f *fakeSeedWorkflows) Start(_ context.Context, p domain.Principal, wsID, wfType string, input []byte) (domain.WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return domain.WorkflowRun{}, f.startErr
	}
	f.startCalls = append(f.startCalls, seedStartCall{
		WorkspaceID:  wsID,
		WorkflowType: wfType,
		Input:        append([]byte(nil), input...),
		PrincipalOrg: p.OrgID,
	})
	rid := f.returnRunID
	if rid == "" {
		rid = "run-seed-1"
	}
	return domain.WorkflowRun{
		ID: rid, OrgID: p.OrgID, WorkspaceID: wsID,
		WorkflowType: wfType, Status: domain.WRQueued,
	}, nil
}

func (f *fakeSeedWorkflows) Get(context.Context, domain.Principal, string) (domain.WorkflowRun, []domain.ActivityEvent, error) {
	return domain.WorkflowRun{}, nil, nil
}

func (f *fakeSeedWorkflows) List(context.Context, domain.Principal, string, int, time.Time) ([]domain.WorkflowRun, error) {
	return nil, nil
}

func (f *fakeSeedWorkflows) Subscribe(context.Context, string) <-chan domain.ActivityEvent {
	ch := make(chan domain.ActivityEvent)
	close(ch)
	return ch
}

// signupSeed posts /v1/auth/signup and returns the session cookie + the
// signed-in user's email so tests can demote roles via the MemStore.
func signupSeed(t *testing.T, srv http.Handler, email, orgName string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "passw0rd!",
		"org_name": orgName + "-" + time.Now().UTC().Format("150405.000000"),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup: status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "nexis_session" {
			return c
		}
	}
	t.Fatalf("signup: no session cookie")
	return nil
}

// setupSeedSampleServer wires the handler into the real server with the
// supplied role for the signed-in user. Returns the cookie, server, and the
// fake workflow so tests can assert the call.
func setupSeedSampleServer(t *testing.T, role string, fakeWf *fakeSeedWorkflows) (*http.Cookie, http.Handler) {
	t.Helper()
	store := local.NewMemStore()
	provider := local.New(local.Config{
		Store:         store,
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        &local.TestMailer{},
		BaseURL:       "http://localhost:3000",
	})

	srv := httpserver.New(
		config.Config{AppEnv: "test", AppBaseURL: "http://localhost:3000"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpserver.Deps{
			Auth:      provider,
			Workflows: fakeWf,
		},
	)
	cookie := signupSeed(t, srv, "owner@example.com", "TestCo")

	if role != "owner" {
		ctx := context.Background()
		user, err := store.GetUserByEmail(ctx, "owner@example.com")
		if err != nil {
			t.Fatalf("lookup user: %v", err)
		}
		orgID, _, err := store.GetMembership(ctx, user.ID)
		if err != nil {
			t.Fatalf("lookup membership: %v", err)
		}
		if err := store.CreateMembership(ctx, orgID, user.ID, domain.Role(role)); err != nil {
			t.Fatalf("demote: %v", err)
		}
		// Re-login to refresh the cookie with the new role claim.
		body, _ := json.Marshal(map[string]string{
			"email": "owner@example.com", "password": "passw0rd!",
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("re-login: status=%d body=%s", rec.Code, rec.Body.String())
		}
		for _, c := range rec.Result().Cookies() {
			if c.Name == "nexis_session" {
				cookie = c
				break
			}
		}
	}
	return cookie, srv
}

// TestSeedSample_OwnerSucceedsReturnsRunID — happy path. Owner posts to
// /seed-sample, the fake workflow records the call, and the handler returns
// 202 with the new run_id.
func TestSeedSample_OwnerSucceedsReturnsRunID(t *testing.T) {
	fakeWf := &fakeSeedWorkflows{returnRunID: "run-seed-happy"}
	cookie, srv := setupSeedSampleServer(t, "owner", fakeWf)

	req := httptest.NewRequest(http.MethodPost, "/v1/workspaces/ws-seed/seed-sample", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s want=202", rec.Code, rec.Body.String())
	}
	var resp struct {
		RunID string `json:"run_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RunID != "run-seed-happy" {
		t.Fatalf("run_id: %q", resp.RunID)
	}

	fakeWf.mu.Lock()
	defer fakeWf.mu.Unlock()
	if len(fakeWf.startCalls) != 1 {
		t.Fatalf("start calls: %d want 1", len(fakeWf.startCalls))
	}
	call := fakeWf.startCalls[0]
	if call.WorkspaceID != "ws-seed" {
		t.Fatalf("workspace id: %q", call.WorkspaceID)
	}
	if call.WorkflowType != "RecoveryPipeline" {
		t.Fatalf("workflow type: %q", call.WorkflowType)
	}
	// Input must contain the seed-sample triggered_by + scenario marker.
	var input map[string]any
	if err := json.Unmarshal(call.Input, &input); err != nil {
		t.Fatalf("input decode: %v", err)
	}
	if input["triggered_by"] != "seed_sample" {
		t.Fatalf("triggered_by: %v", input["triggered_by"])
	}
	if input["scenario"] != "null-deref" {
		t.Fatalf("scenario: %v", input["scenario"])
	}
}

// TestSeedSample_MemberForbidden_403ForMember verifies the owner|admin
// guard. Members trying to seed get 403.
func TestSeedSample_MemberForbidden_403ForMember(t *testing.T) {
	fakeWf := &fakeSeedWorkflows{}
	cookie, srv := setupSeedSampleServer(t, "member", fakeWf)

	req := httptest.NewRequest(http.MethodPost, "/v1/workspaces/ws-seed/seed-sample", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member seed: status=%d body=%s want=403", rec.Code, rec.Body.String())
	}
	// Fake must NOT receive a Start call.
	fakeWf.mu.Lock()
	defer fakeWf.mu.Unlock()
	if len(fakeWf.startCalls) != 0 {
		t.Fatalf("member triggered workflow.Start: %d calls", len(fakeWf.startCalls))
	}
}

// TestSeedSample_WorkflowNotFoundMapsTo404 — when the workflow Start path
// returns ErrNotFound (the workspace doesn't exist), the handler maps to 404
// rather than the generic 500.
func TestSeedSample_WorkflowNotFoundMapsTo404(t *testing.T) {
	fakeWf := &fakeSeedWorkflows{startErr: domain.ErrNotFound}
	cookie, srv := setupSeedSampleServer(t, "owner", fakeWf)
	req := httptest.NewRequest(http.MethodPost, "/v1/workspaces/ws-missing/seed-sample", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status=%d body=%s want=404", rec.Code, rec.Body.String())
	}
}

// TestSeedSample_GenericErrorMapsTo500 — any other error from Start surfaces
// as 500 with a generic "start failed" message.
func TestSeedSample_GenericErrorMapsTo500(t *testing.T) {
	fakeWf := &fakeSeedWorkflows{startErr: errors.New("temporal: connection refused")}
	cookie, srv := setupSeedSampleServer(t, "owner", fakeWf)
	req := httptest.NewRequest(http.MethodPost, "/v1/workspaces/ws-x/seed-sample", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("generic err: status=%d body=%s want=500", rec.Code, rec.Body.String())
	}
}
