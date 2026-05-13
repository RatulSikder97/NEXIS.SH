# Real Integrations — Work Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace every "stored-credential-only" stub on the Integrations surface with a real, round-tripping API client. Six integrations: GitHub, Sentry, ArgoCD, Slack, Datadog, PagerDuty. After this plan, "Configure" round-trips real OAuth/token flows, "Status" reports live API reachability, and the recovery workflow uses each integration's real API surface — not in-memory stubs.

**Architecture:** All integrations keep the existing `domain.IntegrationProvider` port (`Connect / Disconnect / Status / HandleWebhook`). Each adapter gains a tested `*http.Client`-backed API client behind a small typed surface. Webhook receivers move from "verify signature + write `incidents_raw`" to "verify + classify + emit `IncidentDetected` to Sentinel". Secrets stay in `nexis_app_kv_envelopes` (Phase 2 AES-GCM envelope), no plaintext on disk. Per-tenant rate limit + circuit breaker wrapping each client.

**Tech stack:** Go 1.25, `net/http` + `golang.org/x/time/rate` (token-bucket), `github.com/google/go-github/v66`, `github.com/getsentry/sentry-go` (for self-DSN only — we hit the Sentry REST API directly), `github.com/argoproj/argo-cd/v2/pkg/apiclient`, `github.com/slack-go/slack`, `github.com/DataDog/datadog-api-client-go/v2`, `github.com/PagerDuty/go-pagerduty`. JSON wire shapes stay snake_case.

---

## Cross-cutting groundwork (do this first)

### Task 0: Shared API client helpers

**Files:**
- Create: `services/control-plane/internal/adapter/integration/internal/httpx/client.go`
- Create: `services/control-plane/internal/adapter/integration/internal/httpx/client_test.go`

`httpx.Client` wraps `*http.Client` with:
- Per-host token-bucket rate limiter (`golang.org/x/time/rate`) — default 10 req/s burst 20.
- Circuit breaker — opens after 5 consecutive 5xx within 30s, half-opens after 60s.
- Structured request/response logging at `slog.Debug` level with redacted Authorization header.
- Automatic retry with exponential backoff on 429 + 5xx (max 3 attempts).
- Context-aware (`req.WithContext(ctx)` everywhere).

- [ ] Step 1: Write failing test `TestClient_RetriesOn429`.
- [ ] Step 2: Implement minimal client with retry.
- [ ] Step 3: Add `TestClient_OpensBreakerOnConsecutive5xx`.
- [ ] Step 4: Implement breaker state machine.
- [ ] Step 5: Add `TestClient_RedactsAuthHeaderInLogs`.
- [ ] Step 6: Implement log redaction.
- [ ] Step 7: `go test ./internal/adapter/integration/internal/httpx/...`.
- [ ] Step 8: Commit `feat(integrations): shared httpx client with retry+breaker+rate-limit`.

### Task 1: Integration status DTO + frontend wiring for live reachability

**Files:**
- Modify: `services/control-plane/internal/transport/http/dto/integration.go`
- Modify: `services/control-plane/internal/transport/http/handler/integrations.go:IntegrationsStatus`
- Modify: `apps/web/lib/integrations.ts`
- Modify: `apps/web/app/(app)/console/integrations/client.tsx`

Today `Status` returns `connected: bool`. Make it return `{connected, healthy, last_check_at, last_error?, latency_ms}` so the UI can show "Connected — healthy 230ms" vs "Connected — degraded 5s ago (401 Unauthorized)".

- [ ] Step 1: Extend DTO.
- [ ] Step 2: Each adapter's `Status` performs a cheap probe (GitHub `/installations/:id`, Sentry `/api/0/organizations/:slug/`, ArgoCD `/api/version`, Slack `auth.test`, Datadog `validate`, PagerDuty `users/me`).
- [ ] Step 3: Cache health for 60s in Redis keyed by `(org_id, provider)`.
- [ ] Step 4: Update FE card to render health pill + last-check.
- [ ] Step 5: Build + typecheck both sides.
- [ ] Step 6: Commit `feat(integrations): live health probe + UI status pill`.

---

## Per-integration tasks

### Task 2: GitHub — real App installation + PR creation

**Files:**
- Modify: `services/control-plane/internal/adapter/integration/github/provider.go`
- Create: `services/control-plane/internal/adapter/integration/github/client.go`
- Create: `services/control-plane/internal/adapter/integration/github/install_callback.go`
- Modify: `services/control-plane/internal/transport/http/server.go`
- Modify: `services/gitops/internal/adapter/github/client.go` (existing gitops surface)

