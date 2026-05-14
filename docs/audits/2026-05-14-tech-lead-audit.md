# Tech-Lead Audit — 2026-05-14

**Scope:** Read-only structural audit of the NEXIS monorepo (`/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app`) focused on the cross-cutting concerns the DevOps, SQA, BE, FE agent audits won't catch: domain modelling, port discipline, dead code, plan-vs-code drift, multi-tenancy correctness, and the path to flipping the 22 roadmap integration cards.

## Summary
Phases 1–6 ship a real product surface (real Sentinel detector + 6 real integration adapters + Temporal-driven pipeline + L1 LLM spine + eval harness) but the "Phase 6 = MVP closed loop" claim in `docs/PROJECT_PLAN.md` is **not** matched by the workflow code: 3 of the 4 L2 agents and the entire GitOps service are unwired. The "9 AI agents" marketing language has zero alignment with the code — only 5 L1 agent implementations actually run. The recently-shipped projects/self-healing routing layer is plumbed end-to-end in the Sentinel path but is bypassed by the live webhook write path and by every billing/cost surface. The top three architectural risks are (a) **plan-vs-code drift around MVP closure**, (b) **multi-tenant write-side RLS holes** (`USING`-only policies + missing `FORCE ROW LEVEL SECURITY` on every table except `integrations`), and (c) **scattered "incident" domain types** that already make the projects-routing layer fragile and will collapse as soon as the 22 roadmap integrations land.

## Strategic risks (Phase 7+ blocker)

### R-1: The MVP closed loop is not actually closed
**Why this matters:** `docs/PROJECT_PLAN.md` line 617 marks `Phase 6 — Agents L2 + Approval Gate ← MVP CUT-LINE — Completed 2026-05-13`, listing Sentinel/Pathfinder/Synthesiser/Validator/Approval-Gate/GitOps as scope. Three of those are stubs in the code path that actually executes, and the recovery workflow never opens a PR. This is the difference between "I can demo this to a design partner" and "I cannot."

**Where it shows up:**
- `services/control-plane/internal/workflow/recovery/activities.go:662-666` — `PathfinderDiagnose` and `SynthesiserPlan` are still 1-line `a.stub(...)` calls. The L2 Pathfinder + Synthesiser provider implementations exist in `internal/adapter/agents/pathfinder/provider.go` and `.../synthesiser/provider.go`, and `internal/adapter/agents/init_l2/init_l2.go` builds the {name → Agent} map — but `grep -rn "init_l2" services/control-plane/` finds no callers. The L2 agent code is wired into `agents.Registry` *nowhere*, so even the `runAgent` dispatch path can't reach them. They are pure dead code.
- `services/control-plane/internal/workflow/recovery/activities.go:735-734` — only the 5 L1 agents call `a.runAgent(...)`; the 3 L2 agents return stubs unconditionally.
- `services/control-plane/internal/platform/config/config.go:326` defines `GitOpsURL = env("GITOPS_URL", "http://gitops:8082")`. `grep -rn "GitOpsURL"` returns only the definition line — control-plane never calls the gitops service. `services/gitops/internal/usecase/open_pr.go` (`OpenPRUsecase.Run`) is the entire PR-open flow; no caller exists outside the gitops service's own HTTP handler. So the workflow's DevOps activity emits YAML, validates it, and **stops** — no PR ever opens.
- The agent fleet UI at `services/control-plane/internal/transport/http/handler/agents.go:41-67` enumerates 9 agents in the catalog (`architect/backend/qa/devops/data_engineer + sentinel/pathfinder/synthesiser/validator_l2 + approval_gate`) but `internal/domain/agent.go:11-25` only defines 8 `AgentName` constants (no `AgentNameApprovalGate`, no `AgentNameSentinel`). The UI ships 9 cards; the type system enumerates 8 agents; the registry registers 5. Three different "agent counts" depending on where you look.

**What to do:** Either (a) wire `init_l2.New(...)` from `cmd/server/main.go` and replace the `PathfinderDiagnose` + `SynthesiserPlan` activity bodies with `runAgent` dispatches, and stand up a `GitopsClient` in `internal/adapter/gitops/` so `RecoveryPipeline` opens a PR after `ApprovalGateFinalize` succeeds; or (b) walk back the `Completed 2026-05-13` line on Phase 6 in `PROJECT_PLAN.md` so the rest of the team isn't operating off a false "MVP done" signal. Recommendation: do (a). Two-week budget. The Pathfinder + Synthesiser providers are written; only the wiring is missing.

