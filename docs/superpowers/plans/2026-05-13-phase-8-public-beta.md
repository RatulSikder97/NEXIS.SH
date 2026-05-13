# Phase 8 — Public Beta + Docs + Thesis — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development.

**Goal:** Ship the public-beta surface — onboarding polish (sample repo + Shepherd.js tour), empty-state CTAs everywhere, docs site at `docs.nexis.dev`, BetterStack status page at `status.nexis.dev`, invite-code-gated signup, frozen NEXIS-vs-baselines evaluation, NASA-TLX modal — and hand thesis chapters 1–4 to the supervisor.

**Architecture:** No new compute. New surfaces are: one Next.js sub-app (`apps/docs`), one in-app modal + tour overlay, one admin surface, four new HTTP routes (invite, NASA-TLX, eval-export, sample-seed) + two probe endpoints. Two new RLS-protected tables (`invite_codes` system-wide + `nasa_tlx_responses` org-scoped) and one trigger on the existing `organizations` table. Eval runs out-of-band against the Phase 5/6 stack and the JSON is checked into the repo.

**Tech Stack:** Go 1.25 control-plane, Next.js 16.2.2 web + docs (Nextra v4), Postgres 16 RLS, BetterStack (status page) via the Terraform community provider, Shepherd.js v14 (lazy-loaded), Vercel (static hosting for `apps/docs`), Google Forms (thesis-mirror sink).

**Spec:** `docs/superpowers/specs/2026-05-13-phase-8-public-beta.md`.

---

## Salvage / reuse

* All Phase 1–7 infra: pgx pools, RLS middleware, auth/audit ports, KeyVault adapter, integration repos, Stripe adapter, GitHub App install flow, RecoveryPipeline workflow, SSE broker, eval CLI scaffolding.
* `apps/web/components/empty/` — Phase 3 already has the line-art SVGs and a `<Skeleton>` library. Phase 8 wraps these into a `<EmptyState />` component; no new design work.
* `apps/web/app/onboarding/workspace/{page,client}.tsx` — extend, not rewrite.
* `apps/web/app/(auth)/sign-up/page.tsx` — extend with invite gate.
* `services/control-plane/internal/transport/http/middleware/{auth,rls,rbac,audit}.go` — extend the existing owner-only sub-group with the admin routes.
* Phase 5 eval CLI at `apps/web/scripts/eval/` — extend with 3 new baselines (openai-only / ollama-only / human-only) wrapping the existing OpenAI/Ollama providers.
* Phase 4 validator fixture at `services/validator/fixtures/incidents/null_deref_001.json` — reused verbatim by the sample-seeder.

---

## Stage 0 — Schema + RLS + config

### Task 0.1: Drizzle schema additions

**Files:**
- Modify: `packages/db/schema.ts`

- [ ] **Step 1: Add 2 tables + 1 column + JSDoc keys**

```ts
// near the top with other enums
const inviteCodeStatus = ["unused", "partial", "exhausted", "expired"] as const;

// after waitlist
/**
 * users.preferences conventional keys (NOT enforced at the DB level):
 *   tour_completed: boolean       — flipped by the client when Shepherd.js finishes or is dismissed
 *   tlx_prompted_at: timestamptz  — set when the NASA-TLX modal is first opened to prevent re-prompts
 *   theme: "light"|"dark"|"system"
 */

export const inviteCodes = pgTable("invite_codes", {
  code:       text("code").primaryKey(),
  maxUses:    integer("max_uses").notNull().default(1),
  usedCount:  integer("used_count").notNull().default(0),
  expiresAt:  timestamp("expires_at", { withTimezone: true }),
  createdBy:  uuid("created_by").references(() => users.id),
  note:       text("note"),
  createdAt:  timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const nasaTlxResponses = pgTable("nasa_tlx_responses", {
  id:               uuid("id").primaryKey().defaultRandom(),
  userId:           uuid("user_id").notNull().references(() => users.id),
  orgId:            uuid("org_id").notNull().references(() => organizations.id),
  recoveryRunId:    uuid("recovery_run_id"),
  mentalDemand:     integer("mental_demand").notNull(),
  physicalDemand:   integer("physical_demand").notNull(),
  temporalDemand:   integer("temporal_demand").notNull(),
  performance:      integer("performance").notNull(),
  effort:           integer("effort").notNull(),
  frustration:      integer("frustration").notNull(),
  notes:            text("notes"),
  createdAt:        timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  userRecordedIdx: index("nasa_tlx_user_idx").on(t.userId, t.createdAt),
}));
```

Add to `organizations`:

```ts
  successfulRecoveriesCount: integer("successful_recoveries_count").notNull().default(0),
```

- [ ] **Step 2: Generate**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm exec drizzle-kit generate --name=phase8_public_beta
```

### Task 0.2: Control-plane migration + RLS + trigger

**Files:**
- Create: `services/control-plane/migrations/0019_phase8_invite_tlx.up.sql`
- Create: `services/control-plane/migrations/0019_phase8_invite_tlx.down.sql`
- Create: `services/control-plane/migrations/0020_phase8_recoveries_trigger.up.sql`
- Create: `services/control-plane/migrations/0020_phase8_recoveries_trigger.down.sql`
- Create: `services/control-plane/migrations/0021_phase8_rls.up.sql`
- Create: `services/control-plane/migrations/0021_phase8_rls.down.sql`

> **Note**: numbering assumes Phase 7 lands at 0017/0018. Re-number to whatever Phase 7 leaves us with.

- [ ] **Step 1: Tables**

```sql
-- 0019_phase8_invite_tlx.up.sql

