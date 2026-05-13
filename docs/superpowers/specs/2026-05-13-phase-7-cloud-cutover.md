# Phase 7 — Cloud Cutover + Hardening + Billing — Design

**Date:** 2026-05-13
**Phase:** 7 (Weeks 19–21 per `docs/PROJECT_PLAN.md`). The cloud cutover phase — local Docker Compose dies, AWS goes live, and every stubbed external integration becomes real. End of Phase 7 = paid plans, SOC 2-lite controls, pen-test fix list shipped, k6 load test passing.
**Dependencies:** Phase 6 (MVP cut-line; closed-loop recovery live in compose). Phase 7 swaps every external dependency from a local mock to its real cloud counterpart **without changing the Go port interfaces** — the port/adapter discipline from Phases 1–6 is what makes this phase fast. No domain code changes; only adapter swaps, web surface adds for WorkOS+Stripe callbacks, and a new `infra/terraform/` tree.
**External services (all REAL):** AWS (VPC, ECS Fargate, RDS Postgres 16 + pgvector, ElastiCache Redis Serverless, S3, KMS, Secrets Manager, ALB, ACM, Route53, CloudWatch, ECR, IAM, CloudTrail, AWS Backup), **Temporal Cloud** (namespace `nexis-prod`), **Modal.com** (validator sandbox), **Neo4j AuraDB Professional**, **Grafana Cloud** (OTLP HTTPS endpoint + Loki + Tempo + dashboards), **WorkOS** (AuthKit hosted UI), **Stripe** (metered billing + webhooks), **Resend** (transactional email; SES fallback), **PagerDuty** (oncall — optional), **Datadog** (synthetics — optional). The Phase 6 GitHub App stays the same; we only widen the per-org installation model so real customers connect their own repos.

---

## 1. Goals

1. **Terraform IaC for AWS** — every cloud resource declared in `infra/terraform/` under three environment roots (`dev` | `staging` | `prod`) sharing a `modules/` library. Single `terraform apply` per env stands the stack up from zero. State in S3 + DynamoDB lock; cross-account roles for staging/prod isolation. No click-ops anywhere.
2. **One-file adapter swaps** for every Phase 1–6 port that pointed at a local mock — driven by a single env var per port. Specifically: `PATCH_STORE=s3`, `KEYVAULT=kms`, `SECRETS=secretsmanager`, `MAILER=resend`, `BILLING_PROVIDER=stripe`, `AUTH_PROVIDER=workos`, `VALIDATOR_RUNNER=modal`, `GRAPH_PROVIDER=aura`, `TEMPORAL_CLOUD=1`, `OTLP_TARGET=grafana-cloud`. No port signatures change; **the Phase 1–6 acceptance tests pass verbatim against AWS** (this is the cutover contract).
3. **Real WorkOS AuthKit** — `/v1/auth/workos/callback` accepts the OAuth code, exchanges it via the WorkOS Node SDK on the web (or the WorkOS Go SDK on the control-plane — we ship the Go path), JIT-provisions an `organizations` + `users` + `org_members` triple on first sign-in, mints a `session_token`, redirects to `/console` (or `/onboarding/workspace` if no workspace yet). The Phase 2 stubbed `workos/provider.go` becomes the real impl. The Phase 2 password+magic+passkey+SAML+SCIM surface stays — WorkOS now handles all of it.
4. **Real Stripe metered billing** — Phase 3.5's `LocalBillingProvider` swaps to `StripeBillingProvider`. Three price catalog entries: `price_runtime_hours` (carried over from Phase 3.5 at $0.10/hr), `price_recovery_events` ($0.50/recovery, new), `price_tokens` ($0.0005/1k tokens markup). Subscriptions via Stripe Checkout; metered usage pushed via `usage_record.create`; webhook handler at `/v1/webhooks/stripe` verifies the signature, updates the `entitlements` ledger, and the UI reflects within 30 s.
5. **GitHub Actions CI/CD with ArgoCD-driven deploys** — PR pipeline (lint + typecheck + test + arch + trivy + docker-build) is unchanged from Phase 1. Merge-to-main pipeline now: (a) builds + pushes container images to ECR tagged by git SHA, (b) bumps the tag in `infra/k8s/<svc>/values.yaml` in a separate `infra-deploy` repo, (c) ArgoCD app-of-apps watches that repo and syncs to ECS via Argo Rollouts blue/green analysis. Staging auto-syncs on merge; prod requires manual approval inside ArgoCD.
6. **SOC 2-lite controls** — audit immutability proof (the HMAC chain from Phase 2 gets a nightly Merkle root anchored to S3 with Object Lock + Glacier transition), encrypted backups (RDS automated daily + weekly snapshots cross-region copied via AWS Backup), backup-restore drill runbook executed once and documented, incident-response runbook (`docs/runbooks/incident-response.md`), access reviews automated via IAM Access Analyzer, CloudTrail enabled across all accounts.
7. **Pen-test scope + fix list** — a third-party contractor runs a 1-week web-app + cloud-infra pen test against staging; Phase 7 ships the **fix list and remediation patches** (CVEs closed, IAM tightened, headers locked). The pen test itself is external work; Phase 7's deliverable is the post-test code state.
8. **k6 load test** — `tests/load/recovery_pipeline.js` simulates 100 concurrent recovery pipelines (real fixture incidents) against staging. Pass criterion: **p95 wallclock < 8 min, zero cross-tenant data leaks** (verified via S3 bucket policy unit tests + an RLS probe job that runs alongside the load test).

## 2. Non-goals

