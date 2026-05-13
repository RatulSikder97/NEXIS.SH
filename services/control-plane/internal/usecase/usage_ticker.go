// Package usecase holds cron-scoped orchestrators. They depend only on
// domain types + narrow port interfaces defined here; main.go wires real
// repo adapters that satisfy these interfaces via structural typing.
package usecase

import (
	"context"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// ReadyWorkspaceLister enumerates ready workspaces across all orgs. The
// production repo bypasses RLS via the bare admin pool — cron is system-level.
type ReadyWorkspaceLister interface {
	ListAllReady(ctx context.Context) ([]domain.Workspace, error)
}

// UsageRecorder is the narrow surface UsageTicker needs from the billing
// repo: insert one usage row using the admin pool (bypassing RLS).
type UsageRecorder interface {
	RecordUsageAdmin(ctx context.Context, ur domain.UsageRecord) error
}

// UsageTicker records one usage_records row per ready workspace per tick. The
// per-tick quantity is interval-in-hours so PriceCents acts as $/hour; the
// amount_cents column lands fractional cents that round once at the API.
type UsageTicker struct {
	Workspaces ReadyWorkspaceLister
	Billing    UsageRecorder
	Interval   time.Duration
	PriceCents float64
}

// Run lists every ready workspace and inserts one usage record per row.
// Bypasses RLS via the *Admin repo methods because there is no principal in
// ctx at cron time.
func (u *UsageTicker) Run(ctx context.Context) error {
	if u.Workspaces == nil || u.Billing == nil {
		return nil
	}
	fraction := u.Interval.Hours()
	ws, err := u.Workspaces.ListAllReady(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, w := range ws {
		amount := fraction * u.PriceCents
		_ = u.Billing.RecordUsageAdmin(ctx, domain.UsageRecord{
			OrgID:          w.OrgID,
			WorkspaceID:    w.ID,
			Project:        "default",
			Kind:           "runtime_hours",
			Quantity:       fraction,
			UnitPriceCents: u.PriceCents,
			AmountCents:    amount,
			RecordedAt:     now,
		})
	}
	return nil
}
