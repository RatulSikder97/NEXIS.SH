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

	"github.com/nexis-eco/nexis/services/validator/internal/runner"
)

// stubRunner is a tiny in-memory HypothesisRunner for handler tests.
type stubRunner struct {
	resp runner.HypothesisResponse
	err  error
	last runner.HypothesisRequest
}

func (s *stubRunner) Run(_ context.Context, req runner.HypothesisRequest) (runner.HypothesisResponse, error) {
	s.last = req
	return s.resp, s.err
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestValidateProperty_RequiresBearer(t *testing.T) {
	h := ValidateProperty(&stubRunner{}, "tok", discardLogger())
	req := httptest.NewRequest(http.MethodPost, "/v1/validate/property", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestValidateProperty_AllPropertiesHold(t *testing.T) {
	r := &stubRunner{
		resp: runner.HypothesisResponse{
			TestsPassed: true,
			Failures:    []runner.HypothesisFailure{},
			DurationMs:  120,
		},
	}
	h := ValidateProperty(r, "tok", discardLogger())
	body := `{"repo_sha":"deadbeef","patch_diff":"","max_runtime_seconds":15}`
	req := httptest.NewRequest(http.MethodPost, "/v1/validate/property", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var got ValidatePropertyResp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.TestsPassed {
		t.Fatalf("expected TestsPassed=true")
	}
	if got.HypothesisFailures == nil {
		t.Fatalf("HypothesisFailures must be non-nil")
	}
	if r.last.MaxRuntimeSeconds != 15 {
		t.Fatalf("expected MaxRuntimeSeconds=15 forwarded, got %d", r.last.MaxRuntimeSeconds)
	}
}

func TestValidateProperty_FailureReturns422(t *testing.T) {
	r := &stubRunner{
		resp: runner.HypothesisResponse{
			TestsPassed: false,
			Failures: []runner.HypothesisFailure{
				{Test: "test_safe_div_zero_always_raises", Counterexample: "a=0", Shrunk: true},
			},
		},
	}
	h := ValidateProperty(r, "tok", discardLogger())
	req := httptest.NewRequest(http.MethodPost, "/v1/validate/property",
		strings.NewReader(`{"repo_sha":"x","patch_diff":""}`))
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", w.Code)
	}
	var got ValidatePropertyResp
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.TestsPassed {
		t.Fatalf("expected TestsPassed=false")
	}
	if len(got.HypothesisFailures) != 1 {
		t.Fatalf("expected one failure, got %d", len(got.HypothesisFailures))
	}
}

func TestValidateProperty_TransportError(t *testing.T) {
	r := &stubRunner{err: errors.New("dial fail")}
	h := ValidateProperty(r, "tok", discardLogger())
	req := httptest.NewRequest(http.MethodPost, "/v1/validate/property",
		strings.NewReader(`{"repo_sha":"x","patch_diff":""}`))
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	var got ValidatePropertyResp
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Error == "" {
		t.Fatalf("expected non-empty error field")
	}
	if got.HypothesisFailures == nil {
		t.Fatalf("HypothesisFailures must be non-nil even on transport error")
	}
}

func TestValidateProperty_BadBody(t *testing.T) {
	h := ValidateProperty(&stubRunner{}, "tok", discardLogger())
	req := httptest.NewRequest(http.MethodPost, "/v1/validate/property", strings.NewReader(`not-json`))
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