CREATE TABLE invite_codes (
  code         text PRIMARY KEY,
  max_uses     int NOT NULL DEFAULT 1,
  used_count   int NOT NULL DEFAULT 0,
  expires_at   timestamptz,
  created_by   uuid REFERENCES users(id),
  note         text,
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX invite_codes_created_idx ON invite_codes (created_at DESC);

CREATE TABLE nasa_tlx_responses (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id            uuid NOT NULL REFERENCES users(id),
  org_id             uuid NOT NULL REFERENCES organizations(id),
  recovery_run_id    uuid,
  mental_demand      smallint NOT NULL CHECK (mental_demand     BETWEEN 0 AND 20),
  physical_demand    smallint NOT NULL CHECK (physical_demand   BETWEEN 0 AND 20),
  temporal_demand    smallint NOT NULL CHECK (temporal_demand   BETWEEN 0 AND 20),
  performance        smallint NOT NULL CHECK (performance       BETWEEN 0 AND 20),
  effort             smallint NOT NULL CHECK (effort            BETWEEN 0 AND 20),
  frustration        smallint NOT NULL CHECK (frustration       BETWEEN 0 AND 20),
  notes              text,
  created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX nasa_tlx_user_idx ON nasa_tlx_responses (user_id, created_at DESC);
CREATE INDEX nasa_tlx_org_idx  ON nasa_tlx_responses (org_id,  created_at DESC);

ALTER TABLE organizations
  ADD COLUMN successful_recoveries_count int NOT NULL DEFAULT 0;
```

`0019_*.down.sql` drops the column then the two tables (FK from `nasa_tlx_responses` first).

- [ ] **Step 2: Trigger on `pipeline_runs`** (Phase 4 table)

```sql
-- 0020_phase8_recoveries_trigger.up.sql

CREATE OR REPLACE FUNCTION bump_successful_recoveries() RETURNS trigger AS $$
BEGIN
  IF NEW.status = 'succeeded'
     AND NEW.merged_pr_url IS NOT NULL
     AND (OLD.merged_pr_url IS NULL OR OLD.merged_pr_url = '')
  THEN
    UPDATE organizations
       SET successful_recoveries_count = successful_recoveries_count + 1
     WHERE id = NEW.org_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER pipeline_runs_bump_recoveries
  AFTER UPDATE ON pipeline_runs
  FOR EACH ROW
  EXECUTE FUNCTION bump_successful_recoveries();
```

`0020_*.down.sql`: `DROP TRIGGER pipeline_runs_bump_recoveries ON pipeline_runs; DROP FUNCTION bump_successful_recoveries();`

- [ ] **Step 3: RLS**

```sql
-- 0021_phase8_rls.up.sql

ALTER TABLE invite_codes        ENABLE ROW LEVEL SECURITY;
ALTER TABLE nasa_tlx_responses  ENABLE ROW LEVEL SECURITY;

-- invite_codes is system-wide; only owner-role + the unauth /validate handler hit it.
-- We model that by allowing app role to read/write only when an admin context is set.
CREATE POLICY admin_only ON invite_codes
  USING (current_setting('app.is_admin_route', true) = 'true')
  WITH CHECK (current_setting('app.is_admin_route', true) = 'true');

CREATE POLICY tenant_isolation ON nasa_tlx_responses
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid)
  WITH CHECK (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON invite_codes       TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON nasa_tlx_responses TO nexis_app;
```

`0021_*.down.sql`: drops policies + DISABLE RLS.

The `app.is_admin_route` GUC is set by a new middleware (see Stage 1 Task 1.6). The unauth `/v1/auth/invite/validate` handler sets it to `'true'` via `SET LOCAL` for its single SELECT.

- [ ] **Step 4: Apply**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make migrate-up
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm exec drizzle-kit migrate
```

- [ ] **Step 5: Verify**

```bash
docker compose exec -T postgres psql -U nexis -d nexis -c "\dt" | grep -E 'invite_codes|nasa_tlx'
docker compose exec -T postgres psql -U nexis -d nexis -c "SELECT successful_recoveries_count FROM organizations LIMIT 1;"
docker compose exec -T postgres psql -U nexis -d nexis -c "SELECT proname FROM pg_proc WHERE proname='bump_successful_recoveries';"
```

### Task 0.3: Config + env

**Files:**
- Modify: `services/control-plane/internal/platform/config/config.go`
- Modify: `docker-compose.yml`
- Modify: `.env.example`

- [ ] **Step 1: Add 4 env vars**

```go
// config.go additions inside type Config struct
NASATLXGoogleFormURL     string `env:"NASA_TLX_GOOGLE_FORM_URL"`
SentryProbeHMACSecret    string `env:"SENTRY_PROBE_HMAC_SECRET"`
EvalModeEnabled          bool   `env:"EVAL_MODE_ENABLED" envDefault:"false"`
EvalResultsPath          string `env:"EVAL_RESULTS_PATH" envDefault:"docs/eval-results-v1.json"`
```

- [ ] **Step 2: docker-compose env**

```yaml
# control-plane service env block
      NASA_TLX_GOOGLE_FORM_URL:    ${NASA_TLX_GOOGLE_FORM_URL:-}
      SENTRY_PROBE_HMAC_SECRET:    ${SENTRY_PROBE_HMAC_SECRET:-probe-dev-secret}
      EVAL_MODE_ENABLED:           ${EVAL_MODE_ENABLED:-false}
      EVAL_RESULTS_PATH:           ${EVAL_RESULTS_PATH:-docs/eval-results-v1.json}
```

And `.env.example` mirrors.

### Task 0.4: Commit

```bash
git add packages/db apps/web/drizzle services/control-plane/migrations services/control-plane/internal/platform/config docker-compose.yml .env.example
git commit -m "feat(db): phase 8 schema (invite_codes, nasa_tlx_responses, recoveries trigger) + RLS (stage 0)"
```

---

## Stage 1 — Invite codes (domain + handler + signup gate + waitlist)

### Task 1.1: Domain types

**Files:**
- Create: `services/control-plane/internal/domain/invite.go`

```go
package domain

import (
    "context"
    "time"
)

type InviteCode struct {
    Code      string
    MaxUses   int
    UsedCount int
    ExpiresAt *time.Time
    CreatedBy *string
    Note      string
    CreatedAt time.Time
}

type ValidateInviteResult struct {
    Code      string `json:"code"`
    Remaining int    `json:"remaining"`
}

type InviteService interface {
    Validate(ctx context.Context, code string) (ValidateInviteResult, error)
    Redeem(ctx context.Context, code string) error
    Mint(ctx context.Context, p Principal, in MintInviteInput) (InviteCode, error)
    List(ctx context.Context, p Principal, status string) ([]InviteCode, error)
    Revoke(ctx context.Context, p Principal, code string) error
}

type MintInviteInput struct {
    MaxUses   int
    ExpiresAt *time.Time
    Note      string
}
```

Error sentinels in `internal/domain/errors.go`: `ErrInviteNotFound`, `ErrInviteExpired`, `ErrInviteExhausted`.

### Task 1.2: Repo

**Files:**
- Create: `services/control-plane/internal/adapter/repo/invite_codes_repo.go`

```go
package repo

import (
    "context"
    "errors"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type InviteCodesRepo struct{ pool *pgxpool.Pool }

func NewInviteCodesRepo(p *pgxpool.Pool) *InviteCodesRepo { return &InviteCodesRepo{pool: p} }

// withAdminRoute opens a tx and sets app.is_admin_route=true so the RLS policy
// allows access. Used by every invite-codes query — they're system-wide.
func (r *InviteCodesRepo) withAdminRoute(ctx context.Context, fn func(pgx.Tx) error) error {
    tx, err := r.pool.Begin(ctx)
    if err != nil { return err }
    defer tx.Rollback(ctx)
    if _, err := tx.Exec(ctx, "SELECT set_config('app.is_admin_route','true', true)"); err != nil {
        return err
    }
    if err := fn(tx); err != nil { return err }
    return tx.Commit(ctx)
}

func (r *InviteCodesRepo) Get(ctx context.Context, code string) (*domain.InviteCode, error) {
    var ic domain.InviteCode
    err := r.withAdminRoute(ctx, func(tx pgx.Tx) error {
        return tx.QueryRow(ctx, `
            SELECT code, max_uses, used_count, expires_at, created_by, COALESCE(note, ''), created_at
            FROM invite_codes WHERE code=$1`, code).
            Scan(&ic.Code, &ic.MaxUses, &ic.UsedCount, &ic.ExpiresAt, &ic.CreatedBy, &ic.Note, &ic.CreatedAt)
    })
    if errors.Is(err, pgx.ErrNoRows) { return nil, domain.ErrInviteNotFound }
    return &ic, err
}

// RedeemAtomic increments used_count under SELECT FOR UPDATE.
// Returns ErrInviteExhausted/ErrInviteExpired without a side effect.
func (r *InviteCodesRepo) RedeemAtomic(ctx context.Context, code string) error {
    return r.withAdminRoute(ctx, func(tx pgx.Tx) error {
        var maxU, usedU int
        var expiresAt *time.Time
        err := tx.QueryRow(ctx, `
            SELECT max_uses, used_count, expires_at FROM invite_codes
            WHERE code=$1 FOR UPDATE`, code).Scan(&maxU, &usedU, &expiresAt)
        if errors.Is(err, pgx.ErrNoRows) { return domain.ErrInviteNotFound }
        if err != nil { return err }
        if expiresAt != nil && time.Now().After(*expiresAt) { return domain.ErrInviteExpired }
        if usedU >= maxU { return domain.ErrInviteExhausted }
        _, err = tx.Exec(ctx, `UPDATE invite_codes SET used_count=used_count+1 WHERE code=$1`, code)
        return err
    })
}

func (r *InviteCodesRepo) Insert(ctx context.Context, ic *domain.InviteCode) error {
    return r.withAdminRoute(ctx, func(tx pgx.Tx) error {
        return tx.QueryRow(ctx, `
            INSERT INTO invite_codes (code, max_uses, used_count, expires_at, created_by, note, created_at)
            VALUES ($1,$2,0,$3,$4,$5, now())
            RETURNING created_at`,
            ic.Code, ic.MaxUses, ic.ExpiresAt, ic.CreatedBy, ic.Note).Scan(&ic.CreatedAt)
    })
}

func (r *InviteCodesRepo) List(ctx context.Context, status string) ([]domain.InviteCode, error) {
    var out []domain.InviteCode
    err := r.withAdminRoute(ctx, func(tx pgx.Tx) error {
        // status filter computed at the SQL level
        rows, err := tx.Query(ctx, `
            SELECT code, max_uses, used_count, expires_at, created_by, COALESCE(note,''), created_at
            FROM invite_codes
            WHERE
              CASE $1
                WHEN 'unused'    THEN used_count = 0
                WHEN 'partial'   THEN used_count > 0 AND used_count < max_uses
                WHEN 'exhausted' THEN used_count >= max_uses
                WHEN 'expired'   THEN expires_at IS NOT NULL AND expires_at < now()
                ELSE true
              END
            ORDER BY created_at DESC LIMIT 200`, status)
        if err != nil { return err }
        defer rows.Close()
        for rows.Next() {
            var ic domain.InviteCode
            if err := rows.Scan(&ic.Code, &ic.MaxUses, &ic.UsedCount, &ic.ExpiresAt, &ic.CreatedBy, &ic.Note, &ic.CreatedAt); err != nil {
                return err
            }
            out = append(out, ic)
        }
        return rows.Err()
    })
    return out, err
}

func (r *InviteCodesRepo) Revoke(ctx context.Context, code string) error {
    return r.withAdminRoute(ctx, func(tx pgx.Tx) error {
        ct, err := tx.Exec(ctx, `UPDATE invite_codes SET used_count = max_uses WHERE code=$1`, code)
        if err != nil { return err }
        if ct.RowsAffected() == 0 { return domain.ErrInviteNotFound }
        return nil
    })
}
```

### Task 1.3: Service

**Files:**
- Create: `services/control-plane/internal/adapter/invite/service.go`
- Create: `services/control-plane/internal/adapter/invite/service_test.go`

```go
package invite

import (
    "context"
    "crypto/rand"
    "encoding/base32"
    "strings"
    "time"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Service struct{ repo *repo.InviteCodesRepo }

func New(r *repo.InviteCodesRepo) *Service { return &Service{repo: r} }

func (s *Service) Validate(ctx context.Context, code string) (domain.ValidateInviteResult, error) {
    ic, err := s.repo.Get(ctx, normalize(code))
    if err != nil { return domain.ValidateInviteResult{}, err }
    if ic.ExpiresAt != nil && time.Now().After(*ic.ExpiresAt) { return domain.ValidateInviteResult{}, domain.ErrInviteExpired }
    if ic.UsedCount >= ic.MaxUses { return domain.ValidateInviteResult{}, domain.ErrInviteExhausted }
    return domain.ValidateInviteResult{Code: ic.Code, Remaining: ic.MaxUses - ic.UsedCount}, nil
}

func (s *Service) Redeem(ctx context.Context, code string) error {
    return s.repo.RedeemAtomic(ctx, normalize(code))
}

func (s *Service) Mint(ctx context.Context, p domain.Principal, in domain.MintInviteInput) (domain.InviteCode, error) {
    if in.MaxUses <= 0 { in.MaxUses = 1 }
    ic := domain.InviteCode{
        Code:    generate(),
        MaxUses: in.MaxUses, ExpiresAt: in.ExpiresAt, Note: in.Note,
        CreatedBy: &p.UserID,
    }
    return ic, s.repo.Insert(ctx, &ic)
}

func (s *Service) List(ctx context.Context, _ domain.Principal, status string) ([]domain.InviteCode, error) {
    return s.repo.List(ctx, status)
}

func (s *Service) Revoke(ctx context.Context, _ domain.Principal, code string) error {
    return s.repo.Revoke(ctx, normalize(code))
}

func normalize(c string) string { return strings.ToUpper(strings.TrimSpace(c)) }

// generate returns "NX-XXXX" where XXXX is 4 base32 chars from /dev/urandom.
func generate() string {
    var b [4]byte
    _, _ = rand.Read(b[:])
    s := strings.TrimRight(base32.StdEncoding.EncodeToString(b[:]), "=")
    if len(s) > 4 { s = s[:4] }
    return "NX-" + s
}
```

Tests:
- `TestNormalize` — trim + uppercase.
- `TestGenerate_HasPrefix_FourChars` — `strings.HasPrefix(out, "NX-") && len(out) == 7`.
- Integration test (Postgres testcontainer): mint → validate (200 with `remaining=1`) → redeem → validate (409 exhausted) → revoke → validate (409 exhausted, same path).

### Task 1.4: Handlers

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/invite.go`

```go
package handler

import (
    "encoding/json"
    "errors"
    "net/http"

    "github.com/go-chi/chi/v5"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    httpx "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

func InviteValidate(svc domain.InviteService) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var body struct{ Code string `json:"code"` }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
            httpx.WriteError(w, http.StatusBadRequest, "code required"); return
        }
        res, err := svc.Validate(r.Context(), body.Code)
        switch {
        case errors.Is(err, domain.ErrInviteNotFound):  httpx.WriteError(w, http.StatusNotFound,  "not_found"); return
        case errors.Is(err, domain.ErrInviteExpired):   httpx.WriteError(w, http.StatusGone,      "expired");   return
        case errors.Is(err, domain.ErrInviteExhausted): httpx.WriteError(w, http.StatusConflict,  "exhausted"); return
        case err != nil: httpx.WriteError(w, http.StatusInternalServerError, "internal"); return
        }
        httpx.WriteJSON(w, http.StatusOK, res)
    }
}

func InviteMint(svc domain.InviteService) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        p := httpx.PrincipalFromCtx(r.Context())
        var body struct {
            MaxUses   int     `json:"max_uses"`
            ExpiresAt *string `json:"expires_at"`
            Note      string  `json:"note"`
        }
        _ = json.NewDecoder(r.Body).Decode(&body)
        in := domain.MintInviteInput{MaxUses: body.MaxUses, Note: body.Note}
        if body.ExpiresAt != nil {
            t, err := httpx.ParseTime(*body.ExpiresAt)
            if err != nil { httpx.WriteError(w, http.StatusBadRequest, "expires_at"); return }
            in.ExpiresAt = &t
        }
        ic, err := svc.Mint(r.Context(), p, in)
        if err != nil { httpx.WriteError(w, http.StatusInternalServerError, "internal"); return }
        httpx.WriteJSON(w, http.StatusCreated, ic)
    }
}

