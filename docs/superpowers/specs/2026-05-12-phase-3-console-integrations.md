# Phase 3 — Console Shell + Integrations — Design

**Date:** 2026-05-12
**Phase:** 3 (Weeks 7–9 per `docs/PROJECT_PLAN.md`)
**Dependencies:** Phase 2 (completed 2026-05-12).
**External services:** all local mocks for this phase; real GitHub App / Sentry credentials slot in at Phase 7.

---

## 1. Goals

1. **Console shell** — sidebar + topbar + cmdk launcher + inspector drawer chrome, 8 nav surfaces (4 fully implemented, 4 placeholder).
2. **Integrations surface** — GitHub App install round-trip (mocked locally), Sentry webhook ingestion with HMAC verification, ArgoCD token storage. Status badges per tenant.
3. **Settings surface** — org profile editor, member invites + role matrix, API key CRUD UI, theme preference persisted per user.
4. **Audit surface** — paginated/filterable list of `audit_log` rows + side-sheet detail + CSV export.
5. **Member invites** — owner-only `POST /v1/orgs/:id/invites` → email magic link → claim flow.
6. **RBAC** — `RequireRole(owner|admin|member)` middleware gating sensitive routes.
7. **Theme persistence** — per-user preference stored server-side and hydrated on SSR.

## 2. Non-goals

- Real GitHub App OAuth callbacks (Phase 7).
- Real Sentry production webhook traffic — only synthetic signed events via `make seed-sentry`.
- ArgoCD live calls — Phase 6 uses the stored token.
- Datadog / PagerDuty / Slack integrations — Phase 4+.
- Incidents / Approvals / Agents fleet / Live Demo surfaces beyond a structured placeholder (Phase 4-6).
- Empty-state illustrations (carry over from §4.3 polish backlog).
- Monaco diff viewer (Phase 4).
- Tremor charts on Home (Phase 4 — real metrics).

## 3. Architecture

Mirrors Phase 1+2 port/adapter pattern.

### 3.1 control-plane Go layout (additions)

```
internal/
  domain/
    integration.go         # IntegrationProvider port + Connection types
    invite.go              # Invite types, port-side
    audit.go               # extended — AuditLister with filters
  adapter/
    integration/
      factory.go           # per-provider dispatch
      github/
        provider.go        # real webhook handler + mock-install dev endpoint
        provider_test.go
      sentry/
        provider.go        # webhook receiver + incidents_raw ingest
        provider_test.go
      argocd/
        provider.go        # token-only (no live calls)
    keyvault/
      local.go             # LocalKeyVault: AES-GCM with MASTER_KEY env
      local_test.go
    audit/
      hmac_writer.go       # existing — extend with List(ctx, filters, paging)
  usecase/
    invite_issue.go        # issue + email
    invite_claim.go        # consume token + add to org
    integration_status.go  # roll up Connection status per org
  transport/http/
    middleware/
      rbac.go              # RequireRole(roles...)
    handler/
      integrations.go      # GET/POST/DELETE /v1/integrations
      webhooks.go          # POST /v1/webhooks/{provider}
      invites.go           # POST/GET/DELETE /v1/orgs/:id/invites + GET /v1/invites/:token
      audit.go             # extend with list + CSV
      preferences.go       # PATCH /v1/me/preferences
```

### 3.2 Web (Next.js 16) additions

