package repo

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// IncidentsRepo is a write-only adapter for incidents_raw — the landing table
// for unprocessed events from third-party sources (Sentry webhooks today,
// otel/github in later phases). Read paths live in Phase 4's normaliser.
type IncidentsRepo struct{ pool *pgxpool.Pool }

// NewIncidentsRepo builds an IncidentsRepo against the supplied pool. Like
// IntegrationsRepo, every Insert prefers the per-request RLS tx via
// db.FromCtx and only falls back to this pool when no tx is attached.
func NewIncidentsRepo(pool *pgxpool.Pool) *IncidentsRepo {
	return &IncidentsRepo{pool: pool}
}

// Insert lands a single raw incident. The (org_id, source, source_event_id)
// unique index makes Insert idempotent — a retried webhook delivery is a no-op
// rather than a duplicate row.
func (r *IncidentsRepo) Insert(ctx context.Context, orgID string, raw domain.RawIncident) error {
	q := db.FromCtx(ctx, r.pool)
	payloadJSON, err := json.Marshal(raw.Payload)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
        INSERT INTO incidents_raw (org_id, source, source_event_id, title, level, service, environment, raw_payload)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
        ON CONFLICT (org_id, source, source_event_id) DO NOTHING
    `, orgID, raw.Source, raw.SourceEventID, raw.Title, raw.Level, raw.Service, raw.Environment, payloadJSON)
	return err
}

// compile-time conformance check
var _ domain.IncidentSink = (*IncidentsRepo)(nil)
