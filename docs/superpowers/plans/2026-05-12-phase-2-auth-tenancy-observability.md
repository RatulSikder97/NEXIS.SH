# Phase 2 — Auth, Tenancy, Observability — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship signup → login → protected dashboard end-to-end on the local Docker stack with Postgres RLS, HMAC-chained audit log, and distributed traces in local Grafana (Tempo).

**Architecture:** Mirrors Phase 1's port/adapter pattern. Control-plane gets an `AuthProvider` domain port with a `local` adapter (bcrypt + magic-link + TOTP + API keys + JWT sessions) and a `workos` stub. RLS middleware opens a pgx tx per request and `SET LOCAL app.current_org_id`. Audit writer appends per-org HMAC chain. Web app gets `(auth)` and `(app)` route groups with Next middleware guarding the latter. Both runtimes export OTLP traces to the existing `otel-collector` in compose.

**Tech Stack:** Go 1.25 (control-plane), Next.js 16.2.2 + React 19 (web), Postgres 16 (pgvector image), Drizzle ORM (web schema), sqlc + pgx/v5 (control-plane), JWT-HS256 sessions, TOTP RFC 6238, OpenTelemetry SDK (Go + `@vercel/otel`), Playwright (E2E).

**Spec:** `docs/superpowers/specs/2026-05-12-phase-2-auth-tenancy-observability.md` — see it for non-goals, acceptance criteria, env vars, and HTTP surface table.

---

## Salvage / Reuse from Phase 1

- `services/control-plane/internal/domain/{errors,ports}.go` — extend with auth/audit types; do not rewrite.
- `services/control-plane/internal/platform/config/config.go` — extend with `AuthProvider`, `SessionSecret`, `AuditSecret`, `SMTPHost/Port`, `DatabaseURLApp`, `OTel*` envs.
- `services/control-plane/internal/transport/http/server.go` — add middlewares + new route groups; keep `/healthz`, `/v1/_diag/llm`.
- `packages/db/schema.ts` — extend, do not rewrite.
- `services/control-plane/migrations/` — add `0002_phase2_auth.up.sql` + `.down.sql` + `0003_rls.up.sql` + `.down.sql`. Leave existing migrations alone.
- `docker-compose.yml` — add new env vars to `control-plane` + `web`; introduce a `nexis_app` Postgres role via an init script.
- `apps/web/components/ui/Button.tsx` — already shadcn-canonical; reuse for auth forms.

---

## Stage 0 — Schema + RLS migration

### Task 0.1: Add new tables to Drizzle schema

**Files:**
- Modify: `packages/db/schema.ts`

- [ ] **Step 1: Add columns to `organizations` and `users`**

In `packages/db/schema.ts`, extend the existing tables:

```ts
export const organizations = pgTable("organizations", {
  id:           uuid("id").primaryKey().defaultRandom(),
  name:         text("name").notNull(),
  slug:         text("slug").notNull().unique(),
  ownerUserId:  uuid("owner_user_id"),
  createdAt:    timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const users = pgTable("users", {
  id:               uuid("id").primaryKey().defaultRandom(),
  email:            text("email").notNull().unique(),
  passwordHash:     text("password_hash"),
  mfaSecret:        text("mfa_secret"),
  mfaEnabled:       boolean("mfa_enabled").notNull().default(false),
  emailVerifiedAt:  timestamp("email_verified_at", { withTimezone: true }),
  createdAt:        timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});
```

Add the new imports for `boolean` to the top of the file.

- [ ] **Step 2: Add the 4 new tables**

Append to `packages/db/schema.ts`:

```ts
export const orgMembers = pgTable("org_members", {
  orgId:     uuid("org_id").notNull().references(() => organizations.id),
  userId:    uuid("user_id").notNull().references(() => users.id),
  role:      text("role", { enum: ["owner", "admin", "member"] }).notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  pk: primaryKey({ columns: [t.orgId, t.userId] }),
}));

export const sessions = pgTable("sessions", {
  id:        uuid("id").primaryKey().defaultRandom(),
  userId:    uuid("user_id").notNull().references(() => users.id),
  orgId:     uuid("org_id").notNull().references(() => organizations.id),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  expiresAt: timestamp("expires_at", { withTimezone: true }).notNull(),
  revokedAt: timestamp("revoked_at", { withTimezone: true }),
});

export const magicTokens = pgTable("magic_tokens", {
  tokenHash: customType<{ data: Buffer }>({ dataType: () => "bytea" })("token_hash").primaryKey(),
  userId:    uuid("user_id").notNull().references(() => users.id),
  purpose:   text("purpose", { enum: ["login", "verify_email", "reset_password"] }).notNull(),
  expiresAt: timestamp("expires_at", { withTimezone: true }).notNull(),
  usedAt:    timestamp("used_at", { withTimezone: true }),
});

export const apiKeys = pgTable("api_keys", {
  id:          uuid("id").primaryKey().defaultRandom(),
  orgId:       uuid("org_id").notNull().references(() => organizations.id),
  userId:      uuid("user_id").notNull().references(() => users.id),
  prefix:      text("prefix").notNull(),
  hash:        customType<{ data: Buffer }>({ dataType: () => "bytea" })("hash").notNull(),
  scopes:      text("scopes").array().notNull().default(sql`'{}'`),
  name:        text("name").notNull(),
  createdAt:   timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  lastUsedAt:  timestamp("last_used_at", { withTimezone: true }),
  revokedAt:   timestamp("revoked_at", { withTimezone: true }),
}, t => ({
  prefixIdx: index("api_keys_prefix_idx").on(t.prefix),
}));
```

Imports needed: `primaryKey`, `customType`, `boolean`, `sql` from `drizzle-orm` and `drizzle-orm/pg-core`.

- [ ] **Step 3: Extend `auditLog`**

Find the existing `auditLog` definition; add `prev_hash` + `row_hash` columns:

```ts
export const auditLog = pgTable("audit_log", {
  id:        uuid("id").primaryKey().defaultRandom(),
  orgId:     uuid("org_id"),
  actor:     text("actor"),
  action:    text("action").notNull(),
  target:    text("target"),
  metadata:  jsonb("metadata"),
  prevHash:  customType<{ data: Buffer }>({ dataType: () => "bytea" })("prev_hash"),
  rowHash:   customType<{ data: Buffer }>({ dataType: () => "bytea" })("row_hash").notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  orgCreatedIdx: index("audit_log_org_created_idx").on(t.orgId, t.createdAt),
}));
```

- [ ] **Step 4: Generate migration**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && \
/opt/homebrew/bin/pnpm exec drizzle-kit generate --name=phase2_auth
```

Inspect the new `packages/db/migrations/0001_*_phase2_auth.sql` — it should contain `ALTER TABLE`s + `CREATE TABLE`s for the 4 new tables.

### Task 0.2: Control-plane mirror migration

**Files:**
- Create: `services/control-plane/migrations/0002_phase2_auth.up.sql`
- Create: `services/control-plane/migrations/0002_phase2_auth.down.sql`

- [ ] **Step 1: Write up migration**

Mirror the Drizzle migration in raw SQL. Same column names + types (`bytea`, `uuid`, `text`, `text[]`, `timestamptz`, `boolean`, `jsonb`, indexes). Add `ALTER TABLE` statements for the new columns on `organizations`, `users`, `audit_log`.

- [ ] **Step 2: Write down migration**

Drop the 4 new tables + revert the column additions.

- [ ] **Step 3: Apply migration to running postgres**

```bash
docker compose up -d postgres && sleep 3
/opt/homebrew/bin/pnpm --filter @nexis/web exec drizzle-kit migrate
docker compose exec -T postgres psql -U nexis -d nexis -c "\dt"
docker compose exec -T postgres psql -U nexis -d nexis -c "\d users"
```

Expected: 8 tables visible (4 old + 4 new), `users` has `password_hash`, `mfa_secret`, `mfa_enabled`, `email_verified_at`.

### Task 0.3: Postgres `nexis_app` non-superuser role + init script

**Files:**
- Create: `infra/postgres/init/01-nexis-app-role.sql`
- Modify: `docker-compose.yml` (mount init dir on `postgres` service)

- [ ] **Step 1: Init SQL**

`infra/postgres/init/01-nexis-app-role.sql`:

```sql
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nexis_app') THEN
    CREATE ROLE nexis_app LOGIN PASSWORD 'nexis_app_dev_password';
  END IF;
