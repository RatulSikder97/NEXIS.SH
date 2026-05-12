# Phase 3 — Console Shell + Integrations — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the 8-surface console shell with full implementation for Home/Integrations/Settings/Audit, plus per-tenant GitHub install (mock) + Sentry webhook ingestion (HMAC) + ArgoCD token storage + member invites + RBAC + theme persistence.

**Architecture:** Mirrors Phase 1+2 port/adapter pattern. New `IntegrationProvider` port with `github`/`sentry`/`argocd` adapters behind a factory. New `KeyVault` port with `LocalKeyVault` (AES-GCM) — Phase 7's KMS adapter swaps in. New `RequireRole` middleware enforces the RBAC matrix. Console is a Next.js route group `(app)/console/*` with a sidebar+topbar+cmdk+inspector chrome; 4 surfaces fully built, 4 placeholders for Phase 4-6.

**Tech Stack:** Go 1.25, Next.js 16.2.2 + React 19, Tailwind v4, shadcn/ui, `@tanstack/react-table` (new), `cmdk` (new), AES-256-GCM, HMAC-SHA256, Postgres 16 RLS.

**Spec:** `docs/superpowers/specs/2026-05-12-phase-3-console-integrations.md`.

---

## Salvage / Reuse from Phase 1+2

- `internal/domain/{auth,audit,errors}.go` — extend, do not rewrite.
- `internal/transport/http/middleware/{auth,rls}.go` — keep; layer new `rbac.go` on top.
- `internal/adapter/audit/hmac_writer.go` — extend with `List(ctx, filters, paging)`.
- `internal/adapter/auth/local/{provider,store,pgstore}.go` — extend `Store` interface for new repos.
- `apps/web/app/(app)/dashboard/page.tsx` — keep but redirect-route to `/console`.
- `apps/web/components/ui/Button.tsx`, `lib/auth.ts`, `proxy.ts` — reuse.
- `packages/db/schema.ts` — extend.
- `docker-compose.yml` — add `MASTER_KEY` env on control-plane.

---

## Stage 0 — Schema additions + key vault plumbing

### Task 0.1: Extend Drizzle schema

**Files:**
- Modify: `packages/db/schema.ts`

- [ ] **Step 1: Add `preferences` to users**

Find the `users` table definition; add:
```ts
preferences: jsonb("preferences").notNull().default(sql`'{}'::jsonb`),
```

- [ ] **Step 2: Add three new tables**

Append to `packages/db/schema.ts`:

```ts
export const integrations = pgTable("integrations", {
  id:                uuid("id").primaryKey().defaultRandom(),
  orgId:             uuid("org_id").notNull().references(() => organizations.id),
  provider:          text("provider", { enum: ["github", "sentry", "argocd"] }).notNull(),
  status:            text("status", { enum: ["connected", "pending", "error", "disconnected"] }).notNull(),
  installationId:    text("installation_id"),
  secretCiphertext:  bytea("secret_ciphertext"),
  metadata:          jsonb("metadata"),
  lastError:         text("last_error"),
  createdAt:         timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  updatedAt:         timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqOrgProvider: uniqueIndex("integrations_org_provider_uniq").on(t.orgId, t.provider),
}));

export const incidentsRaw = pgTable("incidents_raw", {
  id:           uuid("id").primaryKey().defaultRandom(),
  orgId:        uuid("org_id").notNull().references(() => organizations.id),
  source:       text("source", { enum: ["sentry", "github", "otel"] }).notNull(),
  sourceEventId: text("source_event_id"),
  title:        text("title"),
  level:        text("level"),
  service:      text("service"),
  environment:  text("environment"),
  rawPayload:   jsonb("raw_payload"),
  receivedAt:   timestamp("received_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqSource: uniqueIndex("incidents_raw_source_uniq").on(t.orgId, t.source, t.sourceEventId),
  orgReceivedIdx: index("incidents_raw_org_received_idx").on(t.orgId, t.receivedAt),
}));

export const orgInvites = pgTable("org_invites", {
  tokenHash:     bytea("token_hash").primaryKey(),
  orgId:         uuid("org_id").notNull().references(() => organizations.id),
  email:         text("email").notNull(),
  role:          text("role", { enum: ["admin", "member"] }).notNull(),
  inviterUserId: uuid("inviter_user_id").notNull().references(() => users.id),
  expiresAt:     timestamp("expires_at", { withTimezone: true }).notNull(),
  claimedAt:     timestamp("claimed_at", { withTimezone: true }),
}, t => ({
  orgEmailIdx: index("org_invites_org_email_idx").on(t.orgId, t.email),
}));
```

Imports: `uniqueIndex` from `drizzle-orm/pg-core`. `bytea` helper already exists from Phase 2.

- [ ] **Step 3: Generate Drizzle migration**

```bash
cd apps/web && /opt/homebrew/bin/pnpm exec drizzle-kit generate --name=phase3_console
```

Inspect `packages/db/migrations/0002_*_phase3_console.sql` — should contain ALTER TABLE users + three CREATE TABLE.

### Task 0.2: Control-plane mirror migration + RLS

**Files:**
- Create: `services/control-plane/migrations/0005_phase3_console.up.sql`
- Create: `services/control-plane/migrations/0005_phase3_console.down.sql`
- Create: `services/control-plane/migrations/0006_phase3_rls.up.sql`
- Create: `services/control-plane/migrations/0006_phase3_rls.down.sql`

- [ ] **Step 1: Mirror SQL**

`0005_phase3_console.up.sql`:
```sql
ALTER TABLE users ADD COLUMN preferences jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE integrations (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id            uuid NOT NULL REFERENCES organizations(id),
  provider          text NOT NULL CHECK (provider IN ('github','sentry','argocd')),
  status            text NOT NULL CHECK (status IN ('connected','pending','error','disconnected')),
  installation_id   text,
  secret_ciphertext bytea,
  metadata          jsonb,
  last_error        text,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, provider)
);

CREATE TABLE incidents_raw (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id),
  source          text NOT NULL CHECK (source IN ('sentry','github','otel')),
  source_event_id text,
  title           text,
  level           text,
  service         text,
  environment     text,
  raw_payload     jsonb,
  received_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, source, source_event_id)
);
CREATE INDEX incidents_raw_org_received_idx ON incidents_raw (org_id, received_at);

CREATE TABLE org_invites (
  token_hash       bytea PRIMARY KEY,
  org_id           uuid NOT NULL REFERENCES organizations(id),
  email            text NOT NULL,
  role             text NOT NULL CHECK (role IN ('admin','member')),
  inviter_user_id  uuid NOT NULL REFERENCES users(id),
  expires_at       timestamptz NOT NULL,
  claimed_at       timestamptz
);
CREATE INDEX org_invites_org_email_idx ON org_invites (org_id, email);
```

`0005_phase3_console.down.sql`:
```sql
DROP TABLE IF EXISTS org_invites;
DROP TABLE IF EXISTS incidents_raw;
DROP TABLE IF EXISTS integrations;
ALTER TABLE users DROP COLUMN IF EXISTS preferences;
```

- [ ] **Step 2: RLS policies**

`0006_phase3_rls.up.sql`:
```sql
ALTER TABLE integrations  ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents_raw ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_invites   ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON integrations
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON incidents_raw
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON org_invites
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON integrations  TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON incidents_raw TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON org_invites   TO nexis_app;
```

`0006_phase3_rls.down.sql`:
```sql
DROP POLICY IF EXISTS tenant_isolation ON integrations;
DROP POLICY IF EXISTS tenant_isolation ON incidents_raw;
DROP POLICY IF EXISTS tenant_isolation ON org_invites;
ALTER TABLE integrations  DISABLE ROW LEVEL SECURITY;
ALTER TABLE incidents_raw DISABLE ROW LEVEL SECURITY;
ALTER TABLE org_invites   DISABLE ROW LEVEL SECURITY;
```

- [ ] **Step 3: Apply both migrations**

```bash
cd apps/web && /opt/homebrew/bin/pnpm exec drizzle-kit migrate
docker compose exec -T postgres psql -U nexis -d nexis -f /tmp/0006_phase3_rls.up.sql
# (cp the file into the container first)
docker compose exec -T postgres psql -U nexis -d nexis -c "\dt"
```

Expected: 3 new tables visible, `users` has `preferences`, all 3 new tables have RLS enabled.

### Task 0.3: Config + env

**Files:**
- Modify: `services/control-plane/internal/platform/config/config.go`
- Modify: `docker-compose.yml`
- Modify: `.env.example`

- [ ] **Step 1: Add `MasterKey` to Config**

```go
// Phase 3
MasterKey string // 32-byte base64 for LocalKeyVault
```

Load via `env("MASTER_KEY", "")`. In `Load()` add `MasterKey: env("MASTER_KEY", "")`.

- [ ] **Step 2: Add to compose**

In `docker-compose.yml`, on `control-plane`:
```yaml
      MASTER_KEY: ${MASTER_KEY:-dGVzdC1tYXN0ZXIta2V5LTMyLWJ5dGVzLWxvbmctYWFh}  # base64 of 32-byte dev key
```

- [ ] **Step 3: Add to `.env.example`**

```
# === Master key (Phase 3) — used to encrypt integration secrets at rest ===
MASTER_KEY=dGVzdC1tYXN0ZXIta2V5LTMyLWJ5dGVzLWxvbmctYWFh
```

(`base64` of `test-master-key-32-bytes-long-aaa`.)

### Task 0.4: Commit Stage 0

```bash
git add packages/db apps/web services/control-plane/migrations services/control-plane/internal/platform/config docker-compose.yml .env.example
git commit -m "feat(db): phase 3 schema + RLS + master key plumbing (stage 0)"
```

---

## Stage 1 — LocalKeyVault adapter

### Task 1.1: Domain port

**Files:**
- Create: `services/control-plane/internal/domain/keyvault.go`

- [ ] **Step 1: Port**

```go
package domain

import "context"

type KeyVault interface {
    Encrypt(ctx context.Context, plaintext []byte) (ciphertext []byte, err error)
    Decrypt(ctx context.Context, ciphertext []byte) (plaintext []byte, err error)
}
```

### Task 1.2: LocalKeyVault — AES-GCM (TDD)

**Files:**
- Create: `services/control-plane/internal/adapter/keyvault/local.go`
- Create: `services/control-plane/internal/adapter/keyvault/local_test.go`

- [ ] **Step 1: Tests**

