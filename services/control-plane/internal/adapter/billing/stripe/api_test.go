package stripe

// HTTP-level coverage for the Stripe provider using a httptest.Server that
// mocks the Stripe API endpoints. We point stripe.SetBackend at the local
// server so paymentmethod.* / customer.* / setupintent.* calls land on our
// handler without hitting the real Stripe.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	stripe "github.com/stripe/stripe-go/v82"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// stripeMux is a tiny http.Handler that captures requests and returns canned
// Stripe-shaped JSON. Production code only goes through three endpoints —
// customers, setup_intents, payment_methods — so we keep the mux small.
type stripeMux struct {
	t          *testing.T
	mu         sync.Mutex
	customers  int  // count of POSTs to /v1/customers (mints + updates)
	setups     int  // count of POSTs to /v1/setup_intents
	attaches   int  // /v1/payment_methods/.../attach
	detaches   int  // /v1/payment_methods/.../detach
	custUpdate int  // PATCH-style update to /v1/customers/cus_X (sets default PM)
	failCust   bool // when true, customer.New returns 4xx
	failAttach bool
}

func (m *stripeMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// All Stripe responses are JSON.
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.URL.Path == "/v1/customers" && r.Method == http.MethodPost:
		m.customers++
		if m.failCust {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"message": "bad customer", "type": "invalid_request_error"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "cus_test_123",
			"object": "customer",
		})
	case strings.HasPrefix(r.URL.Path, "/v1/customers/") && r.Method == http.MethodPost:
		m.custUpdate++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "cus_test_123",
			"object": "customer",
		})
	case r.URL.Path == "/v1/setup_intents" && r.Method == http.MethodPost:
		m.setups++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":            "seti_test_abc",
			"object":        "setup_intent",
			"client_secret": "seti_test_abc_secret_xyz",
			"status":        "requires_payment_method",
		})
	case strings.HasSuffix(r.URL.Path, "/attach") && r.Method == http.MethodPost:
		m.attaches++
		if m.failAttach {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"message": "bad pm", "type": "invalid_request_error"},
			})
			return
		}
		pmID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/payment_methods/"), "/attach")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     pmID,
			"object": "payment_method",
			"type":   "card",
			"card": map[string]any{
				"brand":     "visa",
				"last4":     "4242",
				"exp_month": 12,
				"exp_year":  2032,
			},
		})
	case strings.HasSuffix(r.URL.Path, "/detach") && r.Method == http.MethodPost:
		m.detaches++
		pmID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/payment_methods/"), "/detach")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     pmID,
			"object": "payment_method",
		})
	default:
		m.t.Logf("stripe mux: unhandled %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "unhandled route", "type": "invalid_request_error"},
		})
	}
}

// pointAtMux redirects the Stripe SDK's API backend at the test server. The
// backend is cached on a package-global; restoring it via t.Cleanup keeps
// other tests in this package free from leftover state.
func pointAtMux(t *testing.T, server *httptest.Server) {
	t.Helper()
	original := stripe.GetBackend(stripe.APIBackend)
	url := server.URL
	override := stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
		URL: &url,
	})
	stripe.SetBackend(stripe.APIBackend, override)
	t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, original) })
}

// TestCreateSetupIntent_HappyPath mints a customer + a setup intent and
// returns the client_secret. Verifies the stub PaymentMethod row is upserted
// so the next SetupIntent reuses the customer.
func TestCreateSetupIntent_HappyPath(t *testing.T) {
	mux := &stripeMux{t: t}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	pointAtMux(t, srv)

	repo := &fakePersistence{}
	p, _ := New(Config{SecretKey: "sk_test_dummy"}, repo)

	secret, err := p.CreateSetupIntent(context.Background(), domain.Principal{OrgID: "org-1"})
	if err != nil {
		t.Fatalf("CreateSetupIntent: %v", err)
	}
	if secret != "seti_test_abc_secret_xyz" {
		t.Fatalf("client_secret = %q, want seti_test_abc_secret_xyz", secret)
	}
	if mux.customers != 1 {
		t.Fatalf("customer.New called %d times, want 1", mux.customers)
	}
	if mux.setups != 1 {
		t.Fatalf("setupintent.New called %d times, want 1", mux.setups)
	}
	if repo.pm == nil || repo.pm.ExternalCustomerID != "cus_test_123" {
		t.Fatalf("stub PM not upserted: %+v", repo.pm)
	}
}

// TestCreateSetupIntent_ReusesExistingCustomer — when the repo already has a
// stripe customer id stashed, ensureCustomer must skip customer.New.
func TestCreateSetupIntent_ReusesExistingCustomer(t *testing.T) {
	mux := &stripeMux{t: t}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	pointAtMux(t, srv)

	repo := &fakePersistence{pm: &domain.PaymentMethod{
		OrgID:              "org-1",
		Provider:           "stripe",
		ExternalCustomerID: "cus_existing_999",
	}}
	p, _ := New(Config{SecretKey: "sk_test_dummy"}, repo)

	if _, err := p.CreateSetupIntent(context.Background(), domain.Principal{OrgID: "org-1"}); err != nil {
		t.Fatalf("CreateSetupIntent: %v", err)
	}
	if mux.customers != 0 {
		t.Fatalf("customer.New called %d times for existing customer, want 0", mux.customers)
	}
	if mux.setups != 1 {
		t.Fatalf("setupintent.New called %d times, want 1", mux.setups)
	}
}

