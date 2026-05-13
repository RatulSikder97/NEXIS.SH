// Package stripe is the Phase 7 Stripe-backed BillingProvider. The legacy
// AttachCard path is intentionally rejected — Stripe.js tokenises raw PANs in
// the browser and our server boundary never sees them. The SetupIntent flow
// (CreateSetupIntent + AttachPaymentMethod) is the only path the dashboard
// uses when BILLING_PROVIDER=stripe.
package stripe

import (
	"context"
	"fmt"
	"sync"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/paymentmethod"
	"github.com/stripe/stripe-go/v82/setupintent"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Persistence is the narrow surface this provider needs from the billing
// repo. *repo.BillingRepo satisfies it via structural typing. The Stripe
// provider reads existing rows to reuse stashed customer ids and writes back
// the canonical brand/last4 projection after a successful attach.
type Persistence interface {
	UpsertPaymentMethod(ctx context.Context, pm domain.PaymentMethod) error
	GetPaymentMethod(ctx context.Context, orgID string) (*domain.PaymentMethod, error)
	DeletePaymentMethod(ctx context.Context, orgID string) error
}

// Config wires the Stripe SDK against runtime credentials. Empty SecretKey is
// rejected by New so a misconfigured BILLING_PROVIDER=stripe boot fails loud
// rather than silently producing 500s at first card-on-file attempt.
type Config struct {
	SecretKey string
}

// Provider implements domain.BillingProvider against the real Stripe API.
type Provider struct {
	repo      Persistence
	secretKey string
}

// keyMu serialises writes to the package-global stripe.Key. The Stripe SDK
// reads stripe.Key on every call; we set it once at New so subsequent calls
// (across goroutines) all use the same key. The mutex defends against the
// theoretical case of multiple Stripe Providers being constructed (e.g. dev
// tests) racing on the global.
var keyMu sync.Mutex

// New constructs a Provider. Returns an error if SecretKey is empty so the
// factory can refuse to boot under BILLING_PROVIDER=stripe without
// STRIPE_SECRET_KEY.
func New(cfg Config, repo Persistence) (*Provider, error) {
	if cfg.SecretKey == "" {
		return nil, fmt.Errorf("stripe billing: STRIPE_SECRET_KEY is required when BILLING_PROVIDER=stripe")
	}
	keyMu.Lock()
	stripe.Key = cfg.SecretKey
	keyMu.Unlock()
	return &Provider{repo: repo, secretKey: cfg.SecretKey}, nil
}

// compile-time conformance check
var _ domain.BillingProvider = (*Provider)(nil)

// Name returns the provider identifier surfaced in factory + logs.
func (p *Provider) Name() string { return "stripe" }

// AttachCard rejects raw PANs. The dashboard's Stripe Elements flow tokenises
// the card client-side; our backend only ever sees the resulting pm_... id
// via AttachPaymentMethod. Refusing here means a buggy/legacy caller can't
// accidentally POST a PAN at this boundary.
func (p *Provider) AttachCard(_ context.Context, _ domain.Principal, _ domain.AddCardInput) (domain.PaymentMethod, error) {
	return domain.PaymentMethod{}, fmt.Errorf("stripe billing: AttachCard is not supported — use the SetupIntent flow")
}

// GetPaymentMethod returns the projection persisted by AttachPaymentMethod.
// Stripe is the source of truth for the underlying pm_... object, but the
// dashboard only needs brand + last4 + expiry which we cache on the row.
func (p *Provider) GetPaymentMethod(ctx context.Context, princ domain.Principal) (domain.PaymentMethod, error) {
	pm, err := p.repo.GetPaymentMethod(ctx, princ.OrgID)
	if err != nil {
		return domain.PaymentMethod{}, err
	}
	return *pm, nil
}

// DetachCard removes the PaymentMethod from the Stripe customer (best-effort)
// and deletes the local projection row. We swallow Stripe-side detach errors
// because the user-visible contract is "this org no longer has a card on
// file" — leaving an orphan pm_... at Stripe is a cleanup concern, not a
// correctness one.
func (p *Provider) DetachCard(ctx context.Context, princ domain.Principal) error {
	existing, err := p.repo.GetPaymentMethod(ctx, princ.OrgID)
	if err == nil && existing != nil && existing.ExternalPaymentMethodID != "" {
		_, _ = paymentmethod.Detach(existing.ExternalPaymentMethodID, &stripe.PaymentMethodDetachParams{})
	}
	return p.repo.DeletePaymentMethod(ctx, princ.OrgID)
}

// CreateSetupIntent mints a Stripe SetupIntent bound to the org's customer
// (creating the customer on demand when the org hasn't yet attached a payment
// method) and returns the client_secret the browser needs to confirm the
// SetupIntent via Stripe.js.
//
// The customer id is stashed on a stub payment_methods row so subsequent
// SetupIntent attempts reuse the same Stripe customer. The stub row is
// overwritten by AttachPaymentMethod once the browser confirms the
// SetupIntent and posts back the resulting pm_... id.
func (p *Provider) CreateSetupIntent(ctx context.Context, princ domain.Principal) (string, error) {
	customerID, err := p.ensureCustomer(ctx, princ, "")
	if err != nil {
		return "", err
	}
	cardType := "card"
	si, err := setupintent.New(&stripe.SetupIntentParams{
		Customer:           stripe.String(customerID),
		PaymentMethodTypes: stripe.StringSlice([]string{cardType}),
		Usage:              stripe.String("off_session"),
	})
	if err != nil {
		return "", fmt.Errorf("stripe billing: setupintent.New: %w", err)
	}
	if si.ClientSecret == "" {
		return "", fmt.Errorf("stripe billing: setupintent.New returned empty client_secret")
	}
	return si.ClientSecret, nil
}

// AttachPaymentMethod attaches the supplied pm_... id to the org's Stripe
// customer, sets it as the default for future invoices, and persists the
// brand/last4/expiry projection. Returns the freshly-persisted PaymentMethod
// so the handler can echo it back to the caller.
func (p *Provider) AttachPaymentMethod(ctx context.Context, princ domain.Principal, pmID, billingEmail string) (domain.PaymentMethod, error) {
	if pmID == "" {
		return domain.PaymentMethod{}, fmt.Errorf("payment_method id required")
	}
	customerID, err := p.ensureCustomer(ctx, princ, billingEmail)
	if err != nil {
		return domain.PaymentMethod{}, err
	}

	// Attach the PM to the customer. Stripe is idempotent here for an already-
	// attached PM-to-same-customer pair, so retrying is safe.
	attached, err := paymentmethod.Attach(pmID, &stripe.PaymentMethodAttachParams{
		Customer: stripe.String(customerID),
	})
	if err != nil {
		return domain.PaymentMethod{}, fmt.Errorf("stripe billing: paymentmethod.Attach: %w", err)
	}

	// Set the freshly-attached PM as the customer's default for future
	// invoices. Without this Stripe will fall back to "no default" and our
	// invoice cron path can't charge against subscriptions later.
	updateParams := &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{
			DefaultPaymentMethod: stripe.String(attached.ID),
		},
	}
	if billingEmail != "" {
		updateParams.Email = stripe.String(billingEmail)
	}
	if _, err := customer.Update(customerID, updateParams); err != nil {
		return domain.PaymentMethod{}, fmt.Errorf("stripe billing: customer.Update: %w", err)
	}

	brand, last4, expMonth, expYear := extractCardDetails(attached)
	pm := domain.PaymentMethod{
		OrgID:                   princ.OrgID,
		Provider:                "stripe",
		ExternalCustomerID:      customerID,
		ExternalPaymentMethodID: attached.ID,
		Brand:                   brand,
		Last4:                   last4,
		ExpMonth:                expMonth,
		ExpYear:                 expYear,
		BillingEmail:            billingEmail,
	}
	if err := p.repo.UpsertPaymentMethod(ctx, pm); err != nil {
		return domain.PaymentMethod{}, err
	}
	out, err := p.repo.GetPaymentMethod(ctx, princ.OrgID)
	if err != nil {
		return domain.PaymentMethod{}, err
	}
	return *out, nil
}

