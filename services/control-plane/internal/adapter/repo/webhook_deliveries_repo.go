package repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// WebhookDeliveriesRepo persists rows in webhook_deliveries (migration 0023).
//
// Dual-pool — same shape as the rest of the Phase 4+ repos:
//
//   - pool (nexis_app, RLS-aware) is the Querier fallback used by the webhook
//     handler's per-request RLS tx via db.FromCtx. The webhook handler pins
//     app.current_org_id from the URL/query before delegating to the adapter,
//     so the INSERT runs under the tenant policy without a request principal.
//
//   - adminPool (nexis super-user, RLS-bypass) is used by the operator-side
//     read endpoint. The /v1/integrations/webhooks handler has a session
//     principal in ctx but the ListByOrg call is intentionally org-scoped at
//     the SQL layer (defence-in-depth) so the admin pool keeps the read path
//     uniform with the rest of the cross-tenant / system-job reads in the
//     repo package (workflows, incidents, integrations).
//
// The table is append-only at the application boundary — there is no Update
// or Delete here on purpose.
type WebhookDeliveriesRepo struct {
	pool      *pgxpool.Pool // nexis_app — RLS-aware, fallback for db.FromCtx
	adminPool *pgxpool.Pool // nexis super-user — bypasses RLS for operator reads
}

// NewWebhookDeliveriesRepo constructs the repo. adminPool may be nil — the
// read paths fall back to pool which works when RLS is off but breaks once
// the 0023 policy is enabled.
func NewWebhookDeliveriesRepo(pool, adminPool *pgxpool.Pool) *WebhookDeliveriesRepo {
	if adminPool == nil {
		adminPool = pool
	}
	return &WebhookDeliveriesRepo{pool: pool, adminPool: adminPool}
}

// WebhookDelivery is the in-memory shape of one webhook_deliveries row. ID +
// TS are server-assigned on insert; the caller leaves them zero.
type WebhookDelivery struct {
	ID          string
	OrgID       string
	Provider    string
	EventType   string
	Status      string // verified | rejected | processed | failed
	LatencyMs   int
	PayloadSize int
	SourceIP    string
	Headers     map[string]string
	Payload     map[string]any
	Error       string
	TS          time.Time
}

// payloadCap is the soft cap on the JSON payload column. The webhook handler
// truncates before passing in so the cap is enforced once at the boundary
// rather than in the SQL layer (jsonb has no max-size constraint).
const payloadCap = 32 * 1024

// Insert appends one delivery row. Routes through db.FromCtx so the webhook
// handler's per-request RLS tx is honoured. Payload/headers are encoded as
// jsonb; nil maps land as SQL NULL via the jsonOrNil helper.
//
// nil-safe — when r is nil the call is a silent no-op so call sites in
// handler/webhooks.go can defensively pass through even when the repo isn't
// wired (the dev/no-pool boot path).
func (r *WebhookDeliveriesRepo) Insert(ctx context.Context, d WebhookDelivery) error {
	if r == nil {
		return nil
	}
	// Headers / payload come in as Go maps; encode to bytes once so we don't
	// pay the marshal cost twice (jsonOrNil takes []byte).
	var headersJSON, payloadJSON []byte
	if len(d.Headers) > 0 {
		b, err := json.Marshal(d.Headers)
		if err != nil {
			return err
		}
		headersJSON = b
	}
	if len(d.Payload) > 0 {
		b, err := json.Marshal(d.Payload)
		if err != nil {
			return err
		}
		// Belt-and-braces: if the caller forgot to truncate, we still honour
		// the soft cap so a runaway upstream cannot fill the DB row.
		if len(b) > payloadCap {
			b = b[:payloadCap]
		}
		payloadJSON = b
	}
	q := db.FromCtx(ctx, r.pool)
	// source_ip is `inet`; pass an empty string as NULL so the column isn't
	// constrained when the upstream proxy stripped X-Forwarded-For.
	var sourceIP any
	if d.SourceIP != "" {
		sourceIP = d.SourceIP
	}
	_, err := q.Exec(ctx, `
        INSERT INTO webhook_deliveries (
            org_id, provider, event_type, status,
            latency_ms, payload_size, source_ip,
            headers, payload, error
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9, NULLIF($10, ''))`,
		d.OrgID, d.Provider, d.EventType, d.Status,
		d.LatencyMs, d.PayloadSize, sourceIP,
		jsonOrNil(headersJSON), jsonOrNil(payloadJSON), d.Error,
	)
	return err
}

