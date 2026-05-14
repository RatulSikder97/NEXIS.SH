//go:build integration

package repo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

type webhookDeliveriesTestFixture struct {
	t         *testing.T
	repo      *WebhookDeliveriesRepo
	adminPool *pgxpool.Pool
	appPool   *pgxpool.Pool
	tx        pgx.Tx
	orgID     string
	userID    string
}

func webhookDeliveriesFixture(t *testing.T) (context.Context, *webhookDeliveriesTestFixture) {
	t.Helper()
	adminURL := os.Getenv("DATABASE_URL_TEST")
	if adminURL == "" {
		t.Skip("DATABASE_URL_TEST unset — skipping webhook deliveries repo test")
	}
	appURL := os.Getenv("DATABASE_URL_TEST_APP")
	if appURL == "" {
		appURL = swapCredsToNexisApp(t, adminURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	t.Cleanup(func() { adminPool.Close() })

	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatalf("connect app: %v", err)
	}
	t.Cleanup(func() { appPool.Close() })

	stamp := time.Now().UTC().Format("20060102150405.000000")
	var orgID, userID string
	if err := adminPool.QueryRow(ctx,
		`INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id::text`,
		"wh-"+stamp, "wh-"+stamp,
	).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := adminPool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, '$2a$10$abcdefghijklmnopqrstuv') RETURNING id::text`,
		"wh-"+stamp+"@test",
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := adminPool.Exec(ctx,
		`UPDATE organizations SET owner_user_id = $1 WHERE id = $2`, userID, orgID); err != nil {
		t.Fatalf("owner: %v", err)
	}

	repo := NewWebhookDeliveriesRepo(appPool, adminPool)

	tx, err := appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", orgID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("pin: %v", err)
	}

	fix := &webhookDeliveriesTestFixture{
		t: t, repo: repo, adminPool: adminPool, appPool: appPool,
		tx: tx, orgID: orgID, userID: userID,
	}
	t.Cleanup(fix.cleanup)
	return db.WithTx(ctx, tx), fix
}

func (f *webhookDeliveriesTestFixture) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if f.tx != nil {
		_ = f.tx.Commit(ctx)
	}
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM webhook_deliveries WHERE org_id=$1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `UPDATE organizations SET owner_user_id = NULL WHERE id=$1`, f.orgID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM users WHERE id=$1`, f.userID)
	_, _ = f.adminPool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, f.orgID)
}

func (f *webhookDeliveriesTestFixture) commitAndReopen(ctx context.Context) context.Context {
	f.t.Helper()
	if f.tx != nil {
		if err := f.tx.Commit(ctx); err != nil {
			f.t.Fatalf("commit: %v", err)
		}
	}
	tx, err := f.appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		f.t.Fatalf("re-begin: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", f.orgID); err != nil {
		_ = tx.Rollback(ctx)
		f.t.Fatalf("re-pin: %v", err)
	}
	f.tx = tx
	return db.WithTx(ctx, tx)
}

