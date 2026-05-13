// Package billing wires the configured BillingProvider. Today there are two
// sub-adapters: local (Phase 3.5, real) and stripe (Phase 7 stub). The
// factory picks based on cfg.BillingProvider.
package billing

import (
	"fmt"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/billing/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/billing/stripe"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// NewFromConfig constructs the BillingProvider named by cfg.BillingProvider.
// Empty string defaults to "local". Unknown values are a config error.
//
// billingRepo is supplied by main.go rather than constructed here so it can
// be shared with the cron jobs (UsageTicker, InvoiceRoller) without
// double-wiring.
func NewFromConfig(cfg config.Config, billingRepo *repo.BillingRepo) (domain.BillingProvider, error) {
	switch cfg.BillingProvider {
	case "local", "":
		return local.New(billingRepo), nil
	case "stripe":
		return stripe.New(stripe.Config{}), nil
	default:
		return nil, fmt.Errorf("unknown BILLING_PROVIDER %q", cfg.BillingProvider)
	}
}