* **Multi-region replication.** Phase 7 ships us-east-1 only. The Terraform module shape is multi-region-clean (region passed as a top-level variable), but only `dev|staging|prod` in `us-east-1` actually exists. Multi-region failover is Phase 9+.
* **EKS / Kubernetes.** ECS Fargate is the runtime; we do NOT stand up EKS. ArgoCD itself runs as an ECS service (or via AWS-managed Argo if it lands in time — see Risk 17.6). Argo Rollouts is used to drive **ECS** blue/green via the Rollouts ECS provider plugin.
* **Self-hosted Stripe replacement.** Stripe is the only billing provider; we keep the `LocalBillingProvider` for dev but do not ship a second cloud provider.
* **Self-hosted WorkOS replacement.** WorkOS AuthKit is the only cloud auth provider. The `AuthProvider` interface remains intact so a future swap is one-file, but Phase 7 ships only the WorkOS path.
* **Multi-tenant Modal pool tuning.** The Modal sandbox runs one container per validate call (scale-to-zero, ~5 s cold start). Per-tenant warm pools, custom CPU shapes, and GPU pools are Phase 9.
* **Custom domains per tenant.** Customers use `<tenant>.nexis.dev` (wildcard ACM cert). Per-tenant vanity domains (e.g. `recovery.acme.com`) are Phase 8 docs + Phase 9 impl.
* **Real-time Stripe usage push (sub-30s).** Phase 7 ships hourly usage_record batches (Stripe's documented minimum granularity at this tier). The 30 s acceptance criterion only applies to **entitlement changes triggered by webhook events** (subscription created/updated/cancelled), not usage charges.
* **Full SOC 2 Type II.** "SOC 2-lite" = the technical controls and runbooks that a future Type I/II audit would consume. The auditor engagement itself is Phase 9+. The deliverable here is the evidence + control set, not the report.
* **HIPAA, PCI-DSS, FedRAMP.** Out of scope. Phase 7 designs the AWS account boundaries so future compliance work isn't blocked, but no controls beyond SOC 2-lite ship now.
* **Real OTel anomaly-driven Sentinel.** Phase 6 deferred this to Phase 7 in its non-goals; Phase 7 defers it to Phase 8. Sentinel still reads `incidents_raw` only.

## 3. Architecture

Same port/adapter pattern as Phases 1–6. Phase 7 introduces:

* A **new top-level `infra/terraform/` tree** alongside the existing `services/`, `apps/`, `docs/`.
* A **new top-level `infra/k8s/` tree** (rollouts manifests + ArgoCD app-of-apps) — pointed at by ArgoCD; the directory name says k8s but the manifests are Rollouts CRs targeting the ECS provider.
* **Zero new domain code.** Every change is either an adapter swap, a new env var, a new webhook handler, or a new web route.
* **Six adapter additions** under `services/control-plane/internal/adapter/`:
  - `keyvault/kms.go` — AWS KMS envelope encryption (the §3.5 of the project plan).
  - `secrets/secretsmanager.go` — `SecretsStore` impl over AWS Secrets Manager.
  - `mailer/resend.go` (and `mailer/awsses.go` as a fallback) — `Mailer` impl.
  - `auth/workos/provider.go` — gets a **real body** (the Phase 2 stub becomes live).
  - `billing/stripe/provider.go` — gets a **real body** (the Phase 3.5 stub becomes live).
  - `graphstore/neo4j/aura.go` — same driver, different connection URI + TLS bolt scheme.
* **One new validator runner** under `services/validator/internal/runner/modal.go` implementing the same `Runner` interface as the Phase 4 `docker.go`.
* **One new webhook handler** group under `services/control-plane/internal/transport/http/handler/webhooks.go`.
* **Two web surface additions** under `apps/web/app/`: `/auth/workos/callback` (Next.js route handler) and `/console/settings/billing` upgrades (plan switcher + Stripe Customer Portal link).

The control-plane Go layout adds packages but the layering rules from §3.7 of the project plan are unchanged — `adapter` still imports `domain` + `platform`, no upward references. `go-arch-lint` keeps the gate.

### 3.1 control-plane Go layout (Phase 7 additions)

```
services/control-plane/
  internal/
    adapter/
      keyvault/
        local.go                  # UNCHANGED — Phase 3 default for dev
        kms.go                    # NEW — AWS KMS envelope encryption (DEK per row, EncryptionContext={org_id, secret_kind})
        kms_test.go               # NEW — uses localstack KMS for unit tests
        factory.go                # NEW — picks local|kms by KEYVAULT env
      secrets/                    # NEW package
        secretsmanager.go         # NEW — SecretsStore impl over AWS Secrets Manager (Get/Put/Rotate)
        local.go                  # NEW — file-backed dev impl mirroring the contract; replaces .env loader
        factory.go                # NEW — picks local|secretsmanager by SECRETS env
        secretsmanager_test.go
      mailer/
        smtp.go                   # UNCHANGED — Phase 2 SMTPMailer (MailHog dev)
        resend.go                 # NEW — Mailer impl over the Resend HTTP API
        awsses.go                 # NEW — Mailer impl over AWS SES (fallback if Resend rate-limits)
        factory.go                # MODIFY — picks smtp|resend|ses by MAILER env
      patchstore/
        minio/                    # UNCHANGED — Phase 4 dev impl
        s3/                       # MODIFY — fill in the Phase 4 stub with real AWS SDK v2 calls
          store.go                # MODIFY — PutObject + GetObject + DeleteObject + presigned URLs; SSE-S3 + bucket-key
          store_test.go           # NEW — localstack S3 unit tests
        factory.go                # UNCHANGED — already picks minio|s3 by PATCH_STORE
      auth/
        workos/
          provider.go             # REWRITE — replaces the Phase 2 stub; uses github.com/workos/workos-go/v4
          callback.go             # NEW — OAuth code exchange + session mint + JIT org provisioning
          scim.go                 # NEW — SCIM 2.0 webhook handler for directory sync
          provider_test.go        # NEW — replays recorded WorkOS responses
        password/                 # UNCHANGED — local-only password provider stays for dev/test
        factory.go                # MODIFY — picks local|workos by AUTH_PROVIDER env
      billing/
        local/                    # UNCHANGED — Phase 3.5 dev impl
        stripe/
          provider.go             # REWRITE — replaces the Phase 3.5 stub; uses github.com/stripe/stripe-go/v82
          webhook.go              # NEW — webhook signature verification + event dispatch (customer.subscription.* + invoice.* + checkout.session.completed)
          usage_pusher.go         # NEW — hourly cron pushes usage_records → Stripe subscription_item_usage_record.create
          provider_test.go        # NEW — replays Stripe fixtures via stripe-mock
        factory.go                # UNCHANGED — already picks local|stripe by BILLING_PROVIDER
      graphstore/
        neo4j/
          store.go                # UNCHANGED — Phase 6 driver code works with both compose neo4j and AuraDB
          aura.go                 # NEW — connection helper handling neo4j+s:// (TLS bolt) + Aura's auth header quirks
        factory.go                # NEW — picks compose|aura by GRAPH_PROVIDER env
    transport/
      http/
        handler/
          webhooks.go             # NEW — POST /v1/webhooks/stripe + POST /v1/webhooks/workos
          oauth.go                # NEW — GET /v1/auth/workos/callback + GET /v1/auth/workos/sso/{org_slug}
          billing.go              # MODIFY — adds GET /v1/billing/plans, POST /v1/billing/checkout, GET /v1/billing/portal
        middleware/
          stripe_signature.go     # NEW — verifies Stripe-Signature header before the body parser
          workos_signature.go     # NEW — verifies WorkOS webhook signature
    usecase/
      billing_entitlements.go     # NEW — applies Stripe webhook deltas to the entitlements ledger
      billing_usage_pusher.go     # NEW — Stripe usage push cron (hourly batches)
      auth_jit_provision.go       # NEW — first-sign-in tenant provisioning (called from oauth.go)
    platform/
      config/
        config.go                 # MODIFY — Phase 7 fields (AWS region, KMS key arn, Secrets prefix, Stripe keys, WorkOS keys, Resend key, Modal token, Aura URI, Grafana OTLP target, Temporal Cloud namespace)
      otel/
        otel.go                   # MODIFY — when OTLP_TARGET=grafana-cloud, sets up HTTPS exporter with Authorization=Basic <base64(instance_id:api_token)>
      temporal/
        client.go                 # MODIFY — when TEMPORAL_CLOUD=1, dials nexis-prod.tmprl.cloud:7233 with mTLS cert+key from secrets manager
      aws/                        # NEW package
        config.go                 # NEW — loads aws.Config via the default chain (IRSA / task role in prod, profile in dev)
        kms.go                    # NEW — thin KMS client helper
        s3.go                     # NEW — thin S3 client helper
        secrets.go                # NEW — thin Secrets Manager client helper
        ses.go                    # NEW — thin SES client helper
  cmd/
    server/main.go                # MODIFY (COORDINATOR) — wire AWS config + Stripe webhook handler + WorkOS callback + new factories
```

### 3.2 services/validator Modal runner

```
services/validator/
  internal/
    runner/                       # NEW package (Phase 4 stuffed runner logic into sandbox/docker.go)
      runner.go                   # NEW — Runner interface lifted from sandbox/docker.go signatures
      docker.go                   # MOVED — Phase 4 implementation, now behind the Runner interface
      modal.go                    # NEW — Modal.com runner: POSTs {patch_diff, repo_sha} to a Modal endpoint, polls until done
      factory.go                  # NEW — picks docker|modal by VALIDATOR_RUNNER env
    sandbox/
      docker.go                   # ALIAS-FILE — keeps the Phase 4 import path live (re-exports runner.NewDocker)
  modal/                          # NEW — Modal.com deployment scripts
    nexis_validator.py            # NEW — Modal stub: function('validate') accepting patch_diff, returning the same JSON shape as services/validator/POST /v1/validate
    Dockerfile.modal              # NEW — base image with pytest + hypothesis (same as Phase 6 sidecar contents)
    deploy.sh                     # NEW — `modal deploy nexis_validator.py`
    README.md                     # NEW — runbook: how to deploy / rotate / debug
```

The Modal runner adapter sends the same `{repo_sha, patch_diff, hypothesis}` payload as the local Docker runner; the response shape is identical (`{tests_passed, test_count, fail_count, coverage, hypothesis_failures, logs}`). The validator HTTP server is unchanged — only the inner Runner swap happens.

### 3.3 Web changes (Next.js 16)

```
apps/web/
  app/
    auth/
      workos/
        callback/
          route.ts                # NEW — Next route handler; receives ?code= from WorkOS, calls control-plane /v1/auth/workos/callback, sets the nexis_session cookie, redirects
      signin/
        page.tsx                  # MODIFY — replaces the password-form with a "Sign in with WorkOS" button + WorkOS hosted-UI redirect
      signup/
        page.tsx                  # MODIFY — same; WorkOS hosted UI handles signup
    (app)/
      console/
        settings/
          billing/
            page.tsx              # MODIFY — adds plan switcher (Starter | Team | Business), Customer Portal link, prorated-charge preview
            client.tsx            # NEW — uses Stripe.js for the Checkout redirect
  lib/
    auth.ts                       # MODIFY — replaces password-login SDK with WorkOS redirect helper
    billing.ts                    # MODIFY — adds checkout() + portal() helpers
```

The web no longer renders the Phase 2 password form in production — it only renders a single "Sign in" button that redirects to WorkOS. Dev/test keeps the local-auth path (gated on `NEXT_PUBLIC_AUTH_PROVIDER=local`) so Playwright suites don't need WorkOS access.

### 3.4 CI/CD topology

```
┌──────────────┐    PR open / push to PR
│  Developer   │─────────────────────────────────┐
└──────────────┘                                 ▼
                                          ┌────────────────────┐
                                          │ GitHub Actions     │
                                          │ ci.yml (PR)        │
                                          │  - lint            │
                                          │  - typecheck       │
                                          │  - test (Go + Web) │
                                          │  - arch-lint       │
                                          │  - trivy fs scan   │
                                          │  - docker build    │
                                          └─────────┬──────────┘
                                                    │ green ✔
                                                    ▼
                                          ┌────────────────────┐
                                          │  Reviewer + merge  │
                                          └─────────┬──────────┘
                                                    │ merge to main
                                                    ▼
                                          ┌────────────────────┐
                                          │ GitHub Actions     │
                                          │ release.yml (main) │
                                          │  - build images    │
                                          │  - push to ECR     │
                                          │    (sha-tagged)    │
                                          │  - bump tag in     │
                                          │    infra-deploy    │
                                          │    repo            │
                                          └─────────┬──────────┘
                                                    │ commit to infra-deploy/values.yaml
                                                    ▼
                                          ┌────────────────────┐
                                          │  ArgoCD watches    │
                                          │  infra-deploy repo │
                                          │  - staging: auto   │
                                          │  - prod: manual    │
                                          └─────────┬──────────┘
                                                    │ sync
                                                    ▼
                                          ┌────────────────────┐
                                          │ Argo Rollouts CR   │
                                          │  - blue/green ECS  │
                                          │  - SLO analysis    │
                                          │    (latency p95)   │
                                          │  - auto-rollback   │
                                          │    on breach       │
                                          └────────────────────┘
```

A separate **infra-deploy** repo (not `web_app/`) holds the values manifests ArgoCD watches. This isolates "what's deployed where" from "what's the source of truth for the code", and gives auditors a clean release trail.

## 4. Database

Phase 7 ships **no new tables** in the application schema. Two existing tables get small additions:

### 4.1 Modifications

```
entitlements                            -- existed since Phase 3.5 as the billing-state ledger.
  ALTER TABLE entitlements
    ADD COLUMN stripe_subscription_id   text,
    ADD COLUMN stripe_price_id          text,
    ADD COLUMN stripe_customer_id       text,
    ADD COLUMN current_period_end       timestamptz,
    ADD COLUMN trial_end                timestamptz,
    ADD COLUMN cancel_at_period_end     boolean NOT NULL DEFAULT false,
    ADD COLUMN status                   text NOT NULL DEFAULT 'active'
                                          CHECK (status IN ('trialing','active','past_due','canceled','incomplete'));

usage_records                           -- existed since Phase 3.5
  ALTER TABLE usage_records
    ADD COLUMN stripe_usage_record_id   text,                    -- null until pushed
    ADD COLUMN pushed_at                timestamptz;
  CREATE INDEX usage_records_unpushed_idx
    ON usage_records (org_id) WHERE pushed_at IS NULL;          -- drives the hourly Stripe pusher
```

### 4.2 New `audit_anchors` table (SOC 2-lite)

```
audit_anchors
  id              uuid PK
  period_start    timestamptz NOT NULL
  period_end      timestamptz NOT NULL
  row_count       bigint NOT NULL                              -- number of audit_log rows covered
  merkle_root     bytea NOT NULL                               -- sha256 root over audit_log.hmac column
  prev_root       bytea                                        -- previous anchor's merkle_root; nullable for the first one
  s3_object_key   text NOT NULL                                -- where the snapshot was written in the object-lock bucket
  s3_version_id   text NOT NULL                                -- Object Lock version pin
  anchored_at     timestamptz NOT NULL DEFAULT now()
  UNIQUE (period_end)
```

A nightly cron in the control-plane runs:
1. `SELECT period boundaries` from the last anchor onward.
2. Streams the rows' `hmac` column, computes a Merkle root.
3. Uploads the snapshot CSV to the audit-export S3 bucket (Object Lock: COMPLIANCE mode, retention 7 years).
4. Writes the `audit_anchors` row.

Restoration drill: `nex audit verify --since <iso>` re-derives the Merkle root from the live `audit_log` and compares it to the latest `audit_anchors.merkle_root` — any divergence is a compromised audit log.

### 4.3 RLS

Both `entitlements` and `usage_records` were already RLS-protected on `org_id` from Phase 3.5; the new columns inherit the policy. `audit_anchors` is **system-table** (not RLS-protected; written only via the admin pool from the cron job); the audit chain itself is RLS-protected via `audit_log` (Phase 2 baseline).

### 4.4 Drizzle / sqlc sync

`packages/db/schema.ts` is updated to reflect the new columns; `services/control-plane/internal/adapter/repo/queries/billing.sql` adds queries for unpushed usage records + entitlement updates. `make sqlc` regenerates.

## 5. Terraform module design

State backend: a private S3 bucket `nexis-terraform-state-<aws-account>` with versioning + Object Lock + a DynamoDB lock table `nexis-terraform-locks`. Bootstrap is a one-shot operator script (`infra/terraform/bootstrap.sh`) that creates the bucket + table via the AWS CLI; everything after is `terraform init` + `terraform apply`.

### 5.1 Directory layout

```
infra/terraform/
  modules/
    vpc/                          # VPC + 3 private + 2 public subnets across 3 AZs, NAT gateways, route tables, flow logs
      main.tf
      variables.tf
      outputs.tf
    rds/                          # RDS Postgres 16 Multi-AZ + pgvector + parameter group + subnet group + automated backups
    redis/                        # ElastiCache Redis Serverless
    s3-bucket/                    # generic encrypted S3 bucket with versioning + lifecycle + (optional) Object Lock
    kms-key/                      # CMK with annual rotation + per-env aliases
    secrets-manager-secret/       # a single secret + IAM read policy
    ecs-cluster/                  # Fargate cluster + service-discovery namespace + capacity providers
    ecs-service/                  # generic Fargate service: task def + service + ALB target group + autoscaling
    alb/                          # ALB + ACM cert + listener + WAF
    route53-zone/                 # hosted zone + apex + wildcard records
    cloudwatch-logs/              # per-service log group with retention
    iam-task-role/                # ECS task role + execution role with scoped policies
    waf/                          # WAFv2 rules: rate limit, common rule set, SQLi/XSS managed rules
  envs/
    dev/
      backend.tf                  # terraform { backend "s3" { ... } }
      main.tf                     # composes modules: vpc → rds → redis → s3 buckets → kms → secrets → ecs → alb → route53
      variables.tf
      terraform.tfvars            # env-specific values (cidr, az count, instance sizes)
      outputs.tf                  # exports: alb_dns_name, rds_endpoint, etc.
    staging/
      <same shape>
    prod/
      <same shape; manual approval guard via Atlantis or GitHub Actions environments>
  bootstrap.sh                    # one-shot script to create the state bucket + lock table
  README.md                       # runbook
```

### 5.2 Per-environment isolation

Each environment lives in a **separate AWS account** under an AWS Organization root. The `dev` account hosts engineer scratch; `staging` mirrors prod with synthetic data; `prod` is the customer-facing account. Cross-account access via IAM roles (CI/CD assumes `OrganizationAccountAccessRole` from the management account).

Tag every resource `nexis-{env}-{name}` and propagate `Environment={env}` + `ManagedBy=terraform` tags via the AWS provider's `default_tags` block.

### 5.3 Module-by-module purpose

| Module | Purpose | Notes |
|---|---|---|
| `vpc` | 10.0.0.0/16 in prod, /20 in dev. 3 private + 2 public subnets across 3 AZs. NAT gateways per AZ. VPC Flow Logs to CloudWatch. | dev uses 1 NAT to save $32/mo. |
| `rds` | Postgres 16.4, Multi-AZ in staging/prod, `db.m6g.large` baseline, automated backups 7-day retention, weekly snapshot copied via AWS Backup to a cross-region vault. `pg_vector` enabled via custom parameter group. Storage encryption with the env CMK. | RLS policies migrate via the existing `migrate` job. |
| `redis` | ElastiCache Redis Serverless, encryption-in-transit + at-rest, IAM auth disabled (we use AUTH tokens stored in Secrets Manager). | Used for rate limits + session cache + LLM cache (Phase 5 carry-over). |
| `s3-bucket` | Generic bucket factory. Versioning on, default SSE-S3 (bucket-key for cost), lifecycle for old versions, optional Object Lock for the audit-export bucket. | Five buckets per env: `patches`, `audit-export`, `frontend-static`, `terraform-state` (shared), `logs`. |
| `kms-key` | Symmetric CMK, annual rotation on, per-env alias (`alias/nexis-{env}-data`). | One key per env in Phase 7; per-org sub-aliases via the DEK + EncryptionContext pattern from §3.5 of the project plan — no new KMS keys per tenant. |
| `secrets-manager-secret` | A single Secrets Manager entry + IAM read policy keyed to the ECS task role. | One secret per service per env (e.g. `nexis-prod-control-plane`, `nexis-prod-gitops`). |
| `ecs-cluster` | Fargate cluster + Cloud Map namespace `nexis.local`. Container Insights on. | Capacity provider strategy: 100% Fargate; Fargate Spot is dev-only. |
| `ecs-service` | Task definition + service + ALB target group + autoscaling policies (target tracking on CPU 60% + memory 75%). Health check via `/healthz`. min/max set per service: control-plane 2-10, validator 1-4, gitops 1-4, web 2-6. | Tasks run on awsvpc networking in private subnets; ALB ingress only. |
| `alb` | ALB in public subnets + ACM cert (DNS validation) + WAF ACL attachment. HTTPS-only with HTTP→HTTPS redirect. | Wildcard `*.nexis.dev` cert in prod; `*.staging.nexis.dev` in staging. |
| `route53-zone` | Hosted zone for `nexis.dev` (prod) or `staging.nexis.dev` (staging). A/AAAA records → ALB; CNAME `*` → ALB. | DNS-validated ACM cert lives here. |
| `cloudwatch-logs` | Log group per service with 30-day retention (dev), 90-day (staging), 365-day (prod). | Logs are also shipped to Loki via the OTel collector. |
| `iam-task-role` | ECS task role (in-cluster permissions: KMS Decrypt, Secrets Manager Get, S3 Get/Put scoped to the env bucket prefix, KMS Encrypt for envelope encryption) + execution role (ECR pull, CloudWatch logs put). | Least-privilege per service; control-plane has full app permissions, validator has none beyond log writes, gitops has no S3/KMS at all. |
| `waf` | WAFv2 web ACL: AWS-managed common rule set, known-bad-inputs rule set, rate limit 2000 req/5min/IP. | Logs to S3 + CloudWatch. |

### 5.4 Cross-account state isolation

Phase 7 keeps the Terraform state bucket in the **management account**, with explicit cross-account `AssumeRole` from each env's CI runner. This prevents a compromised prod credential from rewriting staging state.

### 5.5 Bootstrap commands

```bash
# One-time per AWS Organization (run with admin creds in management account):
infra/terraform/bootstrap.sh \
  --aws-account-id <mgmt-account> \
  --state-bucket-name nexis-terraform-state \
  --lock-table-name nexis-terraform-locks

# Per environment:
cd infra/terraform/envs/dev
terraform init \
  -backend-config="bucket=nexis-terraform-state" \
  -backend-config="key=envs/dev/terraform.tfstate" \
  -backend-config="region=us-east-1" \
  -backend-config="dynamodb_table=nexis-terraform-locks"

terraform plan -out=tfplan
terraform apply tfplan
```

CI runs the plan + apply in a GitHub Actions environment-gated job (manual approval for prod).

## 6. Provider swap design

Each port → adapter swap reads a single env var and is otherwise zero-touch for callers. The factories are intentionally small switch statements; the table below lists each swap end to end.

| Port (domain) | Phase 1–6 default | Phase 7 swap | Env var | Factory file |
|---|---|---|---|---|
| `domain.KeyVault` | `keyvault.LocalKeyVault` | `keyvault.KMSVault` | `KEYVAULT=local\|kms` | `internal/adapter/keyvault/factory.go` |
| `domain.SecretsStore` | `.env` loader (Phase 1) | `secrets.SecretsManagerStore` | `SECRETS=env\|secretsmanager` | `internal/adapter/secrets/factory.go` |
| `domain.PatchStore` | `patchstore/minio` | `patchstore/s3` | `PATCH_STORE=minio\|s3` | `internal/adapter/patchstore/factory.go` |
| `domain.Mailer` | `mailer.SMTPMailer` (MailHog) | `mailer.ResendMailer` or `mailer.SESMailer` | `MAILER=smtp\|resend\|ses` | `internal/adapter/mailer/factory.go` |
| `domain.AuthProvider` | `auth/password.Provider` | `auth/workos.Provider` | `AUTH_PROVIDER=local\|workos` | `internal/adapter/auth/factory.go` |
| `domain.BillingProvider` | `billing/local.Provider` | `billing/stripe.Provider` | `BILLING_PROVIDER=local\|stripe` | `internal/adapter/billing/factory.go` (unchanged from Phase 3.5) |
| `domain.Graph` | `graphstore/neo4j.Store` (compose) | `graphstore/neo4j.Store` (AuraDB URI) | `GRAPH_PROVIDER=compose\|aura` | `internal/adapter/graphstore/factory.go` |
| `validator.Runner` | `runner.DockerRunner` | `runner.ModalRunner` | `VALIDATOR_RUNNER=docker\|modal` | `services/validator/internal/runner/factory.go` |
| Temporal endpoint | `temporal://temporal:7233` (dev server) | `nexis-prod.tmprl.cloud:7233` + mTLS | `TEMPORAL_CLOUD=0\|1` + `TEMPORAL_TLS_CERT/KEY` | `internal/platform/temporal/client.go` |
| OTLP endpoint | `otel-collector:4317` (compose) | `https://otlp-gateway-<region>.grafana.net/otlp` | `OTLP_TARGET=local\|grafana-cloud` | `internal/platform/otel/otel.go` |

### 6.1 KMSVault (`internal/adapter/keyvault/kms.go`)

Implements `domain.KeyVault` (the same `Encrypt(ctx, plaintext) → bytes` / `Decrypt(ctx, ciphertext) → bytes` contract `LocalKeyVault` already satisfies).

Internal layout per the project plan §3.5:
* `Encrypt(plaintext)`:
  1. `aws kms GenerateDataKey(KeyId=cfg.KMSKeyARN, KeySpec=AES_256, EncryptionContext={org_id, secret_kind})` → `{Plaintext: DEK, CiphertextBlob: encrypted_dek}`.
  2. AES-256-GCM seal `plaintext` with `DEK` → `nonce || ciphertext`.
  3. Wire format: `[1 byte version=0x01][2-byte BE encrypted_dek_len][encrypted_dek][12-byte nonce][ciphertext]`.
  4. Zero the DEK buffer.
* `Decrypt(ciphertext)`:
  1. Parse the wire format.
  2. `aws kms Decrypt(CiphertextBlob=encrypted_dek, EncryptionContext={org_id, secret_kind})` → `DEK`.
  3. AES-GCM open `ciphertext` with `DEK` and `nonce` → plaintext.
  4. Zero the DEK buffer.

`org_id` + `secret_kind` are pulled from a `ctx.Value` set by the HTTP middleware (existing Phase 2 `WithTenant` already plumbs the org id). Phase 7 adds a `WithSecretKind(ctx, "integration_token" | "patch_blob" | …)` helper.

Per the project plan §3.5: **`EncryptionContext` is integrity-checked by KMS**, so a stolen DB row from org A cannot be decrypted with org B's session — KMS refuses. This is the property the SOC 2-lite auditor will inspect.

### 6.2 SecretsManagerStore (`internal/adapter/secrets/secretsmanager.go`)

New port `domain.SecretsStore`:

```go
type SecretsStore interface {
    Get(ctx context.Context, name string) ([]byte, error)
    Put(ctx context.Context, name string, value []byte) error          // rotation
    GetJSON(ctx context.Context, name string, out any) error           // helper for JSON-shaped secrets
}
```

Two impls: `LocalStore` (file under `$XDG_CONFIG_HOME/nexis/secrets/`) for dev; `SecretsManagerStore` (AWS SDK v2) for cloud. The control-plane reads three named secrets at startup: `nexis-{env}-control-plane/db-url`, `nexis-{env}-control-plane/stripe-keys`, `nexis-{env}-control-plane/workos-keys`. The Stripe + WorkOS secrets are JSON envelopes (`{publishable_key, secret_key, webhook_secret}`) consumed via `GetJSON`.

### 6.3 S3 PatchStore (`internal/adapter/patchstore/s3/store.go`)

Replaces the Phase 4 stub. Identical interface to the MinIO impl. AWS SDK v2 client; bucket name from `cfg.PatchStoreBucket`; server-side encryption `AES256` with bucket-keys to cut KMS cost. Object key format unchanged from Phase 4: `{org_id}/{run_id}/{patch_sha}.diff.encrypted`. Patch bytes are pre-encrypted via `domain.KeyVault.Encrypt` *before* hitting S3 — so a leaked S3 object is a sealed AEAD blob, and a leaked KMS-Encrypt operation is bound to its `org_id` EncryptionContext (defence in depth).

Presigned GET URLs for the console (read-only patch view) have 5-minute TTL; never used outside the browser.

### 6.4 ResendMail + AwsSES (`internal/adapter/mailer/{resend,awsses}.go`)

Same `Mailer.Send(ctx, msg domain.Email)` contract as the Phase 2 SMTPMailer. ResendMailer is the default (faster delivery + better deliverability for transactional); AwsSES is a fallback when Resend rate-limits or has an outage. The factory picks one at startup; runtime failover is **not** in scope for Phase 7 (Phase 8+).

### 6.5 Temporal Cloud swap (`internal/platform/temporal/client.go`)

Conditional dial:

```go
if cfg.TemporalCloud {
    creds, _ := tls.LoadX509KeyPair(cfg.TemporalTLSCertPath, cfg.TemporalTLSKeyPath)
    cli, _ = client.Dial(client.Options{
        HostPort:  "nexis-prod.tmprl.cloud:7233",
        Namespace: "nexis-prod.fdqxq",   // Temporal Cloud namespace handle
        ConnectionOptions: client.ConnectionOptions{
            TLS: &tls.Config{Certificates: []tls.Certificate{creds}, MinVersion: tls.VersionTLS13},
        },
    })
} else {
    cli, _ = client.Dial(client.Options{HostPort: cfg.TemporalHost, Namespace: cfg.TemporalNamespace})
}
```

The cert + key are pulled from Secrets Manager at startup and written to in-memory files (`tls.X509KeyPair` from bytes — no disk writes).

### 6.6 Modal validator runner (`services/validator/internal/runner/modal.go`)

```go
type Runner interface {
    Validate(ctx context.Context, req ValidateRequest) (ValidateResponse, error)
}

type DockerRunner struct { /* Phase 4 body */ }
type ModalRunner struct { httpClient *http.Client; endpoint string; token string }

func (m *ModalRunner) Validate(ctx context.Context, req ValidateRequest) (ValidateResponse, error) {
    // POST endpoint with bearer token; Modal returns an invocation id; poll /status until done; fetch result.
    // 5-minute hard timeout — matches Phase 4 docker run.
}
```

Modal-side stub (`modal/nexis_validator.py`):

```python
import modal
app = modal.App("nexis-validator")
image = modal.Image.from_dockerfile("Dockerfile.modal")

@app.function(image=image, timeout=300, cpu=2, memory=4096)
@modal.web_endpoint(method="POST")
def validate(body: dict):
    # apply patch, run pytest + hypothesis, return JSON
    ...
```

The container image bundles the Phase 6 hypothesis sidecar; same protocol, same fixtures.

### 6.7 Grafana Cloud OTLP (`internal/platform/otel/otel.go`)

```go
if cfg.OTLPTarget == "grafana-cloud" {
    headers := map[string]string{
        "Authorization": "Basic " + base64.StdEncoding.EncodeToString(
            []byte(cfg.GrafanaInstanceID + ":" + cfg.GrafanaAPIToken),
        ),
    }
    exporter, _ = otlptracehttp.New(ctx,
        otlptracehttp.WithEndpoint("otlp-gateway-prod-us-east-0.grafana.net"),
        otlptracehttp.WithURLPath("/otlp/v1/traces"),
        otlptracehttp.WithHeaders(headers),
        otlptracehttp.WithTLSClientConfig(&tls.Config{}),
    )
}
```

Logs and metrics use the same auth header against `/otlp/v1/logs` and `/otlp/v1/metrics`. Dashboards committed under `infra/grafana/dashboards/*.json` (imported via the Grafana Cloud Terraform provider in a Phase 8 follow-on; Phase 7 commits the JSON sources).

## 7. WorkOS integration design

### 7.1 Sign-in flow

```
1. user clicks "Sign in"
2. web POSTs /v1/auth/workos/start   →   control-plane returns { authorization_url }
3. web window.location = authorization_url     (WorkOS hosted UI)
4. user completes auth on WorkOS (password / passkey / SSO / MFA — WorkOS handles all of it)
5. WorkOS redirects to https://app.nexis.dev/auth/workos/callback?code=<one-time-code>&state=<csrf>
6. Next route handler validates state, POSTs the code to control-plane /v1/auth/workos/callback
7. control-plane:
   a. exchanges code via WorkOS SDK → { user, organization, raw_attributes }
   b. JIT-provisions (see §7.3) → ( organizations row, users row, org_members row )
   c. mints session_token (Phase 2 sessions table)
   d. returns { session_token, has_workspace }
8. web sets the nexis_session cookie + redirects to /console (or /onboarding/workspace if !has_workspace)
```

`state` is a CSRF token bound to the originating tab; stored in a 5-minute Redis key keyed by random nonce. WorkOS-returned `state` mismatch → 403.

### 7.2 SSO + SAML + SCIM

WorkOS AuthKit covers SSO + SAML + SCIM out of the box. Phase 7 maps:
* **SAML/SSO** → WorkOS's "Organizations" feature. The Phase 3 console exposes a per-org "Configure SSO" link that deep-links into the WorkOS admin portal for the customer (`POST /v1/auth/workos/sso/portal_link` returns a one-time URL).
* **SCIM 2.0** → WorkOS Directory Sync. Webhook handler `/v1/webhooks/workos` receives `dsync.user.created`/`updated`/`deleted` events, applies them to our `users` + `org_members` tables. The handler is signature-verified using WorkOS's webhook secret (HMAC-SHA256).

### 7.3 JIT tenant provisioning (`internal/usecase/auth_jit_provision.go`)

On first sign-in (the WorkOS response carries a stable `user.id` and `organization.id` — neither has been seen before):

1. Open a tx with `app.current_org_id` UNSET (admin pool).
2. `INSERT INTO organizations (id, name, workos_org_id, plan) VALUES (uuid_generate_v4(), <workos.organization.name>, <workos.organization.id>, 'trialing')` — capture the new row id.
3. `INSERT INTO users (id, email, workos_user_id, name, ...)` — capture the new row id.
4. `INSERT INTO org_members (org_id, user_id, role) VALUES (..., ..., 'owner')`.
5. `INSERT INTO entitlements (org_id, status, stripe_customer_id) VALUES (..., 'trialing', NULL)` (Stripe customer created on first checkout, not here).
6. Audit `auth.tenant_provisioned` with `{org_id, user_id, workos_user_id, source: "workos"}`.
7. Mint session, return.

If the WorkOS user is known but the org is new (a known user being invited to a new WorkOS org), step 3 is skipped and only the org + membership are created. The trickier path — an existing org gaining a new member via SCIM — is handled in the SCIM webhook (not the sign-in callback).

### 7.4 Session model

Sessions stay in Postgres (`sessions` table from Phase 2). Phase 7 does NOT switch to WorkOS-hosted sessions because that would force a WorkOS-API round-trip per request. The WorkOS user id is stored on the Postgres session row for audit + invalidation; logout deletes the session row + calls `WorkOS.UserManagement.RevokeSession` (best-effort; non-blocking).

### 7.5 MFA enrolment

Phase 2 shipped local TOTP. Phase 7 deprecates it: WorkOS now owns MFA. The `/console/settings/security` page links out to the WorkOS hosted MFA portal. The Phase 2 `mfa_factors` table stays in the schema (Phase 6 reads it for audit) but is no longer written to.

### 7.6 RBAC mapping

WorkOS organization roles → our `org_members.role`:
* WorkOS `admin` → our `owner` (single-owner orgs are the WorkOS default).
* WorkOS `member` → our `admin` or `member` depending on a per-org default we set at provisioning.

The mapping table lives in `internal/adapter/auth/workos/role_map.go` and is exposed in admin docs for the customer-success team to override per org if needed.

## 8. Stripe integration design

### 8.1 Price catalog

Three Stripe **prices** belong to one **product** named "NEXIS Recovery". They are env-specific (test mode in dev/staging; live mode in prod) and pinned in Secrets Manager:

| Stripe price id (prod) | Display | Unit | Rate | Aggregation |
|---|---|---|---|---|
| `price_xxxxxx_runtime_hours` | Workspace runtime | hour | $0.10 | metered (sum) |
| `price_xxxxxx_recovery_events` | Recovery events | event | $0.50 | metered (sum) |
| `price_xxxxxx_tokens` | LLM tokens (1k) | 1000 tokens | $0.0005 markup | metered (sum) |

Plans: Starter (3 workspaces cap, $0/mo + usage), Team ($99/mo + usage, 10 workspaces), Business ($499/mo + usage, unlimited). Plan = a Stripe subscription with one or three line items (license + metered prices); cap enforcement lives in `usecase/billing_entitlements.go`.

### 8.2 Checkout flow

1. Console → "Upgrade to Team" → `POST /v1/billing/checkout { price_id, success_url, cancel_url }`.
2. Control-plane creates (or reuses) the Stripe customer via `customer.create` (idempotent via `idempotency_key=<org_id>`), then `checkout.Session.create` with `mode=subscription`, `line_items=[{price: <plan_price>, quantity: 1}, {price: <runtime_price>}, {price: <events_price>}, {price: <tokens_price>}]`.
3. Returns `{checkout_url}` — web does `window.location = checkout_url`.
4. Stripe redirects to `success_url` on completion; the actual entitlement update happens via the webhook (don't trust the redirect — it's just UI).

### 8.3 Customer Portal

`GET /v1/billing/portal` returns a Stripe-hosted Customer Portal one-time URL. Customers manage cards + invoices + cancellations there — we don't reimplement.

### 8.4 Webhook handler (`POST /v1/webhooks/stripe`)

Signature verification via `stripe.Webhook.ConstructEvent(payload, sigHeader, cfg.StripeWebhookSecret)`. Events handled:

| Event | Action |
|---|---|
| `checkout.session.completed` | mark entitlement `status=active`, set `stripe_subscription_id`, `current_period_end`. |
| `customer.subscription.updated` | sync plan changes, `cancel_at_period_end`, `trial_end`. |
| `customer.subscription.deleted` | mark `status=canceled`, set `current_period_end`. |
| `invoice.payment_failed` | mark `status=past_due`; trigger email + console banner. |
| `invoice.payment_succeeded` | log to audit; no entitlement change (state set on subscription events). |
| `customer.updated` | sync billing email. |

The handler is **idempotent**: every Stripe event has a unique `id`; we record handled ids in a new `stripe_events_processed` table (small, append-only, RLS-free system table) and short-circuit duplicates.

Per acceptance criterion: **a Stripe test charge updates the entitlement in < 30 s** — the webhook handler writes the entitlement row inline; the UI polls `GET /v1/billing/entitlement` every 5 s while the upgrade modal is open, or listens via the Phase 3.5 SSE workspace channel for an `entitlement_changed` event. SSE is the path tested in the acceptance test.

### 8.5 Usage push (`internal/usecase/billing_usage_pusher.go`)

Hourly cron (`@every 1h`):

1. `SELECT id, org_id, workspace_id, kind, quantity, recorded_at FROM usage_records WHERE pushed_at IS NULL ORDER BY recorded_at` (admin pool, batched 100 rows).
2. Group by `(org_id, kind)`; for each group, sum `quantity`, look up the org's `stripe_subscription_id` + the matching `subscription_item_id` (cached in memory; refreshed when an entitlement updates).
3. `stripe.SubscriptionItemUsageRecord.create(SubscriptionItem=<id>, Quantity=<sum>, Timestamp=<recorded_at>, Action=increment)`.
4. On 2xx, `UPDATE usage_records SET pushed_at=now(), stripe_usage_record_id=<id> WHERE id IN (...)`.
5. On 4xx/5xx, log + retry next cron tick (no state change). 24 h of accumulated failures triggers a PagerDuty page (Phase 7 stretch if PD lands).

The Phase 3.5 minutely usage recorder (the "60-second tick") is unchanged — it still writes `usage_records` rows; only the Stripe sync becomes real. Local dev still picks `BILLING_PROVIDER=local` so no Stripe calls go out.

### 8.6 Tokens metering

The Phase 5 token ledger (`token_ledger` table) already records every LLM call. Phase 7 adds a daily aggregator: at 00:10 UTC, sum the previous day's tokens per org and `INSERT INTO usage_records (kind='tokens', quantity=<sum_tokens/1000>, unit_price_cents=<markup>) ...`. The hourly pusher then ships these rows to Stripe like any other usage row. Daily granularity (not minutely) for tokens because the ledger writes are high-volume and per-minute pushes would saturate Stripe rate limits.

### 8.7 Recovery events metering

When a `workflow_runs` row transitions to `status='succeeded'` (Phase 4 + 6), the workflow's terminal activity also `INSERT INTO usage_records (kind='recovery_events', quantity=1.0, unit_price_cents=50)`. Failed recoveries are not charged.

## 9. SOC 2-lite controls

### 9.1 Audit immutability proof

The Phase 2 audit chain (HMAC linking each row to its predecessor) gets a daily anchor:

1. Cron job at 02:00 UTC: read all `audit_log` rows since the last `audit_anchors.period_end`.
2. Build a Merkle tree over the `hmac` column.
3. Upload the snapshot CSV to `s3://nexis-{env}-audit-export/snapshots/YYYY-MM-DD.csv.gz` with Object Lock COMPLIANCE mode + 7-year retention.
4. Write `audit_anchors` row with `merkle_root`, `prev_root`, `s3_object_key`, `s3_version_id`, `row_count`.

Verification CLI (`nex audit verify`):
* Pulls every `audit_anchors` row.
* For each, re-derives the Merkle root from the live `audit_log` slice.
* Mismatch → exits non-zero with the offending date.
* The Merkle-root → prev_root chain itself is also verified (so a single row of `audit_anchors` can't be tampered with in isolation).

### 9.2 Backups + DR

* **RDS**: automated backups, 7-day retention in same region. AWS Backup vault in `us-west-2` for cross-region replication, 30-day retention. PITR enabled.
* **S3**: versioning on every bucket. Lifecycle: non-current versions to Glacier Deep Archive after 90 days.
* **Restore drill**: documented in `docs/runbooks/dr-drill.md`. Executed once at end of Phase 7 against a staging-clone; logs captured; signed off in the DoD checklist.

### 9.3 Incident-response runbook

`docs/runbooks/incident-response.md` covers:
* Severity definitions (Sev1/2/3/4) + paging policy.
* Comms templates (status page update, customer email).
* Forensics checklist (CloudTrail snapshot, log preservation, KMS access review).
* Post-mortem template.

### 9.4 Access reviews

* CloudTrail enabled in all accounts; logs to `nexis-{env}-cloudtrail` bucket (Object Lock 7y).
* IAM Access Analyzer findings reviewed weekly (calendar reminder in the runbooks).
* Quarterly access review: a script `nex iam-review --account=prod` enumerates IAM users + roles + their permissions, written to a CSV the operator signs off.

### 9.5 Encryption-at-rest matrix

| Asset | Mechanism |
|---|---|
| RDS storage | KMS CMK (`alias/nexis-{env}-rds`) — separate from app CMK |
| ElastiCache | KMS CMK (`alias/nexis-{env}-cache`) |
| S3 buckets | SSE-S3 with bucket-keys (cheap) for non-secret data; SSE-KMS for `audit-export` + `patches` |
| Patch blobs | Pre-encrypted via `domain.KeyVault` envelope (org-bound EncryptionContext) BEFORE S3 PUT |
| Integration secrets (in DB) | Already envelope-encrypted via Phase 2 KeyVault; in Phase 7 the wrapped DEK lives in KMS |
| Secrets Manager | KMS-managed |
| Backups | inherit source-asset encryption |

### 9.6 Encryption-in-transit matrix

* All ALB listeners HTTPS only (TLS 1.2+); HTTP→HTTPS redirect.
* Internal ECS-to-ECS calls TLS-enabled (Cloud Map + ALB-fronted services); Service Connect TLS rolls out where mTLS is overkill.
* RDS + Redis force in-transit encryption (parameter group + Redis AUTH).
* Temporal Cloud connection mTLS (client cert + key from Secrets Manager).

### 9.7 Logging hygiene

* No secrets in logs (Phase 2 redactor in `internal/platform/slog/` already strips `password`, `token`, `key`, `secret`, `authorization` fields; Phase 7 adds `stripe_signature`, `workos_signature`, `kms_dek` to the deny list).
* PII redaction for `email` and `name` in logs at WARN+ (already on for Phase 6's `agent_l2=true` slog records).

## 10. Pen-test scope

External 1-week engagement against staging, executed by a third-party consultancy (Bishop Fox or similar — procurement is out of scope for the spec). Scope:

* Authentication + session: WorkOS callback, CSRF, session fixation, JWT vs cookie attacks, MFA bypass attempts.
* Authorization + RLS: cross-tenant access attempts via crafted API calls, JWT swap attacks, header injection.
* Payment surface: Stripe webhook spoofing, replay attacks, signature timing attacks.
* Validator sandbox: container escape attempts against the Modal endpoint (rate-limited to consultant's IPs).
* Audit log: HMAC-chain tampering, anchor forgery.
* AWS infra: misconfigured S3 ACLs, leaked secrets, over-permissive IAM roles, public ingress on private resources, security-group sprawl.
* Frontend: XSS in user-controlled fields (incident titles, integration names), SSRF via fetch helpers, supply-chain (lockfile audit).

Phase 7 deliverable: a `docs/security/pentest-fixes-2026-Q4.md` file enumerating findings + the merged PRs that close them. No findings critical/high remain open at end-of-phase; medium findings ship a remediation plan.

## 11. Load test design

`tests/load/recovery_pipeline.js` (k6 script):

* Targets the staging ALB.
* 100 concurrent VUs over 30 minutes.
* Each VU loops: sign in (pre-baked WorkOS test user pool) → pick a random fixture incident → POST `/v1/admin/sentinel/trigger` → poll `/v1/workspaces/{ws}/pipelines/{run}/status` every 5 s → assert `pr_url` non-null within 8 min wallclock.
* Concurrent **RLS probe job** running alongside: for each of two test orgs A and B, every 10 s, repeat the Phase 3 cross-tenant `curl` checks (asserting 404s from B for A's resources). Any 200 fails the test.
* Test fixtures live in `tests/load/fixtures/` mirroring the Phase 6 catalog.

Output:
* JSON metrics piped to Grafana Cloud via `k6 cloud` or `--out json=`.
* p95 latency, error rate, RLS violation count, Stripe webhook latency.

Pass criteria (acceptance):
* p95 wallclock < 8 min.
* RLS violation count = 0.
* HTTP error rate < 1%.
* Stripe webhook → entitlement update median < 30 s.

Captured in `docs/load-test/2026-W21-report.md` as the Phase 7 DoD artifact.

## 12. HTTP surface

### 12.1 New routes (control-plane)

```
POST   /v1/auth/workos/start                            → 200 { authorization_url, state }    [public]
GET    /v1/auth/workos/callback                         → 302 redirect with session cookie   [public; consumed by web route handler]
POST   /v1/auth/workos/sso/portal_link                  → 200 { url }                         [auth, owner]
POST   /v1/webhooks/workos                              → 200 (signature-verified)            [public; signature-gated]

POST   /v1/webhooks/stripe                              → 200 (signature-verified)            [public; signature-gated]
POST   /v1/billing/checkout                             → 200 { checkout_url }                [auth, owner]
GET    /v1/billing/portal                               → 200 { url }                         [auth, owner]
GET    /v1/billing/entitlement                          → 200 Entitlement                     [auth, any role]
GET    /v1/billing/plans                                → 200 [Plan]                          [auth, any role]
```

### 12.2 Modified routes (control-plane)

```
POST   /v1/auth/signin                                  # in WorkOS mode, returns 410 Gone with { redirect: <workos_url> }
POST   /v1/auth/signup                                  # same
GET    /v1/billing/usage                                # response gains stripe_subscription_id + current_period_end
```

### 12.3 New routes (web)

```
GET    /auth/workos/callback                             # Next.js route handler; relays to control-plane callback
GET    /console/settings/billing                         # plan switcher UI (extends Phase 3.5)
GET    /console/settings/security                        # links to WorkOS MFA portal (new in Phase 7)
```

### 12.4 RBAC

| Endpoint | owner | admin | member |
|---|---|---|---|
| `POST /v1/billing/checkout` | ✓ | ✗ | ✗ |
| `GET /v1/billing/portal` | ✓ | ✗ | ✗ |
| `GET /v1/billing/entitlement` | ✓ | ✓ | ✓ |
| `POST /v1/auth/workos/sso/portal_link` | ✓ | ✗ | ✗ |
| `POST /v1/webhooks/{stripe,workos}` | n/a (signature-gated, no principal) |

## 13. Acceptance criteria

1. **Phase 1–6 acceptance tests pass against AWS staging.** A new GitHub Actions job `e2e-cloud` reruns the existing Playwright + Go integration test suite against `https://staging.nexis.dev`. Zero failures. (This is the cutover contract.)
2. **Terraform apply from zero.** `cd infra/terraform/envs/staging && terraform init && terraform apply` against an empty AWS account produces a working stack in < 30 minutes. Documented in `infra/terraform/README.md` as the operator runbook.
3. **WorkOS sign-in works end-to-end.** A test user signs in via the WorkOS hosted UI → lands on `/console` → can create a workspace. The `audit_log` row `auth.tenant_provisioned` exists with `source: "workos"`.
4. **Stripe test charge updates entitlement < 30 s.** With Stripe in test mode, a `POST /v1/billing/checkout` → success_url → `webhook` event reaches `customer.subscription.updated` → entitlement row updated → console UI shows the new plan, all within 30 s (measured from webhook receipt to UI poll).
5. **KMSVault round-trip works.** `KEYVAULT=kms` config in staging: encrypt a 1 MB random blob, decrypt it, verify byte-identical. The KMS key alias is `alias/nexis-staging-data`. CloudTrail shows `kms:Encrypt` + `kms:Decrypt` with the right `EncryptionContext`.
6. **Cross-tenant KMS refusal.** With KMSVault active: a row encrypted with `EncryptionContext={org_id: A, secret_kind: integration_token}` cannot be decrypted with `EncryptionContext={org_id: B, ...}` — KMS refuses. Integration test asserts the 403 from KMS surfaces as a domain error.
7. **Modal validator sandbox runs the fixture.** `VALIDATOR_RUNNER=modal` config: the fixture-null-pointer recovery pipeline completes, the validator activity payload shows `tests_passed=true` and `runner: "modal"` in the slog record.
8. **Temporal Cloud connectivity.** With `TEMPORAL_CLOUD=1`: the control-plane connects to `nexis-prod.tmprl.cloud:7233` with mTLS, registers the recovery workflow, and a triggered run completes.
9. **Grafana Cloud receives traces.** Every workflow run produces a trace in Grafana Cloud Tempo with the right service.name + tenant attribute. Phase 6's dashboards (workflow latency, agent token spend) render against the cloud data.
10. **ArgoCD blue/green deploys.** A merge to `main` builds + pushes the image, bumps the tag in the infra-deploy repo, ArgoCD syncs, Argo Rollouts runs an analysis step, the new task set becomes active. Manual rollback by reverting the tag bump works.
11. **k6 load test passes.** Per §11: p95 < 8 min, 0 RLS violations, < 1% error rate, Stripe webhook median < 30 s. Report committed to `docs/load-test/2026-W21-report.md`.
12. **Audit anchor verifies.** `nex audit verify --since 2026-04-01` succeeds against staging after the cutover. A deliberate `UPDATE audit_log SET payload='tampered' WHERE id=...` makes it fail. (The fail case is run in a side branch, not on the live audit log.)
13. **Backup + restore drill.** Documented in `docs/runbooks/dr-drill.md`; executed once against staging; RDS snapshot restored to a parallel `staging-restore` instance; checksum of a known row matches.
14. **Pen-test fix list shipped.** `docs/security/pentest-fixes-2026-Q4.md` exists with zero open critical/high findings. Medium findings each link to a remediation issue or ticket.
15. **WAF blocks a known-bad payload.** A `curl` with a SQLi-shaped query string against the staging ALB is blocked by the WAFv2 managed rule set (HTTP 403 with the WAF response signature).
16. **Migrations idempotent.** Running the migration ECS Task twice in a row is a no-op; the second run exits 0 without applying anything.
17. **`make build && make test && make vet && make arch` green on control-plane;** `pnpm typecheck && pnpm build` green on web; all service `Dockerfile`s build via `docker buildx`; `terraform validate` green for `dev|staging|prod`; `tflint` green; `tfsec` no high findings.

## 14. Stage breakdown

| Stage | Title | Pattern | Owned paths |
|---|---|---|---|
| 0 | Schema + config + env (entitlements columns + audit_anchors table + Phase 7 env var loader changes; bootstrap Stripe + WorkOS dev keys in dev .env) | sequential (coordinator) | `migrations/00xx_phase7_*.sql`, `internal/platform/config/config.go`, `internal/adapter/repo/queries/billing.sql`, `packages/db/schema.ts` |
| 1 | Terraform bootstrap + state + modules library (vpc, kms, s3-bucket, secrets-manager-secret, route53-zone, cloudwatch-logs, iam-task-role) | sequential | `infra/terraform/bootstrap.sh`, `infra/terraform/modules/{vpc,kms-key,s3-bucket,secrets-manager-secret,route53-zone,cloudwatch-logs,iam-task-role}/**`, `infra/terraform/README.md` |
| 2 | Terraform data plane (rds, redis, alb, waf, ecs-cluster, ecs-service) + dev environment root + smoke deploy of a hello-world ECS service | sequential | `infra/terraform/modules/{rds,redis,alb,waf,ecs-cluster,ecs-service}/**`, `infra/terraform/envs/dev/**` |
| 3 | AWS adapter swaps: KMSVault + SecretsManagerStore + S3 PatchStore (real impl filling in the Phase 4 stub) + Resend/SES mailer + AWS platform helpers | **Pattern C shard 1** of the 3 adapter shards | `internal/adapter/keyvault/kms.go` + factory, `internal/adapter/secrets/**`, `internal/adapter/patchstore/s3/store.go`, `internal/adapter/mailer/{resend,awsses}.go` + factory, `internal/platform/aws/**` |
| 4 | Modal validator runner + Temporal Cloud client swap + Grafana Cloud OTLP exporter + AuraDB Neo4j connection | **Pattern C shard 2** | `services/validator/internal/runner/**`, `services/validator/modal/**`, `internal/platform/temporal/client.go`, `internal/platform/otel/otel.go`, `internal/adapter/graphstore/factory.go`, `internal/adapter/graphstore/neo4j/aura.go` |
| 5 | WorkOS auth provider (real impl) + OAuth callback handler + SCIM webhook + JIT provisioning usecase + role mapping | **Pattern C shard 3** (parallel with 3 + 4 — disjoint paths) | `internal/adapter/auth/workos/**`, `internal/usecase/auth_jit_provision.go`, `internal/transport/http/handler/oauth.go`, `internal/transport/http/middleware/workos_signature.go`, `apps/web/app/auth/workos/**`, `apps/web/app/auth/{signin,signup}/page.tsx`, `apps/web/lib/auth.ts` |
| 6 | Stripe billing provider (real impl) + webhook handler + usage pusher cron + entitlement usecase + Customer Portal link + plan switcher UI | sequential (after Stages 3-5 land) | `internal/adapter/billing/stripe/**`, `internal/transport/http/handler/webhooks.go`, `internal/transport/http/handler/billing.go` (modify), `internal/transport/http/middleware/stripe_signature.go`, `internal/usecase/billing_{entitlements,usage_pusher}.go`, `apps/web/app/(app)/console/settings/billing/**`, `apps/web/lib/billing.ts` |
| 7 | GitHub Actions release pipeline + ECR push + infra-deploy repo seed + ArgoCD app-of-apps + Argo Rollouts (ECS provider) CR | sequential | `.github/workflows/release.yml`, `infra/k8s/argocd/app-of-apps.yaml`, `infra/k8s/rollouts/*.yaml`, `infra-deploy/` (separate repo, seeded via script) |
| 8 | SOC 2-lite: audit anchor cron + verify CLI + DR runbook + IR runbook + access-review script + access-policy hardening | sequential | `internal/usecase/audit_anchor.go`, `cmd/nex/audit_verify.go`, `docs/runbooks/{dr-drill,incident-response,access-review}.md`, IAM updates in `infra/terraform/modules/iam-task-role/**` |
| 9 | Staging environment + cutover dry-run + Phase 1–6 acceptance suite runs green against staging | sequential | `infra/terraform/envs/staging/**`, `.github/workflows/e2e-cloud.yml` |
| 10 | k6 load test + RLS probe + load-test report | sequential | `tests/load/recovery_pipeline.js`, `tests/load/fixtures/**`, `docs/load-test/2026-W21-report.md` |
| 11 | Pen-test prep + pen-test fix patches + security review pass | sequential (multi-PR; one PR per finding) | `docs/security/pentest-fixes-2026-Q4.md`, fixes scattered across the tree |
| 12 | Prod environment + go-live cutover + DoD | sequential (coordinator-only — touches `infra/terraform/envs/prod/**` + DNS) | `infra/terraform/envs/prod/**`, `docs/runbooks/cutover-2026-W21.md` |

Stages 3, 4, 5 form the **Pattern C** wave (three backend-engineer agents in parallel; disjoint adapter paths). Stages 6, 7 run **after** the Stage 3-5 wave merges because both touch `cmd/server/main.go` (the coordinator file) and the HTTP server router. Stage 9-12 are sequential because they're all coordinator-owned (Terraform env roots + DNS + cutover).

## 15. Conventions to bake in

* **Commits:** single-line; **no `Co-Authored-By` footers** (the Phase 1 convention carries forward).
* **Resource naming:** every AWS resource tagged `Name=nexis-{env}-{name}`. Examples: `nexis-prod-rds-primary`, `nexis-prod-ecs-control-plane`, `nexis-staging-alb-public`.
* **Region:** us-east-1 only. Multi-region NOT in scope (see §2).
* **KMS:** annual key rotation on for every CMK. Key policies grant `kms:Decrypt` + `kms:GenerateDataKey` to the ECS task role only.
* **S3:** every bucket has default encryption + versioning + a lifecycle rule transitioning non-current versions to Glacier Deep Archive after 90 days. Object Lock COMPLIANCE on `audit-export` only.
* **RDS:** Multi-AZ on staging + prod (not dev). Automated backups 7-day retention; weekly snapshots cross-region copied via AWS Backup. PITR on.
* **ECS:** awsvpc networking, private subnets, ALB in public. Min/max autoscaling: control-plane 2-10, validator 1-4, gitops 1-4, web 2-6. Target tracking on CPU 60% + memory 75%.
* **State:** Terraform state in S3 + DynamoDB lock in the management AWS account. Cross-account assume-role for staging + prod state writes.
* **Secrets:** zero baked into tfvars. Every secret pulled at runtime via the AWS Secrets Manager data source or via the control-plane's `SecretsStore` port.
* **Observability:** every service exports OTLP traces, metrics, logs to Grafana Cloud. CloudWatch keeps a 30/90/365-day mirror by env for compliance.

## 16. Risks / open items

### 16.1 Argo Rollouts ECS provider maturity

The Argo Rollouts AWS-provider-for-ECS plugin is younger than its k8s sibling. Risk: a missing feature (e.g. canary traffic shifting via ALB target group weights) forces us into a workaround. Mitigation: Phase 7 ships **blue/green** as the primary strategy (mature in the ECS plugin); canary lands in Phase 8 if the plugin matures, otherwise stays blue/green. Documented in `infra/k8s/rollouts/README.md`.

### 16.2 Modal cold-start latency

Modal scale-to-zero containers cold-start in ~5 s. Risk: the validator activity's 10-minute Temporal timeout has plenty of headroom, but the user-facing live-demo latency budget (sub-5-min per the Phase 6 acceptance) tightens by 4-5 s. Mitigation: a warm-pool of N=1 always-on Modal containers in prod (`modal serve --keep-warm 1`); cost is ~$5/mo. Dev/staging accept the cold start.

### 16.3 Stripe webhook retry storm

Stripe retries failed webhooks for up to 3 days. Risk: a control-plane outage produces a queue of replayed events that all hit at once on recovery. Mitigation: the `stripe_events_processed` idempotency table; the webhook handler is order-independent; the entitlement table can absorb replayed `updated` events because they only set absolute values, not deltas.

### 16.4 WorkOS SCIM deletion semantics

WorkOS Directory Sync emits `dsync.user.deleted` when a user is removed from a directory. Risk: a misconfigured directory sync could mass-delete users. Mitigation: the handler does **soft delete** (`UPDATE users SET deleted_at=now()`), never hard delete; admins can restore via the console for 30 days. Documented in `internal/adapter/auth/workos/scim.go`.

### 16.5 KMS request rate limits

KMS Encrypt + Decrypt have per-region per-account quotas (~10k/s baseline, ~30k/s burst). Risk: bulk encryption (e.g. seeding a large patch corpus on first cutover) could hit the quota. Mitigation: the envelope encryption pattern already minimises KMS calls (one per write, one per read — not per byte); the patch store batches DEKs per `(org_id, run_id)`. If we still hit limits, request a quota increase (free).

### 16.6 Temporal Cloud namespace cost

Temporal Cloud charges per-action; high-volume workflows could push monthly cost above the budgeted $200/mo (Risk #6 in `PROJECT_PLAN.md` §8). Mitigation: the existing per-tenant workflow budget cap (Phase 5) doubles as a Temporal-action cap; dashboards alert at 70% of monthly threshold. Long-term: if cost crosses $500/mo, self-host Temporal on the existing ECS cluster (documented but Phase 9+).

### 16.7 Cutover blast radius

Cutting over from compose to AWS is a one-way move for production data. Risk: a bug in the S3 PatchStore breaks production patch retrieval. Mitigation: Stage 9 runs the **full Phase 1–6 acceptance suite** against staging before the prod cutover (Stage 12). Stage 12 has a documented rollback: re-point DNS to the old compose host (kept warm for 7 days post-cutover) and re-run from the last verified backup. Documented in `docs/runbooks/cutover-2026-W21.md`.

### 16.8 ECS Fargate ephemeral storage

Fargate tasks have 20 GB ephemeral storage by default. The validator runner (Phase 4) untars patched repos into `/tmp`; large repos could overflow. Mitigation: the Modal runner takes over the patch-apply path in prod (Modal containers have configurable disk); the validator container itself is tiny (HTTP server only). The Phase 4 Docker runner stays as a dev fallback.

### 16.9 Cross-account ECR pulls

Staging + prod live in separate accounts; both pull images from a shared ECR registry in the management account. Risk: an IAM mis-grant breaks the pull at deploy. Mitigation: the ECR repository policy explicitly allow-lists the staging + prod task execution role ARNs; integration test in Stage 7 verifies a pull from each account.

### 16.10 ALB cert rotation

ACM certs auto-renew but require DNS validation. Risk: Route53 zone changes (manual edits) could break renewal silently. Mitigation: the Terraform module pins the validation records; weekly CloudWatch alarm on cert expiry < 30 days; documented in `docs/runbooks/cert-rotation.md`.

### 16.11 Stripe-mock fidelity for tests

The Stripe webhook handler is unit-tested against `stripe-mock`, which lags the real API by 1-2 minor versions. Risk: a real Stripe event shape diverges from the mock fixture. Mitigation: contract tests run against the real Stripe test mode in the e2e-cloud GHA job; mock-only tests are flagged as "fast-path".

### 16.12 WorkOS dev rate limits

WorkOS test environment has a 10 r/s limit per app. Risk: parallel Playwright suites hit it. Mitigation: tests reuse a small pool of pre-baked sessions where possible; the e2e suite serializes sign-in scenarios; production has no such limit.

### 16.13 Audit anchor lag during outage

A nightly anchor missed during an outage means a longer Merkle verification window on recovery. Risk: a multi-day outage produces a multi-day anchor gap. Mitigation: the anchor cron is idempotent (anchors by `period_end`), so on recovery it backfills all missed anchors; the verify CLI handles backfill windows.

### 16.14 Path of least resistance for WorkOS bypass

Local dev keeps the password provider (`AUTH_PROVIDER=local`). Risk: a misconfigured staging env keeps `local` and bypasses WorkOS. Mitigation: a startup assertion in `cmd/server/main.go` — if `ENV in {staging, prod}` and `AUTH_PROVIDER != workos`, the process exits with a fatal error. Same assertion for `BILLING_PROVIDER`, `PATCH_STORE`, `KEYVAULT`, `MAILER`.

### 16.15 Service Connect TLS adoption

Internal ECS-to-ECS TLS via Service Connect is a recent feature. Risk: a service-mesh misconfiguration drops mTLS silently. Mitigation: a per-environment integration test asserts the wire is encrypted by capturing on a sidecar (Stage 9 acceptance). For Phase 7, internal calls go through the ALB (HTTPS-terminated, then re-encrypted to targets) as a fallback if Service Connect proves flaky.

### 16.16 PagerDuty + Datadog optional adapters

The launch prompt lists PagerDuty + Datadog as optional integrations alongside the email mailer. Phase 7 ships them only if they're trivial — both have well-known Go SDKs. If schedule pressure hits, they slip to Phase 8 without blocking the cutover. Owners documented in the stage breakdown.

---

## Appendix A: Env vars added (Phase 7)

```
# control-plane (cloud)
ENV=staging                                  # dev|staging|prod
AWS_REGION=us-east-1
AUTH_PROVIDER=workos                         # was local in Phases 1-6
BILLING_PROVIDER=stripe                      # was local in Phase 3.5
PATCH_STORE=s3                               # was minio
KEYVAULT=kms                                 # was local
SECRETS=secretsmanager                       # was env
MAILER=resend                                # was smtp
GRAPH_PROVIDER=aura                          # was compose
VALIDATOR_RUNNER=modal                       # was docker (set in services/validator)
TEMPORAL_CLOUD=1
OTLP_TARGET=grafana-cloud

# AWS adapter config (read from Secrets Manager; not in plain env in prod)
KMS_KEY_ARN=arn:aws:kms:us-east-1:<acct>:key/<id>
S3_PATCH_BUCKET=nexis-prod-patches
S3_AUDIT_EXPORT_BUCKET=nexis-prod-audit-export
SECRETS_PREFIX=nexis-prod/

# WorkOS
WORKOS_API_KEY=sk_live_...                   # from Secrets Manager
WORKOS_CLIENT_ID=client_01...
WORKOS_WEBHOOK_SECRET=ws_secret_...
WORKOS_REDIRECT_URI=https://app.nexis.dev/auth/workos/callback

# Stripe
STRIPE_SECRET_KEY=sk_live_...                # from Secrets Manager
STRIPE_PUBLISHABLE_KEY=pk_live_...
STRIPE_WEBHOOK_SECRET=whsec_...
STRIPE_PRICE_RUNTIME_HOURS=price_...
STRIPE_PRICE_RECOVERY_EVENTS=price_...
STRIPE_PRICE_TOKENS=price_...

# Resend / SES
RESEND_API_KEY=re_...
SES_FROM_ADDRESS=noreply@nexis.dev

# Temporal Cloud
TEMPORAL_HOST=nexis-prod.tmprl.cloud:7233
TEMPORAL_NAMESPACE=nexis-prod.fdqxq
TEMPORAL_TLS_CERT_PATH=/run/secrets/temporal.crt
TEMPORAL_TLS_KEY_PATH=/run/secrets/temporal.key

# Grafana Cloud
GRAFANA_OTLP_ENDPOINT=otlp-gateway-prod-us-east-0.grafana.net
GRAFANA_INSTANCE_ID=<numeric>
GRAFANA_API_TOKEN=glc_...

# Neo4j AuraDB
NEO4J_URI=neo4j+s://<id>.databases.neo4j.io
NEO4J_USER=neo4j
NEO4J_PASS=<from secrets manager>

# Modal
MODAL_ENDPOINT=https://<account>--nexis-validator-validate.modal.run
MODAL_TOKEN=<from secrets manager>

# Audit + SOC 2
AUDIT_ANCHOR_BUCKET=nexis-prod-audit-export
AUDIT_ANCHOR_CRON=0 2 * * *

# Web
NEXT_PUBLIC_AUTH_PROVIDER=workos
NEXT_PUBLIC_WORKOS_CLIENT_ID=client_01...
NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY=pk_live_...
```

## Appendix B: HTTP route map (full Phase 7 surface)

```
# Auth — new in Phase 7
POST   /v1/auth/workos/start                            [public]
GET    /v1/auth/workos/callback                         [public]
POST   /v1/auth/workos/sso/portal_link                  [auth, owner]
POST   /v1/webhooks/workos                              [public, signature-gated]

# Billing — new in Phase 7
POST   /v1/webhooks/stripe                              [public, signature-gated]
POST   /v1/billing/checkout                             [auth, owner]
GET    /v1/billing/portal                               [auth, owner]
GET    /v1/billing/entitlement                          [auth, any role]
GET    /v1/billing/plans                                [auth, any role]

# Auth — modified (Phase 2 → Phase 7)
POST   /v1/auth/signin                                  # returns 410 with workos redirect when AUTH_PROVIDER=workos
POST   /v1/auth/signup                                  # same

# Web — new in Phase 7
GET    /auth/workos/callback                            [public; Next.js route handler]
GET    /console/settings/billing                        [auth, owner]    # extends Phase 3.5
GET    /console/settings/security                       [auth, any role] # MFA portal link
```

All Phase 1–6 routes survive unchanged. The `/healthz` endpoints on every service are used as ALB health checks.

## Appendix C: IAM policy summary

Per-service IAM task roles (least privilege):

**control-plane (`nexis-{env}-control-plane-task`):**
* `kms:Encrypt`, `kms:Decrypt`, `kms:GenerateDataKey` on `arn:aws:kms:us-east-1:<acct>:key/<id>` (the env data CMK).
* `s3:GetObject`, `s3:PutObject`, `s3:DeleteObject` on `arn:aws:s3:::nexis-{env}-patches/*` + `s3:GetObject` on `arn:aws:s3:::nexis-{env}-audit-export/*`.
* `s3:PutObject` + `s3:PutObjectLegalHold` on `arn:aws:s3:::nexis-{env}-audit-export/*` (anchor cron).
* `secretsmanager:GetSecretValue` on `arn:aws:secretsmanager:us-east-1:<acct>:secret:nexis-{env}/control-plane/*`.
* `ses:SendEmail`, `ses:SendRawEmail` (only if `MAILER=ses`).
* `logs:CreateLogStream`, `logs:PutLogEvents` on its log group.

**gitops (`nexis-{env}-gitops-task`):**
* `secretsmanager:GetSecretValue` on its own secret.
* RDS connection via username/password in Secrets Manager — **no KMS, no S3** (Phase 6 Risk 13.9 mitigation).
* `logs:*` on its log group.

**validator (`nexis-{env}-validator-task`):**
* `logs:*` on its log group.
* No data plane permissions; Modal is called via HTTP with a bearer token from Secrets Manager.

**web (`nexis-{env}-web-task`):**
* `logs:*` only. Web serves static + SSR, never touches AWS data services directly.

**migration ECS task (`nexis-{env}-migrate-task`):**
* `secretsmanager:GetSecretValue` on the RDS admin secret.
* `logs:*`. No S3, no KMS.

Execution role (used by ECS to pull images + emit logs): shared across services; ECR read + CloudWatch logs put.

## Appendix D: Terraform variables (root-level, per-env)

```hcl
variable "env"                  { type = string }              # dev | staging | prod
variable "region"               { type = string  default = "us-east-1" }
variable "vpc_cidr"             { type = string }              # 10.0.0.0/16 prod, 10.10.0.0/20 dev
variable "az_count"             { type = number default = 3 }
variable "nat_gateway_count"    { type = number }              # 1 dev, 3 prod
variable "rds_instance_class"   { type = string }              # db.t4g.medium dev, db.m6g.large prod
variable "rds_multi_az"         { type = bool   default = true }
variable "rds_storage_gb"       { type = number }
variable "redis_node_type"      { type = string }              # serverless in prod
variable "ecs_capacity_provider"{ type = string default = "FARGATE" }
variable "control_plane_min"    { type = number default = 2 }
variable "control_plane_max"    { type = number default = 10 }
variable "domain_name"          { type = string }              # nexis.dev prod, staging.nexis.dev staging
variable "acm_cert_san_wildcard"{ type = bool   default = true }
variable "kms_rotation"         { type = bool   default = true }
variable "audit_object_lock_years" { type = number default = 7 }
variable "tags"                 { type = map(string) }
```

The root module reads `terraform.tfvars` for env-specific values; sensitive values (e.g. WorkOS API key) are NOT in tfvars — they're written directly to Secrets Manager via a one-shot operator script.

## Appendix E: Audit metadata shapes (Phase 7 additions)

```json
// auth.tenant_provisioned
{ "org_id": "...", "user_id": "...", "workos_user_id": "user_...", "workos_org_id": "org_...", "source": "workos" }

// billing.entitlement_changed
{ "org_id": "...", "plan_before": "trialing", "plan_after": "team", "stripe_subscription_id": "sub_...", "stripe_event_id": "evt_..." }

// billing.usage_pushed
{ "org_id": "...", "kind": "runtime_hours", "quantity": 24.0, "stripe_usage_record_id": "mbur_...", "subscription_item_id": "si_..." }

// audit.anchor_written
{ "period_start": "2026-04-30T00:00:00Z", "period_end": "2026-05-01T00:00:00Z", "row_count": 12345, "merkle_root_hex": "...", "s3_object_key": "snapshots/2026-05-01.csv.gz", "s3_version_id": "..." }

// kms.refused (asserted in integration test 6.6)
{ "org_id_attempted": "...", "expected_org_id": "...", "secret_kind": "integration_token", "kms_error": "InvalidCiphertextException" }
```

## Appendix F: Cutover order (Phase 6 → Phase 7)

```
1. Stage 0-2 land   →   staging Terraform up, hello-world ECS service serving traffic at staging.nexis.dev
2. Stages 3-5 land  →   AWS adapter swaps merged behind feature flags; dev still uses compose
3. Stage 6 lands    →   Stripe webhook handler live in staging; test charges flow end-to-end
4. Stage 7 lands    →   GHA release pipeline + ArgoCD live; merge-to-main auto-deploys staging
5. Stage 8 lands    →   audit anchor cron running in staging; nightly anchors verified
6. Stage 9 runs     →   Phase 1–6 acceptance suite green against staging
7. Stage 10 runs    →   k6 load test passes; report committed
8. Stage 11 runs    →   pen-test fixes merged; security review pass complete
9. Stage 12 runs    →   prod Terraform up; DNS cutover; old compose host kept warm for 7 days
10. Phase 8 unlocks →   public beta + docs site
```

The compose stack stays runnable for dev (and as a fallback rollback target) until end of Phase 8. After Phase 8 we deprecate the compose path in CI (still build-tested, no longer e2e-tested).

## Appendix G: Phase 6 → Phase 7 evolution

| Phase 6 (local compose) | Phase 7 (cloud) |
|---|---|
| `keyvault.LocalKeyVault` (AES-GCM with .env master key) | `keyvault.KMSVault` (envelope via AWS KMS DataKey + EncryptionContext) |
| `.env` file loader | `SecretsManagerStore` |
| MinIO `PatchStore` | S3 `PatchStore` (Phase 4 stub filled in) |
| MailHog SMTP | Resend (default) or AWS SES (fallback) |
| Password auth (`auth/password.Provider`) | WorkOS AuthKit hosted UI |
| `LocalBillingProvider` (fake cards) | Stripe metered billing |
| Local Temporal dev server | Temporal Cloud `nexis-prod` namespace |
| Validator: `docker run --rm` | Validator: Modal.com HTTP endpoint |
| Neo4j community in compose | Neo4j AuraDB Professional |
| OTel → local collector → Grafana/Loki/Tempo (compose) | OTel → Grafana Cloud OTLP HTTPS |
| `docker-compose.yml` | `infra/terraform/` (VPC + ECS Fargate + RDS + Redis + S3 + KMS + Secrets + ALB + ACM + Route53 + CloudWatch + WAF + IAM) |
| GitHub Actions: lint + test + docker-build on PR | + release.yml: build → ECR push → infra-deploy bump → ArgoCD sync → Argo Rollouts |
| Phase 2 HMAC audit chain | + nightly Merkle-root anchor to S3 Object Lock + `audit_anchors` ledger + `nex audit verify` CLI |
| Manual backup/restore | RDS automated backups + AWS Backup cross-region + documented DR drill |
| No pen-test | External 1-week pen-test + remediation patches |
| No load-test artifact | k6 100-VU recovery-pipeline test + RLS probe + report |

Phase 8 will polish onboarding, ship the docs site, and freeze the eval-track numbers. Phase 9 is bug burn-down + thesis defense.
