# Phase 2 — Auth, Tenancy, Observability — Design

**Date:** 2026-05-12
**Phase:** 2 (Weeks 4–6 per `docs/PROJECT_PLAN.md`)
**Dependencies:** Phase 1 (completed 2026-05-11, commit `a71783d`).
**External services:** all local mocks for this phase; real credentials slot in at Phase 7 cloud cutover.

---

## 1. Goals

1. **Signup → login → protected dashboard** route works end-to-end on the local Docker stack.
2. **Auto-tenant provisioning:** signup creates `organizations` row + owner membership.
3. **Postgres RLS** enforces tenant isolation on every tenant table — verifiable by SQL.
4. **Audit log** records every mutation as a row in an append-only HMAC chain.
5. **Distributed trace** spanning `web → control-plane` visible in local Grafana (Tempo).

## 2. Non-goals

- WorkOS production wiring (Phase 7).
- Sentry integration (Phase 7).
- GitHub OAuth, passkeys, SAML, SCIM (deferred — interface-ready but unimplemented).
- Real cloud OTLP destination (using local Tempo).
- UI polish beyond a functional dashboard stub.

## 3. Architecture

Mirrors Phase 1's port/adapter pattern (LLM provider, validator/gitops services).

### 3.1 control-plane Go layout (additions)

```
internal/
  domain/
    auth.go              # AuthProvider port, Session, Principal, ErrInvalidCredentials
    audit.go             # AuditWriter port
  adapter/
    auth/
      factory.go         # NewFromConfig (AUTH_PROVIDER=local|workos)
      local/
        provider.go      # bcrypt + magic + TOTP + API keys + JWT sessions
        provider_test.go
      workos/
        provider.go      # stub: returns ErrNotImplemented
    audit/
      hmac_writer.go     # append-only HMAC-chained writer
      hmac_writer_test.go
  transport/http/
    middleware/
      auth.go            # extract bearer / cookie session → ctx Principal
      rls.go             # open tx, SET LOCAL app.current_org_id, attach tx to ctx
      trace.go           # OTel HTTP middleware (server side)
    handler/
      auth.go            # /v1/auth/{signup,login,verify,logout,mfa,apikeys}
      audit.go           # /v1/audit/verify
  usecase/
    auth_signup.go       # tenant + owner provisioning transaction
    auth_login.go
    audit_verify.go
  platform/
    otel/
      otel.go            # init OTLP HTTP exporter, return shutdown fn
```

### 3.2 Web (Next.js 16) additions

```
apps/web/
  app/
    (auth)/
      sign-up/page.tsx
      sign-in/page.tsx
      verify/page.tsx        # magic-link landing
      mfa/page.tsx
    (app)/
      dashboard/page.tsx     # protected
  middleware.ts              # gate /(app)/* on session cookie
  lib/
    auth.ts                  # client SDK wrapping control-plane endpoints
    otel.ts                  # @vercel/otel init
```

## 4. Auth method coverage (MVP cut)

Phase 2 ships the four methods that cover the dev-and-API path:

| Method | Implementation |
|---|---|
| Email + password | bcrypt cost 12; password reset via magic link reuse. |
| Magic link | 32-byte random token, SHA-256 stored, 15-min TTL, single-use, mailed via MailHog (already in compose). |
| MFA TOTP | RFC 6238, 30 s window, 6 digits, ±1 step drift, secret stored on `users` row. |
| API keys | `nx_live_<32-base62>`. Plaintext shown once at creation; SHA-256 hash stored. |

**Deferred to Phase 7** (interface-ready, unimplemented): GitHub OAuth, passkeys, SAML, SCIM. All slot into the same `AuthProvider` port.

## 5. Database

### 5.1 New / modified tables

```
organizations
  + slug          text UNIQUE NOT NULL
  + owner_user_id uuid REFERENCES users(id)

users
  + password_hash    text                 -- nullable (magic-link-only users allowed)
  + mfa_secret       text                 -- TOTP base32 secret, encrypted at rest in Phase 7
  + mfa_enabled      boolean NOT NULL DEFAULT false
  + email_verified_at timestamptz

org_members        (NEW)
  org_id   uuid REFERENCES organizations(id)
  user_id  uuid REFERENCES users(id)
  role     text NOT NULL CHECK (role IN ('owner','admin','member'))
  created_at timestamptz NOT NULL DEFAULT now()
  PRIMARY KEY (org_id, user_id)

sessions           (NEW)
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid()
  user_id    uuid NOT NULL REFERENCES users(id)
  org_id     uuid NOT NULL REFERENCES organizations(id)
  created_at timestamptz NOT NULL DEFAULT now()
  expires_at timestamptz NOT NULL
  revoked_at timestamptz

magic_tokens       (NEW)
  token_hash bytea PRIMARY KEY        -- SHA-256(token)
  user_id    uuid NOT NULL REFERENCES users(id)
  purpose    text NOT NULL            -- 'login' | 'verify_email' | 'reset_password'
  expires_at timestamptz NOT NULL
  used_at    timestamptz

api_keys           (NEW)
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid()
  org_id      uuid NOT NULL REFERENCES organizations(id)
  user_id     uuid NOT NULL REFERENCES users(id)
  prefix      text NOT NULL              -- first 8 chars, indexed
  hash        bytea NOT NULL             -- SHA-256(full key)
  scopes      text[] NOT NULL DEFAULT '{}'
  name        text NOT NULL
  created_at  timestamptz NOT NULL DEFAULT now()
  last_used_at timestamptz
  revoked_at  timestamptz

audit_log          (extend Phase 1 baseline)
  + prev_hash bytea            -- previous row's row_hash (per-org chain)
  + row_hash  bytea NOT NULL   -- HMAC_SHA256(SERVICE_SECRET, prev_hash || canonical_json)
```

