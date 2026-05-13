# Phase 7 — Cloud Cutover + Hardening + Billing — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking. Phase 7 has THREE parallel waves driven by the parallel-dispatch playbook in §1: Wave-1 is a **Pattern C** fan-out across four provider-swap shards (KMSVault / SecretsManager / S3 PatchStore / Mailer + Modal + AuraDB + Temporal Cloud + OTLP), Wave-2 is a **Pattern B** integration wave (Terraform modules + WorkOS provider + Stripe provider running concurrently), Wave-3 is a **Pattern D** rewrite of the Stripe billing subtree behind a sequential coordinator merge. Stages 0–2 are sequential coordinator work; Stages 9–12 are sequential cutover work.

**Date:** 2026-05-13
**Phase:** 7 (Weeks 19–21 per `docs/PROJECT_PLAN.md`).
**Goal:** End local Docker Compose for staging/prod. Stand up AWS via Terraform (VPC + ECS Fargate + RDS + ElastiCache + S3 + KMS + Secrets Manager + ALB + ACM + Route53 + WAF + CloudTrail + AWS Backup). Promote every stubbed external port from Phases 1–6 to its real cloud counterpart behind a single env var. Ship real WorkOS AuthKit + Stripe metered billing + Temporal Cloud + Modal validator runner + Grafana Cloud OTLP + Resend mailer + AuraDB Neo4j. Add SOC 2-lite controls (audit Merkle anchor + nightly DR + IR runbook + access reviews). Run an external pen-test and ship the fix list. Run k6 100-VU load test with an RLS probe sidecar. Cut DNS over from compose to AWS, keep the compose host warm for 7 days as rollback.

**Architecture:** Port/adapter pattern unchanged from Phases 1–6. Phase 7 ships **zero new domain code**. Every change is one of: (a) a port adapter implementation that lives behind the existing factory, (b) a new env var consumed in `internal/platform/config/config.go`, (c) a new webhook or OAuth handler in `internal/transport/http/handler/`, (d) a new Terraform module or environment root under `infra/terraform/`, (e) a new GitHub Actions workflow under `.github/workflows/`. The same `domain.KeyVault`, `domain.SecretsStore`, `domain.PatchStore`, `domain.Mailer`, `domain.AuthProvider`, `domain.BillingProvider`, `domain.Graph`, and `validator.Runner` ports that Phases 1–6 already declared get real bodies. The Phase 1–6 acceptance suite is the **cutover contract**: every test passes verbatim against AWS staging.

**Tech Stack:** Go 1.25, AWS SDK v2 (`github.com/aws/aws-sdk-go-v2/{config,service/kms,service/s3,service/secretsmanager,service/ses}`), Stripe SDK (`github.com/stripe/stripe-go/v82`), WorkOS SDK (`github.com/workos/workos-go/v4`), Resend HTTP API (raw `net/http`), Neo4j Go driver v5 (unchanged from Phase 6), Temporal SDK v1.27 with TLS dial, OpenTelemetry OTLP/HTTP exporter against Grafana Cloud, Terraform 1.10 + `hashicorp/aws` v5, GitHub Actions, ArgoCD 2.13 (ECS provider plugin) + Argo Rollouts 1.7, k6 v0.55, Modal.com Python SDK, Next.js 16.2.2 + React 19 (unchanged from Phase 6).

**Spec:** `docs/superpowers/specs/2026-05-13-phase-7-cloud-cutover.md` (1107 lines — source of truth).

---

## Salvage / Reuse from Phases 1–6

Phase 7 is intentionally a swap-only phase. The following lists what is **kept untouched** and what is **extended in place** — anything missing from these lists is new.

- `internal/domain/{keyvault.go,secrets.go,patchstore.go,mailer.go,auth.go,billing.go,graph.go}` — **untouched.** Every port signature stays. Phase 7 swaps adapters only.
- `internal/adapter/keyvault/local.go` — **kept** as the dev/test default. Phase 7 adds `kms.go` as a peer + a factory.
- `internal/adapter/patchstore/minio/` — **kept** as the dev/test default. Phase 4 already shipped an `s3/` peer with a stub body; Phase 7 fills it in with real AWS SDK v2 calls.
- `internal/adapter/billing/local/` — **kept**. Phase 3.5 shipped the local adapter; the stripe peer adapter exists with a stub body and Phase 7 fills it in.
- `internal/adapter/auth/local/` (password provider) — **kept** for dev/test. Phase 2 shipped `workos/provider.go` as a stub; Phase 7 rewrites it with the real WorkOS SDK body.
- `internal/adapter/mailer/smtp.go` — **kept** for dev (MailHog). Phase 7 adds `resend.go` + `awsses.go` peers.
- `internal/adapter/graphstore/neo4j/store.go` — **kept verbatim**. The Phase 6 Bolt driver speaks to AuraDB unchanged when the URI uses the `neo4j+s://` scheme; Phase 7 only adds `aura.go` as a connection helper.
- `services/validator/internal/runner/docker.go` — **kept** behind the Phase 4 Runner interface. Phase 7 adds `modal.go` as a peer + a factory selector.
- `internal/platform/temporal/client.go` — **extend in place.** Add a conditional `cfg.TemporalCloud` branch; the dev branch stays.
- `internal/platform/otel/otel.go` — **extend in place.** Add a conditional `cfg.OTLPTarget=="grafana-cloud"` branch; the dev branch stays.
- `internal/platform/config/config.go` — extend with Phase 7 fields (AWS region, KMS ARN, Stripe keys, WorkOS keys, Resend key, Modal endpoint, Aura URI, Grafana OTLP target). Existing fields stay.
- `internal/adapter/audit/*` — reused as-is via `domain.AuditWriter`. New audit actions (`auth.tenant_provisioned`, `billing.entitlement_changed`, `billing.usage_pushed`, `audit.anchor_written`, `kms.refused`) are just new metadata blobs.
- `internal/transport/http/server.go` — extend the protected and public route groups with the new routes. The Phase 1–6 routes survive unchanged.
- `internal/transport/http/middleware/` — reused. New middlewares (`stripe_signature.go`, `workos_signature.go`) are siblings.
- `internal/adapter/repo/billing_repo.go` (Phase 3.5) — **extend** with the Stripe-aware columns. The dual-pool pattern (`pool` + `adminPool`) carries forward into the new usage-pusher cron.
- `internal/usecase/billing_*.go` (Phase 3.5) — extend or add. The 60-second usage tick from Phase 3.5 is unchanged; Phase 7 adds an **hourly Stripe push** cron and a **daily token aggregator** cron.
- `docker-compose.yml` — **kept verbatim.** Dev still uses compose. Phase 7 does NOT touch compose; the swap is env-var driven.
- `apps/web/lib/billing.ts` — extend with `checkout()` and `portal()` helpers. The Phase 3.5 entitlements helper stays.
- `apps/web/lib/auth.ts` — extend with `redirectToWorkOS()`. The Phase 2 password-form helpers stay for dev (`NEXT_PUBLIC_AUTH_PROVIDER=local`).
- `cmd/server/main.go` — **coordinator-only edits** (factory swap + new handler mounts + startup assertions).
- `.arch.yaml` — extend the allow list with `internal/platform/aws` and `internal/adapter/secrets`.

**Coordinator-owned files** (the controller serializes edits; sub-agents do not touch these concurrently):

- `services/control-plane/cmd/server/main.go`
- `services/control-plane/internal/transport/http/server.go`
- `services/control-plane/internal/platform/config/config.go`
- `services/control-plane/.arch.yaml`
- `services/control-plane/migrations/0019_*.sql` through `0021_*.sql` (Phase 7 migrations)
- `packages/db/schema.ts`
- `docker-compose.yml` (Phase 7 should not touch; coordinator only if a dev-side bug surfaces)
- `apps/web/proxy.ts`
- `apps/web/app/layout.tsx`
- `infra/terraform/envs/dev/main.tf`, `infra/terraform/envs/staging/main.tf`, `infra/terraform/envs/prod/main.tf` (env-root composition files)
- `.github/workflows/release.yml`
- `.github/workflows/e2e-cloud.yml`

---

## 1. Parallel-dispatch playbook

Phase 7 has **three parallel waves** plus sequential cutover. Each wave runs N agents from a single dispatch message; the coordinator merges the wave by running the verification block in §1.4 after every shard finishes.

### 1.1 Wave map

| Wave | Pattern | Agents in parallel | Stages | When |
|---|---|---|---|---|
| **Wave 1 — Terraform IaC** | Pattern B (1 `lead-software-engineer` + 1 `backend-engineer`) | Terraform modules + envs/dev shard / hello-world ECS service shard | 1–2 | After Stage 0 lands (schema + config + env). |
| **Wave 2 — Provider swaps** | **Pattern C — four backend-engineers in parallel** | KMS+Secrets shard / S3+Mailer+AWS-helpers shard / Modal+Temporal+OTLP+Aura shard / WorkOS+OAuth+JIT+web-callback shard | 3–5 | After Wave 1's `envs/dev` is up and a hello-world ECS task pulls a real image. |
| **Wave 3 — Stripe + CI/CD + SOC2-lite** | **Pattern D — Stripe rewrite + Pattern B** for the rest | Stripe rewrite (1 backend-engineer, sequential) → release pipeline shard (lead-software-engineer) + audit anchor shard (backend-engineer) + UI shard (frontend-engineer) | 6–8 | After Wave 2 merges. Stripe must land sequentially because `billing/stripe/provider.go` is a full rewrite and the webhook handler touches the HTTP router; the other Wave 3 shards run in parallel once Stripe's coordinator-merge passes. |

Stages 0, 9, 10, 11, 12 are **sequential coordinator** work — they all touch env-root Terraform, DNS, the cutover runbook, or the load-test fixture set.

### 1.2 Wave-2 file ownership (must be disjoint — no shard touches another's directory)

| Shard | Owned paths |
|---|---|
| 3.A — KMS + Secrets (Stage 3) | `internal/adapter/keyvault/{kms,kms_test,factory}.go`, `internal/adapter/secrets/**`, `internal/platform/aws/{config,kms,secrets}.go`, supporting tests, `cmd/seed-kms/main.go` |
| 3.B — S3 + Mailer + AWS helpers (Stage 3) | `internal/adapter/patchstore/s3/{store,store_test}.go`, `internal/adapter/mailer/{resend,awsses,factory}.go`, `internal/platform/aws/{s3,ses}.go`, supporting tests |
| 4.A — Modal + Temporal + OTLP + Aura (Stage 4) | `services/validator/internal/runner/{runner,modal,factory}.go`, `services/validator/internal/sandbox/docker.go` (alias-file edit), `services/validator/modal/**`, `internal/platform/temporal/client.go` (extend), `internal/platform/otel/otel.go` (extend), `internal/adapter/graphstore/{neo4j/aura,factory}.go` |
| 5.A — WorkOS + OAuth + JIT + Web callback (Stage 5) | `internal/adapter/auth/workos/{provider,callback,scim,role_map,provider_test}.go`, `internal/adapter/auth/factory.go` (extend), `internal/transport/http/handler/oauth.go`, `internal/transport/http/middleware/workos_signature.go`, `internal/usecase/auth_jit_provision.go`, `apps/web/app/auth/workos/callback/route.ts`, `apps/web/app/auth/{signin,signup}/page.tsx` (extend), `apps/web/lib/auth.ts` (extend) |

### 1.3 Wave-3 file ownership

| Shard | Owned paths |
|---|---|
| 6.A — Stripe (Stage 6, sequential) | `internal/adapter/billing/stripe/{provider,webhook,usage_pusher,provider_test}.go`, `internal/transport/http/handler/{webhooks,billing}.go` (modify), `internal/transport/http/middleware/stripe_signature.go`, `internal/usecase/billing_{entitlements,usage_pusher}.go`, `apps/web/app/(app)/console/settings/billing/{page,client}.tsx`, `apps/web/lib/billing.ts` (extend) |
| 7.A — Release pipeline + ArgoCD (Stage 7) | `.github/workflows/release.yml`, `infra/k8s/argocd/**`, `infra/k8s/rollouts/**`, `infra-deploy/` (seeded via script under `scripts/seed-infra-deploy.sh`) |
| 7.B — Audit anchor + DR + IR + access reviews (Stage 8) | `internal/usecase/audit_anchor.go`, `cmd/nex/audit_verify.go`, `internal/platform/cron/anchor.go` (extend cron registration), `docs/runbooks/{dr-drill,incident-response,access-review,cert-rotation,cutover-2026-W21}.md`, `infra/terraform/modules/iam-task-role/main.tf` (tighten) |
| 7.C — Plan switcher UI (Stage 6 sub-shard) | `apps/web/app/(app)/console/settings/billing/{page,client}.tsx`, `apps/web/app/(app)/console/settings/security/page.tsx`, `apps/web/lib/billing.ts` (extend), `apps/web/components/billing/{PlanSwitcher,PortalLink}.tsx` |

### 1.4 Coordinator merge rule

After each shard in any wave finishes, the coordinator runs on `services/control-plane/`:

```bash
make build && make test && make vet && make arch
```

Plus on the web app:

```bash
cd apps/web && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
```

Plus on Terraform shards:

```bash
cd infra/terraform/envs/dev && terraform init -backend=false && terraform validate && tflint && tfsec .
```

Any failure rolls back to the offending shard's last green commit and re-dispatches just that shard with the error transcript.

### 1.5 Sequential safety net (anti-`local`-bypass)

Stage 0 ships a startup assertion in `cmd/server/main.go` that **fatals** if `ENV in {staging,prod}` and any of `AUTH_PROVIDER`, `BILLING_PROVIDER`, `PATCH_STORE`, `KEYVAULT`, `SECRETS`, `MAILER`, `GRAPH_PROVIDER`, `VALIDATOR_RUNNER`, `TEMPORAL_CLOUD`, `OTLP_TARGET` resolves to a local/dev value. This is Risk 16.14 from the spec and the first line of defence for the cutover contract.

---

## Stage 0 — Schema + RLS + config + env + startup assertions

### Task 0.1: Drizzle schema additions

**Files:**
- Modify: `packages/db/schema.ts` (COORDINATOR)

- [ ] **Step 1: Append the new tables after the Phase 6 `slackNotifications` block (the entitlements/usage_records column additions are handled in the SQL migration; Drizzle does not generate ALTERs cleanly).**

```ts
// Phase 7 — Cloud cutover: Stripe idempotency + audit anchors (new tables only).

export const stripeEventsProcessed = pgTable("stripe_events_processed", {
  id:          text("id").primaryKey(),                              // Stripe event id; idempotency key
  type:        text("type").notNull(),
  processedAt: timestamp("processed_at", { withTimezone: true }).notNull().defaultNow(),
});

export const auditAnchors = pgTable("audit_anchors", {
  id:          uuid("id").primaryKey().defaultRandom(),
  periodStart: timestamp("period_start", { withTimezone: true }).notNull(),
  periodEnd:   timestamp("period_end",   { withTimezone: true }).notNull(),
  rowCount:    bigint("row_count", { mode: "number" }).notNull(),
  merkleRoot:  text("merkle_root").notNull(),                       // hex-encoded sha256
  prevRoot:    text("prev_root"),                                    // nullable for first anchor
  s3ObjectKey: text("s3_object_key").notNull(),
  s3VersionId: text("s3_version_id").notNull(),
  anchoredAt:  timestamp("anchored_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({ uniqPeriodEnd: uniqueIndex("audit_anchors_period_end_uniq").on(t.periodEnd) }));
```

Reviewers: the Phase 3.5 `entitlements` and `usage_records` exports get their new fields appended in-place inside their existing `pgTable("entitlements", {...})` block. Drizzle-kit will emit ALTERs but reorder columns — Task 0.2 SQL is the canonical source.