func InviteList(svc domain.InviteService) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        p := httpx.PrincipalFromCtx(r.Context())
        status := r.URL.Query().Get("status")
        out, err := svc.List(r.Context(), p, status)
        if err != nil { httpx.WriteError(w, http.StatusInternalServerError, "internal"); return }
        httpx.WriteJSON(w, http.StatusOK, out)
    }
}

func InviteRevoke(svc domain.InviteService) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        p := httpx.PrincipalFromCtx(r.Context())
        code := chi.URLParam(r, "code")
        if err := svc.Revoke(r.Context(), p, code); err != nil {
            if errors.Is(err, domain.ErrInviteNotFound) { httpx.WriteError(w, http.StatusNotFound, "not_found"); return }
            httpx.WriteError(w, http.StatusInternalServerError, "internal"); return
        }
        w.WriteHeader(http.StatusNoContent)
    }
}
```

### Task 1.5: WorkOS callback hook for redemption

**Files:**
- Modify: `services/control-plane/internal/transport/http/handler/auth.go` (Phase 2's signup completion handler — extend with cookie + state-param fallback).

After the WorkOS user is created and the local `users` row exists, but before the session cookie is set, the handler reads:

```go
// inside the signup-complete handler, after we have a Principal:
inviteCookie, _ := r.Cookie("nexis_pending_invite")
inviteState := r.URL.Query().Get("invite") // WorkOS forwards state parameter

code := ""
switch {
case inviteCookie != nil && inviteCookie.Value != "":
    code = inviteCookie.Value
case inviteState != "":
    code = inviteState
}

if code != "" {
    if err := deps.Invite.Redeem(r.Context(), code); err != nil {
        // Log and continue; the cookie/state proved this user had a code
        // earlier — we don't fail signup because we couldn't atomically
        // decrement.
        slog.Warn("invite redeem failed at signup completion",
            "code", code, "error", err)
    }
    // Clear the cookie regardless.
    http.SetCookie(w, &http.Cookie{Name: "nexis_pending_invite", Path: "/", MaxAge: -1, HttpOnly: true})
}
```

### Task 1.6: Mount routes + admin middleware

**Files:**
- Modify: `services/control-plane/internal/transport/http/server.go`

Mount the unauthenticated validate route at the public root, mount the admin routes inside the owner-only sub-group (`g3` in the existing layout):

```go
r.Post("/v1/auth/invite/validate", handler.InviteValidate(deps.Invite))

g3.Post("/v1/admin/invite-codes",        handler.InviteMint(deps.Invite))
g3.Get("/v1/admin/invite-codes",         handler.InviteList(deps.Invite))
g3.Delete("/v1/admin/invite-codes/{code}", handler.InviteRevoke(deps.Invite))
```

Wire `deps.Invite` in `cmd/server/main.go`:

```go
inviteRepo := repo.NewInviteCodesRepo(pool)
inviteSvc  := invite.New(inviteRepo)
deps.Invite = inviteSvc
```

### Task 1.7: Web — sign-up gate + invite SDK + waitlist copy

**Files:**
- Create: `apps/web/lib/invite.ts`
- Modify: `apps/web/app/(auth)/sign-up/page.tsx`
- Modify: `apps/web/app/waitlist/page.tsx`
- Modify: `apps/web/proxy.ts`

`apps/web/lib/invite.ts`:

```ts
import { api } from "@/lib/api";

export interface ValidateResult { code: string; remaining: number }

export const invite = {
  async validate(code: string): Promise<ValidateResult> {
    return api.post("/v1/auth/invite/validate", { code });
  },
  async mint(input: { max_uses?: number; expires_at?: string | null; note?: string }) {
    return api.post("/v1/admin/invite-codes", input);
  },
  async list(status: string = "") {
    const q = status ? `?status=${encodeURIComponent(status)}` : "";
    return api.get(`/v1/admin/invite-codes${q}`);
  },
  async revoke(code: string) {
    return api.delete(`/v1/admin/invite-codes/${encodeURIComponent(code)}`);
  },
};
```

`apps/web/app/(auth)/sign-up/page.tsx` (server component — extends existing):

```tsx
import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { SignUpClient } from "./client";

const API = process.env.API_URL_INTERNAL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export default async function SignUpPage({ searchParams }: { searchParams: Promise<{ invite?: string }> }) {
  const sp = await searchParams;
  const c  = await cookies();
  const cookieInvite = c.get("nexis_pending_invite")?.value;

  const code = (sp.invite ?? cookieInvite ?? "").trim();
  if (!code) redirect("/waitlist?reason=no_invite");

  // Validate (idempotent, no side effect).
  const r = await fetch(`${API}/v1/auth/invite/validate`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ code }),
    cache: "no-store",
  });

  if (r.status === 404) redirect("/waitlist?reason=invalid_invite");
  if (r.status === 410) redirect("/waitlist?reason=expired_invite");
  if (r.status === 409) redirect("/waitlist?reason=exhausted_invite");
  if (!r.ok) redirect("/waitlist?reason=error");

  // Stash the code in a short-lived httpOnly cookie that the signup-completion
  // handler will read to redeem.
  c.set({
    name: "nexis_pending_invite",
    value: code,
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 300, // 5 minutes
  });

  return <SignUpClient inviteCode={code} />;
}
```

`apps/web/app/waitlist/page.tsx` — replace the body copy with the new wording (see spec §6.3). Add a `reason` reading from `searchParams` and render a toast-styled banner above the form:

```tsx
const REASON_COPY: Record<string, string> = {
  no_invite:        "NEXIS is in public beta. Drop your email and we'll send an invite when a slot opens.",
  invalid_invite:   "That invite code wasn't recognized. Want to join the waitlist?",
  expired_invite:   "That invite code has expired. We can send you a fresh one — leave your email.",
  exhausted_invite: "That invite code has already been used. Leave your email and we'll send you another.",
  error:            "Something went wrong validating your invite. Try the link again or join the waitlist.",
};
```

`apps/web/proxy.ts` — confirm `/sign-up` is in the public allowlist; add `?invite=` query passthrough if the existing path proxy strips query strings (Phase 1 doesn't, but verify).

### Task 1.8: Tests + smoke

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm typecheck

# Manual smoke
docker compose exec -T postgres psql -U nexis -d nexis -c \
  "INSERT INTO invite_codes (code, max_uses, used_count) VALUES ('NX-TEST', 1, 0);"
curl -s -X POST -H 'content-type: application/json' \
  -d '{"code":"NX-TEST"}' http://localhost:8080/v1/auth/invite/validate | jq
# expect {"code":"NX-TEST","remaining":1}

curl -s -X POST -H 'content-type: application/json' \
  -d '{"code":"NX-DOESNOTEXIST"}' http://localhost:8080/v1/auth/invite/validate
# expect 404
```

### Task 1.9: Commit

```bash
git add services/control-plane apps/web/app/\(auth\)/sign-up apps/web/app/waitlist apps/web/lib/invite.ts apps/web/proxy.ts
git commit -m "feat(invite): public-beta invite-code gate on signup + admin mint/list/revoke (stage 1)"
```

---

## Stage 2 — Sample-repo seeder + onboarding wizard wiring

### Task 2.1: Domain + usecase

**Files:**
- Create: `services/control-plane/internal/domain/sample_seed.go`
- Create: `services/control-plane/internal/usecase/sample_seed/seed.go`
- Create: `services/control-plane/internal/usecase/sample_seed/seed_test.go`

```go
// domain/sample_seed.go
package domain

import "context"

type SampleSeedService interface {
    Seed(ctx context.Context, p Principal, workspaceID string) (runID string, err error)
}
```

```go
// usecase/sample_seed/seed.go
package sample_seed

import (
    "context"
    _ "embed"
    "encoding/json"
    "fmt"
    "time"

    "github.com/google/uuid"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    "github.com/nexis-eco/nexis/services/control-plane/internal/usecase/workflow_kickoff"
)

// We embed the Phase 4 validator fixture so the seeder doesn't need MinIO/S3
// access. The fixture is small (~5 KB) and is the single canonical
// "synthetic incident" the docs + UI reference.
//
//go:embed fixture/null_deref_001.json
var fixtureIncident []byte

type Service struct {
    Incidents *repo.IncidentsRepo
    Pipelines workflow_kickoff.Service  // Phase 4/6 — Start(ctx, IncidentID) (runID, err)
}

func New(i *repo.IncidentsRepo, p workflow_kickoff.Service) *Service { return &Service{Incidents: i, Pipelines: p} }

func (s *Service) Seed(ctx context.Context, p domain.Principal, workspaceID string) (string, error) {
    // Decode fixture into a fresh incident, stamp the new tenant + workspace.
    var raw map[string]any
    if err := json.Unmarshal(fixtureIncident, &raw); err != nil {
        return "", fmt.Errorf("decode fixture: %w", err)
    }
    raw["incident_id"] = uuid.New().String()
    raw["received_at"] = time.Now().UTC().Format(time.RFC3339)

    incidentID, err := s.Incidents.InsertRaw(ctx, p.OrgID, workspaceID, raw)
    if err != nil { return "", fmt.Errorf("insert raw: %w", err) }

    runID, err := s.Pipelines.Start(ctx, p.OrgID, workspaceID, incidentID)
    if err != nil { return "", fmt.Errorf("start workflow: %w", err) }

    return runID, nil
}
```

Test sketch:
- `TestSeed_HappyPath` — mocks `Incidents.InsertRaw` + `Pipelines.Start`, asserts the returned `runID` is the workflow's id and that `incident_id` in the raw is a fresh UUID (not the fixture's static id).

Place the fixture file at `services/control-plane/internal/usecase/sample_seed/fixture/null_deref_001.json` — copy from `services/validator/fixtures/incidents/null_deref_001.json` (Phase 4).

### Task 2.2: Handler

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/sample_seed.go`

```go
package handler

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    httpx "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

func WorkspaceSeedSample(svc domain.SampleSeedService) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        p   := httpx.PrincipalFromCtx(r.Context())
        ws  := chi.URLParam(r, "ws")
        run, err := svc.Seed(r.Context(), p, ws)
        if err != nil {
            httpx.WriteError(w, http.StatusInternalServerError, err.Error()); return
        }
        _ = json.NewEncoder(w).Encode(map[string]string{"run_id": run})
        w.WriteHeader(http.StatusAccepted)
    }
}
```

Mount inside the owner|admin sub-group (`g2`):

```go
g2.Post("/v1/workspaces/{ws}/seed-sample", handler.WorkspaceSeedSample(deps.SampleSeed))
```

Wire `deps.SampleSeed` in `cmd/server/main.go`.

### Task 2.3: Web — onboarding wizard middle step

**Files:**
- Create: `apps/web/components/onboarding/SampleRepoStep.tsx`
- Modify: `apps/web/app/onboarding/workspace/client.tsx`
- Create: `apps/web/lib/seed.ts`

`lib/seed.ts`:

```ts
import { api } from "@/lib/api";