```go
package keyvault

import (
    "bytes"
    "context"
    "testing"
)

func TestLocalKeyVault_RoundTrip(t *testing.T) {
    kv, err := NewLocal(make([]byte, 32))
    if err != nil { t.Fatal(err) }
    msg := []byte("hello secret world")
    ct, err := kv.Encrypt(context.Background(), msg)
    if err != nil { t.Fatal(err) }
    if bytes.Equal(ct, msg) { t.Fatal("ciphertext == plaintext") }
    pt, err := kv.Decrypt(context.Background(), ct)
    if err != nil { t.Fatal(err) }
    if !bytes.Equal(pt, msg) { t.Errorf("roundtrip mismatch: got %q", pt) }
}

func TestLocalKeyVault_TamperedCiphertextFails(t *testing.T) {
    kv, _ := NewLocal(make([]byte, 32))
    ct, _ := kv.Encrypt(context.Background(), []byte("x"))
    ct[len(ct)-1] ^= 0xff
    if _, err := kv.Decrypt(context.Background(), ct); err == nil {
        t.Fatal("want error on tampered ciphertext")
    }
}

func TestLocalKeyVault_RequiresKey32(t *testing.T) {
    if _, err := NewLocal(make([]byte, 16)); err == nil {
        t.Fatal("want error on short key")
    }
}
```

- [ ] **Step 2: Implementation**

```go
package keyvault

import (
    "context"
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "errors"
    "io"
)

const keyLen = 32

type LocalKeyVault struct{ gcm cipher.AEAD }

func NewLocal(key []byte) (*LocalKeyVault, error) {
    if len(key) != keyLen {
        return nil, errors.New("LocalKeyVault: key must be 32 bytes")
    }
    block, err := aes.NewCipher(key)
    if err != nil { return nil, err }
    gcm, err := cipher.NewGCM(block)
    if err != nil { return nil, err }
    return &LocalKeyVault{gcm: gcm}, nil
}

func (k *LocalKeyVault) Encrypt(_ context.Context, plaintext []byte) ([]byte, error) {
    nonce := make([]byte, k.gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil { return nil, err }
    return k.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (k *LocalKeyVault) Decrypt(_ context.Context, ciphertext []byte) ([]byte, error) {
    if len(ciphertext) < k.gcm.NonceSize() { return nil, errors.New("LocalKeyVault: ciphertext too short") }
    nonce, ct := ciphertext[:k.gcm.NonceSize()], ciphertext[k.gcm.NonceSize():]
    return k.gcm.Open(nil, nonce, ct, nil)
}
```

- [ ] **Step 3: Wire from base64 env in main.go**

In `cmd/server/main.go`, after config.Load:

```go
import "encoding/base64"

masterKey, err := base64.StdEncoding.DecodeString(cfg.MasterKey)
if err != nil || len(masterKey) != 32 {
    if cfg.AppEnv == "dev" {
        logger.Warn("MASTER_KEY invalid; deriving zero key for dev")
        masterKey = make([]byte, 32)
    } else {
        logger.Error("MASTER_KEY missing or not 32 bytes (base64)")
        os.Exit(1)
    }
}
kv, err := keyvault.NewLocal(masterKey)
if err != nil { logger.Error("keyvault", "err", err); os.Exit(1) }
// pass kv into Deps in Task 2.4
```

- [ ] **Step 4: Run + commit**

```bash
cd services/control-plane && make build && go test -race ./internal/adapter/keyvault/...
git add . && git commit -m "feat(crypto): LocalKeyVault AES-256-GCM + tests (stage 1)"
```

---

## Stage 2 — IntegrationProvider port + 3 adapters

### Task 2.1: Domain port

**Files:**
- Create: `services/control-plane/internal/domain/integration.go`

- [ ] **Step 1: Types + port**

```go
package domain

import (
    "context"
    "time"
)

type IntegrationProvider string

const (
    IntegrationGitHub IntegrationProvider = "github"
    IntegrationSentry IntegrationProvider = "sentry"
    IntegrationArgoCD IntegrationProvider = "argocd"
)

type IntegrationStatus string

const (
    StatusConnected    IntegrationStatus = "connected"
    StatusPending      IntegrationStatus = "pending"
    StatusError        IntegrationStatus = "error"
    StatusDisconnected IntegrationStatus = "disconnected"
)

type Connection struct {
    Provider       IntegrationProvider
    Status         IntegrationStatus
    InstallationID string
    Metadata       map[string]any
    LastError      string
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

type Integration interface {
    Name() IntegrationProvider
    Connect(ctx context.Context, p Principal, config map[string]any) (Connection, error)
    Disconnect(ctx context.Context, p Principal) error
    Status(ctx context.Context, p Principal) (Connection, error)
    HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error
}

type IntegrationRegistry interface {
    Get(p IntegrationProvider) (Integration, bool)
    List(ctx context.Context, p Principal) ([]Connection, error)
}

type IncidentSink interface {
    Insert(ctx context.Context, orgID string, raw RawIncident) error
}

type RawIncident struct {
    Source       string
    SourceEventID string
    Title        string
    Level        string
    Service      string
    Environment  string
    Payload      map[string]any
}
```

### Task 2.2: Repo for integrations + incidents_raw

**Files:**
- Create: `services/control-plane/internal/adapter/repo/integrations_repo.go`
- Create: `services/control-plane/internal/adapter/repo/incidents_repo.go`

- [ ] **Step 1: Integrations repo**

```go
package repo

import (
    "context"
    "encoding/json"
    "errors"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

type IntegrationsRepo struct{ pool *pgxpool.Pool }

func NewIntegrationsRepo(pool *pgxpool.Pool) *IntegrationsRepo { return &IntegrationsRepo{pool: pool} }

func (r *IntegrationsRepo) Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error {
    metaJSON, _ := json.Marshal(c.Metadata)
    q := db.FromCtx(ctx, r.pool)
    _, err := q.Exec(ctx, `
        INSERT INTO integrations (org_id, provider, status, installation_id, secret_ciphertext, metadata, last_error)
        VALUES ($1,$2,$3,$4,$5,$6,$7)
        ON CONFLICT (org_id, provider) DO UPDATE
          SET status=EXCLUDED.status, installation_id=EXCLUDED.installation_id,
              secret_ciphertext=EXCLUDED.secret_ciphertext, metadata=EXCLUDED.metadata,
              last_error=EXCLUDED.last_error, updated_at=now()
    `, orgID, string(c.Provider), string(c.Status), c.InstallationID, secret, metaJSON, c.LastError)
    return err
}

func (r *IntegrationsRepo) Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error) {
    q := db.FromCtx(ctx, r.pool)
    var c domain.Connection
    var meta []byte
    var secret []byte
    err := q.QueryRow(ctx, `
        SELECT provider, status, installation_id, metadata, last_error, created_at, updated_at, secret_ciphertext
        FROM integrations WHERE org_id=$1 AND provider=$2
    `, orgID, string(provider)).Scan(
        (*string)(&c.Provider), (*string)(&c.Status), &c.InstallationID, &meta, &c.LastError, &c.CreatedAt, &c.UpdatedAt, &secret,
    )
    if errors.Is(err, pgx.ErrNoRows) { return domain.Connection{}, nil, domain.ErrNotFound }
    if err != nil { return domain.Connection{}, nil, err }
    _ = json.Unmarshal(meta, &c.Metadata)
    return c, secret, nil
}

func (r *IntegrationsRepo) List(ctx context.Context, orgID string) ([]domain.Connection, error) {
    q := db.FromCtx(ctx, r.pool)
    rows, err := q.Query(ctx, `
        SELECT provider, status, installation_id, metadata, last_error, created_at, updated_at
        FROM integrations WHERE org_id=$1 ORDER BY provider
    `, orgID)
    if err != nil { return nil, err }
    defer rows.Close()
    out := []domain.Connection{}
    for rows.Next() {
        var c domain.Connection
        var meta []byte
        if err := rows.Scan((*string)(&c.Provider), (*string)(&c.Status), &c.InstallationID, &meta, &c.LastError, &c.CreatedAt, &c.UpdatedAt); err != nil {
            return nil, err
        }
        _ = json.Unmarshal(meta, &c.Metadata)
        out = append(out, c)
    }
    return out, nil
}

func (r *IntegrationsRepo) Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error {
    q := db.FromCtx(ctx, r.pool)
    _, err := q.Exec(ctx, `DELETE FROM integrations WHERE org_id=$1 AND provider=$2`, orgID, string(provider))
    return err
}
```

- [ ] **Step 2: Incidents repo**

```go
package repo

import (
    "context"
    "encoding/json"

    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

type IncidentsRepo struct{ pool *pgxpool.Pool }

func NewIncidentsRepo(pool *pgxpool.Pool) *IncidentsRepo { return &IncidentsRepo{pool: pool} }

func (r *IncidentsRepo) Insert(ctx context.Context, orgID string, raw domain.RawIncident) error {
    q := db.FromCtx(ctx, r.pool)
    payloadJSON, _ := json.Marshal(raw.Payload)
    _, err := q.Exec(ctx, `
        INSERT INTO incidents_raw (org_id, source, source_event_id, title, level, service, environment, raw_payload)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
        ON CONFLICT (org_id, source, source_event_id) DO NOTHING
    `, orgID, raw.Source, raw.SourceEventID, raw.Title, raw.Level, raw.Service, raw.Environment, payloadJSON)
    return err
}
```

### Task 2.3: GitHub adapter

**Files:**
- Create: `services/control-plane/internal/adapter/integration/github/provider.go`
- Create: `services/control-plane/internal/adapter/integration/github/provider_test.go`

- [ ] **Step 1: Failing test — HMAC verification + ack**

```go
package github

import (
    "context"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "testing"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func TestGitHub_HandleWebhook_VerifiesSignature(t *testing.T) {
    p, _ := newTest(t)
    secret := []byte("dev-webhook-secret-32")
    body := []byte(`{"action":"opened"}`)
    mac := hmac.New(sha256.New, secret)
    mac.Write(body)
    sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

    err := p.HandleWebhook(context.Background(), "org-1",
        map[string]string{"X-Hub-Signature-256": sig, "X-Github-Event": "ping"}, body)
    if err != nil { t.Fatalf("verify: %v", err) }
}

func TestGitHub_HandleWebhook_RejectsBadSignature(t *testing.T) {
    p, _ := newTest(t)
    err := p.HandleWebhook(context.Background(), "org-1",
        map[string]string{"X-Hub-Signature-256": "sha256=bad"}, []byte(`{}`))
    if err == nil { t.Fatal("want HMAC mismatch error") }
}

func TestGitHub_Connect_MockInstall(t *testing.T) {
    p, repo := newTest(t)
    princ := domain.Principal{OrgID: "org-1", UserID: "user-1", Role: domain.RoleOwner}
    c, err := p.Connect(context.Background(), princ, map[string]any{
        "installation_id": "inst-123",
        "scopes": []string{"repo"},
    })
    if err != nil { t.Fatal(err) }
    if c.Status != domain.StatusConnected { t.Errorf("status: %s", c.Status) }
    if c.InstallationID != "inst-123" { t.Errorf("install id: %s", c.InstallationID) }
    if !repo.upsertCalled { t.Error("repo upsert not called") }
}
```

- [ ] **Step 2: Implementation**