- [ ] **Step 2: Generate**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm exec drizzle-kit generate --name=phase7_cloud
```

Expected output: a single new migration file at `packages/db/migrations/0007_*_phase7_cloud.sql` containing the `audit_anchors` + `stripe_events_processed` `CREATE TABLE` statements. The ALTERs are written by hand in Task 0.2.

### Task 0.2: Control-plane mirror migrations

**Files:**
- Create: `services/control-plane/migrations/0019_phase7_billing_columns.up.sql`
- Create: `services/control-plane/migrations/0019_phase7_billing_columns.down.sql`
- Create: `services/control-plane/migrations/0020_phase7_audit_anchors.up.sql`
- Create: `services/control-plane/migrations/0020_phase7_audit_anchors.down.sql`
- Create: `services/control-plane/migrations/0021_phase7_rls.up.sql`
- Create: `services/control-plane/migrations/0021_phase7_rls.down.sql`

- [ ] **Step 1: `0019_phase7_billing_columns.up.sql`**

```sql
ALTER TABLE entitlements
  ADD COLUMN IF NOT EXISTS stripe_subscription_id  text,
  ADD COLUMN IF NOT EXISTS stripe_price_id         text,
  ADD COLUMN IF NOT EXISTS stripe_customer_id      text,
  ADD COLUMN IF NOT EXISTS current_period_end      timestamptz,
  ADD COLUMN IF NOT EXISTS trial_end               timestamptz,
  ADD COLUMN IF NOT EXISTS cancel_at_period_end    boolean NOT NULL DEFAULT false;

ALTER TABLE entitlements DROP CONSTRAINT IF EXISTS entitlements_status_check;
ALTER TABLE entitlements ADD  CONSTRAINT entitlements_status_check
  CHECK (status IN ('trialing','active','past_due','canceled','incomplete'));
CREATE INDEX IF NOT EXISTS entitlements_stripe_sub_idx ON entitlements (stripe_subscription_id);

ALTER TABLE usage_records
  ADD COLUMN IF NOT EXISTS stripe_usage_record_id text,
  ADD COLUMN IF NOT EXISTS pushed_at              timestamptz;
CREATE INDEX IF NOT EXISTS usage_records_unpushed_idx ON usage_records (org_id) WHERE pushed_at IS NULL;

CREATE TABLE IF NOT EXISTS stripe_events_processed (
  id           text PRIMARY KEY,
  type         text NOT NULL,
  processed_at timestamptz NOT NULL DEFAULT now()
);
```

- [ ] **Step 2: `0019_phase7_billing_columns.down.sql`** — DROP TABLE stripe_events_processed; DROP INDEX entitlements_stripe_sub_idx + usage_records_unpushed_idx; ALTER TABLE entitlements + usage_records DROP COLUMN each new column.

- [ ] **Step 3: `0020_phase7_audit_anchors.up.sql`**

```sql
CREATE TABLE audit_anchors (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  period_start  timestamptz NOT NULL,
  period_end    timestamptz NOT NULL,
  row_count     bigint      NOT NULL,
  merkle_root   text        NOT NULL,
  prev_root     text,
  s3_object_key text        NOT NULL,
  s3_version_id text        NOT NULL,
  anchored_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (period_end)
);
CREATE INDEX audit_anchors_period_idx ON audit_anchors (period_end DESC);
```

`0020.down.sql`: `DROP TABLE IF EXISTS audit_anchors;`.

- [ ] **Step 4: `0021_phase7_rls.up.sql`** — `entitlements` and `usage_records` are already RLS-protected from Phase 3.5; new columns inherit. `audit_anchors` and `stripe_events_processed` are system tables (admin pool only — no RLS).

```sql
GRANT INSERT, SELECT ON audit_anchors           TO nexis_admin;
GRANT INSERT, SELECT ON stripe_events_processed TO nexis_admin;
```

`0021.down.sql`: empty (revoke is implicit on table drop in `0020.down`).

- [ ] **Step 5: Run migrations and confirm** — `docker compose up -d postgres && make migrate`; then `psql ... -c '\d entitlements' | grep stripe_subscription_id` and `psql ... -c '\d audit_anchors'` to confirm the new columns + table exist.

### Task 0.3: sqlc queries

**Files:**
- Create: `services/control-plane/internal/adapter/repo/queries/audit_anchors.sql`
- Modify: `services/control-plane/internal/adapter/repo/queries/billing.sql`

- [ ] **Step 1: `queries/audit_anchors.sql`** — `:one` `InsertAuditAnchor` (RETURNING *), `:one` `LastAuditAnchor` (ORDER BY period_end DESC LIMIT 1), `:many` `ListAuditAnchorsSince` (WHERE period_end >= $1 ORDER BY period_end ASC).

- [ ] **Step 2: append to `queries/billing.sql`**

```sql
-- name: ListUnpushedUsageRecords :many
SELECT id, org_id, workspace_id, kind, quantity, unit_price_cents, recorded_at
FROM usage_records WHERE pushed_at IS NULL ORDER BY recorded_at LIMIT $1;

-- name: MarkUsageRecordPushed :exec
UPDATE usage_records SET pushed_at = now(), stripe_usage_record_id = $2 WHERE id = $1;

-- name: UpdateEntitlementFromStripe :exec
UPDATE entitlements SET stripe_subscription_id=$2, stripe_price_id=$3, stripe_customer_id=$4,
  current_period_end=$5, trial_end=$6, cancel_at_period_end=$7, status=$8, updated_at=now()
WHERE org_id = $1;

-- name: GetEntitlementByOrg :one
SELECT org_id, plan, status, stripe_subscription_id, stripe_price_id, stripe_customer_id,
       current_period_end, trial_end, cancel_at_period_end, updated_at
FROM entitlements WHERE org_id = $1;

-- name: RecordStripeEvent :execrows
INSERT INTO stripe_events_processed (id, type) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING;
```

- [ ] **Step 3: Generate**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make sqlc
```

Expected: regenerated `internal/adapter/repo/queries/` Go bindings reflect the new queries.

### Task 0.4: Config additions

**Files:**
- Modify: `services/control-plane/internal/platform/config/config.go` (COORDINATOR)

- [ ] **Step 1: Append Phase 7 fields to the `Config` struct (env tags shown; defaults call out the dev-safe value)**

```go
type Config struct {
    // ... Phase 1–6 fields stay verbatim above ...

    // Selectors (default to dev-safe value; FatalIfLocalInCloud blocks staging/prod).
    Env             string `env:"ENV"              envDefault:"dev"`
    AWSRegion       string `env:"AWS_REGION"       envDefault:"us-east-1"`
    AuthProvider    string `env:"AUTH_PROVIDER"    envDefault:"local"`
    BillingProvider string `env:"BILLING_PROVIDER" envDefault:"local"`
    PatchStore      string `env:"PATCH_STORE"      envDefault:"minio"`
    KeyVault        string `env:"KEYVAULT"         envDefault:"local"`
    SecretsBackend  string `env:"SECRETS"          envDefault:"env"`
    Mailer          string `env:"MAILER"           envDefault:"smtp"`
    GraphProvider   string `env:"GRAPH_PROVIDER"   envDefault:"compose"`
    ValidatorRunner string `env:"VALIDATOR_RUNNER" envDefault:"docker"`
    TemporalCloud   bool   `env:"TEMPORAL_CLOUD"   envDefault:"0"`
    OTLPTarget      string `env:"OTLP_TARGET"      envDefault:"local"`

    // AWS adapter config (read from Secrets Manager at runtime; not in plain env in prod).
    KMSKeyARN, S3PatchBucket, S3AuditExportBucket, SecretsPrefix string

    // WorkOS.
    WorkOSAPIKey, WorkOSClientID, WorkOSWebhookSecret, WorkOSRedirectURI string

    // Stripe.
    StripeSecretKey, StripePublishableKey, StripeWebhookSecret string
    StripePriceRuntime, StripePriceEvents, StripePriceTokens   string

    // Mailer.
    ResendAPIKey   string
    SESFromAddress string `env:"SES_FROM_ADDRESS" envDefault:"noreply@nexis.dev"`

    // Temporal Cloud.
    TemporalTLSCertPath, TemporalTLSKeyPath string

    // Grafana Cloud.
    GrafanaOTLPEndpoint, GrafanaInstanceID, GrafanaAPIToken string

    // Neo4j AuraDB.
    Neo4jURI      string `env:"NEO4J_URI"  envDefault:"bolt://neo4j:7687"`
    Neo4jUser     string `env:"NEO4J_USER" envDefault:"neo4j"`
    Neo4jPassword string `env:"NEO4J_PASS" envDefault:"nexis-dev"`

    // Modal.
    ModalEndpoint, ModalToken string

    // Audit anchor cron.
    AuditAnchorBucket string
    AuditAnchorCron   string `env:"AUDIT_ANCHOR_CRON" envDefault:"0 2 * * *"`
}
```

Every field without an `envDefault` is REQUIRED in staging/prod and surfaces a validation error at startup if empty. Spec Appendix A lists the canonical env-var names; field names follow the same camel-case → SCREAMING_SNAKE pattern as Phases 1–6.

- [ ] **Step 2: Append the startup assertion helper (Risk 16.14 mitigation)**

```go
// FatalIfLocalInCloud returns an error if Env in {staging,prod} but any
// provider selector still points at a dev value.
func (c *Config) FatalIfLocalInCloud() error {
    if c.Env != "staging" && c.Env != "prod" { return nil }
    var bad []string
    check := func(name, value, dev string) {
        if value == dev || value == "" { bad = append(bad, name+"="+value) }
    }
    check("AUTH_PROVIDER",    c.AuthProvider,    "local")
    check("BILLING_PROVIDER", c.BillingProvider, "local")
    check("PATCH_STORE",      c.PatchStore,      "minio")
    check("KEYVAULT",         c.KeyVault,        "local")
    check("SECRETS",          c.SecretsBackend,  "env")
    check("MAILER",           c.Mailer,          "smtp")
    check("GRAPH_PROVIDER",   c.GraphProvider,   "compose")
    check("VALIDATOR_RUNNER", c.ValidatorRunner, "docker")
    if !c.TemporalCloud                  { bad = append(bad, "TEMPORAL_CLOUD=0") }
    if c.OTLPTarget != "grafana-cloud"   { bad = append(bad, "OTLP_TARGET="+c.OTLPTarget) }
    if len(bad) > 0 { return fmt.Errorf("cloud env %q has local providers: %v", c.Env, bad) }
    return nil
}
```

### Task 0.5: Startup assertion wiring

**Files:**
- Modify: `services/control-plane/cmd/server/main.go` (COORDINATOR)

- [ ] **Step 1: Right after `cfg, err := config.Load()`**

```go
if err := cfg.FatalIfLocalInCloud(); err != nil {
    slog.Error("fatal: cloud env has local providers", "err", err)
    os.Exit(2)
}
```

- [ ] **Step 2: Verify** — `ENV=staging AUTH_PROVIDER=local make run` exits 2 with `fatal: cloud env has local providers err="cloud env \"staging\" has local providers: [...]"` in stderr.

### Task 0.6: Env exemplar

**Files:**
- Create: `services/control-plane/.env.cloud.example`

- [ ] **Step 1: Write the canonical staging/prod env block**

The full env list lives in spec Appendix A. The file mirrors it verbatim. Reviewer-facing only; never sourced in CI.

### Task 0.7: Commit Stage 0

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
git add packages/db/schema.ts packages/db/migrations services/control-plane/migrations services/control-plane/internal/adapter/repo/queries services/control-plane/internal/platform/config services/control-plane/cmd/server/main.go services/control-plane/.env.cloud.example
git commit -m "feat(phase7): schema migrations + config + startup assertion (stage 0)"
```

---

## Stage 1 — Terraform bootstrap + modules library

### Task 1.1: Bootstrap script

**Files:**
- Create: `infra/terraform/bootstrap.sh`
- Create: `infra/terraform/README.md`

- [ ] **Step 1: `bootstrap.sh` — one-shot script run with management-account admin creds (skeleton)**

```bash
#!/usr/bin/env bash
# Creates the Terraform remote state bucket + DynamoDB lock table.
# Run ONCE per AWS Organization. Idempotent.
set -euo pipefail

# Flags: --aws-account-id <id> --state-bucket-name <name> --lock-table-name <name>
# (parsing block omitted for brevity)

REGION="${AWS_REGION:-us-east-1}"

aws s3api create-bucket --bucket "$STATE_BUCKET" --region "$REGION" --object-lock-enabled-for-bucket || true
aws s3api put-bucket-versioning   --bucket "$STATE_BUCKET" --versioning-configuration Status=Enabled
aws s3api put-bucket-encryption   --bucket "$STATE_BUCKET" --server-side-encryption-configuration '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'
aws s3api put-public-access-block --bucket "$STATE_BUCKET" --public-access-block-configuration 'BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true'

aws dynamodb create-table --table-name "$LOCK_TABLE" \
  --attribute-definitions AttributeName=LockID,AttributeType=S \
  --key-schema AttributeName=LockID,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST --region "$REGION" 2>/dev/null || true

echo "Bootstrap complete: bucket=$STATE_BUCKET table=$LOCK_TABLE region=$REGION"
```

`chmod +x infra/terraform/bootstrap.sh`.

- [ ] **Step 2: `README.md` — operator runbook**

Sections: prerequisites, bootstrap, per-env init, plan, apply, destroy, troubleshooting (Object Lock conflict, lock-row stuck on aborted apply, cross-account assume-role). Reference the spec §5.5 for the exact terraform init incantation.

### Task 1.2: VPC module

**Files:**
- Create: `infra/terraform/modules/vpc/{main,variables,outputs}.tf`

- [ ] **Step 1: `modules/vpc/main.tf` — skeleton (~30 lines)**

```hcl
locals { name = "nexis-${var.env}" }

resource "aws_vpc" "this"             { cidr_block = var.vpc_cidr; enable_dns_support = true; enable_dns_hostnames = true; tags = merge(var.tags, { Name = "${local.name}-vpc" }) }
resource "aws_subnet"  "private"      { count = var.az_count;        vpc_id = aws_vpc.this.id; cidr_block = cidrsubnet(var.vpc_cidr, 4, count.index);     availability_zone = data.aws_availability_zones.available.names[count.index]; tags = merge(var.tags, { Name = "${local.name}-private-${count.index}", Tier = "private" }) }
resource "aws_subnet"  "public"       { count = min(var.az_count, 2); vpc_id = aws_vpc.this.id; cidr_block = cidrsubnet(var.vpc_cidr, 4, count.index + 8); availability_zone = data.aws_availability_zones.available.names[count.index]; map_public_ip_on_launch = true; tags = merge(var.tags, { Name = "${local.name}-public-${count.index}", Tier = "public" }) }
resource "aws_internet_gateway" "this" { vpc_id = aws_vpc.this.id; tags = merge(var.tags, { Name = "${local.name}-igw" }) }
resource "aws_eip"          "nat"     { count = var.nat_gateway_count; domain = "vpc" }
resource "aws_nat_gateway"  "this"    { count = var.nat_gateway_count; subnet_id = aws_subnet.public[count.index].id; allocation_id = aws_eip.nat[count.index].id }

resource "aws_route_table" "public"   { vpc_id = aws_vpc.this.id; route { cidr_block = "0.0.0.0/0" gateway_id = aws_internet_gateway.this.id } }
resource "aws_route_table" "private"  { count = var.az_count; vpc_id = aws_vpc.this.id; route { cidr_block = "0.0.0.0/0" nat_gateway_id = aws_nat_gateway.this[count.index % var.nat_gateway_count].id } }

