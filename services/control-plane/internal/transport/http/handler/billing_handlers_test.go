package handler_test

// Wire-level coverage for the BillingProvider-bound handlers: Get/Add/Detach
// payment method, CreateSetupIntent, ConfirmSetupIntent. The BillingProvider
// port is implemented by a fake here so the adapter doesn't have to be
// stood up.

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

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// fakeBilling captures every call so tests can drive the failure surfaces
// independently. The fake also records audit writes via a separate
// AuditWriter passed to the handler — kept on the test side.
type fakeBilling struct {
	mu         sync.Mutex
	pm         domain.PaymentMethod
	getErr     error
	attachErr  error
	detachErr  error
	intentErr  error
	confirmErr error
	calls      int
}

func (f *fakeBilling) Name() string { return "local" }

func (f *fakeBilling) AttachCard(_ context.Context, p domain.Principal, _ domain.AddCardInput) (domain.PaymentMethod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.attachErr != nil {
		return domain.PaymentMethod{}, f.attachErr
	}
	return domain.PaymentMethod{
		ID: "pm-1", OrgID: p.OrgID, Provider: "local",
		Brand: "visa", Last4: "4242", CreatedAt: time.Now().UTC(),
	}, nil
}

func (f *fakeBilling) GetPaymentMethod(_ context.Context, p domain.Principal) (domain.PaymentMethod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return domain.PaymentMethod{}, f.getErr
	}
	pm := f.pm
	if pm.ID == "" {
		pm = domain.PaymentMethod{
			ID: "pm-get", OrgID: p.OrgID, Provider: "local",
			Brand: "mastercard", Last4: "1111", CreatedAt: time.Now().UTC(),
		}
	}
	return pm, nil
}

func (f *fakeBilling) DetachCard(_ context.Context, _ domain.Principal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.detachErr
}

func (f *fakeBilling) CreateSetupIntent(_ context.Context, _ domain.Principal) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.intentErr != nil {
		return "", f.intentErr
	}
	return "seti_local_abc", nil
}

func (f *fakeBilling) AttachPaymentMethod(_ context.Context, p domain.Principal, _, _ string) (domain.PaymentMethod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.confirmErr != nil {
		return domain.PaymentMethod{}, f.confirmErr
	}
	return domain.PaymentMethod{
		ID: "pm-confirm", OrgID: p.OrgID, Provider: "stripe",
		Brand: "amex", Last4: "0007", CreatedAt: time.Now().UTC(),
	}, nil
}

// noopAudit implements domain.AuditWriter for handler-side tests that don't
// care about the audit row.
type noopAuditWriter struct{}

func (noopAuditWriter) Write(context.Context, domain.Principal, string, string, map[string]any) error {
	return nil
}

func withPrincipalReq(method, path, body string, orgID string) *http.Request {
	var br *bytes.Reader
	if body != "" {
		br = bytes.NewReader([]byte(body))
	} else {
		br = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, br)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	princ := domain.Principal{OrgID: orgID, UserID: "u-1", Role: "owner"}
	return req.WithContext(appmw.WithPrincipal(req.Context(), princ))
}

