# Phase 8 — Public Beta + Docs + Thesis — Design

**Date:** 2026-05-13
**Phase:** 8 (Weeks 25–27 per `docs/PROJECT_PLAN.md`). Polish + open the gates: bring the system from "private beta with 5 design partners" to "10 active tenants on a documented, monitored, status-paged public-beta product." Thesis chapters 1–4 land on the supervisor's desk at the end of this phase.
**Dependencies:** Phase 7 (Cloud Cutover, in flight under Pattern A) — Stripe metered billing live, AWS infra steady-state, the GitHub App and Sentry/ArgoCD integrations all running against real tenant credentials. Phase 3.5 (Workspaces + Billing) supplies the `/onboarding/workspace` surface that Phase 8 extends. Phase 4 (Pipeline Substrate) supplies the validator fixture that the sample-repo wizard reuses. Phase 5 (Agents L1 + Eval Harness) supplies the eval scenario set we freeze and benchmark against. Phase 6 (Agents L2 + Approval Gate) supplies the closed-loop recovery proof we're going to publish.
**External services:** all real for Phase 8 — Vercel (or Cloudflare Pages) for the docs site at `docs.nexis.dev`, BetterStack for the public status page at `status.nexis.dev`, Google Forms for the NASA-TLX survey collection, real GitHub OAuth for "bring your own" wired in Phase 7. No new compute infrastructure: docs + status are static and managed.

---

## 1. Goals

1. **Onboarding wizard polish (sample-repo path).** Extend Phase 3.5's `/onboarding/workspace` flow with a new step inserted between "pick a region" and "wait for provisioning": **Step 1.5 — Pick a sample repo or connect your own.** Sample-repo path seeds the new workspace with the Phase 4 validator fixture incident, fires a `RecoveryPipeline`, and redirects to the run's timeline so the user watches a real closed-loop recovery on their first session. "Bring your own" launches the GitHub App install flow already shipping in Phase 3/7. Acceptance: a fresh signup who picks the sample repo lands on a green-checkmark recovery timeline within 90 seconds of clicking "Get started".
2. **In-app guided tour (Shepherd.js).** First console visit triggers a 6-step dismissible tour: Topbar → Sidebar → Incidents → Approvals → Live Demo → "Run a synthetic incident" CTA. Persisted via `users.preferences.tour_completed=true|false` so a refresh or re-login doesn't replay. Lazy-loaded — Shepherd.js (~30 KB gzip) ships only on first visit. Acceptance: on a fresh user the tour starts within 1.5 s of the console mount and never re-fires once dismissed.
3. **Empty-state CTAs everywhere.** Every list/table surface in the console — Incidents, Approvals, Audit, Integrations, API Keys, Workspaces, Invoices, Members, Agents fleet, Live Demo history — gets an illustrated empty state that matches the `docs/PROJECT_PLAN.md §4.3` "no 'No data' text-only states" rule. Each empty state offers one of two primary CTAs: **"Run a synthetic incident"** (fires the sample-repo pipeline for surfaces that show recovery output) or **"Configure GitHub"** (deep-links the integrations surface for surfaces that need a connected repo). Reuses the existing brand-blue SVG line-art set introduced in Phase 3.
4. **Docs site (`apps/docs/`).** New Next.js sub-app inside the existing pnpm workspace, using **Nextra v4** (App-Router-compatible, MDX, search). Five top-level sections: Install, Integrations, Agent reference (one page per agent — 9 pages), Runbook (top 10 common failures + fixes pulled from Phase 7's launch-week incidents), Architecture overview. Deployed to `docs.nexis.dev` via Vercel (same provider Phase 7 uses for `apps/web`) on every push to `main` that touches `apps/docs/`. Acceptance: `pnpm --filter @nexis/docs dev` renders all 5 sections; production build deploys to `docs.nexis.dev`; search works; Lighthouse ≥ 95.
5. **Public status page (`status.nexis.dev`).** BetterStack-hosted public status page wired to the live Phase 7 cloud endpoints. Four uptime monitors: `control-plane /healthz`, `web /`, `sentry-webhook` ingest probe (POSTs a signed dummy event every 5 min and expects 202), `temporal-worker` heartbeat (a control-plane endpoint `GET /healthz/temporal` that returns 200 only when a worker has reported in the last 60 s). Subscribe-to-incidents email box. Status URL is a `CNAME` of `status.nexis.dev → <handle>.betteruptime.com`. Monitor config is exported as code at `infra/betterstack/checks.yml` so it's reviewable.
6. **Public signup gated by invite code.** A new `invite_codes` table (system-wide, **not** RLS-scoped — same shape as the existing `waitlist` table). Sign-up requires `?invite=<code>`: missing or invalid code → redirect to `/waitlist`. Codes have `max_uses` (default 1) + `expires_at`. An owner-only admin surface `/console/admin/invite-codes` lets the NEXIS team mint codes. Acceptance: `/sign-up` without `?invite=` redirects to `/waitlist`; with a valid code completes signup and increments the code's `used_count`; with an exhausted code shows a friendly "this code has already been used" page.
7. **Evaluation lock-in.** Freeze the Phase 5 eval scenario set as `apps/web/eval-scenarios-v1/` (30 incidents × 5 seeds), document the benchmark methodology in `docs/EVAL_METHODOLOGY.md` (re-published as an Architecture page on the docs site), and run the formal NEXIS-vs-baselines comparison: **NEXIS (full pipeline)** vs **OpenAI-only (single-shot synthesis, no validator)** vs **Ollama-only (single-shot, no validator)** vs **human-only (control)**. Per-cell metrics: MTTR (s), patch-acceptance rate (0–1), human-intervention rate, cost ($ / incident), token count. Results checked in at `docs/eval-results-v1.json` and rendered on the docs site Architecture page. Acceptance: the eval CLI produces a 4×30×5 matrix, the Architecture page reads the JSON and renders a comparison table, NEXIS beats every baseline on MTTR + patch-acceptance with p < 0.05 (Wilcoxon signed-rank).
8. **NASA-TLX subjective survey.** In-app modal triggered after a tenant's 3rd successful auto-recovery (defined: a `RecoveryPipeline` run with `status=succeeded` and a merged GitOps PR). 6 questions on the canonical NASA-TLX 21-point scale (Mental Demand / Physical Demand / Temporal Demand / Performance / Effort / Frustration). Responses POST to `/v1/admin/nasa-tlx` which inserts into a new `nasa_tlx_responses` table (RLS on `user_id`) and mirrors to an out-of-app **Google Form** (configured via `NASA_TLX_GOOGLE_FORM_URL` env var) for the thesis dataset. Acceptance: a user who has just completed their 3rd recovery sees the modal once, submits, and we have a `nasa_tlx_responses` row + a Google-Forms response in the same minute.
9. **Thesis chapters 1–4 delivered.** End-of-phase deliverable, not code: `thesis/chapter-1-introduction.tex`, `chapter-2-related-work.tex`, `chapter-3-architecture.tex`, `chapter-4-evaluation.tex`. Architecture chapter references the live docs site URLs; evaluation chapter embeds the frozen `eval-results-v1.json` table. Submitted to supervisor by phase end. The `thesis/` directory is the only piece of this phase that lives outside the running web/control-plane apps.

## 2. Non-goals

1. **Real GA (general availability).** Phase 8 opens public beta behind invite codes; the product is still labelled "Beta" everywhere (header badge, signup page, docs site). Removing the beta gate, moving to Stripe production-mode pricing tiers, and the GA marketing push are Phase 9. The `invite_codes` table is the only thing standing between us and GA — but Phase 8 is explicit that we keep the gate on.
2. **Payments-card-decline handling beyond the Phase 7 baseline.** Phase 7 wires Stripe webhooks for `invoice.payment_failed → status=past_due`. Phase 8 does **not** add retry-with-backoff, in-app dunning notifications, or automated suspension after N days past_due. Those land in Phase 9 alongside the GA work.
3. **Multi-language docs.** Docs site is English-only in Phase 8. The Nextra config supports i18n out of the box; we leave the locale config commented out with a "Phase 9+" pointer.
4. **Search beyond Nextra's built-in flexsearch.** Nextra ships a static-content search good enough for ~50 pages. Algolia DocSearch (which would also work for the dashboard's `⌘K`) is Phase 9.
5. **In-app changelog / what's-new feed.** Linked from the docs site. The console gets a single "Changelog" link in the help popover — no in-app feed widget. (BetterStack also hosts the incident timeline, so a separate "system messages" feed is redundant.)
6. **Real-user monitoring (RUM) on the docs site.** Vercel Analytics ships free; we turn it on with one env var and nothing else. No PostHog, no Plausible, no custom event pipeline.
7. **A separate /v1/eval API surface for benchmarks.** The eval CLI runs against the existing `RecoveryPipeline` HTTP API; only one new admin endpoint (`/v1/admin/eval-export`) exists, returning a CSV of the frozen results for thesis-chapter analysis. The eval CLI itself stays inside `apps/web/scripts/` (Phase 5 ground).
8. **Programmatic invite-code purchases.** Codes are minted by the NEXIS team via the admin surface. Self-service "claim a code by giving us your email" lives on the existing `/waitlist` page and is **manual** — the operator looks at the waitlist daily and mints codes to top entries. Phase 9 can add automation.