resource "aws_route_table_association" "public"  { count = length(aws_subnet.public);  subnet_id = aws_subnet.public[count.index].id;  route_table_id = aws_route_table.public.id }
resource "aws_route_table_association" "private" { count = length(aws_subnet.private); subnet_id = aws_subnet.private[count.index].id; route_table_id = aws_route_table.private[count.index].id }

resource "aws_flow_log" "vpc" { iam_role_arn = aws_iam_role.flow_log.arn; log_destination = aws_cloudwatch_log_group.vpc_flow.arn; traffic_type = "ALL"; vpc_id = aws_vpc.this.id }
```

- [ ] **Step 2: `variables.tf` + `outputs.tf`**

`variables.tf` declares `env`, `vpc_cidr`, `az_count`, `nat_gateway_count`, `tags`. `outputs.tf` exports `vpc_id`, `private_subnet_ids`, `public_subnet_ids`.

### Task 1.3: KMS module

**Files:**
- Create: `infra/terraform/modules/kms-key/{main,variables,outputs}.tf`

- [ ] **Step 1: `main.tf` skeleton**

```hcl
resource "aws_kms_key" "this" {
  description             = "nexis-${var.env}-${var.purpose}"
  enable_key_rotation     = var.rotation
  deletion_window_in_days = 30
  tags                    = merge(var.tags, { Name = "nexis-${var.env}-${var.purpose}" })
}

resource "aws_kms_alias" "this" {
  name          = "alias/nexis-${var.env}-${var.purpose}"
  target_key_id = aws_kms_key.this.id
}
```

Outputs: `key_id`, `key_arn`, `alias_name`.

### Task 1.4: S3 bucket module

**Files:**
- Create: `infra/terraform/modules/s3-bucket/{main,variables,outputs}.tf`

- [ ] **Step 1: `main.tf` — generic encrypted bucket factory with optional Object Lock (skeleton)**

```hcl
resource "aws_s3_bucket"                                 "this" { bucket = "nexis-${var.env}-${var.name}"; object_lock_enabled = var.object_lock }
resource "aws_s3_bucket_versioning"                      "this" { bucket = aws_s3_bucket.this.id; versioning_configuration { status = "Enabled" } }
resource "aws_s3_bucket_public_access_block"             "this" { bucket = aws_s3_bucket.this.id; block_public_acls = true; block_public_policy = true; ignore_public_acls = true; restrict_public_buckets = true }
resource "aws_s3_bucket_server_side_encryption_configuration" "this" {
  bucket = aws_s3_bucket.this.id
  rule { apply_server_side_encryption_by_default { sse_algorithm = var.kms_key_arn == null ? "AES256" : "aws:kms"; kms_master_key_id = var.kms_key_arn }; bucket_key_enabled = true }
}
resource "aws_s3_bucket_lifecycle_configuration" "this" {
  bucket = aws_s3_bucket.this.id
  rule { id = "glacier-old-versions"; status = "Enabled"; noncurrent_version_transition { noncurrent_days = 90; storage_class = "DEEP_ARCHIVE" } }
}
resource "aws_s3_bucket_object_lock_configuration" "this" {
  count = var.object_lock ? 1 : 0; bucket = aws_s3_bucket.this.id
  rule { default_retention { mode = "COMPLIANCE"; years = var.object_lock_years } }
}
```

Variables: `env`, `name`, `kms_key_arn` (nullable), `object_lock` (bool, default false), `object_lock_years` (default 7), `tags`. Outputs: `bucket_name`, `bucket_arn`.

### Task 1.5: Secrets Manager module

**Files:**
- Create: `infra/terraform/modules/secrets-manager-secret/{main,variables,outputs}.tf`

```hcl
resource "aws_secretsmanager_secret" "this" {
  name        = "nexis-${var.env}/${var.service}/${var.name}"
  description = var.description
  kms_key_id  = var.kms_key_arn
  tags        = merge(var.tags, { Name = "nexis-${var.env}-${var.service}-${var.name}" })
}

resource "aws_secretsmanager_secret_version" "this" {
  count          = var.placeholder_value == null ? 0 : 1
  secret_id      = aws_secretsmanager_secret.this.id
  secret_string  = var.placeholder_value
}
```

Outputs: `secret_arn`, `secret_name`.

### Task 1.6: Route53 + CloudWatch logs + IAM task role modules

**Files:**
- Create: `infra/terraform/modules/route53-zone/{main,variables,outputs}.tf`
- Create: `infra/terraform/modules/cloudwatch-logs/{main,variables,outputs}.tf`
- Create: `infra/terraform/modules/iam-task-role/{main,variables,outputs}.tf`

- [ ] **Step 1: Route53** — wraps `aws_route53_zone` + a wildcard A-record placeholder. Outputs: `zone_id`, `name_servers`.

- [ ] **Step 2: CloudWatch logs** — per-service log group. Variables: `env`, `service`, `retention_days` (defaults per spec §5.3: 30 dev / 90 staging / 365 prod). Outputs: `log_group_name`, `log_group_arn`.

- [ ] **Step 3: IAM task role** — generic task role + execution role pair scoped by `var.service`. Reference spec Appendix C for the exact per-service policy actions/resources.

```hcl
resource "aws_iam_role" "task" {
  name = "nexis-${var.env}-${var.service}-task"
  assume_role_policy = jsonencode({Version = "2012-10-17", Statement = [{Effect = "Allow", Principal = {Service = "ecs-tasks.amazonaws.com"}, Action = "sts:AssumeRole"}]})
}

resource "aws_iam_role_policy" "task" {
  count  = length(var.policies)
  name   = var.policies[count.index].name
  role   = aws_iam_role.task.id
  policy = var.policies[count.index].document
}

resource "aws_iam_role" "execution" {
  name = "nexis-${var.env}-${var.service}-exec"
  assume_role_policy = jsonencode({Version = "2012-10-17", Statement = [{Effect = "Allow", Principal = {Service = "ecs-tasks.amazonaws.com"}, Action = "sts:AssumeRole"}]})
}

resource "aws_iam_role_policy_attachment" "ecr_pull" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}
```

Outputs: `task_role_arn`, `execution_role_arn`.

### Task 1.7: Smoke validate

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/infra/terraform/modules/vpc && terraform init -backend=false && terraform validate
cd ../kms-key                       && terraform init -backend=false && terraform validate
cd ../s3-bucket                     && terraform init -backend=false && terraform validate
cd ../secrets-manager-secret        && terraform init -backend=false && terraform validate
cd ../route53-zone                  && terraform init -backend=false && terraform validate
cd ../cloudwatch-logs               && terraform init -backend=false && terraform validate
cd ../iam-task-role                 && terraform init -backend=false && terraform validate
```

Expected: each prints `Success! The configuration is valid.`

### Task 1.8: Commit Stage 1

```bash
git add infra/terraform/bootstrap.sh infra/terraform/README.md infra/terraform/modules
git commit -m "feat(infra): terraform bootstrap + foundational modules (vpc/kms/s3/secrets/route53/cw-logs/iam) (stage 1)"
```

---

## Stage 2 — Terraform data plane modules + dev env root + hello-world smoke deploy

### Task 2.1: RDS module

**Files:**
- Create: `infra/terraform/modules/rds/{main,variables,outputs}.tf`

- [ ] **Step 1: `main.tf` — Postgres 16 Multi-AZ + pgvector parameter group (skeleton)**

```hcl
resource "aws_db_subnet_group"    "this" { name = "nexis-${var.env}-rds"; subnet_ids = var.subnet_ids }
resource "aws_db_parameter_group" "this" { name = "nexis-${var.env}-pg16-pgvector"; family = "postgres16"
  parameter { name = "shared_preload_libraries"; value = "pg_stat_statements,pgvector"; apply_method = "pending-reboot" } }

resource "aws_security_group" "rds" {
  name = "nexis-${var.env}-rds-sg"; vpc_id = var.vpc_id
  ingress { from_port = 5432; to_port = 5432; protocol = "tcp"; security_groups = var.ecs_security_group_ids }
  egress  { from_port = 0; to_port = 0; protocol = "-1"; cidr_blocks = ["0.0.0.0/0"] }
}

resource "aws_db_instance" "this" {
  identifier                  = "nexis-${var.env}-rds"
  engine                      = "postgres"   engine_version = "16.4"
  instance_class              = var.instance_class
  allocated_storage           = var.storage_gb   max_allocated_storage = var.storage_gb * 4
  storage_type                = "gp3"   storage_encrypted = true   kms_key_id = var.kms_key_arn
  db_name                     = "nexis"   username = "nexis_admin"   manage_master_user_password = true
  multi_az                    = var.multi_az   publicly_accessible = false
  vpc_security_group_ids      = [aws_security_group.rds.id]
  db_subnet_group_name        = aws_db_subnet_group.this.name
  parameter_group_name        = aws_db_parameter_group.this.name
  backup_retention_period     = 7   backup_window = "06:00-06:30"   copy_tags_to_snapshot = true
  deletion_protection         = var.env == "prod"   skip_final_snapshot = var.env == "dev"
  performance_insights_enabled = true
  tags = merge(var.tags, { Name = "nexis-${var.env}-rds" })
}
```

Outputs: `endpoint`, `port`, `db_name`, `master_user_secret_arn`.

### Task 2.2: ElastiCache Redis module

**Files:**
- Create: `infra/terraform/modules/redis/{main,variables,outputs}.tf`

- [ ] **Step 1: `main.tf` — Redis Serverless (skeleton)**

```hcl
resource "aws_elasticache_serverless_cache" "this" {
  engine = "redis"; name = "nexis-${var.env}-redis"
  subnet_ids = var.subnet_ids; security_group_ids = [aws_security_group.redis.id]; kms_key_id = var.kms_key_arn
  cache_usage_limits {
    data_storage    { maximum = var.max_data_gb; unit = "GB" }
    ecpu_per_second { maximum = var.max_ecpu }
  }
}

resource "aws_security_group" "redis" {
  name = "nexis-${var.env}-redis-sg"; vpc_id = var.vpc_id
  ingress { from_port = 6379; to_port = 6379; protocol = "tcp"; security_groups = var.ecs_security_group_ids }
  egress  { from_port = 0; to_port = 0; protocol = "-1"; cidr_blocks = ["0.0.0.0/0"] }
}
```

### Task 2.3: ALB + WAF + ACM module

**Files:**
- Create: `infra/terraform/modules/alb/{main,variables,outputs}.tf`
- Create: `infra/terraform/modules/waf/{main,variables,outputs}.tf`

ALB: HTTPS-only listener, HTTP→HTTPS redirect, security group, ACM cert via DNS validation, optional WAF association. Targets are wired by the `ecs-service` module via output `arn` reference. WAF: WAFv2 web ACL with AWS-managed Common Rule Set + Known Bad Inputs + a 2000 req/5min/IP rate limit rule. Reference spec §5.3 for the rule set list; do not inline rule JSON here.

### Task 2.4: ECS cluster + ECS service modules

**Files:**
- Create: `infra/terraform/modules/ecs-cluster/{main,variables,outputs}.tf`
- Create: `infra/terraform/modules/ecs-service/{main,variables,outputs}.tf`

- [ ] **Step 1: `ecs-cluster/main.tf`**

```hcl
resource "aws_ecs_cluster" "this" {
  name = "nexis-${var.env}"
  setting {
    name  = "containerInsights"
    value = "enabled"
  }
  tags = merge(var.tags, { Name = "nexis-${var.env}" })
}

resource "aws_service_discovery_private_dns_namespace" "this" {
  name        = "nexis.local"
  vpc         = var.vpc_id
  description = "service discovery for nexis-${var.env}"
}

resource "aws_ecs_cluster_capacity_providers" "this" {
  cluster_name       = aws_ecs_cluster.this.name
  capacity_providers = ["FARGATE", "FARGATE_SPOT"]
  default_capacity_provider_strategy {
    capacity_provider = "FARGATE"
    weight            = 100
  }
}
```

- [ ] **Step 2: `ecs-service/main.tf` — generic Fargate service factory (skeleton)**

```hcl
resource "aws_ecs_task_definition" "this" {
  family = "nexis-${var.env}-${var.service}"
  network_mode = "awsvpc"; requires_compatibilities = ["FARGATE"]
  cpu = var.cpu; memory = var.memory
  task_role_arn = var.task_role_arn; execution_role_arn = var.execution_role_arn
  container_definitions = jsonencode([{
    name = var.service; image = var.image; essential = true
    portMappings = [{ containerPort = var.container_port, protocol = "tcp" }]
    environment  = [for k, v in var.env_vars : { name = k, value = v }]
    secrets      = [for k, arn in var.secrets : { name = k, valueFrom = arn }]
    logConfiguration = { logDriver = "awslogs", options = { awslogs-group = var.log_group_name, awslogs-region = var.region, awslogs-stream-prefix = var.service } }
    healthCheck      = { command = ["CMD-SHELL", "curl -f http://localhost:${var.container_port}/healthz || exit 1"], interval = 30, timeout = 5, retries = 3, startPeriod = 60 }
  }])
}

resource "aws_ecs_service" "this" {
  name = "nexis-${var.env}-${var.service}"; cluster = var.cluster_arn
  task_definition = aws_ecs_task_definition.this.arn; desired_count = var.min_count; launch_type = "FARGATE"
  network_configuration { subnets = var.subnet_ids; security_groups = [aws_security_group.service.id]; assign_public_ip = false }
  load_balancer { target_group_arn = aws_lb_target_group.this.arn; container_name = var.service; container_port = var.container_port }
  deployment_controller { type = "ECS" }
  lifecycle { ignore_changes = [desired_count] }   # autoscaling owns this
}

resource "aws_appautoscaling_target" "this" {
  service_namespace = "ecs"; resource_id = "service/${var.cluster_name}/${aws_ecs_service.this.name}"
  scalable_dimension = "ecs:service:DesiredCount"; min_capacity = var.min_count; max_capacity = var.max_count
}

resource "aws_appautoscaling_policy" "cpu" {
  name = "${aws_ecs_service.this.name}-cpu"; service_namespace = "ecs"
  resource_id = aws_appautoscaling_target.this.resource_id; scalable_dimension = aws_appautoscaling_target.this.scalable_dimension
  policy_type = "TargetTrackingScaling"
  target_tracking_scaling_policy_configuration { target_value = 60; predefined_metric_specification { predefined_metric_type = "ECSServiceAverageCPUUtilization" } }
}

resource "aws_appautoscaling_policy" "mem" {
  name = "${aws_ecs_service.this.name}-mem"; service_namespace = "ecs"
  resource_id = aws_appautoscaling_target.this.resource_id; scalable_dimension = aws_appautoscaling_target.this.scalable_dimension
  policy_type = "TargetTrackingScaling"
  target_tracking_scaling_policy_configuration { target_value = 75; predefined_metric_specification { predefined_metric_type = "ECSServiceAverageMemoryUtilization" } }
}
```

Min/max per service (from spec §5.3): control-plane 2-10, validator 1-4, gitops 1-4, web 2-6. Passed via the env-root composition.

### Task 2.5: dev env-root composition

**Files:**
- Create: `infra/terraform/envs/dev/{backend,main,variables,outputs}.tf`
- Create: `infra/terraform/envs/dev/terraform.tfvars`

- [ ] **Step 1: `backend.tf`**

```hcl
terraform {
  required_version = ">= 1.10"
  required_providers {
    aws = { source = "hashicorp/aws"  version = "~> 5.79" }
  }
  backend "s3" {
    bucket         = "nexis-terraform-state"
    key            = "envs/dev/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "nexis-terraform-locks"
    encrypt        = true
  }
}
```