### R-2: Write-side RLS holes across every tenant table except one
**Why this matters:** Plan §3.4 (line 248) and §2.3 (line 138) explicitly require `ENABLE ROW LEVEL SECURITY` + `FORCE ROW LEVEL SECURITY` + a policy with both `USING` and `WITH CHECK`. The actual migrations don't deliver this. Read-side isolation works; write-side does not. SOC 2 evidence is `WITH CHECK` — without it any code path with a typo'd `org_id` writes into the wrong tenant and RLS doesn't catch it.

**Where it shows up:**
- `grep -c "FORCE ROW LEVEL SECURITY" services/control-plane/migrations/*.up.sql` finds it on **exactly one** table — `integrations` (in `0018_phase6_gitops_role.up.sql:21`). Every other tenant table has `ENABLE` but not `FORCE`. The `nexis_app` role is the writer; if anyone in the codebase ever runs as the table owner (migration role, future seed scripts, debugging via `psql`) RLS is bypassed silently.
- `grep "WITH CHECK" services/control-plane/migrations/*.up.sql` finds **exactly two** lines that pair `USING` with `WITH CHECK` — `0019_phase7_stripe.up.sql:58-59` (stripe_customers) and `0018_phase6_gitops_role.up.sql:25` (gitops insert pass-through). Every other tenant table — `org_members`, `sessions`, `magic_tokens`, `api_keys`, `audit_log`, `workspaces`, `workflow_runs`, `incidents_raw`, `approvals`, `webhook_deliveries`, `projects`, `eval_runs`, `eval_transcripts`, `token_budgets`, `token_ledger`, the lot — uses `USING (org_id = current_setting(...))` only. A row inserted with a wrong `org_id` is accepted; only future SELECTs filter it out.
- `services/control-plane/migrations/0024_projects.up.sql:60` — `projects` table policy is `USING` only. No `WITH CHECK`, no `FORCE`. This is brand-new code (Stage 5 of the projects-routing plan).
- `WebhookByQuery` handler at `internal/transport/http/handler/webhooks.go:272-329` (used by Datadog + PagerDuty) reads `org` from the URL query string and binds it as the RLS GUC for the rest of the transaction. The HMAC check inside the adapter is the only thing protecting against cross-tenant writes — and on Datadog the HMAC check is `if len(secret) > 0` gated (`internal/adapter/integration/datadog/provider.go:202`). When a tenant connected Datadog without a webhook secret, an attacker who knows their `org_id` can POST to `/v1/integrations/datadog/webhook?org=<victim_org>` and write into `incidents_raw` for the victim org. RLS does not stop this — the GUC is bound to the attacker-supplied value and `WITH CHECK` is missing anyway.

**What to do:**
1. One migration adds `FORCE ROW LEVEL SECURITY` to every tenant table and rewrites every `tenant_isolation` policy to `USING (...) WITH CHECK (...)`. The `nexis_app` role already runs every request, so the operational footprint is zero; this is a hardening-only patch. Ship it before Phase 7.
2. Make Datadog refuse-on-missing-secret (mirror the PagerDuty behaviour at `internal/adapter/integration/pagerduty/provider.go:236-242`). The "soft fallback when no secret" path in `internal/adapter/integration/datadog/provider.go:268-272` is a vestige from the dev-bootstrap days and should fail closed.
3. The integration test fuzzer described in plan §0 ("swaps tenant IDs and asserts zero cross-leakage") is referenced 5 times in the plan; `tests/integration/rls_test.go` exists but I did not find one that asserts INSERTs into the wrong tenant are rejected. Add it.

### R-3: The "incident" concept is 5 overlapping types and will not survive Phase 7
**Why this matters:** Adding new integration adapters (the 22 marked roadmap) requires touching at least 3 of these types per adapter. The fingerprint-fields-everywhere pattern is already showing strain: every new selector source (GitLab repo, Splunk service, Honeycomb dataset, Kafka cluster, etc.) means adding a column to `projects`, a field to `RawIncident`, a field to `IncidentRow`, a field to `IncidentTrigger`, and a field to `IncidentFingerprint` — five places per integration, plus the SQL projection in `IncidentsRepo.PollFatalSince` and the conversion in `sentinel/rules.go:Apply`. By the time half the roadmap lands this is unmaintainable.