```
apps/web/
  app/
    (app)/
      console/
        layout.tsx                 # sidebar + topbar + cmdk + inspector slot
        page.tsx                   # Home (was dashboard)
        incidents/
          page.tsx                 # placeholder
        approvals/
          page.tsx                 # placeholder
        audit/
          page.tsx                 # full DataTable + filters + CSV
        integrations/
          page.tsx                 # card grid
          [provider]/configure/
            page.tsx               # Configure dialog content
        settings/
          layout.tsx               # 200px sub-sidebar
          profile/page.tsx
          organization/page.tsx
          members/page.tsx
          api-keys/page.tsx
          preferences/page.tsx     # theme + notifications
        agents/
          page.tsx                 # placeholder
        live-demo/
          page.tsx                 # placeholder
      invites/
        [token]/page.tsx           # claim flow
    api/
      webhooks/
        github/route.ts            # browser-side mock-install kickoff (dev only)
  components/
    console/
      Sidebar.tsx
      Topbar.tsx
      CommandPalette.tsx           # ⌘K — cmdk
      InspectorDrawer.tsx
      DataTable.tsx                # generic table primitive (TanStack Table headless)
      EmptyState.tsx
      KPICard.tsx
    integrations/
      IntegrationCard.tsx
      GitHubConfigureForm.tsx
      SentryConfigureForm.tsx
      ArgoCDConfigureForm.tsx
  lib/
    integrations.ts                # SDK extensions
    audit.ts                       # SDK
    invites.ts                     # SDK
    preferences.ts                 # SDK
```

The existing `apps/web/app/(app)/dashboard/` becomes `(app)/console/` — old `/dashboard` route redirects to `/console`.

## 4. Database

### 4.1 New tables

```
integrations
  id           uuid PK
  org_id       uuid REFERENCES organizations(id)
  provider     text NOT NULL                 -- 'github' | 'sentry' | 'argocd'
  status       text NOT NULL                 -- 'connected' | 'pending' | 'error' | 'disconnected'
  installation_id text                       -- GitHub installation id (encrypted-at-rest=false; it's a public id)
  secret_ciphertext bytea                    -- encrypted token / webhook secret
  metadata     jsonb                         -- {scopes:[], orgs:[], last_synced_at:...}
  last_error   text
  created_at   timestamptz NOT NULL DEFAULT now()
  updated_at   timestamptz NOT NULL DEFAULT now()
  UNIQUE (org_id, provider)

incidents_raw
  id           uuid PK DEFAULT gen_random_uuid()
  org_id       uuid REFERENCES organizations(id)
  source       text NOT NULL                 -- 'sentry' | 'github' | 'otel'
  source_event_id text                       -- vendor-side id; unique per source per org
  title        text
  level        text                          -- 'fatal'|'error'|'warning'|'info'
  service      text
  environment  text
  raw_payload  jsonb
  received_at  timestamptz NOT NULL DEFAULT now()
  UNIQUE (org_id, source, source_event_id)

org_invites
  token_hash   bytea PK                      -- SHA-256(token)
  org_id       uuid REFERENCES organizations(id)
  email        text NOT NULL
  role         text NOT NULL                 -- 'admin' | 'member' (no owner via invite)
  inviter_user_id uuid REFERENCES users(id)
  expires_at   timestamptz NOT NULL
  claimed_at   timestamptz
```

### 4.2 Modified tables

`users` gets a JSON `preferences` column:
```sql
ALTER TABLE users ADD COLUMN preferences jsonb NOT NULL DEFAULT '{}'::jsonb;
```

Shape: `{"theme":"light"|"dark"|"system"}` initially.

### 4.3 RLS

All three new tables get `tenant_isolation` policies on `org_id`. Same NULLIF pattern as Phase 2 migration 0004.

## 5. Encrypted token storage

LocalKeyVault adapter, AES-256-GCM:
- `Encrypt(plaintext []byte) (ciphertext []byte, err error)` — derives a per-call random 12-byte nonce, prepends nonce, returns `nonce||ciphertext||tag`.
- `Decrypt(ciphertext []byte) (plaintext []byte, err error)`.
- Key: 32-byte base64 in `MASTER_KEY` env (dev default in `.env.example`; required in non-dev).

The `KeyVault` port lives in `internal/domain/keyvault.go`. Phase 4's KMS adapter will swap in transparently.

## 6. Integration provider port