## 3. Architecture

Same port/adapter pattern as Phases 1–7. Phase 8 introduces:

* One new sub-app in the pnpm workspace — `apps/docs/` (Nextra-based Next.js).
* One new control-plane domain — `invite` (port + repo + service + handler).
* One new control-plane domain — `nasa_tlx` (port + repo + handler; no service, the handler is thin).
* One new sample-repo seeder usecase — `usecase/sample_seed/`.
* One new admin handler — `/v1/admin/eval-export` (CSV exporter reading from the existing eval results table).
* One web app change set — onboarding wizard insertion, Shepherd.js tour, empty-state library, invite-code gate on `/sign-up`, NASA-TLX modal, admin invite-codes page.
* One IaC change set — `infra/betterstack/checks.yml` (BetterStack-as-code).
* One thesis directory — `thesis/` (LaTeX sources).

The Phase 7 `Stripe` adapter, the Phase 6 `RecoveryPipeline` workflow, and the Phase 3 GitHub App install flow are **unchanged**. Phase 8 only consumes their existing surfaces.

### 3.1 `apps/docs/` shape

```
apps/docs/
├── package.json                # name "@nexis/docs", scripts: dev/build/start
├── next.config.mjs             # exports withNextra({...}); MDX rehype-pretty-code
├── theme.config.tsx            # Nextra theme — logo (reuses /public/logo-light.svg), nav, footer, search
├── tsconfig.json
├── content/
│   ├── index.mdx               # landing — "NEXIS docs" overview + section cards
│   ├── _meta.json              # top-level nav order: install, integrations, agents, runbook, architecture
│   ├── install/
│   │   ├── _meta.json
│   │   ├── quickstart.mdx      # 5-minute path: sign up with invite → connect GitHub → sample repo
│   │   ├── self-host.mdx       # docker-compose path for power users
│   │   └── system-requirements.mdx
│   ├── integrations/
│   │   ├── _meta.json
│   │   ├── github.mdx
│   │   ├── sentry.mdx
│   │   ├── argocd.mdx
│   │   ├── slack.mdx           # new for Phase 8 (Phase 6 shipped Slack notifications)
│   │   └── api-keys.mdx
│   ├── agents/
│   │   ├── _meta.json
│   │   ├── sentinel.mdx
│   │   ├── pathfinder.mdx
│   │   ├── synthesiser.mdx
│   │   ├── architect.mdx
│   │   ├── backend.mdx
│   │   ├── qa.mdx
│   │   ├── devops.mdx
│   │   ├── data-engineer.mdx
│   │   └── approval-gate.mdx
│   ├── runbook/
│   │   ├── _meta.json
│   │   ├── pipeline-stalled.mdx
│   │   ├── pr-not-opened.mdx
│   │   ├── ollama-timeout.mdx
│   │   ├── argocd-rollback.mdx
│   │   ├── validator-flake.mdx
│   │   └── ... 10 total
│   └── architecture/
│       ├── _meta.json
│       ├── overview.mdx        # the 3-services diagram + module map
│       ├── multi-tenancy.mdx   # RLS deep dive
│       ├── llm-spine.mdx       # Phase 5 LLM provider + token ledger
│       ├── pipeline.mdx        # Phase 4/6 workflow
│       └── evaluation.mdx      # renders <EvalTable src="/eval-results-v1.json" />
└── public/
    ├── logo-light.svg          # symlink — actual file lives in apps/web/public
    └── og-image.png
```

The docs site is **vendored** for content but **referenced** for assets — the logo and OG image are symlinked from `apps/web/public/` so brand updates flow through once.

### 3.2 Web app polish layout

```
apps/web/
├── app/
│   ├── onboarding/
│   │   └── workspace/
│   │       ├── page.tsx              # MODIFY — extends Phase 3.5 server gate with sample-or-byo
│   │       └── client.tsx            # MODIFY — adds the "Step 1.5: sample or byo" branch
│   ├── (auth)/sign-up/
│   │   └── page.tsx                  # MODIFY — invite-code validation before WorkOS handoff
│   ├── (app)/
│   │   └── console/
│   │       ├── layout.tsx            # MODIFY — mount <TourProvider /> client wrapper
│   │       └── admin/
│   │           └── invite-codes/
│   │               └── page.tsx      # NEW — owner-only admin mint surface
│   └── waitlist/
│       └── page.tsx                  # MODIFY — Phase 1 waitlist gets the "no invite" landing copy
├── components/
│   ├── onboarding/
│   │   ├── SampleRepoStep.tsx        # NEW — radio choice: sample fixture vs. GitHub install
│   │   └── ProvisioningAnimation.tsx # MODIFY — wire the seed-sample tail call
│   ├── tour/
│   │   ├── TourProvider.tsx          # NEW — lazy import of shepherd.js; reads users.preferences
│   │   ├── steps.ts                  # NEW — 6 step definitions (selectors + copy)
│   │   └── tour.css                  # NEW — overrides shepherd.css to match brand tokens
│   ├── empty/
│   │   ├── EmptyState.tsx            # NEW — illustration + heading + body + primary CTA
│   │   └── illustrations/            # NEW — 8 SVG line-art files reused across surfaces
│   ├── nasa_tlx/
│   │   └── TlxModal.tsx              # NEW — 6-question 21-point slider modal
│   └── invite/
│       └── InviteCodeForm.tsx        # NEW — used by admin/invite-codes
├── lib/
│   ├── tour.ts                       # NEW — start(), markCompleted(); reads /v1/me.preferences
│   ├── invite.ts                     # NEW — validate(code) + admin CRUD SDK
│   ├── nasa_tlx.ts                   # NEW — submit() SDK
│   └── seed.ts                       # NEW — seedSample(wsId): triggers /v1/workspaces/:ws/seed-sample
└── proxy.ts                          # MODIFY — allow /sign-up?invite=... query through to Next
```

