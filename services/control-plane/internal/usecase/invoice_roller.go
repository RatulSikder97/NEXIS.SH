package usecase

import (
	"context"
	"time"
)

// InvoiceStore is the narrow billing-repo surface InvoiceRoller needs.
// *repo.BillingRepo satisfies it via structural typing. All methods bypass
// RLS via the admin pool — cron has no principal to pin to.
type InvoiceStore interface {
	AdminListOrgIDsWithUsageInPeriod(ctx context.Context, since, until time.Time) ([]string, error)
	AdminOpenInvoice(ctx context.Context, orgID string, periodStart, periodEnd time.Time) error
	AdminCloseInvoicesForPeriod(ctx context.Context, periodStart time.Time) error
}

// InvoiceRoller is the daily roll-up cron. On each tick:
//
//  1. For every org with usage in the CURRENT month, ensure an open invoice
//     exists. UNIQUE(org_id, period_start) makes this idempotent.
//  2. For every open invoice whose period_start is in the PREVIOUS month,
//     compute total_cents = SUM(amount_cents) and close as paid.
//
// Phase 3.5 marks invoices paid automatically because there is no real
// charge. Phase 7 swaps that for a Stripe-driven flow.
type InvoiceRoller struct {
	Billing InvoiceStore
}

// Run executes the roll-up.
func (r *InvoiceRoller) Run(ctx context.Context) error {
	if r.Billing == nil {
		return nil
	}
	now := time.Now().UTC()
	curStart, curEnd := monthBounds(now)
	prevStart, _ := monthBounds(curStart.AddDate(0, 0, -1))

	orgIDs, err := r.Billing.AdminListOrgIDsWithUsageInPeriod(ctx, curStart, curEnd)
	if err != nil {
		return err
	}
	for _, orgID := range orgIDs {
		if err := r.Billing.AdminOpenInvoice(ctx, orgID, curStart, curEnd); err != nil {
			return err
		}
	}
	return r.Billing.AdminCloseInvoicesForPeriod(ctx, prevStart)
}

// monthBounds returns [start, end) of the calendar month containing t (UTC).
func monthBounds(t time.Time) (time.Time, time.Time) {
	t = t.UTC()
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	return start, end
}