```go
package github

import (
    "context"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "fmt"
    "strings"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Repo interface {
    Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
    Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
    Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

type Provider struct {
    repo Repo
    kv   domain.KeyVault
    // Default webhook secret used when caller didn't supply one (dev/mock path).
    defaultSecret []byte
}

func New(repo Repo, kv domain.KeyVault, defaultSecret []byte) *Provider {
    return &Provider{repo: repo, kv: kv, defaultSecret: defaultSecret}
}

func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationGitHub }

func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
    installID, _ := cfg["installation_id"].(string)
    if installID == "" {
        return domain.Connection{}, errors.New("github: installation_id required")
    }
    secret, _ := cfg["webhook_secret"].(string)
    if secret == "" { secret = string(p.defaultSecret) }
    encSecret, err := p.kv.Encrypt(ctx, []byte(secret))
    if err != nil { return domain.Connection{}, fmt.Errorf("github: encrypt secret: %w", err) }
    scopes, _ := cfg["scopes"].([]any)
    meta := map[string]any{"scopes": scopes}
    c := domain.Connection{
        Provider:       domain.IntegrationGitHub,
        Status:         domain.StatusConnected,
        InstallationID: installID,
        Metadata:       meta,
    }
    if err := p.repo.Upsert(ctx, princ.OrgID, c, encSecret); err != nil {
        return domain.Connection{}, err
    }
    return c, nil
}

func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
    return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationGitHub)
}

func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
    c, _, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationGitHub)
    return c, err
}

func (p *Provider) HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error {
    sig := headers["X-Hub-Signature-256"]
    if sig == "" { sig = headers["X-Hub-Signature-256"] } // case-handled by caller
    if !strings.HasPrefix(sig, "sha256=") {
        return errors.New("github: missing X-Hub-Signature-256")
    }

    _, encSecret, err := p.repo.Get(ctx, orgID, domain.IntegrationGitHub)
    var secret []byte
    if err == nil && encSecret != nil {
        secret, err = p.kv.Decrypt(ctx, encSecret)
        if err != nil { return fmt.Errorf("github: decrypt secret: %w", err) }
    } else {
        // Allow default secret in dev (no connection row yet).
        secret = p.defaultSecret
    }

    want := hmac.New(sha256.New, secret)
    want.Write(body)
    if !hmac.Equal([]byte(hex.EncodeToString(want.Sum(nil))), []byte(strings.TrimPrefix(sig, "sha256="))) {
        return errors.New("github: HMAC mismatch")
    }

    // Phase 3: ack only. Event-specific routing lands in Phase 4-6.
    return nil
}
```

### Task 2.4: Sentry adapter

**Files:**
- Create: `services/control-plane/internal/adapter/integration/sentry/provider.go`
- Create: `services/control-plane/internal/adapter/integration/sentry/provider_test.go`

- [ ] **Step 1: Tests — including incidents_raw insert**

```go
package sentry

import (
    "context"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "testing"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func TestSentry_HandleWebhook_InsertsIncident(t *testing.T) {
    p, repo, sink := newTest(t)
    secret := []byte("dev-sentry-secret-32")
    // Pre-seed connection so verify path finds the secret.
    enc, _ := p.kv.Encrypt(context.Background(), secret)
    repo.connections["org-1"] = repoEntry{c: domain.Connection{Provider: domain.IntegrationSentry, Status: domain.StatusConnected}, secret: enc}

    body := []byte(`{"id":"e1","level":"error","title":"T","environment":"prod","tags":[["service","api"]]}`)
    mac := hmac.New(sha256.New, secret)
    mac.Write(body)
    sig := hex.EncodeToString(mac.Sum(nil))

    if err := p.HandleWebhook(context.Background(), "org-1",
        map[string]string{"Sentry-Hook-Signature": sig}, body); err != nil {
        t.Fatalf("webhook: %v", err)
    }
    if len(sink.inserts) != 1 { t.Fatalf("want 1 insert, got %d", len(sink.inserts)) }
    if sink.inserts[0].SourceEventID != "e1" { t.Errorf("event id: %s", sink.inserts[0].SourceEventID) }
    if sink.inserts[0].Service != "api" { t.Errorf("service tag: %s", sink.inserts[0].Service) }
}

func TestSentry_HandleWebhook_RejectsBadHMAC(t *testing.T) {
    p, repo, _ := newTest(t)
    enc, _ := p.kv.Encrypt(context.Background(), []byte("secret"))
    repo.connections["org-1"] = repoEntry{secret: enc}
    err := p.HandleWebhook(context.Background(), "org-1",
        map[string]string{"Sentry-Hook-Signature": "bad"}, []byte(`{}`))
    if err == nil { t.Fatal("want HMAC error") }
}
```

- [ ] **Step 2: Implementation**

```go
package sentry

import (
    "context"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Repo interface {
    Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
    Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
    Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

type Provider struct {
    repo Repo
    kv   domain.KeyVault
    sink domain.IncidentSink
}

func New(repo Repo, kv domain.KeyVault, sink domain.IncidentSink) *Provider {
    return &Provider{repo: repo, kv: kv, sink: sink}
}

func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationSentry }

func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
    dsn, _ := cfg["dsn"].(string)
    secret, _ := cfg["webhook_secret"].(string)
    if secret == "" {
        return domain.Connection{}, errors.New("sentry: webhook_secret required")
    }
    enc, err := p.kv.Encrypt(ctx, []byte(secret))
    if err != nil { return domain.Connection{}, err }
    c := domain.Connection{
        Provider: domain.IntegrationSentry,
        Status:   domain.StatusConnected,
        Metadata: map[string]any{"dsn": dsn},
    }
    if err := p.repo.Upsert(ctx, princ.OrgID, c, enc); err != nil {
        return domain.Connection{}, err
    }
    return c, nil
}

func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
    return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationSentry)
}

func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
    c, _, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationSentry)
    return c, err
}

func (p *Provider) HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error {
    sig := headers["Sentry-Hook-Signature"]
    if sig == "" { return errors.New("sentry: missing Sentry-Hook-Signature") }

    _, enc, err := p.repo.Get(ctx, orgID, domain.IntegrationSentry)
    if err != nil || enc == nil {
        return fmt.Errorf("sentry: no connection for org=%s", orgID)
    }
    secret, err := p.kv.Decrypt(ctx, enc)
    if err != nil { return fmt.Errorf("sentry: decrypt: %w", err) }

    mac := hmac.New(sha256.New, secret)
    mac.Write(body)
    if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(sig)) {
        return errors.New("sentry: HMAC mismatch")
    }

    var evt struct {
        ID          string     `json:"id"`
        Level       string     `json:"level"`
        Title       string     `json:"title"`
        Environment string     `json:"environment"`
        Tags        [][]string `json:"tags"`
    }
    if err := json.Unmarshal(body, &evt); err != nil {
        return fmt.Errorf("sentry: decode: %w", err)
    }

    service := ""
    for _, t := range evt.Tags {
        if len(t) >= 2 && t[0] == "service" { service = t[1]; break }
    }
    var payload map[string]any
    _ = json.Unmarshal(body, &payload)

    return p.sink.Insert(ctx, orgID, domain.RawIncident{
        Source: "sentry", SourceEventID: evt.ID, Title: evt.Title, Level: evt.Level,
        Service: service, Environment: evt.Environment, Payload: payload,
    })
}
```

### Task 2.5: ArgoCD adapter (token-only)

**Files:**
- Create: `services/control-plane/internal/adapter/integration/argocd/provider.go`

- [ ] **Step 1: Minimal — Connect stores token, Disconnect deletes, no webhook**

```go
package argocd

import (
    "context"
    "errors"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Repo interface {
    Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
    Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
    Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

type Provider struct {
    repo Repo
    kv   domain.KeyVault
}

func New(repo Repo, kv domain.KeyVault) *Provider { return &Provider{repo: repo, kv: kv} }

func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationArgoCD }

func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
    serverURL, _ := cfg["server_url"].(string)
    token, _ := cfg["token"].(string)
    if serverURL == "" || token == "" {
        return domain.Connection{}, errors.New("argocd: server_url and token required")
    }
    enc, err := p.kv.Encrypt(ctx, []byte(token))
    if err != nil { return domain.Connection{}, err }
    c := domain.Connection{
        Provider: domain.IntegrationArgoCD,
        Status:   domain.StatusConnected,
        Metadata: map[string]any{"server_url": serverURL},
    }
    if err := p.repo.Upsert(ctx, princ.OrgID, c, enc); err != nil { return domain.Connection{}, err }
    return c, nil
}

func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
    return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationArgoCD)
}

func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
    c, _, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationArgoCD)
    return c, err
}

func (p *Provider) HandleWebhook(_ context.Context, _ string, _ map[string]string, _ []byte) error {
    return errors.New("argocd: no webhooks in Phase 3")
}
```

### Task 2.6: Factory + registry

**Files:**
- Create: `services/control-plane/internal/adapter/integration/factory.go`

- [ ] **Step 1: Registry**

```go
package integration

import (
    "context"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/argocd"
    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/github"
    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/sentry"
    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Registry struct {
    providers map[domain.IntegrationProvider]domain.Integration
    repo      *repo.IntegrationsRepo
}

type Deps struct {
    Repo                 *repo.IntegrationsRepo
    KV                   domain.KeyVault
    IncidentSink         domain.IncidentSink
    GitHubDefaultSecret  []byte
}

func NewRegistry(d Deps) *Registry {
    return &Registry{
        providers: map[domain.IntegrationProvider]domain.Integration{
            domain.IntegrationGitHub: github.New(d.Repo, d.KV, d.GitHubDefaultSecret),
            domain.IntegrationSentry: sentry.New(d.Repo, d.KV, d.IncidentSink),
            domain.IntegrationArgoCD: argocd.New(d.Repo, d.KV),
        },
        repo: d.Repo,
    }
}

func (r *Registry) Get(p domain.IntegrationProvider) (domain.Integration, bool) {
    v, ok := r.providers[p]
    return v, ok
}

func (r *Registry) List(ctx context.Context, princ domain.Principal) ([]domain.Connection, error) {
    return r.repo.List(ctx, princ.OrgID)
}
```

### Task 2.7: Wire into main + Deps + commit

**Files:**
- Modify: `services/control-plane/cmd/server/main.go`
- Modify: `services/control-plane/internal/transport/http/server.go`

- [ ] **Step 1: Extend Deps**

```go
type Deps struct {
    Pool         *pgxpool.Pool
    AppPool      *pgxpool.Pool
    Auth         domain.AuthProvider
    Audit        domain.AuditWriter
    Integrations *integration.Registry
}
```

- [ ] **Step 2: Construct in main.go**

```go
intRepo := repo.NewIntegrationsRepo(pool)
incRepo := repo.NewIncidentsRepo(pool)
registry := integration.NewRegistry(integration.Deps{
    Repo: intRepo, KV: kv, IncidentSink: incRepo,
    GitHubDefaultSecret: []byte(cfg.GitHubDefaultWebhookSecret),
})
```

