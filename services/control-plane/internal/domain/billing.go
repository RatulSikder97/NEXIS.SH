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
//
// TotalCents is the floor()-ed whole-cent figure for invoice line items.
// TotalCentsExact preserves sub-cent precision so a few minutes of $0.10/hr
// metering still renders as $0.0021 instead of $0.00.
type UsageBreakdown struct {
	TotalCents      int64        `json:"total_cents"`
	TotalCentsExact float64      `json:"total_cents_exact"`
	ByWorkspace     []UsageGroup `json:"by_workspace"`
	ByKind          []UsageGroup `json:"by_kind"`
}

// UsageGroup is one bucket inside a UsageBreakdown. Key is the workspace id
// or the kind name depending on which slice it lives in.
type UsageGroup struct {
	Key             string  `json:"key"`
	TotalCents      int64   `json:"total_cents"`
	TotalCentsExact float64 `json:"total_cents_exact"`
}

// BillingProvider is the port the HTTP billing handlers depend on. Two
// implementations live in internal/adapter/billing/{local,stripe}/ — the
// local one is real for Phase 3.5; stripe is real in Phase 7 once
// STRIPE_SECRET_KEY is wired.
//
// Two payment-collection flows live behind this port:
//
//   - Legacy `AttachCard` accepts raw card data at the BillingProvider
//     boundary (PAN + CVC never crosses the boundary into the repo). The
//     local provider implements this flow end-to-end with synthetic
//     ids; the stripe provider rejects it (the Stripe SDK must never see
//     a raw PAN — Stripe.js is responsible for tokenisation client-side).
//
//   - Stripe SetupIntent flow used by the dashboard's Stripe Elements
//     form (Phase 7). `CreateSetupIntent` returns a client_secret the
//     browser uses to confirm the SetupIntent via Stripe.js, then the
//     browser posts the resulting payment_method id back to
//     `AttachPaymentMethod`, which attaches it to the org's Stripe
//     customer, sets it as the default for future invoices, and persists
//     the brand+last4+expiry projection. The local provider stubs both
//     calls with synthetic ids so the same wire surface works in dev
//     without a Stripe key.
type BillingProvider interface {
	Name() string
	AttachCard(ctx context.Context, p Principal, in AddCardInput) (PaymentMethod, error)
	GetPaymentMethod(ctx context.Context, p Principal) (PaymentMethod, error)
	DetachCard(ctx context.Context, p Principal) error

	// CreateSetupIntent returns the client_secret of a freshly-minted Stripe
	// SetupIntent bound to the org's Stripe customer (creating the customer
	// on demand when the org hasn't yet attached a payment method). The
	// local provider returns a synthetic "seti_local_..." secret.
	CreateSetupIntent(ctx context.Context, p Principal) (clientSecret string, err error)

	// AttachPaymentMethod attaches a previously-tokenised payment_method id
	// (e.g. "pm_1Nyz..." from Stripe.js) to the org's Stripe customer, sets
	// it as the default for future invoices, and persists the brand/last4
	// projection. The local provider accepts any pmID and writes a synthetic
	// row identical in shape to AttachCard.
	AttachPaymentMethod(ctx context.Context, p Principal, pmID, billingEmail string) (PaymentMethod, error)
}