export async function seedSample(workspaceId: string): Promise<{ run_id: string }> {
  return api.post(`/v1/workspaces/${workspaceId}/seed-sample`, {});
}
```

`components/onboarding/SampleRepoStep.tsx`:

```tsx
"use client";

import * as React from "react";
import { Button } from "@/components/ui/Button";

interface Props {
  onSample: () => void;
  onByo: () => void;
  busy?: boolean;
}

export function SampleRepoStep({ onSample, onByo, busy }: Props) {
  return (
    <div className="space-y-6">
      <header>
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          One last step
        </p>
        <h1 className="mt-1 text-2xl font-semibold text-[var(--color-foreground)]">
          Try a sample repo or connect your own
        </h1>
      </header>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <button
          type="button"
          disabled={busy}
          onClick={onSample}
          className="rounded-2xl border border-[var(--color-border)] bg-white p-6 text-left transition hover:border-[var(--color-brand-primary)] focus:outline-none focus:ring-2 focus:ring-[var(--color-brand-primary)]"
        >
          <span className="inline-flex items-center gap-2 rounded-full bg-[var(--color-brand-accent)]/30 px-2 py-0.5 text-xs font-medium text-[var(--color-brand-primary)]">
            Recommended
          </span>
          <h3 className="mt-3 text-base font-semibold text-[var(--color-foreground)]">
            Try a sample repo
          </h3>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            Skip integrations. We'll seed a synthetic incident from our test repo and walk you through a real auto-recovery in under 90 seconds.
          </p>
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={onByo}
          className="rounded-2xl border border-[var(--color-border)] bg-white p-6 text-left transition hover:border-[var(--color-brand-primary)] focus:outline-none focus:ring-2 focus:ring-[var(--color-brand-primary)]"
        >
          <h3 className="text-base font-semibold text-[var(--color-foreground)]">
            Bring your own repo
          </h3>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            Install the NEXIS GitHub App on the repo you care about. We'll show you how to wire Sentry next.
          </p>
        </button>
      </div>
    </div>
  );
}
```

`apps/web/app/onboarding/workspace/client.tsx` — extend the existing state machine:

```tsx
type Phase =
  | { kind: "form"; error: string | null }
  | { kind: "sample_or_byo"; ws: Workspace }
  | { kind: "running"; ws: Workspace; sampleRunId?: string }
  | { kind: "byo"; ws: Workspace };
```

After the existing `form → running` transition, when the SSE animation fires its `ready` event, branch:

```tsx
function onReady() {
  setPhase({ kind: "sample_or_byo", ws: phase.ws });
}

async function chooseSample() {
  const ws = phase.ws;
  setBusy(true);
  try {
    const { run_id } = await seedSample(ws.id);
    setPhase({ kind: "running", ws, sampleRunId: run_id });
    // animation handles SSE on the new run id; on its second `ready`/`completed`
    // the client navigates to /console/incidents/{run_id}.
  } catch (err) {
    // surface error toast but stay on this step
  } finally { setBusy(false); }
}

function chooseByo() {
  router.replace("/console/integrations/github");
}
```

`ProvisioningAnimation.tsx` already subscribes to `/v1/workspaces/:id/events` for the workspace; we now reuse the same component pointed at the pipeline-run SSE (`/v1/workspaces/:ws/pipelines/:run/events`). Inline the `endpoint` prop and let the parent thread the URL.

### Task 2.4: Tests + smoke

```bash
cd services/control-plane && make build && make test
cd apps/web && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm test

# Smoke: complete signup → workspace → pick sample → confirm run is queued
WS=$(curl -s -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/workspaces | jq -r '.[0].id')
RUN=$(curl -s -X POST -H "Cookie: nexis_session=$CK" -H 'content-type: application/json' \
  "http://localhost:8080/v1/workspaces/$WS/seed-sample" | jq -r '.run_id')
echo "Started run $RUN"
curl -s -H "Cookie: nexis_session=$CK" \
  "http://localhost:8080/v1/workspaces/$WS/pipelines/$RUN" | jq '.status'
```

### Task 2.5: Commit

```bash
git add services/control-plane apps/web/app/onboarding apps/web/components/onboarding apps/web/lib/seed.ts
git commit -m "feat(onboarding): sample-repo wizard step + seed-sample endpoint (stage 2)"
```

---

## Stage 3 — Shepherd.js in-app tour

### Task 3.1: Library install

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm add shepherd.js@^14
```

### Task 3.2: Tour provider + steps

**Files:**
- Create: `apps/web/components/tour/TourProvider.tsx`
- Create: `apps/web/components/tour/steps.ts`
- Create: `apps/web/components/tour/tour.css`
- Create: `apps/web/lib/tour.ts`

`apps/web/lib/tour.ts`:

```ts
import { api } from "@/lib/api";

export async function markTourCompleted(): Promise<void> {
  await api.patch("/v1/me/preferences", { tour_completed: true });
}
```

`components/tour/steps.ts` — verbatim from spec Appendix C.

`components/tour/TourProvider.tsx`:

```tsx
"use client";

import * as React from "react";
import { markTourCompleted } from "@/lib/tour";

interface Me { preferences?: { tour_completed?: boolean } }

export function TourProvider({ me }: { me: Me }) {
  React.useEffect(() => {
    if (me.preferences?.tour_completed) return;

    let cancelled = false;
    (async () => {
      const [{ default: Shepherd }, { TOUR_STEPS }] = await Promise.all([
        import("shepherd.js"),
        import("./steps"),
      ]);
      // ensure the bundled css is on the page
      await import("./tour.css");
      if (cancelled) return;

      const tour = new Shepherd.Tour({
        useModalOverlay: true,
        defaultStepOptions: {
          cancelIcon: { enabled: true },
          classes: "nexis-tour-step",
          scrollTo: { behavior: window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth", block: "center" },
        },
      });

      TOUR_STEPS.forEach((s) => tour.addStep(s));
      const finish = () => { markTourCompleted().catch(() => {}); };
      tour.on("complete", finish);
      tour.on("cancel", finish);
      // Defer start so the layout has painted.
      setTimeout(() => tour.start(), 250);
    })();

    return () => { cancelled = true; };
  }, [me.preferences?.tour_completed]);

  return null;
}
```

`components/tour/tour.css` — minimal overrides:

```css
.nexis-tour-step {
  --shepherd-bg: var(--color-card);
  --shepherd-text-color: var(--color-foreground);
}
.nexis-tour-step .shepherd-content,
.nexis-tour-step .shepherd-header {
  background: var(--shepherd-bg) !important;
  color: var(--shepherd-text-color) !important;
}
.nexis-tour-step .shepherd-button {
  background: var(--color-brand-primary);
  color: white;
  border-radius: 6px;
  padding: 6px 12px;
}
.nexis-tour-step .shepherd-button-secondary {
  background: transparent;
  color: var(--color-muted-foreground);
}
```

### Task 3.3: Mount the provider + add `data-tour` attributes

**Files:**
- Modify: `apps/web/app/(app)/console/layout.tsx`
- Modify: `apps/web/components/console/Topbar.tsx`
- Modify: `apps/web/components/console/Sidebar.tsx`
- Modify: `apps/web/app/(app)/console/page.tsx`

`layout.tsx` — render the provider after the existing `<Sidebar/>+<Topbar/>` chrome:

```tsx
import { TourProvider } from "@/components/tour/TourProvider";

// inside the layout server component, after fetching `me`:
return (
  <>
    <Sidebar />
    <Topbar />
    <main>{children}</main>
    <TourProvider me={me} />
  </>
);
```

Attributes — sprinkle through existing components:

* `apps/web/components/console/Topbar.tsx` — root element: `data-tour="topbar"`.
* `apps/web/components/console/Sidebar.tsx` — root: `data-tour="sidebar"`; each nav `<Link>` gets `data-tour="nav-{slug}"` where slug ∈ `{home, incidents, approvals, audit, agents, integrations, live-demo, settings}`.
* `apps/web/app/(app)/console/page.tsx` — the primary CTA button on the Home empty state gets `data-tour="cta-sample-incident"`.

### Task 3.4: Re-launch from Help menu

**Files:**
- Modify: `apps/web/components/console/Topbar.tsx` (Help popover)
- Modify: `apps/web/lib/tour.ts`

Add a `resetAndLaunchTour()` helper:

```ts
import { api } from "@/lib/api";

export async function resetAndLaunchTour(): Promise<void> {
  await api.patch("/v1/me/preferences", { tour_completed: false });
  // hard reload so TourProvider re-mounts and reads the new prefs
  if (typeof window !== "undefined") window.location.assign("/console?tour=replay");
}
```

Wire into the Topbar Help popover as a new menu item: "Restart tour".

### Task 3.5: Smoke

```bash
cd apps/web && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
# Visual smoke — open /console in a freshly-cleared session; tour should fire.
```

### Task 3.6: Commit

```bash
git add apps/web/components/tour apps/web/lib/tour.ts apps/web/components/console apps/web/app/\(app\)/console apps/web/package.json apps/web/pnpm-lock.yaml
git commit -m "feat(tour): shepherd.js 6-step console tour + restart from help (stage 3)"
```

---

## Stage 4 — Empty-state CTAs everywhere

### Task 4.1: Component

**Files:**
- Create: `apps/web/components/empty/EmptyState.tsx`
- Create: `apps/web/components/empty/illustrations/index.ts`

```tsx
// EmptyState.tsx
import * as React from "react";
import Link from "next/link";
import { Button } from "@/components/ui/Button";
import { illustrations } from "./illustrations";

export interface EmptyStateProps {
  illustration: keyof typeof illustrations;
  title: string;
  body: string;
  cta?: { label: string; href?: string; onClick?: () => void; variant?: "primary"|"ghost" };
  secondary?: { label: string; href: string };
}

export function EmptyState({ illustration, title, body, cta, secondary }: EmptyStateProps) {
  const Illustration = illustrations[illustration];
  return (
    <div className="mx-auto flex max-w-md flex-col items-center px-6 py-16 text-center">
      <Illustration className="h-32 w-32 text-[var(--color-brand-accent)]" aria-hidden />
      <h3 className="mt-6 text-base font-semibold text-[var(--color-foreground)]">{title}</h3>
      <p className="mt-2 text-sm text-[var(--color-muted-foreground)]">{body}</p>
      <div className="mt-6 flex items-center gap-3">
        {cta ? (
          cta.href ? (
            <Button asChild variant={cta.variant ?? "primary"}><Link href={cta.href}>{cta.label}</Link></Button>
          ) : (
            <Button onClick={cta.onClick} variant={cta.variant ?? "primary"}>{cta.label}</Button>
          )
        ) : null}
        {secondary ? (
          <Link href={secondary.href} className="text-sm text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]">
            {secondary.label}
          </Link>
        ) : null}
      </div>
    </div>
  );
}
```