**Where it shows up:**
- `services/control-plane/internal/domain/agent.go:44` defines `IncidentPayload` (what L1 agents read).
- `services/control-plane/internal/domain/integration.go:80` defines `RawIncident` (what adapters persist).
- `services/control-plane/internal/domain/sentinel.go:29` defines `IncidentTrigger` (what detector emits) with `SentryOrganizationSlug/SentryProjectSlug/DatadogServiceTag/PagerDutyServiceID/GitHubRepo` fields **inlined directly** on the trigger struct.
- `services/control-plane/internal/domain/sentinel.go:64` defines `IncidentRow` (projection from `incidents_raw`) with the **exact same five** fingerprint fields inlined.
- `services/control-plane/internal/domain/project.go:107` defines `IncidentFingerprint` with the same five fields again, and the Router reconstructs it from `IncidentTrigger` fields in `internal/sentinel/router.go:93-100`.
- The fingerprint is then persisted to `incidents_raw.raw_payload->'_fingerprint'` (JSONB), not to dedicated columns. `services/control-plane/internal/adapter/repo/incidents_repo.go:60-99` writes JSONB; `PollFatalSince` at `:155-194` re-reads via `raw_payload->'_fingerprint'->>'sentry_organization_slug'` etc. — five JSONB extractions per row per Sentinel tick. The `projects` table has dedicated indexed columns for the same data (`0024_projects.up.sql:54-56`); `incidents_raw` does not. There is no index on the fingerprint JSON path. At 100 incidents/min the Sentinel tick will become the dominant query cost.

**What to do:**
1. Collapse the 5 types into one canonical `Incident` (the persistent shape) + a separate `IncidentEvent` (the immutable webhook envelope). Move the fingerprint to its own `IncidentSelectors` value type embedded in both. One field-add per new integration becomes one field-add, not five.
2. Migrate `incidents_raw` to add explicit `selector_source`, `selector_key`, `selector_secondary_key` columns indexed by `(org_id, selector_source, selector_key)`. The `projects` indexes already follow this shape per source; matching them in `incidents_raw` is what lets a sub-millisecond router lookup work at scale.
3. The router's persistence path (`internal/sentinel/router.go:127-138` calling `UpdateProjectID`) only fires from the Sentinel goroutine post-poll. Webhook ingest writes `project_id=NULL` and the column is filled in seconds later by the poll loop. Read-after-write callers (the workflow start at `internal/sentinel/detector.go:282-298`) lose the project_id on every fast turnaround. Move the project match into the INSERT path: each integration adapter has the fingerprint at hand when it calls `sink.Insert(...)`, so the repo can do `SELECT id FROM projects WHERE ... LIMIT 1` inline and persist `project_id` on the same row. Removes the eventually-consistent gap.

## Architectural debt

### A-1: `.arch.yaml` has been quietly weakened from the plan's §3.7 layering rules
`services/control-plane/.arch.yaml` permits:
- `usecase → adapter` (line 56) — the comment says this is for the eval harness. Plan §3.7 forbids this: "usecase depends only on domain interfaces — never on a concrete adapter." `internal/usecase/eval_runner.go:22-30` directly imports `architect`, `backend`, `qa`, `devops`, `data_engineer`, `llm`, `repo`, `retrieval` — that's ~9 concrete adapter packages from a usecase file.
- `transport → adapter` (line 70) — Plan §3.7: "transport never talks to adapter directly — always through a usecase." 25+ files under `internal/transport/` import `internal/adapter/repo`, `internal/adapter/integration`, `internal/adapter/audit` directly. The HTTP handler layer is doing usecase work.
- `adapter → adapter` (line 68) — needed for `integration/factory.go` to compose its 6 sub-adapters, fair enough, but the rule now permits any adapter package to depend on any other adapter package. There is no constraint stopping `internal/adapter/billing` from importing `internal/adapter/integration/github`, for example.
- `adapter → workflow` (line 68) — `internal/adapter/workflow/service.go` wraps the Temporal client + recovery workflow registration. The plan put workflow on top of adapter; this inverts it.