// ExistsByProviderEventID returns true when a webhook_deliveries row already
// exists for (orgID, provider) with the provider-specific event id stashed
// inside the headers JSONB. The event id is canonicalised at the handler
// boundary into a single header key (`Idempotency-Key`) so the SQL lookup is
// uniform across providers; see handler/webhooks.go::stashIdempotencyKey for
// the per-provider mapping.
//
// Uses the admin pool because the lookup must run BEFORE the per-request RLS
// tx is opened — we want to dedupe even when the caller is anonymous (every
// webhook arrives without a session). Tenancy is enforced by the org_id=$1
// filter; the orgID itself is validated upstream against the organizations
// table before this call.
//
// nil-safe / no-pool-safe — returns (false, nil) so the dev path keeps
// working when the repo isn't wired.
func (r *WebhookDeliveriesRepo) ExistsByProviderEventID(ctx context.Context, orgID, provider, eventID string) (bool, error) {
	if r == nil || r.adminPool == nil || eventID == "" {
		return false, nil
	}
	var exists bool
	err := r.adminPool.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM webhook_deliveries
            WHERE org_id = $1::uuid
              AND provider = $2
              AND headers ->> 'Idempotency-Key' = $3
        )`,
		orgID, provider, eventID,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// ListFilter holds optional filters for the operator-side list endpoint.
// Empty Provider matches every provider. Limit defaults to 50 (max 200);
// Offset defaults to 0.
type ListFilter struct {
	OrgID    string
	Provider string
	Limit    int
	Offset   int
}

// ListByOrg returns the most-recent N deliveries for an org, optionally
// filtered by provider. Returns the page rows + the unpaginated total so the
// dashboard can render "N of M" without an extra round-trip.
//
// Runs on the admin pool — the operator endpoint authenticates via the
// session middleware (which guarantees orgID matches the caller's org) and
// then the SQL-layer org_id filter is the tenancy boundary. Using the admin
// pool here keeps the read uniform with the rest of the cross-tenant /
// system-job reads in this package.
func (r *WebhookDeliveriesRepo) ListByOrg(ctx context.Context, f ListFilter) ([]WebhookDelivery, int, error) {
	if r == nil || r.adminPool == nil {
		return []WebhookDelivery{}, 0, nil
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	// Total (unpaginated) for the pager — keeps the wire shape stable.
	var total int
	if err := r.adminPool.QueryRow(ctx, `
        SELECT count(*) FROM webhook_deliveries
        WHERE org_id=$1 AND ($2='' OR provider=$2)`,
		f.OrgID, f.Provider,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []WebhookDelivery{}, 0, nil
	}

	rows, err := r.adminPool.Query(ctx, `
        SELECT id::text, org_id::text, provider, event_type, status,
               latency_ms, payload_size,
               COALESCE(host(source_ip), ''),
               COALESCE(headers::text, ''),
               COALESCE(payload::text, ''),
               COALESCE(error, ''), ts
        FROM webhook_deliveries
        WHERE org_id=$1 AND ($2='' OR provider=$2)
        ORDER BY ts DESC
        LIMIT $3 OFFSET $4`,
		f.OrgID, f.Provider, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]WebhookDelivery, 0, limit)
	for rows.Next() {
		var d WebhookDelivery
		var headersStr, payloadStr string
		if err := rows.Scan(
			&d.ID, &d.OrgID, &d.Provider, &d.EventType, &d.Status,
			&d.LatencyMs, &d.PayloadSize,
			&d.SourceIP,
			&headersStr, &payloadStr,
			&d.Error, &d.TS,
		); err != nil {
			return nil, 0, err
		}
		if headersStr != "" {
			_ = json.Unmarshal([]byte(headersStr), &d.Headers)
		}
		if payloadStr != "" {
			_ = json.Unmarshal([]byte(payloadStr), &d.Payload)
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}