END$$;

GRANT CONNECT ON DATABASE nexis TO nexis_app;
GRANT USAGE ON SCHEMA public TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO nexis_app;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO nexis_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO nexis_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT USAGE ON SEQUENCES TO nexis_app;
```

- [ ] **Step 2: Wire into compose**

In `docker-compose.yml`, on `postgres` service, add:

```yaml
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./infra/postgres/init:/docker-entrypoint-initdb.d:ro
```

Note: `/docker-entrypoint-initdb.d` only runs on a fresh volume. To apply against an existing volume:

```bash
docker compose cp infra/postgres/init/01-nexis-app-role.sql postgres:/tmp/
docker compose exec -T postgres psql -U nexis -d nexis -f /tmp/01-nexis-app-role.sql
```

- [ ] **Step 3: Verify role**

```bash
docker compose exec -T postgres psql -U nexis -d nexis -c "\du nexis_app"
```

Expected: role exists, no superuser/createrole/createdb attributes.

### Task 0.4: RLS migration

**Files:**
- Create: `services/control-plane/migrations/0003_rls.up.sql`
- Create: `services/control-plane/migrations/0003_rls.down.sql`

- [ ] **Step 1: Up migration enables RLS + policies on every tenant table**

```sql
-- Enable RLS on all tenant tables
ALTER TABLE org_members  ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions     ENABLE ROW LEVEL SECURITY;
ALTER TABLE magic_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys     ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log    ENABLE ROW LEVEL SECURITY;

-- magic_tokens has no org_id (pre-tenant) — use user_id-derived policy via org_members
CREATE POLICY tenant_isolation ON org_members
  USING (org_id = current_setting('app.current_org_id', true)::uuid);

CREATE POLICY tenant_isolation ON sessions
  USING (org_id = current_setting('app.current_org_id', true)::uuid);

CREATE POLICY user_isolation ON magic_tokens
  USING (user_id IN (
    SELECT user_id FROM org_members
    WHERE org_id = current_setting('app.current_org_id', true)::uuid
  ));

CREATE POLICY tenant_isolation ON api_keys
  USING (org_id = current_setting('app.current_org_id', true)::uuid);

CREATE POLICY tenant_isolation ON audit_log
  USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- organizations/users are NOT RLS-protected — fetched pre-session
-- (signup must create org row before SET LOCAL is possible)
```

- [ ] **Step 2: Down migration**

```sql
DROP POLICY IF EXISTS tenant_isolation ON org_members;
DROP POLICY IF EXISTS tenant_isolation ON sessions;
DROP POLICY IF EXISTS user_isolation ON magic_tokens;
DROP POLICY IF EXISTS tenant_isolation ON api_keys;
DROP POLICY IF EXISTS tenant_isolation ON audit_log;

ALTER TABLE org_members  DISABLE ROW LEVEL SECURITY;
ALTER TABLE sessions     DISABLE ROW LEVEL SECURITY;
ALTER TABLE magic_tokens DISABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys     DISABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log    DISABLE ROW LEVEL SECURITY;
```

- [ ] **Step 3: Apply**

```bash
docker compose exec -T postgres psql -U nexis -d nexis \
  -f - < services/control-plane/migrations/0003_rls.up.sql
docker compose exec -T postgres psql -U nexis -d nexis \
  -c "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename;
      SELECT tablename, rowsecurity FROM pg_tables WHERE schemaname='public' AND rowsecurity=true;"
```

Expected: rowsecurity=true on 5 tables.

### Task 0.5: Commit Stage 0

```bash
git add packages/db apps/web services/control-plane/migrations infra/postgres docker-compose.yml
git commit -m "feat(db): phase 2 schema + nexis_app role + RLS policies (stage 0)"
```

---

## Stage 1 — AuthProvider port + local adapter (TDD)

### Task 1.1: Domain types — `AuthProvider`, `Principal`, `Session`, `User`, `Org`

**Files:**
- Create: `services/control-plane/internal/domain/auth.go`
- Modify: `services/control-plane/internal/domain/errors.go`

- [ ] **Step 1: Auth types + port**

`services/control-plane/internal/domain/auth.go`:

```go
package domain