The rules are still enforced (CI runs go-arch-lint), but the rules don't match the plan's architecture chapter anymore. Either fix the imports or update the plan; right now `PROJECT_PLAN.md` describes an architecture that doesn't exist.

### A-2: Empty App-Router groups
`apps/web/app/console/`, `apps/web/app/dashboard/`, `apps/web/app/admin/`, and `apps/web/app/(console)/` are all empty directories. They exist as legacy stub trees from earlier phases and are not referenced. They don't ship anything but they confuse the routing surface (a developer adding a new admin page will reasonably try `/app/admin/...` before discovering everything actually lives under `/app/(app)/console/...`). Delete them.

### A-3: Three configure forms still in source after the Wave 1 cutover
`apps/web/components/integrations/GitHubConfigureForm.tsx`, `SentryConfigureForm.tsx`, `ArgoCDConfigureForm.tsx` are dead — the only references in source are comments in `(app)/console/integrations/client.tsx` saying "Wave 2 cleanup." Wave 1 has shipped. Remove them.

### A-4: 9-card vs 6-provider mismatch in the integration manifest
`apps/web/components/integrations/ProviderLogo.tsx:31-89` defines a `ProviderID` union with 28 entries. `apps/web/lib/integrations-config.ts:14-20` defines `IntegrationProvider` (the manifest-driven dialog input type) with **6**. The 28-card grid in `(app)/console/integrations/client.tsx:45-89` is built from the 28-entry `ProviderID` set; the Configure dialog only knows what to do for 6. Clicking Configure on `gitlab` or `splunk` either crashes the dialog or silently does nothing (the `available: false` flag disables the CTA, so it just dead-ends). The "live: 6 · roadmap: 22" counter at line 228 is accurate; the contract for each roadmap card flipping live is not declared anywhere.

The path to flipping a roadmap card requires, today: a new Go integration adapter package, a new `domain.IntegrationProvider` constant, a row in `integration/factory.go:NewRegistry`, a manifest entry in `lib/integrations-config.ts`, a logo in `ProviderLogo.tsx`, plus migration entries widening the various source/provider CHECK constraints (the `0016_phase6_provider_widen.up.sql` and `0021_real_integrations_widen.up.sql` and `0022_incidents_raw_source_widen.up.sql` migrations already exist for exactly this reason). That's 7 touchpoints per provider. Aim to get this to ≤2: declarative manifest entry (FE) + Go adapter file conforming to the `domain.Integration` port. The constant + factory + CHECK constraints should be derivable from the port; the manifest should be the source of truth FE-side.

### A-5: WorkOS provider is mostly local
`services/control-plane/internal/adapter/auth/workos/provider.go` (296 LOC) — `VerifyToken`, `Logout`, `GetUser`, `GetOrg` all delegate straight to the wrapped local provider. The only WorkOS-specific method is `ConsumeOAuthCode`. Plan §3.1 says "Identity = WorkOS"; today identity = local + a WorkOS callback. Passkeys, MFA, SAML, SCIM (the named capabilities in plan §0 line 26) are all served by the local provider. Either acknowledge this in the plan as Phase 7 work, or stop calling the current state "WorkOS integration."

### A-6: Token budgets are org-scoped; usage is workspace-scoped; recovery is project-scoped
`migrations/0011_phase5_agents.up.sql:2-14` — `token_budgets` is keyed by `(org_id, period_start)` only. `token_ledger` (line 16) ties to `workflow_run_id` but has no `workspace_id` or `project_id` columns. `usage_records` (Phase 3.5) is workspace-scoped. `projects` are the unit of recovery. When the user asks "what's my Q3 spend on the orders-api project?" — there's no answer to that query without scanning every `token_ledger` row and joining through `workflow_runs.project_id`. Per-project budgets table is the right next step. Suggested shape: `project_budgets (org_id, project_id, period_start, period_end, allowed_tokens_in, allowed_tokens_out, allowed_cost_cents)` with a check in the `LLMClient` middleware that picks the tightest applicable budget (project > workspace > org). Roll up to `org` for backwards compatibility — the existing org budget becomes the default cap.