- [ ] **Step 2: `main.tf` — compose modules (skeleton; each `module "X" { source = "../../modules/X"; ... }` passes the corresponding tfvars in)**

```hcl
provider "aws" { region = var.region; default_tags { tags = var.tags } }

module "vpc"             { source = "../../modules/vpc"             ... }
module "kms_data"        { source = "../../modules/kms-key"         purpose = "data"        ... }
module "s3_patches"      { source = "../../modules/s3-bucket"       name = "patches"        kms_key_arn = module.kms_data.key_arn ... }
module "s3_audit_export" { source = "../../modules/s3-bucket"       name = "audit-export"   object_lock = true object_lock_years = var.audit_object_lock_years ... }
module "rds"             { source = "../../modules/rds"             ... }
module "redis"           { source = "../../modules/redis"           ... }
module "ecs_cluster"     { source = "../../modules/ecs-cluster"     ... }
module "alb"             { source = "../../modules/alb"             ... }
module "ecs_hello"       { source = "../../modules/ecs-service"     service = "hello" image = "public.ecr.aws/docker/library/nginx:1.27-alpine" cpu = 256 memory = 512 ... }
```

Each module gets `env`, `vpc_id` (or `subnet_ids`), `tags`. The hello-world service is the smoke target. Once Stage 7's release pipeline lands, additional `module "ecs_control_plane" { ... }`, `module "ecs_validator" { ... }` etc. are added to the same env-root.

- [ ] **Step 3: `terraform.tfvars` (dev)** — `env="dev", region="us-east-1", vpc_cidr="10.10.0.0/20", az_count=2, nat_gateway_count=1, rds_instance_class="db.t4g.medium", rds_multi_az=false, rds_storage_gb=50, domain_name="dev.nexis.dev", acm_cert_san_wildcard=true, audit_object_lock_years=1, tags={Environment="dev", ManagedBy="terraform"}`.

### Task 2.6: Hello-world smoke deploy

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/infra/terraform/envs/dev
terraform init
terraform plan -out=tfplan
terraform apply tfplan
ALB_DNS=$(terraform output -raw alb_dns_name)
curl -fsS "https://${ALB_DNS}/" | head -5     # expect nginx welcome page
```

Expected: HTML containing `<title>Welcome to nginx!</title>`. Total apply time < 30 minutes per spec §13 criterion 2.

### Task 2.7: Commit Stage 2

```bash
git add infra/terraform/modules/{rds,redis,alb,waf,ecs-cluster,ecs-service} infra/terraform/envs/dev
git commit -m "feat(infra): data-plane modules + dev env root + hello-world smoke (stage 2)"
```

---

## Stage 3 — Wave-2 shard A+B: KMS + Secrets + S3 + Mailer + AWS helpers

> **Pattern C dispatch.** Run Stage 3 alongside Stages 4 and 5 via three parallel `backend-engineer` agents. The shard owns the paths listed in §1.2. The coordinator merges after **all three** Wave-2 shards return green.

### Task 3.1: AWS SDK helpers

**Files:**
- Create: `services/control-plane/internal/platform/aws/{config,kms,s3,secrets,ses}.go`

- [ ] **Step 1: `aws/config.go`** — `Load(ctx, region) (aws.Config, error)` wraps `config.LoadDefaultConfig(ctx, config.WithRegion(region))`. Default chain handles env, shared, IRSA, task role.

- [ ] **Step 2: `aws/{kms,s3,secrets,ses}.go`** — each a 3-line thin wrapper: `func NewKMS(cfg aws.Config) *kms.Client { return kms.NewFromConfig(cfg) }`. Repeat for `s3.NewFromConfig`, `secretsmanager.NewFromConfig`, `sesv2.NewFromConfig`.

### Task 3.2: KMSVault adapter (exemplar — full body)

**Files:**
- Create: `services/control-plane/internal/adapter/keyvault/kms.go`
- Create: `services/control-plane/internal/adapter/keyvault/kms_test.go`
- Create: `services/control-plane/internal/adapter/keyvault/factory.go`

- [ ] **Step 1: `kms.go` — full envelope encryption**

```go
package keyvault

import (
    "context"
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/binary"
    "errors"
    "fmt"
    "io"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/kms"
    "github.com/aws/aws-sdk-go-v2/service/kms/types"

    "nexis/control-plane/internal/domain"
)

const wireVersion byte = 0x01

// KMSVault implements domain.KeyVault via AWS KMS envelope encryption.
// Wire format: [1 ver][2 dek_len][dek][12 nonce][ciphertext].
type KMSVault struct {
    kms      *kms.Client
    keyARN   string
}

func NewKMSVault(c *kms.Client, keyARN string) *KMSVault {
    return &KMSVault{kms: c, keyARN: keyARN}
}

func (v *KMSVault) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
    orgID, secretKind := domain.TenantFromCtx(ctx), domain.SecretKindFromCtx(ctx)
    if orgID == "" || secretKind == "" {
        return nil, fmt.Errorf("keyvault.kms: missing org_id or secret_kind in ctx")
    }
    out, err := v.kms.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
        KeyId:   aws.String(v.keyARN),
        KeySpec: types.DataKeySpecAes256,
        EncryptionContext: map[string]string{
            "org_id":      orgID,
            "secret_kind": secretKind,
        },
    })
    if err != nil {
        return nil, fmt.Errorf("kms generate-data-key: %w", err)
    }
    defer func() {
        for i := range out.Plaintext {
            out.Plaintext[i] = 0   // zero the DEK
        }
    }()
    block, err := aes.NewCipher(out.Plaintext)
    if err != nil {
        return nil, err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, err
    }
    ct := gcm.Seal(nil, nonce, plaintext, nil)

    buf := make([]byte, 0, 1+2+len(out.CiphertextBlob)+len(nonce)+len(ct))
    buf = append(buf, wireVersion)
    dekLen := make([]byte, 2)
    binary.BigEndian.PutUint16(dekLen, uint16(len(out.CiphertextBlob)))
    buf = append(buf, dekLen...)
    buf = append(buf, out.CiphertextBlob...)
    buf = append(buf, nonce...)
    buf = append(buf, ct...)
    return buf, nil
}

func (v *KMSVault) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
    if len(ciphertext) < 1+2+12 {
        return nil, errors.New("keyvault.kms: ciphertext too short")
    }
    if ciphertext[0] != wireVersion {
        return nil, fmt.Errorf("keyvault.kms: unknown wire version 0x%02x", ciphertext[0])
    }
    dekLen := binary.BigEndian.Uint16(ciphertext[1:3])
    if len(ciphertext) < 3+int(dekLen)+12 {
        return nil, errors.New("keyvault.kms: malformed wire")
    }
    encDEK := ciphertext[3 : 3+dekLen]
    nonce  := ciphertext[3+dekLen : 3+dekLen+12]
    ct     := ciphertext[3+dekLen+12:]

    orgID, secretKind := domain.TenantFromCtx(ctx), domain.SecretKindFromCtx(ctx)
    out, err := v.kms.Decrypt(ctx, &kms.DecryptInput{
        CiphertextBlob: encDEK,
        EncryptionContext: map[string]string{
            "org_id":      orgID,
            "secret_kind": secretKind,
        },
    })
    if err != nil {
        return nil, fmt.Errorf("kms decrypt: %w", err)
    }
    defer func() {
        for i := range out.Plaintext {
            out.Plaintext[i] = 0
        }
    }()
    block, err := aes.NewCipher(out.Plaintext)
    if err != nil {
        return nil, err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    return gcm.Open(nil, nonce, ct, nil)
}
```

- [ ] **Step 2: `kms_test.go`** — table-driven test against `localstack` (started via `docker-compose.test.yml`). Cases: round-trip a 1MB blob; round-trip with a different `secret_kind` succeeds; round-trip across two org ids fails with `InvalidCiphertextException`.

- [ ] **Step 3: `factory.go` — KEYVAULT selector**

```go
func New(ctx context.Context, cfg *config.Config) (domain.KeyVault, error) {
    switch cfg.KeyVault {
    case "local": return NewLocalKeyVault(cfg.KeyVaultMasterKey)
    case "kms":
        awsCfg, err := aws.Load(ctx, cfg.AWSRegion)
        if err != nil { return nil, fmt.Errorf("aws load: %w", err) }
        return NewKMSVault(aws.NewKMS(awsCfg), cfg.KMSKeyARN), nil
    default: return nil, fmt.Errorf("unknown KEYVAULT=%q", cfg.KeyVault)
    }
}
```

> **Same shape for the other adapters in this stage.** `secrets/secretsmanager.go`, `mailer/resend.go`, `mailer/awsses.go`, `patchstore/s3/store.go` follow the same skeleton: thin AWS SDK wrapper + factory that switches on the env var. Only KMSVault is pasted in full as the exemplar.

### Task 3.3: SecretsManagerStore adapter

**Files:**
- Create: `services/control-plane/internal/adapter/secrets/secretsmanager.go`
- Create: `services/control-plane/internal/adapter/secrets/local.go`
- Create: `services/control-plane/internal/adapter/secrets/factory.go`
- Create: `services/control-plane/internal/adapter/secrets/secretsmanager_test.go`

- [ ] **Step 1: Declare the new port (must NOT exist yet)**

If `internal/domain/secrets.go` does not exist, create it:

```go
package domain

import "context"

type SecretsStore interface {
    Get(ctx context.Context, name string) ([]byte, error)
    Put(ctx context.Context, name string, value []byte) error
    GetJSON(ctx context.Context, name string, out any) error
}
```

- [ ] **Step 2: `secretsmanager.go`** — AWS SDK v2 `GetSecretValue` / `PutSecretValue`. JSON unmarshalling helper. Path prefix from `cfg.SecretsPrefix`.

- [ ] **Step 3: `local.go`** — file-backed dev impl at `$XDG_CONFIG_HOME/nexis/secrets/<name>`. Permissions 0600.

- [ ] **Step 4: `factory.go`** — SECRETS=env|secretsmanager selector. The `env` branch is a thin wrapper over `os.Getenv` to keep the contract uniform.

### Task 3.4: S3 PatchStore — fill in Phase 4 stub

**Files:**
- Modify: `services/control-plane/internal/adapter/patchstore/s3/store.go`
- Create: `services/control-plane/internal/adapter/patchstore/s3/store_test.go`

- [ ] **Step 1: Replace the stub body of `Put`/`Get`/`Delete` with real AWS SDK v2 calls + presigned URL helper (skeleton)**

```go
func (s *S3Store) Put(ctx context.Context, key string, blob []byte) error {
    _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
        Bucket: aws.String(s.bucket), Key: aws.String(key),
        Body: bytes.NewReader(blob),
        ServerSideEncryption: types.ServerSideEncryptionAes256,
        BucketKeyEnabled: aws.Bool(true),
    })
    return err
}

func (s *S3Store) Get(ctx context.Context, key string) ([]byte, error) {
    out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
    if err != nil { return nil, err }
    defer out.Body.Close()
    return io.ReadAll(out.Body)
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
    _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
    return err
}

func (s *S3Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
    pre, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}, s3.WithPresignExpires(ttl))
    if err != nil { return "", err }
    return pre.URL, nil
}
```

- [ ] **Step 2: `store_test.go`** — uses `localstack` S3. Asserts Put → Get round-trip, Presign URL returns the same bytes via `curl`, Delete makes subsequent Get return 404.

### Task 3.5: Resend mailer + AWS SES mailer

**Files:**
- Create: `services/control-plane/internal/adapter/mailer/resend.go`
- Create: `services/control-plane/internal/adapter/mailer/awsses.go`
- Modify: `services/control-plane/internal/adapter/mailer/factory.go`

- [ ] **Step 1: `resend.go`** — POSTs to `https://api.resend.com/emails` with bearer token. Body shape: `{from, to, subject, html, text}`. Returns the `id` field as the message id.

- [ ] **Step 2: `awsses.go`** — uses `sesv2.SendEmail` with the Phase 2 message shape. Reads `SES_FROM_ADDRESS` from config.

- [ ] **Step 3: `factory.go`** — MAILER=smtp|resend|ses selector. The smtp branch stays for dev (MailHog).

### Task 3.6: Commit Stage 3 (Wave-2 shard A+B)

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane
make build && make test && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
git add services/control-plane/internal/adapter/{keyvault,secrets,patchstore/s3,mailer} services/control-plane/internal/platform/aws services/control-plane/internal/domain/secrets.go
git commit -m "feat(adapter): KMSVault + SecretsManagerStore + S3 PatchStore + Resend/SES mailer (stage 3)"
```

---

## Stage 4 — Wave-2 shard C: Modal validator + Temporal Cloud + Grafana Cloud OTLP + AuraDB

> **Pattern C dispatch.** Run Stage 4 alongside Stages 3 and 5. Owns the paths in §1.2.

### Task 4.1: Lift Phase 4 Docker runner behind a Runner interface

**Files:**
- Create: `services/validator/internal/runner/runner.go`
- Create: `services/validator/internal/runner/docker.go`  (move from `sandbox/docker.go`)
- Create: `services/validator/internal/runner/factory.go`
- Modify: `services/validator/internal/sandbox/docker.go`  (alias-file)

- [ ] **Step 1: `runner/runner.go`** — declare `ValidateRequest{RepoSHA,PatchDiff,Hypothesis,Tests}`, `ValidateResponse{TestsPassed,TestCount,FailCount,Coverage,HypothesisFailures,Logs,Runner}`, and `Runner interface { Validate(ctx, req) (resp, error) }`. JSON tags use snake_case.

- [ ] **Step 2: `runner/docker.go`** — paste the Phase 4 body verbatim. Sets `resp.Runner = "docker"` before return.

- [ ] **Step 3: `sandbox/docker.go`** — alias-file: `package sandbox; func NewDocker() runner.Runner { return runner.NewDocker() }`.

- [ ] **Step 4: `runner/factory.go`** — switch on `VALIDATOR_RUNNER`: `docker` (default) → `NewDocker()`; `modal` → `NewModal(endpoint, token, &http.Client{Timeout: 5*time.Minute})`; anything else → `fmt.Errorf("unknown VALIDATOR_RUNNER=%q", ...)`.

### Task 4.2: Modal runner

**Files:**
- Create: `services/validator/internal/runner/modal.go`

```go
package runner

type ModalRunner struct { endpoint, token string; http *http.Client }

func NewModal(endpoint, token string, c *http.Client) *ModalRunner {
    return &ModalRunner{endpoint, token, c}
}