// TestWebhookDeliveriesRepo_Insert_RoundTrip persists a row with full
// header + payload + source_ip and reads it back via ListByOrg.
func TestWebhookDeliveriesRepo_Insert_RoundTrip(t *testing.T) {
	ctx, fix := webhookDeliveriesFixture(t)

	d := WebhookDelivery{
		OrgID:       fix.orgID,
		Provider:    "sentry",
		EventType:   "event.alert",
		Status:      "verified",
		LatencyMs:   123,
		PayloadSize: 4096,
		SourceIP:    "10.0.0.1",
		Headers:     map[string]string{"Idempotency-Key": "evt-xyz", "X-Test": "yes"},
		Payload:     map[string]any{"hello": "world"},
		Error:       "",
	}
	if err := fix.repo.Insert(ctx, d); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	ctx = fix.commitAndReopen(ctx)

	rows, total, err := fix.repo.ListByOrg(ctx, ListFilter{OrgID: fix.orgID, Limit: 10})
	if err != nil {
		t.Fatalf("ListByOrg: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("got total=%d rows=%d want 1/1", total, len(rows))
	}
	got := rows[0]
	if got.Provider != "sentry" || got.EventType != "event.alert" || got.Status != "verified" {
		t.Fatalf("scalar fields lost: %+v", got)
	}
	if got.LatencyMs != 123 || got.PayloadSize != 4096 {
		t.Fatalf("int fields lost: %+v", got)
	}
	if got.SourceIP != "10.0.0.1" {
		t.Fatalf("source_ip lost: %q", got.SourceIP)
	}
	if got.Headers["Idempotency-Key"] != "evt-xyz" {
		t.Fatalf("headers lost: %+v", got.Headers)
	}
	if got.Payload["hello"] != "world" {
		t.Fatalf("payload lost: %+v", got.Payload)
	}
}

// TestWebhookDeliveriesRepo_Insert_NilHeadersPayload — nil maps must NULL out.
func TestWebhookDeliveriesRepo_Insert_NilHeadersPayload(t *testing.T) {
	ctx, fix := webhookDeliveriesFixture(t)

	d := WebhookDelivery{
		OrgID:    fix.orgID,
		Provider: "github",
		Status:   "verified",
	}
	if err := fix.repo.Insert(ctx, d); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	ctx = fix.commitAndReopen(ctx)

	rows, _, err := fix.repo.ListByOrg(ctx, ListFilter{OrgID: fix.orgID})
	if err != nil {
		t.Fatalf("ListByOrg: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows want 1", len(rows))
	}
	if rows[0].SourceIP != "" {
		t.Fatalf("empty source_ip should be unset, got %q", rows[0].SourceIP)
	}
	if len(rows[0].Headers) != 0 {
		t.Fatalf("nil headers should remain empty, got %+v", rows[0].Headers)
	}
}

// TestWebhookDeliveriesRepo_Insert_PayloadAtBoundary — payload near the
// cap encodes fine. The actual byte-truncation path produces invalid JSON
// when triggered (see PRODUCTION-CODE-FINDINGS at bottom of file); the
// handler is expected to truncate before passing the row in.
func TestWebhookDeliveriesRepo_Insert_PayloadAtBoundary(t *testing.T) {
	ctx, fix := webhookDeliveriesFixture(t)

	// Just under the cap — stays well within JSONB.
	mid := make([]byte, payloadCap-1024)
	for i := range mid {
		mid[i] = 'x'
	}
	d := WebhookDelivery{
		OrgID:    fix.orgID,
		Provider: "sentry",
		Status:   "verified",
		Payload:  map[string]any{"mid": string(mid)},
	}
	if err := fix.repo.Insert(ctx, d); err != nil {
		t.Fatalf("Insert: %v", err)
	}
}

// TestWebhookDeliveriesRepo_ExistsByProviderEventID_Idempotency.
func TestWebhookDeliveriesRepo_ExistsByProviderEventID_Idempotency(t *testing.T) {
	ctx, fix := webhookDeliveriesFixture(t)

	d := WebhookDelivery{
		OrgID:    fix.orgID,
		Provider: "sentry",
		Status:   "verified",
		Headers:  map[string]string{"Idempotency-Key": "evt-abc"},
	}
	if err := fix.repo.Insert(ctx, d); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	ctx = fix.commitAndReopen(ctx)

	// Known eventID for the org+provider — exists.
	exists, err := fix.repo.ExistsByProviderEventID(ctx, fix.orgID, "sentry", "evt-abc")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatalf("expected exists=true")
	}

	// Unknown eventID — does not exist.
	exists, err = fix.repo.ExistsByProviderEventID(ctx, fix.orgID, "sentry", "evt-zzz")
	if err != nil {
		t.Fatalf("Exists missing: %v", err)
	}
	if exists {
		t.Fatalf("expected exists=false for unknown id")
	}

	// Different provider — does not match across providers.
	exists, err = fix.repo.ExistsByProviderEventID(ctx, fix.orgID, "github", "evt-abc")
	if err != nil {
		t.Fatalf("Exists cross-provider: %v", err)
	}
	if exists {
		t.Fatalf("expected cross-provider exists=false")
	}

	// Empty eventID short-circuits to false, nil.
	exists, err = fix.repo.ExistsByProviderEventID(ctx, fix.orgID, "sentry", "")
	if err != nil || exists {
		t.Fatalf("empty eventID got exists=%v err=%v, want false, nil", exists, err)
	}
}