// TestBillingGetPaymentMethod_HappyPath — returns the PM with 200.
func TestBillingGetPaymentMethod_HappyPath(t *testing.T) {
	prov := &fakeBilling{}
	h := handler.BillingGetPaymentMethod(prov)
	req := withPrincipalReq(http.MethodGet, "/v1/billing/payment-method", "", "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "pm-get") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestBillingGetPaymentMethod_NotFound — provider returns ErrNotFound.
// Handler maps to 404 with "no payment method".
func TestBillingGetPaymentMethod_NotFound(t *testing.T) {
	prov := &fakeBilling{getErr: domain.ErrNotFound}
	h := handler.BillingGetPaymentMethod(prov)
	req := withPrincipalReq(http.MethodGet, "/v1/billing/payment-method", "", "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no payment method") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestBillingGetPaymentMethod_NotImplemented — provider that doesn't
// support GetPaymentMethod returns 501.
func TestBillingGetPaymentMethod_NotImplemented(t *testing.T) {
	prov := &fakeBilling{getErr: domain.ErrNotImplemented}
	h := handler.BillingGetPaymentMethod(prov)
	req := withPrincipalReq(http.MethodGet, "/v1/billing/payment-method", "", "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestBillingAddPaymentMethod_HappyPath posts a valid AddCard request and
// asserts 201 + the persisted PM is echoed.
func TestBillingAddPaymentMethod_HappyPath(t *testing.T) {
	prov := &fakeBilling{}
	h := handler.BillingAddPaymentMethod(prov, noopAuditWriter{})
	body, _ := json.Marshal(map[string]any{
		"card_number":   "4242424242424242",
		"exp_month":     12,
		"exp_year":      2030,
		"cvc":           "123",
		"postal_code":   "12345",
		"billing_email": "owner@example.com",
	})
	req := withPrincipalReq(http.MethodPost, "/v1/billing/payment-method", string(body), "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "pm-1") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestBillingAddPaymentMethod_NotImplemented — stripe provider rejects raw
// card flow with ErrNotImplemented → 501.
func TestBillingAddPaymentMethod_NotImplemented(t *testing.T) {
	prov := &fakeBilling{attachErr: domain.ErrNotImplemented}
	h := handler.BillingAddPaymentMethod(prov, noopAuditWriter{})
	body, _ := json.Marshal(map[string]any{"card_number": "x", "exp_month": 1, "exp_year": 2030, "cvc": "1"})
	req := withPrincipalReq(http.MethodPost, "/v1/billing/payment-method", string(body), "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestBillingAddPaymentMethod_GenericErrorIs400 — adapter validation
// failures map to 400 with the error string surfaced.
func TestBillingAddPaymentMethod_GenericErrorIs400(t *testing.T) {
	prov := &fakeBilling{attachErr: errors.New("invalid card number")}
	h := handler.BillingAddPaymentMethod(prov, noopAuditWriter{})
	body, _ := json.Marshal(map[string]any{"card_number": "x", "exp_month": 1, "exp_year": 2030, "cvc": "1"})
	req := withPrincipalReq(http.MethodPost, "/v1/billing/payment-method", string(body), "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid card number") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestBillingDeletePaymentMethod_HappyPath — happy path is 204 No Content.
func TestBillingDeletePaymentMethod_HappyPath(t *testing.T) {
	prov := &fakeBilling{}
	h := handler.BillingDeletePaymentMethod(prov, noopAuditWriter{})
	req := withPrincipalReq(http.MethodDelete, "/v1/billing/payment-method", "", "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestBillingDeletePaymentMethod_NotImplemented — provider doesn't support
// → 501.
func TestBillingDeletePaymentMethod_NotImplemented(t *testing.T) {
	prov := &fakeBilling{detachErr: domain.ErrNotImplemented}
	h := handler.BillingDeletePaymentMethod(prov, noopAuditWriter{})
	req := withPrincipalReq(http.MethodDelete, "/v1/billing/payment-method", "", "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestBillingCreateSetupIntent_HappyPath returns 201 with the client_secret.
func TestBillingCreateSetupIntent_HappyPath(t *testing.T) {
	prov := &fakeBilling{}
	h := handler.BillingCreateSetupIntent(prov, noopAuditWriter{}, config.Config{AppEnv: "test"})
	req := withPrincipalReq(http.MethodPost, "/v1/billing/payment-method/intent", "", "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "seti_local_abc") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestBillingCreateSetupIntent_NotImplemented — provider returns 501.
func TestBillingCreateSetupIntent_NotImplemented(t *testing.T) {
	prov := &fakeBilling{intentErr: domain.ErrNotImplemented}
	h := handler.BillingCreateSetupIntent(prov, noopAuditWriter{}, config.Config{})
	req := withPrincipalReq(http.MethodPost, "/v1/billing/payment-method/intent", "", "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestBillingConfirmSetupIntent_HappyPath — post a PM id + email, get 201
// with the persisted method shape.
func TestBillingConfirmSetupIntent_HappyPath(t *testing.T) {
	prov := &fakeBilling{}
	h := handler.BillingConfirmSetupIntent(prov, noopAuditWriter{}, config.Config{})
	body, _ := json.Marshal(map[string]string{
		"payment_method": "pm_test_xyz",
		"billing_email":  "owner@example.com",
	})
	req := withPrincipalReq(http.MethodPost, "/v1/billing/payment-method/confirm", string(body), "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "pm-confirm") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestBillingConfirmSetupIntent_MissingPMIs400 — missing payment_method
// field is a client error.
func TestBillingConfirmSetupIntent_MissingPMIs400(t *testing.T) {
	prov := &fakeBilling{}
	h := handler.BillingConfirmSetupIntent(prov, noopAuditWriter{}, config.Config{})
	body, _ := json.Marshal(map[string]string{
		"billing_email": "owner@example.com",
	})
	req := withPrincipalReq(http.MethodPost, "/v1/billing/payment-method/confirm", string(body), "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "payment_method required") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestBillingConfirmSetupIntent_DeclinedCardIs400 — adapter validation
// errors map to 400 (Stripe's "card declined" surfaces user-actionable
// messages).
func TestBillingConfirmSetupIntent_DeclinedCardIs400(t *testing.T) {
	prov := &fakeBilling{confirmErr: errors.New("card declined")}
	h := handler.BillingConfirmSetupIntent(prov, noopAuditWriter{}, config.Config{})
	body, _ := json.Marshal(map[string]string{
		"payment_method": "pm_test_decline",
	})
	req := withPrincipalReq(http.MethodPost, "/v1/billing/payment-method/confirm", string(body), "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rec.Code)
	}
}
