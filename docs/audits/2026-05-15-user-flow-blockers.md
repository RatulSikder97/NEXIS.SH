# User-Flow Blockers — 2026-05-15

## Summary
Audited by walking every Configure dialog wire-shape, the project Connect wizard, the recovery → approval flow, and the dashboards' polling endpoints. Found **18 user-flow blockers** that static analysis missed: 6 CRITICAL (will break the demo / first-time user / approver UX), 7 HIGH (broken for a major persona — operator reviewing a recovery, owner using non-GitHub integrations), 4 MEDIUM, 1 LOW. The single most damaging finding is **B-1**: the "Reject" button silently approves the recovery because `lib/approvals.ts:reject()` POSTs to `/approve`. Three discovery endpoints surface 404 in the wizard (sentry/projects, slack/channels) or aren't surfaced at all (patch diff viewer). Several integration provider configs are missing fields the backend requires for the production path (e.g. `escalation_policy_id` for PagerDuty's OnCallFor).

## CRITICAL (will break the demo / first-time user)

### B-1: "Reject" button silently APPROVES the recovery
**Where:** `apps/web/lib/approvals.ts:156-167`
**User flow it breaks:** /console/approvals → click "Reject" on a pending decision. The user types a rejection note, hits Reject, and the recovery is APPROVED and merged.
**Wire-shape evidence:**
```ts
reject: async (wsId, runId, notes?) => {
  const r = await fetch(`${API}/v1/workspaces/${wsId}/pipelines/${runId}/approve`, {  // <-- /approve, not /reject
    method: "POST",
    body: JSON.stringify({ decision: "reject", notes }),  // BE ignores `decision`
  });
```
The BE (`services/control-plane/internal/transport/http/handler/pipeline_decide.go:25-27`) wires `PipelineApprove` and `PipelineReject` as two distinct routes (`/approve` → `domain.ApprovalApproved`, `/reject` → `domain.ApprovalRejected`). The body schema `pipelineDecideReq` only reads `notes`; the `decision` field in the body is silently ignored. So POSTing `{decision: "reject"}` to `/approve` triggers an APPROVAL.
**Why it slipped earlier audits:** Static analysis sees both endpoints exist on the BE and both methods exist on the FE SDK. Only walking the click confirms they're connected wrong.
**Fix sketch:** Change line 158 to `/reject` and drop the `decision` field from both bodies (BE ignores it anyway).

### B-2: `/v1/workspaces/{ws}/approvals/pending` route is missing on the BE
**Where:** FE call site: `apps/web/lib/approvals.ts:115-122`. BE: not mounted anywhere in `services/control-plane/internal/transport/http/server.go`.
**User flow it breaks:** /console/approvals page server-fetches this endpoint (`apps/web/app/(app)/console/approvals/page.tsx:43-50`) and ALWAYS gets a 404. The "pending approvals" list is permanently empty. The sidebar badge (`Sidebar.tsx:138-163 usePendingApprovalsCount`) also relies on this endpoint and stays at 0 forever.
**Why it slipped earlier audits:** Grepping for "approvals/pending" only finds it in DTOs (`dto/ops.go:147`) — the route mount is genuinely absent.
**Fix sketch:** Mount `g.Get("/v1/workspaces/{ws_id}/approvals/pending", handler.ApprovalsList(deps.Approvals))` in the protected group of `server.go`. Handler reads `approval_decisions WHERE decision='pending' AND workspace_id=$1` joined with `workflow_runs`.

### B-3: Slack OAuth install callback has the same RLS WITH-CHECK bug GitHub had
**Where:** `services/control-plane/internal/transport/http/handler/integration_oauth.go:294-334` (`SlackInstallCallback`).
**User flow it breaks:** /console/integrations → click Configure on Slack → "Continue with Slack" → bounce through Slack OAuth → callback hits `/v1/integrations/slack/callback` → handler calls `p.Connect(r.Context(), princ, ...)` WITHOUT running it through `runWithTenantTx`. The integrations row INSERT then trips the WITH-CHECK policy from migration 0025 because `app.current_org_id` is not pinned on the connection.
**Why it slipped earlier audits:** `GitHubInstallCallback` (line 234-238) was just patched to use `runWithTenantTx`; the sibling Slack handler was not.
**Fix sketch:** Wrap the `p.Connect(...)` call in `runWithTenantTx(r.Context(), appPool, princ, func(ctx) error { ... })`. Plumb `appPool` through the handler signature — server.go line 281 needs to pass `deps.AppPool` to `SlackInstallCallback`.

### B-4: Per-project Overview tab shows ALL incidents (project filter is ignored)
**Where:** BE: `services/control-plane/internal/transport/http/handler/pipelines.go:86-117` (`PipelinesList`). FE: `apps/web/app/(app)/console/projects/[id]/page.tsx:97-115`.
**User flow it breaks:** /console/projects/{id} → Overview tab. The page fetches `/v1/workspaces/{ws}/incidents?project_id={id}&limit=10`, that 404s, falls back to `/v1/workspaces/{ws}/pipelines?project_id={id}&limit=10`. **`PipelinesList` reads no query params except `limit` and `before`** — `project_id` is silently dropped. The user sees every incident in the workspace under every project tab, with no way to scope.
**Why it slipped earlier audits:** Static analysis can't see that the FE query string is ignored by the BE; the routes both exist.
**Fix sketch:** Add `project_id` to `domain.WorkflowFilter` and join `workflow_runs.input::jsonb->>'project_id' = $X` in `PipelinesList`. (The Demo handler already stamps `project_id` into `demoPayload` at line 287-289.) Then advertise the same filter in `OrgActivity` for the activity tab.

### B-5: Approver cannot see the patch — no diff viewer exists in the approval flow
**Where:** Workflow emits `patch_key` to `activity_events.payload` (`services/control-plane/internal/workflow/recovery/activities.go:701`). No HTTP endpoint exists to fetch the patch from MinIO; no FE component renders a diff outside `/console/eval/[id]/client.tsx`.
**User flow it breaks:** /console/approvals → click an approval row → drill into the recovery → the approver sees per-agent payloads with `patch_key: "..."` strings but never sees the actual code change. They must approve or reject blind.
**Wire-shape evidence:** Grep `grep -rn "patch_key\|presigned\|/v1/patches" services/control-plane/internal/transport/` returns zero hits. The MinIO patchstore has `SignURL`/`Get` methods (`adapter/patchstore/s3/store.go`) but nothing wires them to an HTTP route.
**Why it slipped earlier audits:** The pipeline timeline renders test counts (line 227 of `ActivityTimeline.tsx`) but the comment mentions `patch_key` only in a docstring — never code that resolves it.
**Fix sketch:** Add `g.Get("/v1/workspaces/{ws_id}/pipelines/{run_id}/patch", handler.PipelinePatch(patchStore))` returning a presigned URL or the inline unified diff. Add a `<PatchDiffBlock>` panel to `apps/web/app/(app)/console/incidents/[id]/client.tsx` (live demo target) and `apps/web/app/(app)/console/approvals/client.tsx` (approval modal).

### B-6: 23 of 27 Live Demo scenarios POST → 400 "invalid scenario"
**Where:** BE allowlist: `services/control-plane/internal/transport/http/handler/pipelines.go:183-188` accepts only `{schema-drift, null-deref, oom, synthetic}`. FE catalog: `apps/web/components/live-demo/SCENARIOS.ts` declares 27+ scenarios (`unhandled_promise_rejection`, `regex_catastrophic_backtracking`, `goroutine_leak`, …) and only `null-deref` has `ready: true`. The disabled UX hides the wire gap until someone toggles the flag.
**User flow it breaks:** /console/live-demo → click "Run scenario" on anything other than null-deref. The button is disabled by `ready: false`, so today no user can reach this. BUT the FE has 26 cards labelled "coming soon" — a user reasonably expects 3 ready (matching the eval matrix's three labels) since the BE accepts 4. The UX silently undersells the wired capability.
**Why it slipped earlier audits:** Both sides match (`ready: true` count = ready BE allowlist), but the mismatch in surface area is invisible to a static scan.
**Fix sketch:** Either flip `ready: true` on the three scenarios the BE actually accepts (`schema-drift`, `oom`, `synthetic`), or drop the 23 unready cards from the catalog until a fixture lands. Update the catalog comment at `SCENARIOS.ts:6` to point to the live allowlist.

## HIGH (broken for specific personas / less-common paths)

### B-7: PagerDuty Configure flow omits `escalation_policy_id` — `OnCallFor` always errors
**Where:** FE manifest: `apps/web/lib/integrations-config.ts:181-204` lists only `api_token` + `service_id`. BE adapter: `services/control-plane/internal/adapter/integration/pagerduty/provider.go:103-150` accepts an optional `escalation_policy_id`. **`OnCallFor` (line 276-292) returns `pagerduty: no escalation_policy_id configured` when it's empty.**
**User flow it breaks:** A high-severity recovery fires → Approval Gate tries to look up the on-call user via `Provider.OnCallFor` → errors "no escalation_policy_id". The approver flow has no PagerDuty on-call hint, and any "DM the on-call via Slack" path (`slack.DMUserByEmail`) is dead.
**Why it slipped earlier audits:** Optional in the adapter; static scan doesn't connect the optional-field gap to the OnCallFor consumer.
**Fix sketch:** Add `{name: "escalation_policy_id", label: "Escalation policy ID", type: "text", required: false, help: "Profile → Escalation Policies → ID"}` to the PagerDuty manifest. Surface the same field in the Project Connect Wizard (already exists at line 871-879) so per-project routing works.

### B-8: `/v1/integrations/sentry/projects` is missing — wizard always falls back to manual slug entry
**Where:** FE call site: `apps/web/components/projects/ProjectConnectWizard.tsx:543-547`. BE: not mounted. Server.go has no `/sentry/projects` handler; only `/sentry/probe` exists.
**User flow it breaks:** Project Connect Wizard → Step 3 "Connect Sentry" with Sentry already connected. The picker shows `Loading Sentry projects…` then degrades to two text fields. Always.
**Why it slipped earlier audits:** Wizard degrades gracefully so it doesn't error; the picker just never appears.
**Fix sketch:** Mount `g.Get("/v1/integrations/sentry/projects", handler.SentryProjectsList(deps.Integrations))`. Handler reuses the Sentry adapter's `client.ValidateToken` which already returns `[]ProjectRef` (`adapter/integration/sentry/client.go:96-109`).

### B-9: `/v1/integrations/slack/channels` is missing — wizard always falls back to channel-ID paste
**Where:** FE call site: `apps/web/components/projects/ProjectConnectWizard.tsx:548-552`. BE: not mounted.
**User flow it breaks:** Same as B-8 — Step 4 "Monitoring" Slack section. Auto-complete never appears; user must paste a `C01234ABCDE` channel ID by hand.
**Fix sketch:** Mount `g.Get("/v1/integrations/slack/channels", handler.SlackChannelsList(deps.Integrations))`. Use Slack adapter's bot token + `conversations.list` call.

### B-10: GitHub repos dropdown returns rows but the wizard's `installation_id` field stays undefined
**Where:** BE: `services/control-plane/internal/adapter/integration/github/client.go:194-200` (`RepoDetail` has `FullName`, `DefaultBranch`, `Private`, `HTMLURL` only). FE: `apps/web/components/projects/ProjectConnectWizard.tsx:686-691` reads `r.installation_id` to set `github_installation_id` selector.
**User flow it breaks:** Project Connect Wizard → Step 2 → user picks a repo from the dropdown. The wizard sets `github_installation_id: undefined` (the JSON field never lands in the body). BE's `requireGitHub` check (`usecase/projects.go:388-398`) accepts `0` so the project gets created — BUT downstream gitops PR-open paths reading `selectors.github_installation_id` will look up a different installation than the one the user selected if multiple are connected.
**Why it slipped earlier audits:** Both sides "work" — the field is just permanently empty.
**Fix sketch:** Add `InstallationID int64 \`json:"installation_id"\`` to `RepoDetail` and populate it in `ListInstallationReposDetailed` (the token's installation id is in scope). Already typed correctly on the FE.

### B-11: Sentry adapter expects `client_secret` for webhook HMAC but the FE manifest never asks for it
**Where:** BE: `services/control-plane/internal/adapter/integration/sentry/provider.go:136-158` — `client_secret` field of the `Connect` config. If empty it defaults to `auth_token`, which means **the customer must configure their Sentry webhook signing key to equal their auth token**, an undocumented and bizarre requirement.
**User flow it breaks:** /console/integrations → Configure Sentry → user pastes org_slug + project_slug + auth_token → Connect succeeds → first real Sentry webhook delivery arrives → HMAC verification fails because the customer set a different Sentry "client secret" in their dashboard.
**Why it slipped earlier audits:** Adapter has the fallback so unit tests pass; integration with the actual Sentry webhook config doesn't get exercised.
**Fix sketch:** Add `{name: "client_secret", label: "Webhook signing secret", type: "password", required: false, help: "Settings → Developer Settings → Webhook → Client Secret. Leave blank to reuse the auth token (uncommon)."}` to the Sentry manifest. Generate one client-side via `generateWebhookSecret()` (already exists at `apps/web/lib/integrations.ts:247`) and display it once.

### B-12: PagerDuty + Datadog webhook secrets are not configured in compose → all PD/DD webhooks fail HMAC
**Where:** `docker-compose.yml:359-441` lists every control-plane env var; `PAGERDUTY_WEBHOOK_SECRETS` and `DATADOG_WEBHOOK_SIGNING_SECRET` are absent.
**User flow it breaks:** Customer configures PagerDuty's outbound webhook URL (`/v1/integrations/pagerduty/webhook?org=...`) and triggers a test. The adapter returns `pagerduty: no webhook secrets configured` (`provider.go:238`) → HTTP 500 → webhook delivery never lands in `incidents_raw`. Same shape for Datadog when a per-tenant `webhook_secret` is also unset.
**Why it slipped earlier audits:** Compose works for the demo path (Sentry+GitHub only). The two non-Sentry incident sources are entirely untested.
**Fix sketch:** Add both env vars to `docker-compose.yml` (e.g. `PAGERDUTY_WEBHOOK_SECRETS: ${PAGERDUTY_WEBHOOK_SECRETS:-}` and `DATADOG_WEBHOOK_SIGNING_SECRET: ${DATADOG_WEBHOOK_SIGNING_SECRET:-}`). Document in `.env.example` how to mint them with `openssl rand -hex 32`.

### B-13: Slack OAuth requires `SLACK_CLIENT_ID` + `SLACK_CLIENT_SECRET` but neither is wired in compose
**Where:** `services/control-plane/internal/adapter/integration/real.go:89-102` — Slack flips to OAuth mode only when `cfg.SlackClientID != "" && len(cfg.SlackClientSecret) > 0`. `docker-compose.yml` has neither.
**User flow it breaks:** /console/integrations → Configure Slack → "Continue with Slack" → bounce to `/v1/integrations/slack/install` → handler 503s `slack: oauth credentials not configured` (line 268 of integration_oauth.go).
**Fix sketch:** Add `SLACK_CLIENT_ID`, `SLACK_CLIENT_SECRET`, `SLACK_SIGNING_SECRET`, `SLACK_APP_REDIRECT_URI` to the compose `control-plane.environment` block, all defaulted to empty strings so dev still boots without them. Surface a clearer 503 message in the FE Configure dialog so the user sees "Slack OAuth not configured" instead of a generic redirect-failed message.

## MEDIUM

### B-14: Slack interactivity (button clicks in DMs) doesn't write to webhook_deliveries
**Where:** `services/control-plane/internal/transport/http/handler/slack_interactivity.go` does not call `logDelivery`.
**User flow it breaks:** /console/webhooks page → operator wants to audit "did the approver click Approve in Slack?" — no row. Every other provider's HandleWebhook flows through `Webhook` / `WebhookByQuery` which both call `logDelivery`. Slack interactivity is a sibling but skips it.
**Fix sketch:** After the signature verifies in `SlackInteractivity`, call `logDelivery(ctx, deliveries, orgID, "slack", "interactivity", "processed", elapsed, body, hdrs, ip, "")`.

### B-15: Cost page sums eval-only spend instead of the dedicated cost endpoint
**Where:** `apps/web/app/(app)/console/cost/page.tsx:30-54` iterates workspaces and reads `/v1/workspaces/{ws}/eval` to compute MTD spend. The dedicated `/v1/orgs/{org_id}/cost` endpoint exists (`handler/ops_cost.go:28`) and returns the real per-org token-ledger rollup, but the page never uses it.
**User flow it breaks:** /console/cost shows only eval-harness runs in MTD spend. Real production recovery runs (which write to `token_ledger`) are absent. A new tenant who hasn't kicked off any eval runs sees `$0` MTD even after running 50 recoveries.
**Fix sketch:** Replace the eval-list iteration with a single `fetch(${API}/v1/orgs/${orgId}/cost)` call. The cost endpoint already returns the per-agent and per-day breakdown the client needs.

### B-16: Project Connect Wizard accepts un-prefixed `datadog_service_tag` and lets the user submit
**Where:** FE wizard `apps/web/components/projects/ProjectConnectWizard.tsx:884-898` shows a placeholder `service:checkout` but no client-side validation. BE (`usecase/projects.go:416-418`) returns `ErrInvalidSelectors: datadog_service_tag must start with 'service:'`.
**User flow it breaks:** User types `checkout` → POST → 400 banner appears in the wizard. Recoverable but a poor UX — the field looks fine, the form lets you advance through Step 5, only the final Create button surfaces the error.
**Fix sketch:** Add a `pattern="^service:.*"` to the input and a client-side helper string. Same for `datadog_env_tag` (`env:`).

### B-17: Project Connect Wizard's Owner User ID is a freeform text field with no lookup
**Where:** `apps/web/components/projects/ProjectConnectWizard.tsx:582-590`. The placeholder is the caller's own user id and the hint says "Defaults to you", but a non-default owner requires the user to know another user's UUID by heart.
**User flow it breaks:** A team owner wants to assign a project to a different engineer in the org. They have no way to look up that user's id from the wizard.
**Fix sketch:** Add a `/v1/orgs/{org_id}/users` BE endpoint returning the org membership list, and replace the text input with a typeahead. Out of scope for a quick fix — current behavior is functional; only the UX is poor.

## LOW

### B-18: ConfigureDialog OAuth flow surfaces a generic "Connect failed" string when the BE 503s
**Where:** `apps/web/components/integrations/ConfigureDialog.tsx:183-184`. The OAuth path navigates `window.location.assign(url)` which doesn't await an error response — instead, the BE 302s to `/console/integrations?install_error=<code>`. The integrations page reads `?installed=github` (line 165) but does not read `install_error`.
**User flow it breaks:** Slack install with missing env credentials → user is redirected to `/console/integrations?install_error=provider_unavailable` and sees zero feedback. No banner, no toast.
**Fix sketch:** In `apps/web/app/(app)/console/integrations/client.tsx:160-179`, also handle `searchParams.get("install_error")` and render the error code as a banner.

## Quick wins (≤30 min each)

| # | File:line | Fix | Wave to dispatch |
|---|---|---|---|
| B-1 | `apps/web/lib/approvals.ts:158` | Change URL from `/approve` to `/reject`; drop `decision` field from both bodies. | frontend-engineer |
| B-3 | `services/control-plane/internal/transport/http/handler/integration_oauth.go:317` | Wrap `p.Connect(...)` in `runWithTenantTx(r.Context(), appPool, princ, ...)`; thread `appPool` through `SlackInstallCallback` signature + server.go call site. | backend-engineer |
| B-7 | `apps/web/lib/integrations-config.ts:188-203` | Append optional `escalation_policy_id` field to PagerDuty manifest. | frontend-engineer |
| B-10 | `services/control-plane/internal/adapter/integration/github/client.go:194-200` | Add `InstallationID int64 \`json:"installation_id"\`` to `RepoDetail`; populate from the token's installation id in `ListInstallationReposDetailed`. | backend-engineer |
| B-11 | `apps/web/lib/integrations-config.ts:70-92` | Append optional `client_secret` (password) field to Sentry manifest; rename existing `auth_token` help text to clarify it doubles as HMAC if `client_secret` is blank. | frontend-engineer |
| B-12 | `docker-compose.yml:387` | Add `PAGERDUTY_WEBHOOK_SECRETS` and `DATADOG_WEBHOOK_SIGNING_SECRET` env vars (default empty); document in `.env.example`. | lead-software-engineer |
| B-13 | `docker-compose.yml:387` | Add `SLACK_CLIENT_ID`, `SLACK_CLIENT_SECRET`, `SLACK_SIGNING_SECRET`, `SLACK_APP_REDIRECT_URI` env vars. | lead-software-engineer |
| B-14 | `services/control-plane/internal/transport/http/handler/slack_interactivity.go` | Call `logDelivery(...)` after signature verify + after handler completes. | backend-engineer |
| B-16 | `apps/web/components/projects/ProjectConnectWizard.tsx:884-898` | Add `pattern="^service:.*"` to Datadog service tag input; similar for env tag. | frontend-engineer |
| B-18 | `apps/web/app/(app)/console/integrations/client.tsx:160` | Also handle `install_error` query param and render a banner. | frontend-engineer |

## Larger fixes (>1h)

| # | File:line | Fix | Wave to dispatch |
|---|---|---|---|
| B-2 | `server.go` + new handler | Mount `/v1/workspaces/{ws_id}/approvals/pending` route + handler that joins `approval_decisions` + `workflow_runs`. | backend-engineer |
| B-4 | `pipelines.go:86-117` + `domain.WorkflowFilter` | Thread `project_id` query param into `PipelinesList`; JSON-extract `input->>'project_id'` in SQL. Same for `OrgActivity`. | backend-engineer |
| B-5 | new `/v1/.../patch` route + diff viewer | Add `g.Get("/v1/workspaces/{ws_id}/pipelines/{run_id}/patch", ...)` returning the unified-diff from the patchstore for the latest Backend.Codegen `patch_key`. Add a `<PatchDiffBlock>` panel to the incident detail + approval modal. | backend-engineer + frontend-engineer |
| B-6 | `SCENARIOS.ts` + `pipelines.go:183-188` | Either expand BE allowlist + add fixtures, or shrink FE catalog to match. | backend-engineer (preferred) or frontend-engineer |
| B-8 | new sentry/projects handler | Mount `/v1/integrations/sentry/projects` reusing `client.ValidateToken`. | backend-engineer |
| B-9 | new slack/channels handler | Mount `/v1/integrations/slack/channels` using `conversations.list`. | backend-engineer |
| B-15 | `apps/web/app/(app)/console/cost/page.tsx` | Replace eval-list iteration with `fetch(${API}/v1/orgs/${orgId}/cost)`. | frontend-engineer |
| B-17 | new `/v1/orgs/{org_id}/users` + typeahead | Mount org-users endpoint; add user picker to wizard. | backend-engineer + frontend-engineer |