// TestCreateSetupIntent_CustomerError surfaces as the wrapped error.
func TestCreateSetupIntent_CustomerError(t *testing.T) {
	mux := &stripeMux{t: t, failCust: true}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	pointAtMux(t, srv)

	repo := &fakePersistence{}
	p, _ := New(Config{SecretKey: "sk_test_dummy"}, repo)

	if _, err := p.CreateSetupIntent(context.Background(), domain.Principal{OrgID: "org-1"}); err == nil {
		t.Fatalf("expected error")
	}
}

// TestAttachPaymentMethod_HappyPath drives the full Attach→Update→Persist
// path including card brand/last4 extraction.
func TestAttachPaymentMethod_HappyPath(t *testing.T) {
	mux := &stripeMux{t: t}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	pointAtMux(t, srv)

	repo := &fakePersistence{}
	p, _ := New(Config{SecretKey: "sk_test_dummy"}, repo)

	got, err := p.AttachPaymentMethod(context.Background(),
		domain.Principal{OrgID: "org-1"},
		"pm_test_xyz", "owner@example.com",
	)
	if err != nil {
		t.Fatalf("AttachPaymentMethod: %v", err)
	}
	if got.Brand != "visa" || got.Last4 != "4242" {
		t.Fatalf("card details lost: brand=%q last4=%q", got.Brand, got.Last4)
	}
	if got.ExternalPaymentMethodID != "pm_test_xyz" {
		t.Fatalf("pm id: %q", got.ExternalPaymentMethodID)
	}
	if got.BillingEmail != "owner@example.com" {
		t.Fatalf("billing email: %q", got.BillingEmail)
	}
	if mux.attaches != 1 {
		t.Fatalf("attach calls = %d, want 1", mux.attaches)
	}
	if mux.custUpdate != 1 {
		t.Fatalf("customer.Update calls = %d, want 1 (default PM setup)", mux.custUpdate)
	}
}

// TestAttachPaymentMethod_EmptyPMRejected.
func TestAttachPaymentMethod_EmptyPMRejected(t *testing.T) {
	mux := &stripeMux{t: t}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	pointAtMux(t, srv)

	repo := &fakePersistence{}
	p, _ := New(Config{SecretKey: "sk_test_dummy"}, repo)
	_, err := p.AttachPaymentMethod(context.Background(),
		domain.Principal{OrgID: "org-1"}, "", "owner@example.com")
	if err == nil {
		t.Fatalf("expected error for empty pmID")
	}
}

// TestAttachPaymentMethod_AttachError — Stripe-side attach error propagates.
func TestAttachPaymentMethod_AttachError(t *testing.T) {
	mux := &stripeMux{t: t, failAttach: true}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	pointAtMux(t, srv)

	repo := &fakePersistence{}
	p, _ := New(Config{SecretKey: "sk_test_dummy"}, repo)
	_, err := p.AttachPaymentMethod(context.Background(),
		domain.Principal{OrgID: "org-1"}, "pm_bad", "")
	if err == nil {
		t.Fatalf("expected error")
	}
}

// TestDetachCard_HappyPath — detaches at Stripe + deletes the local row.
func TestDetachCard_HappyPath(t *testing.T) {
	mux := &stripeMux{t: t}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	pointAtMux(t, srv)

	repo := &fakePersistence{pm: &domain.PaymentMethod{
		OrgID: "org-1", Provider: "stripe",
		ExternalCustomerID: "cus_test_123", ExternalPaymentMethodID: "pm_xyz",
	}}
	p, _ := New(Config{SecretKey: "sk_test_dummy"}, repo)
	if err := p.DetachCard(context.Background(), domain.Principal{OrgID: "org-1"}); err != nil {
		t.Fatalf("DetachCard: %v", err)
	}
	if mux.detaches != 1 {
		t.Fatalf("detach calls = %d want 1", mux.detaches)
	}
	if repo.deletes != 1 {
		t.Fatalf("delete calls = %d want 1", repo.deletes)
	}
}

// TestDetachCard_PMRowMissingStillDeletes — when no PM row exists, no Stripe
// detach call is made but the persistence delete still runs.
func TestDetachCard_PMRowMissingStillDeletes(t *testing.T) {
	mux := &stripeMux{t: t}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	pointAtMux(t, srv)

	repo := &fakePersistence{pm: nil}
	p, _ := New(Config{SecretKey: "sk_test_dummy"}, repo)
	if err := p.DetachCard(context.Background(), domain.Principal{OrgID: "org-1"}); err != nil {
		t.Fatalf("DetachCard: %v", err)
	}
	if mux.detaches != 0 {
		t.Fatalf("detach called %d times for missing PM, want 0", mux.detaches)
	}
	if repo.deletes != 1 {
		t.Fatalf("delete calls = %d", repo.deletes)
	}
}

// TestEnsureCustomer_RepoErrorOnReadProceedsToMint — the read-side error
// (e.g. ErrNotFound from the repo) is treated as "no existing customer"
// and ensureCustomer goes ahead with customer.New.
func TestEnsureCustomer_RepoErrorOnReadProceedsToMint(t *testing.T) {
	mux := &stripeMux{t: t}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	pointAtMux(t, srv)

	repo := &fakePersistence{getErr: errors.New("not found")}
	p, _ := New(Config{SecretKey: "sk_test_dummy"}, repo)

	if _, err := p.CreateSetupIntent(context.Background(), domain.Principal{OrgID: "org-1"}); err != nil {
		t.Fatalf("CreateSetupIntent: %v", err)
	}
	if mux.customers != 1 {
		t.Fatalf("customer mint called %d times, want 1", mux.customers)
	}
}