### A-7: Sentinel polling source filter is locked to 3 providers
`internal/adapter/repo/incidents_repo.go:151-194` — `PollFatalSince` query has `source IN ('sentry','datadog','pagerduty')` hardcoded. Add an 8th adapter (Prometheus alerts, GitLab pipeline failures, Splunk) and it will write to `incidents_raw` but Sentinel will not see those rows until someone updates this SQL. The string list should come from `integration.Registry.Sources()` (a port the integration package exposes once the adapter set is canonical), not be hardcoded in the repo.

### A-8: `WebhookByQuery` query-string `org` parameter
`internal/transport/http/handler/webhooks.go:283` — `orgID := r.URL.Query().Get("org")`. The org_id is taken from a URL query parameter, then pinned as the RLS GUC for the rest of the transaction. This is required for providers like Datadog/PagerDuty whose webhook URL is configured per-customer, but the design loses one layer of safety: an unauthenticated attacker can submit a payload signed for org A to the URL `?org=B` — if the adapter's HMAC check uses the org-specific secret from `B`'s integration row, the call fails fast (good). If the adapter has any global signing secret fallback (Datadog does — see R-2 above), the attacker bypass works. Pattern-fix: when both per-tenant and global secrets are configured, the per-tenant secret MUST be required. The global is the bootstrap only.

### A-9: Stub fallback for every L1 agent
`internal/workflow/recovery/activities.go:217-272` — every L1 agent activity ends with "demote to stub fallback on LLM 401, network error, schema retries exhausted." This is great for dev demos. It's a serious concern in prod: a real LLM provider outage silently produces stub patches that look identical to real ones in the UI (`stub-fallback` is in the model field but the patch is structurally similar). The Approval Gate's "auto-merge on low severity" path could in principle auto-merge a stub patch. Add an explicit check in `ApprovalGateRoute`: if any upstream activity returned `model == "stub-fallback"`, force severity to high and require human approval. Otherwise a network blip on the LLM call rolls fake fixes to prod.

## Dead-code census

| Path | Status |
|---|---|
| `services/control-plane/internal/adapter/agents/init_l2/init_l2.go` | Wires Pathfinder + Synthesiser into a registry map. **Never called.** `grep -rn "init_l2" .` returns only the package definition + a doc-comment reference in `pathfinder/provider.go`. |
| `services/control-plane/internal/adapter/agents/pathfinder/provider.go` | 200+ LOC of Pathfinder agent (Neo4j + DoWhy). Reachable only via the unused `init_l2.New(...)`. |
| `services/control-plane/internal/adapter/agents/synthesiser/provider.go` | Same — reachable only via `init_l2.New`. The `RecoveryPipeline` workflow's `SynthesiserPlan` activity calls `a.stub(...)` instead. |
| `services/control-plane/internal/platform/config/config.go:326` (`GitOpsURL`) | Configured, never read. `grep -rn "GitOpsURL"` returns only the definition. |
| `services/gitops/internal/usecase/open_pr.go` | OpenPR flow + audit. No control-plane caller. The service is reachable via its own HTTP surface only; the recovery workflow never sends a request to it. |
| `services/control-plane/internal/transport/http/handler/agents.go:65-67` | The "approval_gate" agent entry in `agentCatalog` has no corresponding `domain.AgentName` constant. The handler treats it as a string; the agents API responds with the catalog row but no agent code can be dispatched under that name. |
| `apps/web/app/console/`, `apps/web/app/dashboard/`, `apps/web/app/admin/`, `apps/web/app/(console)/` | Empty App-Router directories. |
| `apps/web/components/integrations/GitHubConfigureForm.tsx` / `SentryConfigureForm.tsx` / `ArgoCDConfigureForm.tsx` | Imported nowhere outside dead comments. Replaced by `ConfigureDialog`. |
| `services/control-plane/internal/adapter/agents/llm.go:31` (`Agent` field on `LLMClient.LedgerEntry`) | Set by every agent provider; consumed only in `token_ledger` writes. No drift here; flagged so reviewers know the lineage. |

## Plan-vs-code drift