All new tenant tables (`org_members`, `sessions`, `magic_tokens`, `api_keys`) have an `org_id` column and RLS enabled (§5.2).

### 5.2 Row-Level Security

Every tenant table gets:

```sql
ALTER TABLE <table> ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON <table>
  USING (org_id = current_setting('app.current_org_id', true)::uuid);
```

The control-plane connects as a **non-superuser role** (`nexis_app`) so RLS actually enforces. Migrations + admin tasks run as the superuser role.

The RLS middleware opens a pgx transaction per request, runs `SET LOCAL app.current_org_id = $1` with the Principal's org id, and hands the tx to the handler. Commit on 2xx, rollback otherwise.

### 5.3 Migrations

- Drizzle migration generated under `packages/db/migrations/0001_phase2_auth.sql`.
- Mirror sqlc migration `services/control-plane/migrations/0002_phase2_auth.up.sql` + `.down.sql`.
- A separate file `0003_rls.up.sql` enables RLS + policies (kept distinct so it can be re-run on a dropped role).

## 6. Sessions

- **Token shape:** JWT HS256, claims `{sid, uid, oid, role, iat, exp}`.
- **Signing key:** `SESSION_SECRET` env (32-byte base64), rotated per-deployment in Phase 7.
- **Storage:** `nexis_session` cookie (`HttpOnly`, `SameSite=Lax`, `Secure` only set when behind HTTPS — Phase 7).
- **Revocation:** middleware reads `sid` from JWT, joins `sessions` table on every request; row with `revoked_at IS NOT NULL` rejects the session. Result cached in Redis (already in compose) for 60 s per `sid` to keep RLS-middleware overhead bounded.
- **Lifetime:** 7-day rolling (slide on every request); refresh handled inline by middleware.

## 7. Audit log

### 7.1 HMAC chain

- Per-org chain — `prev_hash` of row N = `row_hash` of row N-1 with the same `org_id` (NULL for the first row in an org).
- `row_hash = HMAC_SHA256(AUDIT_SECRET, prev_hash || canonical_json(payload))` where `payload = {id, org_id, actor, action, target, metadata, created_at}` — excludes `prev_hash` and `row_hash` themselves.
- Canonical JSON = sorted keys, no whitespace, RFC 8785 subset.
- `AUDIT_SECRET` is independent of `SESSION_SECRET`.

### 7.2 Writer

`AuditWriter.Write(ctx, action, target, metadata)` — called from every mutation usecase. Pulls Principal from ctx, computes `prev_hash` (per-org), inserts inside the same RLS tx.

### 7.3 Verify

`GET /v1/audit/verify` (admin-only) re-walks the chain per org and returns `{ok, rows_checked, first_bad_row_id?}`. Wired to `make audit-verify` in the control-plane Makefile for CI.

### 7.4 Events instrumented in Phase 2

`user.signup`, `user.login`, `user.logout`, `user.mfa_enroll`, `user.mfa_verify`, `apikey.create`, `apikey.revoke`, `member.invite`, `member.role_change`.

## 8. Observability

### 8.1 OTel pipeline (local-only this phase)

```
web (Node)  ─┐
             ├─→ otel-collector:4318 (already in compose)
              ─┘                    ↓
                                 Tempo (traces) / Loki (logs) / Prometheus (metrics)
                                    ↓
                                 Grafana (datasources already provisioned)
```

- **Go (control-plane):** `go.opentelemetry.io/otel/sdk/trace` + OTLP HTTP. Service name `nexis-control-plane`. Sampling: AlwaysSample in dev, parent-based in Phase 7.
- **Web (Next.js):** `@vercel/otel` (Next-native, replaces deprecated `@opentelemetry/api` boilerplate). Service name `nexis-web`. Auto-instruments `fetch` so the web → control-plane span links across services.
- **Trace ID propagation:** W3C `traceparent` header. Both runtimes inject/extract automatically.
- **Response header:** `x-trace-id` echoed on every response for debugging.

### 8.2 Sentry

**Deferred to Phase 7.** Local errors land in container logs via the standard slog/console pipeline; no value in mocking Sentry locally.

## 9. WorkOS adapter (Phase 2 stub)