**What real means:**
- `GET /v1/integrations/github/install` → 302 to `https://github.com/apps/<APP_NAME>/installations/new?state=<csrf>` using the existing GitHub App registration (env `GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY_PATH`, `GITHUB_APP_SLUG`).
- `GET /v1/integrations/github/install/callback` — exchanges `installation_id` for an installation token (App JWT signed with App private key → `POST /app/installations/{id}/access_tokens`), persists `installation_id` + sha256(installation token) in `nexis_app_kv_envelopes`.
- `Status` — lists installation repos (`/installation/repositories`), persists count + first 5 names for the UI.
- `HandleWebhook` — verifies HMAC, dispatches: `push` → no-op (already covered by Sentry), `pull_request.closed.merged` → audit, `installation.deleted` → mark disconnected.
- Move stub `gitops` PR opener to use this client. Real PR via `POST /repos/{owner}/{repo}/pulls`.
- Token caching: installation tokens are 1h-scoped; cache in Redis keyed by `installation_id` until 5 min before expiry.

- [ ] Step 1: Write failing test `TestGitHubClient_MintsAppJWT`.
- [ ] Step 2: Implement App-JWT minter (RS256, 10-min TTL).
- [ ] Step 3: Test + impl `TestClient_ExchangesInstallationToken`.
- [ ] Step 4: Test + impl `TestClient_OpensPullRequest`.
- [ ] Step 5: Wire callback route in `server.go` (public — no session yet).
- [ ] Step 6: Replace gitops stub with real client.
- [ ] Step 7: Integration test against `https://api.github.com` using GitHub Personal Access Token for the test suite (skip with build tag `+integration` if no token).
- [ ] Step 8: Commit `feat(github): real App install callback + installation token + PR creation`.

### Task 3: Sentry — real REST API for issue backfill + project assertion

**Files:**
- Modify: `services/control-plane/internal/adapter/integration/sentry/provider.go`
- Create: `services/control-plane/internal/adapter/integration/sentry/client.go`