| Phase | Plan claim (PROJECT_PLAN.md) | Reality | Severity |
|---|---|---|---|
| 1 | "9 sections" landing page (§4.1) | All 9 sections present. ✅ | OK |
| 1 | LLM Provider abstraction with `OpenAIProvider` + `OllamaProvider` (§5 Phase 1) | Both impls present, switchable via `LLM_PROVIDER`. ✅ | OK |
| 1 | `optional AnthropicProvider later` (§0) | Not present. Not yet due. | Defer |
| 2 | "Postgres RLS on every tenant table" (§5 Phase 2) | Read side enforced; write side has no `WITH CHECK` on any table except 2; `FORCE` on 1. | High |
| 2 | "integration test fuzzer that swaps tenant IDs and asserts zero cross-leakage" (§0, §3.4) | `tests/integration/rls_test.go` exists. Visual inspection (file count, name) suggests SELECT side; INSERT-side fuzz not present. | High |
| 3 | "Sentry webhook receiver (HMAC verified)" (§5 Phase 3) | Sentry verifies HMAC strictly. ✅ | OK |
| 3.5 | "Workspace as the compute unit inside an org. After signup, owner is redirected to onboarding to create their first workspace" | Implemented; provisioning SSE animation real. ✅ | OK |
| 3.5 | "per-hour runtime metering" — `usage_records` per workspace per project | `usage_ticker.go:53` writes `Project: "default"` literal. Projects feature exists but not joined to billing. | Medium |
| 4 | "9 placeholder activities — each just logs + sleeps; full DAG wired with retries+timeouts" | Workflow has 9 activity types, DAG is real. Pathfinder/Synthesiser/Sentinel-after-real-detector are still stubs but plan said stubs for Phase 4 was OK. ✅ | OK |
| 5 | "5 L1 agents call llm.Provider only" | True. `cmd/server/main.go:351-357` registers exactly the 5 L1. ✅ | OK |
| 5 | "Eval harness running same incident through both providers" | `cmd/eval/main.go` + `usecase/eval_runner.go` real, hits OpenAI + Ollama sequentially, persists eval_transcripts. ✅ | OK |
| 6 | "Sentinel — streaming anomaly detector subscribed to Sentry + OTel metrics; emits IncidentDetected" | Real Sentinel detector goroutine exists. Polls `incidents_raw` for `level='fatal' AND source IN (sentry,datadog,pagerduty)`. Not subscribed to OTel metrics — the OTel metrics path is absent. | Medium |
| 6 | "Pathfinder — Neo4j codegraph + DoWhy causal inference … → root-cause hypothesis" | Pathfinder provider code exists; **workflow returns stub**. Pathfinder is unwired (R-1). | **Critical** |
| 6 | "Synthesiser — orchestrates retrieval + delegates to Backend or Data Engineer L1" | Synthesiser provider code exists; **workflow returns stub**. Unwired (R-1). | **Critical** |
| 6 | "Validator — runs sandbox + property-based tests" | Validator service + Hypothesis sidecar both present; `BackendCodegen` activity calls it (`activities.go:704-721`). ✅ | OK |
| 6 | "Approval Gate — severity router: low → auto-merge; medium → 2-min countdown then auto; high/unknown → human required. Slack + email + console" | `ApprovalGateRoute` activity + workflow signal/timer race both real. Notifier (slack + email + console) registered. ✅ | OK |
| 6 | "GitOps service — opens PR on tenant repo via GitHub App with patch + agent transcript + risk score" | **Service exists. No caller from control-plane.** R-1. | **Critical** |
| 6 | "ArgoCD app-of-apps wired; PR merge triggers sync; rollback on SLO breach" | ArgoCD adapter exists for receiving webhooks + token storage, but no rollback automation. SLO-breach trigger not present in code I traced. | High |
| 6 | "Live Pipeline Demo surface: 'Inject fault' button" | UI present, workflow runs against fixtures. ✅ | OK |
| 6 | "3 design partners walk through Live Demo without intervention" | Acceptance — not code-verifiable. | OK |
| 7 | "Cloud cutover — AWS Live" | Not marked Completed in plan, but Stripe migration (0019), Phase 7 spec, terraform skeleton, and AWS adapters under `internal/platform/aws/` all landed early. Plan should acknowledge "Phase 7 started, see DevOps audit for status." | Inform |

## Recommended next-2-week roadmap