Add `GitHubDefaultWebhookSecret` to Config, loaded from `GITHUB_WEBHOOK_SECRET` env with sensible dev default.

- [ ] **Step 3: Run + commit**

```bash
cd services/control-plane && make build && make test && make arch
git add . && git commit -m "feat(integration): port + github/sentry/argocd adapters + registry (stage 2)"
```

---

## Stage 3 — HTTP routes for integrations + webhooks

### Task 3.1: Handlers + DTOs

**Files:**
- Create: `services/control-plane/internal/transport/http/dto/integrations.go`
- Create: `services/control-plane/internal/transport/http/handler/integrations.go`
- Create: `services/control-plane/internal/transport/http/handler/webhooks.go`

- [ ] **Step 1: DTOs**

```go
package dto

type IntegrationResp struct {
    Provider       string         `json:"provider"`
    Status         string         `json:"status"`
    InstallationID string         `json:"installation_id,omitempty"`
    Metadata       map[string]any `json:"metadata,omitempty"`
    LastError      string         `json:"last_error,omitempty"`
    CreatedAt      string         `json:"created_at"`
    UpdatedAt      string         `json:"updated_at"`
}

type ConnectReq map[string]any
```

- [ ] **Step 2: Integration handlers**

```go
package handler

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
    appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
    "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func IntegrationsList(reg *integration.Registry) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        princ, _ := appmw.PrincipalFrom(r.Context())
        rows, err := reg.List(r.Context(), princ)
        w.Header().Set("Content-Type", "application/json")
        if err != nil { w.WriteHeader(500); _ = json.NewEncoder(w).Encode(dto.ErrorResp{Error: err.Error()}); return }
        out := make([]dto.IntegrationResp, 0, len(rows))
        for _, c := range rows { out = append(out, toResp(c)) }
        _ = json.NewEncoder(w).Encode(out)
    }
}

func IntegrationsConnect(reg *integration.Registry, audit domain.AuditWriter) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        provider := domain.IntegrationProvider(chi.URLParam(r, "provider"))
        p, ok := reg.Get(provider)
        if !ok { http.Error(w, `{"error":"unknown provider"}`, 400); return }
        var cfg map[string]any
        if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil { http.Error(w, `{"error":"bad json"}`, 400); return }
        princ, _ := appmw.PrincipalFrom(r.Context())
        c, err := p.Connect(r.Context(), princ, cfg)
        if err != nil { http.Error(w, `{"error":"`+err.Error()+`"}`, 400); return }
        _ = audit.Write(r.Context(), princ, "integration.connected", string(provider), map[string]any{"installation_id": c.InstallationID})
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(201)
        _ = json.NewEncoder(w).Encode(toResp(c))
    }
}

func IntegrationsDisconnect(reg *integration.Registry, audit domain.AuditWriter) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        provider := domain.IntegrationProvider(chi.URLParam(r, "provider"))
        p, ok := reg.Get(provider)
        if !ok { http.Error(w, `{"error":"unknown provider"}`, 400); return }
        princ, _ := appmw.PrincipalFrom(r.Context())
        if err := p.Disconnect(r.Context(), princ); err != nil { http.Error(w, `{"error":"`+err.Error()+`"}`, 500); return }
        _ = audit.Write(r.Context(), princ, "integration.disconnected", string(provider), nil)
        w.WriteHeader(204)
    }
}

func GitHubMockInstall(reg *integration.Registry, audit domain.AuditWriter, appBaseURL, appEnv string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if appEnv != "dev" { http.Error(w, "disabled in non-dev", 403); return }
        princ, _ := appmw.PrincipalFrom(r.Context())
        p, _ := reg.Get(domain.IntegrationGitHub)
        installID := "mock-" + princ.OrgID[:8]
        if _, err := p.Connect(r.Context(), princ, map[string]any{
            "installation_id": installID,
            "scopes":          []any{"repo", "metadata"},
        }); err != nil { http.Error(w, err.Error(), 500); return }
        _ = audit.Write(r.Context(), princ, "integration.connected", "github", map[string]any{"installation_id": installID, "via": "mock"})
        http.Redirect(w, r, appBaseURL+"/console/integrations?installed=github", http.StatusFound)
    }
}

func toResp(c domain.Connection) dto.IntegrationResp {
    return dto.IntegrationResp{
        Provider: string(c.Provider), Status: string(c.Status),
        InstallationID: c.InstallationID, Metadata: c.Metadata, LastError: c.LastError,
        CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
    }
}
```

- [ ] **Step 3: Webhook handler**

```go
package handler

import (
    "io"
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func Webhook(reg *integration.Registry) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        provider := domain.IntegrationProvider(chi.URLParam(r, "provider"))
        orgID := chi.URLParam(r, "org_id")
        p, ok := reg.Get(provider)
        if !ok { http.Error(w, "unknown provider", 404); return }
        body, _ := io.ReadAll(r.Body)
        defer r.Body.Close()
        hdrs := map[string]string{}
        for k := range r.Header { hdrs[k] = r.Header.Get(k) }
        if err := p.HandleWebhook(r.Context(), orgID, hdrs, body); err != nil {
            http.Error(w, err.Error(), 401)
            return
        }
        w.WriteHeader(200)
    }
}
```

- [ ] **Step 4: Mount routes in server.go**

In the **protected group** (`RequireAuth + RLS`):
```go
g.Get("/v1/integrations", handler.IntegrationsList(deps.Integrations))
g.Post("/v1/integrations/{provider}/connect", handler.IntegrationsConnect(deps.Integrations, deps.Audit))
g.Delete("/v1/integrations/{provider}", handler.IntegrationsDisconnect(deps.Integrations, deps.Audit))
g.Get("/v1/integrations/github/mock_install", handler.GitHubMockInstall(deps.Integrations, deps.Audit, cfg.AppBaseURL, cfg.AppEnv))
```

In the **public** routes (no auth — HMAC-only):
```go
r.Post("/v1/webhooks/{provider}/{org_id}", handler.Webhook(deps.Integrations))
```

The webhook receiver runs OUTSIDE the RLS tx because there's no Principal — the adapter inserts via `incidentsRepo.Insert(ctx, orgID, ...)` which uses `db.FromCtx(ctx, pool)` → pool fallback. INSERT into `incidents_raw` doesn't need RLS because RLS USING only blocks SELECT/UPDATE/DELETE; INSERTs flow through. To keep the integrity story clean, also explicitly `SET LOCAL` for the duration:

Update `webhooks.go` to wrap in a tx with `SET LOCAL app.current_org_id`:

```go
import (
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

func Webhook(reg *integration.Registry, pool *pgxpool.Pool) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        provider := domain.IntegrationProvider(chi.URLParam(r, "provider"))
        orgID := chi.URLParam(r, "org_id")
        p, ok := reg.Get(provider); if !ok { http.Error(w, "unknown provider", 404); return }
        body, _ := io.ReadAll(r.Body); defer r.Body.Close()
        hdrs := make(map[string]string, len(r.Header)); for k := range r.Header { hdrs[k] = r.Header.Get(k) }

        tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
        if err != nil { http.Error(w, "db", 500); return }
        defer tx.Rollback(r.Context())
        if _, err := tx.Exec(r.Context(), "SELECT set_config('app.current_org_id', $1, true)", orgID); err != nil {
            http.Error(w, "rls", 500); return
        }
        ctx := db.WithTx(r.Context(), tx)
        if err := p.HandleWebhook(ctx, orgID, hdrs, body); err != nil {
            http.Error(w, err.Error(), 401); return
        }
        _ = tx.Commit(r.Context())
        w.WriteHeader(200)
    }
}
```

Pass `deps.AppPool` (which has nexis_app + RLS) — actually for webhook ingestion the admin `deps.Pool` is fine because we're operating without a principal. Use `deps.Pool`.

- [ ] **Step 5: Run + commit**

```bash
cd services/control-plane && make build && make test && make arch
git add . && git commit -m "feat(http): integration + webhook routes (stage 3)"
```

---

## Stage 4 — Member invites

### Task 4.1: Invite domain + store

**Files:**
- Create: `services/control-plane/internal/domain/invite.go`
- Modify: `services/control-plane/internal/adapter/auth/local/store.go` (add invite ops to Store interface)
- Modify: `services/control-plane/internal/adapter/auth/local/{memstore,pgstore}.go`

- [ ] **Step 1: Invite types**

```go
package domain

import "time"

type Invite struct {
    TokenHash      []byte
    OrgID          string
    Email          string
    Role           Role
    InviterUserID  string
    ExpiresAt      time.Time
    ClaimedAt      *time.Time
}
```

- [ ] **Step 2: Extend Store interface**

```go
CreateInvite(ctx context.Context, i *domain.Invite) error
GetInvite(ctx context.Context, tokenHash []byte) (*domain.Invite, error)
ListInvites(ctx context.Context, orgID string) ([]domain.Invite, error)
MarkInviteClaimed(ctx context.Context, tokenHash []byte) error
DeleteInvite(ctx context.Context, tokenHash []byte) error
```

- [ ] **Step 3: Implement in memstore + pgstore**

memstore: simple map keyed by hex(token_hash).
pgstore: raw SQL against `org_invites`, using `db.FromCtx(ctx, s.pool)`.

### Task 4.2: Invite handlers

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/invites.go`
- Modify: `services/control-plane/internal/adapter/auth/local/provider.go` (add IssueInvite + ClaimInvite methods on Provider)

- [ ] **Step 1: Provider methods**

```go
func (p *Provider) IssueInvite(ctx context.Context, princ domain.Principal, email string, role domain.Role) (string, error) {
    // gen 32-byte random token, SHA-256 hash
    raw := make([]byte, 32)
    rand.Read(raw)
    token := base64.RawURLEncoding.EncodeToString(raw)
    h := sha256.Sum256([]byte(token))
    inv := &domain.Invite{
        TokenHash: h[:], OrgID: princ.OrgID, Email: email, Role: role,
        InviterUserID: princ.UserID, ExpiresAt: p.clock().Add(7 * 24 * time.Hour),
    }
    if err := p.store.CreateInvite(ctx, inv); err != nil { return "", err }
    if p.mailer != nil {
        link := p.baseURL + "/invites/" + token
        _ = p.mailer.SendMagicLink(ctx, email, link)
    }
    return token, nil
}