func (m *ModalRunner) Validate(ctx context.Context, req ValidateRequest) (ValidateResponse, error) {
    var zero ValidateResponse
    body, _ := json.Marshal(req)
    r, _ := http.NewRequestWithContext(ctx, "POST", m.endpoint, bytes.NewReader(body))
    r.Header.Set("Authorization", "Bearer "+m.token)
    r.Header.Set("Content-Type", "application/json")
    resp, err := m.http.Do(r)
    if err != nil { return zero, fmt.Errorf("modal: %w", err) }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        b, _ := io.ReadAll(resp.Body)
        return zero, fmt.Errorf("modal %d: %s", resp.StatusCode, b)
    }
    var out ValidateResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { return zero, err }
    out.Runner = "modal"
    return out, nil
}
```

### Task 4.3: Modal deployment artefacts

**Files:**
- Create: `services/validator/modal/nexis_validator.py`
- Create: `services/validator/modal/Dockerfile.modal`
- Create: `services/validator/modal/deploy.sh`
- Create: `services/validator/modal/README.md`

- [ ] **Step 1: `nexis_validator.py`** — single Modal function. Signature: `validate(body: dict) -> dict`. Body matches `ValidateRequest`; returns a dict matching `ValidateResponse`. The function applies the patch, runs `pytest` + `hypothesis` (when `body["hypothesis"] is True`), returns the consolidated result.

- [ ] **Step 2: `Dockerfile.modal`** — Python 3.12 + pytest + hypothesis + the Phase 6 fixture deps. Same base image as the Phase 6 hypothesis sidecar.

- [ ] **Step 3: `deploy.sh`** — `modal deploy nexis_validator.py`. README documents `modal token new`, `modal app list`, `modal app logs`.

### Task 4.4: Temporal Cloud swap

**Files:**
- Modify: `services/control-plane/internal/platform/temporal/client.go`

- [ ] **Step 1: Append the Cloud branch (skeleton)**

```go
func NewClient(cfg *config.Config) (client.Client, error) {
    if cfg.TemporalCloud {
        certPEM, _ := os.ReadFile(cfg.TemporalTLSCertPath)
        keyPEM,  _ := os.ReadFile(cfg.TemporalTLSKeyPath)
        crt, err := tls.X509KeyPair(certPEM, keyPEM)
        if err != nil { return nil, fmt.Errorf("temporal tls: %w", err) }
        return client.Dial(client.Options{
            HostPort:  cfg.TemporalHost, Namespace: cfg.TemporalNamespace,
            ConnectionOptions: client.ConnectionOptions{
                TLS: &tls.Config{Certificates: []tls.Certificate{crt}, MinVersion: tls.VersionTLS13},
            },
        })
    }
    return client.Dial(client.Options{HostPort: cfg.TemporalHost, Namespace: cfg.TemporalNamespace})
}
```

`TemporalHost` defaults to `temporal:7233` (compose) and is overridden to `nexis-prod.tmprl.cloud:7233` in cloud envs.

### Task 4.5: Grafana Cloud OTLP exporter

**Files:**
- Modify: `services/control-plane/internal/platform/otel/otel.go`

- [ ] **Step 1: Append the cloud branch (skeleton)**

```go
func newTraceExporter(ctx context.Context, cfg *config.Config) (trace.SpanExporter, error) {
    if cfg.OTLPTarget == "grafana-cloud" {
        creds := base64.StdEncoding.EncodeToString([]byte(cfg.GrafanaInstanceID + ":" + cfg.GrafanaAPIToken))
        return otlptracehttp.New(ctx,
            otlptracehttp.WithEndpoint(cfg.GrafanaOTLPEndpoint),
            otlptracehttp.WithURLPath("/otlp/v1/traces"),
            otlptracehttp.WithHeaders(map[string]string{"Authorization": "Basic " + creds}))
    }
    return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint), otlptracegrpc.WithInsecure())
}
```

Logs + metrics exporters get the same treatment with `/otlp/v1/logs` and `/otlp/v1/metrics` URL paths.

### Task 4.6: Neo4j AuraDB helper

**Files:**
- Create: `services/control-plane/internal/adapter/graphstore/neo4j/aura.go`
- Create: `services/control-plane/internal/adapter/graphstore/factory.go`

- [ ] **Step 1: `aura.go`** — when URI scheme is `neo4j+s://` use TLS-with-system-root-CAs config; otherwise fall through to the Phase 6 plain bolt driver:

```go
func NewAuraDriver(uri, user, pass string) (neo4j.DriverWithContext, error) {
    return neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, pass, ""),
        func(c *neo4j.Config) {
            c.MaxConnectionPoolSize = 50
        })
}
```

- [ ] **Step 2: `factory.go`** — GRAPH_PROVIDER=compose|aura selector. The Phase 6 store wraps whichever driver is returned.

### Task 4.7: Modify validator HTTP handler to use the runner

**Files:**
- Modify: `services/validator/internal/transport/http/handler.go`

- [ ] **Step 1: At server boot, call `runner.New()` once; inject into the handler struct; the `/v1/validate` handler reads from `h.runner.Validate(ctx, req)` instead of the direct `sandbox` call.**

### Task 4.8: Commit Stage 4

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/validator   && go build ./... && go test ./...
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
git add services/validator services/control-plane/internal/platform/temporal services/control-plane/internal/platform/otel services/control-plane/internal/adapter/graphstore
git commit -m "feat(adapter): Modal runner + Temporal Cloud + Grafana Cloud OTLP + AuraDB driver (stage 4)"
```

---

## Stage 5 — Wave-2 shard D: WorkOS provider + OAuth callback + SCIM + JIT provisioning + web sign-in

> **Pattern C dispatch.** Run Stage 5 alongside Stages 3 and 4. Owns the paths in §1.2.

### Task 5.1: WorkOS provider real body

**Files:**
- Modify: `services/control-plane/internal/adapter/auth/workos/provider.go`
- Create: `services/control-plane/internal/adapter/auth/workos/callback.go`
- Create: `services/control-plane/internal/adapter/auth/workos/scim.go`
- Create: `services/control-plane/internal/adapter/auth/workos/role_map.go`
- Create: `services/control-plane/internal/adapter/auth/workos/provider_test.go`
- Modify: `services/control-plane/internal/adapter/auth/factory.go`

- [ ] **Step 1: `provider.go` — replace the Phase 2 stub with the WorkOS SDK body**

Imports `github.com/workos/workos-go/v4/pkg/usermanagement`. The provider holds an SDK client, a `RedirectURI`, a CSRF state generator (5-minute Redis TTL via the existing rate-limit cache), and the WorkOS client id + API key. Methods:

- `AuthorizationURL(ctx, state) (string, error)` — calls `usermanagement.GetAuthorizationURL` with `Provider="authkit"` and the state token.
- `AuthenticateWithCode(ctx, code) (UserInfo, error)` — calls `usermanagement.AuthenticateWithCode`, returns a normalised `UserInfo{WorkOSUserID, Email, FirstName, LastName, OrganizationID}`.

The full WorkOS code-exchange flow body is NOT pasted here (spec §7 has it). Reviewers reading this plan should treat it as: same shape as the `KMSVault` exemplar — thin wrapper around the SDK + audit.

- [ ] **Step 2: `callback.go`** — handles the `/v1/auth/workos/callback` HTTP path. Steps:
  1. Validate `state` against the Redis cache (delete on use).
  2. Call `provider.AuthenticateWithCode(ctx, code)`.
  3. Dispatch to `usecase/auth_jit_provision.go` (Task 5.3).
  4. Mint a session via the Phase 2 sessions table (`adminPool`).
  5. Set the `nexis_session` cookie via response header.
  6. 302 redirect to `/console` or `/onboarding/workspace`.

- [ ] **Step 3: `scim.go`** — handles SCIM Directory Sync webhook events. Soft-delete only (`UPDATE users SET deleted_at=now()`). See spec §7.2 + Risk 16.4.

- [ ] **Step 4: `role_map.go`** — `WorkOSRoleToInternal(workosRole, defaultRole)` returns `owner` for `admin`, `defaultRole` for `member` (tunable per org), `member` for anything else.

- [ ] **Step 5: `provider_test.go`** — replays recorded WorkOS HTTP responses from `testdata/workos/*.json`. Covers: happy path, expired state, mismatched state, unknown user, known user joining a new org.

- [ ] **Step 6: `auth/factory.go`** — switch on `cfg.AuthProvider`: `local` → existing `local.NewProvider(...)`; `workos` → `workos.NewProvider(cfg.WorkOSAPIKey, cfg.WorkOSClientID, cfg.WorkOSRedirectURI)`; default → error.

### Task 5.2: OAuth handler

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/oauth.go`
- Modify: `services/control-plane/internal/transport/http/server.go` (COORDINATOR)

- [ ] **Step 1: `handler/oauth.go`** — endpoints:

```go
func (h *OAuthHandler) Start(w http.ResponseWriter, r *http.Request)        { /* mint state, call provider, return {url} */ }
func (h *OAuthHandler) Callback(w http.ResponseWriter, r *http.Request)     { /* call provider.AuthenticateWithCode + JIT + mint session */ }
func (h *OAuthHandler) PortalLink(w http.ResponseWriter, r *http.Request)   { /* owner-only; returns a one-time WorkOS admin URL */ }
```

- [ ] **Step 2: `server.go` — mount under the public + protected groups**

```go
r.Route("/v1/auth/workos", func(r chi.Router) {
    r.Post("/start",         oauthHandler.Start)
    r.Get("/callback",       oauthHandler.Callback)
})

r.Group(func(r chi.Router) {
    r.Use(middleware.AuthRequired, middleware.RequireRole(domain.RoleOwner))
    r.Post("/v1/auth/workos/sso/portal_link", oauthHandler.PortalLink)
})
```

### Task 5.3: JIT provisioning usecase

**Files:**
- Create: `services/control-plane/internal/usecase/auth_jit_provision.go`

- [ ] **Step 1: `JITProvision` signature (algorithm below; full body straightforward Postgres tx)**

```go
type JITService struct { pool *pgxpool.Pool; audit domain.AuditWriter }
type JITInput  struct { WorkOSUserID, WorkOSOrgID, Email, Name, OrgName string }
type JITOutput struct { OrgID, UserID string; Created, HasWorkspace bool }

func (s *JITService) Provision(ctx context.Context, in JITInput) (JITOutput, error) {
    // 1. Open admin-pool tx (RLS off; this is a system path).
    // 2. SELECT users + organizations by workos_user_id / workos_org_id.
    // 3. INSERT missing rows; never UPDATE existing rows here (SCIM owns updates).
    // 4. INSERT org_members(role=owner if first member, else WorkOS role map).
    // 5. INSERT entitlements(status='trialing', stripe_customer_id=NULL) iff org is new.
    // 6. Audit 'auth.tenant_provisioned' with {org_id, user_id, workos_user_id, workos_org_id, source:"workos"}.
    // 7. HasWorkspace := SELECT EXISTS(SELECT 1 FROM workspaces WHERE org_id=$1).
}
```

### Task 5.4: WorkOS webhook (SCIM)

**Files:**
- Create: `services/control-plane/internal/transport/http/middleware/workos_signature.go`
- Modify: `services/control-plane/internal/transport/http/handler/webhooks.go` (this file is created in Stage 6 — coordinate file ownership; Stage 5 only adds the WorkOS branch)

- [ ] **Step 1: `middleware/workos_signature.go`** — read `WorkOS-Signature` header, verify HMAC-SHA256 against `cfg.WorkOSWebhookSecret`, replay protection via 5-minute timestamp window. Reject on mismatch with 403.

- [ ] **Step 2: In Stage 6's `webhooks.go`, the WorkOS handler dispatches to `workos.SCIM.Apply(ctx, event)`.**

### Task 5.5: Web — WorkOS sign-in route

**Files:**
- Create: `apps/web/app/auth/workos/callback/route.ts`
- Modify: `apps/web/app/auth/signin/page.tsx`
- Modify: `apps/web/app/auth/signup/page.tsx`
- Modify: `apps/web/lib/auth.ts`

- [ ] **Step 1: `route.ts`** — Next 16 route handler. Validates `state`, POSTs `{code, state}` to `${API}/v1/auth/workos/callback`, copies the `Set-Cookie` header from the control-plane response, redirects to the location the control-plane returns (302).

- [ ] **Step 2: `signin/page.tsx`** — when `NEXT_PUBLIC_AUTH_PROVIDER=workos`, render a single "Sign in with WorkOS" button that POSTs `/v1/auth/workos/start` (server action) and `window.location =` the returned `authorization_url`. When `=local`, the Phase 2 password form is still rendered.

- [ ] **Step 3: `signup/page.tsx`** — identical to signin; WorkOS hosted UI handles signup.

- [ ] **Step 4: `lib/auth.ts`** — adds `redirectToWorkOS(returnTo: string)` helper.

### Task 5.6: Commit Stage 5

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web              && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
git add services/control-plane/internal/adapter/auth/workos services/control-plane/internal/transport/http/handler/oauth.go services/control-plane/internal/transport/http/middleware/workos_signature.go services/control-plane/internal/usecase/auth_jit_provision.go apps/web/app/auth apps/web/lib/auth.ts
git commit -m "feat(auth): WorkOS provider + OAuth callback + JIT provisioning + web sign-in (stage 5)"
```

### Task 5.7: Coordinator merge — Wave-2 close-out

After Stages 3, 4, 5 all return green and have been committed in separate shards, the coordinator runs:

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/validator    && go build ./... && go test ./...
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web              && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
```

Any failure rolls back the offending shard's last commit and re-dispatches that single shard.

---

## Stage 6 — Stripe billing real body + webhook + usage pusher + UI plan switcher

> **Pattern D dispatch.** This stage is a full rewrite of the `billing/stripe/` subtree and is sequential against Wave-3 because `cmd/server/main.go` and `transport/http/server.go` are touched.

### Task 6.1: Stripe provider real body

**Files:**
- Modify: `services/control-plane/internal/adapter/billing/stripe/provider.go`
- Create: `services/control-plane/internal/adapter/billing/stripe/webhook.go`
- Create: `services/control-plane/internal/adapter/billing/stripe/usage_pusher.go`
- Create: `services/control-plane/internal/adapter/billing/stripe/provider_test.go`

- [ ] **Step 1: `provider.go` — rewrite (skeleton; see spec §8.2 + Stripe SDK docs for full params)**

```go
package stripe

import (
    "context"
    "github.com/stripe/stripe-go/v82"
    "github.com/stripe/stripe-go/v82/checkout/session"
    "github.com/stripe/stripe-go/v82/customer"
    "github.com/stripe/stripe-go/v82/billingportal"
    "nexis/control-plane/internal/domain"
    "nexis/control-plane/internal/platform/config"
)

type Provider struct { secretKey, webhookSecret, priceRuntime, priceEvents, priceTokens string }

func NewProvider(cfg *config.Config) *Provider {
    stripe.Key = cfg.StripeSecretKey
    return &Provider{cfg.StripeSecretKey, cfg.StripeWebhookSecret,
        cfg.StripePriceRuntime, cfg.StripePriceEvents, cfg.StripePriceTokens}
}

// CreateCustomer — customer.New with IdempotencyKey="org-"+OrgID, Metadata{org_id}.
// Checkout       — session.New, mode=subscription, 4 line items (plan + 3 metered prices).
// Portal         — billingportal.New, Customer + ReturnURL.
// Each returns the URL field of the response. Errors surface as fmt.Errorf("stripe %s: %w", op, err).
```

Full method bodies match the Stripe Go SDK examples; the only Nexis-specific logic is the `IdempotencyKey="org-"+OrgID` (so re-running `CreateCustomer` for the same org returns the same Stripe customer) and the four-line-item Checkout shape.

- [ ] **Step 2: `webhook.go` — signature verification + event router (skeleton)**

```go
type WebhookRouter struct {
    secret     string
    repo       domain.BillingRepo
    audit      domain.AuditWriter
    sse        domain.SSEBroker
    eventsRepo domain.StripeEventsRepo
}

func (w *WebhookRouter) Handle(rw http.ResponseWriter, r *http.Request) {
    body, _ := io.ReadAll(r.Body)
    event, err := webhook.ConstructEvent(body, r.Header.Get("Stripe-Signature"), w.secret)
    if err != nil { http.Error(rw, "invalid signature", 400); return }

    // Idempotency: RecordStripeEvent returns rows=0 on duplicate event id.
    rows, err := w.eventsRepo.RecordStripeEvent(r.Context(), event.ID, string(event.Type))
    if err != nil { http.Error(rw, "db", 500); return }
    if rows == 0 { rw.WriteHeader(200); return }

    switch event.Type {
    case stripeapi.EventTypeCheckoutSessionCompleted:    w.onCheckoutCompleted(r.Context(), event)
    case stripeapi.EventTypeCustomerSubscriptionUpdated,
         stripeapi.EventTypeCustomerSubscriptionDeleted: w.onSubscriptionChange(r.Context(), event)
    case stripeapi.EventTypeInvoicePaymentFailed:        w.onPaymentFailed(r.Context(), event)
    }
    rw.WriteHeader(200)
}
```

Per-event helpers do narrow updates: `customer.subscription.updated` sets `stripe_subscription_id`, `current_period_end`, `cancel_at_period_end`, `status` via `repo.UpdateEntitlementFromStripe`, then publishes an `entitlement_changed` SSE event on the org channel. See spec §8.4 for the full event → field mapping.

- [ ] **Step 3: `usage_pusher.go` — hourly cron (skeleton; full algorithm in spec §8.5)**

```go
type UsagePusher struct { repo domain.BillingRepo; audit domain.AuditWriter }