1. **Close the MVP loop (R-1) — 5 days.** Wire `init_l2.New(...)` from `cmd/server/main.go`; replace stub bodies of `PathfinderDiagnose` + `SynthesiserPlan` with `runAgent(domain.AgentNamePathfinder, in)` / `runAgent(domain.AgentNameSynthesiser, in)`. Add a GitopsClient adapter (mirror the existing ValidatorClient pattern at `internal/adapter/validator/client.go`) and a `(*Activities).OpenPR` activity that runs after `ApprovalGateFinalize` succeeds. Record the PR URL on `PipelineOutput.PRURL`. This is what makes the "Completed Phase 6" line in the plan honest.
2. **One-migration RLS hardening (R-2) — 1 day.** Add `FORCE ROW LEVEL SECURITY` + rewrite every `tenant_isolation` policy with `WITH CHECK (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid)`. Then ship an integration test that, for every tenant table, asserts an `INSERT … (org_id = <wrong>)` from a tx with `app.current_org_id = <right>` raises `42501`/`P0001`. This is SOC 2 evidence.
3. **Datadog soft-fail closure (part of R-2) — 1 hour.** `internal/adapter/integration/datadog/provider.go:268-272` — change to "no secret = refuse webhook." Mirror PagerDuty.
4. **Domain consolidation (R-3) — 3 days.** Replace `IncidentPayload + RawIncident + IncidentRow + IncidentTrigger + IncidentFingerprint` with one `Incident` + one `IncidentEvent` + a `Selectors` value type. Migrate `incidents_raw` to add explicit `selector_source`, `selector_key` columns (indexed) and stop using JSONB for the routing keys. Move the projects match into the INSERT path so `incidents_raw.project_id` is filled on write, not via eventually-consistent backfill.
5. **Stub-fallback tripwire (A-9) — 0.5 day.** Force severity=high in `ApprovalGateRoute` when any prior activity has `model == "stub-fallback"`. Prevents a transient LLM outage from auto-deploying placeholder patches.
6. **Per-project token budget table (A-6) — 1 day.** Add migration + repo + middleware check. The eval harness UI already aggregates per-provider; per-project is the next natural axis.
7. **Dead-code purge — 0.5 day.** Delete `apps/web/app/{console,dashboard,admin,(console)}/` empty dirs, the three legacy ConfigureForm components, and either wire or delete the `init_l2` + Pathfinder + Synthesiser package trees (item 1 unblocks the wire path; otherwise they're gone).
8. **Roadmap integration scaffolding (A-4) — 2 days.** Define the contract for a new integration: one Go adapter package implementing `domain.Integration` + one manifest entry in `lib/integrations-config.ts`. Generate the `domain.IntegrationProvider` constant, the factory entry, and the migration CHECK widening from the adapter (via codegen or a registry pattern). After this, each of the 22 roadmap cards becomes a ~half-day exercise instead of a multi-file refactor.
9. **`.arch.yaml` reconciliation (A-1) — 0.5 day.** Decide whether to remove the loose `usecase → adapter` and `transport → adapter` permissions and refactor the violators (eval_runner, 25 handlers) to go through usecase, OR amend `PROJECT_PLAN.md` §3.7 to match the current rules. Don't leave the plan and the lint config in disagreement.

---

**Files referenced:**

- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/docs/PROJECT_PLAN.md`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/.arch.yaml`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/cmd/server/main.go`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/internal/domain/{agent,integration,project,sentinel,ports}.go`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/internal/sentinel/{detector,router,rules}.go`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/internal/workflow/recovery/{workflow,activities,types}.go`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/internal/adapter/agents/{init_l2,pathfinder,synthesiser,registry}.go` (or `…/{init_l2,pathfinder,synthesiser}/provider.go`)
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/internal/adapter/integration/{factory,real}.go` + `integration/{sentry,datadog,pagerduty,github,argocd,slack}/provider.go`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/internal/adapter/repo/incidents_repo.go`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/internal/transport/http/handler/{agents,webhooks}.go`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/internal/transport/http/middleware/rls.go`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/internal/adapter/auth/{factory.go,workos/provider.go}`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/internal/usecase/{eval_runner,usage_ticker}.go`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane/migrations/{0003_rls,0004_rls_nullif,0017_phase6_rls,0024_projects,0011_phase5_agents}.up.sql`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/gitops/internal/usecase/open_pr.go`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/(app)/console/integrations/client.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/lib/integrations-config.ts`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/integrations/{ProviderLogo,ConfigureDialog}.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/.github/workflows/ci.yml`