```go
type IntegrationProvider interface {
    Name() string

    // Connect persists a new connection. For GitHub: stores installation_id.
    // For Sentry: stores webhook secret + DSN. For ArgoCD: stores API token.
    // The shape of `config` is provider-specific (validated inside the adapter).
    Connect(ctx context.Context, p Principal, config map[string]any) (Connection, error)

    // Disconnect deletes the row + revokes upstream where applicable.
    Disconnect(ctx context.Context, p Principal) error

    // Status returns the cached connection state.
    Status(ctx context.Context, p Principal) (Connection, error)

    // HandleWebhook verifies HMAC and processes the event. Different providers
    // produce different side effects (Sentry → incidents_raw insert; GitHub
    // → no-op in Phase 3, ack only).
    HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error
}
```

`Connection` is `{Provider, Status, InstallationID, Metadata, LastError, CreatedAt}`. Encrypted secrets never escape the adapter.

## 7. HTTP surface (additions)

```
GET    /v1/integrations                                   → 200 [{provider, status, metadata}]
POST   /v1/integrations/:provider/connect                 → 201 {provider, status}
DELETE /v1/integrations/:provider                         → 204
GET    /v1/integrations/github/mock_install               → dev-only; sets a fake installation_id + 302 back to console
POST   /v1/webhooks/github/:org_id                        → 200 (HMAC verified)
POST   /v1/webhooks/sentry/:org_id                        → 200 (HMAC verified)

POST   /v1/orgs/:id/invites                               → 201 {token_prefix} (owner only)
GET    /v1/orgs/:id/invites                               → 200 [...]
DELETE /v1/orgs/:id/invites/:tokenHash                    → 204
GET    /v1/invites/:token                                 → 200 {org, role, inviter_email}    (public)
POST   /v1/invites/:token/claim                           → 200 + cookie (public)

GET    /v1/audit?since=...&until=...&actor=...&action=...&limit=...&offset=...
                                                          → 200 {rows:[], total}
GET    /v1/audit.csv?<same filters>                       → 200 text/csv

PATCH  /v1/me/preferences                                 → 204 {theme}
GET    /v1/me/preferences                                 → 200 {theme}
```

Webhook routes deliberately include the `org_id` in the path so a single `Sentry` org-level dashboard endpoint maps unambiguously to a tenant. The HMAC secret stored in `integrations.secret_ciphertext` keys verification.

## 8. RBAC matrix

| Endpoint | owner | admin | member |
|---|---|---|---|
| `GET /v1/me*` | ✓ | ✓ | ✓ |
| `POST /v1/apikeys` | ✓ | ✓ | ✗ |
| `GET /v1/audit*` | ✓ | ✓ | ✗ |
| `POST /v1/integrations/:provider/connect` | ✓ | ✓ | ✗ |
| `DELETE /v1/integrations/*` | ✓ | ✓ | ✗ |
| `POST /v1/orgs/:id/invites` | ✓ | ✗ | ✗ |
| `DELETE /v1/orgs/:id/invites/*` | ✓ | ✗ | ✗ |
| Webhook routes | n/a (HMAC, not user-auth) |

`middleware/rbac.go` exports `RequireRole(domain.RoleOwner)` etc.

## 9. Console shell

### 9.1 Chrome

- **Sidebar** 240px expanded / 64px collapsed (persisted in localStorage). Sections: Workspace (Home, Incidents, Approvals, Audit), Platform (Agents, Integrations, Live Demo), Settings pinned bottom. Org switcher pinned top, user menu bottom.
- **Topbar** 56px. Breadcrumb left, `⌘K` cmdk trigger center, theme toggle + notifications popover + help right.
- **Main** `max-w-[1440px]`, `px-6 py-6`.
- **InspectorDrawer** 480px slide-over — Phase 3 only wires it for the Audit row detail.

### 9.2 Surfaces — Phase 3 coverage

