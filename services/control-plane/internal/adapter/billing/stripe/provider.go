// Package stripe is the Phase 7 placeholder for the Stripe-backed
// BillingProvider. Every method returns domain.ErrNotImplemented; the factory
// only constructs this when BILLING_PROVIDER=stripe so the dev path picks the
// local provider by default.
package stripe

import (
	"context"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Config is reserved for the Phase 7 Stripe wiring (api key, webhook secret,
// price catalog). Empty today.
type Config struct{}

// Provider is the stub implementation. Zero value is usable.
type Provider struct{}

// New returns a Provider. Accepts a Config so the future Phase 7 wiring can
// pass real credentials without touching the call sites.
func New(_ Config) *Provider { return &Provider{} }

// compile-time conformance check
var _ domain.BillingProvider = (*Provider)(nil)

// Name returns "stripe" so the wire surface can show which provider is active.
func (p *Provider) Name() string { return "stripe" }

// AttachCard is not implemented in Phase 3.5.
func (p *Provider) AttachCard(_ context.Context, _ domain.Principal, _ domain.AddCardInput) (domain.PaymentMethod, error) {
	return domain.PaymentMethod{}, domain.ErrNotImplemented
}

// GetPaymentMethod is not implemented in Phase 3.5.
func (p *Provider) GetPaymentMethod(_ context.Context, _ domain.Principal) (domain.PaymentMethod, error) {
	return domain.PaymentMethod{}, domain.ErrNotImplemented
}

// DetachCard is not implemented in Phase 3.5.
func (p *Provider) DetachCard(_ context.Context, _ domain.Principal) error {
	return domain.ErrNotImplemented
}