import (
	"context"
	"time"
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type User struct {
	ID              string
	Email           string
	PasswordHash    string
	MFASecret       string
	MFAEnabled      bool
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
}

type Organization struct {
	ID          string
	Name        string
	Slug        string
	OwnerUserID string
	CreatedAt   time.Time
}

type Session struct {
	ID        string
	UserID    string
	OrgID     string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Principal is the resolved actor for a request — populated by auth middleware,
// read by handlers, used for RLS SET LOCAL and audit attribution.
type Principal struct {
	UserID    string
	OrgID     string
	Role      Role
	SessionID string
}

// SignupInput / LoginInput / ... narrow value types
type SignupInput struct {
	Email    string
	Password string
	OrgName  string
}

type SignupResult struct {
	User    User
	Org     Organization
	Session SessionToken
}

type LoginInput struct {
	Email    string
	Password string
	MFACode  string // optional; required if user has MFA enabled
}

type SessionToken struct {
	Token     string // signed JWT — opaque to caller
	ExpiresAt time.Time
}

type APIKey struct {
	ID         string
	OrgID      string
	UserID     string
	Prefix     string
	Name       string
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

type APIKeyCreated struct {
	Key       APIKey
	Plaintext string // shown once at creation
}

// AuthProvider is the single port the auth usecases depend on. Adapters in
// internal/adapter/auth/* (local, workos) implement it.
type AuthProvider interface {
	Name() string

	Signup(ctx context.Context, in SignupInput) (SignupResult, error)
	Login(ctx context.Context, in LoginInput) (SessionToken, error)

	// VerifyToken parses + validates a session token (JWT) and returns the
	// Principal. Includes a server-side revocation check (sessions table).
	VerifyToken(ctx context.Context, token string) (Principal, error)

	// Logout marks the session revoked. The token continues to verify
	// cryptographically until expiry but VerifyToken will reject it.
	Logout(ctx context.Context, sessionID string) error

	IssueMagicLink(ctx context.Context, email, purpose string) error
	ConsumeMagicLink(ctx context.Context, token string) (SessionToken, error)

	EnrollMFA(ctx context.Context, userID string) (qrPNG []byte, secret string, err error)
	VerifyMFA(ctx context.Context, userID, code string) error
	DisableMFA(ctx context.Context, userID string) error

	CreateAPIKey(ctx context.Context, p Principal, name string, scopes []string) (APIKeyCreated, error)
	ListAPIKeys(ctx context.Context, p Principal) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, p Principal, id string) error

	// VerifyAPIKey is called by middleware when the request carries
	// "Authorization: Bearer nx_live_...". Returns a Principal on success.
	VerifyAPIKey(ctx context.Context, key string) (Principal, error)
}
```

- [ ] **Step 2: Extend `domain/errors.go`**

Append:

```go
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrMFARequired        = errors.New("mfa required")
	ErrMFAInvalid         = errors.New("mfa invalid")
	ErrSessionRevoked     = errors.New("session revoked")
	ErrSessionExpired     = errors.New("session expired")
	ErrNotImplemented     = errors.New("not implemented")
)
```

- [ ] **Step 3: Build**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && go build ./...
```

Expected: zero errors.

### Task 1.2: Local provider — password + signup + login (TDD)

**Files:**
- Create: `services/control-plane/internal/adapter/auth/local/store.go` (DB ops)
- Create: `services/control-plane/internal/adapter/auth/local/password.go`
- Create: `services/control-plane/internal/adapter/auth/local/provider.go`
- Create: `services/control-plane/internal/adapter/auth/local/provider_test.go`

- [ ] **Step 1: Write failing test** — table-driven, uses an in-memory store fake

`provider_test.go`:

```go
package local

import (
	"context"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func TestProvider_Signup_CreatesOrgAndOwnerMembership(t *testing.T) {
	p, store := newTestProvider(t)
	res, err := p.Signup(context.Background(), domain.SignupInput{
		Email: "alice@example.com", Password: "correct horse battery staple",
		OrgName: "Alice Co",
	})
	if err != nil { t.Fatalf("Signup: %v", err) }
	if res.User.Email != "alice@example.com" { t.Errorf("email = %q", res.User.Email) }
	if res.Org.Name != "Alice Co" { t.Errorf("org name = %q", res.Org.Name) }
	if res.Session.Token == "" { t.Error("empty session token") }
	got, _ := store.getMembership(res.Org.ID, res.User.ID)
	if got != domain.RoleOwner { t.Errorf("role = %q", got) }
}

func TestProvider_Login_RejectsWrongPassword(t *testing.T) {
	p, _ := newTestProvider(t)
	_, _ = p.Signup(context.Background(), domain.SignupInput{
		Email: "a@b.com", Password: "right", OrgName: "X",
	})
	_, err := p.Login(context.Background(), domain.LoginInput{Email: "a@b.com", Password: "wrong"})
	if err == nil || !errorsIs(err, domain.ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}
}

func TestProvider_VerifyToken_RoundTrips(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(context.Background(), domain.SignupInput{
		Email: "a@b.com", Password: "x", OrgName: "X",
	})
	princ, err := p.VerifyToken(context.Background(), res.Session.Token)
	if err != nil { t.Fatalf("VerifyToken: %v", err) }
	if princ.UserID != res.User.ID { t.Errorf("userID mismatch") }
	if princ.OrgID != res.Org.ID { t.Errorf("orgID mismatch") }
}

func TestProvider_VerifyToken_RejectsRevoked(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	princ, _ := p.VerifyToken(context.Background(), res.Session.Token)
	_ = p.Logout(context.Background(), princ.SessionID)
	_, err := p.VerifyToken(context.Background(), res.Session.Token)
	if err == nil || !errorsIs(err, domain.ErrSessionRevoked) {
		t.Fatalf("want ErrSessionRevoked, got %v", err)
	}
}

// newTestProvider returns Provider + the in-memory store backing it.
func newTestProvider(t *testing.T) (*Provider, *memStore) { /* defined in store.go */ }

func errorsIs(err, target error) bool { return err != nil && (err == target || (err.Error() == target.Error())) /* simplified */ }
```

- [ ] **Step 2: In-memory store + helpers (`store.go`)**

Define a `Store` interface used by `Provider`, then a `memStore` implementation used by tests. The real production store is the pgx-backed one wired in Task 1.6. Methods needed for this task:

```go
type Store interface {
	CreateOrganization(ctx context.Context, o *domain.Organization) error
	CreateUser(ctx context.Context, u *domain.User) error
	CreateMembership(ctx context.Context, orgID, userID string, role domain.Role) error
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	GetUser(ctx context.Context, id string) (*domain.User, error)
	GetMembership(ctx context.Context, userID string) (orgID string, role domain.Role, err error)
	CreateSession(ctx context.Context, s *domain.Session) error
	GetSession(ctx context.Context, id string) (*domain.Session, error)
	RevokeSession(ctx context.Context, id string) error
}
```

`memStore` keeps a map per table, returns deep copies, uses a sync.RWMutex.

- [ ] **Step 3: `password.go`**

```go
package local

import "golang.org/x/crypto/bcrypt"

const bcryptCost = 12

func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	return string(b), err
}

func verifyPassword(hash, pw string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
}
```

- [ ] **Step 4: `provider.go` — Signup, Login, VerifyToken, Logout**

`Provider` carries: `store Store`, `sessionSecret []byte`, `clock func() time.Time`, `sessionTTL time.Duration` (default 7d).

Signup transaction:
1. Hash password.
2. Generate slug from `OrgName` (kebab-case, fallback to short uuid).
3. CreateUser → CreateOrganization (set `owner_user_id` = user.id) → CreateMembership(role=owner) → CreateSession.
4. Sign JWT (`HS256`, claims `{sid, uid, oid, role, iat, exp}`).
5. Return `SignupResult{User, Org, SessionToken}`.

Login:
1. GetUserByEmail.
2. verifyPassword.
3. If `MFAEnabled` and `MFACode` empty → return `ErrMFARequired`.
4. If `MFAEnabled` → call `verifyTOTP(user.MFASecret, MFACode)` (Task 1.4 stub returns nil for now; will be filled in 1.4).
5. GetMembership → CreateSession → sign JWT.

VerifyToken:
1. Parse + validate JWT (signature + exp).
2. GetSession by `sid`.
3. If `RevokedAt != nil` → `ErrSessionRevoked`.
4. If `time.Now > ExpiresAt` → `ErrSessionExpired`.
5. Return `Principal{UserID, OrgID, Role: <from membership>, SessionID}`.

Logout:
1. RevokeSession(id).

Add `github.com/golang-jwt/jwt/v5` to `go.mod` (already present from cb1a74b) — verify via `grep`.

- [ ] **Step 5: Run tests, verify pass**

```bash
cd services/control-plane && go test -race ./internal/adapter/auth/local/... -v
```

Expected: 4/4 pass.

- [ ] **Step 6: Commit**

```bash
git add services/control-plane/internal/{domain,adapter/auth/local}
git commit -m "feat(auth): domain port + local adapter signup/login (stage 1.1-1.2)"
```

### Task 1.3: Magic-link (TDD)

**Files:**
- Modify: `services/control-plane/internal/adapter/auth/local/provider.go` (add IssueMagicLink, ConsumeMagicLink)
- Create: `services/control-plane/internal/adapter/auth/local/mailer.go` (interface + dev SMTP impl)
- Modify: `services/control-plane/internal/adapter/auth/local/store.go` (add magic token ops)
- Add tests in `provider_test.go`

- [ ] **Step 1: Failing test**

```go
func TestProvider_MagicLink_RoundTrip(t *testing.T) {
	p, store := newTestProvider(t)
	res, _ := p.Signup(context.Background(), domain.SignupInput{
		Email: "a@b.com", Password: "x", OrgName: "X",
	})
	if err := p.IssueMagicLink(context.Background(), "a@b.com", "login"); err != nil {
		t.Fatalf("issue: %v", err)
	}
	token := store.lastMagicToken() // dev mailer captures the plaintext token
	st, err := p.ConsumeMagicLink(context.Background(), token)
	if err != nil { t.Fatalf("consume: %v", err) }
	princ, _ := p.VerifyToken(context.Background(), st.Token)
	if princ.UserID != res.User.ID { t.Errorf("userID mismatch") }
}

func TestProvider_MagicLink_RejectsReuse(t *testing.T) {
	p, store := newTestProvider(t)
	_, _ = p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	_ = p.IssueMagicLink(context.Background(), "a@b.com", "login")
	token := store.lastMagicToken()
	_, _ = p.ConsumeMagicLink(context.Background(), token)
	_, err := p.ConsumeMagicLink(context.Background(), token)
	if err == nil { t.Fatal("want error on reuse, got nil") }
}
```

- [ ] **Step 2: `Mailer` interface + dev impl**

`mailer.go`:

```go
package local

import (
	"context"
	"fmt"
	"net/smtp"
)

type Mailer interface {
	SendMagicLink(ctx context.Context, to, link string) error
}

type SMTPMailer struct {
	Host, Port, From string
}

func (m *SMTPMailer) SendMagicLink(_ context.Context, to, link string) error {
	body := fmt.Sprintf("Subject: Your NEXIS magic link\r\nFrom: %s\r\nTo: %s\r\n\r\nClick: %s\r\n",
		m.From, to, link)
	return smtp.SendMail(m.Host+":"+m.Port, nil, m.From, []string{to}, []byte(body))
}

// testMailer records the last link instead of sending.
type testMailer struct{ lastLink string }
func (m *testMailer) SendMagicLink(_ context.Context, _, link string) error { m.lastLink = link; return nil }
```

- [ ] **Step 3: Implement `IssueMagicLink` + `ConsumeMagicLink`**

IssueMagicLink:
1. GetUserByEmail. If not found, return nil (don't leak existence).
2. Generate 32-byte random token. Compute SHA-256(token).
3. `store.CreateMagicToken({hash, userID, purpose, expires=now+15m})`.
4. `mailer.SendMagicLink(email, baseURL + "/auth/verify?token=" + base64url(token))`.

ConsumeMagicLink:
1. Compute SHA-256 of input.
2. `store.GetMagicToken(hash)`.
3. If `nil` or `expired` or `used_at != nil` → return error.
4. `store.MarkMagicTokenUsed(hash)`.
5. Look up user → membership → create session → return JWT.

- [ ] **Step 4: Run tests, commit**

```bash
go test -race ./internal/adapter/auth/local/... -v
git add . && git commit -m "feat(auth): magic-link issue+consume (stage 1.3)"
```

### Task 1.4: TOTP MFA (TDD)

**Files:**
- Create: `services/control-plane/internal/adapter/auth/local/totp.go`
- Modify: `provider.go` (wire EnrollMFA, VerifyMFA, DisableMFA, real verifyTOTP)
- Tests in `provider_test.go`

- [ ] **Step 1: Add `github.com/pquerna/otp/totp` to go.mod**

```bash
cd services/control-plane && go get github.com/pquerna/otp/totp && go mod tidy
```

- [ ] **Step 2: Failing tests**

```go
func TestProvider_MFA_EnrollAndVerify(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	qr, secret, err := p.EnrollMFA(context.Background(), res.User.ID)
	if err != nil { t.Fatal(err) }
	if len(qr) == 0 { t.Error("empty QR") }
	if secret == "" { t.Error("empty secret") }
	code, _ := totp.GenerateCode(secret, time.Now())
	if err := p.VerifyMFA(context.Background(), res.User.ID, code); err != nil {
		t.Fatalf("verify: %v", err)
	}
	u, _ := p.store.GetUser(context.Background(), res.User.ID)
	if !u.MFAEnabled { t.Error("MFA not enabled after verify") }
}

func TestProvider_Login_RequiresMFAWhenEnabled(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(...)
	_, secret, _ := p.EnrollMFA(...)
	code, _ := totp.GenerateCode(secret, time.Now())
	_ = p.VerifyMFA(..., code)

	_, err := p.Login(context.Background(), domain.LoginInput{Email: "a@b.com", Password: "x"})
	if !errorsIs(err, domain.ErrMFARequired) { t.Fatalf("want ErrMFARequired, got %v", err) }

	code2, _ := totp.GenerateCode(secret, time.Now())
	_, err = p.Login(context.Background(), domain.LoginInput{Email: "a@b.com", Password: "x", MFACode: code2})
	if err != nil { t.Fatalf("login with MFA: %v", err) }
}
```

- [ ] **Step 3: `totp.go`**

```go
package local

import (
	"bytes"
	"image/png"

	"github.com/pquerna/otp/totp"
)

func generateMFASecret(account string) (secret string, qrPNG []byte, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "NEXIS", AccountName: account})
	if err != nil { return "", nil, err }
	img, err := key.Image(200, 200)
	if err != nil { return "", nil, err }
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil { return "", nil, err }
	return key.Secret(), buf.Bytes(), nil
}

func verifyTOTP(secret, code string) bool { return totp.Validate(code, secret) }
```

- [ ] **Step 4: Wire in `provider.go`**

EnrollMFA: GetUser → generateMFASecret → store.UpdateUserMFASecret (but `mfa_enabled=false`) → return qr,secret.
VerifyMFA: GetUser → verifyTOTP(user.MFASecret, code). If valid → UpdateUserMFAEnabled(true).
DisableMFA: store.ClearUserMFA.
Update Login to call verifyTOTP when `MFAEnabled`.

- [ ] **Step 5: Run tests + commit**

```bash
go test -race ./internal/adapter/auth/local/... -v
git add . && git commit -m "feat(auth): TOTP MFA enroll+verify (stage 1.4)"
```

### Task 1.5: API keys (TDD)

**Files:**
- Create: `services/control-plane/internal/adapter/auth/local/apikey.go`
- Modify: `provider.go` (CreateAPIKey, ListAPIKeys, RevokeAPIKey, VerifyAPIKey)
- Modify: `store.go` (api_keys ops)
- Tests in `provider_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestProvider_APIKey_CreateAndVerify(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(...)
	princ := domain.Principal{UserID: res.User.ID, OrgID: res.Org.ID, Role: domain.RoleOwner}
	c, err := p.CreateAPIKey(context.Background(), princ, "ci-token", []string{"read"})
	if err != nil { t.Fatal(err) }
	if !strings.HasPrefix(c.Plaintext, "nx_live_") { t.Errorf("prefix: %q", c.Plaintext) }
	if c.Key.Prefix != c.Plaintext[:8] { t.Errorf("stored prefix mismatch") }

	got, err := p.VerifyAPIKey(context.Background(), c.Plaintext)
	if err != nil { t.Fatal(err) }
	if got.OrgID != res.Org.ID { t.Errorf("orgID mismatch") }
}

func TestProvider_APIKey_RevokeRejects(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(...)
	princ := domain.Principal{UserID: res.User.ID, OrgID: res.Org.ID, Role: domain.RoleOwner}
	c, _ := p.CreateAPIKey(context.Background(), princ, "x", nil)
	_ = p.RevokeAPIKey(context.Background(), princ, c.Key.ID)
	_, err := p.VerifyAPIKey(context.Background(), c.Plaintext)
	if err == nil { t.Fatal("want error on revoked key") }
}
```

- [ ] **Step 2: `apikey.go`**

```go
package local

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

const apiKeyPrefix = "nx_live_"

func generateAPIKey() (plaintext, prefix string, hash []byte, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil { return }
	tail := base64.RawURLEncoding.EncodeToString(b) // 32 chars
	plaintext = apiKeyPrefix + tail
	prefix = plaintext[:8] // "nx_live_"
	h := sha256.Sum256([]byte(plaintext))
	hash = h[:]
	return
}

func hashAPIKey(plaintext string) []byte {
	h := sha256.Sum256([]byte(plaintext))
	return h[:]
}
```

- [ ] **Step 3: Wire `provider.go`**

CreateAPIKey: generateAPIKey → store.CreateAPIKey(hash, prefix, name, scopes, principal) → return `APIKeyCreated{Key, Plaintext}`. Plaintext never re-derivable.

VerifyAPIKey: hashAPIKey(input) → store.GetAPIKeyByHash(hash) → check `revoked_at == nil` → return Principal (look up membership for org).

- [ ] **Step 4: Run + commit**

```bash
go test -race ./internal/adapter/auth/local/... -v
git add . && git commit -m "feat(auth): api keys create/verify/revoke (stage 1.5)"
```

### Task 1.6: Real pgx-backed Store

**Files:**
- Create: `services/control-plane/internal/adapter/auth/local/pgstore.go`
- Create: `services/control-plane/internal/adapter/auth/local/pgstore_test.go` (integration test against a real postgres)
- Modify: `internal/platform/db/db.go` (NEW: pgx pool init helper)

- [ ] **Step 1: `internal/platform/db/db.go`**

```go
package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func New(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil { return nil, err }
	cfg.MaxConns = 20
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	return pgxpool.NewWithConfig(ctx, cfg)
}
```

Update `.arch.yaml` if `platform/db` is not yet covered (it should be by `internal/platform/**`).

- [ ] **Step 2: `pgstore.go`**

Implement every method on the `Store` interface against `*pgxpool.Pool`. Each method:
- Wraps SELECTs as `pool.QueryRow(ctx, sql, args...).Scan(...)`.
- Wraps INSERTs as `pool.Exec(ctx, sql, args...)`.
- Uses `RETURNING *` only where needed.
- For ops inside an RLS transaction (Stage 3), each accepts `pgx.Tx` instead — see Task 3.2 refactor. For now, methods take the pool.

Schema-aware methods you need: `CreateOrganization`, `CreateUser`, `CreateMembership`, `GetUserByEmail`, `GetUser`, `GetMembership(userID) → (orgID, role)`, `CreateSession`, `GetSession`, `RevokeSession`, `CreateMagicToken`, `GetMagicToken`, `MarkMagicTokenUsed`, `UpdateUserMFASecret`, `UpdateUserMFAEnabled`, `ClearUserMFA`, `CreateAPIKey`, `GetAPIKeyByHash`, `ListAPIKeysByOrg`, `RevokeAPIKey`.

- [ ] **Step 3: Integration test**

`pgstore_test.go` runs only when `DATABASE_URL_TEST` is set:

```go
//go:build integration

package local

import (
	"context"
	"os"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

func TestPGStore_FullSignupCycle(t *testing.T) {
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" { t.Skip("DATABASE_URL_TEST not set") }
	pool, err := db.New(context.Background(), url)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	s := NewPGStore(pool)

	ctx := context.Background()
	user := &domain.User{Email: "pgtest+" + randID(t) + "@example.com", PasswordHash: "x"}
	if err := s.CreateUser(ctx, user); err != nil { t.Fatal(err) }
	// ... org, membership, session round-trip
	got, err := s.GetUserByEmail(ctx, user.Email)
	if err != nil { t.Fatal(err) }
	if got.ID != user.ID { t.Errorf("id mismatch") }
}
```

- [ ] **Step 4: Wire factory**

Update `internal/adapter/auth/factory.go` (created next task) to use `pgstore` in non-test paths.

- [ ] **Step 5: Run integration test**

```bash
docker compose up -d postgres
cd services/control-plane && \
  DATABASE_URL_TEST="postgres://nexis:nexis_dev_password@localhost:5432/nexis" \
  go test -tags=integration -race -run TestPGStore ./internal/adapter/auth/local/... -v
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add services/control-plane && \
git commit -m "feat(auth): pgx-backed store + db pool helper (stage 1.6)"
```

### Task 1.7: Factory + config wiring

**Files:**
- Create: `services/control-plane/internal/adapter/auth/factory.go`
- Modify: `internal/platform/config/config.go` (add envs from Appendix A)
- Modify: `cmd/server/main.go` (init pool, provider, pass to httpserver.New)

- [ ] **Step 1: Extend `config.go`**

Add fields per Appendix A: `AuthProvider`, `AllowStubWorkOS`, `SessionSecret`, `AuditSecret`, `SMTPHost`, `SMTPPort`, `SMTPFrom`, `DatabaseURLApp`, `OTelServiceName`, `OTelEndpoint`, `AppBaseURL`. Load with `env()` helper.

- [ ] **Step 2: `factory.go`**

```go
package auth

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/workos"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

func NewFromConfig(cfg config.Config, pool *pgxpool.Pool) (domain.AuthProvider, error) {
	switch cfg.AuthProvider {
	case "local", "":
		return local.New(local.Config{
			Store:         local.NewPGStore(pool),
			SessionSecret: []byte(cfg.SessionSecret),
			Mailer:        &local.SMTPMailer{Host: cfg.SMTPHost, Port: cfg.SMTPPort, From: cfg.SMTPFrom},
			BaseURL:       cfg.AppBaseURL,
		}), nil
	case "workos":
		if cfg.AllowStubWorkOS {
			return local.New(/* same as above */), nil
		}
		return workos.New(workos.Config{}), nil
	default:
		return nil, fmt.Errorf("unknown AUTH_PROVIDER %q", cfg.AuthProvider)
	}
}
```

- [ ] **Step 3: Update `main.go` to init pool + provider**

Inside `main()`, after config.Load:

```go
ctx := context.Background()
pool, err := db.New(ctx, cfg.DatabaseURLApp)
if err != nil { logger.Error("db pool", "err", err); os.Exit(1) }
defer pool.Close()

authProvider, err := auth.NewFromConfig(cfg, pool)
if err != nil { logger.Error("auth provider", "err", err); os.Exit(1) }

srv := httpserver.New(cfg, logger, httpserver.Deps{Pool: pool, Auth: authProvider})
```

- [ ] **Step 4: Update `internal/transport/http/server.go` to accept Deps**

```go
type Deps struct {
	Pool *pgxpool.Pool
	Auth domain.AuthProvider
}

func New(cfg config.Config, logger *slog.Logger, deps Deps) http.Handler { ... }
```

The healthz + llm-diag routes from Phase 1 stay. Auth routes wire in Stage 2.

- [ ] **Step 5: Build + arch-lint**

```bash
cd services/control-plane && make build && make arch
```

Expected: green. If arch-lint flags transport→adapter (it shouldn't — already allowed), fix the .arch.yaml.

- [ ] **Step 6: Commit Stage 1**

```bash
git add services/control-plane && \
git commit -m "feat(auth): factory + config + wire pool into main (stage 1.7)"
```

### Task 1.8: WorkOS stub

**Files:**
- Create: `services/control-plane/internal/adapter/auth/workos/provider.go`
- Create: `services/control-plane/internal/adapter/auth/workos/provider_test.go`

- [ ] **Step 1: Stub provider — every method returns `ErrNotImplemented`**

```go
package workos

import (
	"context"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Config struct{}

type Provider struct{}

func New(_ Config) *Provider { return &Provider{} }

func (*Provider) Name() string { return "workos" }

// All methods identical shape — returning ErrNotImplemented. Phase 7 fills them in.
func (*Provider) Signup(_ context.Context, _ domain.SignupInput) (domain.SignupResult, error) {
	return domain.SignupResult{}, domain.ErrNotImplemented
}
// ... repeat for every AuthProvider method
```

- [ ] **Step 2: Test that the stub conforms to the interface**

```go
package workos

import (
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func TestProvider_ImplementsAuthProvider(t *testing.T) {
	var _ domain.AuthProvider = (*Provider)(nil)
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/adapter/auth/workos/...
git add . && git commit -m "feat(auth): workos stub adapter (stage 1.8)"
```

---

## Stage 2 — HTTP auth routes + middleware

### Task 2.1: Cookie + bearer extraction middleware

**Files:**
- Create: `services/control-plane/internal/transport/http/middleware/auth.go`
- Create: tests at `services/control-plane/tests/integration/auth_middleware_test.go`

- [ ] **Step 1: Failing test** — three scenarios:
  - No header/cookie + unprotected route → 200.
  - No header/cookie + protected route → 401.
  - Valid cookie → handler sees `Principal` in ctx.

- [ ] **Step 2: Implement middleware**

```go
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type ctxKeyPrincipal struct{}

func Auth(p domain.AuthProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearer(r)
			if token == "" {
				if c, err := r.Cookie("nexis_session"); err == nil { token = c.Value }
			}
			if token != "" {
				princ, err := verify(r.Context(), p, token)
				if err == nil {
					ctx := context.WithValue(r.Context(), ctxKeyPrincipal{}, princ)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") { return strings.TrimPrefix(h, "Bearer ") }
	return ""
}

func verify(ctx context.Context, p domain.AuthProvider, token string) (domain.Principal, error) {
	if strings.HasPrefix(token, "nx_live_") { return p.VerifyAPIKey(ctx, token) }
	return p.VerifyToken(ctx, token)
}

func PrincipalFrom(ctx context.Context) (domain.Principal, bool) {
	p, ok := ctx.Value(ctxKeyPrincipal{}).(domain.Principal)
	return p, ok
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := PrincipalFrom(r.Context()); !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 3: Run tests + commit**

```bash
go test -race ./tests/integration/... -run TestAuth -v
git add . && git commit -m "feat(http): auth middleware extracts principal from cookie/bearer (stage 2.1)"
```

### Task 2.2: Auth handlers — signup, login, logout

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/auth.go`
- Create: `services/control-plane/internal/transport/http/dto/auth.go` (request/response DTOs)
- Modify: `internal/transport/http/server.go` (mount routes)
- Add integration tests

- [ ] **Step 1: DTOs**

`dto/auth.go`:

```go
package dto

type SignupReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	OrgName  string `json:"org_name"`
}

type LoginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	MFACode  string `json:"mfa_code,omitempty"`
}

type AuthResp struct {
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
	Org struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"org"`
	ExpiresAt string `json:"expires_at"`
}

type ErrorResp struct{ Error string `json:"error"` }
```

- [ ] **Step 2: Handlers**

`handler/auth.go` — Signup, Login, Logout. Each:
1. Decode JSON.
2. Call provider.
3. Set `nexis_session` cookie with the JWT (`HttpOnly`, `SameSite=Lax`, `Secure` only if cfg.AppEnv != "dev").
4. Return JSON body (without the token — that's in the cookie).
5. Error → 4xx with `ErrorResp`.

- [ ] **Step 3: Mount in `server.go`**

```go
authMW := middleware.Auth(deps.Auth)
r.Use(authMW)

r.Post("/v1/auth/signup", handler.Signup(deps.Auth, cfg))
r.Post("/v1/auth/login",  handler.Login(deps.Auth, cfg))
r.Group(func(g chi.Router) {
	g.Use(middleware.RequireAuth)
	g.Post("/v1/auth/logout", handler.Logout(deps.Auth))
})
```

- [ ] **Step 4: Integration tests**

`tests/integration/auth_handlers_test.go`:
- POST signup → 201 + cookie set.
- POST login (bad password) → 401.
- POST login (good) → 200 + cookie.
- POST logout (no cookie) → 401.
- POST logout (with cookie) → 204 + subsequent request with same cookie → 401.

Each test builds a fresh control-plane via `httpserver.New(...)` with an in-memory store (use the `memStore` from Task 1.2 — make sure it's exported under a test helper).

- [ ] **Step 5: Run + commit**

```bash
go test -race ./tests/integration/... -v
git add . && git commit -m "feat(http): signup/login/logout handlers (stage 2.2)"
```

### Task 2.3: Auth handlers — magic, MFA, API keys, /me

**Files:**
- Modify: `handler/auth.go` (add 6 more handlers)
- Modify: `server.go` (mount routes)
- Add tests

- [ ] **Step 1: Handlers**

Endpoints per Appendix B:
- `POST /v1/auth/magic` — IssueMagicLink (no auth required).
- `GET  /v1/auth/verify?token=...` — ConsumeMagicLink → 302 redirect to `cfg.AppBaseURL + "/dashboard"` with cookie set.
- `POST /v1/auth/mfa/enroll` — protected; return `{qr_data_url, recovery_codes (TODO Phase 3)}`. QR is `data:image/png;base64,<...>`.
- `POST /v1/auth/mfa/verify` — protected.
- `DELETE /v1/auth/mfa` — protected.
- `POST /v1/apikeys` — protected.
- `GET /v1/apikeys` — protected.
- `DELETE /v1/apikeys/:id` — protected.
- `GET /v1/me` — protected; returns `{user, org, role}`.

- [ ] **Step 2: Tests**

For each:
- Unauthorized request to a protected endpoint → 401.
- Happy path → expected status + body.
- One adversarial test (e.g. MFA verify with wrong code → 400).

- [ ] **Step 3: Run + commit**

```bash
go test -race ./tests/integration/... -v
git add . && git commit -m "feat(http): magic, mfa, apikeys, /me handlers (stage 2.3)"
```

---

## Stage 3 — RLS middleware

### Task 3.1: Tx-per-request RLS middleware

**Files:**
- Create: `services/control-plane/internal/transport/http/middleware/rls.go`
- Add tests

- [ ] **Step 1: Failing test**

Two test handlers — one queries `api_keys`, one queries with explicit `SET LOCAL`. Verify that wrapping with `RLS(pool)` produces the same row count as the explicit version, and that without the middleware (using `nexis_app` role) the query returns zero rows.

- [ ] **Step 2: Implement**

```go
package middleware

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ctxKeyTx struct{}

func TxFrom(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(ctxKeyTx{}).(pgx.Tx)
	return tx, ok
}

func RLS(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			princ, ok := PrincipalFrom(r.Context())
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			ctx := r.Context()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				http.Error(w, "db", http.StatusInternalServerError)
				return
			}
			defer tx.Rollback(ctx) // no-op if committed

			if _, err := tx.Exec(ctx, "SET LOCAL app.current_org_id = $1", princ.OrgID); err != nil {
				http.Error(w, "rls", http.StatusInternalServerError)
				return
			}

			rec := &statusRecorder{ResponseWriter: w, status: 200}
			ctx2 := context.WithValue(ctx, ctxKeyTx{}, tx)
			next.ServeHTTP(rec, r.WithContext(ctx2))

			if rec.status >= 200 && rec.status < 400 {
				_ = tx.Commit(ctx)
			}
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(c int) { r.status = c; r.ResponseWriter.WriteHeader(c) }
```

- [ ] **Step 3: Apply to protected routes**

In `server.go`, wrap the protected group:

```go
r.Group(func(g chi.Router) {
	g.Use(middleware.RequireAuth, middleware.RLS(deps.Pool))
	// all protected handlers here
})
```

- [ ] **Step 4: Refactor handlers to use `TxFrom(ctx)` for queries**

Handlers that read/write tenant tables (api_keys, sessions, audit_log) must pull the tx from ctx, not query the pool directly. Update `handler/auth.go` API-key handlers + `/v1/me` (touches `org_members`).

The auth signup/login paths still use the pool directly because they pre-date the principal.

- [ ] **Step 5: Acceptance**

End-to-end test:
1. Spin up two orgs A and B via signup.
2. Create 2 API keys in each.
3. Login as A → `GET /v1/apikeys` → 2 keys, all org A.
4. Login as B → `GET /v1/apikeys` → 2 keys, all org B.

Also raw SQL check via `nexis_app` role:

```bash
PGPASSWORD=nexis_app_dev_password docker compose exec -T postgres psql \
  -U nexis_app -d nexis \
  -c "SELECT count(*) FROM api_keys;"
```

Expected: 0 (no `SET LOCAL`, RLS blocks all rows).

- [ ] **Step 6: Commit**

```bash
git add . && git commit -m "feat(http): RLS middleware + tx-per-request (stage 3)"
```

---

## Stage 4 — Audit log writer

### Task 4.1: HMAC chain writer

**Files:**
- Create: `services/control-plane/internal/adapter/audit/hmac_writer.go`
- Create: `services/control-plane/internal/adapter/audit/hmac_writer_test.go`
- Modify: `internal/domain/audit.go` (port)
- Update factory in `internal/adapter/audit/factory.go`

- [ ] **Step 1: Port**

`internal/domain/audit.go`:

```go
package domain

import "context"

type AuditWriter interface {
	Write(ctx context.Context, p Principal, action, target string, metadata map[string]any) error
}
```

- [ ] **Step 2: Failing test**

```go
func TestHMACWriter_AppendsChainedRows(t *testing.T) {
	w, tx := newTestWriter(t)
	princ := domain.Principal{UserID: "u", OrgID: "org-1"}
	if err := w.Write(ctx, princ, "user.signup", "u", nil); err != nil { t.Fatal(err) }
	if err := w.Write(ctx, princ, "user.login", "u", nil); err != nil { t.Fatal(err) }
	rows, _ := tx.dump("audit_log")
	if len(rows) != 2 { t.Fatalf("rows: %d", len(rows)) }
	// row 1 prev_hash NULL, row 2 prev_hash = row 1's row_hash
	if rows[1].prevHash != rows[0].rowHash { t.Error("chain broken") }
	// row_hash = HMAC(prev || canonicalJSON(payload))
	want := hmacSHA256(secret, append(rows[0].rowHash, canonicalJSON(rows[1]) ...))
	if !bytes.Equal(rows[1].rowHash, want) { t.Error("row_hash mismatch") }
}

func TestHMACWriter_VerifyDetectsTamper(t *testing.T) {
	w, tx := newTestWriter(t)
	_ = w.Write(ctx, princ, "a", "x", nil)
	_ = w.Write(ctx, princ, "b", "x", nil)
	tx.mutate(1, "action", "z") // tamper row 1
	res, err := audit.Verify(ctx, tx.pool, secret)
	if err != nil { t.Fatal(err) }
	if res.OK { t.Error("verify should fail") }
}
```

- [ ] **Step 3: Implement writer**

```go
package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type HMACWriter struct {
	secret []byte
	tx     func(context.Context) pgx.Tx
}

func New(secret []byte, txFn func(context.Context) pgx.Tx) *HMACWriter {
	return &HMACWriter{secret: secret, tx: txFn}
}

func (w *HMACWriter) Write(ctx context.Context, p domain.Principal, action, target string, metadata map[string]any) error {
	tx := w.tx(ctx)
	var prev []byte
	if err := tx.QueryRow(ctx,
		"SELECT row_hash FROM audit_log WHERE org_id=$1 ORDER BY created_at DESC LIMIT 1", p.OrgID,
	).Scan(&prev); err != nil && err != pgx.ErrNoRows {
		return err
	}
	now := time.Now().UTC()
	id := newUUID()
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

	_, err := tx.Exec(ctx, `
		INSERT INTO audit_log (id, org_id, actor, action, target, metadata, prev_hash, row_hash, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		id, p.OrgID, p.UserID, action, target, metadata, prev, rowHash, now,
	)
	return err
}

func canonicalJSON(m map[string]any) []byte {
	keys := make([]string, 0, len(m))
	for k := range m { keys = append(keys, k) }
	sort.Strings(keys)
	out := []byte{'{'}
	for i, k := range keys {
		if i > 0 { out = append(out, ',') }
		kj, _ := json.Marshal(k)
		vj, _ := json.Marshal(m[k])
		out = append(out, kj...)
		out = append(out, ':')
		out = append(out, vj...)
	}
	out = append(out, '}')
	return out
}
```

- [ ] **Step 4: Verify function**

```go
func Verify(ctx context.Context, pool *pgxpool.Pool, secret []byte) (VerifyResult, error) {
	// SELECT id, org_id, ..., prev_hash, row_hash FROM audit_log ORDER BY org_id, created_at
	// For each org: prev := NULL; for each row recompute hmac(prev || canonicalJSON(payload)); check == row_hash
	// Return first bad row id if any.
}
```

- [ ] **Step 5: Verify endpoint**

`handler/audit.go`:

```go
func AuditVerify(secret []byte, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := audit.Verify(r.Context(), pool, secret)
		// JSON-encode {ok, rows_checked, first_bad_row_id}
	}
}
```

Wire: `r.Get("/v1/audit/verify", handler.AuditVerify(...))` — admin-only, but Phase 2 doesn't enforce admin RBAC yet; gated by `RequireAuth + RLS` is fine for now.

- [ ] **Step 6: Instrument writes**

Call `audit.Write(...)` from every mutation usecase: signup, login, logout, MFA enroll/verify/disable, API key create/revoke. Pass through `AuditWriter` via `Deps`.

- [ ] **Step 7: Run + commit**

```bash
go test -race -tags=integration ./internal/adapter/audit/... -v
git add . && git commit -m "feat(audit): HMAC-chained writer + verify endpoint + instrumented mutations (stage 4)"
```

---

## Stage 5 — Web auth pages + Next middleware

### Task 5.1: Client SDK + Next middleware guard

**Files:**
- Create: `apps/web/lib/auth.ts` (thin SDK)
- Create: `apps/web/middleware.ts` (route guard)

- [ ] **Step 1: `lib/auth.ts`**

```ts
import { cookies } from "next/headers";

const API = process.env.NEXT_PUBLIC_API_URL!; // http://localhost:8080

export type AuthResp = {
  user: { id: string; email: string };
  org:  { id: string; name: string; slug: string };
  expires_at: string;
};

export async function signup(input: { email: string; password: string; org_name: string }): Promise<AuthResp> {
  const r = await fetch(`${API}/v1/auth/signup`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    credentials: "include",
    body: JSON.stringify(input),
  });
  if (!r.ok) throw new Error((await r.json()).error ?? r.statusText);
  return r.json();
}

export async function login(input: { email: string; password: string; mfa_code?: string }): Promise<AuthResp> { /* identical shape */ }
export async function logout(): Promise<void> { /* POST /v1/auth/logout */ }
export async function me(): Promise<AuthResp | null> { /* GET /v1/me; null on 401 */ }

export async function hasSession() {
  const c = (await cookies()).get("nexis_session");
  return !!c;
}
```

(Next 16: `cookies()` is now async. Verify by reading `apps/web/node_modules/next/dist/docs/01-app/02-guides/cookies.md`.)

- [ ] **Step 2: `middleware.ts`**

```ts
import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

export function middleware(req: NextRequest) {
  const session = req.cookies.get("nexis_session");
  const path = req.nextUrl.pathname;
  const inAppGroup = path.startsWith("/dashboard"); // expand as more (app) routes land
  if (inAppGroup && !session) {
    const url = req.nextUrl.clone();
    url.pathname = "/sign-in";
    url.searchParams.set("next", path);
    return NextResponse.redirect(url);
  }
  return NextResponse.next();
}

export const config = { matcher: ["/dashboard/:path*"] };
```

### Task 5.2: Sign-up / sign-in / verify / mfa pages

**Files:**
- Create: `apps/web/app/(auth)/layout.tsx` — bare card layout, light bg
- Create: `apps/web/app/(auth)/sign-up/page.tsx`
- Create: `apps/web/app/(auth)/sign-in/page.tsx`
- Create: `apps/web/app/(auth)/verify/page.tsx`
- Create: `apps/web/app/(auth)/mfa/page.tsx`

- [ ] **Step 1: Layout**

Centered card on muted background. Logo at top. `min-h-screen flex items-center justify-center`. Use existing `Button`, no new primitives.

- [ ] **Step 2: Sign-up page** (client component because form submit)

Fields: email, password, org_name. Submit calls `signup()` from SDK. On success, `router.push("/dashboard")`. On `error.message.includes("mfa")` → push `/mfa?next=/dashboard`.

- [ ] **Step 3: Sign-in page**

Same shape; calls `login()`. If response throws `ErrMFARequired`, show MFA code field and re-submit with `mfa_code`.

- [ ] **Step 4: Verify page** — handles magic-link landing

Reads `?token=...` from URL, GETs `${API}/v1/auth/verify?token=...` server-side (the API sets the cookie and 302s to /dashboard).

- [ ] **Step 5: MFA enroll/verify page**

Enroll: calls `POST /v1/auth/mfa/enroll`, displays `<img src={qr_data_url} />`, input for code, calls `POST /v1/auth/mfa/verify`. On success → `/dashboard`.

### Task 5.3: Dashboard stub

**Files:**
- Create: `apps/web/app/(app)/layout.tsx`
- Create: `apps/web/app/(app)/dashboard/page.tsx`

- [ ] **Step 1: Layout**

Top bar with logo, email, logout button (calls `logout()` SDK then `router.push("/sign-in")`).

- [ ] **Step 2: Dashboard page**

Server component: calls `me()`. If null → middleware should have redirected already, but defensively redirect to /sign-in. Renders user email + org name + a "Phase 2 complete ✓" badge.

### Task 5.4: Build + commit

- [ ] **Step 1: Verify**

```bash
/opt/homebrew/bin/pnpm --filter @nexis/web typecheck
/opt/homebrew/bin/pnpm --filter @nexis/web build
```

- [ ] **Step 2: Commit**

```bash
git add apps/web
git commit -m "feat(web): auth pages + dashboard stub + middleware guard (stage 5)"
```

---

## Stage 6 — OpenTelemetry SDK

### Task 6.1: Go OTel init

**Files:**
- Create: `services/control-plane/internal/platform/otel/otel.go`
- Modify: `cmd/server/main.go`
- Modify: `server.go` (add otelhttp middleware)
- Add `go.opentelemetry.io/otel/*` deps

- [ ] **Step 1: Install deps**

```bash
cd services/control-plane && go get \
  go.opentelemetry.io/otel \
  go.opentelemetry.io/otel/sdk \
  go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp \
  go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp \
  && go mod tidy
```

- [ ] **Step 2: `otel.go`**

```go
package otel

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func Init(ctx context.Context, serviceName, endpoint string) (shutdown func(context.Context) error, err error) {
	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(stripScheme(endpoint)),
		otlptracehttp.WithInsecure(),
	)
	if err != nil { return nil, err }

	res, _ := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
	))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithMaxExportBatchSize(64)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	return func(c context.Context) error {
		ctx2, cancel := context.WithTimeout(c, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx2)
	}, nil
}

func stripScheme(s string) string { /* drop http:// / https:// */ }
```

- [ ] **Step 3: Wire in `main.go`**

```go
shutdown, err := otel.Init(ctx, cfg.OTelServiceName, cfg.OTelEndpoint)
if err != nil { logger.Warn("otel init failed", "err", err) } else { defer shutdown(ctx) }
```

- [ ] **Step 4: Add `otelhttp` middleware**

`server.go`:

```go
r.Use(func(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http.server")
})
// Echo trace id on every response
r.Use(traceIDHeader)
```

`traceIDHeader` middleware reads the current span from ctx and sets `x-trace-id`.

### Task 6.2: Web OTel init

**Files:**
- Create: `apps/web/instrumentation.ts` (Next 16 OTel hook)
- Add: `@vercel/otel` dep

- [ ] **Step 1: Install**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && \
/opt/homebrew/bin/pnpm add @vercel/otel
```

- [ ] **Step 2: `instrumentation.ts`**

```ts
import { registerOTel } from "@vercel/otel";

export function register() {
  registerOTel({ serviceName: process.env.OTEL_SERVICE_NAME ?? "nexis-web" });
}
```

Per Next 16 docs (read `node_modules/next/dist/docs/01-app/03-api-reference/05-config/01-next-config-js/instrumentationHook.md`): `instrumentation.ts` is auto-loaded if at app root. No config flag needed in Next 16 (the flag was removed).

- [ ] **Step 3: Set env in compose**

In `docker-compose.yml`, on `web` service:

```yaml
      OTEL_SERVICE_NAME: nexis-web
      OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4318
```

(On `control-plane` it's already there from Phase 1's stage-2 compose.)

### Task 6.3: Verify trace end-to-end

- [ ] **Step 1: Rebuild + bring up stack**

```bash
docker compose up --build -d web control-plane
sleep 10
```

- [ ] **Step 2: Send a login, capture trace id**

```bash
curl -i -X POST http://localhost:8080/v1/auth/login \
  -H "content-type: application/json" \
  -d '{"email":"verify@example.com","password":"x"}' 2>&1 | grep -i x-trace-id
```

(Use any seeded user; if none, signup first.)

- [ ] **Step 3: Query Tempo via Grafana**

Open `http://localhost:3030` → Explore → datasource Tempo → search by traceID. Verify the span tree shows `nexis-web POST /v1/auth/login` parent + `nexis-control-plane http.server` child.

Alternatively `curl http://localhost:3200/api/traces/<id>` directly.

- [ ] **Step 4: Commit**

```bash
git add . && git commit -m "feat(otel): web + control-plane export traces to local Tempo (stage 6)"
```

---

## Stage 7 — Compose env updates

### Task 7.1: Update docker-compose with Phase 2 env

**Files:**
- Modify: `docker-compose.yml`
- Modify: `.env.example`

- [ ] **Step 1: Add to `control-plane` env block**

```yaml
      AUTH_PROVIDER: ${AUTH_PROVIDER:-local}
      ALLOW_STUB_WORKOS: ${ALLOW_STUB_WORKOS:-0}
      SESSION_SECRET: ${SESSION_SECRET:-dev-session-secret-replace-in-prod-32b}
      AUDIT_SECRET:   ${AUDIT_SECRET:-dev-audit-secret-replace-in-prod-32b}
      DATABASE_URL_APP: postgres://nexis_app:nexis_app_dev_password@postgres:5432/nexis?sslmode=disable
      SMTP_HOST: mailhog
      SMTP_PORT: "1025"
      SMTP_FROM: no-reply@nexis.local
      APP_BASE_URL: http://localhost:3000
      OTEL_SERVICE_NAME: nexis-control-plane
```

- [ ] **Step 2: Add to `web` env block**

```yaml
      OTEL_SERVICE_NAME: nexis-web
      OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4318
      NEXT_PUBLIC_API_URL: http://localhost:8080
```

- [ ] **Step 3: Update `.env.example`** to document each var.

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml .env.example
git commit -m "chore(compose): phase-2 env vars (stage 7)"
```

---

## Stage 8 — Playwright E2E + DoD verification

### Task 8.1: Playwright setup

**Files:**
- Create: `apps/web/playwright.config.ts`
- Create: `apps/web/tests/e2e/auth.spec.ts`
- Modify: `apps/web/package.json` (add `@playwright/test`, `e2e` script)

- [ ] **Step 1: Install**

```bash
cd apps/web && /opt/homebrew/bin/pnpm add -D @playwright/test
/opt/homebrew/bin/pnpm exec playwright install chromium
```

- [ ] **Step 2: `playwright.config.ts`**

```ts
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e",
  use: { baseURL: "http://localhost:3000", trace: "on-first-retry" },
  workers: 1, // sequential — stack-shared state
});
```

- [ ] **Step 3: `auth.spec.ts`**

```ts
import { test, expect } from "@playwright/test";

const email = `e2e+${Date.now()}@example.com`;

test("signup → MFA enroll → logout → login → dashboard", async ({ page, request }) => {
  // signup
  await page.goto("/sign-up");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByLabel("Org name").fill("E2E Co");
  await page.getByRole("button", { name: "Sign up" }).click();
  await expect(page).toHaveURL(/\/dashboard$/);

  // MFA enroll (via UI affordance on dashboard or direct nav)
  await page.goto("/mfa");
  const secret = await page.getByTestId("mfa-secret").textContent();
  // Use a helper to compute current TOTP from secret (small dep: otplib or hand-roll)
  const code = totpNow(secret!);
  await page.getByLabel("Code").fill(code);
  await page.getByRole("button", { name: "Verify" }).click();

  // logout
  await page.getByRole("button", { name: "Log out" }).click();
  await expect(page).toHaveURL(/\/sign-in$/);

  // login with MFA
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill("correct-horse-battery-staple");
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.getByLabel("MFA code").fill(totpNow(secret!));
  await page.getByRole("button", { name: "Submit" }).click();
  await expect(page).toHaveURL(/\/dashboard$/);
});
```

`totpNow` — use `otplib` (`pnpm add -D otplib`) or hand-roll a 5-line RFC 6238.

- [ ] **Step 4: package.json script**

```json
"scripts": {
  "e2e": "playwright test",
  "e2e:headed": "playwright test --headed"
}
```

### Task 8.2: Run E2E against running stack

- [ ] **Step 1: Bring up stack**

```bash
docker compose up --build -d
sleep 20
```

- [ ] **Step 2: Run**

```bash
/opt/homebrew/bin/pnpm --filter @nexis/web e2e
```

Expected: all tests pass.

### Task 8.3: DoD verification

- [ ] **Step 1: RLS verification**

```bash
docker compose exec -T postgres psql -U nexis_app -d nexis -c "SELECT count(*) FROM api_keys;"
# Expect: 0

docker compose exec -T postgres psql -U nexis_app -d nexis << 'SQL'
BEGIN;
SET LOCAL app.current_org_id = (SELECT id FROM organizations LIMIT 1);
SELECT count(*) FROM api_keys;
ROLLBACK;
SQL
# Expect: > 0 if any keys exist
```

- [ ] **Step 2: Audit verify**

```bash
TOKEN=$(curl -s -c - -X POST http://localhost:8080/v1/auth/login \
  -H "content-type: application/json" \
  -d '{"email":"<from-e2e>","password":"correct-horse-battery-staple"}' \
  | awk '/nexis_session/{print $7}')

curl -s "http://localhost:8080/v1/audit/verify" -H "Cookie: nexis_session=$TOKEN"
# Expect: {"ok":true,"rows_checked":>0}
```

- [ ] **Step 3: Trace visible in Tempo**

Verified manually in Stage 6 — re-verify after E2E run (more spans now).

- [ ] **Step 4: Full build matrix**

```bash
cd services/control-plane && make build && make test && make vet && make arch
/opt/homebrew/bin/pnpm --filter @nexis/web typecheck
/opt/homebrew/bin/pnpm --filter @nexis/web test
/opt/homebrew/bin/pnpm --filter @nexis/web build
```

All green.

- [ ] **Step 5: Mark Phase 2 complete**

In `docs/PROJECT_PLAN.md`, append `— Completed YYYY-MM-DD` to the Phase 2 heading (line 504).

- [ ] **Step 6: Commit**

```bash
git add apps/web docs/PROJECT_PLAN.md
git commit -m "feat(test): playwright e2e + phase 2 acceptance (stage 8)"
```

---

## Phase 2 Definition of Done

- [ ] `docker compose up -d` runs the full stack (16 services + new auth wiring).
- [ ] Playwright `auth.spec.ts` passes: signup → MFA → logout → login.
- [ ] `GET /v1/me` returns the user + org when authenticated.
- [ ] `GET /v1/audit/verify` returns `{"ok":true}` after the E2E run.
- [ ] RLS-scoped SELECT returns only the current org's rows; without `SET LOCAL` returns 0 rows.
- [ ] Distributed trace visible in Tempo with web → control-plane spans.
- [ ] `AUTH_PROVIDER=workos` fails fast at startup unless `ALLOW_STUB_WORKOS=1`.
- [ ] All Go tests pass (`make test` per service; `tags=integration` for pgstore).
- [ ] All web tests pass (vitest + playwright).
- [ ] `pnpm --filter @nexis/web build` succeeds.
- [ ] `go-arch-lint check` still passes.
- [ ] CI workflow extended (or still passes — Phase 2 doesn't break it).
- [ ] `docs/PROJECT_PLAN.md` Phase 2 heading marked `— Completed YYYY-MM-DD`.

---

## Risks / call-outs

- **bcrypt cost 12 on Apple Silicon** is ~250 ms per hash. Acceptance still passes; just a slow signup.
- **Cookie `Secure` flag** is `false` in dev. Phase 7's Caddy + HTTPS flips it from `APP_ENV`.
- **MailHog persistence** — emails dropped on restart. Playwright reads the link from the response, not from MailHog, so this is fine.
- **sqlc generation** — Phase 2 doesn't add sqlc queries for new tables; auth provider uses raw pgx. Phase 3 can introduce sqlc queries if the surface grows.
- **`packages/db/migrations` vs `services/control-plane/migrations`** — both maintained in parallel (Phase 1 cost). Phase 2 keeps the pattern.
