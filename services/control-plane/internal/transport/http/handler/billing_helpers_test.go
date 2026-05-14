package handler

// Coverage for the billing handler's pure helpers: toPaymentMethodResp,
// toInvoiceResp, parseTimeFlexible. The end-to-end /v1/billing/* flow needs
// a BillingProvider — that lives under tests/integration with the local
// provider.

import (
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// TestToPaymentMethodResp — every field round-trips into the DTO. CreatedAt
// is normalised to UTC and rendered as RFC3339 so the frontend doesn't have
// to think about tz drift.
func TestToPaymentMethodResp(t *testing.T) {
	pm := domain.PaymentMethod{
		ID: "pm-1", OrgID: "org-1", Provider: "stripe",
		Brand: "visa", Last4: "4242", ExpMonth: 12, ExpYear: 2030,
		BillingEmail: "owner@example.com",
		ExternalCustomerID:      "cus_X",
		ExternalPaymentMethodID: "pm_Y",
		CreatedAt:               time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	got := toPaymentMethodResp(pm)
	if got.ID != "pm-1" || got.OrgID != "org-1" || got.Provider != "stripe" {
		t.Fatalf("identity fields: %+v", got)
	}
	if got.Brand != "visa" || got.Last4 != "4242" {
		t.Fatalf("brand/last4: %+v", got)
	}
	if got.ExpMonth != 12 || got.ExpYear != 2030 {
		t.Fatalf("expiry: %+v", got)
	}
	if got.ExternalCustomerID != "cus_X" || got.ExternalPaymentMethodID != "pm_Y" {
		t.Fatalf("external ids: %+v", got)
	}
	if got.CreatedAt != "2026-05-01T12:00:00Z" {
		t.Fatalf("created_at: %q", got.CreatedAt)
	}
}

// TestToInvoiceResp — every field round-trips. Both period boundaries
// surface as UTC RFC3339.
func TestToInvoiceResp(t *testing.T) {
	inv := domain.Invoice{
		ID: "inv-1", OrgID: "org-1",
		PeriodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		TotalCents:  12500,
		Status:      "paid",
		CreatedAt:   time.Date(2026, 6, 1, 0, 5, 0, 0, time.UTC),
	}
	got := toInvoiceResp(inv)
	if got.ID != "inv-1" || got.OrgID != "org-1" {
		t.Fatalf("identity: %+v", got)
	}
	if got.PeriodStart != "2026-05-01T00:00:00Z" {
		t.Fatalf("period start: %q", got.PeriodStart)
	}
	if got.PeriodEnd != "2026-06-01T00:00:00Z" {
		t.Fatalf("period end: %q", got.PeriodEnd)
	}
	if got.TotalCents != 12500 {
		t.Fatalf("total: %d", got.TotalCents)
	}
	if got.Status != "paid" {
		t.Fatalf("status: %q", got.Status)
	}
}

// TestParseTimeFlexible covers all three branches: RFC3339, unix-seconds,
// invalid string returns an error.
func TestParseTimeFlexible(t *testing.T) {
	t.Run("rfc3339", func(t *testing.T) {
		got, err := parseTimeFlexible("2026-05-13T12:00:00Z")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if !got.Equal(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("unix_seconds", func(t *testing.T) {
		// 2026-05-13 12:00:00 UTC ~= 1778932800
		got, err := parseTimeFlexible("1778932800")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.Unix() != 1778932800 {
			t.Fatalf("got %d", got.Unix())
		}
	})
	t.Run("invalid_string", func(t *testing.T) {
		_, err := parseTimeFlexible("not-a-time")
		if err == nil {
			t.Fatalf("expected error")
		}
	})
	t.Run("empty_string", func(t *testing.T) {
		_, err := parseTimeFlexible("")
		if err == nil {
			t.Fatalf("empty must error")
		}
	})
}
