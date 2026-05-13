package repo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditRepo appends "gitops.pr_opened" rows to the global audit_log table.
// The chain hashing matches the control-plane's HMACWriter so a downstream
// Verify run sees the gitops rows as part of the same per-org chain.
//
// Phase 6 simplification: the gitops service runs under the nexis_gitops
// role (migration 0018) which has a permissive INSERT policy on audit_log
// — no app.current_org_id GUC bind is required.
type AuditRepo struct {
	pool   *pgxpool.Pool
	secret []byte
}

// NewAuditRepo wires a repo with the HMAC secret. secret should match the
// AUDIT_SECRET used by the control-plane so Verify works across both
// services.
func NewAuditRepo(p *pgxpool.Pool, secret []byte) *AuditRepo {
	return &AuditRepo{pool: p, secret: secret}
}

// AppendPROpened inserts a row chained on the org's previous row_hash. The
// payload mirrors the control-plane's AuditWriter shape so the Verify walker
// has a single canonical form.
//
// Returns the row id on success; errors are surfaced verbatim.
func (r *AuditRepo) AppendPROpened(ctx context.Context, orgID, actor, target string, metadata map[string]any) (string, error) {
	if r.pool == nil {
		return "", errors.New("audit repo: pool nil")
	}
	if len(r.secret) == 0 {
		return "", errors.New("audit repo: HMAC secret empty")
	}

	var prev []byte
	err := r.pool.QueryRow(ctx,
		`SELECT row_hash FROM audit_log WHERE org_id=$1 ORDER BY created_at DESC LIMIT 1`,
		orgID,
	).Scan(&prev)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	id := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)

	payload := map[string]any{
		"id":         id,
		"org_id":     orgID,
		"actor":      actor,
		"action":     "gitops.pr_opened",
		"target":     target,
		"metadata":   metadata,
		"created_at": now.Format(time.RFC3339Nano),
	}
	canon := canonicalJSON(payload)
	mac := hmac.New(sha256.New, r.secret)
	mac.Write(prev)
	mac.Write(canon)
	rowHash := mac.Sum(nil)

	var metaArg any
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return "", fmt.Errorf("audit repo: marshal metadata: %w", err)
		}
		metaArg = b
	}
	var actorArg any
	if actor != "" {
		actorArg = actor
	}

	_, err = r.pool.Exec(ctx, `
        INSERT INTO audit_log (id, org_id, actor, action, target, metadata, prev_hash, row_hash, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, orgID, actorArg, "gitops.pr_opened", target, metaArg, prev, rowHash, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// canonicalJSON encodes a map with keys sorted lexicographically. Same
// implementation as control-plane's audit package; we duplicate to avoid a
// cross-service import.
func canonicalJSON(m map[string]any) []byte {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []byte{'{'}
	for i, k := range keys {
		if i > 0 {
			out = append(out, ',')
		}
		kj, _ := json.Marshal(k)
		vj, _ := json.Marshal(m[k])
		out = append(out, kj...)
		out = append(out, ':')
		out = append(out, vj...)
	}
	out = append(out, '}')
	return out
}