func (p *Provider) ClaimInvite(ctx context.Context, token, password string) (domain.SessionToken, error) {
    h := sha256.Sum256([]byte(token))
    inv, err := p.store.GetInvite(ctx, h[:])
    if err != nil { return domain.SessionToken{}, err }
    if inv.ClaimedAt != nil { return domain.SessionToken{}, errors.New("already claimed") }
    if p.clock().After(inv.ExpiresAt) { return domain.SessionToken{}, errors.New("expired") }

    // Create user (if email doesn't exist) + add to org_members + revoke invite
    existing, _ := p.store.GetUserByEmail(ctx, inv.Email)
    var userID string
    if existing != nil {
        userID = existing.ID
    } else {
        hash, _ := hashPassword(password)
        u := &domain.User{ID: uuid.NewString(), Email: inv.Email, PasswordHash: hash}
        if err := p.store.CreateUser(ctx, u); err != nil { return domain.SessionToken{}, err }
        userID = u.ID
    }
    if err := p.store.CreateMembership(ctx, inv.OrgID, userID, inv.Role); err != nil {
        return domain.SessionToken{}, err
    }
    if err := p.store.MarkInviteClaimed(ctx, h[:]); err != nil {
        return domain.SessionToken{}, err
    }
    // session
    s := &domain.Session{ID: uuid.NewString(), UserID: userID, OrgID: inv.OrgID, ExpiresAt: p.clock().Add(p.sessionTTL)}
    if err := p.store.CreateSession(ctx, s); err != nil { return domain.SessionToken{}, err }
    return p.signSession(s, inv.Role)
}
```

Add these methods to `domain.AuthProvider` interface; stub in workos to return ErrNotImplemented.

- [ ] **Step 2: HTTP handlers**

```go
package handler

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

type issueInviteReq struct {
    Email string `json:"email"`
    Role  string `json:"role"` // admin|member
}

func InviteIssue(auth domain.AuthProvider, audit domain.AuditWriter) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        princ, _ := appmw.PrincipalFrom(r.Context())
        var req issueInviteReq
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil { http.Error(w, "bad json", 400); return }
        role := domain.Role(req.Role)
        if role != domain.RoleAdmin && role != domain.RoleMember { http.Error(w, "bad role", 400); return }
        // OrgID from path must match principal
        if chi.URLParam(r, "id") != princ.OrgID { http.Error(w, "forbidden", 403); return }

        // We need access to the Provider's IssueInvite — cast carefully:
        type inviter interface {
            IssueInvite(ctx context.Context, p domain.Principal, email string, role domain.Role) (string, error)
        }
        inv, ok := auth.(inviter)
        if !ok { http.Error(w, "not supported by provider", 501); return }
        token, err := inv.IssueInvite(r.Context(), princ, req.Email, role)
        if err != nil { http.Error(w, err.Error(), 500); return }
        _ = audit.Write(r.Context(), princ, "invite.issued", req.Email, map[string]any{"role": req.Role})
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(201)
        _ = json.NewEncoder(w).Encode(map[string]any{"token_prefix": token[:8]})
    }
}

func InviteList(auth domain.AuthProvider) http.HandlerFunc { /* delegate to store via provider */ }
func InviteRevoke(auth domain.AuditWriter) http.HandlerFunc { /* DELETE /v1/orgs/:id/invites/:tokenHash */ }
func InviteGet(auth domain.AuthProvider) http.HandlerFunc { /* GET /v1/invites/:token — returns {org, role, inviter_email} */ }

type claimReq struct {
    Password string `json:"password"`
}

func InviteClaim(auth domain.AuthProvider, cfg config.Config) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        token := chi.URLParam(r, "token")
        var req claimReq
        _ = json.NewDecoder(r.Body).Decode(&req)
        type claimer interface {
            ClaimInvite(ctx context.Context, token, password string) (domain.SessionToken, error)
        }
        c, _ := auth.(claimer)
        st, err := c.ClaimInvite(r.Context(), token, req.Password)
        if err != nil { http.Error(w, err.Error(), 400); return }
        setSessionCookie(w, cfg, st)
        w.WriteHeader(200)
    }
}
```

To avoid the type-assertion gymnastics, the cleaner path is to **add `IssueInvite`, `ClaimInvite`, `ListInvites`, `GetInvite`, `RevokeInvite` to the `domain.AuthProvider` interface itself**. Stub them in `workos.Provider` to return `domain.ErrNotImplemented`. Then handlers call them directly.

- [ ] **Step 3: Mount + commit**

```go
g.Post("/v1/orgs/{id}/invites", appmw.RequireRole(domain.RoleOwner)(handler.InviteIssue(deps.Auth, deps.Audit)))
g.Get("/v1/orgs/{id}/invites", appmw.RequireRole(domain.RoleOwner, domain.RoleAdmin)(handler.InviteList(deps.Auth)))
g.Delete("/v1/orgs/{id}/invites/{token_hash}", appmw.RequireRole(domain.RoleOwner)(handler.InviteRevoke(deps.Auth)))

// public
r.Get("/v1/invites/{token}", handler.InviteGet(deps.Auth))
r.Post("/v1/invites/{token}/claim", handler.InviteClaim(deps.Auth, cfg))
```

(`RequireRole` lands in Stage 5; for Stage 4 use plain `RequireAuth` and add the role check inline. Re-wire in Stage 5.)

```bash
cd services/control-plane && make build && make test
git add . && git commit -m "feat(invites): issue+claim+list+revoke + email magic link (stage 4)"
```

---

## Stage 5 — RBAC middleware + audit list

### Task 5.1: RequireRole middleware

**Files:**
- Create: `services/control-plane/internal/transport/http/middleware/rbac.go`

- [ ] **Step 1: Implement**

```go
package middleware

