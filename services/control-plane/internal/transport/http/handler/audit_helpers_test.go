package handler_test

// Coverage for AuditList, AuditCSV, parseFilter (via wire), and AuditVerify.
// We use a fake audit.Lister so the handler logic is exercised end-to-end
// without standing up Postgres.

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

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/audit"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// fakeLister captures the filter so the test can assert parseFilter's
// translation of the query string was correct, and returns canned rows.
type fakeLister struct {
	mu       sync.Mutex
	lastF    audit.AuditFilter
	rows     []audit.AuditRow
	total    int
	failErr  error
}

func (f *fakeLister) List(_ context.Context, _ string, filt audit.AuditFilter) ([]audit.AuditRow, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastF = filt
	if f.failErr != nil {
		return nil, 0, f.failErr
	}
	return f.rows, f.total, nil
}

// withPrincipal returns a request decorated with a Principal in the ctx.
// AuditList and AuditCSV both pull the principal off the ctx via the
// middleware helper; using the public helper here mirrors what the chi
// stack does in production.
func withPrincipal(req *http.Request, orgID string) *http.Request {
	princ := domain.Principal{OrgID: orgID, UserID: "u-1", Role: "owner"}
	return req.WithContext(appmw.WithPrincipal(req.Context(), princ))
}

// TestAuditList_ReturnsRowsAndTotal — happy path. Filter the rows by query
// params, return {rows, total} envelope.
func TestAuditList_ReturnsRowsAndTotal(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeLister{
		rows: []audit.AuditRow{
			{ID: "1", OrgID: "org-1", Actor: "alice@x.com", Action: "user.created", Target: "u-2", CreatedAt: now},
			{ID: "2", OrgID: "org-1", Actor: "alice@x.com", Action: "session.created", Target: "s-1", CreatedAt: now.Add(time.Minute)},
		},
		total: 2,
	}

	h := handler.AuditList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit?actor=alice&action=user.created&limit=50&offset=0", nil)
	req = withPrincipal(req, "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Rows  []audit.AuditRow `json:"rows"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 || len(resp.Rows) != 2 {
		t.Fatalf("envelope: %+v", resp)
	}

	// Verify parseFilter pulled the query params correctly.
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastF.Actor != "alice" {
		t.Fatalf("actor: %q", fake.lastF.Actor)
	}
	if fake.lastF.Action != "user.created" {
		t.Fatalf("action: %q", fake.lastF.Action)
	}
	if fake.lastF.Limit != 50 {
		t.Fatalf("limit: %d", fake.lastF.Limit)
	}
}

// TestAuditList_ListerErrorMapsTo500 — when the lister errors, the handler
// returns 500 with the JSON error envelope.
func TestAuditList_ListerErrorMapsTo500(t *testing.T) {
	fake := &fakeLister{failErr: errors.New("db down")}
	h := handler.AuditList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	req = withPrincipal(req, "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestAuditList_ParseFilter_SinceUntil — RFC3339 timestamps in `since` and
// `until` propagate to the filter struct.
func TestAuditList_ParseFilter_SinceUntil(t *testing.T) {
	fake := &fakeLister{}
	h := handler.AuditList(fake)
	since := "2026-05-01T00:00:00Z"
	until := "2026-05-31T23:59:59Z"
	req := httptest.NewRequest(http.MethodGet, "/v1/audit?since="+since+"&until="+until, nil)
	req = withPrincipal(req, "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastF.Since == nil || fake.lastF.Until == nil {
		t.Fatalf("since/until: %+v %+v", fake.lastF.Since, fake.lastF.Until)
	}
}

// TestAuditList_ParseFilter_BadTimestampSilentlyDropped — a malformed `since`
// query string is silently dropped; the handler returns 200 with no `since`
// in the filter.
func TestAuditList_ParseFilter_BadTimestampSilentlyDropped(t *testing.T) {
	fake := &fakeLister{}
	h := handler.AuditList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit?since=not-a-date", nil)
	req = withPrincipal(req, "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastF.Since != nil {
		t.Fatalf("bad since must be dropped, got %v", fake.lastF.Since)
	}
}

// TestAuditCSV_HasHeaderRowAndCorrectContentType — happy path. The CSV
// response should carry text/csv content-type and a header row followed by
// one data row per source AuditRow.
func TestAuditCSV_HasHeaderRowAndCorrectContentType(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeLister{
		rows: []audit.AuditRow{
			{ID: "row-1", Actor: "alice", Action: "user.created", Target: "u-2", CreatedAt: now},
		},
	}
	h := handler.AuditCSV(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit.csv", nil)
	req = withPrincipal(req, "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("content-type: %q", ct)
	}
	if disp := rec.Header().Get("Content-Disposition"); !strings.Contains(disp, "audit.csv") {
		t.Fatalf("disposition: %q", disp)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "id,actor,action,target,created_at") {
		t.Fatalf("header row missing: %q", body)
	}
	if !strings.Contains(body, "row-1,alice,user.created,u-2") {
		t.Fatalf("data row missing: %q", body)
	}
}

// TestAuditCSV_ListerErrorReturns500 — the error path emits the JSON
// envelope (not partial CSV).
func TestAuditCSV_ListerErrorReturns500(t *testing.T) {
	fake := &fakeLister{failErr: errors.New("db lost")}
	h := handler.AuditCSV(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit.csv", nil)
	req = withPrincipal(req, "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("error response content-type: %q", ct)
	}
}

// TestAuditCSV_ClampsLimitTo10k — the CSV handler ignores the request's
// limit param and forces 10000. We verify by checking the fake's captured
// filter.
func TestAuditCSV_ClampsLimitTo10k(t *testing.T) {
	fake := &fakeLister{}
	h := handler.AuditCSV(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit.csv?limit=5", nil)
	req = withPrincipal(req, "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastF.Limit != 10000 {
		t.Fatalf("CSV must force limit=10000, got %d", fake.lastF.Limit)
	}
}