| # | Surface | Phase 3 content |
|---|---|---|
| 1 | Home | Greeting + 3 quick actions + 4 KPI cards (Open incidents = 0 stub, Awaiting approval = 0 stub, MTTR 7d = "—", Patch acceptance = "—") + activity feed (last 10 audit rows for the org). |
| 2 | Incidents | Placeholder: "Phase 4" badge + brief explainer. Empty state. |
| 3 | Approvals | Placeholder: same. |
| 4 | Audit | Full DataTable + filters (date range, actor, action) + CSV export + side-sheet JSON detail. |
| 5 | Integrations | Card grid (6 cards: GitHub, Sentry, ArgoCD wired; Datadog, PagerDuty, Slack "coming Phase 4"). Configure dialog. Status badge from `/v1/integrations`. |
| 6 | Settings | 7 sub-routes: Profile (email, name), Organization (name, slug), Members & Roles (table + invite button), Approval Policies (placeholder), API Keys (full CRUD), Notifications (placeholder), Billing (placeholder Phase 7), Preferences (theme). |
| 7 | Agents fleet | Placeholder grid: re-uses 9 agent cards from landing in muted state with "Phase 5" badge. |
| 8 | Live Pipeline Demo | Placeholder: three-pane layout from landing's LiveDemoEmbed component. |

## 10. Data flow — GitHub install (mock)

1. User clicks **Configure** on the GitHub card → opens dialog.
2. Dialog shows "Install GitHub App" button → links to `GET /v1/integrations/github/mock_install?org_id=<current>`.
3. Mock endpoint:
   - Generates a fake `installation_id` (uuidv4).
   - `Connect(ctx, principal, {installation_id, scopes:["repo","metadata"]})`.
   - Stores row `(org_id, provider=github, status=connected, installation_id, secret_ciphertext=<encrypted("dev-webhook-secret-32")>)`.
   - Writes audit event `integration.connected` `{provider:"github"}`.
   - 302s to `/console/integrations?installed=github`.
4. Console card now shows **Connected ✓** badge with the installation id (last 6 chars).

In Phase 7 we replace step 2's target with the real GitHub App `https://github.com/apps/<app>/installations/new?state=<csrf>` and step 3 becomes `GET /v1/integrations/github/callback` that verifies the CSRF + fetches the real installation id.

## 11. Data flow — Sentry webhook

1. User clicks **Configure** on the Sentry card → dialog shows DSN field + a generated webhook URL + a copy-to-clipboard webhook secret (revealed once).
2. User submits → `POST /v1/integrations/sentry/connect {dsn, webhook_secret}`. Server:
   - Encrypts both, stores them in `integrations`.
   - Returns `{webhook_url: "https://<host>/v1/webhooks/sentry/<org_id>"}`.
