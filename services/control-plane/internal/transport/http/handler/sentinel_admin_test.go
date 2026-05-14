package handler_test

// Wire-level coverage for SentinelTrigger (POST /v1/admin/sentinel/trigger).
// Owner-only. We mount the handler via the real router so the auth + role
// middleware fires, then drive it with a fake SentinelTriggerer.

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
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
)

// fakeSentinelTrigger captures every TriggerOne call so the test can assert
// the routing + bookkeeping side. Satisfies handler.SentinelTriggerer.
type fakeSentinelTrigger struct {
	mu       sync.Mutex
	calls    []sentinelCall
	failErr  error
	returnID string
}

type sentinelCall struct {
	OrgID      string
	IncidentID string
}

func (f *fakeSentinelTrigger) TriggerOne(_ context.Context, orgID, incidentID string) (domain.WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return domain.WorkflowRun{}, f.failErr
	}
	f.calls = append(f.calls, sentinelCall{OrgID: orgID, IncidentID: incidentID})
	rid := f.returnID
	if rid == "" {
		rid = "run-sentinel-1"
	}
	return domain.WorkflowRun{
		ID: rid, OrgID: orgID, WorkspaceID: "ws-default-" + orgID,
		Status: domain.WRQueued,
	}, nil
}

// setupSentinelAdminServer wires the handler with the supplied role for the
// signed-in user. Returns the cookie, server, the fake, and the resolved
// principal org id (read off the auth cookie's claim).
func setupSentinelAdminServer(t *testing.T, role string, fake *fakeSentinelTrigger) (*http.Cookie, http.Handler, string) {
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
			Auth:     provider,
			Sentinel: fake,
		},
	)
	// Signup creates an owner.
	body, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "passw0rd!",
		"org_name": "TestCo-" + time.Now().UTC().Format("150405.000000"),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "nexis_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("no session cookie after signup")
	}

	// Lookup principal org id via the store (the /v1/me JSON shape carries
	// it nested under `org.id`, but we already have a backdoor through the
	// MemStore for tests).
	ctx0 := context.Background()
	user0, err := store.GetUserByEmail(ctx0, "alice@example.com")
	if err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	orgID0, _, err := store.GetMembership(ctx0, user0.ID)
	if err != nil {
		t.Fatalf("lookup membership: %v", err)
	}
	me := struct{ OrgID string }{OrgID: orgID0}

	if role != "owner" {
		ctx := context.Background()
		user, err := store.GetUserByEmail(ctx, "alice@example.com")
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
			"email": "alice@example.com", "password": "passw0rd!",
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
			}
		}
	}

	return cookie, srv, me.OrgID
}

// TestSentinelTrigger_FiresIncidentDetected_AdminOnly — owner-only path.
// Owner POSTs an empty body (falls back to principal org), the fake
// triggerer records the call, the response carries the run_id.
func TestSentinelTrigger_FiresIncidentDetected_AdminOnly(t *testing.T) {
	fake := &fakeSentinelTrigger{returnID: "run-sentinel-happy"}
	cookie, srv, orgID := setupSentinelAdminServer(t, "owner", fake)

	body, _ := json.Marshal(map[string]string{
		"incident_id": "inc-42",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/sentinel/trigger", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("sentinel trigger: status=%d body=%s want=202", rec.Code, rec.Body.String())
	}

	var resp struct {
		RunID string `json:"run_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RunID != "run-sentinel-happy" {
		t.Fatalf("run_id: %q", resp.RunID)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) != 1 {
		t.Fatalf("trigger calls: %d want 1", len(fake.calls))
	}
	if fake.calls[0].OrgID != orgID {
		t.Fatalf("call org: got %q want %q", fake.calls[0].OrgID, orgID)
	}
	if fake.calls[0].IncidentID != "inc-42" {
		t.Fatalf("call incident_id: %q", fake.calls[0].IncidentID)
	}
}

// TestSentinelTrigger_MemberForbidden — role guard. Member → 403.
func TestSentinelTrigger_MemberForbidden(t *testing.T) {
	fake := &fakeSentinelTrigger{}
	cookie, srv, _ := setupSentinelAdminServer(t, "member", fake)

	body, _ := json.Marshal(map[string]string{"incident_id": "inc-1"})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/sentinel/trigger", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member: status=%d body=%s want=403", rec.Code, rec.Body.String())
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) != 0 {
		t.Fatalf("member triggered: %d calls", len(fake.calls))
	}
}

// TestSentinelTrigger_CrossOrgIs403 — even an owner of org A cannot target
// org B. The principal-org check is the cross-tenant fence.
func TestSentinelTrigger_CrossOrgIs403(t *testing.T) {
	fake := &fakeSentinelTrigger{}
	cookie, srv, _ := setupSentinelAdminServer(t, "owner", fake)

	body, _ := json.Marshal(map[string]string{
		"org_id":      "00000000-0000-0000-0000-000000000099",
		"incident_id": "inc-9",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/sentinel/trigger", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-org: status=%d body=%s want=403", rec.Code, rec.Body.String())
	}
}

// TestSentinelTrigger_NotFoundMapsTo404 — the fake returns ErrNotFound (no
// default workspace), the handler maps to 404.
func TestSentinelTrigger_NotFoundMapsTo404(t *testing.T) {
	fake := &fakeSentinelTrigger{failErr: domain.ErrNotFound}
	cookie, srv, _ := setupSentinelAdminServer(t, "owner", fake)

	body, _ := json.Marshal(map[string]string{"incident_id": "inc-x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/sentinel/trigger", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status=%d body=%s want=404", rec.Code, rec.Body.String())
	}
}

// TestSentinelTrigger_GenericErrorMapsTo500 — anything else → 500.
func TestSentinelTrigger_GenericErrorMapsTo500(t *testing.T) {
	fake := &fakeSentinelTrigger{failErr: errors.New("temporal: boom")}
	cookie, srv, _ := setupSentinelAdminServer(t, "owner", fake)

	body, _ := json.Marshal(map[string]string{"incident_id": "inc-x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/sentinel/trigger", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("generic: status=%d body=%s want=500", rec.Code, rec.Body.String())
	}
}

// Ensure the fake satisfies the published interface — compile-time check.
var _ handler.SentinelTriggerer = (*fakeSentinelTrigger)(nil)