`illustrations/index.ts` — maps slugs to existing SVGs in `apps/web/public/illustrations/` (Phase 3 set). If any of the 10 slugs in spec §5.3 is missing, copy a placeholder from the existing set; the design is intentionally consistent line-art.

```ts
import telescope from "./telescope.svg";
import checkmarkCluster from "./checkmark-cluster.svg";
// ...

export const illustrations = {
  telescope, "checkmark-cluster": checkmarkCluster,
  scroll: scrollSvg, plug, "key-ring": keyRing,
  globe, receipt, people, antenna, playback,
} as const;
```

### Task 4.2: Replace per-surface empty states

For each surface, replace the existing zero-data branch with `<EmptyState />`. List of files:

- `apps/web/app/(app)/console/incidents/page.tsx`
- `apps/web/app/(app)/console/approvals/page.tsx`
- `apps/web/app/(app)/console/audit/page.tsx`
- `apps/web/app/(app)/console/integrations/page.tsx`
- `apps/web/app/(app)/console/settings/api-keys/page.tsx`
- `apps/web/app/(app)/console/settings/workspaces/page.tsx`
- `apps/web/app/(app)/console/settings/billing/page.tsx`
- `apps/web/app/(app)/console/settings/members/page.tsx`
- `apps/web/app/(app)/console/agents/page.tsx`
- `apps/web/app/(app)/console/live-demo/page.tsx`

Example wire-up (Incidents):

```tsx
{rows.length === 0 ? (
  <EmptyState
    illustration="telescope"
    title="No incidents yet"
    body="When an integration ingests its first event we'll classify and route it here."
    cta={{ label: "Run a synthetic incident", onClick: () => seedSample(currentWorkspaceId).then(r => router.push(`/console/incidents/${r.run_id}`)) }}
    secondary={{ label: "Connect Sentry", href: "/console/integrations/sentry" }}
  />
) : (
  <DataTable rows={rows} columns={cols} />
)}
```

### Task 4.3: CI grep guard

Add a CI step that grep-fails the build if "No data" appears in any rendered string in `apps/web/app/(app)/console/`:

`.github/workflows/web-ci.yml` addition:

```yaml
- name: Forbid bare-text empty states
  run: |
    if grep -RIn --include='*.tsx' '"No data"' apps/web/app/\(app\)/console; then
      echo "Bare 'No data' empty state found. Use <EmptyState />."
      exit 1
    fi
```

### Task 4.4: Commit

```bash
git add apps/web/components/empty apps/web/app/\(app\)/console .github/workflows/web-ci.yml
git commit -m "feat(empty): EmptyState component + per-surface CTAs + CI guard against 'No data' (stage 4)"
```

---

## Stage 5 — NASA-TLX modal + handler + Google Forms mirror

### Task 5.1: Repo + handler

**Files:**
- Create: `services/control-plane/internal/adapter/repo/nasa_tlx_repo.go`
- Create: `services/control-plane/internal/transport/http/handler/nasa_tlx.go`

```go
// nasa_tlx_repo.go
package repo

import (
    "context"

    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

type TLXRecord struct {
    ID                                                  string
    UserID, OrgID                                       string
    RecoveryRunID                                       *string
    MentalDemand, PhysicalDemand, TemporalDemand        int
    Performance, Effort, Frustration                    int
    Notes                                               string
}

type NASATLXRepo struct{ pool *pgxpool.Pool }

func NewNASATLXRepo(p *pgxpool.Pool) *NASATLXRepo { return &NASATLXRepo{pool: p} }

func (r *NASATLXRepo) Insert(ctx context.Context, rec *TLXRecord) error {
    q := db.FromCtx(ctx, r.pool)
    return q.QueryRow(ctx, `
        INSERT INTO nasa_tlx_responses (user_id, org_id, recovery_run_id,
            mental_demand, physical_demand, temporal_demand,
            performance, effort, frustration, notes)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
        RETURNING id`,
        rec.UserID, rec.OrgID, rec.RecoveryRunID,
        rec.MentalDemand, rec.PhysicalDemand, rec.TemporalDemand,
        rec.Performance, rec.Effort, rec.Frustration, rec.Notes,
    ).Scan(&rec.ID)
}

// SelectByOrg used by the eval-export.
func (r *NASATLXRepo) SelectByOrg(ctx context.Context, orgID string) ([]TLXRecord, error) {
    q := db.FromCtx(ctx, r.pool)
    rows, err := q.Query(ctx, `
        SELECT id, user_id, org_id, recovery_run_id,
               mental_demand, physical_demand, temporal_demand,
               performance, effort, frustration, COALESCE(notes,'')
        FROM nasa_tlx_responses WHERE org_id=$1 ORDER BY created_at DESC`, orgID)
    if err != nil { return nil, err }
    defer rows.Close()
    out := []TLXRecord{}
    for rows.Next() {
        var t TLXRecord
        if err := rows.Scan(&t.ID, &t.UserID, &t.OrgID, &t.RecoveryRunID,
            &t.MentalDemand, &t.PhysicalDemand, &t.TemporalDemand,
            &t.Performance, &t.Effort, &t.Frustration, &t.Notes); err != nil { return nil, err }
        out = append(out, t)
    }
    return out, rows.Err()
}

// unused imports placeholder
var _ = domain.ErrNotFound
```

```go
// handler/nasa_tlx.go
package handler

import (
    "context"
    "encoding/json"
    "log/slog"
    "net/http"
    "net/url"
    "strconv"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
    httpx "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

type tlxBody struct {
    MentalDemand   int     `json:"mental_demand"`
    PhysicalDemand int     `json:"physical_demand"`
    TemporalDemand int     `json:"temporal_demand"`
    Performance    int     `json:"performance"`
    Effort         int     `json:"effort"`
    Frustration    int     `json:"frustration"`
    Notes          string  `json:"notes"`
    RunID          *string `json:"recovery_run_id"`
}

func NASATLXSubmit(r *repo.NASATLXRepo, cfg *config.Config, http_ *http.Client) http.HandlerFunc {
    return func(w http.ResponseWriter, req *http.Request) {
        p := httpx.PrincipalFromCtx(req.Context())
        var body tlxBody
        if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
            httpx.WriteError(w, http.StatusBadRequest, "json"); return
        }
        if !valid(body) {
            httpx.WriteError(w, http.StatusBadRequest, "range 0..20"); return
        }
        rec := &repo.TLXRecord{
            UserID: p.UserID, OrgID: p.OrgID, RecoveryRunID: body.RunID,
            MentalDemand: body.MentalDemand, PhysicalDemand: body.PhysicalDemand,
            TemporalDemand: body.TemporalDemand, Performance: body.Performance,
            Effort: body.Effort, Frustration: body.Frustration,
            Notes: body.Notes,
        }
        if err := r.Insert(req.Context(), rec); err != nil {
            httpx.WriteError(w, http.StatusInternalServerError, "insert"); return
        }

        // Fire-and-forget mirror to Google Forms.
        if cfg.NASATLXGoogleFormURL != "" {
            go mirrorToGoogleForm(cfg.NASATLXGoogleFormURL, http_, body)
        }

        httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": rec.ID})
    }
}

func mirrorToGoogleForm(formURL string, http_ *http.Client, b tlxBody) {
    form := url.Values{
        "entry.1": []string{strconv.Itoa(b.MentalDemand)},
        "entry.2": []string{strconv.Itoa(b.PhysicalDemand)},
        "entry.3": []string{strconv.Itoa(b.TemporalDemand)},
        "entry.4": []string{strconv.Itoa(b.Performance)},
        "entry.5": []string{strconv.Itoa(b.Effort)},
        "entry.6": []string{strconv.Itoa(b.Frustration)},
        "entry.7": []string{b.Notes},
    }
    ctx, cancel := context.WithTimeout(context.Background(), 10_000_000_000) // 10s
    defer cancel()
    rq, _ := http.NewRequestWithContext(ctx, http.MethodPost, formURL, nil)
    rq.PostForm = form
    rq.Header.Set("content-type", "application/x-www-form-urlencoded")
    if _, err := http_.Do(rq); err != nil {
        slog.Warn("nasa-tlx google-form mirror failed", "err", err)
    }
}

func valid(b tlxBody) bool {
    for _, v := range []int{b.MentalDemand, b.PhysicalDemand, b.TemporalDemand, b.Performance, b.Effort, b.Frustration} {
        if v < 0 || v > 20 { return false }
    }
    return true
}
```

Mount in the protected group (any authenticated member):

```go
g.Post("/v1/admin/nasa-tlx", handler.NASATLXSubmit(deps.NASATLX, deps.Config, deps.HTTPClient))
```

### Task 5.2: `/v1/me` extension — `should_prompt_tlx`

**Files:**
- Modify: `services/control-plane/internal/transport/http/handler/me.go`

```go
// inside the existing GET /v1/me response builder, after computing org+prefs:
var alreadyPrompted bool
if v, ok := me.Preferences["tlx_prompted_at"]; ok && v != nil {
    alreadyPrompted = true
}
var recoveries int
_ = pool.QueryRow(ctx, `SELECT successful_recoveries_count FROM organizations WHERE id=$1`, p.OrgID).Scan(&recoveries)
shouldPrompt := !alreadyPrompted && recoveries >= 3
resp["should_prompt_tlx"] = shouldPrompt
```

### Task 5.3: Web — TLX modal + SDK

**Files:**
- Create: `apps/web/components/nasa_tlx/TlxModal.tsx`
- Create: `apps/web/lib/nasa_tlx.ts`
- Modify: `apps/web/app/(app)/console/layout.tsx` (mount the modal)
- Modify: `apps/web/components/console/Topbar.tsx` (add "Give feedback" Help menu item)

`lib/nasa_tlx.ts`:

```ts
import { api } from "@/lib/api";

export interface TlxPayload {
  mental_demand: number; physical_demand: number; temporal_demand: number;
  performance: number; effort: number; frustration: number;
  notes?: string; recovery_run_id?: string;
}

export const nasaTlx = {
  submit(p: TlxPayload) { return api.post("/v1/admin/nasa-tlx", p); },
};
```

`TlxModal.tsx` — six labeled sliders + textarea + submit/dismiss. Sketch:

```tsx
"use client";

import * as React from "react";
import { Dialog } from "@/components/ui/Dialog";
import { Slider } from "@/components/ui/Slider";
import { Button } from "@/components/ui/Button";
import { Textarea } from "@/components/ui/Textarea";
import { nasaTlx, type TlxPayload } from "@/lib/nasa_tlx";
import { markTlxPrompted } from "@/lib/me";

const SUBSCALES = [
  { key: "mental_demand",   label: "Mental Demand",   wording: "How much mental and perceptual activity was required..." },
  { key: "physical_demand", label: "Physical Demand", wording: "How much physical activity was required..." },
  { key: "temporal_demand", label: "Temporal Demand", wording: "How much time pressure did you feel..." },
  { key: "performance",     label: "Performance",     wording: "How successful do you think you were...", invertedLabels: true },
  { key: "effort",          label: "Effort",          wording: "How hard did you have to work..." },
  { key: "frustration",     label: "Frustration",     wording: "How insecure, discouraged, irritated..." },
] as const;

interface Props { open: boolean; recoveryRunId?: string; onClose: () => void }

export function TlxModal({ open, recoveryRunId, onClose }: Props) {
  const [values, setValues] = React.useState<Record<string, number>>(
    Object.fromEntries(SUBSCALES.map(s => [s.key, 10])),
  );
  const [notes, setNotes] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => { if (open) void markTlxPrompted(); }, [open]);

  async function submit() {
    setBusy(true);
    try {
      const p = { ...values, notes, recovery_run_id: recoveryRunId } as TlxPayload;
      await nasaTlx.submit(p);
      onClose();
    } finally { setBusy(false); }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <Dialog.Title>How did that feel?</Dialog.Title>
      <Dialog.Description>
        Six questions, ~30 seconds. Your responses inform a published university study and never identify you publicly. <a className="underline" href="#" onClick={(e)=>{e.preventDefault();/* show why-we-ask modal */}}>Why we ask</a>.
      </Dialog.Description>
      <div className="space-y-6">
        {SUBSCALES.map(s => (
          <div key={s.key}>
            <label className="text-sm font-medium">{s.label}</label>
            <p className="text-xs text-[var(--color-muted-foreground)]">{s.wording}</p>
            <Slider min={0} max={20} step={1} value={[values[s.key]]} onValueChange={(v)=>setValues(x=>({...x,[s.key]:v[0]}))} />
            <div className="mt-1 flex justify-between text-[10px] text-[var(--color-muted-foreground)]">
              <span>{s.invertedLabels ? "Perfect" : "Very Low"}</span>
              <span>{s.invertedLabels ? "Failure" : "Very High"}</span>
            </div>
          </div>
        ))}
        <Textarea placeholder="Anything else?" value={notes} onChange={e=>setNotes(e.target.value)} />
      </div>
      <Dialog.Footer>
        <Button variant="ghost" onClick={onClose}>Not now</Button>
        <Button onClick={submit} disabled={busy}>Submit</Button>
      </Dialog.Footer>
    </Dialog>
  );
}
```

`apps/web/lib/me.ts` adds:

```ts
export async function markTlxPrompted(): Promise<void> {
  await api.patch("/v1/me/preferences", { tlx_prompted_at: new Date().toISOString() });
}
```

`apps/web/app/(app)/console/layout.tsx` — wire `should_prompt_tlx` from the server-rendered `me` into a client wrapper that mounts the modal once:

```tsx
const me = await fetchMe();
// ...
<TlxLauncher should={me.should_prompt_tlx} recoveryRunId={me.last_successful_run_id} />
```

Where `TlxLauncher` is a tiny client component that opens the modal on mount when `should=true`.

Topbar help menu — add `Restart tour`, `Give feedback (NASA-TLX)`, `Status page → status.nexis.dev`, `Docs → docs.nexis.dev`.

### Task 5.4: Smoke

```bash
# Manually bump the recovery count, then visit /console.
docker compose exec -T postgres psql -U nexis -d nexis -c \
  "UPDATE organizations SET successful_recoveries_count=3 WHERE id='$ORG';"
# Hard refresh /console — modal should appear.
```

### Task 5.5: Commit

```bash
git add services/control-plane apps/web/components/nasa_tlx apps/web/lib/nasa_tlx.ts apps/web/lib/me.ts apps/web/app/\(app\)/console
git commit -m "feat(nasa-tlx): post-3rd-recovery modal + handler + google-form mirror (stage 5)"
```

---

## Stage 6 — Admin invite-codes surface

### Task 6.1: Page + components

**Files:**
- Create: `apps/web/app/(app)/console/admin/invite-codes/page.tsx`
- Create: `apps/web/components/invite/InviteCodeForm.tsx`
- Create: `apps/web/components/invite/InviteCodeRow.tsx`

`page.tsx` (server component):

```tsx
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { listCodesServerSide } from "@/lib/invite.server";
import { ClientView } from "./client";

export default async function InviteCodesAdminPage() {
  const me = await (await fetch(`${process.env.API_URL_INTERNAL}/v1/me`, {
    headers: { cookie: (await cookies()).toString() }, cache: "no-store",
  })).json();
  if (me.role !== "owner") redirect("/console?error=forbidden");
  const codes = await listCodesServerSide("");
  return <ClientView codes={codes} />;
}
```

`client.tsx`:

```tsx
"use client";

import * as React from "react";
import { invite } from "@/lib/invite";
import { InviteCodeForm } from "@/components/invite/InviteCodeForm";
import { InviteCodeRow } from "@/components/invite/InviteCodeRow";

interface InviteCode { code: string; max_uses: number; used_count: number; expires_at?: string|null; note?: string; created_at: string; }

export function ClientView({ codes: initial }: { codes: InviteCode[] }) {
  const [codes, setCodes] = React.useState(initial);
  const [filter, setFilter] = React.useState("");

  async function refresh() { setCodes(await invite.list(filter)); }
  async function mint(input: Parameters<typeof invite.mint>[0]) {
    const c = await invite.mint(input);
    setCodes([c, ...codes]);
  }
  async function mintBulk() {
    for (let i = 0; i < 10; i++) await mint({ max_uses: 1, note: "bulk" });
  }
  async function revoke(code: string) {
    await invite.revoke(code);
    await refresh();
  }

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Invite codes</h1>
        <div className="flex gap-2">
          <Button onClick={mintBulk} variant="ghost">Mint 10</Button>
        </div>
      </header>
      <InviteCodeForm onMint={mint} />
      <div className="flex gap-2">
        {["", "unused", "partial", "exhausted", "expired"].map(s => (
          <Button key={s} variant={filter === s ? "primary" : "ghost"} onClick={() => { setFilter(s); refresh(); }}>
            {s || "all"}
          </Button>
        ))}
      </div>
      <ul className="divide-y divide-[var(--color-border)] rounded-2xl border border-[var(--color-border)] bg-white">
        {codes.map(c => <InviteCodeRow key={c.code} code={c} onRevoke={() => revoke(c.code)} />)}
      </ul>
    </div>
  );
}
```

`InviteCodeForm.tsx` — `max_uses` (number), `expires_at` (datetime-local), `note` (text), Submit.

`InviteCodeRow.tsx` — copy-on-click pill for the code; usage counter (`2 / 5`); revoke button.

### Task 6.2: Smoke + commit

```bash
# Visit /console/admin/invite-codes as owner; mint 1, verify it appears.
curl -s -H "Cookie: nexis_session=$OWNER_CK" \
  -H 'content-type: application/json' -d '{"max_uses":2}' \
  http://localhost:8080/v1/admin/invite-codes | jq

git add apps/web/app/\(app\)/console/admin apps/web/components/invite apps/web/lib/invite.ts
git commit -m "feat(admin): invite-codes admin surface + mint/list/revoke flow (stage 6)"
```

---

## Stage 7 — Evaluation lock-in (frozen v1)

### Task 7.1: Freeze scenarios

**Files:**
- Create: `apps/web/eval-scenarios-v1/` (copy from Phase 5's scenarios directory)
- Create: `apps/web/eval-scenarios-v1/README.md` (provenance + change-control)

```bash
mkdir -p /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/eval-scenarios-v1
cp -r /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/scripts/eval/scenarios/* \
      /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/eval-scenarios-v1/
```

`README.md` records the freeze date, scenario count, seed range, and a "DO NOT MODIFY — bump to v2 instead" note. Reviewed at code review time.

### Task 7.2: Three new eval baselines

**Files:**
- Create: `services/control-plane/internal/adapter/llm/eval_openai_single.go`
- Create: `services/control-plane/internal/adapter/llm/eval_ollama_single.go`
- Modify: `apps/web/scripts/eval/run.ts`

`eval_openai_single.go` is a thin wrapper around the existing OpenAI provider: one shot, no validator gate. The provider is selected by `LLM_PROVIDER=openai-eval`. Mirror for `LLM_PROVIDER=ollama-eval`.

`run.ts` adds three baseline modes:

```ts
const BASELINES = ["nexis", "openai-only", "ollama-only", "human-only"] as const;
type Baseline = typeof BASELINES[number];

async function runMatrix() {
  const scenarios = await loadScenarios("apps/web/eval-scenarios-v1");
  const rows: any[] = [];
  for (const baseline of BASELINES) {
    if (baseline === "human-only") {
      console.log(`# human-only: export incidents and record results manually`);
      await exportHumanOnly(scenarios);
      continue;
    }
    for (const s of scenarios) {
      for (let seed = 0; seed < 5; seed++) {
        const t0 = Date.now();
        const result = await runOne(baseline, s, seed);
        rows.push({ baseline, scenario: s.id, seed, mttr_seconds: (Date.now()-t0)/1000, ...result });
      }
    }
  }
  await fs.promises.writeFile("apps/web/eval-runs-v1.jsonl", rows.map(r => JSON.stringify(r)).join("\n"));
}
```

### Task 7.3: Stats + freeze

Add a Python script at `apps/web/scripts/eval/freeze.py` that reads `eval-runs-v1.jsonl`, computes per-baseline p50/p95 MTTR, patch-acceptance, fix-precision, fix-recall, cost, and runs the Wilcoxon + McNemar tests. Writes `docs/eval-results-v1.json` matching Appendix D's shape.

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm tsx scripts/eval/run.ts
python apps/web/scripts/eval/freeze.py
```

### Task 7.4: Eval export endpoint

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/eval_export.go`

```go
package handler