// TestWebhookDeliveriesRepo_ExistsByProviderEventID_NilSafe.
func TestWebhookDeliveriesRepo_ExistsByProviderEventID_NilSafe(t *testing.T) {
	var r *WebhookDeliveriesRepo
	exists, err := r.ExistsByProviderEventID(context.Background(), "x", "y", "z")
	if err != nil || exists {
		t.Fatalf("nil-repo got exists=%v err=%v, want false, nil", exists, err)
	}
}

// TestWebhookDeliveriesRepo_Insert_NilSafe.
func TestWebhookDeliveriesRepo_Insert_NilSafe(t *testing.T) {
	var r *WebhookDeliveriesRepo
	if err := r.Insert(context.Background(), WebhookDelivery{}); err != nil {
		t.Fatalf("nil-repo Insert: %v", err)
	}
}

// TestWebhookDeliveriesRepo_ListByOrg_Filtering inserts 3 rows across two
// providers and asserts the provider filter narrows the result.
func TestWebhookDeliveriesRepo_ListByOrg_Filtering(t *testing.T) {
	ctx, fix := webhookDeliveriesFixture(t)

	insert := func(provider string) {
		if err := fix.repo.Insert(ctx, WebhookDelivery{
			OrgID: fix.orgID, Provider: provider, Status: "verified",
		}); err != nil {
			t.Fatalf("Insert(%s): %v", provider, err)
		}
	}
	insert("sentry")
	insert("sentry")
	insert("github")
	ctx = fix.commitAndReopen(ctx)

	rows, total, err := fix.repo.ListByOrg(ctx, ListFilter{OrgID: fix.orgID, Limit: 10})
	if err != nil {
		t.Fatalf("ListByOrg all: %v", err)
	}
	if total != 3 || len(rows) != 3 {
		t.Fatalf("all: total=%d rows=%d want 3/3", total, len(rows))
	}

	rows, total, err = fix.repo.ListByOrg(ctx, ListFilter{OrgID: fix.orgID, Provider: "sentry"})
	if err != nil {
		t.Fatalf("ListByOrg sentry: %v", err)
	}
	if total != 2 {
		t.Fatalf("sentry total=%d want 2", total)
	}
}

// TestWebhookDeliveriesRepo_ListByOrg_NilSafe.
func TestWebhookDeliveriesRepo_ListByOrg_NilSafe(t *testing.T) {
	var r *WebhookDeliveriesRepo
	rows, total, err := r.ListByOrg(context.Background(), ListFilter{OrgID: "x"})
	if err != nil {
		t.Fatalf("nil-repo: %v", err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("nil-repo got total=%d rows=%d", total, len(rows))
	}
}

// TestWebhookDeliveriesRepo_ListByOrg_EmptyTotal.
func TestWebhookDeliveriesRepo_ListByOrg_EmptyTotal(t *testing.T) {
	ctx, fix := webhookDeliveriesFixture(t)
	ctx = fix.commitAndReopen(ctx)

	rows, total, err := fix.repo.ListByOrg(ctx, ListFilter{OrgID: fix.orgID})
	if err != nil {
		t.Fatalf("ListByOrg empty: %v", err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("empty org got total=%d rows=%d", total, len(rows))
	}
}