3. Outside the app, the user (in Sentry's UI) registers the webhook URL + secret. For Phase 3, this step is simulated by `make seed-sentry`:

```bash
ORG_ID=... make seed-sentry
```

Which POSTs an HMAC-signed test event:
```http
POST /v1/webhooks/sentry/<org_id>
X-Sentry-Hook-Signature: <hex(hmac_sha256(secret, body))>
Content-Type: application/json

{"id":"abc123","level":"error","title":"Test","environment":"prod","tags":[["service","api"]]}
```

4. Server `HandleWebhook`:
   - Decrypts the stored secret.
   - Computes HMAC over body, compares to header. Mismatch → 401.
   - Inserts a row into `incidents_raw`.
   - Writes audit event `webhook.received` `{source:"sentry", incident_id}`.
5. Acceptance: `SELECT count(*) FROM incidents_raw WHERE source='sentry'` increments within 5s of the curl.

## 12. Acceptance criteria

1. **Mock GitHub install round-trips:**
   ```bash
   curl -s -b "nexis_session=<cookie>" 'http://localhost:8080/v1/integrations/github/mock_install?org_id=<org>' -L
   curl -s -b "nexis_session=<cookie>" 'http://localhost:8080/v1/integrations'
   # → entry with provider=github, status=connected
   ```
   Console card shows **Connected ✓**.

2. **Sentry seed event ingests in <5s:**
   ```bash
   make seed-sentry ORG_ID=<org>
   docker compose exec -T postgres psql -U nexis_app -d nexis \
     -c "SET LOCAL app.current_org_id='<org>'; SELECT count(*) FROM incidents_raw;"
   # → ≥ 1
   ```

3. **Theme persists per user:** User toggles dark in Settings → Preferences. Reload (different browser session via cookie carry) → still dark. SSR sees `users.preferences.theme = "dark"` and emits `<html class="dark">`.

4. **Member invite works end-to-end:**
   - Owner POSTs `/v1/orgs/:id/invites {email, role:"member"}` → 201.
   - MailHog shows email with magic link.
   - Anonymous user opens link → claim page renders `{org, role, inviter}`.
   - Anonymous submits password → 200 + cookie + redirect to `/console`.
   - New `org_members` row exists with role=member.

5. **Audit list filters work:** `GET /v1/audit?action=integration.connected` returns only those rows; CSV export downloads.

6. **RBAC enforced:** member-role user gets 403 on `POST /v1/integrations/...`.

7. **Build matrix green:** Go + web (typecheck, vitest, playwright `console.spec.ts`).

8. **All 8 nav surfaces render** under `/console/*` without errors (placeholders for the 4 deferred).

## 13. Stage breakdown (controller will plan in detail)

| Stage | Title |
|---|---|
| 0 | Schema additions (3 new tables + users.preferences) + RLS + master-key plumbing |
| 1 | LocalKeyVault adapter (AES-GCM) + tests |
| 2 | IntegrationProvider port + 3 adapters (GitHub/Sentry/ArgoCD) + factory |
| 3 | HTTP routes — integrations + webhooks + mock_install |
| 4 | Org invites — issue/list/revoke/claim + email |
| 5 | RBAC middleware + role gating + audit list endpoint + CSV |
| 6 | Console shell — sidebar/topbar/cmdk/inspector + route group + 4 placeholder pages |
| 7 | Integrations surface — card grid + Configure dialogs |
| 8 | Settings surface — Profile, Organization, Members, API Keys, Preferences |
| 9 | Audit surface — DataTable + filters + side sheet + CSV |
| 10 | Theme persistence — preferences endpoint + SSR hydration |
| 11 | E2E + DoD audit + Phase 3 complete |

## 14. Risks / open items

- **Console depth.** 8 surfaces in 3 weeks is tight even with 4 placeholders. Risk: surface count creeps. Mitigation: stage 6's acceptance is "every nav link routes to a page that renders". Polish in Phase 4-6.
- **Theme SSR flash.** With server-rendered theme class, hydration mismatch is possible if cookie disagrees with client. Mitigation: `suppressHydrationWarning` on `<html>` and let `next-themes` reconcile on mount (already pattern from Phase 1).
- **GitHub webhook event volume.** Real GitHub sends many event types. Phase 3 only logs + acks; Phase 4-6 actually consumes specific events. Reflected in `HandleWebhook` returning nil for unknown event types.
- **`org_invites` token shape.** 32-byte random base64url; consumed via `POST /v1/invites/:token/claim` (token in URL path is fine — single-use, expires in 7 days).
- **Per-org webhook URL exposure.** The webhook URL contains the `org_id`. That's a tenant identifier, but it's not secret on its own — the HMAC secret is what proves authenticity. Acceptable for Phase 3; review in Phase 7 hardening.
- **TanStack Table dep.** Adding `@tanstack/react-table` to apps/web for DataTable. Headless, ~12 KB gzipped, used by shadcn DataTable recipes. No runtime lock-in.

---

## Appendix A: Env vars added in Phase 3

```
# control-plane
MASTER_KEY=<32-byte base64>                # required in non-dev
GITHUB_WEBHOOK_SECRET=<bytes>              # optional Phase 3 — used by mock_install default
SENTRY_DEFAULT_WEBHOOK_SECRET=<bytes>      # optional Phase 3

# web
NEXT_PUBLIC_API_URL                        # already set, used by console SDK
```

## Appendix B: Make targets added in Phase 3

```
# Project root Makefile (new — Phase 1 only had per-service Makefiles)
seed-sentry:                              # POSTs a signed test event to /v1/webhooks/sentry
verify-audit:                             # Runs `curl /v1/audit/verify` after auth
```
