package rolerecommend

// Store — the DB half of the role recommender. Collects Signals per org via
// SQL over org_members + users + audit_log + sessions, persists Candidates to
// role_recommendations, and services the list/decide handlers.
//
// Every method routes through db.FromCtx so protected HTTP requests run
// inside the per-request RLS tx (tenant isolation enforced by Postgres),
// while the cron path (RunAll) falls back to the owning admin pool — cron is
// system-level, same convention as the other usecase orchestrators.

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// Recommendation is one persisted role_recommendations row, email-enriched
// for display. DecidedAt is nil / DecidedBy empty while status is pending.
type Recommendation struct {
	ID              string
	OrgID           string
	UserID          string
	Email           string
	CurrentRole     domain.Role
	RecommendedRole domain.Role
	Rule            string
	Rationale       string
	Status          string
	CreatedAt       time.Time
	DecidedAt       *time.Time
	DecidedBy       string
}

// Member is one org_members row enriched with the user's email and login
// recency — powers GET /v1/orgs/{id}/members.
type Member struct {
	UserID      string
	Email       string
	Role        domain.Role
	JoinedAt    time.Time
	LastLoginAt *time.Time
}

// Store persists and serves role recommendations. The pool is the fallback
// Querier used when ctx carries no RLS tx (cron); wire the admin pool.
type Store struct {
	pool db.Querier
	cfg  Config
}

// NewStore builds a Store with DefaultConfig thresholds.
func NewStore(pool db.Querier) *Store {
	return &Store{pool: pool, cfg: DefaultConfig()}
}

// signalsSQL computes one Signals row per org member. The correlated
// subqueries stay on the audit_log (org_id, created_at) and sessions PK
// indexes; member counts per org are small so N+1 subqueries beat a fan-out
// join here.
const signalsSQL = `
SELECT om.user_id::text,
       u.email,
       om.role,
       om.created_at,
       (SELECT max(s.created_at) FROM sessions s
         WHERE s.user_id = om.user_id AND s.org_id = om.org_id)                       AS last_login_at,
       (SELECT count(*) FROM audit_log a
         WHERE a.org_id = om.org_id AND a.actor = om.user_id::text
           AND a.created_at >= $2 AND a.action = ANY($4))                             AS admin_actions_30d,
       (SELECT count(*) FROM audit_log a
         WHERE a.org_id = om.org_id AND a.actor = om.user_id::text
           AND a.created_at >= $3)                                                   AS total_actions_60d,
       (SELECT count(*) FROM audit_log a
         WHERE a.org_id = om.org_id AND a.actor = om.user_id::text
           AND a.created_at >= $3 AND a.action = ANY($4))                             AS admin_actions_60d
FROM org_members om
JOIN users u ON u.id = om.user_id
WHERE om.org_id = $1
ORDER BY om.created_at, om.user_id`

