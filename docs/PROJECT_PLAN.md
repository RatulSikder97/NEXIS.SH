# NEXIS — Master Project Plan

**Date:** 2026-05-10
**Author:** Ratul Sikder + 4 expert sub-agents (lead-software-engineer, tech-lead, saas-ui-designer, planner)
**Status:** Locked. This is the source of truth. `/clear` your session and reference this file.

Production-grade multi-tenant SaaS for autonomous fault recovery via 9 AI agents. Solo dev, 6–9 month launch window, doubles as Masters / FYP submission.

---

## 0. Locked Decisions (one-page summary)

Every choice below was made by a domain-expert sub-agent. **Don't relitigate.** Sections 4–6 below explain the reasoning.

| Area | Decision |
|---|---|
| **Goal** | Multi-tenant SaaS launch (10 design partners by month 6) **+** Masters thesis defense |
| **Architecture shape** | **3 deployable services** + modular monolith for the agent plane (NOT 9 microservices) |
| **Services** | `nexis-control-plane` (monolith with 6+ agent modules), `nexis-validator` (isolated sandbox), `nexis-gitops` (GitHub App credential boundary) |
| **Backend language** | **Go 1.22** + **Chi** (matches existing repo, ships as single binary, killer concurrency for parallel agent fan-out) |
| **API contract** | **Connect-RPC + Protobuf + buf** for service-to-service; **OpenAPI 3.1** generated from proto for external/partner endpoints |
| **ORM/query** | **sqlc** (compile-time-checked SQL, RLS-friendly, no ORM magic) + `pgx` |
| **Workflow engine** | **Temporal Cloud** (managed; self-hosted Cassandra is a full-time job for solo dev). The recovery loop IS a Temporal workflow. |
| **Database** | **RDS Postgres 16 Multi-AZ** + `pgvector` + `pg_partman` |
| **Multi-tenancy** | **Row-level + Postgres RLS** with `org_id` everywhere, enforced via `SET LOCAL app.current_org_id` per transaction. Integration test fuzzer that swaps tenant IDs and asserts zero cross-leakage. |
| **Auth** | **WorkOS** (better SAML/SCIM/Directory Sync than Clerk; single vendor for AuthKit + Enterprise SSO). Supports passwords, magic link, GitHub OAuth, passkeys, MFA, SAML, SCIM, organizations, invites. **API keys built in our DB** (`argon2id` hash + `nx_live_*` prefix). |
| **Vector store** | **pgvector** primary (one DB, RLS scoped, transactional with incidents). Turbopuffer when >10M vectors per tenant. |
| **Graph DB** | **Neo4j AuraDB Professional** (Pathfinder dependency graph + causal inference) |
| **Streaming (Sentinel)** | **Postgres windowed aggregates + Temporal cron** for v1. Add Flink/Kinesis only when streaming customers onboard. |
| **Event bus / queue** | **SQS FIFO** (validator job queue) + **SNS** (tenant-scoped fanout) + **EventBridge** (webhook ingress for GitHub/Sentry/ArgoCD with schema registry + DLQ). NO Kafka. |
| **Dev environment (now)** | **Docker Compose locally** — postgres, redis, control-plane, validator, gitops, web all in one `docker-compose.yml`. No AWS until Phase 7. Same Dockerfiles ship to ECS later. |
| **Compute (Phase 7+ cloud)** | **ECS Fargate** for all 3 services. NOT EKS (2 weeks of YAML you don't have). NOT Lambda (15-min limit kills LLM workflows). |
| **Validator sandbox (dev)** | **Local Docker** with read-only rootfs + `--network=none` + ephemeral container. Same interface as Modal so swap is one file. |
| **Validator sandbox (Phase 7+)** | **Modal.com** (managed Firecracker, scale-to-zero, ships fastest) → Firecracker-on-EC2 in v1.5. |
| **Cache** | **Redis 7 (docker-compose)** in dev → **ElastiCache Redis Serverless** in cloud. Same `redis://` DSN. |
| **Object storage** | **MinIO (docker-compose, S3-compatible)** in dev → **S3 + CloudFront** in cloud. Same SDK, swap endpoint. |
| **Secrets (infra)** | **`.env.local` + Doppler** in dev → **AWS Secrets Manager + KMS** in cloud. |
| **Secrets (customer credentials)** | **Per-tenant DEK** — in dev, master key is a 32-byte random in `.env.local`; in cloud, KMS holds it. Same envelope-encryption code, different KMS adapter. EncryptionContext binding — wrong `org_id` makes the call refuse. SOC 2 evidence (cloud only). |
| **CDN + edge** | None in dev (direct hit). **CloudFront** in cloud (signed URLs for log access; WAF for OWASP). |
| **Container registry** | **GHCR** in dev/CI → **ECR** in cloud. |
| **Load balancer** | **Caddy or Traefik (docker-compose)** in dev with auto-TLS for `nexis.local` → **ALB + AWS WAF** in cloud. |
| **Observability (dev)** | **Local Grafana stack** in docker-compose: Grafana + Loki + Tempo + Prometheus. OTel SDK exports OTLP to local OTel Collector. Switch endpoint to Grafana Cloud in Phase 7. |
| **App errors** | **Sentry self-hosted** optional in dev (skip if not needed) → **Sentry SaaS** in cloud. |
| **LLM provider — primary** | **OpenAI** (`gpt-4o` for synthesis, `gpt-4o-mini` for cheap classifications). Use OpenAI's prompt caching when available; otherwise rely on a thin in-process cache keyed by system-prompt hash. |
| **LLM provider — local / offline** | **Ollama** (default model `llama3.1:8b-instruct` for general tasks, `qwen2.5-coder:14b` for code synthesis if hardware allows). Use cases: offline FYP defense demo, privacy-sensitive customer trials, dev work without burning OpenAI credits. |
| **LLM provider — abstraction** | All agents call `internal/adapter/llm.Provider` interface. Concrete impls: `OpenAIProvider`, `OllamaProvider`, optional `AnthropicProvider` later. **Switch via `LLM_PROVIDER` env var** — no code change. |
| **Email transactional** | **MailHog (docker-compose)** in dev → **Resend** in cloud. |
| **Frontend framework** | **Next.js 16.2.2** App Router + **Tailwind v4** + **shadcn/ui** (Radix primitives, copy-paste, no lock-in) + **Tremor** (KPI/charts) + **Monaco** (diff viewer) + **next-themes** (light default + dark toggle) |
| **Landing page direction** | **Linear + Vercel-inspired**, LIGHT mode, premium-but-calm. Hero terminal + 6-step pipeline diagram + 9-agent grid + live pipeline demo embed |
| **Console direction** | **Linear + Vercel app-inspired** (sidebar + topbar + cmdk + inspector drawer). LIGHT default, dark toggle. shadcn primitives + Tremor charts + Monaco diffs |
| **Logo strategy** | Ship a **light-mode SVG variant** (`/public/logo-light.svg`) — light-blue strokes (#BFCFE8) → navy (#0B1220), brand-blue strokes (#3B82F6) stay. Wire via `<ThemeAwareLogo>` swap. **Do NOT invert via CSS** (hue shifts). |
| **CI/CD** | **GitHub Actions** (`ubuntu-latest-4-cores`) + **ArgoCD** (dogfood — we're shipping ArgoCD as a customer integration) |
| **IaC** | **Terraform** (modules per dependency: VPC, ECS, RDS, ElastiCache, S3, KMS) |
| **Testing** | Go: `testing` + `testcontainers-go` + `testify`. Frontend: **Vitest** unit + **Playwright** E2E. |
| **Billing** | None in MVP. Manual invoicing for design partners. **Stripe metered** in Phase 7. |

---

## 1. Pre-flight Checklist (Week 0)

Provision before writing code. Each item ~30–90 min. **Cloud-only items are deferred to Phase 7** — for Phases 1–6 everything runs locally in Docker.

### Required NOW (Phases 1–6 local Docker dev)

- [ ] **Local dev tooling** — Docker Desktop (allocate ≥ 8GB RAM + 4 CPU), Node 20 LTS, Go 1.22, Python 3.12, `pnpm`, `uv`, `direnv`, `buf`, `sqlc`, `golang-migrate`, `mkcert` (for local TLS).
- [ ] **OpenAI API key** — primary LLM provider. Set spend limit ($50/mo dev). Capture `OPENAI_API_KEY`. Use `gpt-4o` for synthesis, `gpt-4o-mini` for cheap classifications.
- [ ] **Ollama installed locally** — fallback / offline LLM. `brew install ollama` then `ollama pull llama3.1:8b-instruct` and (if disk + RAM permit) `ollama pull qwen2.5-coder:14b`. Verify `curl http://localhost:11434/api/tags` works.
- [ ] **GitHub App registration** — name `nexis-bot-dev` (separate prod app later); scopes: `repo`, `pull_request`, `checks`, `contents:write`, `actions:read`. Generate private key + webhook secret. **Use `smee.io` to forward GitHub webhooks to your local docker-compose.**
- [ ] **WorkOS account** — sandbox environment only for now. Capture API key, AuthKit Client ID, webhook secret. Configure `http://localhost:3000` as redirect URI.
- [ ] **GitHub repo** — `nexis-eco` monorepo, branch protection on `main`, required reviews = 1.
- [ ] **Doppler (or `.env.local`)** — local secrets vault. Nothing committed.
- [ ] **University paperwork** — supervisor signoff on scope, **ethics application submitted for NASA-TLX user study (8–10 week lead time — start now)**.

### Optional now (nice to have for parity with cloud)

- [ ] **Neo4j Desktop or `neo4j` Docker image** — Pathfinder dependency graph. Local container is fine.
- [ ] **Temporal locally** — `docker-compose` includes Temporal dev server (the `temporalio/auto-setup` image), no Temporal Cloud needed yet.
- [ ] **MinIO** — already in docker-compose (S3-compatible, local).
- [ ] **MailHog** — local SMTP catcher for transactional email testing.
- [ ] **Sentry self-hosted** — skip unless you need it; OTel + Grafana + slog covers most dev needs.

### Required at Phase 7 (cloud cutover)

- [ ] **AWS account** — root + IAM admin; Organizations + Budget alarm $200/mo.
- [ ] **Domain via Route53** — `nexis.dev` (or chosen); A/AAAA + TXT records.
- [ ] **WorkOS prod environment** — separate from sandbox.
- [ ] **GitHub App `nexis-bot` (prod)** — separate from dev.
- [ ] **Anthropic API key** *(optional, when migrating off OpenAI for cost/quality at scale)* — workspace + spend limit; or Bedrock access for enterprise customers.
- [ ] **Neo4j AuraDB Professional** — managed graph DB.
- [ ] **Modal.com account** — validator sandbox at scale.
- [ ] **Grafana Cloud** — free tier, OTLP endpoint + API token.
- [ ] **Sentry SaaS org** — production error tracking.
- [ ] **Temporal Cloud** — namespace `nexis-prod`; mTLS cert.
- [ ] **Resend** — verified domain.
- [ ] **Stripe** — live mode account.

---

## 2. Architecture Brief (from `lead-software-engineer`)

### 2.1 Why 3 services, not 9

Solo dev + 6–9 month timeline + 9 logical agents. Honest call: **modular monolith for the agent plane** (one runtime, 6 agent modules) **+ 2 separate services that have non-negotiable runtime/security boundaries**.

- 9 microservices = 9 deploys, 9 dashboards, 9 IAM roles, distributed tracing nightmare, cross-service contract drift you can't afford solo.
- Monolith with strict module boundaries (separate Go packages, no cross-module DB access, internal interfaces only via `internal/domain/ports.go`) ships in **half the time** AND can be split at a clean seam later.
- The 2 services that MUST be separate: **Validator** (pod-escape blast radius) and **GitOps** (holds GitHub App private key — minimize surface area).

### 2.2 Service / Module Topology

#### Service A: `nexis-control-plane` (Go + Chi, single ECS task, scaled horizontally)

Owns: HTTP/Connect-RPC API, tenancy middleware, auth, dashboard backend, Temporal workflow definitions, the 6 collapsed agent modules.

Internal modules (Go packages with strict boundaries enforced via `go-arch-lint`):

- `mod/sentinel` — Sentry/ArgoCD webhook ingestion + Postgres-backed anomaly aggregation. Owns: anomaly detection. Does NOT own: graph traversal.
- `mod/pathfinder` — Neo4j queries + DoWhy causal inference (Python sidecar via gRPC). Owns: causal RCA. Depends on: sentinel events.
- `mod/synthesiser` — Claude API client + prompt cache + pgvector retrieval + contract loader. Owns: patch generation. Does NOT own: validation execution.
- `mod/exec-team` — Architect / Backend / QA / DevOps / DataEng as **LLM personas** (shared infra: prompt registry, code-graph access, ADR store). Splitting these into 4 services = 4× prompt-management overhead for zero runtime benefit.
- `mod/approval-gate` — severity router + Slack/web UI signal handler. Owns routing decision + audit log.
- `mod/tenancy` — RLS enforcement + tenant context middleware.
- `mod/billing` — Stripe usage metering (Phase 7+).

#### Service B: `nexis-validator` (Go, separate VPC subnet, Modal.com offload in v1)

Owns: validation job dispatch to Modal sandbox (or DinD on Fargate in v1.5), property-test harness, distributed replay coordinator. Receives jobs from Temporal via SQS FIFO. **Hard isolation**: separate ECS cluster, no DB write access, ephemeral IAM role per job. Does NOT own: patch decisions.

#### Service C: `nexis-gitops` (Go, minimal surface)

Owns: GitHub App JWT minting, PR creation, ArgoCD webhook receipt, auto-revert trigger. Holds the GitHub App private key (only ECS task with KMS Decrypt permission on that secret). Pulls jobs from SQS.

### 2.3 Network + Security

- **VPC layout**: 3 private subnets (control-plane, validator, data), 2 public (ALB only). Validator subnet has NAT egress only to GitHub/registry, **zero ingress from control plane** — pulls jobs from SQS.
- **Service-to-service auth**: Skip mTLS / service mesh in v1 (Istio/Linkerd solo = no). Use VPC Security Groups + IAM-signed SQS messages + signed JWTs between services with short TTL via internal KMS-signed keys.
- **PrivateLink**: For Neo4j Aura, Anthropic (when GA), Temporal Cloud. Secrets Manager via VPC endpoint.
- **Multi-tenancy**: Row-level + Postgres RLS on every table, `org_id` from JWT injected via `SET LOCAL app.current_org_id`. Schema-per-tenant = migration hell at 100 customers. DB-per-tenant = cost suicide at 10. **App connects as a non-`BYPASSRLS` role.** Migrations run as a separate role.
- **Customer credentials**: Per-tenant DEK generated in KMS, encrypted with tenant-CMK, ciphertext + DEK ciphertext stored in `tenant_secrets`. Decrypt happens **only inside `gitops` service IAM boundary**, never logged, never crosses module.
- **Validator sandbox**: Modal task with no IAM beyond signed-URL S3 write to scoped prefix; network egress allowlist only; ephemeral filesystem; gVisor on roadmap.

### 2.4 Top 5 Risks + Mitigations

| Risk | Mitigation |
|---|---|
| Validator escape → lateral move | Modal-managed sandbox in v1 (already isolated); VPC subnet isolation, SQS-only pull, no Neo4j/RDS reach, IMDS blocked, ephemeral task per job, root FS read-only, Falco monitoring on roadmap. |
| LLM cost runaway | Per-tenant daily $ cap in Synthesiser middleware, Anthropic prompt cache (target ≥60% hit), Temporal activity-level token budget, CloudWatch alarm at 80% of monthly forecast, kill-switch via feature flag. |
| GitHub App private key compromise | Key only in `nexis-gitops` IAM-scoped Secrets Manager entry, rotated quarterly, signed JWT TTL = 10 min, all PR creations logged to immutable S3 (Object Lock), anomaly detection on PR rate per installation. |
| Cross-tenant data leak | RLS `FORCE` on every table, integration test suite that runs as `tenant_a` and asserts `SELECT *` from `tenant_b`'s rows returns 0, code review checklist, custom Go vet rule banning raw SQL outside `internal/adapter/repo/`. |
| Bad patch auto-deployed → outage | Severity router defaults to human-approval for ANY pattern unseen in last 30d; auto-approve only for patches matching known-good signature with confidence ≥ 0.95; ArgoCD progressive rollout (10%→50%→100%); auto-revert on SLO breach within 5 min; per-customer kill-switch. |

### 2.5 Estimated Monthly Infra Cost

**Dev (Phases 1–6) — local Docker**: $0 infra. Only spend = OpenAI credits (~$30–80/mo at moderate testing). Ollama = $0 (local compute). **Total dev runway: ≤ $80/mo.**

**Cloud (Phase 7+) — AWS at 10 customers, ~100 incidents/day** — itemized:

| Service | $/mo |
|---|---|
| ECS Fargate (control-plane, 2 tasks 1vCPU/2GB) | 75 |
| ECS Fargate (validator, ~3000 task-min/mo @ 2vCPU/4GB) OR Modal equivalent | 110 |
| ECS Fargate (gitops, 1 task 0.5vCPU/1GB) | 20 |
| ALB + WAF | 35 |
| RDS Postgres db.t4g.medium Multi-AZ + 100GB gp3 | 180 |
| Neo4j AuraDB Professional | 65 |
| ElastiCache Redis Serverless | 60 |
| S3 + CloudFront | 25 |
| SQS + SNS + EventBridge | 10 |
| Secrets Manager + KMS | 15 |
| ECR | 5 |
| VPC (NAT GW = the killer) | 70 |
| **AWS subtotal** | **670** |
| Temporal Cloud | 200 |
| Grafana Cloud Pro | 250 |
| LLM API — OpenAI gpt-4o (~3000 incidents/mo, prompt-cached) OR Anthropic equivalent | 500–700 |
| Resend | 20 |
| Sentry (NEXIS own) | 30 |
| **Total** | **~$1,775/mo** |

**At 100 customers, ~1000 incidents/day**: AWS scales to ~$3.5k, LLM dominates → ~$5–7k, Temporal ~$600, Grafana ~$600. **Total ~$11–13k/mo = $110–130/customer COGS.** Pricing at $500–1000/customer/mo = healthy gross margin.

---

## 3. Backend Stack + Standard Folder Layout (from `tech-lead`)

### 3.1 Auth Architecture (WorkOS + ours)

**Identity = WorkOS. Authorization (roles, scopes, RBAC, ABAC, API keys, audit) = ours.**

1. **Login (browser)**: User → WorkOS AuthKit hosted UI → JWT (RS256, 5-min access + refresh) → frontend stores in HttpOnly cookie → every API call carries it.
2. **JWT verification (every request)**: Backend caches WorkOS JWKS (1h TTL, refresh on `kid` miss). Verify signature, `iss`, `aud`, `exp`. Extract `sub` (WorkOS user ID) and `org_id` (WorkOS org ID).
3. **Webhook sync**: WorkOS → `/v1/webhooks/workos` → verify HMAC → upsert into our `users`, `organizations`, `organization_memberships`, `directory_users` (SCIM) tables. Idempotent on `external_id`.
4. **Just-in-time provisioning**: First time we see a `sub` not in our DB (race with webhook), upsert from the JWT claims so the request doesn't 500.

### 3.2 Local Tables We Own

```sql
organizations (id, workos_org_id, name, plan, created_at)
users (id, workos_user_id, email, created_at)
organization_memberships (org_id, user_id, role_id, joined_at)
roles (id, org_id NULL for system roles, name)
permissions (id, key)              -- 'incident:approve', 'agent:configure'
role_permissions (role_id, permission_id)
api_keys (id, org_id, name, prefix, hash, scopes[], last_used_at, expires_at, revoked_at)
audit_logs (id, org_id, actor_user_id, actor_api_key_id, action, target, metadata, created_at)
tenant_secrets (id, org_id, kind, encrypted_dek, ciphertext, created_at)
```

- **6 default roles**: `owner`, `admin`, `engineer`, `viewer`, `billing`, `service`
- **API keys**: server-generated `nx_live_<32 random bytes base62>`, `argon2id` hashed, 8-char prefix for lookup. Scopes are a subset of permissions. Rotated and revocable.
- **ABAC** evaluated in usecase layer when permission alone isn't enough — e.g. "engineer can approve incident only if `incident.team_id ∈ user.teams`". Plain Go predicates, not OPA.

### 3.3 Middleware Chain (Chi, exact order)

```go
r.Use(middleware.RequestID)       // 1. assign request_id, propagate via header
r.Use(middleware.Tracing)         // 2. otelhttp: start span, inject traceparent
r.Use(middleware.Logger)          // 3. slog with request_id + trace_id baggage
r.Use(middleware.Recoverer)       // 4. panic → 500 + log + alert
r.Use(middleware.Cors)            // 5. allowlist of frontend origins per env
r.Use(middleware.Auth)            // 6. JWT verify OR API key verify → ctx.Principal
r.Use(middleware.Tenant)          // 7. resolve org_id, SET LOCAL app.current_org_id
r.Use(middleware.RBAC)            // 8. check required permission for route
// ... handler runs ...
r.Use(middleware.Audit)           // 9. on success, write audit_log row async
```

Order rationale: RequestID before everything so logs/traces correlate. Recoverer before CORS so panics still send CORS headers. Auth before Tenant because we need the principal to know which orgs they belong to. Tenant before RBAC because RBAC may join `org_memberships`. Audit last so it sees the full outcome.

### 3.4 Multi-Tenant Context Propagation (the critical part)

```go
// platform/db/tx.go
func (db *DB) WithTenant(ctx context.Context, orgID uuid.UUID, fn func(pgx.Tx) error) error {
    return db.pool.BeginFunc(ctx, func(tx pgx.Tx) error {
        // SET LOCAL is scoped to this transaction. RLS policies read this GUC.
        if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", orgID); err != nil {
            return err
        }
        return fn(tx)
    })
}
```

Every tenant table has:

```sql
ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON incidents
    USING (org_id = current_setting('app.current_org_id')::uuid)
    WITH CHECK (org_id = current_setting('app.current_org_id')::uuid);
```

**Integration test for every tenant table**: create rows in two orgs, set `app.current_org_id` to org A, query, assert zero org-B rows. **This is your SOC 2 evidence.**

### 3.5 Customer-Supplied Secrets (KMS Envelope Encryption)

```
On write:
1. Generate fresh 256-bit DEK via crypto/rand
2. Encrypt plaintext with AES-256-GCM using DEK → ciphertext + nonce
3. KMS Encrypt(KeyId=alias/nexis-tenant-secrets, Plaintext=DEK,
               EncryptionContext={org_id, secret_kind}) → encrypted_dek
4. Store: { encrypted_dek, ciphertext, nonce, kms_key_arn, created_at }
5. Wipe DEK from memory

On read:
1. KMS Decrypt(encrypted_dek, EncryptionContext={org_id, secret_kind}) → DEK
   (EncryptionContext is integrity-checked: wrong org_id → KMS refuses)
2. AES-GCM decrypt ciphertext with DEK → plaintext
3. Use immediately, don't log, don't cache beyond request scope
```

EncryptionContext binding means even a stolen DB row can't be decrypted as a different tenant — auditor catnip.

### 3.6 Standard Folder Layout (every Go service follows this)

```
service-name/
├── cmd/
│   └── server/main.go            # entrypoint: wire deps, start server, graceful shutdown
├── internal/
│   ├── domain/                   # PURE business types + interfaces. Zero deps on infra.
│   │   ├── incident.go
│   │   ├── agent.go
│   │   ├── errors.go             # ErrNotFound, ErrForbidden, ErrConflict
│   │   └── ports.go              # interfaces: IncidentRepo, EventBus, SecretVault, LLMClient
│   │
│   ├── usecase/                  # application logic. Orchestrates domain + ports.
│   │   ├── incident/
│   │   │   ├── create.go
│   │   │   ├── approve.go        # signals Temporal workflow
│   │   │   └── create_test.go    # uses mock ports
│   │   └── agent/ingest_event.go
│   │
│   ├── adapter/                  # implementations of domain ports (driven side)
│   │   ├── repo/
│   │   │   ├── incident_pg.go    # implements domain.IncidentRepo via sqlc
│   │   │   └── queries/          # sqlc .sql files
│   │   ├── secrets/kms_vault.go
│   │   ├── workflow/temporal_client.go
│   │   └── llm/anthropic.go
│   │
│   ├── transport/                # driving side: how the world calls us
│   │   ├── http/
│   │   │   ├── server.go         # Chi router
│   │   │   ├── middleware/       # see §3.3
│   │   │   ├── handler/
│   │   │   └── dto/              # request/response DTOs with validator tags
│   │   ├── rpc/                  # Connect-RPC handlers (generated stubs in /gen)
│   │   └── worker/temporal.go    # Temporal worker registration
│   │
│   ├── platform/                 # cross-cutting infra wiring (not business logic)
│   │   ├── config/
│   │   ├── db/                   # pgx pool + WithTenant + migrations runner
│   │   ├── observability/        # OTel + slog setup
│   │   ├── auth/                 # WorkOS JWT verifier, API key hasher
│   │   └── crypto/               # KMS envelope encryption helpers
│   │
│   └── workflow/                 # Temporal workflow & activity definitions
│       ├── recovery_workflow.go
│       └── activities/
│           ├── diagnose.go
│           └── validate.go
│
├── gen/                          # generated code (commit this; do not hand-edit)
│   ├── proto/                    # buf-generated Connect handlers
│   └── sqlc/                     # sqlc-generated query code
│
├── migrations/                   # golang-migrate, numbered + reversible
│   ├── 0001_init.up.sql
│   └── 0001_init.down.sql
│
├── test/
│   ├── integration/              # testcontainers Postgres + migrations
│   └── e2e/
│
├── deploy/
│   ├── Dockerfile                # distroless final stage
│   └── helm/                     # ArgoCD chart
│
├── buf.gen.yaml
├── sqlc.yaml
├── .arch.yaml                    # go-arch-lint rules (below)
├── Makefile                      # make build|test|lint|gen|migrate
└── go.mod
```

### 3.7 Layering Rules (enforced by `go-arch-lint`)

```
domain    → (nothing — pure)
usecase   → domain
adapter   → domain, platform
transport → usecase, domain, platform, gen
workflow  → usecase, domain, adapter, platform
platform  → (stdlib + 3rd party only; no internal pkgs)
cmd       → everything (composition root)
```

**Hard rules:**
- `domain` imports nothing from `internal/*`. Period.
- `usecase` depends only on `domain` interfaces — never on a concrete adapter.
- `transport` never talks to `adapter` directly — always through a usecase.
- `adapter` never imports `transport` or `usecase`.
- DTOs in `transport/http/dto` never leak into `domain` — map at the edge.

CI fails the build if a PR violates these. **Non-negotiable.**

---

## 4. UI Direction (from `saas-ui-designer`)

### 4.1 Landing Page (LIGHT MODE)

**References**: Linear (linear.app) + Vercel (vercel.com). Calm authority, monochrome base + one accent, animated terminal/pipeline embeds that breathe rather than shout.

**Color tokens (HSL)**:
```css
--background:        0 0% 100%
--foreground:        222 47% 11%       /* #0B1220 navy */
--muted:             210 20% 96%
--muted-foreground:  215 16% 47%
--border:            214 32% 91%
--brand-primary:     217 91% 60%       /* #3B82F6 — matches logo */
--brand-accent:      217 60% 82%       /* #BFCFE8 — matches logo */
--brand-deep:        222 47% 14%
--success:           160 84% 39%
--warning:           38 92% 50%
--danger:            0 72% 51%
```

**Typography**: Display = **Inter Tight** 700/600 (-0.03em). Body = **Inter** 400/500. Mono = **JetBrains Mono** 400/500.

**Type scale**: display 72px / h1 56px / h2 36px / h3 22px / body 16px / small 13px.

**Spacing**: 4 / 8 / 12 / 16 / 24 / 32 / 48 / 64 / 96 / 128. **Radius**: 6 (controls), 10 (cards), 16 (large surfaces), 999 (pills).

**Motion**: Framer Motion `whileInView` + `viewport={{ once: true, margin: "-80px" }}`, `duration: 0.6`, ease `[0.22, 1, 0.36, 1]`. Stagger 60ms. `prefers-reduced-motion` → opacity only. **One** autoplay loop allowed: hero pipeline terminal.

**9 sections**:

1. **Sticky navbar** — 64px white with `backdrop-blur`, 1px bottom border appears after 8px scroll. Logo + 5 nav links + Sign in (ghost) + Get started (primary).
2. **Hero (center-stacked)** — Eyebrow: "Autonomous engineering, supervised by you." Headline: **"Nine AI agents. One engineering team that ships fixes while you sleep."** Sub: detect → diagnose → patch → validate → approve → deploy. Two CTAs. Below the fold: contained animated terminal showing the 6-step loop looping subtly.
3. **Trusted by strip** — 56px `--muted` band, grayscale logos.
4. **Problem section** — 3-column cards: MTTR is hours / engineers do toil / post-mortems by the exhausted.
5. **How it works** — Full-bleed light section with horizontal pipeline: Detect → Diagnose → Synthesise → Validate → Approve → Deploy. Glowing token traverses on scroll, pauses at Approve gate.
6. **The 9 Agents** — 3×3 grid. Each card: agent ID + role + Owns / Does not own. Hover lifts to `shadow-md` with brand-tinted ring.
7. **Live pipeline demo embed** — Real interactive widget driven by SSE replay. Scenario picker (left) + animated canvas (center) + scrolling terminal log (right). "Run a synthetic incident" CTA.
8. **Metrics strip** — Three giant numbers in display weight: **MTTR ↓ 60%** | **≥ 80% patch correctness** | **< 90s detection.** Count up on scroll into view.
9. **Final CTA + footer** — Centered "Give your team back its weekends." Footer: 5 columns + status + SOC2-in-progress badges.

### 4.2 Logo Strategy

**Decision: ship `/public/logo-light.svg` variant.**
- Light-blue strokes (`#BFCFE8`) → **`#0B1220`** (brand-deep navy)
- Brand-blue strokes (`#3B82F6`) → **stay `#3B82F6`**

Wire via a `<ThemeAwareLogo>` component that swaps `src` based on resolved theme. **Do not invert via CSS filters** — hue shifts.

### 4.3 Console / Admin Dashboard

**Framework**: **shadcn/ui (Radix + Tailwind v4) + Tremor (charts) + Monaco (diffs).** Reasoning: NEXIS console is engineer-facing, dense, bespoke (live SSE pipeline canvas, Monaco diff viewers, custom agent grids, audit-log timelines, RLHF widgets). Off-the-shelf admin frameworks (Ant Pro, Refine, Mantine) impose layout opinions that fight the custom canvas. shadcn = Radix accessibility + Tailwind tokens shared 1:1 with landing + zero runtime lock-in. Tremor for KPI cards + time-series. Monaco for diffs. **Same stack as Vercel / Resend / Supabase consoles.**

**Dark mode tokens**:
```css
--background: 222 47% 6%        /* #0A0E1A */
--surface:    222 40% 9%
--card:       222 40% 11%
--border:     217 19% 20%
--foreground: 210 20% 96%
--muted-fg:   215 16% 65%
--brand:      217 91% 65%
```
Persisted via `next-themes`, default `light`, system-respecting opt-in.

**Typography**: Same family as landing but **14px base** for density.

**Layout chrome**:
- **Sidebar**: 240px expanded / 64px collapsed (icon-only). Persisted state. Sections: **Workspace** (Home, Incidents, Approvals, Audit), **Platform** (Agents, Integrations, Live Demo), **Settings** at bottom. Org switcher pinned top, user menu pinned bottom.
- **Topbar**: 56px. Left: breadcrumb. Center: command palette trigger (`⌘K`) — full-width-ish, "Search incidents, agents, approvals..." Right: theme toggle, notifications popover, help.
- **Main**: `max-w-[1440px]`, `px-6 py-6`; data-table pages full-width inside that.
- **Right inspector drawer**: 480px slide-over for incident / agent detail (Linear pattern).

**References**: Linear + Vercel app.

**8 surfaces** (one paragraph each):

1. **Home** — Greeting (`Good afternoon, {firstName}`) + 3-action quick grid + 4 KPI cards (Open incidents, Awaiting approval, MTTR 7d, Patch acceptance %). Two-column below: latest activity (60%) + get-started checklist (40%).
2. **Incidents** — `DataTable` (ID · Severity · Service · Status · Detected · Owner · Agents) + filters bar. Row click → `/incidents/[id]` with left rail Recovery Timeline, main pane tabs (Overview / RCA / Patch diff Monaco / Validation / Logs), sticky **Approval Action Bar** on `awaiting_approval`.
3. **Approvals queue** — Pre-filtered Incidents to `status = awaiting_approval`. Each row expandable inline showing patch summary + 3 fastest-to-decide signals. Inline Approve/Reject. Bulk-approve disabled by policy.
4. **Audit log** — Append-only `DataTable` (Timestamp · Actor · Action · Resource · IP · Hash). CSV export. Row click → side sheet with full JSON + cryptographic hash chain link.
5. **Integrations** — Card grid: GitHub, Sentry, ArgoCD, Datadog, PagerDuty, Slack. Status badge + last-sync. Configure → shadcn Dialog with OAuth/token + scope checklist.
6. **Settings** — 200px sub-sidebar: Profile / Organization / Members & Roles / Approval Policies / API Keys / Notifications / Billing. Approval Policies marquee surface = rule builder + simulator preview.
7. **Agents fleet panel** — 3×3 grid of 9 agent cards (layer badge, live status dot, current task, p95 latency). Click → `/agents/[id]` with tabs Tasks / Performance (Tremor) / Reasoning / Audit (hash-chained) / Delegation (sankey) / RLHF.
8. **Live Pipeline Demo** — Three-pane: scenario picker (240px left) + `<PipelineCanvas>` (center, SSE-driven) + terminal log pane (right 400px). Run / Pause / Reset + speed slider.

**Empty states**: every list/table ships an illustrated empty state — centered light SVG (line-art, brand-blue accent) + one-sentence what + one-sentence do + primary CTA. **No "No data" text-only states.**

**Loading**: shadcn `Skeleton` shape-matched to final content. Shimmer respects `prefers-reduced-motion`.

**Errors**: Per-route `error.tsx` with calm card showing error category (not stack), `Try again` + `Copy error ID` + status page link.

### 4.4 Auth UX Flows (handled by WorkOS AuthKit hosted UI)

- **Sign in** — Centered 400px card; logo top, email + password, "Continue with GitHub" + Google SSO above divider, magic-link fallback.
- **Sign up** — Same shell, name + email + password, inline strength meter, terms checkbox, post-submit verification screen.
- **Invite-accept** — Token-validated landing showing inviter avatar + org + role; user sets password (or SSO) and lands on Home with toast.
- **MFA setup** — Two-step wizard (TOTP / passkey / SMS), QR + recovery codes screen with explicit "I've saved these" gate.
- **Passkey enrollment** — Single-screen prompt explaining benefit, large "Create passkey" button → WebAuthn → success state with device nickname.

---

## 5. Phased Delivery (from `planner`)

### Phase 1 — Foundations & Landing (Weeks 1–3) — **LOCAL DOCKER** — Completed 2026-05-11

**Scope**
- Monorepo scaffold (`apps/web`, `apps/api`, `services/{control-plane,validator,gitops}`, `packages/ui`, `packages/db`, `infra/`).
- **`docker-compose.yml`** in repo root with services: `postgres` (16 + pgvector), `redis` (7), `minio` (S3-compatible), `mailhog`, `caddy` (reverse proxy with auto-TLS for `*.nexis.local`), `temporal` (dev server), `neo4j` (community), `otel-collector`, `grafana` + `loki` + `tempo` + `prometheus`. Plus the app services: `web`, `control-plane`, `validator`, `gitops`.
- Local TLS via `mkcert` + Caddy → `https://app.nexis.local`, `https://api.nexis.local`.
- GitHub Actions: lint + typecheck + test + Docker build on PR. (Cloud deploy = Phase 7.)
- Next.js 16.2.2 landing page (LIGHT MODE), shadcn/ui + Tailwind v4 + Framer Motion. All 9 sections per §4.1.
- Generate `/public/logo-light.svg` per §4.2.
- sqlc + Drizzle (web only) baseline schemas: `tenants`, `users`, `audit_log`.
- **LLM provider abstraction** in `services/control-plane/internal/adapter/llm/` — `Provider` interface + `OpenAIProvider` + `OllamaProvider` impls, switched by `LLM_PROVIDER` env var (default `openai`, can flip to `ollama`).

**Deliverables** — `docker compose up` brings the entire stack up healthy; landing renders at `https://app.nexis.local`; CI green.

**Success criteria**
- `docker compose ps` → all services healthy in < 60s on a clean `docker compose up`.
- `curl -k -I https://app.nexis.local` → 200.
- Lighthouse perf ≥ 92, a11y ≥ 95 on the landing page.
- Waitlist POST persists row visible via `psql` in the postgres container.
- `LLM_PROVIDER=ollama curl https://api.nexis.local/v1/_diag/llm` → returns model info from local Ollama.

**Sub-agents** — `frontend-engineer` (landing) + `backend-engineer` (docker-compose + LLM abstraction + scaffolds). Parallel — disjoint surfaces.

**Dependencies** — Pre-flight complete (the "Required NOW" subset).

---

### Phase 2 — Auth, Tenancy, Observability (Weeks 4–6) — Completed 2026-05-12

**Scope**
- WorkOS integration (passwords + magic + GitHub OAuth + passkeys + MFA + SAML + SCIM + API keys).
- Tenant provisioning flow: signup → tenant row → owner role → default workspace.
- Postgres RLS on every tenant table (`SET LOCAL app.current_org_id`).
- OpenTelemetry SDK in `apps/api` + `apps/web`; OTLP exporter → Grafana Cloud.
- Sentry wired both runtimes.
- Audit log writer — every mutation appends signed row.

**Deliverables** — Signup → login → dashboard shell route protected; trace visible in Grafana; audit row written.

**Success criteria**
- Playwright: signup → MFA enroll → logout → login passes.
- SQL `SET app.current_org_id='X'` then `SELECT * FROM incidents` returns only tenant X rows (RLS verified).
- Grafana shows distributed trace spanning web → api.

**Sub-agents** — `backend-engineer` (auth + RLS) + `lead-software-engineer` (OTel pipeline).

**Dependencies** — Phase 1.

---

### Phase 3 — Console Shell + Integrations (Weeks 7–9) — Completed 2026-05-13

**Scope**
- Admin console with 8 navigation surfaces stubbed per §4.3. LIGHT MODE default + dark toggle.
- Integrations surface end-to-end: GitHub App install (per-tenant), Sentry webhook receiver (HMAC verified), ArgoCD API token entry.
- Settings: org profile, API key CRUD, member invites, role matrix.
- Audit surface lists rows with filters.

**Deliverables** — Console you can demo to a design partner. GitHub install round-trips. Sentry webhook ingests a test event.

**Success criteria**
- Install GitHub App on a real repo → callback persists `installation_id` → console "Connected" badge appears.
- Send test Sentry event → row in `incidents_raw` table within 5s.
- Theme toggle persists per user.

**Sub-agents** — `frontend-engineer` (shell + 4 surfaces) + `backend-engineer` (integrations + webhooks). Parallel — split by file.

**Dependencies** — Phase 2.

---

### Phase 3.5 — Workspaces + Billing (Week 9.5)

**Scope**
- Workspace as the compute unit inside an org. After signup, owner is redirected to onboarding to create their first workspace (name + region from 6 fake datacenters).
- Provisioning state-machine animation: creating organization → allocating host → provisioning datacenter → deploying → ready. Streamed via SSE, ~6 seconds total.
- Workspace switcher in the sidebar; selected workspace badge in topbar with region.
- Billing: Stripe-style payment method form (mocked locally; real Stripe in Phase 7) + invoice list + current-period usage breakdown per workspace per project. AWS-style per-hour runtime metering — control-plane cron records `usage_records` for active workspaces.
- New Settings sub-route: Billing.

**Deliverables** — Signup → onboarding wizard → see provisioning animation → land on console with workspace + region visible. Settings → Billing shows payment method form and a usage summary.

**Success criteria**
- New user signup redirects to `/onboarding/workspace`; cannot reach `/console` until a workspace exists.
- Provisioning animation completes; workspace row appears with `status=ready` and a region.
- Settings → Billing accepts a (mocked) card; payment method displayed as `Visa ••••4242`.
- `usage_records` table has runtime-hour rows for the live workspace.

**Sub-agents** — `backend-engineer` (workspaces + billing + SSE) + `frontend-engineer` (onboarding animation + switcher + billing UI).

**Dependencies** — Phase 3.

---

### Phase 4 — Pipeline Substrate: Temporal + Sandbox (Weeks 10–12) — **LOCAL DOCKER**

**Scope**
- Temporal **dev server** (already in docker-compose from Phase 1) + worker scaffolding in `services/control-plane/internal/workflow`.
- Define workflow `RecoveryPipeline` with 9 placeholder activities — each just logs + sleeps; full DAG wired with retries+timeouts.
- Validator sandbox **as a local Docker run** (the `validator` service spawns a `docker run --rm --network=none --read-only --tmpfs /tmp <patched-image> pytest`) accepting `{repo_sha, patch_diff}` returning `{tests_passed, coverage, logs}`. Same interface as the Phase 7 Modal swap.
- Patch storage: **MinIO bucket per tenant**, envelope-encrypted via local master key (§3.5 — same code, dev KMS adapter).
- Pipeline UI: read-only timeline view of a workflow run via SSE.

**Deliverables** — Trigger workflow from console → see all 9 activity steps light up in order in UI + Temporal Web.

**Success criteria**
- `temporal workflow start RecoveryPipeline` completes < 60s with stub activities.
- Sandbox runs `pytest` on a fixture repo, returns pass/fail JSON, container destroyed.

**Sub-agents** — `lead-software-engineer` (Temporal + sandbox infra) + `tech-lead` (DAG design review).

**Dependencies** — Phase 3.

---

### Phase 5 — Agents L1 + LLM Spine (Weeks 13–15) — **OPENAI + OLLAMA DUAL-MODE**

**Scope**
- LLM `Provider` interface fully fleshed out: `OpenAIProvider` (gpt-4o for synthesis, gpt-4o-mini for classifications, OpenAI prompt caching enabled where supported, in-process system-prompt cache otherwise) + `OllamaProvider` (default `llama3.1:8b-instruct`, `qwen2.5-coder:14b` for code synthesis when available).
- All agents call `llm.Provider` only — no direct vendor SDK imports outside `internal/adapter/llm/`.
- Per-call structured-output enforcement: JSON-schema validation, retry up to 2× on schema mismatch, fall back from gpt-4o → gpt-4o-mini if budget tight.
- Implement L1 agents as Temporal activities: Architect (plan), Backend (codegen), QA (test gen), DevOps (yaml), Data Engineer (migrations).
- Each agent: typed input/output, retry policy, deterministic prompt template, structured output (JSON schema).
- Cost guardrails: per-tenant monthly token budget enforced before LLM call. Ollama runs count as $0.
- pgvector seeded with synthetic codebase chunks for retrieval.
- **Eval harness**: same incident run with `LLM_PROVIDER=openai` and `LLM_PROVIDER=ollama`, output diff captured. Used in Phase 8 baselines.

**Deliverables** — Workflow now produces real agent outputs end-to-end against a synthetic incident; transcripts visible in console. Both OpenAI and Ollama paths verified.

**Success criteria**
- `LLM_PROVIDER=openai`: synthetic incident → Architect plan + Backend patch + QA tests in < 90s, cost < $0.40/run logged.
- `LLM_PROVIDER=ollama` (on a laptop with 16GB RAM + qwen2.5-coder:14b): same flow completes < 5 min, $0 cost, ≥ 60% test-passing patch quality vs OpenAI baseline.
- OpenAI prompt-cache hit rate ≥ 60% after 5 runs (where cache headers supported).

**Sub-agents** — `backend-engineer` (agent activities); use `claude-api` skill knowledge for caching patterns (concepts transfer to OpenAI / Ollama).

**Dependencies** — Phase 4.

---

### Phase 6 — Agents L2 + Approval Gate (Weeks 16–18) `← MVP CUT-LINE`

**Scope**
- **Sentinel** — streaming anomaly detector subscribed to Sentry + OTel metrics; emits `IncidentDetected`.
- **Pathfinder** — Neo4j codegraph + DoWhy causal inference (start with one heuristic + DoWhy on metric series) → root-cause hypothesis.
- **Synthesiser** — orchestrates retrieval + delegates to Backend or Data Engineer L1.
- **Validator** — runs sandbox + property-based tests (Hypothesis for Python fixtures).
- **Approval Gate** — severity router: `low` → auto-merge; `medium` → notify + 2-min countdown then auto; `high/unknown` → human required. Slack + email + console.
- **GitOps service** — opens PR on tenant repo via GitHub App with patch + agent transcript + risk score.
- ArgoCD app-of-apps wired; PR merge triggers sync; rollback on SLO breach (Argo Rollouts undo).
- **Live Pipeline Demo** surface: button "Inject fault" → fixture repo + fixture incident → full loop runs visibly.

**Deliverables** — End-to-end: fault → detection → patched PR → preview env → approve → deploy → rollback path tested. **This is the MVP demo.**

**Success criteria**
- Demo script: inject null-pointer fault into fixture → PR opened in < 5 min → human approves → deployed → synthetic SLO breach triggers rollback. Recorded video.
- 3 design partners walk through Live Demo without intervention.
- All audit rows present.

**Sub-agents** — `planner` first, then split: `backend-engineer` (Sentinel + Pathfinder + Synthesiser + Validator + GitOps) + `lead-software-engineer` (ArgoCD + Argo Rollouts) + `frontend-engineer` (Live Demo + Approvals UI).

**Dependencies** — Phase 5.

---

### Phase 7 — Cloud Cutover + Hardening + Billing (Weeks 19–21) — **AWS LIVE**

**Scope** (this is when local-Docker dev becomes a real cloud SaaS)
- **Terraform** for AWS: VPC (3 private + 2 public subnets), ECS Fargate cluster, RDS Postgres 16 Multi-AZ + pgvector, ElastiCache Redis Serverless, S3 buckets (artifacts/audit/fixtures), KMS (per-env CMK + per-org alias), Secrets Manager, ALB+ACM+Route53.
- **Cloud swap of the abstractions**: `S3Provider` replaces `MinIOProvider`, `KMSVault` replaces `LocalKeyVault`, `SecretsManagerStore` replaces `.env` loader, `ResendMail` replaces `MailHog`. Same interfaces — switching is 1 file each.
- **Temporal Cloud namespace** `nexis-prod`; swap dev-server endpoint.
- **Modal.com** for validator sandbox at scale (replaces local docker-run).
- **Neo4j AuraDB Professional** (replaces local container).
- **Grafana Cloud** OTLP endpoint (replaces local stack); dashboards + alerts committed under `infra/grafana/`.
- **GitHub Actions deploys**: build images → push to ECR → bump tag in `infra/k8s/<svc>/values.yaml` → ArgoCD picks up → staging auto-syncs, prod manual.
- **Stripe metered billing** (incidents processed + tokens); webhook → entitlements.
- **SOC 2-lite**: audit immutability (Postgres trigger + nightly hash to S3 Object Lock), backup/restore drill, IR runbook.
- **Per-tenant Redis token-bucket** rate limits.
- **Pen-test pass** on auth + sandbox (1-week consultant or `trivy` + manual checklist).
- **Load test**: 100 concurrent workflows on staging — fix bottlenecks.

**Deliverables** — Live at `https://app.nexis.dev`; same workflows that ran in docker-compose now run on Fargate; paid plan switch flips real billing; tenant isolation report; load test PDF.

**Success criteria**
- All Phase 1–6 acceptance tests still pass against the cloud env (provider swap = transparent).
- k6: 100 concurrent runs, p95 < 8 min, 0 cross-tenant leaks.
- Stripe test charge succeeds, entitlement updates within 30s.

**Sub-agents** — `lead-software-engineer` (Terraform + ECS + cloud cutover) + `backend-engineer` (billing) + `code-reviewer` multi-agent (security + tenant audit).

**Dependencies** — Phase 6.

---

### Phase 8 — Public Beta + Eval Lock-in (Weeks 22–24)

**Scope**
- Onboarding flow polish (sample repo wizard, in-app tour with Shepherd.js).
- Docs site (Nextra) at `docs.nexis.dev`.
- Status page + uptime monitoring (BetterStack).
- Evaluation track final results frozen (see §7).
- Thesis writing sprint — chapters 1–4.

**Deliverables** — Public signup open behind invite code; 5+ pilot tenants live; thesis draft.

**Success criteria** — 50 waitlist → 10 active tenants → ≥ 3 successful auto-recoveries in production. Thesis sent to supervisor.

**Sub-agents** — `frontend-engineer` (onboarding + docs) + `tech-lead` (thesis system chapter review).

**Dependencies** — Phase 7.

---

### Phase 9 — Polish & Defense (Weeks 25–28, optional buffer to wk 36)

**Scope** — Bug burn-down from beta feedback; performance pass (cache warming, prompt compaction); thesis revisions + defense slides + rehearsal; marketing (case studies, Show HN, Product Hunt).

**Deliverables** — Defended thesis; v1.0 tag; launch announcement.

**Success criteria** — Defense passed; launch day no P0 incident.

**Sub-agents** — `debugger` (bug burn-down) + `code-reviewer` multi-agent (release audit).

**Dependencies** — Phase 8.

---

## 6. MVP Cut-line

**Phase 6 = MVP.** End of week 18 you can demo full closed-loop recovery to design partners and your supervisor. Phases 7–9 are monetisation, compliance, and polish — not required for "the system works."

---

## 7. Parallel Eval Track

| Build phase | Eval work running alongside |
|---|---|
| 1–2 | Ethics application submitted (university IRB ~8wk turnaround). Literature review for thesis chap 2. |
| 3 | Build fault-injection harness (`chaos/` package): 30 fault classes (null deref, schema drift, OOM, race, deploy fail, etc). |
| 4 | Define metrics: MTTR, fix-precision, fix-recall, human-intervention rate, $/incident. |
| 5 | Implement baselines: (a) PagerDuty + human, (b) single-LLM fix-suggestion (Claude direct, no pipeline), (c) human-only control. |
| 6 | Run benchmark: NEXIS vs 3 baselines × 30 faults × 5 seeds. Capture all metrics. |
| 7 | Recruit 12 engineers for NASA-TLX user study (within-subjects, NEXIS vs PagerDuty). Run sessions. |
| 8 | Statistical analysis (Wilcoxon signed-rank for TLX, bootstrap CI for MTTR). Freeze numbers. Thesis chap 4. |
| 9 | Defense rehearsal with live demo. |

---

## 8. Risk Register

| # | Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| 1 | **Scope creep (solo dev)** | High | Critical | Phase scope is contractual. Anything outside the phase → `BACKLOG.md`, not the current sprint. Weekly self-review against this doc. |
| 2 | **LLM cost runaway** | High | High | Per-tenant token budget hard cap. Prompt caching enforced (target ≥ 60% hit). Spend dashboard checked weekly. Synthesiser caps at 3 retry attempts. |
| 3 | **Validator sandbox escape** | Medium | Critical | Modal.com (already isolated). No network egress except allowlist. Read-only rootfs. Pen-test in Phase 7. Never run untrusted patches without sandbox. |
| 4 | **Multi-tenant data leak** | Medium | Critical | RLS enforced at Postgres level (Phase 2), not app level. Every query path covered by integration test asserting cross-tenant 404. Code review checklist mandates `org_id` filter. |
| 5 | **Vendor lock-in (WorkOS)** | Low | Medium | Auth abstraction layer — `IIdentityProvider` interface, WorkOS is one impl. Migration to Clerk/self-host = swap one file. Same for Modal (Firecracker fallback documented). |
| 6 | **Temporal ops complexity** | Medium | Medium | Use **Temporal Cloud**, not self-hosted. Cost ~$200/mo accepted. |
| 7 | **Design-partner ghosting** | High | Medium | Recruit 5 partners by end of Phase 3 (oversubscribe). Weekly 20-min check-ins. If 3 ghost, you still have 2. Live Demo (Phase 6) lets cold prospects self-serve. |
| 8 | **Thesis defense scheduling** | Medium | High | Book defense slot at Phase 6 completion (12-week lead). If slip, Phase 9 buffer absorbs. Ethics submission week 0 — non-negotiable critical-path. |

---

## 9. Definition of Done (Whole Project)

- [ ] All 9 agents implemented, tested, observable.
- [ ] Closed-loop recovery demonstrated on ≥ 3 real production incidents at design partners.
- [ ] Multi-tenant RLS verified by integration tests + external review.
- [ ] Auth supports all 8 methods (password, magic, OAuth, passkey, MFA, SAML, SCIM, API key) — Playwright suite green for each.
- [ ] OTel traces, metrics, logs flowing to Grafana for every workflow span.
- [ ] Stripe billing live, ≥ 1 paying customer.
- [ ] Status page, docs site, runbook published.
- [ ] Eval benchmark: NEXIS beats baselines on MTTR (p < 0.05) and NASA-TLX (lower workload).
- [ ] Thesis defended, grade recorded.
- [ ] v1.0 tagged, launch post live, no P0 in first 7 days.

---

## 10. Cursor Agent Prompts (Copy-paste, one per phase)

**Phase 1** — `Bootstrap NEXIS monorepo for LOCAL DOCKER dev (cloud comes in Phase 7). Stack: pnpm + turborepo, Next.js 16.2.2 (apps/web, light-mode landing per docs/PROJECT_PLAN.md §4.1), Go 1.22 + Chi (services/control-plane skeleton with internal/{domain,usecase,adapter,transport,platform,workflow} per §3.6), sqlc + pgx (packages/db). Create docker-compose.yml at repo root with services: postgres-16-pgvector, redis-7, minio, mailhog, caddy (reverse proxy with mkcert TLS for *.nexis.local), temporalio/auto-setup (dev server), neo4j community, otel-collector, grafana+loki+tempo+prometheus, plus the app services web/control-plane/validator/gitops. Implement landing page (9 sections per §4.1) using shadcn/ui + Tailwind v4 + Framer Motion. Generate /public/logo-light.svg variant per §4.2. Implement LLM Provider abstraction in services/control-plane/internal/adapter/llm/ with OpenAIProvider + OllamaProvider impls, default openai, switchable via LLM_PROVIDER env. Add GitHub Actions: lint+typecheck+test+docker-build on PR. Acceptance: docker compose up brings stack healthy in <60s, https://app.nexis.local renders landing, Lighthouse perf ≥ 92, LLM_PROVIDER=ollama curl /v1/_diag/llm returns model info from local Ollama.`

**Phase 2** — `Add WorkOS auth (password, magic link, GitHub OAuth, passkey, MFA, SAML, SCIM, API keys) to apps/web + services/control-plane. Implement tables per §3.2 with sqlc. Enforce Postgres RLS keyed by current_setting('app.current_org_id') with WithTenant tx wrapper per §3.4. Wire OpenTelemetry SDK in both runtimes exporting OTLP to Grafana Cloud. Wire Sentry. Implement middleware chain in exact order per §3.3. Acceptance: Playwright signup→MFA→login passes; RLS test proves cross-tenant isolation; trace visible in Grafana.`

**Phase 3** — `Build admin console shell with 8 nav surfaces per §4.3 (Home, Incidents, Approvals, Audit, Integrations, Settings, Agents, Live Demo). LIGHT MODE default, dark toggle via next-themes. Implement Integrations surface fully: GitHub App install flow (store installation_id per tenant), Sentry webhook receiver with HMAC verification writing to incidents_raw, ArgoCD token entry. Settings: API key CRUD per §3.2, member invites with role matrix. Acceptance: GitHub install round-trips, Sentry test event ingests in <5s, theme persists per user.`

**Phase 4** — `Use the local Temporal dev server already in docker-compose. In services/control-plane/internal/workflow define RecoveryPipeline workflow with 9 placeholder activities matching the agent fleet, full DAG wired with retries+timeouts. Build validator sandbox as a local Docker run: services/validator spawns docker run --rm --network=none --read-only --tmpfs /tmp <patched-image> pytest, accepting {repo_sha, patch_diff} returning {tests_passed, coverage, logs}. Encrypted patch storage in MinIO via envelope encryption per §3.5 (LocalKeyVault adapter; KMS adapter swaps in Phase 7). Console: read-only pipeline timeline view subscribed via SSE. Acceptance: temporal workflow start RecoveryPipeline completes <60s with stubs, sandbox runs pytest on fixture repo and self-destructs.`

**Phase 5** — `Implement L1 agents (Architect, Backend, QA, DevOps, Data Engineer) as Temporal activities calling the llm.Provider interface (NOT direct vendor SDKs). Use OpenAIProvider (gpt-4o synthesis, gpt-4o-mini classifications, prompt caching where supported + in-process system-prompt cache fallback) AND OllamaProvider (llama3.1:8b-instruct general, qwen2.5-coder:14b for code synthesis if RAM permits). Each agent: typed input, JSON-schema structured output with retry-on-mismatch (max 2), retry policy, per-tenant token budget enforced pre-call. Seed pgvector with synthetic codebase chunks for Backend agent retrieval. Emit token+cost telemetry per workflow (Ollama = $0). Build eval harness running same incident through both providers, capture output diff. Acceptance: LLM_PROVIDER=openai produces real plan+patch+tests in <90s for <$0.40 with cache hit ≥60% after 5 runs; LLM_PROVIDER=ollama on a 16GB laptop with qwen2.5-coder:14b completes same flow in <5min for $0 with ≥60% test-passing patch quality vs OpenAI baseline.`

**Phase 6** — `Implement L2 agents + Approval Gate + GitOps. Sentinel: streaming anomaly detector subscribed to Sentry+OTel metrics emits IncidentDetected. Pathfinder: Neo4j codegraph queries + DoWhy causal inference (Python sidecar via gRPC) returns root-cause hypothesis. Synthesiser: orchestrates retrieval + delegates to Backend/Data Engineer L1. Validator: sandbox + Hypothesis property-based tests. Approval Gate: severity router (low=auto, medium=2min countdown, high=human-required) with Slack+email+console notify. GitOps service: opens PR on tenant repo via GitHub App with patch + agent transcript + risk score. ArgoCD app-of-apps wired with Argo Rollouts auto-rollback on SLO breach. Build Live Pipeline Demo surface per §4.3 surface 8: "Inject fault" button runs full real loop on fixture repo. Acceptance: end-to-end demo recorded, 3 design partners self-serve Live Demo successfully.`

**Phase 7** — `CLOUD CUTOVER from local Docker to AWS. Terraform (infra/): VPC + ECS Fargate cluster + RDS Postgres 16 Multi-AZ + pgvector + ElastiCache Redis Serverless + S3 + KMS (per-env CMK + per-org alias) + Secrets Manager + ALB+ACM+Route53 for nexis.dev. Cloud-swap each abstraction: S3Provider replaces MinIOProvider, KMSVault replaces LocalKeyVault, SecretsManagerStore replaces .env loader, ResendMail replaces MailHog (one-file swap each — same interface). Switch Temporal to Cloud namespace nexis-prod. Switch validator sandbox to Modal.com. Switch graph to Neo4j AuraDB Professional. Switch observability to Grafana Cloud OTLP endpoint (commit dashboards under infra/grafana/). GitHub Actions deploys: build images → push to ECR → ArgoCD app-of-apps syncs (auto staging, manual prod). THEN Stripe metered billing (per incident + per token), webhook→entitlements, plan switcher in Settings. SOC2-lite: append-only audit via Postgres trigger, nightly hash anchored to S3 Object Lock, backup/restore drill documented. Per-tenant Redis token-bucket rate limits. k6 load test: 100 concurrent workflows on staging, fix bottlenecks. Run trivy + manual security checklist on sandbox + auth. Use code-reviewer multi-agent for security pass. Acceptance: all Phase 1–6 acceptance tests still pass against cloud env, k6 p95<8min with 0 cross-tenant leaks, Stripe test charge updates entitlement <30s.`

**Phase 8** — `Polish onboarding: sample repo wizard, in-app tour (Shepherd.js), empty-state CTAs per §4.3. Build docs site at docs.nexis.dev (Nextra) covering install, integrations, agent reference, runbook. Wire BetterStack status page + uptime checks. Open public signup behind invite code. Freeze evaluation benchmark numbers (NEXIS vs 3 baselines × 30 faults × 5 seeds) and NASA-TLX results. Acceptance: 10 active tenants, ≥3 successful auto-recoveries in production, thesis chap 1–4 sent to supervisor.`

**Phase 9** — `Bug burn-down from beta feedback (triage in GitHub Projects, P0/P1 only). Performance pass: prompt compaction, cache warming, p95 latency targets. Thesis revisions per supervisor feedback, defense slide deck, live demo rehearsal. Marketing: 2 case studies, Show HN post draft, Product Hunt assets. Tag v1.0, publish launch announcement. Acceptance: defense passed, launch day 0 P0 incidents in first 7 days.`

---

## How to use this document

1. **`/clear`** your Claude Code session.
2. In the new session, attach this file: `@docs/PROJECT_PLAN.md`.
3. Start with the Phase 1 prompt above (or "let's begin Phase 1").
4. Each phase's prompt is self-contained — an agent with no prior context can execute it.
5. After each phase ends green, append a `Completed YYYY-MM-DD` line under that phase's heading. This file is the project memory.