func (u *UsagePusher) Tick(ctx context.Context) error {
    // 1. ListUnpushedUsageRecords(ctx, 100)              — admin pool.
    // 2. Group by (org_id, kind); sum quantities.
    // 3. Resolve subscription_item_id from cache (refreshed on entitlement webhook).
    // 4. subscriptionitem.CreateUsage(subItemID, {Quantity, Timestamp, Action:increment}).
    // 5. On 2xx: MarkUsageRecordPushed for each row in the group.
    // 6. Audit 'billing.usage_pushed' with org_id, kind, quantity, stripe_usage_record_id.
    // On 4xx/5xx: log + retry on next tick (no state change).
}
```

Registered in `internal/platform/cron/cron.go` with `@every 1h`. Spec §8.5 covers the failure-mode retry budget (24 h before PagerDuty page).

- [ ] **Step 4: `provider_test.go`** — uses `stripe-mock` (`docker run -p 12111:12111 stripe/stripe-mock`). Tests: customer creation idempotency, checkout shape, webhook signature pass/fail, idempotency replay of the same event id.

### Task 6.2: Webhooks handler + Stripe signature middleware

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/webhooks.go`
- Create: `services/control-plane/internal/transport/http/middleware/stripe_signature.go`

- [ ] **Step 1: `webhooks.go`** — two handlers behind their respective middlewares:

```go
type WebhooksHandler struct {
    stripe *stripe.WebhookRouter
    workos *workos.WebhookRouter
}

func (h *WebhooksHandler) Stripe(rw http.ResponseWriter, r *http.Request) {
    h.stripe.Handle(rw, r)
}

func (h *WebhooksHandler) WorkOS(rw http.ResponseWriter, r *http.Request) {
    h.workos.Handle(rw, r)
}
```

- [ ] **Step 2: `middleware/stripe_signature.go`** — read `Stripe-Signature` header; verification itself happens inside `stripe.WebhookRouter.Handle` (the SDK reads the body), so this middleware only buffers the body for the SDK and adds a request timeout (10 s).

- [ ] **Step 3: Register routes in `server.go` (COORDINATOR)**

```go
r.Group(func(r chi.Router) {
    // Public, signature-gated.
    r.Use(middleware.StripeSignatureBuffer)
    r.Post("/v1/webhooks/stripe", webhooksHandler.Stripe)

    r.Use(middleware.WorkOSSignature)
    r.Post("/v1/webhooks/workos", webhooksHandler.WorkOS)
})
```

### Task 6.3: Billing handler additions

**Files:**
- Modify: `services/control-plane/internal/transport/http/handler/billing.go`

- [ ] **Step 1: Add three handlers**

```go
func (h *BillingHandler) Plans(w http.ResponseWriter, r *http.Request)        { /* returns the static plan catalog */ }
func (h *BillingHandler) Checkout(w http.ResponseWriter, r *http.Request)     { /* owner-only; calls Provider.Checkout */ }
func (h *BillingHandler) Portal(w http.ResponseWriter, r *http.Request)       { /* owner-only; calls Provider.Portal */ }
```

The Phase 3.5 `Entitlement` and `Usage` handlers stay; only the response shape on `Usage` gains `stripe_subscription_id` + `current_period_end`.

- [ ] **Step 2: Mount in `server.go`**

```go
r.Group(func(r chi.Router) {
    r.Use(middleware.AuthRequired, middleware.RequireRole(domain.RoleOwner))
    r.Post("/v1/billing/checkout", billingHandler.Checkout)
    r.Get( "/v1/billing/portal",   billingHandler.Portal)
})

r.Group(func(r chi.Router) {
    r.Use(middleware.AuthRequired)
    r.Get("/v1/billing/plans",       billingHandler.Plans)
    r.Get("/v1/billing/entitlement", billingHandler.Entitlement)   // unchanged
})
```

### Task 6.4: Entitlements usecase

**Files:**
- Create: `services/control-plane/internal/usecase/billing_entitlements.go`
- Create: `services/control-plane/internal/usecase/billing_usage_pusher.go`

- [ ] **Step 1: `billing_entitlements.go`** — applies the Phase 3.5 `entitlements` ledger writes from webhook events. Centralises the Stripe-event → row update mapping; the webhook router calls into it rather than touching the repo directly.

- [ ] **Step 2: `billing_usage_pusher.go`** — wraps the Phase 6.1 Step 3 `UsagePusher.Tick` for cron registration.

### Task 6.5: Web — plan switcher + portal link

**Files:**
- Modify: `apps/web/app/(app)/console/settings/billing/page.tsx`
- Create: `apps/web/app/(app)/console/settings/billing/client.tsx`
- Modify: `apps/web/lib/billing.ts`
- Create: `apps/web/components/billing/PlanSwitcher.tsx`
- Create: `apps/web/components/billing/PortalLink.tsx`

- [ ] **Step 1: `page.tsx`** — server component fetches the plan list, entitlement, usage. Passes to `client.tsx`.

- [ ] **Step 2: `client.tsx`** — uses Stripe.js for the Checkout redirect. On plan-switch click, POSTs `/v1/billing/checkout` with `{price_id: <plan>, success_url, cancel_url}` and `window.location = checkout_url`.

- [ ] **Step 3: `lib/billing.ts`** — extend with `checkout(planPriceID)`, `portal()` helpers.

- [ ] **Step 4: `PlanSwitcher.tsx`** — Tremor card grid; one card per plan (Starter, Team, Business). Shows feature matrix + price + "Current" pill when matching `entitlement.plan`.

- [ ] **Step 5: `PortalLink.tsx`** — single anchor that POSTs `/v1/billing/portal` and opens the returned URL.

### Task 6.6: Commit Stage 6

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web              && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
git add services/control-plane/internal/adapter/billing/stripe services/control-plane/internal/transport/http/handler/{webhooks,billing}.go services/control-plane/internal/transport/http/middleware/stripe_signature.go services/control-plane/internal/usecase/billing_*.go apps/web/app/(app)/console/settings/billing apps/web/lib/billing.ts apps/web/components/billing
git commit -m "feat(billing): Stripe provider + webhook + usage pusher + plan switcher UI (stage 6)"
```

---

## Stage 7 — Release pipeline (GHA + ECR + ArgoCD app-of-apps + Argo Rollouts)

### Task 7.1: ECR repositories via Terraform

**Files:**
- Create: `infra/terraform/modules/ecr-repo/{main,variables,outputs}.tf`
- Modify: `infra/terraform/envs/dev/main.tf` (add `module "ecr_*"` for each service)

- [ ] **Step 1: `ecr-repo/main.tf`**

```hcl
resource "aws_ecr_repository" "this" {
  name                 = "nexis/${var.service}"
  image_tag_mutability = "IMMUTABLE"
  image_scanning_configuration { scan_on_push = true }
  encryption_configuration { encryption_type = "AES256" }
  tags = merge(var.tags, { Name = "nexis-${var.service}-ecr" })
}

resource "aws_ecr_lifecycle_policy" "this" {
  repository = aws_ecr_repository.this.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "expire untagged > 7d"
      selection    = { tagStatus = "untagged", countType = "sinceImagePushed", countUnit = "days", countNumber = 7 }
      action       = { type = "expire" }
    }]
  })
}
```

Repos created per service: `nexis/control-plane`, `nexis/validator`, `nexis/gitops`, `nexis/web`, `nexis/causal-inference`.

### Task 7.2: GitHub Actions release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Full workflow**

```yaml
name: release
on: { push: { branches: [main] } }
permissions: { id-token: write, contents: write, packages: write }

jobs:
  build-and-push:
    runs-on: ubuntu-24.04
    strategy: { matrix: { service: [control-plane, validator, gitops, web] } }
    steps:
      - uses: actions/checkout@v4
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::${{ vars.AWS_MANAGEMENT_ACCOUNT_ID }}:role/github-actions-release
          aws-region: us-east-1
      - id: ecr
        uses: aws-actions/amazon-ecr-login@v2
      - name: Build & push
        env: { IMAGE: "${{ steps.ecr.outputs.registry }}/nexis/${{ matrix.service }}:${{ github.sha }}" }
        run: docker buildx build --platform linux/amd64 --tag "$IMAGE" --push services/${{ matrix.service }}

  bump-infra-deploy:
    needs: build-and-push
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
        with: { repository: nexis-ai/infra-deploy, token: "${{ secrets.INFRA_DEPLOY_PAT }}", ref: main }
      - env: { SHA: "${{ github.sha }}" }
        run: |
          for svc in control-plane validator gitops web; do
            yq -i ".image.tag = strenv(SHA)" "staging/${svc}/values.yaml"
          done
          git config user.email "ci@nexis.dev"; git config user.name "nexis-release-bot"
          git add staging; git commit -m "release: ${SHA}"; git push origin main
```

> **Note for reviewers:** the `infra-deploy` repo seed (the values.yaml skeletons and the ArgoCD app-of-apps manifests) is created by `scripts/seed-infra-deploy.sh` in Task 7.4. That script is run once by the operator after the GHA secret `INFRA_DEPLOY_PAT` is provisioned.

### Task 7.3: ArgoCD app-of-apps + Argo Rollouts

**Files:**
- Create: `infra/k8s/argocd/app-of-apps.yaml`
- Create: `infra/k8s/argocd/staging/{control-plane,validator,gitops,web}.yaml`
- Create: `infra/k8s/argocd/prod/{control-plane,validator,gitops,web}.yaml`
- Create: `infra/k8s/rollouts/{control-plane,validator,gitops,web}.yaml`
- Create: `infra/k8s/rollouts/README.md`

- [ ] **Step 1: `app-of-apps.yaml`** — single ArgoCD Application that points at `infra/k8s/argocd/{staging,prod}/`. Spec §3.4 shows the layout; the YAML mirrors the Argo standard pattern.

- [ ] **Step 2: per-env app manifests** — each application references the corresponding `infra-deploy/<env>/<service>/values.yaml` and an Argo Rollouts CR under `infra/k8s/rollouts/`.

> **Output guidance from the planning brief:** do NOT paste the full Argo Rollouts CR YAML here. Reviewers should treat it as a standard ECS Rollouts CR with strategy `blueGreen`, `analysis.templates: [latency-p95, error-rate]`, `analysis.successCondition: result[0] < 500 && result[1] < 0.01`, autoPromote false in prod / auto in staging. The README in `infra/k8s/rollouts/README.md` documents the exact knobs.

### Task 7.4: infra-deploy repo seed

**Files:**
- Create: `scripts/seed-infra-deploy.sh`

- [ ] **Step 1: `scripts/seed-infra-deploy.sh`** — clones `infra-deploy` (creates it via `gh repo create nexis-ai/infra-deploy --private` if absent), writes a `staging/<service>/values.yaml` and `prod/<service>/values.yaml` per service, commits, pushes.

Schema of `values.yaml`:

```yaml
image:
  repository: ${REGISTRY}/nexis/${SERVICE}
  tag:        v0.0.0-bootstrap
service:
  port: ${SVC_PORT}
env:
  ENV: ${ENV}
```

### Task 7.5: ECR cross-account pull policy

**Files:**
- Create: `infra/terraform/modules/ecr-repo/policy.tf`

Attaches a repository policy allow-listing the `staging` + `prod` task execution role ARNs. Risk 16.9 mitigation. Reference spec for exact policy — do not inline here.

### Task 7.6: Commit Stage 7

```bash
git add .github/workflows/release.yml infra/k8s infra/terraform/modules/ecr-repo scripts/seed-infra-deploy.sh
git commit -m "feat(release): GHA release pipeline + ECR repos + ArgoCD app-of-apps + Argo Rollouts (stage 7)"
```

---

## Stage 8 — SOC 2-lite: audit anchor cron + verify CLI + DR runbook + IR runbook + access reviews

### Task 8.1: Audit anchor usecase

**Files:**
- Create: `services/control-plane/internal/usecase/audit_anchor.go`
- Create: `services/control-plane/internal/usecase/audit_anchor_test.go`

- [ ] **Step 1: `audit_anchor.go`** — daily anchor cron (skeleton)

```go
type AnchorService struct {
    audit   domain.AuditWriter
    anchors domain.AuditAnchorRepo
    auditDB domain.AuditLogRepo
    s3      domain.PatchStore         // reuses PatchStore port for S3 writes
    bucket  string
}