- `internal/adapter/auth/workos/provider.go` exists with the full `AuthProvider` interface.
- Every method returns `domain.ErrNotImplemented` with a comment pointing to the Phase 7 task.
- Factory behaviour for `AUTH_PROVIDER=workos`:
  - Default: factory returns an error at startup, control-plane refuses to boot.
  - `ALLOW_STUB_WORKOS=1`: factory logs a warning and substitutes the `local` provider — lets us shake out the wiring in Phase 2 without real WorkOS creds.
- Phase 7 fills in the bodies; no other code in the codebase changes.

## 10. Acceptance criteria

All must pass against the running local Docker stack:

1. **Playwright E2E** (`apps/web/tests/e2e/auth.spec.ts`):
   - Sign up `e2e@example.com` → email visible in MailHog → click magic link → MFA enroll → logout → login → land on `/dashboard`.
2. **RLS verification** (SQL run from `nexis_app` role):
   ```sql
   BEGIN;
   SET LOCAL app.current_org_id = 'org-X-uuid';
   SELECT count(*) FROM api_keys;      -- returns only org X's rows
   ROLLBACK;
   ```
   And without the `SET LOCAL`, the same SELECT returns zero rows.
3. **Distributed trace** visible in Grafana → Explore → Tempo: search by `x-trace-id` returned on `POST /v1/auth/login`, see two spans (`http.server` on web, `http.server` on control-plane) linked by parent.
4. **Audit chain verify:** `curl localhost:8080/v1/audit/verify` returns `{ok:true, rows_checked: >0}` after the Playwright flow runs.
5. **Build/test/lint matrix green** across both runtimes (same shape as Phase 1 DoD).

## 11. Stage breakdown (controller will write detailed plan)

| Stage | Title | Verification |
|---|---|---|
| 0 | Schema + RLS migration | `\d users`, `\d sessions`, RLS policies visible. |
| 1 | `AuthProvider` port + local adapter (TDD) | `go test ./internal/adapter/auth/local/...` all green. |
| 2 | HTTP auth routes + auth middleware | `curl POST /v1/auth/signup` returns 201 + sets cookie. |
| 3 | RLS middleware + apply policies | RLS verification SQL passes. |
| 4 | Audit writer + HMAC chain + verify endpoint | `/v1/audit/verify` green after seeded inserts. |
| 5 | Web auth pages + Next middleware guard | Browser flow: signup → dashboard works. |
| 6 | OTel SDK both runtimes + Tempo verification | Span visible in Grafana. |
| 7 | WorkOS stub adapter | `AUTH_PROVIDER=workos ALLOW_STUB_WORKOS=1` boots without panic. |
| 8 | Playwright E2E + DoD audit | All five acceptance criteria pass. |

## 12. Risks / open items

- **bcrypt cost on Apple Silicon dev:** cost 12 ≈ 250 ms. May need to drop to 11 for dev-only via env. Decision: keep 12, accept the latency.
- **Cookie `Secure` flag:** Phase 2 ships HTTP-only locally; the cookie is `Secure=false` in dev. Phase 7's Caddy + HTTPS flips this with no code change (read from `APP_ENV`).
- **MailHog persistence:** MailHog forgets emails on restart. Acceptance criteria tolerate this; tests grab the link inline.
- **Drizzle ↔ sqlc divergence:** maintaining two migration trees is a known cost (carried from Phase 1 Stage 8). Phase 2 keeps the pattern; Phase 3 may consolidate.

---

## Appendix A: Env vars added in Phase 2

```
# control-plane
AUTH_PROVIDER=local                  # local | workos (Phase 7)
ALLOW_STUB_WORKOS=0                  # 1 to permit workos adapter to fall through to local
SESSION_SECRET=<32-byte base64>
AUDIT_SECRET=<32-byte base64>
SMTP_HOST=mailhog
SMTP_PORT=1025
DATABASE_URL_APP=postgres://nexis_app:nexis_app_dev_password@postgres:5432/nexis
OTEL_SERVICE_NAME=nexis-control-plane
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318

# web
OTEL_SERVICE_NAME=nexis-web
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
NEXT_PUBLIC_API_URL=http://localhost:8080
```

## Appendix B: New control-plane HTTP surface

```
POST   /v1/auth/signup             { email, password, org_name } → 201, sets cookie
POST   /v1/auth/login              { email, password }            → 200, sets cookie
POST   /v1/auth/magic              { email, purpose }             → 202
GET    /v1/auth/verify             ?token=...                     → 302 dashboard | 401
POST   /v1/auth/logout                                            → 204
POST   /v1/auth/mfa/enroll                                        → 200 { qr_data_url, recovery_codes }
POST   /v1/auth/mfa/verify         { code }                       → 204
DELETE /v1/auth/mfa                                               → 204
POST   /v1/apikeys                 { name, scopes[] }             → 201 { key_plaintext_once }
GET    /v1/apikeys                                                → 200 [{ id, prefix, name, last_used_at }]
DELETE /v1/apikeys/:id                                            → 204
GET    /v1/me                                                     → 200 { user, org, role }
GET    /v1/audit/verify                                           → 200 { ok, rows_checked }
```