// ensureCustomer reuses the org's existing Stripe customer id if one is
// stashed on the payment_methods row, otherwise mints a new customer and
// upserts a stub row carrying just the customer id. The stub is replaced by
// AttachPaymentMethod once the browser confirms a SetupIntent.
func (p *Provider) ensureCustomer(ctx context.Context, princ domain.Principal, billingEmail string) (string, error) {
	if existing, err := p.repo.GetPaymentMethod(ctx, princ.OrgID); err == nil && existing != nil && existing.ExternalCustomerID != "" {
		return existing.ExternalCustomerID, nil
	}
	params := &stripe.CustomerParams{
		Metadata: map[string]string{
			"nexis_org_id": princ.OrgID,
		},
	}
	if billingEmail != "" {
		params.Email = stripe.String(billingEmail)
	}
	cust, err := customer.New(params)
	if err != nil {
		return "", fmt.Errorf("stripe billing: customer.New: %w", err)
	}
	stub := domain.PaymentMethod{
		OrgID:              princ.OrgID,
		Provider:           "stripe",
		ExternalCustomerID: cust.ID,
		BillingEmail:       billingEmail,
	}
	if err := p.repo.UpsertPaymentMethod(ctx, stub); err != nil {
		return "", err
	}
	return cust.ID, nil
}

// extractCardDetails reads brand/last4/expiry from a *stripe.PaymentMethod.
// Non-card payment methods return zero values; the dashboard renders those
// rows as generic "payment method on file" without brand-specific iconography.
func extractCardDetails(pm *stripe.PaymentMethod) (brand, last4 string, expMonth, expYear int) {
	if pm == nil || pm.Card == nil {
		return "", "", 0, 0
	}
	return string(pm.Card.Brand), pm.Card.Last4, int(pm.Card.ExpMonth), int(pm.Card.ExpYear)
}