**What real means:**
- `Connect` accepts `{auth_token, organization_slug, project_slug}`. Validate token reachability against `GET /api/0/organizations/{slug}/projects/` — confirm the project exists.
- `Status` — calls `GET /api/0/projects/{org}/{project}/stats/` (lightweight) → returns "healthy" + project event count last 1h.
- `HandleWebhook` — keep HMAC verify, but on `issue.created` immediately POST to internal `IncidentDetected` channel (Sentinel's existing detector path) with `{title, culprit, fingerprint, level, stacktrace_summary}` from Sentry's event payload (not just `incidents_raw` insert).
- New method `BackfillRecentIssues(ctx, since)` for catching up on missed webhooks — pulls `GET /api/0/projects/{org}/{project}/issues/?statsPeriod=1h&query=is:unresolved`.

- [ ] Step 1: Test `TestSentry_ValidatesProjectOnConnect` (mock httptest server).
- [ ] Step 2: Impl `Connect`.
- [ ] Step 3: Test + impl `TestSentry_BackfillRecentIssues`.
- [ ] Step 4: Test + impl `TestSentry_WebhookDispatchesIncidentDetected` — verifies the Sentinel channel sees the event.
- [ ] Step 5: Cron — every 5 min, run backfill for connected tenants.
- [ ] Step 6: Commit `feat(sentry): real REST client + issue backfill cron + sentinel dispatch`.

### Task 4: ArgoCD — real API for sync + rollback

**Files:**
- Modify: `services/control-plane/internal/adapter/integration/argocd/provider.go`
- Create: `services/control-plane/internal/adapter/integration/argocd/client.go`

**What real means:**
- `Connect` accepts `{server_url, auth_token, project, app_name}`. Validate via `GET /api/v1/applications/{app_name}` (returns 200 if reachable+authz).
- `Status` — calls `GET /api/v1/applications/{app_name}` → returns sync status (`Synced`/`OutOfSync`) + health (`Healthy`/`Degraded`).
- New method `SyncApp(ctx, app, revision)` — `POST /api/v1/applications/{app}/sync` body `{revision: sha}`.
- New method `Rollback(ctx, app, toRevision)` — `POST /api/v1/applications/{app}/rollback` body `{id: <history_id>}`.
- Approval Gate calls `SyncApp` on green; `Rollback` on SLO breach (existing Phase 6 wiring point).

- [ ] Step 1: Test + impl `TestArgoCD_ValidatesAppOnConnect`.
- [ ] Step 2: Test + impl `TestArgoCD_SyncReturnsOperationState`.
- [ ] Step 3: Test + impl `TestArgoCD_RollbackToLastHealthy`.
- [ ] Step 4: Wire into Approval Gate's "auto-merge → sync" path.
- [ ] Step 5: Commit `feat(argocd): real sync + rollback API client`.

### Task 5: Slack — flip from "coming soon" + DM approver flow

**Files:**
- Modify: `services/control-plane/internal/adapter/integration/slack/provider.go` (already 75% real)
- Modify: `apps/web/app/(app)/console/integrations/client.tsx` — flip Slack from "Coming Soon" to "Configure"
- Create: `services/control-plane/internal/transport/http/handler/slack_interactivity.go`

**What real means:**
- `Connect` runs Slack OAuth v2 install flow (`GET /v1/integrations/slack/install` → redirect to `slack.com/oauth/v2/authorize`, callback exchanges code for bot token via `POST oauth.v2.access`).
- `Status` — `auth.test` → returns bot name + workspace.
- Approval Gate posts approval block to a configured channel; high-severity DMs the approver directly using `chat.postMessage` to a user ID resolved via `users.lookupByEmail`.
- New endpoint `POST /v1/integrations/slack/interactivity` — handles button clicks ("Approve" / "Reject") + verifies Slack request signature, routes back into `approvals` repo.
- Flip UI card from `available:false` → `true`.

- [ ] Step 1: Test + impl `TestSlack_OAuthV2Exchange`.
- [ ] Step 2: Test + impl `TestSlack_InteractivityVerifiesSignature`.
- [ ] Step 3: Wire interactivity endpoint to existing `approvals.Decide`.
- [ ] Step 4: Flip UI flag.
- [ ] Step 5: Commit `feat(slack): real OAuth + interactivity + approver DM`.

### Task 6: Datadog — new adapter

**Files:**
- Create: `services/control-plane/internal/adapter/integration/datadog/provider.go`
- Create: `services/control-plane/internal/adapter/integration/datadog/client.go`
- Modify: `services/control-plane/internal/adapter/integration/factory.go` — register
- Modify: `services/control-plane/internal/domain/integrations.go` — add `IntegrationDatadog` enum
- Migration: `services/control-plane/migrations/0021_datadog.up.sql` — none needed (uses existing `integrations` table)
- Modify: `apps/web/app/(app)/console/integrations/client.tsx` — flip available
- Add to `dto.AvailableIntegrations` list

**What real means:**
- `Connect` accepts `{api_key, app_key, site}` (`site` is `datadoghq.com` / `datadoghq.eu` / etc). Validate via `GET /api/v1/validate`.
- `Status` — same validate endpoint.
- New port methods on `domain.MetricsProvider` (NEW interface): `QueryAnomaly(ctx, query, window)` + `SubscribeAlerts(ctx) <-chan AlertEvent`.
- Webhook receiver `POST /v1/integrations/datadog/webhook` — Datadog `alert.triggered` → emit `IncidentDetected` to Sentinel (same path as Sentry).
- Sentinel becomes multi-source: Sentry + Datadog.

- [ ] Step 1: Add domain enum + register in factory.
- [ ] Step 2: Test + impl `TestDatadog_ValidatesKeyOnConnect`.
- [ ] Step 3: Test + impl `TestDatadog_WebhookDispatchesIncidentDetected`.
- [ ] Step 4: UI flip + Configure dialog.
- [ ] Step 5: Commit `feat(datadog): adapter + webhook → sentinel`.

### Task 7: PagerDuty — new adapter

**Files:**
- Create: `services/control-plane/internal/adapter/integration/pagerduty/provider.go`
- Create: `services/control-plane/internal/adapter/integration/pagerduty/client.go`
- Modify: `services/control-plane/internal/adapter/integration/factory.go`
- Modify: `services/control-plane/internal/domain/integrations.go` — add `IntegrationPagerDuty`
- Modify: `apps/web/app/(app)/console/integrations/client.tsx`

**What real means:**
- `Connect` accepts `{api_token, service_id}`. Validate via `GET /users/me`.
- New port `domain.OnCallProvider` with `WhoIsOnCall(ctx, escalationPolicyID) (User, error)`.
- Approval Gate consults `WhoIsOnCall` for high-severity human-required incidents → DM that user via Slack (cross-integration).
- Webhook `POST /v1/integrations/pagerduty/webhook` — `incident.triggered` → emit `IncidentDetected` (third Sentinel source).
- New method `Trigger(ctx, severity, summary, details)` — creates a PagerDuty incident when NEXIS detects something the customer hasn't seen (escalation path for Sentinel-only detection).

- [ ] Step 1: Domain enum + factory.
- [ ] Step 2: Test + impl `TestPagerDuty_WhoIsOnCall`.
- [ ] Step 3: Test + impl `TestPagerDuty_TriggerIncident`.
- [ ] Step 4: UI flip.
- [ ] Step 5: Commit `feat(pagerduty): adapter + on-call query + escalation trigger`.

---

## Sentinel cutover (Task 8)

Make Sentinel a real multi-source detector. Today it polls Sentry only.

**Files:**
- Modify: `services/control-plane/internal/agents/sentinel/detector.go`
- Modify: `services/control-plane/internal/transport/http/handler/sentinel_admin.go`

- [ ] Step 1: Refactor Sentinel to consume from a `chan IncidentDetected` rather than poll Sentry.
- [ ] Step 2: Sentry adapter → publishes to this channel from webhook + backfill cron.
- [ ] Step 3: Datadog adapter → publishes from webhook.
- [ ] Step 4: PagerDuty adapter → publishes from webhook.
- [ ] Step 5: Sentinel correlation window: dedupe events within 60s by fingerprint (Sentry `fingerprint` / Datadog `alert_id` / PagerDuty `incident_id`).
- [ ] Step 6: Test `TestSentinel_DedupesAcrossSources`.
- [ ] Step 7: Commit `feat(sentinel): multi-source incident detection (sentry+datadog+pagerduty)`.

---

## Frontend (Task 9)

**Files:**
- Modify: `apps/web/app/(app)/console/integrations/client.tsx`
- Create: `apps/web/components/integrations/ConfigureDialog.tsx`
- Create: `apps/web/components/integrations/HealthPill.tsx`

- [ ] Step 1: `HealthPill` — renders "Healthy 230ms" green / "Degraded — 401" amber / "Down" red / "Disconnected" zinc.
- [ ] Step 2: `ConfigureDialog` — per-integration config form (Radix Dialog). Fields driven by a manifest in `lib/integrations-config.ts` (GitHub: no form, OAuth redirect; Sentry: org_slug + project_slug + auth_token; ArgoCD: server_url + token + project + app_name; Slack: OAuth redirect; Datadog: api_key + app_key + site; PagerDuty: api_token + service_id).
- [ ] Step 3: All cards show `HealthPill` next to provider name.
- [ ] Step 4: Flip Slack + Datadog + PagerDuty to available.
- [ ] Step 5: Build + typecheck.
- [ ] Step 6: Commit `feat(web): real integration configure dialog + health pill`.

---

## End-to-end demo recovery (Task 10)

Acceptance: a `seed-sample` call now triggers the real path through every adapter that's wired.

- [ ] Step 1: Create test fixture repo (`fixtures/demo-app/`) — a tiny Express app with a known null-pointer in `routes/orders.js`.
- [ ] Step 2: Sentry SDK installed in fixture; throws on hitting `/orders/null`.
- [ ] Step 3: Datadog metric on `5xx_rate` configured.
- [ ] Step 4: ArgoCD app `nexis-demo` pointing at fixture repo.
- [ ] Step 5: Triggering the fault: Sentry webhook fires → Sentinel emits `IncidentDetected` → recovery pipeline runs L1/L2 → GitHub PR opens → ArgoCD syncs → green.
- [ ] Step 6: Run end-to-end on local docker-compose with `.env.integration` populated. Commit transcript to `docs/demos/2026-05-13-real-recovery-transcript.md`.
- [ ] Step 7: Commit `feat: end-to-end real-integration demo`.

---

## Verification (final gate)

```bash
cd services/control-plane && make build && make vet && make arch && make test
cd ../.. && pnpm --filter @nexis/web typecheck && pnpm --filter @nexis/web build
docker compose up -d && curl http://localhost:8080/healthz
# Run the end-to-end demo from Task 10
```

All green = real integrations shipped. Tag: `v0.9.0-real-integrations`.

---

## Effort estimate

| Task | Wall-clock (with parallel BE+FE) |
|---|---|
| 0+1 shared groundwork | 30 min |
| 2 GitHub real | 90 min |
| 3 Sentry real | 60 min |
| 4 ArgoCD real | 60 min |
| 5 Slack flip + interactivity | 45 min |
| 6 Datadog adapter | 75 min |
| 7 PagerDuty adapter | 60 min |
| 8 Sentinel multi-source | 45 min |
| 9 FE config dialog + health pill | 60 min |
| 10 End-to-end demo | 90 min |
| **Total** | **~10 hours** wall-clock with parallel BE+FE dispatching |

Sequential (no parallel agents): ~20 hours. Pattern B + C dispatching (one BE per integration in parallel × 6) cuts this roughly in half.

---

## Non-goals

- Phase 7 cloud cutover (Terraform, ECS, RDS) — separate work, separate plan.
- Phase 8 thesis writing + eval benchmark — separate work, separate plan.
- Phase 9 polish — separate work.
- Real OAuth-token rotation jobs — covered by existing 1-hour installation token caching for GitHub; other integrations use long-lived tokens.
- Customer-self-serve uninstall via the third party's UI — `HandleWebhook` covers `installation.deleted` for GitHub; the others use disconnect-from-console only.
