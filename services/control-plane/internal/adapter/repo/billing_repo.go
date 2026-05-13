package repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// BillingRepo persists payment_methods, invoices, and usage_records. Tenant-
// facing methods route through db.FromCtx so RLS enforcement is automatic;
// admin-only methods (RecordUsageAdmin, AdminListOrgIDsWithUsageInPeriod,
// AdminOpenInvoice, AdminCloseInvoicesForPeriod) hit adminPool directly so
// the cron can write across all orgs without an RLS principal in ctx.
type BillingRepo struct {
	pool      *pgxpool.Pool
	adminPool *pgxpool.Pool
}

// NewBillingRepo constructs a BillingRepo. pool is the application pool used
// for RLS-aware reads inside the per-request tx; adminPool is the admin pool
// used for cron paths and falls back to pool when nil.
func NewBillingRepo(pool, adminPool *pgxpool.Pool) *BillingRepo {
	if adminPool == nil {
		adminPool = pool
	}
	return &BillingRepo{pool: pool, adminPool: adminPool}
}

// UpsertPaymentMethod inserts or updates the (org_id) row. The unique index on
// org_id guarantees one payment method per org — Phase 3.5 does not support
// multiple cards. brand + last4 + expiry are surfaced to the dashboard; PAN
// + CVC are never persisted.
func (r *BillingRepo) UpsertPaymentMethod(ctx context.Context, pm domain.PaymentMethod) error {
	q := db.FromCtx(ctx, r.pool)
	_, err := q.Exec(ctx, `
        INSERT INTO payment_methods
          (org_id, provider, external_customer_id, external_payment_method_id,
           brand, last4, exp_month, exp_year, billing_email)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
        ON CONFLICT (org_id) DO UPDATE SET
          provider                   = EXCLUDED.provider,
          external_customer_id       = EXCLUDED.external_customer_id,
          external_payment_method_id = EXCLUDED.external_payment_method_id,
          brand                      = EXCLUDED.brand,
          last4                      = EXCLUDED.last4,
          exp_month                  = EXCLUDED.exp_month,
          exp_year                   = EXCLUDED.exp_year,
          billing_email              = EXCLUDED.billing_email`,
		pm.OrgID, pm.Provider, pm.ExternalCustomerID, pm.ExternalPaymentMethodID,
		pm.Brand, pm.Last4, pm.ExpMonth, pm.ExpYear, pm.BillingEmail,
	)
	return err
}