func (s *AnchorService) Anchor(ctx context.Context, period time.Time) error {
    last, _ := s.anchors.LastAuditAnchor(ctx)
    from := time.Time{}
    if last.ID != "" { from = last.PeriodEnd }

    rows, err := s.auditDB.ListAuditLogBetween(ctx, from, period)
    if err != nil { return err }
    if len(rows) == 0 { return nil }

    // 1. Merkle root over rows[].HMAC.
    leaves := make([][32]byte, len(rows))
    for i, r := range rows { leaves[i] = sha256.Sum256(r.HMAC) }
    rootHex := hex.EncodeToString(merkleRoot(leaves)[:])

    // 2. Upload gzipped CSV snapshot to S3 (Object Lock COMPLIANCE).
    key := fmt.Sprintf("snapshots/%s.csv.gz", period.Format("2006-01-02"))
    if err := s.s3.Put(ctx, key, gzip(marshalCSV(rows))); err != nil { return err }
    versionID := s.s3.(domain.VersionedPatchStore).LastVersionID()

    // 3. Insert anchor row + audit.
    _, err = s.anchors.InsertAuditAnchor(ctx, domain.AuditAnchor{
        PeriodStart: from, PeriodEnd: period, RowCount: int64(len(rows)),
        MerkleRoot: rootHex, PrevRoot: last.MerkleRoot,
        S3ObjectKey: key, S3VersionID: versionID,
    })
    if err != nil { return err }
    _ = s.audit.Write(ctx, "audit.anchor_written", map[string]any{
        "period_start": from, "period_end": period, "row_count": len(rows),
        "merkle_root_hex": rootHex, "s3_object_key": key, "s3_version_id": versionID,
    })
    return nil
}
```

`merkleRoot`, `marshalCSV`, `gzip` are private helpers in the same file. The S3 store gains a `LastVersionID()` getter on a small `VersionedPatchStore` interface — MinIO returns empty string, S3 returns the PutObject response version id.

- [ ] **Step 2: Register the cron in `internal/platform/cron/cron.go`** — `@daily at 02:00 UTC` calls `AnchorService.Anchor(ctx, time.Now().Truncate(24h).Add(-1*time.Hour))`. The cron is registered only when `cfg.AuditAnchorBucket != ""`.

### Task 8.2: `nex audit verify` CLI

**Files:**
- Create: `services/control-plane/cmd/nex/audit_verify.go`
- Modify: `services/control-plane/cmd/nex/main.go` (existing CLI entry — extend with the subcommand)

- [ ] **Step 1: `audit_verify.go`** — subcommand `nex audit verify --since <iso>` (skeleton)

```go
func runVerify(ctx context.Context, sinceISO string) error {
    since, _ := time.Parse(time.RFC3339, sinceISO)
    anchors, err := repo.ListAuditAnchorsSince(ctx, since)
    if err != nil { return err }
    var prev string
    for _, a := range anchors {
        if a.PrevRoot != prev { return fmt.Errorf("chain break at %s", a.PeriodEnd) }
        rows, _ := auditDB.ListAuditLogBetween(ctx, a.PeriodStart, a.PeriodEnd)
        leaves := make([][32]byte, len(rows))
        for i, r := range rows { leaves[i] = sha256.Sum256(r.HMAC) }
        got := hex.EncodeToString(merkleRoot(leaves)[:])
        if got != a.MerkleRoot { return fmt.Errorf("merkle mismatch at %s: got=%s want=%s", a.PeriodEnd, got, a.MerkleRoot) }
        prev = a.MerkleRoot
    }
    fmt.Println("audit verify OK:", len(anchors), "anchors")
    return nil
}
```

Exit 0 on success, 1 on mismatch.

### Task 8.3: DR runbook

**Files:**
- Create: `docs/runbooks/dr-drill.md`

Documents the staging restore drill executed at end of Phase 7. Sections: prerequisites (admin AWS creds in staging account), step-by-step (snapshot id, restore command, target instance `staging-restore`, password rotation, RLS verification, application connectivity test from a temp pod), pass criteria (a known row checksum matches), tear-down. Acceptance criterion 13 references this file.

### Task 8.4: Incident-response runbook

**Files:**
- Create: `docs/runbooks/incident-response.md`

Sections: severity definitions (Sev1/2/3/4 per the spec), paging policy, communication templates (status-page update, customer email), forensics checklist (CloudTrail snapshot, log preservation, KMS access review, SCIM directory snapshot), post-mortem template (5-whys + remediation owners + timeline). Cross-references the `cert-rotation` and `access-review` runbooks.

### Task 8.5: Access reviews

**Files:**
- Create: `docs/runbooks/access-review.md`
- Create: `services/control-plane/cmd/nex/iam_review.go`

- [ ] **Step 1: `iam_review.go`** — enumerates IAM users + roles + their attached policies in a given account, writes a CSV with columns `principal,type,policy_arn,actions,resources,last_used`. Used quarterly per the spec.

- [ ] **Step 2: `access-review.md`** — operator runbook for the quarterly review meeting; references the `nex iam-review --account=prod` command output.

### Task 8.6: IAM hardening per service

**Files:**
- Modify: `infra/terraform/envs/dev/main.tf` (and staging/prod when those env-roots ship)

- [ ] **Step 1: Per-service policy attachments use the module's `policies` variable**

```hcl
module "iam_control_plane" {
  source   = "../../modules/iam-task-role"
  env      = var.env
  service  = "control-plane"
  policies = [
    { name = "kms",     document = data.aws_iam_policy_document.kms.json },
    { name = "s3",      document = data.aws_iam_policy_document.s3.json },
    { name = "secrets", document = data.aws_iam_policy_document.secrets.json },
    { name = "ses",     document = data.aws_iam_policy_document.ses.json },
  ]
  tags = var.tags
}
```

The policy JSON documents are built from `data "aws_iam_policy_document"` blocks. Reference spec Appendix C for the exact actions/resources per service — do not inline the JSON here.

### Task 8.7: Commit Stage 8

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
git add services/control-plane/internal/usecase/audit_anchor.go services/control-plane/cmd/nex docs/runbooks infra/terraform/envs/dev/main.tf
git commit -m "feat(soc2): audit anchor cron + nex audit verify CLI + DR/IR/access-review runbooks (stage 8)"
```

---

## Stage 9 — Staging env-root + cutover dry-run + Phase 1–6 acceptance against AWS

### Task 9.1: staging env-root

**Files:**
- Create: `infra/terraform/envs/staging/{backend,main,variables,outputs}.tf`
- Create: `infra/terraform/envs/staging/terraform.tfvars`

- [ ] **Step 1: Mirror the dev env-root structure**

`backend.tf` uses key `envs/staging/terraform.tfstate`. `main.tf` composes the same modules as dev, plus the real services (control-plane / validator / gitops / web) — not the nginx hello-world.

- [ ] **Step 2: `terraform.tfvars` (staging)** — `env="staging", region="us-east-1", vpc_cidr="10.20.0.0/16", az_count=3, nat_gateway_count=3, rds_instance_class="db.m6g.large", rds_multi_az=true, rds_storage_gb=200, domain_name="staging.nexis.dev", acm_cert_san_wildcard=true, audit_object_lock_years=7, tags={Environment="staging", ManagedBy="terraform"}`.

- [ ] **Step 3: Apply**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/infra/terraform/envs/staging
terraform init
terraform plan -out=tfplan
terraform apply tfplan
```

Expected: full stack stood up. Migrations applied via the bootstrap ECS Task. ALB DNS resolves and serves a 503 from the not-yet-deployed services. First release pipeline run from `main` brings them up.

### Task 9.2: Cutover dry-run

**Files:**
- Modify: `services/control-plane/.env.cloud.example` (reference)
- Modify: env vars set per Stage 0 (no new files)

- [ ] **Step 1: Set every env-flag selector to the cloud value in staging**

```bash
ENV=staging
AUTH_PROVIDER=workos BILLING_PROVIDER=stripe PATCH_STORE=s3 KEYVAULT=kms \
SECRETS=secretsmanager MAILER=resend GRAPH_PROVIDER=aura VALIDATOR_RUNNER=modal \
TEMPORAL_CLOUD=1 OTLP_TARGET=grafana-cloud
```

- [ ] **Step 2: Verify the startup assertion green-lights**

The control-plane ECS task definition reads these from Secrets Manager + plain env. The first task that starts must NOT exit 2.

### Task 9.3: e2e-cloud workflow

**Files:**
- Create: `.github/workflows/e2e-cloud.yml`

- [ ] **Step 1: Workflow body**

```yaml
name: e2e-cloud
on:
  workflow_dispatch:
  schedule:
    - cron: "0 6 * * *"   # daily at 06:00 UTC

jobs:
  e2e-staging:
    runs-on: ubuntu-24.04
    environment: staging
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.25" }
      - uses: pnpm/action-setup@v4
        with: { version: 10 }

      - name: Go integration tests against staging
        env:
          STAGING_API_URL: https://api.staging.nexis.dev
        run: |
          cd services/control-plane
          go test -tags=e2e_cloud ./internal/integration/...

      - name: Playwright vs staging
        env:
          STAGING_WEB_URL: https://app.staging.nexis.dev
        run: |
          cd apps/web
          pnpm install --frozen-lockfile
          pnpm exec playwright install
          pnpm exec playwright test --grep @cloud
```

The `@cloud` tag is added in Phase 7 to the existing Playwright suites that exercise the WorkOS + Stripe webhook + Modal validator paths.

### Task 9.4: Run the suite

```bash
gh workflow run e2e-cloud.yml --ref main
gh run watch  # block until done; expect green
```

Acceptance criterion 1 (cutover contract) is the green run.

### Task 9.5: Commit Stage 9

```bash
git add infra/terraform/envs/staging .github/workflows/e2e-cloud.yml
git commit -m "feat(staging): staging terraform env-root + e2e-cloud GHA workflow (stage 9)"
```

---

## Stage 10 — k6 load test + RLS probe + report

### Task 10.1: k6 recovery-pipeline script

**Files:**
- Create: `tests/load/recovery_pipeline.js`
- Create: `tests/load/fixtures/incidents/{null-pointer,oom,schema-drift,trivial-ui-fix}.json`

- [ ] **Step 1: `recovery_pipeline.js` (k6, skeleton)**

```javascript
import http from 'k6/http'; import { check, sleep } from 'k6'; import { Rate, Trend } from 'k6/metrics';

export const options = {
  vus: 100, duration: '30m',
  thresholds: {
    'pipeline_wallclock_p95': ['p(95)<480000'], 'http_req_failed': ['rate<0.01'],
    'rls_violations': ['count==0'], 'stripe_webhook_latency': ['med<30000'],
  },
};
const wallclock = new Trend('pipeline_wallclock_p95');
const FIXTURES = ['null-pointer','oom','schema-drift','trivial-ui-fix'];
const SESSION_POOL = JSON.parse(open('./fixtures/sessions.json'));

export default function () {
  const session = SESSION_POOL[__VU % SESSION_POOL.length];
  const label = FIXTURES[Math.floor(Math.random() * FIXTURES.length)];
  const start = Date.now();
  const trigger = http.post(`${__ENV.STAGING_API}/v1/admin/sentinel/trigger`,
    JSON.stringify({ label }),
    { headers: { 'Cookie': `nexis_session=${session.token}`, 'Content-Type': 'application/json' } });
  check(trigger, { 'trigger 202': r => r.status === 202 });
  const runId = JSON.parse(trigger.body).run_id;

  let status = 'running';
  while (status === 'running' && Date.now() - start < 8 * 60 * 1000) {
    sleep(5);
    const res = http.get(`${__ENV.STAGING_API}/v1/workspaces/${session.ws}/pipelines/${runId}/status`,
      { headers: { 'Cookie': `nexis_session=${session.token}` } });
    status = JSON.parse(res.body).status;
  }
  wallclock.add(Date.now() - start);
  check({ status }, { 'pr opened': r => r.status === 'succeeded' });
}
```

- [ ] **Step 2: RLS probe** — a separate k6 scenario running 1 VU at 0.1 rps that hits both orgs' workspace endpoints with each other's session token and asserts 404. Every 200 increments `rls_violations`.

### Task 10.2: Pre-bake the session pool

**Files:**
- Create: `tests/load/seed/seed-sessions.go`

A Go script that signs up 100 test users via the WorkOS test environment (using the WorkOS Admin API) and serialises `{user_id, ws_id, session_token}` tuples to `tests/load/fixtures/sessions.json`. Run once before the load test.

### Task 10.3: Run + report

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/tests/load
go run ./seed/seed-sessions.go --count=100 --env=staging > fixtures/sessions.json
k6 run --out json=results.json --env STAGING_API=https://api.staging.nexis.dev recovery_pipeline.js
```

Generate `docs/load-test/2026-W21-report.md` from the k6 metrics. Pass criteria from spec §11 verified before the file is committed.

### Task 10.4: Commit Stage 10

```bash
git add tests/load docs/load-test
git commit -m "feat(loadtest): k6 100-VU recovery pipeline + RLS probe + W21 report (stage 10)"
```

---

## Stage 11 — Pen-test fix list + security review pass

### Task 11.1: Engage pen-test contractor (operator)

Out of scope for the plan — procurement happens outside the repo. The contractor delivers a findings doc; the engineering deliverable is the remediation patches.

### Task 11.2: Remediation tracker

**Files:**
- Create: `docs/security/pentest-fixes-2026-Q4.md`

Schema:

```markdown
| ID  | Category   | Severity | Finding                                                | Fix PR    | Status |
|-----|------------|----------|--------------------------------------------------------|-----------|--------|
| F-1 | Auth       | High     | Session cookie missing __Host- prefix in dev           | #421      | merged |
| F-2 | RLS        | Medium   | Cross-tenant patch presign URL leaks bucket prefix     | #422      | merged |
| ... | ...        | ...      | ...                                                    | ...       | ...    |
```

Acceptance criterion 14: no Critical/High remains open; every Medium has a linked remediation issue.

### Task 11.3: Security review pass

Run the `security-review` skill against the entire Phase 7 diff (`git log main --since="3 weeks ago"`). Outputs are added to `docs/security/pentest-fixes-2026-Q4.md` under a "Pre-pen-test internal review" section.

### Task 11.4: Commit Stage 11

```bash
git add docs/security
git commit -m "docs(security): pen-test fix tracker + internal security review pass (stage 11)"
```

Subsequent fix PRs per finding follow individually; each has its own commit `fix(security): close F-NN <short desc>`.

---

## Stage 12 — Prod env-root + DNS cutover + DoD

### Task 12.1: prod env-root

**Files:**
- Create: `infra/terraform/envs/prod/{backend,main,variables,outputs}.tf`
- Create: `infra/terraform/envs/prod/terraform.tfvars`

- [ ] **Step 1: Mirror staging with prod values** — `terraform.tfvars`: `env="prod", region="us-east-1", vpc_cidr="10.30.0.0/16", az_count=3, nat_gateway_count=3, rds_instance_class="db.m6g.large", rds_multi_az=true, rds_storage_gb=500, domain_name="nexis.dev", acm_cert_san_wildcard=true, audit_object_lock_years=7, tags={Environment="prod", ManagedBy="terraform"}`.

- [ ] **Step 2: Manual-gated apply via GHA**

A GHA workflow `.github/workflows/terraform-prod.yml` with `environment: prod` and a required reviewer runs `terraform plan`; merging the artefact triggers `terraform apply`. Reviewers required: 2 of {`@nexis-ai/tech-lead`, `@nexis-ai/secops`}.

### Task 12.2: DNS cutover

**Files:**
- Create: `docs/runbooks/cutover-2026-W21.md`

- [ ] **Step 1: Cutover steps documented and executed**

1. Confirm staging acceptance (Stages 9-11) all green.
2. Final RDS snapshot of staging.
3. Seed prod RDS from the bootstrap migration ECS task (empty schema, no data).
4. Seed Stripe + WorkOS prod credentials into prod Secrets Manager.
5. Deploy services via ArgoCD prod app-of-apps (manual approval).
6. Smoke-test prod end-to-end with a no-billing throwaway org.
7. Update Route53 A/AAAA records for `nexis.dev` → prod ALB.
8. Keep the old compose host warm at `compose.nexis.dev` for 7 days as rollback.
9. Verify post-cutover SLOs (Grafana Cloud) for 24 h.
10. Tag the release `v0.7.0` on main.

Rollback: re-point Route53 back to the compose host (recorded in `cutover-2026-W21.md`).

### Task 12.3: Mark Phase 7 complete

```bash
# Append "— Completed 2026-MM-DD" to the Phase 7 heading in PROJECT_PLAN.md
git add docs/PROJECT_PLAN.md docs/runbooks/cutover-2026-W21.md infra/terraform/envs/prod
git commit -m "feat(prod): prod terraform env-root + DNS cutover + phase 7 complete (stage 12)"
```

---

## Definition of Done

The DoD mirrors spec §13 acceptance criteria 1-17 verbatim, with one checkbox per criterion. The plan executor must tick every box before declaring Phase 7 complete.

