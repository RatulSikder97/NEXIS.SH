package handler_test

// Coverage for the ValidatorRuns handler's nil-pool path. The DB query
// path requires Postgres and is exercised under tests/integration; here
// we cover the early-return + unauthorized branch.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// TestValidatorRuns_NilPoolReturnsEmpty — dev/no-DB boot path returns an
// empty list + the stub note rather than 500.
func TestValidatorRuns_NilPoolReturnsEmpty(t *testing.T) {
	h := handler.ValidatorRuns(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/ops/validator-runs", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), domain.Principal{OrgID: "org-1", Role: "owner"}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Runs []any  `json:"runs"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Runs) != 0 {
		t.Fatalf("expected 0 runs, got %d", len(resp.Runs))
	}
	if resp.Note == "" {
		t.Fatalf("expected stub note")
	}
}

// TestValidatorRuns_NoPrincipalIs401 — calling without a principal in ctx
// returns 401.
func TestValidatorRuns_NoPrincipalIs401(t *testing.T) {
	h := handler.ValidatorRuns(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/ops/validator-runs", nil)
	// No principal in ctx.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: %d", rec.Code)
	}
}