// GetPaymentMethod returns the org's payment method or domain.ErrNotFound.
func (r *BillingRepo) GetPaymentMethod(ctx context.Context, orgID string) (*domain.PaymentMethod, error) {
	q := db.FromCtx(ctx, r.pool)
	var pm domain.PaymentMethod
	err := q.QueryRow(ctx, `
        SELECT id, org_id, provider,
               COALESCE(external_customer_id, ''),
               COALESCE(external_payment_method_id, ''),
               COALESCE(brand, ''),
               COALESCE(last4, ''),
               COALESCE(exp_month, 0),
               COALESCE(exp_year, 0),
               COALESCE(billing_email, ''),
               created_at
        FROM payment_methods WHERE org_id=$1`, orgID).Scan(
		&pm.ID, &pm.OrgID, &pm.Provider, &pm.ExternalCustomerID, &pm.ExternalPaymentMethodID,
		&pm.Brand, &pm.Last4, &pm.ExpMonth, &pm.ExpYear, &pm.BillingEmail, &pm.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &pm, nil
}

// DeletePaymentMethod removes the row. Idempotent — no row deleted is not an
// error.
func (r *BillingRepo) DeletePaymentMethod(ctx context.Context, orgID string) error {
	q := db.FromCtx(ctx, r.pool)
	_, err := q.Exec(ctx, `DELETE FROM payment_methods WHERE org_id=$1`, orgID)
	return err
}

// RecordUsage inserts a single usage_records row using the per-request RLS tx
// when present (the recompute endpoint runs under a principal). For the cron,
// use RecordUsageAdmin instead.
func (r *BillingRepo) RecordUsage(ctx context.Context, ur domain.UsageRecord) error {
	q := db.FromCtx(ctx, r.pool)
	_, err := q.Exec(ctx, `
        INSERT INTO usage_records
          (org_id, workspace_id, project, kind, quantity, unit_price_cents, amount_cents, recorded_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		ur.OrgID, ur.WorkspaceID, ur.Project, ur.Kind, ur.Quantity, ur.UnitPriceCents, ur.AmountCents, ur.RecordedAt,
	)
	return err
}

// RecordUsageAdmin is the cron-friendly variant: it uses the bare admin pool
// rather than threading a tx, because there is no principal in ctx at cron
// time. RLS is bypassed intentionally.
func (r *BillingRepo) RecordUsageAdmin(ctx context.Context, ur domain.UsageRecord) error {
	_, err := r.adminPool.Exec(ctx, `
        INSERT INTO usage_records
          (org_id, workspace_id, project, kind, quantity, unit_price_cents, amount_cents, recorded_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		ur.OrgID, ur.WorkspaceID, ur.Project, ur.Kind, ur.Quantity, ur.UnitPriceCents, ur.AmountCents, ur.RecordedAt,
	)
	return err
}

// ListUsage returns the usage_records rows for one org in a time window. Used
// by the dev recompute endpoint; production reads go through UsageSum.
func (r *BillingRepo) ListUsage(ctx context.Context, orgID string, since, until time.Time) ([]domain.UsageRecord, error) {
	q := db.FromCtx(ctx, r.pool)
	rows, err := q.Query(ctx, `
        SELECT id, org_id, workspace_id, project, kind,
               quantity::float8, unit_price_cents::float8, amount_cents::float8, recorded_at
        FROM usage_records
        WHERE org_id=$1 AND recorded_at >= $2 AND recorded_at < $3
        ORDER BY recorded_at DESC`, orgID, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.UsageRecord{}
	for rows.Next() {
		var u domain.UsageRecord
		if err := rows.Scan(&u.ID, &u.OrgID, &u.WorkspaceID, &u.Project, &u.Kind, &u.Quantity, &u.UnitPriceCents, &u.AmountCents, &u.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UsageSum returns the total amount_cents in the window plus breakdowns by
// workspace and by kind. The totals are floor()-ed to int64 cents; per-tick
// fractional cents accumulate and only round once at the surface.
func (r *BillingRepo) UsageSum(ctx context.Context, orgID string, since, until time.Time) (domain.UsageBreakdown, error) {
	q := db.FromCtx(ctx, r.pool)
	out := domain.UsageBreakdown{ByWorkspace: []domain.UsageGroup{}, ByKind: []domain.UsageGroup{}}

	var totalF float64
	if err := q.QueryRow(ctx, `
        SELECT COALESCE(SUM(amount_cents), 0)::float8
        FROM usage_records
        WHERE org_id=$1 AND recorded_at >= $2 AND recorded_at < $3`, orgID, since, until).Scan(&totalF); err != nil {
		return out, err
	}
	out.TotalCents = int64(totalF)

	rows, err := q.Query(ctx, `
        SELECT workspace_id::text, COALESCE(SUM(amount_cents), 0)::float8
        FROM usage_records
        WHERE org_id=$1 AND recorded_at >= $2 AND recorded_at < $3
        GROUP BY workspace_id`, orgID, since, until)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var key string
		var amount float64
		if err := rows.Scan(&key, &amount); err != nil {
			rows.Close()
			return out, err
		}
		out.ByWorkspace = append(out.ByWorkspace, domain.UsageGroup{Key: key, TotalCents: int64(amount)})
	}
	rows.Close()

	rows, err = q.Query(ctx, `
        SELECT kind, COALESCE(SUM(amount_cents), 0)::float8
        FROM usage_records
        WHERE org_id=$1 AND recorded_at >= $2 AND recorded_at < $3
        GROUP BY kind`, orgID, since, until)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var amount float64
		if err := rows.Scan(&key, &amount); err != nil {
			return out, err
		}
		out.ByKind = append(out.ByKind, domain.UsageGroup{Key: key, TotalCents: int64(amount)})
	}
	return out, rows.Err()
}

// OpenInvoice idempotently inserts an open invoice for the given period. The
// UNIQUE(org_id, period_start) index makes this a no-op on the second call.
func (r *BillingRepo) OpenInvoice(ctx context.Context, orgID string, periodStart, periodEnd time.Time) error {
	q := db.FromCtx(ctx, r.pool)
	_, err := q.Exec(ctx, `
        INSERT INTO invoices (org_id, period_start, period_end, total_cents, status)
        VALUES ($1, $2, $3, 0, 'open')
        ON CONFLICT (org_id, period_start) DO NOTHING`, orgID, periodStart, periodEnd)
	return err
}

// CloseInvoice closes an invoice in-place: status -> paid, total_cents updated.
// Phase 3.5 marks invoices paid automatically because there is no real charge.
func (r *BillingRepo) CloseInvoice(ctx context.Context, orgID string, periodStart time.Time, totalCents int64) error {
	q := db.FromCtx(ctx, r.pool)
	_, err := q.Exec(ctx, `
        UPDATE invoices SET total_cents=$3, status='paid'
        WHERE org_id=$1 AND period_start=$2 AND status='open'`, orgID, periodStart, totalCents)
	return err
}

// ListInvoices returns all of an org's invoices, newest first.
func (r *BillingRepo) ListInvoices(ctx context.Context, orgID string) ([]domain.Invoice, error) {
	q := db.FromCtx(ctx, r.pool)
	rows, err := q.Query(ctx, `
        SELECT id, org_id, period_start, period_end, total_cents, status, created_at
        FROM invoices WHERE org_id=$1 ORDER BY period_start DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Invoice{}
	for rows.Next() {
		var inv domain.Invoice
		if err := rows.Scan(&inv.ID, &inv.OrgID, &inv.PeriodStart, &inv.PeriodEnd, &inv.TotalCents, &inv.Status, &inv.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// AdminListOrgIDsWithUsageInPeriod enumerates distinct org_ids that have any
// usage_records in [since, until). Used by the invoice cron to know which
// orgs need an open invoice. Bypasses RLS via the bare pool.
func (r *BillingRepo) AdminListOrgIDsWithUsageInPeriod(ctx context.Context, since, until time.Time) ([]string, error) {
	rows, err := r.adminPool.Query(ctx, `
        SELECT DISTINCT org_id::text FROM usage_records
        WHERE recorded_at >= $1 AND recorded_at < $2`, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// AdminOpenInvoice is the cron-friendly variant of OpenInvoice. Bypasses RLS.
func (r *BillingRepo) AdminOpenInvoice(ctx context.Context, orgID string, periodStart, periodEnd time.Time) error {
	_, err := r.adminPool.Exec(ctx, `
        INSERT INTO invoices (org_id, period_start, period_end, total_cents, status)
        VALUES ($1, $2, $3, 0, 'open')
        ON CONFLICT (org_id, period_start) DO NOTHING`, orgID, periodStart, periodEnd)
	return err
}

// AdminCloseInvoicesForPeriod closes every open invoice with period_start in
// the window, computing each org's total_cents as the sum of amount_cents in
// usage_records during the invoice's [period_start, period_end). Bypasses RLS.
func (r *BillingRepo) AdminCloseInvoicesForPeriod(ctx context.Context, periodStart time.Time) error {
	_, err := r.adminPool.Exec(ctx, `
        UPDATE invoices i SET
          total_cents = COALESCE((
            SELECT FLOOR(SUM(u.amount_cents))::bigint FROM usage_records u
            WHERE u.org_id = i.org_id
              AND u.recorded_at >= i.period_start
              AND u.recorded_at < i.period_end
          ), 0),
          status = 'paid'
        WHERE i.period_start = $1 AND i.status = 'open'`, periodStart)
	return err
}
