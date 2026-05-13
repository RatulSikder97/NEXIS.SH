package domain

import (
	"context"
	"time"
)

// PaymentMethod is the tenant-visible projection of payment_methods. The raw
// PAN is NEVER persisted — only brand + last4 + expiry + an external id
// (synthetic for the local provider, real Stripe pm_... for the stripe
// provider in Phase 7).
type PaymentMethod struct {
	ID                      string
	OrgID                   string
	Provider                string // "local" | "stripe"
	ExternalCustomerID      string
	ExternalPaymentMethodID string
	Brand                   string
	Last4                   string
	ExpMonth                int
	ExpYear                 int
	BillingEmail            string
	CreatedAt               time.Time
}

// AddCardInput is the unencrypted shape the LocalBillingProvider accepts. It
// is intentionally kept out of any persistence interface — CardNumber and CVC
// must die at the BillingProvider boundary. The repo layer never sees these
// fields.
type AddCardInput struct {
	CardNumber   string
	ExpMonth     int
	ExpYear      int
	CVC          string
	PostalCode   string
	BillingEmail string
}

// Invoice is the monthly roll-up record. Phase 3.5 marks invoices paid
// automatically since there is no real charge; Phase 7 swaps that for a
// Stripe-backed flow.
type Invoice struct {
	ID          string
	OrgID       string
	PeriodStart time.Time
	PeriodEnd   time.Time
	TotalCents  int64
	Status      string // "open" | "paid" | "void"
	CreatedAt   time.Time
}

// UsageRecord is one row in usage_records — a single tick of metered runtime
// for one workspace. The cron creates one of these per ready workspace per
// USAGE_TICK_SECONDS.
type UsageRecord struct {
	ID             string
	OrgID          string
	WorkspaceID    string
	Project        string
	Kind           string
	Quantity       float64
	UnitPriceCents float64
	AmountCents    float64
	RecordedAt     time.Time
}

// UsageBreakdown is the response shape for GET /v1/billing/usage. ByWorkspace
// + ByKind are independent groupings of the same time window.
type UsageBreakdown struct {
	TotalCents  int64        `json:"total_cents"`
	ByWorkspace []UsageGroup `json:"by_workspace"`
	ByKind      []UsageGroup `json:"by_kind"`
}

// UsageGroup is one bucket inside a UsageBreakdown. Key is the workspace id
// or the kind name depending on which slice it lives in.
type UsageGroup struct {
	Key        string `json:"key"`
	TotalCents int64  `json:"total_cents"`
}

// BillingProvider is the port the HTTP billing handlers depend on. Two
// implementations live in internal/adapter/billing/{local,stripe}/ — the
// local one is real for Phase 3.5; stripe is a stub returning
// ErrNotImplemented until Phase 7.
type BillingProvider interface {
	Name() string
	AttachCard(ctx context.Context, p Principal, in AddCardInput) (PaymentMethod, error)
	GetPaymentMethod(ctx context.Context, p Principal) (PaymentMethod, error)
	DetachCard(ctx context.Context, p Principal) error
}