// collectSignals reads the per-member behaviour signals for one org.
func collectSignals(ctx context.Context, q db.Querier, orgID string, now time.Time) ([]Signals, error) {
	rows, err := q.Query(ctx, signalsSQL,
		orgID,
		now.AddDate(0, 0, -30),
		now.AddDate(0, 0, -60),
		AdminAuditActions,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Signals{}
	for rows.Next() {
		var s Signals
		var role string
		if err := rows.Scan(&s.UserID, &s.Email, &role, &s.MemberSince,
			&s.LastLoginAt, &s.AdminActions30d, &s.TotalActions60d, &s.AdminActions60d); err != nil {
			return nil, err
		}
		s.Role = domain.Role(role)
		out = append(out, s)
	}
	return out, rows.Err()
}

// Refresh runs the analyser for one org and persists any newly-fired
// candidates as pending recommendations. Idempotent:
//
//   - the partial unique index (one pending row per member) makes repeated
//     runs no-ops while a recommendation is still open, and
//   - a rule dismissed for a user within the last 30 days is not re-raised
//     (dismissal cool-down keyed on the rule id).
//
// Returns the number of NEW pending rows created.
func (s *Store) Refresh(ctx context.Context, orgID string) (int, error) {
	q := db.FromCtx(ctx, s.pool)
	now := time.Now().UTC()

	signals, err := collectSignals(ctx, q, orgID, now)
	if err != nil {
		return 0, err
	}

	created := 0
	for _, c := range EvaluateAll(now, signals, s.cfg) {
		tag, err := q.Exec(ctx, `
            INSERT INTO role_recommendations (org_id, user_id, existing_role, recommended_role, rule, rationale)
            SELECT $1, $2, $3, $4, $5, $6
            WHERE NOT EXISTS (
                SELECT 1 FROM role_recommendations
                WHERE org_id = $1 AND user_id = $2 AND rule = $5
                  AND status = 'dismissed'
                  AND decided_at >= now() - interval '30 days'
            )
            ON CONFLICT (org_id, user_id) WHERE status = 'pending' DO NOTHING`,
			orgID, c.UserID, string(c.CurrentRole), string(c.RecommendedRole), c.Rule, c.Rationale,
		)
		if err != nil {
			return created, err
		}
		created += int(tag.RowsAffected())
	}
	return created, nil
}

// RunAll refreshes every org — the periodic entry point for the cron
// orchestrator in cmd/server. Uses the fallback pool directly (no request
// ctx at cron time). Returns total new recommendations across all orgs.
func (s *Store) RunAll(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text FROM organizations`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	orgIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		orgIDs = append(orgIDs, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	total := 0
	for _, orgID := range orgIDs {
		n, err := s.Refresh(ctx, orgID)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// ListPending returns the open recommendations for one org, newest first.
func (s *Store) ListPending(ctx context.Context, orgID string) ([]Recommendation, error) {
	q := db.FromCtx(ctx, s.pool)
	rows, err := q.Query(ctx, `
        SELECT rr.id::text, rr.org_id::text, rr.user_id::text, u.email,
               rr.existing_role, rr.recommended_role, rr.rule, rr.rationale,
               rr.status, rr.created_at
        FROM role_recommendations rr
        JOIN users u ON u.id = rr.user_id
        WHERE rr.org_id = $1 AND rr.status = 'pending'
        ORDER BY rr.created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Recommendation{}
	for rows.Next() {
		var r Recommendation
		var cur, rec string
		if err := rows.Scan(&r.ID, &r.OrgID, &r.UserID, &r.Email,
			&cur, &rec, &r.Rule, &r.Rationale, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.CurrentRole = domain.Role(cur)
		r.RecommendedRole = domain.Role(rec)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListMembers returns every member of the org with email + role + login
// recency — the data the Members & Roles page needs to render a real table
// (its Phase 3 placeholder only showed the caller).
func (s *Store) ListMembers(ctx context.Context, orgID string) ([]Member, error) {
	q := db.FromCtx(ctx, s.pool)
	rows, err := q.Query(ctx, `
        SELECT om.user_id::text, u.email, om.role, om.created_at,
               (SELECT max(s2.created_at) FROM sessions s2
                 WHERE s2.user_id = om.user_id AND s2.org_id = om.org_id)
        FROM org_members om
        JOIN users u ON u.id = om.user_id
        WHERE om.org_id = $1
        ORDER BY om.created_at, om.user_id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Member{}
	for rows.Next() {
		var m Member
		var role string
		if err := rows.Scan(&m.UserID, &m.Email, &role, &m.JoinedAt, &m.LastLoginAt); err != nil {
			return nil, err
		}
		m.Role = domain.Role(role)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Decide resolves one pending recommendation.
//
// accept=true applies the role change: org_members.role is updated to the
// recommended role inside the same RLS tx that marks the row accepted, but
// ONLY if the member's live role still equals the row's existing_role — a
// stale recommendation (role changed since analysis) returns ErrConflict
// instead of silently clobbering. Owner rows are refused defensively even
// though the engine never emits them.
//
// accept=false marks the row dismissed; the rule enters its 30-day cool-down
// for that user.
func (s *Store) Decide(ctx context.Context, orgID, recID string, accept bool, deciderUserID string) (Recommendation, error) {
	q := db.FromCtx(ctx, s.pool)

	var rec Recommendation
	var cur, recRole string
	err := q.QueryRow(ctx, `
        SELECT rr.id::text, rr.org_id::text, rr.user_id::text, u.email,
               rr.existing_role, rr.recommended_role, rr.rule, rr.rationale,
               rr.status, rr.created_at
        FROM role_recommendations rr
        JOIN users u ON u.id = rr.user_id
        WHERE rr.id = $1 AND rr.org_id = $2`, recID, orgID).
		Scan(&rec.ID, &rec.OrgID, &rec.UserID, &rec.Email,
			&cur, &recRole, &rec.Rule, &rec.Rationale, &rec.Status, &rec.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Recommendation{}, domain.ErrNotFound
	}
	if err != nil {
		return Recommendation{}, err
	}
	rec.CurrentRole = domain.Role(cur)
	rec.RecommendedRole = domain.Role(recRole)

	if rec.Status != "pending" {
		return Recommendation{}, domain.ErrConflict
	}

	if accept {
		if rec.CurrentRole == domain.RoleOwner || rec.RecommendedRole == domain.RoleOwner {
			return Recommendation{}, domain.ErrConflict
		}
		// Guarded role change: the WHERE pins the role the analyser saw, so
		// a concurrent manual change makes this a 0-row update → conflict.
		tag, err := q.Exec(ctx, `
            UPDATE org_members SET role = $1
            WHERE org_id = $2 AND user_id = $3 AND role = $4`,
			string(rec.RecommendedRole), orgID, rec.UserID, string(rec.CurrentRole))
		if err != nil {
			return Recommendation{}, err
		}
		if tag.RowsAffected() == 0 {
			return Recommendation{}, domain.ErrConflict
		}
	}

	status := "dismissed"
	if accept {
		status = "accepted"
	}
	now := time.Now().UTC()
	_, err = q.Exec(ctx, `
        UPDATE role_recommendations
        SET status = $1, decided_at = $2, decided_by = $3
        WHERE id = $4 AND org_id = $5 AND status = 'pending'`,
		status, now, deciderUserID, recID, orgID)
	if err != nil {
		return Recommendation{}, err
	}

	rec.Status = status
	rec.DecidedAt = &now
	rec.DecidedBy = deciderUserID
	return rec, nil
}