import (
    "net/http"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func RequireRole(allowed ...domain.Role) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            princ, ok := PrincipalFrom(r.Context())
            if !ok { http.Error(w, `{"error":"unauthorized"}`, 401); return }
            for _, a := range allowed {
                if princ.Role == a { next.ServeHTTP(w, r); return }
            }
            http.Error(w, `{"error":"forbidden"}`, 403)
        })
    }
}
```

- [ ] **Step 2: Apply to existing endpoints**

In `server.go` protected group:
- `POST /v1/apikeys` → require owner|admin
- `POST /v1/integrations/*/connect` + `DELETE /v1/integrations/*` → owner|admin
- `POST /v1/orgs/{id}/invites` etc → owner only
- `GET /v1/audit*` → owner|admin

Use nested groups for clarity:

```go
g.Group(func(g2 chi.Router) {
    g2.Use(appmw.RequireRole(domain.RoleOwner))
    g2.Post("/v1/orgs/{id}/invites", handler.InviteIssue(...))
    g2.Delete("/v1/orgs/{id}/invites/{token_hash}", handler.InviteRevoke(...))
})
g.Group(func(g2 chi.Router) {
    g2.Use(appmw.RequireRole(domain.RoleOwner, domain.RoleAdmin))
    g2.Post("/v1/integrations/{provider}/connect", handler.IntegrationsConnect(...))
    g2.Delete("/v1/integrations/{provider}", handler.IntegrationsDisconnect(...))
    g2.Get("/v1/audit", handler.AuditList(...))
    g2.Get("/v1/audit.csv", handler.AuditCSV(...))
    g2.Post("/v1/apikeys", handler.APIKeyCreate(...))
})
```

### Task 5.2: Audit list endpoint

**Files:**
- Modify: `services/control-plane/internal/adapter/audit/hmac_writer.go` (add List)
- Modify: `services/control-plane/internal/transport/http/handler/audit.go` (add List + CSV)

- [ ] **Step 1: List method on the writer (it has the pool)**

```go
type AuditFilter struct {
    Since, Until *time.Time
    Actor        string
    Action       string
    Limit, Offset int
}

type AuditRow struct {
    ID, OrgID, Actor, Action, Target string
    Metadata  map[string]any
    CreatedAt time.Time
}

func (w *HMACWriter) List(ctx context.Context, orgID string, f AuditFilter) ([]AuditRow, int, error) {
    q := db.FromCtx(ctx, w.pool)
    // dynamic WHERE clause; bind args
    where := "WHERE org_id=$1"
    args := []any{orgID}
    idx := 2
    if f.Since != nil { where += fmt.Sprintf(" AND created_at >= $%d", idx); args = append(args, *f.Since); idx++ }
    if f.Until != nil { where += fmt.Sprintf(" AND created_at <= $%d", idx); args = append(args, *f.Until); idx++ }
    if f.Actor != "" { where += fmt.Sprintf(" AND actor = $%d", idx); args = append(args, f.Actor); idx++ }
    if f.Action != "" { where += fmt.Sprintf(" AND action = $%d", idx); args = append(args, f.Action); idx++ }
    var total int
    if err := q.QueryRow(ctx, "SELECT count(*) FROM audit_log "+where, args...).Scan(&total); err != nil {
        return nil, 0, err
    }
    if f.Limit == 0 { f.Limit = 50 }
    rowsQ := "SELECT id, org_id, actor, action, target, metadata, created_at FROM audit_log " + where +
             fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
    args = append(args, f.Limit, f.Offset)
    rows, err := q.Query(ctx, rowsQ, args...)
    if err != nil { return nil, 0, err }
    defer rows.Close()
    out := []AuditRow{}
    for rows.Next() {
        var r AuditRow
        var meta []byte
        if err := rows.Scan(&r.ID, &r.OrgID, &r.Actor, &r.Action, &r.Target, &meta, &r.CreatedAt); err != nil {
            return nil, 0, err
        }
        _ = json.Unmarshal(meta, &r.Metadata)
        out = append(out, r)
    }
    return out, total, nil
}
```

- [ ] **Step 2: Handlers**

```go
func AuditList(w *audit.HMACWriter) http.HandlerFunc {
    return func(rw http.ResponseWriter, r *http.Request) {
        princ, _ := appmw.PrincipalFrom(r.Context())
        f := parseFilter(r) // since/until from ?since=RFC3339, actor, action, limit, offset
        rows, total, err := w.List(r.Context(), princ.OrgID, f)
        if err != nil { http.Error(rw, err.Error(), 500); return }
        rw.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(rw).Encode(map[string]any{"rows": rows, "total": total})
    }
}

func AuditCSV(w *audit.HMACWriter) http.HandlerFunc {
    return func(rw http.ResponseWriter, r *http.Request) {
        princ, _ := appmw.PrincipalFrom(r.Context())
        f := parseFilter(r)
        f.Limit = 10_000
        rows, _, _ := w.List(r.Context(), princ.OrgID, f)
        rw.Header().Set("Content-Type", "text/csv")
        rw.Header().Set("Content-Disposition", `attachment; filename="audit.csv"`)
        cw := csv.NewWriter(rw)
        _ = cw.Write([]string{"id", "actor", "action", "target", "created_at"})
        for _, row := range rows {
            _ = cw.Write([]string{row.ID, row.Actor, row.Action, row.Target, row.CreatedAt.Format(time.RFC3339)})
        }
        cw.Flush()
    }
}
```

- [ ] **Step 3: Run + commit**

```bash
cd services/control-plane && make build && make test && make arch
git add . && git commit -m "feat(rbac+audit): RequireRole middleware + audit list/CSV (stage 5)"
```

---

## Stage 6 — Console shell scaffold

### Task 6.1: Route group + sidebar/topbar/inspector

**Files:**
- Create: `apps/web/app/(app)/console/layout.tsx`
- Create: `apps/web/components/console/Sidebar.tsx`
- Create: `apps/web/components/console/Topbar.tsx`
- Create: `apps/web/components/console/CommandPalette.tsx`
- Create: `apps/web/components/console/InspectorDrawer.tsx`
- Modify: `apps/web/app/(app)/dashboard/page.tsx` → redirect to `/console`

- [ ] **Step 1: Install deps**

```bash
cd apps/web && /opt/homebrew/bin/pnpm add cmdk @tanstack/react-table @radix-ui/react-popover @radix-ui/react-dropdown-menu
```

- [ ] **Step 2: Sidebar (240/64 with persisted state)**

```tsx
"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import {
  LayoutGrid, AlertCircle, CheckSquare, FileText,
  Boxes, Plug, PlaySquare, Settings, ChevronLeft, ChevronRight,
} from "lucide-react";
import { ThemeAwareLogo } from "@/components/ThemeAwareLogo";
import { cn } from "@/lib/utils";

const WORKSPACE = [
  { href: "/console", label: "Home", icon: LayoutGrid },
  { href: "/console/incidents", label: "Incidents", icon: AlertCircle },
  { href: "/console/approvals", label: "Approvals", icon: CheckSquare },
  { href: "/console/audit", label: "Audit", icon: FileText },
];
const PLATFORM = [
  { href: "/console/agents", label: "Agents", icon: Boxes },
  { href: "/console/integrations", label: "Integrations", icon: Plug },
  { href: "/console/live-demo", label: "Live Demo", icon: PlaySquare },
];

export function Sidebar() {
  const path = usePathname();
  const [collapsed, setCollapsed] = useState(false);
  useEffect(() => { setCollapsed(localStorage.getItem("nexis_sidebar_collapsed") === "1"); }, []);
  const toggle = () => {
    setCollapsed(c => { const n = !c; localStorage.setItem("nexis_sidebar_collapsed", n ? "1" : "0"); return n; });
  };

  return (
    <aside className={cn(
      "fixed inset-y-0 left-0 flex flex-col border-r border-[var(--color-border)] bg-[var(--color-card)] transition-all",
      collapsed ? "w-16" : "w-60",
    )}>
      <div className="h-14 flex items-center px-4 border-b border-[var(--color-border)]">
        {!collapsed && <ThemeAwareLogo width={96} height={26} />}
      </div>
      <nav className="flex-1 overflow-y-auto py-4 space-y-6">
        <Section title="Workspace" items={WORKSPACE} path={path} collapsed={collapsed} />
        <Section title="Platform"  items={PLATFORM}  path={path} collapsed={collapsed} />
      </nav>
      <Link href="/console/settings/profile" className={cn(
        "border-t border-[var(--color-border)] flex items-center gap-3 px-4 h-12 text-sm",
        path.startsWith("/console/settings") && "bg-[var(--color-muted)]",
      )}>
        <Settings className="h-4 w-4 shrink-0" />{!collapsed && "Settings"}
      </Link>
      <button onClick={toggle} className="border-t border-[var(--color-border)] h-10 text-xs flex items-center justify-center text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)]">
        {collapsed ? <ChevronRight className="h-4 w-4" /> : <><ChevronLeft className="h-4 w-4 mr-1" /> Collapse</>}
      </button>
    </aside>
  );
}

function Section({ title, items, path, collapsed }: any) {
  return (
    <div>
      {!collapsed && <p className="px-4 text-xs uppercase tracking-widest text-[var(--color-muted-foreground)] mb-2">{title}</p>}
      <ul>
        {items.map((it: any) => {
          const Icon = it.icon;
          const active = path === it.href || path.startsWith(it.href + "/");
          return (
            <li key={it.href}>
              <Link href={it.href} className={cn(
                "flex items-center gap-3 px-4 h-9 text-sm hover:bg-[var(--color-muted)]",
                active && "bg-[var(--color-muted)] text-[var(--color-foreground)] font-medium",
              )}>
                <Icon className="h-4 w-4 shrink-0" />{!collapsed && it.label}
              </Link>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
```

- [ ] **Step 3: Topbar (breadcrumb + cmdk trigger + theme toggle + user menu)**

```tsx
"use client";

import { usePathname } from "next/navigation";
import { Search } from "lucide-react";
import { ThemeToggle } from "@/components/ThemeToggle";
import { CommandPalette } from "@/components/console/CommandPalette";
import { useState } from "react";

export function Topbar() {
  const path = usePathname();
  const [open, setOpen] = useState(false);
  const crumbs = path.split("/").filter(Boolean);

  return (
    <header className="sticky top-0 z-30 h-14 flex items-center border-b border-[var(--color-border)] bg-[var(--color-background)] px-6">
      <nav aria-label="Breadcrumb" className="text-sm text-[var(--color-muted-foreground)] flex items-center gap-1">
        {crumbs.map((c, i) => (
          <span key={i} className="flex items-center gap-1">
            {i > 0 && <span>/</span>}
            <span className={i === crumbs.length - 1 ? "text-[var(--color-foreground)]" : ""}>{c}</span>
          </span>
        ))}
      </nav>
      <button onClick={() => setOpen(true)} className="ml-8 flex-1 max-w-md flex items-center gap-2 rounded-md border border-[var(--color-border)] px-3 h-9 text-sm text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)]">
        <Search className="h-4 w-4" /> Search incidents, agents, approvals…
        <kbd className="ml-auto text-xs">⌘K</kbd>
      </button>
      <div className="ml-auto flex items-center gap-2">
        <ThemeToggle />
      </div>
      <CommandPalette open={open} onOpenChange={setOpen} />
    </header>
  );
}
```

- [ ] **Step 4: CommandPalette (cmdk minimal — search routes + actions)**

```tsx
"use client";

import { Command } from "cmdk";
import { useRouter } from "next/navigation";
import { useEffect } from "react";

const ITEMS = [
  { label: "Go to Home", path: "/console" },
  { label: "Go to Incidents", path: "/console/incidents" },
  { label: "Go to Integrations", path: "/console/integrations" },
  { label: "Go to Audit", path: "/console/audit" },
  { label: "Go to Settings", path: "/console/settings/profile" },
];

export function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const router = useRouter();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); onOpenChange(true); }
      if (e.key === "Escape") onOpenChange(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onOpenChange]);

  return (
    <Command.Dialog open={open} onOpenChange={onOpenChange} className="fixed inset-0 z-50 grid place-items-start pt-32 bg-black/30 backdrop-blur-sm">
      <Command className="mx-auto w-[640px] max-w-[90vw] rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] shadow-xl">
        <Command.Input placeholder="Search…" className="w-full px-4 h-12 border-b border-[var(--color-border)] bg-transparent outline-none text-sm" />
        <Command.List className="max-h-80 overflow-y-auto py-2">
          <Command.Empty className="px-4 py-4 text-sm text-[var(--color-muted-foreground)]">No results.</Command.Empty>
          {ITEMS.map(it => (
            <Command.Item key={it.path} onSelect={() => { router.push(it.path); onOpenChange(false); }}
              className="px-4 py-2 text-sm cursor-pointer aria-selected:bg-[var(--color-muted)]">
              {it.label}
            </Command.Item>
          ))}
        </Command.List>
      </Command>
    </Command.Dialog>
  );
}
```

- [ ] **Step 5: InspectorDrawer (Radix Sheet)**

```tsx
"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";

export function InspectorDrawer({ open, onOpenChange, title, children }: {
  open: boolean; onOpenChange: (v: boolean) => void; title: string; children: React.ReactNode;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/30 z-40" />
        <Dialog.Content className="fixed inset-y-0 right-0 z-50 w-[480px] max-w-[90vw] bg-[var(--color-background)] border-l border-[var(--color-border)] shadow-xl flex flex-col">
          <div className="h-14 flex items-center justify-between px-6 border-b border-[var(--color-border)]">
            <Dialog.Title className="text-sm font-medium">{title}</Dialog.Title>
            <Dialog.Close className="p-1 hover:bg-[var(--color-muted)] rounded"><X className="h-4 w-4" /></Dialog.Close>
          </div>
          <div className="flex-1 overflow-y-auto p-6">{children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
```

- [ ] **Step 6: Console layout**

`apps/web/app/(app)/console/layout.tsx`:

```tsx
import type { ReactNode } from "react";
import { Sidebar } from "@/components/console/Sidebar";
import { Topbar } from "@/components/console/Topbar";

export default function ConsoleLayout({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen flex">
      <Sidebar />
      <div className="flex-1 ml-60 transition-all"
           // Note: collapsed state changes margin; for simplicity we accept the 240 default here.
           // Sidebar reads localStorage on its own; a real implementation would lift state up.
      >
        <Topbar />
        <main className="max-w-[1440px] mx-auto px-6 py-6">{children}</main>
      </div>
    </div>
  );
}
```

- [ ] **Step 7: Dashboard redirect**

Replace `apps/web/app/(app)/dashboard/page.tsx`:

```tsx
import { redirect } from "next/navigation";
export default function Dashboard() { redirect("/console"); }
```

Update `apps/web/proxy.ts` matcher to also include `/console/:path*`.

- [ ] **Step 8: Run + commit**

```bash
/opt/homebrew/bin/pnpm --filter @nexis/web typecheck
/opt/homebrew/bin/pnpm --filter @nexis/web build
git add . && git commit -m "feat(web): console shell (sidebar/topbar/cmdk/inspector) + dashboard→console redirect (stage 6)"
```

### Task 6.2: Placeholder pages

**Files:**
- Create: `apps/web/app/(app)/console/page.tsx` (Home)
- Create: `apps/web/app/(app)/console/incidents/page.tsx`
- Create: `apps/web/app/(app)/console/approvals/page.tsx`
- Create: `apps/web/app/(app)/console/agents/page.tsx`
- Create: `apps/web/app/(app)/console/live-demo/page.tsx`

- [ ] **Step 1: Home — greeting + 4 KPI cards + activity feed**

```tsx
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { KPICard } from "@/components/console/KPICard";

const API = process.env.API_URL_INTERNAL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export default async function Home() {
  const c = await cookies();
  const session = c.get("nexis_session"); if (!session) redirect("/sign-in");

  const [meR, auditR] = await Promise.all([
    fetch(`${API}/v1/me`, { headers: { cookie: `nexis_session=${session.value}` }, cache: "no-store" }),
    fetch(`${API}/v1/audit?limit=10`, { headers: { cookie: `nexis_session=${session.value}` }, cache: "no-store" }),
  ]);
  if (!meR.ok) redirect("/sign-in");
  const me = await meR.json();
  const audit = auditR.ok ? await auditR.json() : { rows: [] };

  const firstName = me.user.email.split("@")[0];
  const hour = new Date().getHours();
  const greeting = hour < 12 ? "Good morning" : hour < 18 ? "Good afternoon" : "Good evening";

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-3xl font-semibold">{greeting}, {firstName}</h1>
        <p className="text-sm text-[var(--color-muted-foreground)] mt-1">Welcome to the NEXIS console.</p>
      </div>
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <KPICard label="Open incidents" value="0" />
        <KPICard label="Awaiting approval" value="0" />
        <KPICard label="MTTR 7d" value="—" />
        <KPICard label="Patch acceptance" value="—" />
      </div>
      <section>
        <h2 className="font-medium mb-3">Recent activity</h2>
        <ul className="divide-y divide-[var(--color-border)] rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
          {audit.rows.length === 0 && <li className="px-4 py-6 text-sm text-[var(--color-muted-foreground)]">No activity yet.</li>}
          {audit.rows.map((row: any) => (
            <li key={row.ID} className="px-4 py-3 text-sm flex justify-between">
              <span><span className="font-mono text-xs text-[var(--color-muted-foreground)]">{row.Actor.slice(0,8)}</span>{" "}<span className="font-medium">{row.Action}</span> · {row.Target}</span>
              <span className="text-[var(--color-muted-foreground)]">{new Date(row.CreatedAt).toLocaleString()}</span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
```

`KPICard.tsx`:

```tsx
export function KPICard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">{label}</p>
      <p className="text-3xl font-semibold mt-2">{value}</p>
    </div>
  );
}
```

- [ ] **Step 2: 4 placeholder pages**

Each renders a heading + "Phase 4" / "Phase 5" / "Phase 6" badge + 1-paragraph description. Example for Incidents:

```tsx
export default function IncidentsPage() {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-semibold">Incidents</h1>
        <span className="rounded-full bg-[var(--color-accent)]/30 text-[var(--color-accent-foreground)] px-2 py-0.5 text-xs font-medium">Phase 4</span>
      </div>
      <p className="text-sm text-[var(--color-muted-foreground)] max-w-2xl">
        Real-time incident table with severity, service, owner, and agent timeline. Ships in Phase 4 along with the Temporal-backed recovery pipeline.
      </p>
    </div>
  );
}
```

Approvals → Phase 4. Agents → Phase 5. Live Demo → Phase 6.

- [ ] **Step 3: Run + commit**

```bash
/opt/homebrew/bin/pnpm --filter @nexis/web build
git add . && git commit -m "feat(web): home + 4 placeholder console pages (stage 6.2)"
```

---

## Stage 7 — Integrations surface (UI)

### Task 7.1: SDK

**Files:**
- Create: `apps/web/lib/integrations.ts`

```ts
const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type Integration = {
  provider: "github" | "sentry" | "argocd";
  status: "connected" | "pending" | "error" | "disconnected";
  installation_id?: string;
  metadata?: Record<string, unknown>;
  last_error?: string;
  created_at?: string;
  updated_at?: string;
};

export const integrations = {
  list: async (): Promise<Integration[]> => {
    const r = await fetch(`${API}/v1/integrations`, { credentials: "include" });
    if (!r.ok) throw new Error(r.statusText);
    return r.json();
  },
  connect: async (provider: string, config: Record<string, unknown>): Promise<Integration> => {
    const r = await fetch(`${API}/v1/integrations/${provider}/connect`, {
      method: "POST", credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(config),
    });
    if (!r.ok) throw new Error((await r.json()).error ?? r.statusText);
    return r.json();
  },
  disconnect: async (provider: string): Promise<void> => {
    await fetch(`${API}/v1/integrations/${provider}`, { method: "DELETE", credentials: "include" });
  },
  mockInstallGithub: () => {
    window.location.href = `${API}/v1/integrations/github/mock_install`;
  },
};
```

### Task 7.2: Card grid

**Files:**
- Create: `apps/web/app/(app)/console/integrations/page.tsx`
- Create: `apps/web/components/integrations/IntegrationCard.tsx`
- Create: `apps/web/components/integrations/{GitHubConfigureForm,SentryConfigureForm,ArgoCDConfigureForm}.tsx`

- [ ] **Step 1: Page (server component fetches initial list)**

```tsx
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { IntegrationsClient } from "./client";

const API = process.env.API_URL_INTERNAL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export default async function IntegrationsPage() {
  const c = await cookies();
  const session = c.get("nexis_session"); if (!session) redirect("/sign-in");
  const r = await fetch(`${API}/v1/integrations`, { headers: { cookie: `nexis_session=${session.value}` }, cache: "no-store" });
  const list = r.ok ? await r.json() : [];
  return <IntegrationsClient initial={list} />;
}
```

`client.tsx` is a client component that renders cards for 6 providers (3 wired, 3 "coming Phase 4") and opens a dialog with the provider-specific form.

- [ ] **Step 2: IntegrationCard**

```tsx
"use client";

import { CheckCircle, Circle, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/Button";

export function IntegrationCard({ name, description, status, onConfigure, comingSoon }: {
  name: string;
  description: string;
  status?: "connected" | "pending" | "error" | "disconnected";
  onConfigure: () => void;
  comingSoon?: boolean;
}) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
      <div className="flex items-start justify-between">
        <h3 className="font-medium">{name}</h3>
        {comingSoon ? (
          <span className="text-xs text-[var(--color-muted-foreground)]">Coming Phase 4</span>
        ) : (
          <StatusBadge status={status} />
        )}
      </div>
      <p className="text-sm text-[var(--color-muted-foreground)] mt-2">{description}</p>
      <div className="mt-4">
        <Button variant="outline" size="sm" onClick={onConfigure} disabled={comingSoon}>
          {status === "connected" ? "Manage" : "Configure"}
        </Button>
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status?: string }) {
  if (status === "connected") return <span className="flex items-center gap-1 text-xs text-[var(--color-success)]"><CheckCircle className="h-3 w-3" />Connected</span>;
  if (status === "error") return <span className="flex items-center gap-1 text-xs text-[var(--color-destructive)]"><AlertTriangle className="h-3 w-3" />Error</span>;
  return <span className="flex items-center gap-1 text-xs text-[var(--color-muted-foreground)]"><Circle className="h-3 w-3" />Not connected</span>;
}
```

- [ ] **Step 3: Configure forms (3 — GitHub, Sentry, ArgoCD)**

GitHubConfigureForm: shows the **Install via mock OAuth** button → calls `integrations.mockInstallGithub()` which navigates the browser to the control-plane `mock_install` endpoint. The endpoint sets the cookie context (already there) and 302s back. After return, the page revalidates and shows Connected.

SentryConfigureForm:
- DSN input
- Generated webhook secret (32-byte random, displayed once with "copy" button)
- Submit → `integrations.connect("sentry", {dsn, webhook_secret})`
- After success, show the webhook URL (`${API}/v1/webhooks/sentry/${orgId}`) and the secret with a "copy" button + "Now register this in Sentry → Settings → Webhooks" instruction.

ArgoCDConfigureForm:
- Server URL input
- Token input
- Submit → `integrations.connect("argocd", {server_url, token})`

- [ ] **Step 4: Commit**

```bash
/opt/homebrew/bin/pnpm --filter @nexis/web build
git add . && git commit -m "feat(web): integrations surface — card grid + configure dialogs (stage 7)"
```

---

## Stage 8 — Settings surface

### Task 8.1: Settings sub-layout

**Files:**
- Create: `apps/web/app/(app)/console/settings/layout.tsx`

```tsx
import Link from "next/link";
import type { ReactNode } from "react";

const NAV = [
  { href: "/console/settings/profile", label: "Profile" },
  { href: "/console/settings/organization", label: "Organization" },
  { href: "/console/settings/members", label: "Members & Roles" },
  { href: "/console/settings/api-keys", label: "API Keys" },
  { href: "/console/settings/preferences", label: "Preferences" },
];

export default function SettingsLayout({ children }: { children: ReactNode }) {
  return (
    <div className="flex gap-8">
      <nav className="w-[200px] shrink-0 space-y-1 text-sm">
        {NAV.map(n => (
          <Link key={n.href} href={n.href} className="block px-3 py-2 rounded hover:bg-[var(--color-muted)]">{n.label}</Link>
        ))}
      </nav>
      <div className="flex-1 max-w-2xl">{children}</div>
    </div>
  );
}
```

### Task 8.2: Sub-pages

**Files:**
- Create: `apps/web/app/(app)/console/settings/profile/page.tsx`
- Create: `apps/web/app/(app)/console/settings/organization/page.tsx`
- Create: `apps/web/app/(app)/console/settings/members/page.tsx`
- Create: `apps/web/app/(app)/console/settings/api-keys/page.tsx`
- Create: `apps/web/app/(app)/console/settings/preferences/page.tsx`

For **Profile**: show email (read-only), display name (read-only Phase 3).

For **Organization**: show name + slug (read-only Phase 3 — editable in Phase 4 when we add a PATCH endpoint).

For **Members & Roles**:
- Table of `org_members` (need a new `GET /v1/orgs/:id/members` endpoint — add to handler with same RBAC as audit).
- "Invite member" button → dialog with email + role select (admin|member) → calls `POST /v1/orgs/:id/invites`.
- Pending invites list with revoke buttons.

For **API Keys**: existing endpoints from Phase 2. Build full CRUD UI:
- Table of keys (id, prefix, name, scopes, created_at, last_used_at, revoked?).
- "Create" button → dialog → on submit show plaintext_once exactly once with copy-to-clipboard.
- Revoke button per row.

For **Preferences**: theme picker (light/dark/system) → calls `PATCH /v1/me/preferences {theme}`.

Each page client-side fetches via SDK extensions.

### Task 8.3: Theme preferences endpoint

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/preferences.go`
- Modify: `services/control-plane/internal/adapter/auth/local/{store,memstore,pgstore}.go` — add `GetUserPreferences` + `UpdateUserPreferences`
- Modify: `internal/domain/auth.go` — add `GetMe(ctx, p) (User, Org, Role, Preferences, error)` to AuthProvider

- [ ] **Step 1: Handler**

```go
func GetPreferences(auth domain.AuthProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        princ, _ := appmw.PrincipalFrom(r.Context())
        u, err := auth.GetUser(r.Context(), princ.UserID)
        if err != nil { http.Error(w, err.Error(), 500); return }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(u.Preferences)
    }
}

func PatchPreferences(auth domain.AuthProvider, audit domain.AuditWriter) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        princ, _ := appmw.PrincipalFrom(r.Context())
        var p map[string]any
        if err := json.NewDecoder(r.Body).Decode(&p); err != nil { http.Error(w, "bad json", 400); return }
        // simple whitelist
        if t, ok := p["theme"].(string); ok && t != "light" && t != "dark" && t != "system" {
            http.Error(w, `{"error":"invalid theme"}`, 400); return
        }
        type prefsUpdater interface {
            UpdateUserPreferences(ctx context.Context, userID string, prefs map[string]any) error
        }
        u, ok := auth.(prefsUpdater)
        if !ok { http.Error(w, "unsupported", 501); return }
        if err := u.UpdateUserPreferences(r.Context(), princ.UserID, p); err != nil { http.Error(w, err.Error(), 500); return }
        _ = audit.Write(r.Context(), princ, "user.preferences_updated", princ.UserID, p)
        w.WriteHeader(204)
    }
}
```

Mount in protected group at `/v1/me/preferences` (GET, PATCH).

Add `User.Preferences map[string]any` field to the domain struct + plumbing in store implementations.

### Task 8.4: SSR theme hydration

**Files:**
- Modify: `apps/web/app/layout.tsx`

- [ ] **Step 1: Read cookie + preferences on the server**

If we already have a session cookie, fetch `/v1/me/preferences` server-side and pass `theme` into the `ThemeProvider` as `defaultTheme`. This sets `<html class="dark">` correctly on first paint:

```tsx
import { cookies } from "next/headers";

export default async function RootLayout(...) {
  const c = await cookies();
  const session = c.get("nexis_session");
  let theme = "light";
  if (session) {
    try {
      const r = await fetch(`${API}/v1/me/preferences`, {
        headers: { cookie: `nexis_session=${session.value}` }, cache: "no-store",
      });
      if (r.ok) {
        const prefs = await r.json();
        if (prefs?.theme) theme = prefs.theme;
      }
    } catch {}
  }
  return (
    <html lang="en" className={`${inter.variable} ${jetBrainsMono.variable} h-full antialiased ${theme === "dark" ? "dark" : ""}`} suppressHydrationWarning>
      <body className="min-h-full flex flex-col font-sans">
        <ThemeProvider defaultTheme={theme}>
          {/* ... */}
        </ThemeProvider>
      </body>
    </html>
  );
}
```

The Preferences page calls `PATCH /v1/me/preferences {theme}` AND `setTheme(theme)` from `useTheme()` so the change is immediate.

### Task 8.5: Commit

```bash
git add . && git commit -m "feat(web): settings sub-routes + preferences endpoint + SSR theme hydration (stage 8)"
```

---

## Stage 9 — Audit surface

### Task 9.1: SDK + Page

**Files:**
- Create: `apps/web/lib/audit.ts`
- Create: `apps/web/app/(app)/console/audit/page.tsx` (server fetches initial page)
- Create: `apps/web/components/console/AuditTable.tsx` (client — uses TanStack Table)

- [ ] **Step 1: SDK**

```ts
export type AuditRow = {
  ID: string; Actor: string; Action: string; Target: string;
  Metadata?: Record<string, unknown>; CreatedAt: string;
};