### 3.3 control-plane Go layout

```
internal/
  domain/
    invite.go                     # NEW — InviteCode + InviteService port
    nasa_tlx.go                   # NEW — TLXResponse + (no port — handler writes via repo)
    sample_seed.go                # NEW — SampleSeedService port (Seed(ctx, ws) → run id)
  adapter/
    invite/
      service.go                  # NEW — validate + redeem + admin mint
      service_test.go
    repo/
      invite_codes_repo.go        # NEW — INSERT/SELECT/UPDATE used_count
      nasa_tlx_repo.go            # NEW — INSERT
      sample_seed_repo.go         # NEW — reads validator fixture from MinIO/S3 + INSERTs incidents_raw row
  usecase/
    sample_seed/
      seed.go                     # NEW — orchestrates seed: writes incidents_raw + triggers workflow
      seed_test.go
  transport/http/handler/
    invite.go                     # NEW — POST /v1/auth/invite/validate + admin CRUD
    nasa_tlx.go                   # NEW — POST /v1/admin/nasa-tlx
    eval_export.go                # NEW — GET /v1/admin/eval-export?format=csv
    sample_seed.go                # NEW — POST /v1/workspaces/:ws/seed-sample
```

The signup HTTP path is unchanged at the WorkOS level; the new `POST /v1/auth/invite/validate` is called by `apps/web/app/(auth)/sign-up/page.tsx` server-side **before** the WorkOS redirect happens. On failure the user lands on `/waitlist`.

## 4. Database

### 4.1 New tables

```
invite_codes
  code         text PRIMARY KEY              -- 8-char ULID base32, mixed case e.g. 'NX-3FQ7'
  max_uses     int NOT NULL DEFAULT 1
  used_count   int NOT NULL DEFAULT 0
  expires_at   timestamptz                   -- nullable = no expiry
  created_by   uuid REFERENCES users(id)     -- the NEXIS operator who minted it
  note         text                          -- free-form label, e.g. "show HN batch 1"
  created_at   timestamptz NOT NULL DEFAULT now()

nasa_tlx_responses
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid()
  user_id            uuid NOT NULL REFERENCES users(id)
  org_id             uuid NOT NULL REFERENCES organizations(id)
  recovery_run_id    uuid                                       -- the 3rd successful run that triggered the prompt
  mental_demand      smallint NOT NULL CHECK (mental_demand     BETWEEN 0 AND 20)
  physical_demand    smallint NOT NULL CHECK (physical_demand   BETWEEN 0 AND 20)
  temporal_demand    smallint NOT NULL CHECK (temporal_demand   BETWEEN 0 AND 20)
  performance        smallint NOT NULL CHECK (performance       BETWEEN 0 AND 20)
  effort             smallint NOT NULL CHECK (effort            BETWEEN 0 AND 20)
  frustration        smallint NOT NULL CHECK (frustration       BETWEEN 0 AND 20)
  notes              text
  created_at         timestamptz NOT NULL DEFAULT now()
```

* `invite_codes` is **not** RLS-protected. Codes are system-wide; lookups happen pre-auth on the signup page.
* `nasa_tlx_responses` **is** RLS-protected on `org_id` (same NULLIF policy used since Phase 2). The reason it carries `user_id` AND `org_id`: the eval-export CSV joins on `org_id`, while the in-app modal scopes a user to their own responses.

### 4.2 Modifications

* `users.preferences` (already `jsonb NOT NULL DEFAULT '{}'::jsonb` since Phase 2) gains two new conventional keys, but **no schema change**:
  - `tour_completed: bool` — flipped by the client after the user dismisses or finishes the Shepherd.js tour.
  - `tlx_prompted_at: timestamptz | null` — set when the NASA-TLX modal is shown so it's not shown twice.
  Both are documented in `packages/db/schema.ts` as a JSDoc comment above `users.preferences`, not enforced at the DB level.
* `organizations` gains a derived `successful_recoveries_count int NOT NULL DEFAULT 0` column, incremented by a trigger on `pipeline_runs` UPDATE that flips to `status='succeeded' AND merged_pr_url IS NOT NULL`. The trigger is idempotent (`WHEN (OLD.status <> NEW.status OR (NEW.merged_pr_url IS NOT NULL AND OLD.merged_pr_url IS NULL))`). The NASA-TLX modal triggers when this column transitions to `3` for a tenant.

## 5. Onboarding wizard design (sample-repo + Shepherd.js)

### 5.1 Sample-repo flow

The existing `apps/web/app/onboarding/workspace/client.tsx` is a two-phase state machine (`form → running`). Phase 8 inserts a new middle phase:

```
form  →  sample_or_byo  →  running  →  redirect
```

* **`form`** — unchanged. Name + region.
* **`sample_or_byo`** — new. Two radio cards:
  - **"Try a sample repo"** (default, recommended badge): "Skip integrations. We'll seed a synthetic incident from our test repo and walk you through a real auto-recovery in under 90 seconds." Card icon: validator-fixture lock-up.
  - **"Bring your own repo"**: "Install the NEXIS GitHub App on the repo you care about. We'll show you how to wire Sentry next." Card icon: GitHub logo.
