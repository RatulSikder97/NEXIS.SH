// Package audit implements the domain.AuditWriter port with a per-org
// HMAC-SHA256 chain.
//
// Each row's row_hash = HMAC(secret, prev_hash || canonicalJSON(payload)), so
// any tampering (insertion, deletion, mutation) of an earlier row breaks
// every subsequent row's hash and is detected by Verify.
//
// The writer chooses between the per-request RLS tx (when one is in ctx) and
// the owning pool. Signup/login run BEFORE the RLS middleware fires — there is
// no tx in ctx and no principal cookie yet — so those callsites land on the
// pool path. Protected mutations (MFA, api-keys) run inside the RLS tx and the
// audit INSERT therefore participates in the same atomic boundary as the
// mutation that triggered it.
package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// HMACWriter appends to audit_log under a per-org hash chain. The secret is
// the AUDIT_SECRET configured on the control-plane — independent from the
// session signing secret so a session-secret leak does not let an attacker
// forge audit rows.
type HMACWriter struct {
	secret []byte
	pool   db.Querier // fallback used when ctx carries no tx (signup/login path)
}

// New constructs an HMACWriter. The pool is used as the Querier fallback when
// the request hasn't entered an RLS tx (signup, login, magic-link consume).
func New(secret []byte, pool db.Querier) *HMACWriter {
	return &HMACWriter{secret: secret, pool: pool}
}

// Write appends one row to audit_log. The row's prev_hash links to the
// previous row for the same org (or nil for the first row in that org), and
// row_hash = HMAC(secret, prev_hash || canonicalJSON(payload)).
//
// The payload deliberately includes id + created_at so the chain is bound to
// the exact identity and timestamp persisted, not just the user-visible fields.
func (w *HMACWriter) Write(ctx context.Context, p domain.Principal, action, target string, metadata map[string]any) error {
	q := db.FromCtx(ctx, w.pool)

	var prev []byte
	err := q.QueryRow(ctx,
		`SELECT row_hash FROM audit_log WHERE org_id = $1 ORDER BY created_at DESC LIMIT 1`,
		p.OrgID,
	).Scan(&prev)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		prev = nil
	}

	now := time.Now().UTC()
	id := uuid.NewString()
	payload := map[string]any{
		"id":         id,
		"org_id":     p.OrgID,
		"actor":      p.UserID,
		"action":     action,
		"target":     target,
		"metadata":   metadata,
		"created_at": now.Format(time.RFC3339Nano),
	}
	canon := canonicalJSON(payload)
	mac := hmac.New(sha256.New, w.secret)
	mac.Write(prev)
	mac.Write(canon)
	rowHash := mac.Sum(nil)

	// Serialise metadata to jsonb-friendly bytes; nil → SQL NULL.
	var metaArg any
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		metaArg = b
	}

	_, err = q.Exec(ctx, `
        INSERT INTO audit_log (id, org_id, actor, action, target, metadata, prev_hash, row_hash, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, p.OrgID, p.UserID, action, target, metaArg, prev, rowHash, now,
	)
	return err
}

// canonicalJSON encodes a map with keys sorted lexicographically. We avoid
// json.Marshal on the top-level map because Go's encoder sorts keys for maps
// but we want the encoding to be explicit (and stable across Go versions) so
// the Verify routine can reproduce the exact bytes that were HMAC'd at Write.
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

// VerifyResult is the body returned by GET /v1/audit/verify.
//
//   - OK reports overall chain integrity.
//   - RowsChecked is the total number of rows the walker visited (across all
//     orgs) before stopping.
//   - FirstBadRowID is set on tamper to the id of the first row whose
//     recomputed HMAC didn't match its stored row_hash.
//   - Error is set when verification fails for an infrastructure reason (DB
//     down, scan failure) rather than a chain mismatch.
type VerifyResult struct {
	OK            bool   `json:"ok"`
	RowsChecked   int    `json:"rows_checked"`
	FirstBadRowID string `json:"first_bad_row_id,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Verify walks the audit log per-org and recomputes the HMAC chain. Returns
// {OK:false, FirstBadRowID} on tamper. The pool argument MUST be a connection
// that can read all rows across all orgs — in practice, the admin pool. Verify
// does NOT use the per-request RLS tx because the integrity check is a global
// invariant, not a tenant-scoped query.
func Verify(ctx context.Context, pool db.Querier, secret []byte) (VerifyResult, error) {
	rows, err := pool.Query(ctx, `
        SELECT id, org_id, actor, action, target, metadata, prev_hash, row_hash, created_at
        FROM audit_log
        ORDER BY org_id, created_at`)
	if err != nil {
		return VerifyResult{}, err
	}
	defer rows.Close()

	perOrg := map[string][]byte{} // org_id → expected prev_hash for the next row in that org

	var count int
	for rows.Next() {
		var (
			id, orgID, actor, action string
			target                   *string
			metadataRaw              []byte
			prev, rowHash            []byte
			createdAt                time.Time
		)
		if err := rows.Scan(&id, &orgID, &actor, &action, &target, &metadataRaw, &prev, &rowHash, &createdAt); err != nil {
			return VerifyResult{}, err
		}
		count++

		var meta map[string]any
		if len(metadataRaw) > 0 {
			_ = json.Unmarshal(metadataRaw, &meta)
		}

		tgt := ""
		if target != nil {
			tgt = *target
		}

		payload := map[string]any{
			"id":         id,
			"org_id":     orgID,
			"actor":      actor,
			"action":     action,
			"target":     tgt,
			"metadata":   meta,
			"created_at": createdAt.UTC().Format(time.RFC3339Nano),
		}
		canon := canonicalJSON(payload)
		mac := hmac.New(sha256.New, secret)
		mac.Write(perOrg[orgID]) // nil for the first row in this org
		mac.Write(canon)
		want := mac.Sum(nil)
		if !hmac.Equal(want, rowHash) {
			return VerifyResult{OK: false, RowsChecked: count, FirstBadRowID: id}, nil
		}
		perOrg[orgID] = rowHash
	}
	if err := rows.Err(); err != nil {
		return VerifyResult{}, err
	}
	return VerifyResult{OK: true, RowsChecked: count}, nil
}