import (
    "encoding/csv"
    "net/http"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
    httpx "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

func EvalExport(runs *repo.PipelineRunsRepo, tlx *repo.NASATLXRepo) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        p := httpx.PrincipalFromCtx(r.Context())
        include := r.URL.Query().Get("include")  // "runs,tlx"
        w.Header().Set("content-type", "text/csv")
        cw := csv.NewWriter(w)
        defer cw.Flush()

        if include == "" || include == "runs" || include == "runs,tlx" {
            _ = cw.Write([]string{"section", "id", "scenario", "seed", "mttr_seconds", "patch_acceptance", "cost_usd"})
            rows, _ := runs.ListEvalRows(r.Context(), p.OrgID)
            for _, row := range rows {
                _ = cw.Write([]string{"run", row.ID, row.Scenario, row.Seed, row.MTTRSeconds, row.PatchAcceptance, row.CostUSD})
            }
        }
        if include == "tlx" || include == "runs,tlx" {
            _ = cw.Write([]string{"section", "user", "mental", "physical", "temporal", "performance", "effort", "frustration", "notes"})
            rows, _ := tlx.SelectByOrg(r.Context(), p.OrgID)
            for _, row := range rows {
                _ = cw.Write([]string{"tlx", row.UserID,
                    itoa(row.MentalDemand), itoa(row.PhysicalDemand), itoa(row.TemporalDemand),
                    itoa(row.Performance), itoa(row.Effort), itoa(row.Frustration), row.Notes})
            }
        }
    }
}
```

Mount in owner-only sub-group:

```go
g3.Get("/v1/admin/eval-export", handler.EvalExport(deps.Runs, deps.NASATLX))
```

### Task 7.5: Commit

```bash
git add apps/web/eval-scenarios-v1 apps/web/eval-runs-v1.jsonl apps/web/scripts/eval docs/eval-results-v1.json services/control-plane
git commit -m "feat(eval): freeze scenarios v1 + 4-baseline matrix + eval-export CSV (stage 7)"
```

---

## Stage 8 — Docs site (`apps/docs/`)

### Task 8.1: Scaffold

**Files:**
- Create: `apps/docs/package.json`
- Create: `apps/docs/next.config.mjs`
- Create: `apps/docs/theme.config.tsx`
- Create: `apps/docs/tsconfig.json`
- Modify: `pnpm-workspace.yaml`

```bash
mkdir -p /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/docs/content/{install,integrations,agents,runbook,architecture}
mkdir -p /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/docs/public
mkdir -p /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/docs/components
mkdir -p /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/docs/data
```

`apps/docs/package.json`:

```json
{
  "name": "@nexis/docs",
  "private": true,
  "version": "0.1.0",
  "scripts": {
    "dev": "next dev -p 3001",
    "build": "next build",
    "start": "next start -p 3001",
    "lint": "next lint"
  },
  "dependencies": {
    "next": "16.2.2",
    "nextra": "^4.0.0",
    "nextra-theme-docs": "^4.0.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0"
  },
  "devDependencies": {
    "@types/node": "^20.0.0",
    "@types/react": "^19.0.0",
    "typescript": "^5.6.0"
  }
}
```

`next.config.mjs` — exact contents from spec §7.1.

`theme.config.tsx`:

```tsx
import type { DocsThemeConfig } from "nextra-theme-docs";