- [ ] **AC1 — Cutover contract.** `e2e-cloud.yml` green against `staging.nexis.dev`; Playwright + Go integration suites pass; zero failures.
- [ ] **AC2 — Terraform apply from zero.** `cd infra/terraform/envs/staging && terraform init && terraform apply` against an empty AWS account produces a working stack in < 30 minutes. Same for `envs/prod`. Documented in `infra/terraform/README.md`.
- [ ] **AC3 — WorkOS sign-in E2E.** Test user signs in via the WorkOS hosted UI; lands on `/console`; creates a workspace. `audit_log` shows `auth.tenant_provisioned` with `source: "workos"`.
- [ ] **AC4 — Stripe entitlement under 30 s.** `POST /v1/billing/checkout` → `success_url` → webhook → entitlement updated → console reflects, all within 30 s. Measured from webhook receipt to UI SSE event.
- [ ] **AC5 — KMSVault round-trip.** Encrypt + decrypt a 1 MB blob; bytes match. CloudTrail shows `kms:Encrypt` + `kms:Decrypt` with `EncryptionContext={org_id, secret_kind}`.
- [ ] **AC6 — Cross-tenant KMS refusal.** A row sealed with `org_id=A` is rejected when decrypted under `org_id=B`. The integration test asserts the 403 surfaces as `domain.ErrCrossTenantDecryptRefused`. Audit row `kms.refused` written.
- [ ] **AC7 — Modal validator sandbox.** `VALIDATOR_RUNNER=modal`; fixture-null-pointer recovery pipeline completes; validator activity payload shows `tests_passed=true` and `runner: "modal"`.
- [ ] **AC8 — Temporal Cloud connectivity.** `TEMPORAL_CLOUD=1`; control-plane connects to `nexis-prod.tmprl.cloud:7233` over mTLS; recovery workflow runs and completes.
- [ ] **AC9 — Grafana Cloud traces.** Workflow runs produce traces in Grafana Cloud Tempo with the right service.name + tenant attribute. Phase 6 dashboards render against the cloud data.
- [ ] **AC10 — ArgoCD blue/green.** Merge-to-main builds + pushes to ECR + bumps `infra-deploy/staging/<svc>/values.yaml` + ArgoCD syncs + Argo Rollouts analysis passes; new task set becomes active. Manual rollback by reverting the values.yaml bump works.
- [ ] **AC11 — k6 load test.** Per §11: p95 < 8 min, 0 RLS violations, < 1% error rate, Stripe webhook median < 30 s. Report committed to `docs/load-test/2026-W21-report.md`.
- [ ] **AC12 — Audit anchor verifies.** `nex audit verify --since 2026-04-01` exits 0 against staging. A deliberate tampered row in a side branch makes it exit 1.
- [ ] **AC13 — Backup + restore drill.** `docs/runbooks/dr-drill.md` executed once; RDS snapshot restored to `staging-restore`; known-row checksum matches.
- [ ] **AC14 — Pen-test fix list shipped.** `docs/security/pentest-fixes-2026-Q4.md` exists; zero open Critical/High; every Medium has a linked remediation issue or merged PR.
- [ ] **AC15 — WAF blocks SQLi.** `curl 'https://api.staging.nexis.dev/?q=1%27+OR+1%3D1--'` returns 403 with the WAF response signature.
- [ ] **AC16 — Migrations idempotent.** Running the migration ECS Task twice in a row exits 0 the second time without applying anything.
- [ ] **AC17 — Build + test + lint green.** `make build && make test && make vet && make arch` on control-plane; `pnpm typecheck && pnpm build` on web; `go build ./... && go test ./...` on validator and gitops; all service Dockerfiles `docker buildx build` green; `terraform validate` green for dev|staging|prod; `tflint` green; `tfsec` no high findings.

Additional plan-level DoD:

- [ ] Every Phase 1–6 acceptance suite passes verbatim against staging (the cutover contract).
- [ ] Startup assertion `FatalIfLocalInCloud` fatals the staging task when any selector is `local`; verified once via a fault-injection workflow.
- [ ] Compose host kept warm at `compose.nexis.dev` for 7 days post-cutover; rollback runbook documented.
- [ ] `docs/PROJECT_PLAN.md` Phase 7 heading carries a completion date.

---

## Risk register (cross-references spec §16)

The full risks table is in spec §16.1 – §16.16. The plan-executor must verify each mitigation has a concrete artefact in the repo before closing the phase.

| Risk (spec ref) | Mitigation artefact (must exist) | Detection signal |
|---|---|---|
| 16.1 Argo Rollouts ECS maturity | `infra/k8s/rollouts/README.md` documents blue/green primary; canary deferred to Phase 8 | Promotion analysis green in staging |
| 16.2 Modal cold start | `MODAL_KEEP_WARM=1` set in prod env via Secrets Manager; cost noted | p95 latency Grafana dashboard |
| 16.3 Stripe webhook retry storm | `stripe_events_processed` idempotency table + handler short-circuit | Duplicate event id repeat count == 0 |
| 16.4 WorkOS SCIM deletion | `internal/adapter/auth/workos/scim.go` does soft-delete only; restore window 30 days | No hard-delete on `dsync.user.deleted` |
| 16.5 KMS request rate limits | Envelope encryption pattern (per-write DEK only); patch store batches DEKs per `(org_id, run_id)` | CloudWatch `kms:ThrottleException` metric stays at zero |
| 16.6 Temporal Cloud cost | Phase 5 budget cap doubles as Temporal-action cap; dashboard alert at 70% of monthly threshold | Grafana panel: temporal actions/min per tenant |
| 16.7 Cutover blast radius | Stage 9 runs full Phase 1–6 acceptance suite against staging first; Stage 12 keeps compose warm 7 days | Rollback drill documented in `cutover-2026-W21.md` |
| 16.8 Fargate ephemeral storage | Modal runner takes over the patch-apply path in prod; validator container is HTTP-only | `df -h /tmp` in validator task < 50% always |
| 16.9 Cross-account ECR pulls | ECR repo policy allow-lists staging+prod execution role ARNs; integration test in Stage 7 | `aws ecr describe-image` from each account succeeds |
| 16.10 ALB cert rotation | Terraform pins DNS validation records; weekly CloudWatch alarm on cert expiry < 30 days | Alarm `cert-expiry-warn` armed |
| 16.11 Stripe-mock fidelity | Contract tests run against the real Stripe test mode in e2e-cloud; mock-only tests flagged | Contract test green in `e2e-cloud.yml` |
| 16.12 WorkOS dev rate limits | Tests reuse pre-baked session pool; e2e suite serialises sign-in scenarios | `tests/load/fixtures/sessions.json` exists |
| 16.13 Audit anchor lag | Cron idempotent on `period_end`; backfill on recovery; verify CLI handles gaps | `nex audit verify` succeeds across multi-day backfill |
| 16.14 Path-of-least-resistance local bypass | Startup assertion `FatalIfLocalInCloud` in `cmd/server/main.go` (Stage 0) | Process exits 2 in staging if any selector is local |
| 16.15 Service Connect TLS | Per-env integration test asserts wire encrypted via packet capture sidecar (Stage 9); fallback path through ALB | Sidecar reports `tls_observed=true` |
| 16.16 PagerDuty + Datadog optional | Adapters slip to Phase 8 if schedule pressure; owners documented in `docs/runbooks/incident-response.md` | Both adapters absent without blocking the cutover |

---

## Appendix A: File-create / file-modify roster (by stage)

Every path is rooted at `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/`. Per-stage owner is the coordinator unless listed otherwise.

- **Stage 0** (coordinator): `services/control-plane/migrations/0019..0021_phase7_*.{up,down}.sql`; `services/control-plane/internal/adapter/repo/queries/{audit_anchors,billing}.sql`; `services/control-plane/internal/platform/config/config.go`; `services/control-plane/cmd/server/main.go`; `packages/db/schema.ts`; `services/control-plane/.env.cloud.example`.
- **Stage 1** (infra): `infra/terraform/{bootstrap.sh,README.md}`; `infra/terraform/modules/{vpc,kms-key,s3-bucket,secrets-manager-secret,route53-zone,cloudwatch-logs,iam-task-role}/{main,variables,outputs}.tf`.
- **Stage 2** (infra): `infra/terraform/modules/{rds,redis,alb,waf,ecs-cluster,ecs-service}/{main,variables,outputs}.tf`; `infra/terraform/envs/dev/{backend,main,variables,outputs,terraform.tfvars}.tf`.
- **Stage 3** — 3.A: `internal/platform/aws/{config,kms,secrets}.go` + `internal/adapter/keyvault/{kms,kms_test,factory}.go` + `internal/domain/secrets.go` + `internal/adapter/secrets/{secretsmanager,local,factory,secretsmanager_test}.go`. 3.B: `internal/platform/aws/{s3,ses}.go` + `internal/adapter/patchstore/s3/{store,store_test}.go` + `internal/adapter/mailer/{resend,awsses,factory}.go`.
- **Stage 4** — 4.A: `services/validator/internal/runner/{runner,docker,modal,factory}.go` + `services/validator/internal/sandbox/docker.go` (alias) + `services/validator/modal/{nexis_validator.py,Dockerfile.modal,deploy.sh,README.md}` + `services/control-plane/internal/platform/{temporal/client.go,otel/otel.go}` + `internal/adapter/graphstore/{neo4j/aura.go,factory.go}`.
- **Stage 5** — 5.A: `internal/adapter/auth/workos/{provider,callback,scim,role_map,provider_test}.go` + `internal/adapter/auth/factory.go` + `internal/transport/http/{handler/oauth,middleware/workos_signature}.go` + `internal/usecase/auth_jit_provision.go` + `apps/web/app/auth/workos/callback/route.ts` + `apps/web/app/auth/{signin,signup}/page.tsx` + `apps/web/lib/auth.ts`.
- **Stage 6** — 6.A: `internal/adapter/billing/stripe/{provider,webhook,usage_pusher,provider_test}.go` + `internal/transport/http/{handler/{webhooks,billing},middleware/stripe_signature}.go` + `internal/usecase/billing_{entitlements,usage_pusher}.go`. 7.C: `apps/web/app/(app)/console/settings/billing/{page,client}.tsx` + `apps/web/lib/billing.ts` + `apps/web/components/billing/{PlanSwitcher,PortalLink}.tsx`.
- **Stage 7** — shard 7.A: `.github/workflows/release.yml`; `infra/k8s/argocd/{app-of-apps,staging/*,prod/*}.yaml`; `infra/k8s/rollouts/{control-plane,validator,gitops,web,README}.{yaml,md}`; `infra/terraform/modules/ecr-repo/{main,variables,outputs,policy}.tf`; `scripts/seed-infra-deploy.sh`.
- **Stage 8** — shard 7.B: `internal/usecase/audit_anchor.go`; `services/control-plane/cmd/nex/{audit_verify,iam_review}.go`; `docs/runbooks/{dr-drill,incident-response,access-review,cert-rotation}.md`.
- **Stage 9** (coordinator): `infra/terraform/envs/staging/{backend,main,variables,outputs,terraform.tfvars}.tf`; `.github/workflows/e2e-cloud.yml`.
- **Stage 10** (coordinator): `tests/load/{recovery_pipeline.js,fixtures/incidents/*.json,seed/seed-sessions.go}`; `docs/load-test/2026-W21-report.md`.
- **Stage 11** (coordinator): `docs/security/pentest-fixes-2026-Q4.md`; per-finding fix PRs scattered across the tree.
- **Stage 12** (coordinator): `infra/terraform/envs/prod/{backend,main,variables,outputs,terraform.tfvars}.tf`; `docs/runbooks/cutover-2026-W21.md`; `.github/workflows/terraform-prod.yml`; `docs/PROJECT_PLAN.md`.

---

## Appendix B: Verification commands per stage

- **Stage 0:** `cd services/control-plane && make migrate && make build && make test && make vet && make arch`.
- **Stage 1:** `for m in vpc kms-key s3-bucket secrets-manager-secret route53-zone cloudwatch-logs iam-task-role; do (cd infra/terraform/modules/$m && terraform init -backend=false && terraform validate); done`.
- **Stage 2:** `cd infra/terraform/envs/dev && terraform init && terraform apply -auto-approve && curl -fsS "https://$(terraform output -raw alb_dns_name)/" | head -5`.
- **Stage 3:** coordinator merge + `go test -tags=integration ./internal/adapter/{keyvault,patchstore,mailer,secrets}/...`.
- **Stage 4:** coordinator merge + `cd services/validator && go test ./...`.
- **Stage 5–6:** coordinator merge + `go test -tags=integration ./internal/adapter/{auth/workos,billing/stripe}/...`.
- **Stage 7:** `gh workflow run release.yml --ref main && gh run watch`.
- **Stage 8:** `go test ./internal/usecase/audit_anchor*  && go run ./cmd/nex audit verify --since 2026-01-01`.
- **Stage 9:** `gh workflow run e2e-cloud.yml --ref main && gh run watch`.
- **Stage 10:** `k6 run --env STAGING_API=https://api.staging.nexis.dev tests/load/recovery_pipeline.js`.
- **Stage 11:** manual pen-test + `pnpm exec playwright test --grep @security`.
- **Stage 12:** `cd infra/terraform/envs/prod && terraform plan -out=tfplan && terraform apply tfplan` (manual-gated).

---

## Appendix C: Coordinator merge checklist (run after every shard)

```bash
cd services/control-plane && make build && make test && make vet && make arch
cd services/validator     && go build ./... && go test ./...
cd services/gitops        && go build ./... && go test ./...
cd apps/web               && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
cd infra/terraform/envs/dev && terraform init -backend=false && terraform validate
tflint --recursive infra/terraform/ && tfsec infra/terraform/
```

Any single command failing rolls back the offending shard's last commit and re-dispatches just that shard with the error transcript.

---

## Appendix D: Cross-references to spec (where the plan does NOT inline content)

- IAM policies per service → spec Appendix C + §9.5.
- ALB + WAF managed rule set → spec §5.3 (`waf` row).
- Modal `nexis_validator.py` Python body → spec §6.6.
- WorkOS sign-in / SCIM / role map full flow → spec §7.1–§7.6.
- Stripe price catalog + webhook event table → spec §8.1 + §8.4.
- Argo Rollouts CR YAML → spec §3.4 + §16.1.
- Audit anchor S3 Object Lock retention → spec §9.1.
- k6 pass criteria → spec §11. Pen-test scope → spec §10. Cutover order → spec Appendix F.
- Env-var canonical list → spec Appendix A. Risk register full bodies → spec §16.1–§16.16.

---

## Appendix E: Sequencing summary

```
Stage 0 (coordinator) → Wave 1 (Stages 1-2: Terraform IaC) → Wave 2 (Stages 3-5, Pattern C, 4 shards parallel: KMS/Secrets+S3/Mailer+Modal/Temporal/OTLP/Aura+WorkOS) → Wave 3 (Stage 6 Stripe sequential, then Stages 7+8 parallel: release pipeline / SOC2-lite) → Sequential cutover (Stages 9 staging+e2e / 10 k6 / 11 pen-test / 12 prod+DNS+DoD)
```

Total stages: 13. Parallel shards: 7. Sequential stages: 0, 1, 2, 6 (Stripe), 9–12.

---

## Appendix F: Operator pre-flight (one-shot, manual; out of scope for plan executor)

Before any agent dispatches, the operator must complete:

1. **AWS Organization** — `aws organizations create-account` for `nexis-{mgmt,dev,staging,prod}`.
2. **Terraform state** — `./infra/terraform/bootstrap.sh --aws-account-id <mgmt> --state-bucket-name nexis-terraform-state --lock-table-name nexis-terraform-locks`.
3. **Secrets Manager** — `aws secretsmanager create-secret --name nexis-{env}/control-plane/{stripe-keys,workos-keys,db-url,temporal-cert}` per env.
4. **WorkOS** — dashboard.workos.com → create app → copy `client_id`, `api_key`, `webhook_secret` to Secrets Manager.
5. **Stripe** — dashboard.stripe.com → create product "NEXIS Recovery" + three metered prices (runtime/events/tokens) → copy `webhook_secret`.
6. **Modal** — `modal token new` + `modal deploy services/validator/modal/nexis_validator.py`.
7. **Grafana Cloud** — create stack → grab Instance ID + API token → store in Secrets Manager.

The plan assumes the above are done before Stage 0 begins.

---

## Appendix G: Phase 8 hand-off

Phase 7 explicitly does not finish (Phase 8 picks up): multi-region failover (Phase 9+); custom domains per tenant (docs Phase 8, impl Phase 9); real-time Stripe push (Phase 7 ships hourly); SOC 2 Type II auditor engagement (Phase 7 ships evidence + controls only); Sentinel anomaly-driven (Grafana Cloud anomaly hook); PagerDuty + Datadog adapters (if schedule slips); public beta launch + docs site (see `2026-05-13-phase-8-public-beta.md`); compose-path deprecation in CI (compose-build stays, compose-e2e drops).