export const audit = {
  list: async (q: { since?: string; until?: string; actor?: string; action?: string; limit?: number; offset?: number } = {}) => {
    const params = new URLSearchParams(Object.entries(q).filter(([, v]) => v != null).map(([k, v]) => [k, String(v)]));
    const r = await fetch(`${API}/v1/audit?${params}`, { credentials: "include" });
    return r.json() as Promise<{ rows: AuditRow[]; total: number }>;
  },
  csvUrl: (q: any) => {
    const params = new URLSearchParams(Object.entries(q).filter(([, v]) => v != null).map(([k, v]) => [k, String(v)]));
    return `${API}/v1/audit.csv?${params}`;
  },
};
```

- [ ] **Step 2: AuditTable (client component)**

TanStack Table headless setup with columns: Timestamp / Actor / Action / Target / View. Row click opens InspectorDrawer with the full JSON of the row (metadata + hash chain link).

Filter bar above table:
- Date range (two text inputs, type=datetime-local)
- Actor (text input — userID)
- Action (text input)
- "Apply" button refetches

"Export CSV" button → opens `audit.csvUrl(filters)` in a new tab.

- [ ] **Step 3: Commit**

```bash
git add . && git commit -m "feat(web): audit surface — TanStack table + filters + CSV export (stage 9)"
```

---

## Stage 10 — Invite claim flow + Members UI integration

### Task 10.1: Web claim page

**Files:**
- Create: `apps/web/app/invites/[token]/page.tsx`
- Modify: `apps/web/proxy.ts` (do NOT redirect `/invites/*`; it's public)
- Create: `apps/web/lib/invites.ts`

- [ ] **Step 1: SDK**

```ts
export type InviteInfo = { org: { id: string; name: string; slug: string }; role: string; inviter_email: string };

export const invites = {
  get: async (token: string): Promise<InviteInfo> => {
    const r = await fetch(`${API}/v1/invites/${token}`);
    if (!r.ok) throw new Error("invalid invite");
    return r.json();
  },
  claim: async (token: string, password: string): Promise<void> => {
    const r = await fetch(`${API}/v1/invites/${token}/claim`, {
      method: "POST", credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ password }),
    });
    if (!r.ok) throw new Error("claim failed");
  },
};
```

- [ ] **Step 2: Claim page (client component, public)**

Renders: org name + role + inviter email + password input + submit. On success → `router.push("/console")`.

- [ ] **Step 3: Proxy matcher carve-out**

`apps/web/proxy.ts` matcher already only matches `/console/:path*` (Stage 6), so `/invites/*` is naturally outside the proxy gate. Verify.

### Task 10.2: Members management UI

(already covered in Task 8.2 — verify it works against the live endpoints from Stage 4.)

### Task 10.3: Commit

```bash
git add . && git commit -m "feat(web): invite claim page (stage 10)"
```

---

## Stage 11 — Makefile, env, acceptance, DoD

### Task 11.1: Root Makefile + seed-sentry

**Files:**
- Create: `Makefile` (root — new)

```makefile
.PHONY: seed-sentry verify-audit help

ORG_ID ?=
SECRET ?= dev-sentry-webhook-secret-32
HOST   ?= http://localhost:8080

seed-sentry:
	@test -n "$(ORG_ID)" || (echo "ORG_ID required" && exit 1)
	@BODY='{"id":"e-$$(date +%s)","level":"error","title":"Test fault","environment":"prod","tags":[["service","api"]]}'; \
	SIG=$$(printf '%s' "$$BODY" | openssl dgst -sha256 -hmac "$(SECRET)" -hex | awk '{print $$2}'); \
	curl -sf -X POST "$(HOST)/v1/webhooks/sentry/$(ORG_ID)" \
	  -H "Content-Type: application/json" \
	  -H "Sentry-Hook-Signature: $$SIG" \
	  -d "$$BODY" && echo " ✓ posted"

help:
	@grep '^[a-z]' Makefile | awk -F: '{print $$1}' | sort -u
```

### Task 11.2: Acceptance E2E

**Files:**
- Modify: `apps/web/tests/e2e/auth.spec.ts` (or add new `apps/web/tests/e2e/console.spec.ts`)

- [ ] **Step 1: console.spec.ts**

```ts
import { test, expect } from "@playwright/test";

const email = `e2e-console+${Date.now()}@example.com`;

test("signup → console renders → mock GitHub install → connected", async ({ page }) => {
  await page.goto("/sign-up");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByLabel("Org name").fill(`Console ${Date.now()}`);
  await page.getByRole("button", { name: /sign up/i }).click();
  await expect(page).toHaveURL(/\/console$/);

  // Go to Integrations
  await page.goto("/console/integrations");
  await expect(page.getByText(/GitHub/)).toBeVisible();

  // Open GitHub Configure → mock install
  await page.getByRole("button", { name: /configure/i }).first().click();
  await page.getByRole("button", { name: /install/i }).click();
  // Browser is redirected through the control-plane and back; wait for connected badge
  await expect(page.getByText(/Connected/i)).toBeVisible({ timeout: 5_000 });
});

test("audit surface lists rows", async ({ page }) => {
  // Assumes the previous test logged in this browser context — sequential.
  await page.goto("/console/audit");
  await expect(page.getByText(/integration.connected/i)).toBeVisible();
});
```

- [ ] **Step 2: Run**

```bash
docker compose up --build -d
sleep 25
/opt/homebrew/bin/pnpm --filter @nexis/web exec playwright test --reporter=list
```

Expected: all tests pass.

### Task 11.3: Sentry acceptance

- [ ] **Step 1: Connect via UI** → grabs webhook secret → register in own form.

- [ ] **Step 2: Seed event**

```bash
ORG_ID=<from-db> SECRET=<webhook-secret> make seed-sentry
docker compose exec -T postgres psql -U nexis -d nexis \
  -c "SELECT count(*) FROM incidents_raw WHERE source='sentry';"
```

Expected: ≥ 1 within 5 s.

### Task 11.4: Mark Phase 3 complete

- [ ] **Step 1: PROJECT_PLAN.md**

```bash
# Find Phase 3 heading
grep -n 'Phase 3 — Console' docs/PROJECT_PLAN.md
```

Append ` — Completed 2026-05-12` (or current date) to the heading.

### Task 11.5: Final commit

```bash
git add Makefile docs/PROJECT_PLAN.md apps/web/tests
git commit -m "feat(test): playwright console e2e + sentry seed + phase 3 complete (stage 11)"
```

---

## Phase 3 Definition of Done

- [ ] All 8 console nav links route to a page that renders without error.
- [ ] GitHub mock install: card shows "Connected" after redirect.
- [ ] Sentry test event via `make seed-sentry`: row in `incidents_raw` within 5 s.
- [ ] ArgoCD token entry persists; status shows "Connected".
- [ ] Member invite end-to-end: owner POST → email in MailHog → claim page → new `org_members` row.
- [ ] RBAC: member user gets 403 on integration connect.
- [ ] Audit list with filters: GET `/v1/audit?action=integration.connected` returns only those rows; CSV export downloads.
- [ ] Theme persists: dark toggle in Preferences → reload → SSR sets `<html class="dark">`.
- [ ] All Go tests pass (incl. new keyvault, github, sentry, argocd adapter tests; integration tests against postgres).
- [ ] All web tests pass (vitest + playwright `console.spec.ts`).
- [ ] `pnpm --filter @nexis/web build` succeeds.
- [ ] `go-arch-lint check` still passes (new `internal/adapter/integration/**` covered by `adapter` rule).
- [ ] `docs/PROJECT_PLAN.md` Phase 3 marked complete.

---

## Risks captured

- Cmdk + Radix Dialog stacking — verify z-index doesn't fight the InspectorDrawer (cmdk has its own portal).
- Theme SSR hydration mismatch — `suppressHydrationWarning` on `<html>` + `defaultTheme` from server should be enough, but watch for FOUC during dev.
- `Sentry-Hook-Signature` header casing — Go `http.Header` is case-insensitive, but our adapter reads via map lookup; capture all common spellings (`X-Sentry-Hook-Signature` too).
- `org_invites` has no `id` column (token_hash is PK) — delete endpoint must accept the token_hash hex in the URL.
- Persisted sidebar collapse state needs to lift to layout so the main content margin updates. Phase 3 ships a simple version; iterate in Phase 4.