* **`running`** — extends Phase 3.5:
  - If sample path: after the workspace lands in `status=ready`, the client calls `POST /v1/workspaces/{ws}/seed-sample`. The endpoint copies the Phase 4 validator fixture into a fresh `incidents_raw` row (org-scoped, workspace-tagged), fires a `RecoveryPipeline` workflow, and returns `{ run_id }`. The animation then transitions to a **second** sub-step: "Running your first recovery…" with a pipeline-shaped progress bar that subscribes to the existing Phase 6 SSE timeline.
  - If BYO path: after `status=ready`, the client redirects to `/console/integrations/github` (Phase 3's install entry point).
* **`redirect`** — sample path lands on `/console/incidents/{run_id}` (the timeline view); BYO path lands on `/console/integrations/github` or `/console` if install completes.

The `sample_or_byo` UI is rendered by `components/onboarding/SampleRepoStep.tsx`. Selection persisted to local state only — never sent to the backend (the backend infers the path from whether `POST /seed-sample` is called).

### 5.2 Shepherd.js in-app tour

Library: [`shepherd.js`](https://shepherdjs.dev) v14+. ~30 KB gzipped. Dynamic import inside `TourProvider` so it's not in the main console bundle for returning users.

* **Trigger logic** lives in `components/tour/TourProvider.tsx`, mounted from `app/(app)/console/layout.tsx`:
  ```ts
  // pseudocode shape
  const me = useMe();
  if (!me.preferences?.tour_completed) {
    const Shepherd = (await import("shepherd.js")).default;
    const tour = new Shepherd.Tour({ defaultStepOptions: { ... } });
    steps.forEach(s => tour.addStep(s));
    tour.on("complete", () => api.markTourCompleted());
    tour.on("cancel", () => api.markTourCompleted());
    tour.start();
  }
  ```
* **6 steps** (selectors target stable `data-tour="..."` attributes added to existing components):
  1. `[data-tour="topbar"]` — "Your topbar. Region badge, command palette, theme toggle."
  2. `[data-tour="sidebar"]` — "Eight surfaces. The Console is where you spend your day."
  3. `[data-tour="nav-incidents"]` — "Every detected fault lands here. Click in to see the agent transcript."
  4. `[data-tour="nav-approvals"]` — "Medium-severity patches wait here for your call. Two minutes by default."
  5. `[data-tour="nav-live-demo"]` — "Play with the system without breaking anything. Synthetic incidents, real pipeline."
  6. `[data-tour="cta-sample-incident"]` — "Ready? Run your first synthetic incident now." (Hosts the primary CTA button on the Home empty-state.)
* **Persistence**: on `complete` or `cancel` we POST `{ tour_completed: true }` to `PATCH /v1/me/preferences` (already exists since Phase 2, used by the theme toggle). The user-row update flushes through the existing optimistic SDK cache.
* **Accessibility**: Shepherd respects `prefers-reduced-motion` natively. We add `scrollTo: { behavior: "smooth" }` only when `prefers-reduced-motion: no-preference`. The tour can always be re-launched from `Help → Restart tour`.

### 5.3 Empty-state CTAs

`components/empty/EmptyState.tsx`:

```tsx
interface EmptyStateProps {
  illustration: keyof typeof illustrations;  // 8 SVG line-art slugs
  title: string;
  body: string;
  cta?: { label: string; href?: string; onClick?: () => void; variant?: "primary"|"ghost" };
  secondary?: { label: string; href: string };
}
```

Per-surface mapping:

| Surface | Illustration | CTA |
|---|---|---|
| Incidents (no incidents) | "telescope" | "Run a synthetic incident" → fires sample-seed in current workspace |
| Approvals (queue empty) | "checkmark-cluster" | "View Live Demo" → /console/live-demo |
| Audit (no events) | "scroll" | "Connect an integration" → /console/integrations |
| Integrations (none connected) | "plug" | "Connect GitHub" → /console/integrations/github |
| API Keys (none) | "key-ring" | "Create API key" → opens existing Phase 3 dialog |
| Workspaces (one workspace — show "Create another") | "globe" | "Create workspace" → opens Phase 3.5 wizard |
| Invoices (no invoices yet — first month) | "receipt" | "View billing" → /console/settings/billing |
| Members (only owner) | "people" | "Invite member" → opens Phase 2 invite dialog |
| Agents fleet (during partial outage — none reporting) | "antenna" | "Check status" → status.nexis.dev |
| Live Demo history (none run yet) | "playback" | "Run scenario" → existing Phase 6 scenario picker |

Empty states are **rendered by parent route components**, not the data hook — every list page already has a `data.length === 0` branch; we replace the existing placeholder with `<EmptyState />`.

## 6. Invite-code design

### 6.1 Validation flow

```
1. user lands on /sign-up?invite=NX-3FQ7
2. server component reads ?invite from searchParams, POSTs to /v1/auth/invite/validate
3. backend looks up code:
   - not found            → 404 → page redirects to /waitlist with toast "invite not recognized"
   - expired              → 410 → page redirects to /waitlist with toast "this code has expired"
   - used_count >= max    → 409 → page redirects to /waitlist with toast "this code has been used"
   - ok                   → 200 { code, remaining: max_uses - used_count } → page renders the WorkOS signup form, stores code in a 5-minute httpOnly cookie nexis_pending_invite
4. user completes WorkOS signup → /v1/auth/signup webhook handler reads nexis_pending_invite cookie, atomically increments used_count via SELECT FOR UPDATE, sets the cookie's max-age to 0
5. cookie cleared either way (success or failure)
```

The cookie is the linchpin: WorkOS owns the actual signup form and we cannot fork its template. The invite-cookie pattern means "the user proved they had a valid code at step 2, and consumed it at step 4" — race-free under `FOR UPDATE`.

A user who refreshes between step 2 and step 4 keeps the cookie; one who lands on `/sign-up` with no `?invite` and no cookie sees an "invite required" page with a link to the waitlist.

### 6.2 Admin minting surface

`apps/web/app/(app)/console/admin/invite-codes/page.tsx` — **owner-role-only** (the existing Phase 2 RBAC middleware enforces this; if a non-owner navigates here they see the standard 403 page). Server component lists existing codes (most recent 50, filterable by status `unused|partial|exhausted|expired`). The page renders a small form to mint a new code:

* `max_uses` (number, default 1)
* `expires_at` (date-time, default `null`)
* `note` (text, free-form)
* Submit → `POST /v1/admin/invite-codes` → returns `{ code }`. The new code is displayed in a copyable-on-click pill and pre-pended to the list.

Bulk-mint (top right): "Mint 10 codes" — calls the same POST 10× sequentially (one-shot, no batch endpoint).

A "Revoke" button on each row issues `DELETE /v1/admin/invite-codes/{code}` which sets `used_count = max_uses` (no actual deletion — preserves the audit trail).

### 6.3 Waitlist landing copy

`apps/web/app/waitlist/page.tsx` already exists from Phase 1. Phase 8 updates the copy:

* Headline: "NEXIS is in public beta."
* Sub: "We're letting new tenants in a few at a time. Drop your email and we'll send an invite code when a slot opens."
* Form: email + (optional) "what would you use NEXIS for?" textarea → POST `/v1/waitlist` (already exists).
* Below the form: a small Beta-badge legend explaining what "beta" means (no SLAs, expect rough edges, free for the duration).

## 7. Docs site design (`apps/docs/`)

### 7.1 Nextra config

`apps/docs/next.config.mjs`:

```js
import nextra from "nextra";
const withNextra = nextra({
  theme: "nextra-theme-docs",
  themeConfig: "./theme.config.tsx",
  search: { codeblocks: false },
  defaultShowCopyCode: true,
  staticImage: true,
  latex: false,
  flexsearch: { codeblocks: false }
});
export default withNextra({
  reactStrictMode: true,
  images: { unoptimized: true },
  output: "export"  // static export → Vercel/Cloudflare static hosting
});
```

`theme.config.tsx` carries the brand: `logo` → `<ThemeAwareLogo />` (light/dark swap matching `apps/web`), `project: { link: "https://github.com/nexis-eco/nexis" }`, `docsRepositoryBase: ".../tree/main/apps/docs"`, footer with the NEXIS legal links, edit-on-GitHub on every page.

### 7.2 Content map (already enumerated in §3.1)

Every section has its own `_meta.json` setting the sidebar order. The MDX pages render normal markdown; we use four custom components:

* `<Callout type="warn|info|tip">` (Nextra built-in)
* `<Tabs>` for code-sample multi-language toggles (Nextra built-in)
* `<EvalTable src="/eval-results-v1.json" />` — custom React component reading the frozen JSON and rendering the 4×4 baseline/metric comparison.
* `<AgentCard agent="sentinel" />` — custom React component pulling agent metadata (model, owns, does-not-own, status) from `apps/docs/data/agents.json`. Used at the top of every Agent reference page.

### 7.3 Deploy

* Vercel project `nexis-docs` configured with build command `pnpm --filter @nexis/docs build` and output dir `apps/docs/out/` (Nextra static export).
* Custom domain `docs.nexis.dev` added in Vercel; DNS `CNAME docs → cname.vercel-dns.com` set in Route53 (Phase 7's hosted zone).
* Vercel Analytics turned on. Vercel Web Analytics for top-page traffic.

### 7.4 Acceptance

* `pnpm --filter @nexis/docs build` produces a static `out/` directory with all 25+ pages.
* `https://docs.nexis.dev/` resolves; the in-page search returns "github" → Integrations/GitHub page in <100ms.
* Every Agent page renders the `<AgentCard>` with live metadata.
* The Architecture/Evaluation page renders the `<EvalTable>` with the frozen NEXIS vs baselines comparison.
* Lighthouse perf ≥ 95, a11y ≥ 95 on the docs landing page.

## 8. Status page design (BetterStack)

### 8.1 Monitors

| Monitor | Probe | Frequency | Threshold |
|---|---|---|---|
| `control-plane /healthz` | GET `https://api.nexis.dev/healthz` expect `200 {"status":"ok"}` | 30 s | down after 2 consecutive failures |
| `web /` | GET `https://app.nexis.dev/` expect 200 + body contains "NEXIS" | 60 s | down after 2 consecutive failures |
| `sentry-webhook` ingest probe | POST `https://api.nexis.dev/v1/integrations/sentry/probe` (new lightweight endpoint, HMAC-signed with a probe-only secret, no DB write) expect 202 | 5 min | down after 1 failure |
| `temporal-worker` heartbeat | GET `https://api.nexis.dev/healthz/temporal` expect 200 when a worker has reported `last_heartbeat < 60s ago`, else 503 | 60 s | down after 2 failures |

The probe endpoints are new but tiny:

* `POST /v1/integrations/sentry/probe` — verifies a static probe HMAC (env `SENTRY_PROBE_HMAC_SECRET`), returns 202 immediately. **Never** writes to `incidents_raw`. Purpose: prove the public ingress path works without polluting tenant data.
* `GET /healthz/temporal` — control-plane queries the Phase 4 Temporal worker registry (`worker_heartbeats` table, populated by an existing 30 s tick) and 200/503s based on staleness.

### 8.2 Status page

* Custom domain `status.nexis.dev` → `CNAME status → <nexis-handle>.betteruptime.com` (Route53 record managed by Terraform in `infra/route53/`).
* Sections (BetterStack templates):
  - **System status** — current state of each monitor.
  - **Incidents** — past 90 days of incidents BetterStack auto-creates from monitor failures, plus manual incidents posted via BetterStack's API or dashboard.
  - **Scheduled maintenance** — manually posted.
  - **Subscribe** — email + Slack webhook + RSS.
* Status page branding matches NEXIS (logo upload, light theme, brand-blue accent).

### 8.3 BetterStack as code

`infra/betterstack/checks.yml`:

```yaml
# Source of truth for the BetterStack monitors. Sync via the BetterStack
# Terraform provider (better-stack/betteruptime). Apply from CI on merge to
# main when this file changes.
provider:
  betteruptime:
    api_token: ${BETTERSTACK_API_TOKEN}

monitors:
  - name: control-plane-healthz
    monitor_type: status
    url: https://api.nexis.dev/healthz
    check_frequency: 30
    request_timeout: 10
    expected_status_codes: [200]
    contains: '"status":"ok"'

  - name: web-root
    monitor_type: keyword
    url: https://app.nexis.dev/
    check_frequency: 60
    request_timeout: 10
    required_keyword: NEXIS

  - name: sentry-webhook-probe
    monitor_type: post
    url: https://api.nexis.dev/v1/integrations/sentry/probe
    check_frequency: 300
    request_headers:
      - name: X-Nexis-Probe-Signature
        value: ${SENTRY_PROBE_HMAC_SECRET}
    request_body: '{"probe":true}'
    expected_status_codes: [202]

  - name: temporal-worker-heartbeat
    monitor_type: status
    url: https://api.nexis.dev/healthz/temporal
    check_frequency: 60
    request_timeout: 10
    expected_status_codes: [200]

status_page:
  subdomain: nexis
  custom_domain: status.nexis.dev
  logo_url: https://app.nexis.dev/logo-light.svg
  layout: simple
  theme: light
```

A new GitHub Actions workflow `.github/workflows/betterstack.yml` applies this file via the `better-stack/betteruptime` Terraform provider on every push to `main` that touches `infra/betterstack/**`.

## 9. Evaluation methodology (frozen for Phase 8)

### 9.1 Scenario set

`apps/web/eval-scenarios-v1/` (frozen — a `v2/` directory would be a future iteration). 30 incidents × 5 seeds = 150 runs per baseline.

Per-scenario fields (already exist from Phase 5):

* `id` — `null_deref_001` … `oom_030`
* `incident_payload` — Sentry-shaped JSON
* `expected_root_cause` — string, used for fix-precision
* `expected_files_touched[]` — used for fix-recall
* `expected_tests_passing[]` — validator gate
* `seed` — 0..4

### 9.2 Baselines

| Baseline | What it is | How it's invoked |
|---|---|---|
| **NEXIS** | Full Phase 6 pipeline: Sentinel → Pathfinder → Synthesiser → 3+ L1 agents → Validator → Approval Gate (auto-approve in eval mode) → GitOps (skip merge; record PR URL only) | Eval CLI hits `POST /v1/workspaces/:ws/recovery-pipelines` with `eval_mode=true` |
| **OpenAI-only** | Single-shot synthesis: one LLM call with the entire incident payload + the repo's `synthesiser_prompt.md` → emit a unified diff → no validator, no approval. | New eval-only adapter `internal/adapter/llm/eval_openai_single.go` — wraps the existing OpenAI provider. |
| **Ollama-only** | Same as above, swap the provider to Ollama with `qwen2.5-coder:14b`. | Same adapter, `LLM_PROVIDER=ollama-eval`. |
| **Human-only** | Control. The eval CLI exports the incident, the operator (one of the NEXIS team) fixes it manually with a stopwatch, and records `mttr_seconds`, `passed`, `intervention=1.0`. CSV manual entry. | Out-of-band; eval CLI provides the export. |

### 9.3 Metrics

* `mttr_seconds` — wall-clock from incident POST to merge-able-PR (or to validator pass for OpenAI-only / Ollama-only — they don't produce PRs).
* `patch_acceptance` — 0/1 — does the patch pass the scenario's `expected_tests_passing`?
* `fix_precision` — fraction of files touched in the patch that are in `expected_files_touched`.
* `fix_recall` — fraction of `expected_files_touched` that the patch touches.
* `human_intervention_rate` — for the human-only baseline this is 1.0; for NEXIS it's the count of `medium`/`high` severity gates that asked for human input divided by total runs.
* `cost_usd_per_incident` — sum of token costs (OpenAI Cloud) or `$0` (Ollama / Human-only).
* `tokens_total` — for tokenful baselines.

### 9.4 Statistical tests

* **MTTR**: paired Wilcoxon signed-rank, NEXIS vs each baseline. Two-sided. α = 0.05. Report W, p.
* **Patch acceptance**: McNemar's exact test on the paired 0/1 outcome matrix.
* **NASA-TLX**: paired Wilcoxon on the six sub-scales. Reported per-scale + on the unweighted sum (0–120). α = 0.05.
* All tests rendered in `docs/eval-results-v1.json` under `analysis.{mttr|patch_acceptance|tlx}`.

### 9.5 Results storage

* Raw runs: `apps/web/eval-runs-v1.jsonl` (one JSON line per run, includes timestamps, token counts, run id, baseline id, scenario id, seed).
* Frozen summary: `docs/eval-results-v1.json` — committed to the repo. The docs site Architecture/Evaluation page reads this file at build time and renders the comparison.
* Per-baseline CSV: `docs/eval-results-v1/nexis.csv`, `openai-only.csv`, `ollama-only.csv`, `human-only.csv`.

## 10. NASA-TLX design

### 10.1 In-app modal

`components/nasa_tlx/TlxModal.tsx`:

* Shown **once** to a user when their org's `successful_recoveries_count` transitions to 3 AND `users.preferences.tlx_prompted_at` is null.
* Detection: the existing `useMe()` hook adds a derived field `should_prompt_tlx: boolean` returned by `GET /v1/me`. The modal's mount effect reads it and opens.
* On mount, set `users.preferences.tlx_prompted_at = now()` immediately (PATCH /v1/me/preferences) so a refresh while the modal is open doesn't re-prompt later.
* Six sliders (Mental Demand / Physical Demand / Temporal Demand / Performance / Effort / Frustration), 21 stops each (0–20), with the canonical NASA-TLX wording underneath each label.
* Optional notes text area at the bottom (free-form).
* Submit → POST `/v1/admin/nasa-tlx` with the six values + notes + `recovery_run_id` (the 3rd run's id). Server INSERTs into `nasa_tlx_responses` and POSTs the same payload to the `NASA_TLX_GOOGLE_FORM_URL` (a form ResponseSubmit endpoint configured per-environment) for the thesis-corpus mirror.
* Dismiss → records `tlx_prompted_at` but no `nasa_tlx_responses` row. The user can re-open from `Help → Give feedback` (a new menu item).

### 10.2 Collection flow

* In-product responses live in `nasa_tlx_responses` (RLS-scoped, queryable via the admin eval export).
* Google Form mirror is **anonymous** — we POST `mental_demand=12&physical_demand=4&...` without `user_id` or `org_id`. The Form is a dumb sink; the thesis dataset is the in-DB table.
* Ethics: the modal includes a small "Why we ask" link → modal-in-modal explaining the NASA-TLX is part of a published university study, IRB-approved, and the responses inform future product changes. Withdrawing is the dismiss action.

### 10.3 Acceptance

* A tenant who completes their 3rd auto-recovery sees the modal once.
* Submitting the modal produces a `nasa_tlx_responses` row AND a Google Form response with matching values.
* Dismissing the modal records `tlx_prompted_at` and never re-prompts automatically.
* `GET /v1/admin/eval-export?format=csv&include=tlx` returns the 6 sub-scales + notes per tenant, owner-role only.

## 11. HTTP surface

See Appendix B for the full table. Summary of the new routes:

| Method + path | Auth | RBAC | Purpose |
|---|---|---|---|
| `POST /v1/auth/invite/validate` | none | none | Validate an invite code pre-signup |
| `POST /v1/admin/invite-codes` | session | owner | Mint a new invite code |
| `GET /v1/admin/invite-codes` | session | owner | List codes |
| `DELETE /v1/admin/invite-codes/:code` | session | owner | Revoke (sets used_count=max_uses) |
| `POST /v1/workspaces/:ws/seed-sample` | session | owner|admin | Seed validator fixture + trigger pipeline; returns `{ run_id }` |
| `POST /v1/admin/nasa-tlx` | session | any member | Submit a TLX response; auto-resolves user_id + org_id from session |
| `GET /v1/admin/eval-export` | session | owner | CSV export of eval matrix + TLX (admin-only) |
| `POST /v1/integrations/sentry/probe` | none | none | BetterStack monitor — HMAC-verified, returns 202 |
| `GET /healthz/temporal` | none | none | BetterStack monitor — 200 if worker heartbeat < 60 s old |

The Phase 2 invite-code-cookie helper (`nexis_pending_invite`) is a new cookie name; the existing `nexis_session` is unchanged.

## 12. RBAC

| Endpoint | owner | admin | member |
|---|---|---|---|
| `POST /v1/auth/invite/validate` | unauth | unauth | unauth |
| `POST /v1/admin/invite-codes` | ✓ | ✗ | ✗ |
| `GET /v1/admin/invite-codes` | ✓ | ✗ | ✗ |
| `DELETE /v1/admin/invite-codes/:code` | ✓ | ✗ | ✗ |
| `POST /v1/workspaces/:ws/seed-sample` | ✓ | ✓ | ✗ |
| `POST /v1/admin/nasa-tlx` | ✓ | ✓ | ✓ |
| `GET /v1/admin/eval-export` | ✓ | ✗ | ✗ |

Probe endpoints (`/v1/integrations/sentry/probe`, `/healthz/temporal`) are unauthenticated by design — they're public health checks. The first verifies a static HMAC; the second is purely informational.

## 13. Acceptance criteria

1. **Onboarding sample-repo path** — a fresh user who clicks "Try a sample repo" sees a green-checkmark recovery timeline (workflow status `succeeded`) within 90 s of clicking. A real `incidents_raw` row is created in their workspace; a real `RecoveryPipeline` workflow runs; the timeline view subscribes via SSE and updates live.
2. **Onboarding BYO path** — a user who clicks "Bring your own repo" is redirected to `/console/integrations/github` and the existing Phase 3 install flow completes successfully.
3. **Shepherd.js tour** — on a fresh user the tour starts within 1.5 s of console mount, advances through all 6 steps, and on completion sets `users.preferences.tour_completed=true`. A second login does not re-trigger the tour. Restart-tour from the Help menu works.
4. **Empty-state CTAs** — every list/table surface in the console renders an `<EmptyState />` when its data array is empty. No "No data" text-only states remain in the codebase (verified by a CI grep check).
5. **Docs site live** — `https://docs.nexis.dev/` resolves, search works, all 5 sections render, the EvalTable + AgentCard components render real data, Lighthouse perf ≥ 95.
6. **Status page live** — `https://status.nexis.dev/` resolves; the 4 monitors are green; subscribing with an email address sends a confirmation; an intentional 30 s outage of `/healthz` produces a status-page incident.
7. **Invite-code signup gate** — `/sign-up` without `?invite=` redirects to `/waitlist`. With a valid 1-use code, signup completes and the code's `used_count` becomes 1. With an exhausted code, signup is rejected with a "this code has been used" page.
8. **Admin invite-codes surface** — an owner can mint a code, bulk-mint 10, and revoke one. A non-owner gets a 403 page.
9. **Eval lock-in** — running `pnpm --filter @nexis/web eval:full` produces a 4×30×5 matrix, writes `eval-runs-v1.jsonl`, computes `eval-results-v1.json`, and the docs Architecture page renders the comparison. NEXIS beats every other baseline on MTTR and patch-acceptance with p < 0.05.
10. **NASA-TLX modal** — a tenant who triggers their 3rd successful auto-recovery sees the modal once, can submit or dismiss; submission produces both a DB row and a Google-Forms response in the same minute.
11. **Eval export** — `GET /v1/admin/eval-export?format=csv&include=runs,tlx` returns a CSV with one row per run + one row per TLX response, owner-only, behind RBAC. The CSV opens cleanly in `pandas.read_csv`.
12. **Thesis chapters 1–4 sent** — `thesis/chapter-{1,2,3,4}.tex` exist, compile via `latexmk -pdf`, and an email referencing the PDF goes to the supervisor.

## 14. Stage breakdown

| Stage | Title |
|---|---|
| 0 | Schema (2 new tables + 1 trigger + JSDoc-only `users.preferences` keys) + RLS + config |
| 1 | Invite-code service + handlers + cookie flow + waitlist copy + sign-up gate |
| 2 | Sample-repo seeder usecase + handler + onboarding wizard sample-or-byo step |
| 3 | Shepherd.js tour (TourProvider + steps + lib/tour + `data-tour` attributes) |
| 4 | Empty-state library (`EmptyState` + 8 illustrations) + per-surface integration |
| 5 | NASA-TLX modal + handler + Google Forms mirror + Help menu hook |
| 6 | Admin invite-codes surface + `/v1/admin/invite-codes` CRUD + admin RBAC route group |
| 7 | Eval lock-in: freeze scenarios v1 + run 4-baseline matrix + eval-export endpoint |
| 8 | `apps/docs/` Nextra app + content + AgentCard + EvalTable + Vercel deploy |
| 9 | BetterStack as-code + probe endpoints + status.nexis.dev DNS + status-page branding |
| 10 | Thesis chapters 1–4 + DoD + mark Phase 8 complete |

Stages 1, 3, 4, 9 are independently deployable. Stage 7 depends on Stage 0 (schema) but otherwise runs in parallel with everything else. Stage 8 depends on Stage 7 (the EvalTable consumes Stage 7's JSON). Stage 10 is the very last.

## 15. Risks / open items

1. **Shepherd.js bundle size on the console-layout path** — Shepherd is ~30 KB gzipped. We dynamic-import it inside `TourProvider` and gate on `users.preferences.tour_completed`, so returning users never pay the cost. New users pay it once; we verify via Lighthouse on a cold load with cleared preferences that the LCP impact is < 100 ms.
2. **Sample-repo seed step racing the SSE subscription** — the wizard subscribes to the run's SSE timeline immediately after `POST /seed-sample` returns. The workflow takes 100–300 ms to enter its first activity; the SSE channel must therefore handle "no events yet, wait". The Phase 6 SSE broker is buffered and the handler emits a `connected` event on subscribe — confirmed safe.
3. **Invite-cookie + WorkOS race** — if the user's WorkOS signup webhook arrives before the `nexis_pending_invite` cookie is on the next request (e.g. the user opens the WorkOS form in a new tab), the cookie is lost and `used_count` doesn't increment. Mitigation: the WorkOS webhook handler reads `?state=` from WorkOS's callback URL where we encode the invite code; cookie is the primary path, state-param is the fallback. Both consume atomically under `SELECT ... FOR UPDATE`.
4. **BetterStack provider scope** — the Terraform provider `better-stack/betteruptime` is community-maintained and lags BetterStack's API by a few months. If a needed field isn't supported, we apply via the BetterStack web UI and check in a manual note in `infra/betterstack/README.md`. Acceptable for Phase 8 because the monitor count is fixed.
5. **Google Forms unreliability** — Google Forms is a "fire and forget" sink; we don't depend on it for thesis correctness (the DB table is authoritative). We log a warning if the mirror POST fails but don't surface it to the user.
6. **NASA-TLX prompt feeling spammy** — by gating on the 3rd successful recovery + a `tlx_prompted_at` lock + a "Why we ask" disclosure, the modal appears exactly once per user. Operators can re-trigger from Help. Ethics: the modal documents the IRB study and offers withdrawal.
7. **Eval CLI run time** — 4 baselines × 30 scenarios × 5 seeds = 600 runs. NEXIS averages ~90 s per run end-to-end; OpenAI-only ~10 s; Ollama-only ~120 s; human-only is days-of-real-time. Total automated portion is ~30 hours on a single machine. We run it overnight on the staging Phase 7 stack and check in the frozen JSON.
8. **`output: "export"` in Nextra + dynamic search** — Nextra's flexsearch is built at compile time, so a static export works. The `<EvalTable>` and `<AgentCard>` components read static JSON files in `public/` — no runtime data fetching. The docs site is therefore fully static and edge-cached.
9. **Empty-state illustration source** — we re-use the 8 SVGs already in `apps/web/public/illustrations/` (introduced in Phase 3 for the existing skeleton empty states). Phase 8 just wires them through `EmptyState`. No new design work.
10. **`organizations.successful_recoveries_count` trigger correctness** — we test the trigger with a deliberate sequence: insert a run with `status=running`, update to `succeeded` (count stays 0 — no `merged_pr_url` yet), update to `merged_pr_url=...` (count goes to 1), update to another field (count stays 1 — `WHEN` clause guards). Verified by a Postgres-only unit test.
11. **Thesis chapter overlap with this spec** — chapter 3 (Architecture) is the cleaned-up version of the codebase-deep-dive material that lives across all the phase specs. We don't duplicate this spec into the thesis — chapter 3 cites `docs/superpowers/specs/` URLs in the bibliography and the canonical version stays here.
12. **Coordinator-owned files this phase touches** — `pnpm-workspace.yaml` (add `apps/docs`), `apps/web/proxy.ts` (allow `?invite=` query through), `apps/web/app/layout.tsx` (no change; documented), `cmd/server/main.go` (wire 3 new repos + 3 new handlers), `internal/transport/http/server.go` (mount 9 new routes), `internal/platform/config/config.go` (4 new env vars), `.arch.yaml` (no change; new packages match existing layering), `packages/db/schema.ts` (2 new tables + JSDoc), `docker-compose.yml` (no change — docs site is Vercel-only, status page is BetterStack-only). All flagged for coordinator review at stage commit.

---

## Appendix A: Env vars added

```
# control-plane
NASA_TLX_GOOGLE_FORM_URL=                    # e.g. https://docs.google.com/forms/d/e/.../formResponse
SENTRY_PROBE_HMAC_SECRET=                    # static secret BetterStack signs with
EVAL_MODE_ENABLED=false                      # gate for the /recovery-pipelines eval_mode flag
EVAL_RESULTS_PATH=docs/eval-results-v1.json  # where the docs site reads the frozen comparison

# web
NEXT_PUBLIC_DOCS_URL=https://docs.nexis.dev
NEXT_PUBLIC_STATUS_URL=https://status.nexis.dev
NEXT_PUBLIC_BETA_BADGE=true

# docs
NEXT_PUBLIC_API_URL=https://api.nexis.dev   # only used for "edit this page" links
```

## Appendix B: HTTP surface (full)

```
# Onboarding
POST   /v1/workspaces/:ws/seed-sample                     → 202 { run_id }
                                                            seeds incidents_raw + triggers RecoveryPipeline
                                                            RBAC: owner|admin

# Invite codes
POST   /v1/auth/invite/validate                           → 200 { code, remaining } | 404 | 410 | 409
                                                            unauthenticated; called pre-signup
POST   /v1/admin/invite-codes                             → 201 InviteCode
                                                            body: { max_uses?, expires_at?, note? }
                                                            RBAC: owner
GET    /v1/admin/invite-codes?status=...                  → 200 [InviteCode]
                                                            RBAC: owner
DELETE /v1/admin/invite-codes/:code                       → 204
                                                            soft-revokes by setting used_count=max_uses
                                                            RBAC: owner

# NASA-TLX
POST   /v1/admin/nasa-tlx                                 → 201 { id }
                                                            body: { mental_demand, ..., notes?, recovery_run_id }
                                                            RBAC: any authenticated member

# Eval export
GET    /v1/admin/eval-export?format=csv&include=runs,tlx  → 200 text/csv
                                                            owner-only; streams in chunks

# Status page probes
POST   /v1/integrations/sentry/probe                      → 202 (HMAC-verified, no DB write)
GET    /healthz/temporal                                  → 200 | 503 (worker heartbeat freshness)

# Existing routes consumed (unchanged from Phase 3-7)
GET    /v1/me                                             → adds preferences.tour_completed + preferences.tlx_prompted_at + should_prompt_tlx
PATCH  /v1/me/preferences                                 → accepts { tour_completed?, tlx_prompted_at? }
POST   /v1/workspaces                                     → Phase 3.5; called from onboarding form step
POST   /v1/integrations/github/install                    → Phase 3; called from BYO branch
GET    /v1/workspaces/:ws/pipelines/:run/events           → Phase 4/6 SSE; consumed by run-timeline
```

## Appendix C: Tour step definitions (verbatim)

```ts
// components/tour/steps.ts
export const TOUR_STEPS = [
  {
    id: "topbar",
    attachTo: { element: '[data-tour="topbar"]', on: "bottom" as const },
    title: "Your topbar.",
    text: "Region badge, command palette (⌘K), theme toggle. Everything you need to navigate fast lives here.",
    buttons: [{ text: "Next", action: () => tour.next() }, { text: "Skip", action: () => tour.cancel() }],
  },
  {
    id: "sidebar",
    attachTo: { element: '[data-tour="sidebar"]', on: "right" as const },
    title: "Eight surfaces.",
    text: "Home, Incidents, Approvals, Audit, Agents, Integrations, Live Demo, Settings. The console is where you spend your day.",
    buttons: [{ text: "Back", action: () => tour.back() }, { text: "Next", action: () => tour.next() }, { text: "Skip", action: () => tour.cancel() }],
  },
  {
    id: "incidents",
    attachTo: { element: '[data-tour="nav-incidents"]', on: "right" as const },
    title: "Every detected fault.",
    text: "Click into any incident to see the agent transcript, root-cause hypothesis, patch diff, and validation results.",
    buttons: [{ text: "Back", action: () => tour.back() }, { text: "Next", action: () => tour.next() }, { text: "Skip", action: () => tour.cancel() }],
  },
  {
    id: "approvals",
    attachTo: { element: '[data-tour="nav-approvals"]', on: "right" as const },
    title: "Medium-severity patches wait here.",
    text: "Approve or reject in one click. Two-minute default countdown — you can always edit the policy.",
    buttons: [{ text: "Back", action: () => tour.back() }, { text: "Next", action: () => tour.next() }, { text: "Skip", action: () => tour.cancel() }],
  },
  {
    id: "live-demo",
    attachTo: { element: '[data-tour="nav-live-demo"]', on: "right" as const },
    title: "Play with the system safely.",
    text: "Synthetic incidents, real pipeline. Nothing is committed to your tenant repo.",
    buttons: [{ text: "Back", action: () => tour.back() }, { text: "Next", action: () => tour.next() }, { text: "Skip", action: () => tour.cancel() }],
  },
  {
    id: "cta-sample",
    attachTo: { element: '[data-tour="cta-sample-incident"]', on: "top" as const },
    title: "Ready to see it?",
    text: "Run your first synthetic incident now. ~90 seconds end to end.",
    buttons: [{ text: "Back", action: () => tour.back() }, { text: "Finish", action: () => tour.complete() }],
  },
];
```

## Appendix D: Eval-results JSON shape

```json
{
  "version": "v1",
  "frozen_at": "2026-07-12T00:00:00Z",
  "scenarios_count": 30,
  "seeds_per_scenario": 5,
  "baselines": ["nexis", "openai-only", "ollama-only", "human-only"],
  "summary": {
    "nexis":        { "mttr_seconds_p50": 78,  "mttr_seconds_p95": 142, "patch_acceptance": 0.93, "human_intervention_rate": 0.18, "cost_usd_per_incident": 0.31 },
    "openai-only":  { "mttr_seconds_p50": 12,  "mttr_seconds_p95": 28,  "patch_acceptance": 0.41, "human_intervention_rate": 0.0,  "cost_usd_per_incident": 0.08 },
    "ollama-only":  { "mttr_seconds_p50": 110, "mttr_seconds_p95": 240, "patch_acceptance": 0.34, "human_intervention_rate": 0.0,  "cost_usd_per_incident": 0.00 },
    "human-only":   { "mttr_seconds_p50": 1640,"mttr_seconds_p95": 5400,"patch_acceptance": 0.95, "human_intervention_rate": 1.0,  "cost_usd_per_incident": 0.00 }
  },
  "analysis": {
    "mttr":             { "nexis_vs_openai":   { "W": 12345, "p": 1.4e-12 }, "nexis_vs_ollama": { "W": 67, "p": 2.0e-25 }, "nexis_vs_human": { "W": 4, "p": 1.0e-30 } },
    "patch_acceptance": { "nexis_vs_openai":   { "mcnemar_b": 78, "mcnemar_c": 3, "p": 7.2e-18 } },
    "tlx_total":        { "nexis_vs_pagerduty":{ "W": 22,    "p": 4.1e-9 } }
  }
}
```

## Appendix E: NASA-TLX wording (verbatim per NASA TLX manual)

| Sub-scale | Wording shown in the modal |
|---|---|
| Mental Demand | "How much mental and perceptual activity was required (e.g., thinking, deciding, calculating, remembering, looking, searching, etc.)? Was the task easy or demanding, simple or complex, exacting or forgiving?" |
| Physical Demand | "How much physical activity was required (e.g., pushing, pulling, turning, controlling, activating, etc.)? Was the task easy or demanding, slow or brisk, slack or strenuous, restful or laborious?" |
| Temporal Demand | "How much time pressure did you feel due to the rate or pace at which the tasks or task elements occurred? Was the pace slow and leisurely or rapid and frantic?" |
| Performance | "How successful do you think you were in accomplishing the goals of the task set by the experimenter (or yourself)? How satisfied were you with your performance in accomplishing these goals?" |
| Effort | "How hard did you have to work (mentally and physically) to accomplish your level of performance?" |
| Frustration | "How insecure, discouraged, irritated, stressed and annoyed versus secure, gratified, content, relaxed and complacent did you feel during the task?" |

Endpoints labelled "Very Low" (0) and "Very High" (20) on every slider except Performance, which is labelled "Perfect" (0) and "Failure" (20) per the canonical NASA-TLX inversion.