const config: DocsThemeConfig = {
  logo: <span style={{ fontWeight: 700 }}>NEXIS docs</span>,
  project: { link: "https://github.com/nexis-eco/nexis" },
  docsRepositoryBase: "https://github.com/nexis-eco/nexis/tree/main/apps/docs",
  footer: { text: "© NEXIS 2026 · Public beta" },
  primaryHue: 217,
  primarySaturation: 91,
  feedback: { content: null },
  search: { placeholder: "Search docs" },
  toc: { backToTop: true },
};
export default config;
```

`apps/docs/tsconfig.json` — extends `apps/web/tsconfig.json` with `"jsx": "preserve"` and `include: ["**/*.ts","**/*.tsx","**/*.mdx"]`.

`pnpm-workspace.yaml` — already covers `apps/*`; no edit needed.

### Task 8.2: Content

Stub every page with at least a heading + 200 words of placeholder copy lifted from the existing specs. The 9 Agent pages embed `<AgentCard agent="..." />`. The Architecture/Evaluation page embeds `<EvalTable src="/eval-results-v1.json" />`.

```bash
# Copy the eval JSON into the docs public dir at build time.
ln -sf /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/docs/eval-results-v1.json \
       /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/docs/public/eval-results-v1.json
```

Each MDX file follows the same shape:

```mdx
# GitHub integration

import { Callout, Tabs } from "nextra/components";

<Callout type="info">
NEXIS installs as a GitHub App (`nexis-bot`). It needs `repo`, `pull_request`, `checks`, `contents:write`, and `actions:read`.
</Callout>

## Install

1. Visit `/console/integrations/github`.
2. Click **Install on a repo** → you'll be redirected to GitHub.
3. Pick the repo(s) and confirm.
4. We store the `installation_id` in our `integrations` table (RLS-scoped to your org).

## How NEXIS uses the GitHub App

...
```

The full content map is in spec §3.1. The 10 runbook pages are dictated by the top-10 launch-week incidents we expect — we list the obvious ones (`pipeline-stalled`, `pr-not-opened`, `ollama-timeout`, `argocd-rollback`, `validator-flake`, `webhook-hmac-mismatch`, `rls-blocked-query`, `temporal-worker-restart-storm`, `llm-rate-limit`, `pgvector-index-stale`) and fill them with the same shape: symptoms, diagnose, fix, prevent.

### Task 8.3: Custom components

**Files:**
- Create: `apps/docs/components/AgentCard.tsx`
- Create: `apps/docs/components/EvalTable.tsx`
- Create: `apps/docs/data/agents.json`

`agents.json` — 9 records (one per agent), each: `id`, `layer` (L1|L2), `model`, `owns[]`, `does_not_own[]`, `status` (operational|preview).

`AgentCard.tsx` — renders a one-line summary block from the JSON entry.

`EvalTable.tsx` — fetches `/eval-results-v1.json` at build time via static import and renders the 4-baseline × 5-metric comparison.

```tsx
import data from "../public/eval-results-v1.json";

export function EvalTable() {
  const baselines = Object.keys(data.summary);
  const metrics = ["mttr_seconds_p50", "mttr_seconds_p95", "patch_acceptance", "human_intervention_rate", "cost_usd_per_incident"];
  return (
    <table>
      <thead><tr><th>Metric</th>{baselines.map(b => <th key={b}>{b}</th>)}</tr></thead>
      <tbody>
        {metrics.map(m => (
          <tr key={m}>
            <td>{m}</td>
            {baselines.map(b => <td key={b}>{(data.summary as any)[b][m]}</td>)}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
```

### Task 8.4: Build + deploy

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app && /opt/homebrew/bin/pnpm install
cd apps/docs && /opt/homebrew/bin/pnpm build
```

Vercel project config (manual + once):

* Root directory: `apps/docs`
* Build command: `pnpm --filter @nexis/docs build`
* Output dir: `apps/docs/out`
* Domain: `docs.nexis.dev`
* DNS: `CNAME docs → cname.vercel-dns.com` in Route53 (Terraform — `infra/route53/docs.tf`).

`.github/workflows/docs-deploy.yml`:

```yaml
name: deploy-docs
on:
  push:
    branches: [main]
    paths: ["apps/docs/**", "docs/eval-results-v1.json"]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: pnpm/action-setup@v3
      - uses: actions/setup-node@v4
        with: { node-version: '20', cache: 'pnpm' }
      - run: pnpm install --frozen-lockfile
      - run: pnpm --filter @nexis/docs build
      - uses: amondnet/vercel-action@v25
        with:
          vercel-token: ${{ secrets.VERCEL_TOKEN }}
          vercel-org-id: ${{ secrets.VERCEL_ORG_ID }}
          vercel-project-id: ${{ secrets.VERCEL_DOCS_PROJECT_ID }}
          working-directory: apps/docs
          vercel-args: '--prod'
```

### Task 8.5: Verify

```bash
# Local dev
pnpm --filter @nexis/docs dev   # open http://localhost:3001
# Production smoke (after deploy)
curl -s -I https://docs.nexis.dev | head -3
```

### Task 8.6: Commit

```bash
git add apps/docs pnpm-workspace.yaml .github/workflows/docs-deploy.yml infra/route53/docs.tf
git commit -m "feat(docs): nextra-based docs.nexis.dev with install/integrations/agents/runbook/architecture (stage 8)"
```

---

## Stage 9 — Status page (BetterStack) + probe endpoints

### Task 9.1: Probe endpoints

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/sentry_probe.go`
- Create: `services/control-plane/internal/transport/http/handler/temporal_health.go`

```go
// sentry_probe.go
package handler

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "io"
    "net/http"

    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
    httpx "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

func SentryProbe(cfg *config.Config) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        body, _ := io.ReadAll(r.Body)
        sig := r.Header.Get("X-Nexis-Probe-Signature")
        m := hmac.New(sha256.New, []byte(cfg.SentryProbeHMACSecret))
        _, _ = m.Write(body)
        expected := hex.EncodeToString(m.Sum(nil))
        if !hmac.Equal([]byte(sig), []byte(expected)) {
            // Allow the static-secret check too — simpler for BetterStack which
            // can't sign per-request. If sig == secret literal, accept.
            if sig != cfg.SentryProbeHMACSecret { httpx.WriteError(w, http.StatusUnauthorized, "sig"); return }
        }
        w.WriteHeader(http.StatusAccepted)
    }
}
```

```go
// temporal_health.go
package handler

import (
    "context"
    "net/http"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"

    httpx "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

func TemporalHealth(pool *pgxpool.Pool) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
        defer cancel()
        var lastBeat time.Time
        err := pool.QueryRow(ctx, `SELECT max(reported_at) FROM worker_heartbeats`).Scan(&lastBeat)
        if err != nil || time.Since(lastBeat) > 60*time.Second {
            httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "stale", "last_heartbeat": lastBeat})
            return
        }
        httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "last_heartbeat": lastBeat})
    }
}
```

Mount unauthenticated:

```go
r.Post("/v1/integrations/sentry/probe", handler.SentryProbe(deps.Config))
r.Get("/healthz/temporal", handler.TemporalHealth(deps.Pool))
```

### Task 9.2: BetterStack as code

**Files:**
- Create: `infra/betterstack/checks.yml` (exact content from spec §8.3)
- Create: `infra/betterstack/main.tf` (Terraform module wrapping the `better-stack/betteruptime` provider)
- Create: `infra/betterstack/README.md` (apply instructions + manual-override note)
- Create: `.github/workflows/betterstack.yml`

`main.tf` consumes `checks.yml` via `yamldecode`:

```hcl
terraform {
  required_providers {
    betteruptime = { source = "BetterStackHQ/better-uptime", version = "~> 0.10" }
  }
}

variable "betterstack_api_token" { type = string, sensitive = true }
variable "sentry_probe_hmac_secret" { type = string, sensitive = true }

provider "betteruptime" { api_token = var.betterstack_api_token }

locals {
  cfg = yamldecode(file("${path.module}/checks.yml"))
}

resource "betteruptime_monitor" "all" {
  for_each = { for m in local.cfg.monitors : m.name => m }

  url                   = each.value.url
  monitor_type          = each.value.monitor_type
  check_frequency       = each.value.check_frequency
  request_timeout       = lookup(each.value, "request_timeout", 10)
  expected_status_codes = lookup(each.value, "expected_status_codes", [200])
  required_keyword      = lookup(each.value, "required_keyword", null)

  dynamic "request_headers" {
    for_each = lookup(each.value, "request_headers", [])
    content {
      name  = request_headers.value.name
      value = replace(request_headers.value.value, "$${SENTRY_PROBE_HMAC_SECRET}", var.sentry_probe_hmac_secret)
    }
  }
}

resource "betteruptime_status_page" "main" {
  subdomain    = local.cfg.status_page.subdomain
  company_name = "NEXIS"
  timezone     = "UTC"
  layout       = local.cfg.status_page.layout
}

resource "betteruptime_status_page_section" "system" {
  status_page_id = betteruptime_status_page.main.id
  name           = "System status"
  position       = 0
}
```

`.github/workflows/betterstack.yml`:

```yaml
name: betterstack
on:
  push:
    branches: [main]
    paths: ["infra/betterstack/**"]
jobs:
  apply:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - working-directory: infra/betterstack
        env:
          TF_VAR_betterstack_api_token: ${{ secrets.BETTERSTACK_API_TOKEN }}
          TF_VAR_sentry_probe_hmac_secret: ${{ secrets.SENTRY_PROBE_HMAC_SECRET }}
        run: |
          terraform init
          terraform apply -auto-approve
```

### Task 9.3: DNS

**Files:**
- Modify: `infra/route53/status.tf`

```hcl
resource "aws_route53_record" "status_nexis_dev" {
  zone_id = data.aws_route53_zone.main.id
  name    = "status.nexis.dev"
  type    = "CNAME"
  ttl     = 300
  records = ["nexis.betteruptime.com"]
}
```

### Task 9.4: Smoke

```bash
# Probe endpoint
curl -s -X POST -H 'X-Nexis-Probe-Signature: probe-dev-secret' \
  -H 'content-type: application/json' \
  -d '{"probe":true}' \
  http://localhost:8080/v1/integrations/sentry/probe
# expect 202

# Temporal health
curl -s http://localhost:8080/healthz/temporal | jq
# expect {status:"ok", last_heartbeat:"..."}

# After deploy, status page
curl -s -I https://status.nexis.dev | head -3
```

### Task 9.5: Commit

```bash
git add services/control-plane infra/betterstack infra/route53 .github/workflows/betterstack.yml
git commit -m "feat(status): betterstack monitors + status.nexis.dev + probe endpoints (stage 9)"
```

---

## Stage 10 — Thesis chapters 1–4 + DoD + mark complete

### Task 10.1: Thesis directory

**Files:**
- Create: `thesis/chapter-1-introduction.tex`
- Create: `thesis/chapter-2-related-work.tex`
- Create: `thesis/chapter-3-architecture.tex`
- Create: `thesis/chapter-4-evaluation.tex`
- Create: `thesis/main.tex` (root document `\input`-ing the four chapters)
- Create: `thesis/bibliography.bib`
- Create: `thesis/figures/` (architecture diagrams + eval charts)

Each chapter contains the actual thesis prose. Pulls citations and figures from the codebase + the frozen `docs/eval-results-v1.json`. Compile via `latexmk -pdf main.tex` to produce `thesis/main.pdf`.

Chapter 3 (Architecture) cites:
* `docs/superpowers/specs/2026-05-13-phase-3-console-integrations.md` for the console + integrations design.
* `docs/superpowers/specs/2026-05-13-phase-4-pipeline-substrate.md` for the workflow substrate.
* `docs/superpowers/specs/2026-05-13-phase-5-agents-l1-llm-spine.md` for the LLM-provider abstraction.
* `docs/superpowers/specs/2026-05-13-phase-6-agents-l2-approval-gate.md` for the closed-loop recovery design.
* `docs/superpowers/specs/2026-05-13-phase-7-cloud-cutover.md` for the cloud topology.

Chapter 4 (Evaluation) embeds `\input{tables/eval-results-v1.tex}` — a LaTeX render of the same JSON.

### Task 10.2: Generate the eval LaTeX table

```bash
python apps/web/scripts/eval/freeze.py --output-latex thesis/tables/eval-results-v1.tex
```

The python script gets a `--output-latex` flag that writes a `\begin{tabular}` block matching the JSON.

### Task 10.3: Build the PDF + send to supervisor

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/thesis && latexmk -pdf -interaction=nonstopmode main.tex
# Verify
ls -la thesis/main.pdf
```

Submit manually (out of band — email to supervisor + share the GitHub URL). Record the date in `docs/PROJECT_PLAN.md` under Phase 8 completion.

### Task 10.4: Verify acceptance criteria

Run the full DoD checklist below; fix anything red.

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm test && /opt/homebrew/bin/pnpm build
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/docs && /opt/homebrew/bin/pnpm build

# Lighthouse
npx unlighthouse-cli https://docs.nexis.dev --site-name "NEXIS docs"
```

### Task 10.5: Mark Phase 8 complete

```bash
# Edit docs/PROJECT_PLAN.md — append "— Completed YYYY-MM-DD" to the Phase 8 heading.
git add docs/PROJECT_PLAN.md thesis/
git commit -m "docs: phase 8 complete — thesis chapters 1-4 sent, public beta live"
```

---

## Definition of Done

- [ ] Fresh signup with valid `?invite=` completes; the code's `used_count` becomes 1; without `?invite=` redirects to `/waitlist`.
- [ ] Onboarding wizard offers "sample repo or BYO"; sample path seeds a real incident, fires a pipeline, and lands on the run timeline within 90 s.
- [ ] On first console visit Shepherd.js runs the 6-step tour; on completion `users.preferences.tour_completed=true`; a second login does not replay.
- [ ] Every list/table surface in the console renders `<EmptyState />` when empty; CI grep guard passes.
- [ ] Owner can mint, list, filter, and revoke invite codes at `/console/admin/invite-codes`; non-owner gets 403.
- [ ] Sample-seed + invite endpoints are RBAC-correct per spec §12.
- [ ] After a tenant's 3rd successful auto-recovery (`pipeline_runs.status=succeeded` + `merged_pr_url`), the NASA-TLX modal appears once; submission writes a `nasa_tlx_responses` row AND POSTs to Google Forms.
- [ ] `pnpm --filter @nexis/web eval:full` produces a 4×30×5 matrix; `freeze.py` writes `docs/eval-results-v1.json`; NEXIS beats every baseline on MTTR and patch-acceptance with p < 0.05.
- [ ] `GET /v1/admin/eval-export?format=csv&include=runs,tlx` returns owner-only CSV; `pandas.read_csv` parses it cleanly.
- [ ] `https://docs.nexis.dev/` resolves; all 5 sections render; search works; Lighthouse perf ≥ 95.
- [ ] `https://status.nexis.dev/` resolves; 4 monitors green; subscribe email confirms.
- [ ] Probe endpoints: `POST /v1/integrations/sentry/probe` returns 202 with valid HMAC; `GET /healthz/temporal` returns 200 when a worker beat is < 60 s old, 503 otherwise.
- [ ] `make build/test/vet/arch` green; `pnpm typecheck/build` green for both `@nexis/web` and `@nexis/docs`.
- [ ] `thesis/main.pdf` builds; chapters 1–4 sent to supervisor; PR description records the date.
- [ ] `docs/PROJECT_PLAN.md` Phase 8 heading marked complete.

---

## Risks captured

* **Shepherd bundle LCP** — gated on `tour_completed`. Verified on cold load that the impact is < 100 ms. If it regresses, lazy-load on user interaction (hover the topbar `?` icon) instead of mounted.
* **Invite-cookie + WorkOS race** — cookie is primary; `?state=` is the WorkOS fallback. Both paths consume atomically under `SELECT ... FOR UPDATE`. If both arrive (cookie + state) we redeem once and ignore the second.
* **`app.is_admin_route` GUC** — a careless query against `invite_codes` without the GUC silently returns zero rows (RLS denies). We document this loudly in the repo file and add a Postgres test that asserts a query without the GUC returns `0`.
* **Sample-seed throws when no Phase 6 workflow is wired** — `Pipelines.Start` returns `ErrUnsupported` in dev when `EVAL_MODE_ENABLED=true` but the worker isn't running. The endpoint surfaces a 503 with `temporal_worker_down`; the wizard catches and offers a "skip and continue to console" link.
* **BetterStack provider lag** — the community Terraform provider sometimes lags the BetterStack API. Mitigation: `infra/betterstack/README.md` documents the manual-apply fallback (web UI), and the file in source control is the source of truth for review even if the provider can't apply a specific field.
* **Google Forms POST failure** — fire-and-forget; we log a warning but don't surface to the user. The DB row is authoritative for the thesis dataset.
* **Eval CLI run time** — 30 hours of CPU time. We schedule it on the Phase 7 staging stack overnight and check in the frozen JSON. If a baseline regresses post-freeze, we bump to `v2/` rather than mutating `v1/`.
* **NASA-TLX modal spamming** — `tlx_prompted_at` lock + 3-recovery gate + Help-menu manual launch keeps the surface area to one prompt per user. Operators can disable per-user via `users.preferences.tlx_prompted_at = '1970-01-01'`.
* **`organizations.successful_recoveries_count` trigger drift** — if a future migration changes the `pipeline_runs` schema (column rename), the trigger silently breaks. Add a Postgres unit test in `services/control-plane/test/integration/trigger_recoveries_test.go` that drives the trigger end-to-end.
* **Nextra v4 + Next 16.2.2 compatibility** — Nextra v4 was released aligned with the App Router. If a peer-dep collision appears, pin Next to the version Nextra publishes against in the docs sub-app only — the web app's Next pin stays at 16.2.2.
* **Symlink for `eval-results-v1.json` not portable to Vercel** — Vercel's build clones the repo and may not resolve the symlink. The `docs-deploy.yml` workflow `cp`s the file into `apps/docs/public/` as a build step before `pnpm build`.
* **Status page domain change** — `status.nexis.dev` needs the Vercel/Cloudflare-managed DNS in Route53 (Phase 7). If Phase 7 hasn't finalised the hosted zone, the CNAME lives in BetterStack's own provided domain (`nexis.betteruptime.com`) until cutover.

---

## Coordinator review at stage commit

The following files are coordinator-owned per Phase 8 conventions. Flag any changes for review:

* `pnpm-workspace.yaml` — Stage 8 adds nothing (covered by `apps/*`), but verify the docs lockfile resolved.
* `apps/web/proxy.ts` — Stage 1 adds `?invite=` allowlist.
* `apps/web/app/layout.tsx` — no change; documented.
* `cmd/server/main.go` — Stage 0–9 wire 5 new repos + 5 new services + 9 new handler mounts.
* `internal/transport/http/server.go` — Stage 1, 2, 5, 6, 7, 9 mount 9 new routes (3 owner-only, 3 owner|admin, 1 any-member, 2 unauthenticated).
* `internal/platform/config/config.go` — Stage 0 adds 4 env vars.
* `.arch.yaml` — Stage 2 adds the `usecase/sample_seed` package; verify it's in the `usecase → domain` allowed-edge list.
* `packages/db/schema.ts` — Stage 0 adds 2 tables + 1 column.
* `docker-compose.yml` — Stage 0 adds 4 env vars; no service additions.

Each stage commit's first reviewer is the coordinator; once green, the agent rotation owns subsequent stages.
