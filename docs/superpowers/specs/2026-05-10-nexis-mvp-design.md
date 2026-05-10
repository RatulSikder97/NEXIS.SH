# NEXIS — Production Design Spec (Full Architecture, Domain-1 Ship)

**Date:** 2026-05-10
**Author:** Ratul Sikder (with Claude)
**Status:** Draft for review
**Scope:** Real-customer launch with **full production tech stack wired** (Kafka/Flink/Spark/Neo4j/vector DB/Temporal/EKS). Domain 1 (DevOps auto-recovery) ships fully implemented in v1. Domains 2–6 ship as platform-ready scaffolds with one minimal connector each, lit up incrementally post-v1.

---

## 0. Summary

NEXIS v1 ships as a **multi-tenant SaaS** that runs the **full closed-loop DevOps recovery pipeline** for engineering teams: detect deployment / CI / runtime failures, diagnose the root cause, synthesise a contract-aware patch, validate it in a real isolated sandbox, route it through a policy-driven approval gate, open a real GitHub PR, watch the rollout, and auto-rollback on SLO breach.

A second, equally first-class surface — the **Live Pipeline Demo** — runs the same pipeline against NEXIS-owned demo fixtures so the product can be shown to professors, examiners, and prospective customers without onboarding their data.

The dashboard is rebuilt on **shadcn/ui** with seven surfaces: Home, Incidents, Approval Queue, Audit Log, Integrations, Settings, Live Pipeline Demo, plus an **Agents fleet panel** in Admin.

### 0.1 Dual-Layer Multi-Agent Architecture

NEXIS is a **dual-layer multi-agent system** (per Masters Research Proposal §5):

- **Layer 1 — Execution Team (5 agents)**: Architect, Backend, QA, DevOps, Data Engineer.
  Code-writing / system-building agents that mirror real engineering roles. They are the targets of repair execution from Layer 2.
- **Layer 2 — Self-Healing Loop (4 agents)**: Sentinel, Pathfinder, Synthesiser, Validator.
  Detect / diagnose / synthesise repair-plan / validate.
- **Platform services (3)**: api-gateway, auth, integrations.
  Edge, identity, connector ingestion.

The spec maps each agent to a service or activity (§2.1). Agents are first-class entities — every workflow event records an `agent` field — and the dashboard's Agents panel (§9.3.7) shows fleet status, task volume, success rate, delegation graph, and reasoning evidence per agent.

### 0.2 Document scope

This spec is the source of truth for both:
- **Product implementation** (production-grade SaaS, what Cursor builds)
- **Masters research thesis** (Layer-1/Layer-2 framing, RLHF loop, causal inference, shadow-execution correctness primitive, evaluation track in Appendix A)

Single document, no fork. After review, the writing-plans skill turns it into a phased build plan.

---

## 1. Locked Decisions (Recap)

| Area | Decision |
|---|---|
| Goal | Real-customer launch, design-partner-ready, full production tech stack wired |
| Hosting | Multi-tenant SaaS, AWS, single region (us-east-1) at launch, multi-region in roadmap |
| Domains | All 6 architected & scaffolded. **Domain 1 (DevOps auto-recovery) fully implemented in v1.** Domains 2–6 (Data, Backend/API, QA, Architecture, Admin Reports) ship as platform-ready services with one minimal connector each; full coverage rolls in incrementally. |
| Pipeline depth | Full closed loop: detect → diagnose → synthesise → validate → approve → PR → deploy → rollback |
| Auto-merge (D) | Off by default, policy-flagged opt-in, **never** to production in v1 |
| Auth | GitHub OAuth + email/password, JWT in HttpOnly cookie. Google v1.5, SAML v2. |
| Multi-tenancy | `org_id` on every row + Postgres RLS as defense-in-depth |
| LLM | Claude Sonnet 4.6 (synthesis), Haiku 4.5 (cheap classification). Provider abstraction. Prompt caching on. |
| **Event bus** | **Kafka via AWS MSK Serverless** (or Redpanda Cloud as fallback). Source of truth for all platform events: telemetry ingest, fault reports, RCA, patches, validation runs, approvals, deployments, audit. Temporal consumes & emits via Kafka adapters. |
| **Stream processing** | **Apache Flink on Kubernetes (EKS)** for Sentinel anomaly windows + DataOps lineage joins + Backend/API contract diffing. SQL-first jobs where possible. |
| **Batch / replay** | **Apache Spark on EMR Serverless** for Validator distributed shadow replay (large-volume historical data) + nightly memory-document re-embedding + ML training jobs (failure classifier). |
| **Graph** | **Neo4j AuraDB** (managed) for Pathfinder dependency / lineage / blast-radius traversal. OpenLineage events feed the graph via Flink job. |
| **Vector store** | **Pinecone** (serverless, multi-tenant) for Synthesiser semantic memory. **Postgres + pgvector** kept as a fallback / cheap-tier and for tests. |
| Workflow engine | Temporal (self-hosted on EKS) for the orchestration of long-running workflows + human-in-the-loop. |
| Cache / rate limit | Redis (ElastiCache) |
| Artifact storage | S3 (versioned, SSE-KMS, lifecycle to Glacier after 90 days) |
| Secrets | Secrets Manager for app secrets. Customer-supplied secrets KMS-envelope-encrypted at row level, key per org. |
| Compute | EKS, namespace per env, Fargate-backed validator pods, EMR Serverless for Spark, MSK Serverless for Kafka, Flink on EKS via FlinkKubernetesOperator. |
| **Service count** | **9 services**: api-gateway, auth, recovery-engine (Temporal worker), gitops, integrations, dataops-integration, backend-api-integration, qa-automation, arch-governance. |
| Backend lang | Go + Chi (services), Java for Flink jobs, Python (PySpark) for Spark jobs. |
| Observability | OpenTelemetry → Grafana Cloud (logs, traces, metrics). App errors → Sentry (NEXIS's own). Kafka lag + Flink checkpoint lag + Spark job state on Grafana. |
| CI/CD | GitHub Actions → ECR → ArgoCD. Trivy + gosec + golangci-lint + npm audit + spotbugs (Java) + bandit (Python). |
| IaC | Terraform (modules per dependency: VPC, EKS, RDS, MSK, EMR, ElastiCache, Neo4j-Aura-link, Pinecone-link, S3, KMS). |
| Frontend | Next.js 16.2.2 + Tailwind v4 + shadcn/ui + next-themes + TanStack Query + Zustand + Recharts + react-hook-form + zod + **Clerk** (`@clerk/nextjs`) |
| API style | OpenAPI 3.1 spec → generated TS client + generated Go server stubs (oapi-codegen) |
| Testing | Go: `testing` + `testcontainers-go` (Postgres, Kafka, Temporal). Java/Flink: `MiniCluster` tests. PySpark: `pyspark.testing`. Frontend: Vitest + Playwright. |
| Billing | None in MVP. Manual invoicing. Stripe v1.5. |
| Email | Resend |

> **Next.js 16.2.2 caveat:** This version has breaking changes from common training data. Implementation must read `node_modules/next/dist/docs/` before writing route, layout, server-action, or data-fetching code. Do not assume App Router conventions still match Next.js 14/15.

---

## 2. High-Level Architecture

```
                                ┌──────────────────────────────────────┐
                                │   Browser — Next.js 16 + shadcn/ui   │
                                │   (Console, 7 surfaces + Admin)      │
                                └──────────────┬───────────────────────┘
                                               │ HTTPS, Clerk session JWT
                                               ▼
                                ┌──────────────────────────────────────┐
                                │   AWS ALB → API Gateway / BFF (Go)   │
                                │   - Clerk JWT verify + RBAC + ABAC   │
                                │   - Per-org rate limit (Redis)       │
                                │   - OpenAPI 3.1 contract             │
                                │   - mTLS to internal services        │
                                └──┬─────────────────────────────────┬─┘
                                   │                                 │
        ┌──────────────────────────┼────────────────┬────────────────┤
        │                          │                │                │
   ┌────▼────────┐         ┌───────▼──────┐  ┌──────▼─────────┐ ┌────▼─────────────┐
   │  Auth Svc   │         │ Recovery     │  │ GitOps Svc     │ │ Integrations Svc │
   │  - Clerk    │         │ Engine       │  │ - GitHub App   │ │ - Webhooks       │
   │    sync     │         │ (Temporal    │  │ - ArgoCD watch │ │ - Connector reg  │
   │  - RBAC     │         │  worker)     │  │ - Health gate  │ │ - KMS secrets    │
   │  - API keys │         │ - Sentinel   │  │ - Auto-rollback│ │ - Demo router    │
   │  - Sessions │         │ - Pathfinder │  └──────┬─────────┘ └────────┬─────────┘
   │  - Audit    │         │ - Synthesiser│         │                    │
   └─────────────┘         │ - Validator  │         │                    │
                           │ - ApprovalGt │         │                    │
                           └──┬───────────┘         │                    │
                              │                     │                    │
        ┌─────────────────────┼─────────────────────┼────────────────────┼─────────┐
        │   Domain Integration Services (event producers + connectors)             │
        │   ┌────────────────┐  ┌────────────────┐  ┌────────────────┐  ┌──────┐  │
        │   │ DataOps        │  │ Backend/API    │  │ QA Automation  │  │Arch- │  │
        │   │ Integration    │  │ Integration    │  │ Service        │  │Gov   │  │
        │   │ Airflow,dbt,   │  │ OpenAPI,traces │  │ test plans,    │  │ADR,  │  │
        │   │ Spark,Flink,   │  │ logs,metrics,  │  │ regression gen │  │owner-│  │
        │   │ OpenLineage    │  │ health endp.   │  │ replay fixtures│  │ship, │  │
        │   └────────┬───────┘  └────────┬───────┘  └────────┬───────┘  │graph │  │
        │            │                   │                   │          └──┬───┘  │
        └────────────┼───────────────────┼───────────────────┼─────────────┼──────┘
                     │                   │                   │             │
                     └─────────┬─────────┴─────────┬─────────┴─────────────┘
                               │                   │
                     ┌─────────▼─────────┐ ┌───────▼────────────────────────────┐
                     │  Kafka (MSK Srvls)│ │  Stream/Batch Processing            │
                     │  Topics:          │ │  ┌───────────────┐  ┌────────────┐  │
                     │  - telemetry      │ │  │ Flink on EKS  │  │ Spark on   │  │
                     │  - lineage        │ │  │ (anomaly,     │  │ EMR Srvls  │  │
                     │  - faults         │ │  │  contract,    │  │ (replay,   │  │
                     │  - rca            │ │  │  lineage)     │  │  embed,    │  │
                     │  - patches        │ │  └───────────────┘  │  ML jobs)  │  │
                     │  - validations    │ │                     └────────────┘  │
                     │  - approvals      │ └─────────────────────────────────────┘
                     │  - deployments    │
                     │  - audit          │
                     └─────────┬─────────┘
                               │
        ┌──────────────────────┴──────────────────────────────────────────────┐
        │                  Persistence & Memory Layer                          │
        │                                                                       │
   ┌────▼─────────┐  ┌──────────────┐  ┌──────────┐  ┌──────────┐ ┌────────────▼─┐
   │ RDS Postgres │  │ ElastiCache  │  │ S3       │  │ Neo4j    │ │ Pinecone     │
   │ 16           │  │ Redis 7      │  │ Artifacts│  │ AuraDB   │ │ Serverless   │
   │ - tenants    │  │ - rate limit │  │ - patches│  │ - service│ │ - per-org    │
   │ - incidents  │  │ - job locks  │  │ - logs   │  │   graph  │ │   namespaces │
   │ - audit      │  │ - GH tokens  │  │ - evid.  │  │ - lineage│ │ - 1536-d     │
   │ - RLS        │  │ - SSE bus    │  │ - replay │  │ - blast  │ │   embeddings │
   │ - pgvector   │  └──────────────┘  └──────────┘  │   radius │ └──────────────┘
   │   (fallback) │                                  └──────────┘
   └──────────────┘
                                ┌──────────────────────────────────────┐
                                │  Temporal Cluster (EKS, self-hosted) │
                                │  - workflow state                    │
                                │  - activity exec                     │
                                │  - human-in-the-loop signals         │
                                └──────────────────────────────────────┘

External (per-customer):                     NEXIS-owned (Live Pipeline Demo):
- GitHub repo + Actions  ◄─ GitHub App      - Gitea (in-cluster)
- Sentry / Datadog       ◄─ webhook         - synthetic Sentry/ArgoCD feeders
- ArgoCD instance        ◄─ webhook         - sandboxed K8s namespace
- Customer K8s/Cloud     ◄─ PR + ArgoCD     - Routes via demo connectors → is_demo=true rows

Identity & Auth:
- Clerk (hosted) — passkeys, MFA, OAuth, SAML, organizations, invites, sessions
- Auth Svc syncs Clerk webhooks → users/orgs/org_members tables, owns RBAC + API keys + audit

Observability:
- All services → OTel SDK → Grafana Cloud (logs, traces, metrics, dashboards)
- App errors → Sentry (NEXIS's own)
- Kafka lag, Flink checkpoints, Spark job state → Grafana
- Audit events → Postgres → S3 nightly Parquet export (7yr retention)
```

### 2.1 Service ↔ Agent mapping

Every service hosts one or more **named agents**. Agents are the unit of identity in events, audit, dashboard, and metrics. Agents in the Layer-2 self-healing loop run as Temporal activities inside `recovery-engine` (workflow steps, not separate HTTP services). Layer-1 execution agents run as their own services so they can independently take delegated tasks from Synthesiser, expose code-writing capabilities, and grow domain coverage.

| Service | Layer | Agent(s) hosted | Owns | Does NOT own |
|---|---|---|---|---|
| `api-gateway` | Platform | — | HTTP/JSON edge, OpenAPI contract, Clerk JWT verification, RBAC + ABAC enforcement, rate limit, request fan-out, mTLS to internal services. The only internet-exposed service. | Domain logic. |
| `auth` | Platform | — | Clerk webhook sync (users/orgs/members), local RBAC + ABAC tables, API key issuance + verification, session activity log, auth audit, brute-force / lockout policy enforcement. | Identity flow itself (Clerk owns it). |
| `integrations` | Platform | — | Connector registry, customer credential storage (KMS-encrypted), inbound webhook receivers (Sentry, ArgoCD, GitHub Apps, OpenLineage), connector health checks, **demo connector routing**. Publishes raw events to Kafka. | The recovery loop itself. |
| `recovery-engine` | L2 | **Sentinel**, **Pathfinder**, **Synthesiser**, **Validator**, ApprovalGate | Temporal workflow definitions; activities for each L2 agent. Publishes per-agent step events to Kafka. | Customer credentials, GitHub I/O, K8s I/O. |
| `gitops` | L1 | **DevOps Agent** | All GitHub App I/O (PR open/comment/merge), ArgoCD event ingestion, deployment health watch, auto-rollback. Owns the GitHub App private key. Receives delegated DevOps repair tasks (manifest patches, IaC fixes) from Synthesiser. | Workflow state. |
| `dataops-integration` | L1 | **Data Engineer Agent** | Connectors for Airflow, dbt, Spark, Flink, OpenLineage. Normalises DAG/job/run/test failures into platform fault events. Receives delegated data repair tasks (schema migrations, dbt model patches) from Synthesiser. | Generic recovery loop logic. |
| `backend-api-integration` | L1 | **Backend Agent** | Connectors for OpenAPI specs, service traces (Tempo / Jaeger compat), logs (Loki), metrics, health endpoints. Detects API contract drift via Flink. **Code-writing capability** — receives delegated backend code repair tasks from Synthesiser, returns concrete `PatchCandidate`. | Generic recovery loop logic. |
| `qa-automation` | L1 | **QA Agent** | Test plan generation, regression test synthesis, replay-fixture selection from S3, owned test artifact registry. **Property-based test generation** (Hypothesis / fast-check) consumed by Validator activity. Returns structured defect reports to Backend Agent. | Validator pod execution. |
| `arch-governance` | L1 | **Architect Agent** | Service ownership registry, ADR ingestion, dependency-graph updates pushed to Neo4j, **contract registry** (API/data contracts). **Hard-blocks contract violations** in any patch before Validator runs (synchronous gate via Synthesiser → Architect call). Policy checks. Subscribes to lineage Kafka topic. | Direct dependency-graph queries (Pathfinder owns those). |

### 2.2 Service-count rationale

9 services match the architectural concerns:
- The 5 platform services (api-gateway, auth, recovery-engine, gitops, integrations) own the spine.
- The 4 domain integration services own external-system surface area per domain. Each is small in v1 (often one connector + one event producer + one Kafka consumer) but exists so that adding new connectors / domains does not bloat shared services. Domains 2–6 light up by adding code inside these 4 services, not by spinning up more.
- Sentinel / Pathfinder / Synthesiser / Validator / ApprovalGate stay collapsed inside `recovery-engine` as **Temporal activities** (not separate HTTP services) because they are workflow steps, not independent request-response endpoints.

### 2.3 Network isolation

- ALB → only `api-gateway` from the public internet. All other services are in private subnets with security groups allowing only intra-VPC traffic.
- Service-to-service calls use **mTLS** (cert-manager + Linkerd / Istio service mesh — Linkerd default for lower complexity).
- Validator pods run in a **dedicated EKS namespace `nexis-validator`** with a NetworkPolicy: no egress except to ECR mirror, internal S3 endpoint, Temporal cluster, Kafka brokers. No customer-network egress.
- GitHub App / Sentry / ArgoCD webhooks hit `api-gateway` over HTTPS with payload signature verification before forwarding to `integrations`.
- Kafka brokers (MSK Serverless) accessible only from VPC security groups owned by NEXIS services.
- Neo4j AuraDB accessed via VPC-Peered private endpoint.
- Pinecone accessed via PrivateLink.

### 2.4 Event topology (Kafka)

Topics (all multi-tenant, partition key = `org_id`):

| Topic | Producer | Consumer(s) | Schema |
|---|---|---|---|
| `telemetry.raw` | integrations + domain-integration services | Flink (Sentinel anomaly job) | Raw provider event envelope |
| `lineage.events` | dataops-integration (OpenLineage), arch-governance | Flink (graph-update job) → Neo4j | OpenLineage spec |
| `faults.normalized` | Flink (Sentinel job) | recovery-engine (workflow trigger) | FaultReport v1 |
| `workflow.steps` | recovery-engine activities | api-gateway SSE pump, audit logger, Agents-panel aggregator | Workflow step event |
| `rca.reports` | recovery-engine (Pathfinder activity) | qa-automation, audit logger | RootCauseReport v1 |
| `patches.candidates` | recovery-engine (Synthesiser activity) | gitops, audit logger | PatchCandidate v1 |
| `validations.results` | recovery-engine (Validator activity) | gitops, audit logger | ValidationReport v1 |
| `approvals.decided` | recovery-engine (ApprovalGate activity) + api-gateway (human approve) | gitops, audit logger | ApprovalDecision v1 |
| `deployments.events` | gitops | recovery-engine (rollback workflow), audit logger | DeploymentEvent v1 |
| `audit.events` | every mutating service | audit ingester (writes to Postgres + S3) | AuditEvent v1 |

All schemas registered in **Confluent Schema Registry / AWS Glue Schema Registry** (Avro). Producers and consumers cross-validated in CI.

### 2.5 Stream processing (Flink)

Flink jobs (Java/SQL, deployed via FlinkKubernetesOperator on EKS):

| Job | Purpose | In | Out |
|---|---|---|---|
| `sentinel-anomaly` | Sliding-window anomaly detection on raw telemetry. Uses statistical baselines + a small classifier. | `telemetry.raw` | `faults.normalized` |
| `lineage-graph-loader` | Joins OpenLineage events into batched graph upserts to Neo4j. | `lineage.events` | Neo4j Cypher commands |
| `contract-diff` (Backend/API domain) | Compares OpenAPI snapshots, emits contract-violation faults. | `telemetry.raw` (CI snapshots) | `faults.normalized` |

### 2.6 Batch processing (Spark on EMR Serverless)

Spark jobs (PySpark, scheduled via Airflow OR Temporal cron workflows):

| Job | Purpose |
|---|---|
| `validator-replay` | Distributed replay of historical traffic against a candidate patch (called as Validator activity for high-volume incidents). |
| `memory-reembed` | Nightly re-embedding of memory_documents when models change. |
| `failure-classifier-train` | Weekly retrain of the lightweight failure classifier consumed by `sentinel-anomaly`. Output: model artifact in S3. |
| `audit-export` | Nightly export of audit events to S3 Parquet. |

### 2.7 Backend Microservice Folder Standard (binding rule)

**Every Go microservice under `backend/cmd/<svc>` MUST follow this exact structure.** Cursor refactors each existing service to match this layout in Phase 0. No exceptions: if a folder isn't needed for a given service, omit it; do not invent alternatives.

```
backend/
  api/                            # OpenAPI 3.1 spec — source of truth for HTTP contract
    openapi.yaml                  # one root spec, $ref-split per service
    components/                   # shared schemas, error envelopes
    services/<svc>.yaml           # per-service paths

  cmd/
    <svc>/
      main.go                     # thin entrypoint, ≤80 lines, NO business logic
                                  # Order: load config → init platform deps (otel, pg,
                                  # redis, kafka, secrets, auth) → init repo →
                                  # init adapters → init service → init handler →
                                  # run server with graceful shutdown.

  internal/
    services/
      <svc>/
        config/
          config.go               # one struct + Load(); ALL env reads happen here
          config_test.go
        handler/
          routes.go               # single Routes(deps) chi.Router; mounts middleware
                                  # + sub-routers; calls platform/httpserver chain
          health.go               # /healthz, /readyz wired to platform/probes
          <resource>.go           # one file per top-level resource (e.g. incidents.go)
          <resource>_test.go      # handler-level tests
        service/
          service.go              # main Service struct, ctor, dependency interfaces
          <feature>.go            # one file per feature/use-case
          <feature>_test.go       # service-level tests (mock adapters)
          interfaces.go           # consumer-owned interfaces (Repository, LLMClient, ...)
        model/
          <type>.go               # domain types & DTOs (NO db/json tags — that's serializer's job)
        store/
          postgres/
            <repo>_repo.go        # implements service.Repository
            <repo>_repo_test.go   # testcontainers-go integration tests
            queries.sql           # SQL kept literal where useful
          kafka/
            producer.go           # implements service.EventPublisher
            consumer.go           # implements service.EventConsumer (when needed)
          s3/
            artifact_store.go     # implements service.ArtifactStore
        events/
          schema/                 # mirror of Schema-Registry Avro types as Go structs
          <topic>.go              # producer/consumer wrappers per topic
          <topic>_test.go
        worker/                   # ONLY in services running Temporal workers
          workflows.go
          activities.go
          worker.go               # registers workflows + activities
        adapter/
          llm/                    # external system adapters; one folder per provider
          github/
          neo4j/
          pinecone/
        testdata/
          *.json                  # golden fixtures
        README.md                 # one-page service overview: purpose, deps, runbook link

    platform/                     # shared cross-service code — depended on by ALL services
      env/                        # env loading + validation (envconfig)
      otel/                       # tracer + meter + logger init, propagation, exporter
      httpserver/                 # chi middleware chain: requestid, logger, recoverer,
                                  # tracing, auth, rbac, ratelimit, audit, panic→5xx
      httpclient/                 # otel-instrumented http.Client + retry/circuit-break
      pg/                         # pgx pool + tx helpers + SET LOCAL app.current_org_id
      redis/                      # go-redis client
      kafka/                      # franz-go / segmentio kafka client + schema registry
      authz/                      # RBAC + ABAC: Allow(ctx, action, resource) helpers
      authn/                      # Clerk JWT verifier + API key verifier
      secrets/                    # KMS envelope encrypt/decrypt helpers
      audit/                      # AuditEvent struct + WriteAudit(ctx, evt) helper
      probes/                     # standard /healthz + /readyz building blocks
      contracts/                  # oapi-codegen output (server stubs + types)
      errors/                     # ErrorEnvelope + ProblemDetails RFC9457
      validate/                   # request validation helpers (go-playground/validator)
      testutil/                   # testcontainers helpers (Postgres, Redis, Kafka, Temporal)

  migrations/                     # one tree of SQL migrations, golang-migrate format
    20260510120000_initial.up.sql
    20260510120000_initial.down.sql
    ...

  Dockerfile                      # one Dockerfile, ARG SERVICE — already in repo, keep
  Makefile                        # targets: build, test, lint, gen, run-<svc>, e2e
  go.mod
  go.sum
```

#### 2.7.1 Layering rules (enforced by go-arch-lint)

```
cmd/<svc>            → may import: internal/services/<svc>/*, internal/platform/*
internal/services/<svc>/handler   → may import: service, model, platform/{contracts,httpserver,errors,validate,authz,authn}
internal/services/<svc>/service   → may import: model, events, adapter (interfaces only via service.interfaces.go)
internal/services/<svc>/store/*   → may import: model, platform/{pg,redis,s3,kafka,otel}
internal/services/<svc>/adapter/* → may import: model, service.interfaces.go, platform/{otel,httpclient,secrets}
internal/services/<svc>/events    → may import: model, platform/{kafka,otel}
internal/services/<svc>/worker    → may import: service, model, events, platform/{otel,kafka}
internal/platform/*               → MAY NOT import any internal/services/*
```

#### 2.7.2 Conventions

- **Chi** for routing in every service. Middleware composed via `platform/httpserver.Chain(...)`.
- **Errors**: services return wrapped `error` with sentinel kinds; handlers translate to `ProblemDetails` (RFC 9457) JSON via `platform/errors`.
- **Logging**: `slog` only, JSON handler, trace-correlated via `platform/otel`. NO `log.Printf`.
- **Context**: every public method takes `ctx context.Context` first. Use `ctx` for cancellation, deadlines, request id, org id, trace id.
- **Org isolation**: every service-layer call extracts `org_id` from `ctx` (set by `platform/authn` middleware) and passes it explicitly into the repo. Repo wraps every Postgres tx with `SET LOCAL app.current_org_id = $1`.
- **Configuration**: ONE `config/config.go` per service. NO direct `os.Getenv` outside it. Validated on `Load()` with `validate` tags.
- **Tests**: handler tests with `httptest`. Service tests with mock adapters (interfaces in `service/interfaces.go` make this easy). Store tests with `testcontainers-go` against real Postgres/Kafka/Redis.
- **Codegen**: handlers implement `oapi-codegen`-generated `ServerInterface`. Run `make gen` to regenerate after editing `api/openapi.yaml`. CI fails if generated code is out of date.
- **No global state**: dependencies wired in `main.go`, passed down. NO package-level vars except true constants.
- **One file per feature**: a 300-line `service.go` is a refactor signal. Split per use case.
- **Migrations**: every schema change is a migration file under `migrations/`. NO ad-hoc DDL.

#### 2.7.3 Refactor plan for current code

Phase 0 turns the current `internal/services/<svc>/{handler,service}.go` files into the structure above. Each service in scope:

| Current | New |
|---|---|
| `cmd/orchestrator` (218 LoC) | merge into `cmd/recovery-engine`; logic becomes Temporal workflow + activities |
| `cmd/sentinel`, `cmd/pathfinder`, `cmd/synthesiser`, `cmd/validator`, `cmd/approval-gate` | each becomes a Temporal **activity** in `internal/services/recovery-engine/worker/activities.go`; their HTTP servers are deleted |
| `cmd/auth` (330 LoC) | refactored in place to new layout; gains Clerk webhook handler + API key issuance |
| `cmd/devops-gitops` | renamed to `cmd/gitops`, refactored in place |
| `cmd/integration-registry` | renamed to `cmd/integrations`, refactored in place |
| `cmd/repair`, `cmd/validator-deploy` | deleted (logic absorbed into recovery-engine activities) |
| **NEW** `cmd/dataops-integration` | scaffolded |
| **NEW** `cmd/backend-api-integration` | scaffolded |
| **NEW** `cmd/qa-automation` | scaffolded |
| **NEW** `cmd/arch-governance` | scaffolded |
| **NEW** `cmd/api-gateway` | scaffolded — replaces orchestrator's HTTP role |

---

## 3. Domain Model + Database Schema

Postgres 16. One database, schema `public` for tenant data, schema `nexis_demo` for demo-mode rows, schema `audit` for append-only audit events.

### 3.1 Core tables (abbreviated DDL — see migrations for full)

```sql
-- Tenancy
CREATE TABLE orgs (
  id UUID PRIMARY KEY,
  name TEXT NOT NULL,
  slug TEXT UNIQUE NOT NULL,
  plan TEXT NOT NULL DEFAULT 'design_partner',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id UUID PRIMARY KEY,
  clerk_user_id TEXT UNIQUE NOT NULL,    -- source of truth, synced from Clerk webhook
  email CITEXT UNIQUE NOT NULL,
  name TEXT,
  avatar_url TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ                 -- soft delete (audit retention)
);

-- API keys for service-account / programmatic access (see §11.5)
CREATE TABLE api_keys (
  id UUID PRIMARY KEY,
  org_id UUID NOT NULL REFERENCES orgs(id),
  created_by_user_id UUID NOT NULL REFERENCES users(id),
  name TEXT NOT NULL,
  key_hash TEXT NOT NULL,                -- bcrypt hash of full key
  key_prefix TEXT NOT NULL,              -- first 12 chars for UI ("nxs_live_xxx")
  scopes JSONB NOT NULL DEFAULT '[]',
  service_scope JSONB,                   -- optional [service_id, ...]
  ip_allowlist INET[],
  expires_at TIMESTAMPTZ NOT NULL,
  last_used_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON api_keys (org_id, revoked_at);

-- Auth session activity log (Clerk owns sessions; we log for audit)
CREATE TABLE auth_sessions (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  org_id UUID REFERENCES orgs(id),
  clerk_session_id TEXT UNIQUE NOT NULL,
  ip INET,
  user_agent TEXT,
  device_label TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at TIMESTAMPTZ
);

-- Per-agent run record (powers the Agents fleet panel §9.3.6)
CREATE TABLE agent_runs (
  id UUID PRIMARY KEY,
  org_id UUID NOT NULL REFERENCES orgs(id),
  agent TEXT NOT NULL,                   -- 'sentinel','pathfinder','synthesiser','validator',
                                          -- 'approval_gate','architect','backend','qa',
                                          -- 'devops','data_engineer'
  layer TEXT NOT NULL CHECK (layer IN ('L1','L2','platform')),
  workflow_id TEXT,
  incident_id UUID REFERENCES incidents(id),
  status TEXT NOT NULL,                  -- 'running','succeeded','failed','timeout'
  input_summary JSONB,
  output_summary JSONB,
  delegated_to TEXT,                     -- agent name if this run delegated to another
  llm_tokens_in INT,
  llm_tokens_out INT,
  llm_cache_hit BOOLEAN,
  duration_ms INT,
  started_at TIMESTAMPTZ NOT NULL,
  finished_at TIMESTAMPTZ
);
CREATE INDEX ON agent_runs (org_id, agent, started_at DESC);
CREATE INDEX ON agent_runs (incident_id);

-- RLHF feedback corpus (see §7.9)
CREATE TABLE feedback_examples (
  id UUID PRIMARY KEY,
  org_id UUID NOT NULL REFERENCES orgs(id),
  agent TEXT NOT NULL,
  input_artifact JSONB NOT NULL,         -- snapshot of RepairPlan / PatchCandidate / etc.
  output_artifact JSONB NOT NULL,
  reward NUMERIC(3,2) NOT NULL,          -- -1.0 .. +1.0
  reward_source TEXT NOT NULL,           -- 'human_approve','human_reject','deploy_healthy','rollback'
  decided_by_user_id UUID REFERENCES users(id),
  incident_id UUID REFERENCES incidents(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON feedback_examples (org_id, agent, reward DESC, created_at DESC);

-- Versioned per-agent prompt corpus
CREATE TABLE prompt_versions (
  id UUID PRIMARY KEY,
  agent TEXT NOT NULL,
  version INT NOT NULL,
  system_prompt TEXT NOT NULL,
  example_ids UUID[],                    -- references feedback_examples
  dataset_hash TEXT NOT NULL,
  status TEXT NOT NULL,                  -- 'shadow','active','retired'
  promoted_at TIMESTAMPTZ,
  promoted_by_user_id UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (agent, version)
);

CREATE TABLE org_members (
  org_id UUID NOT NULL REFERENCES orgs(id),
  user_id UUID NOT NULL REFERENCES users(id),
  role TEXT NOT NULL CHECK (role IN ('admin','sre','engineer','reviewer','viewer')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, user_id)
);

CREATE TABLE teams (
  id UUID PRIMARY KEY,
  org_id UUID NOT NULL REFERENCES orgs(id),
  name TEXT NOT NULL,
  UNIQUE (org_id, name)
);

-- Sessions
CREATE TABLE sessions (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  org_id UUID NOT NULL REFERENCES orgs(id),
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  user_agent TEXT,
  ip INET
);

-- Connectors / customer credentials
CREATE TABLE connectors (
  id UUID PRIMARY KEY,
  org_id UUID NOT NULL REFERENCES orgs(id),
  kind TEXT NOT NULL,           -- 'github','sentry','argocd','demo-github','demo-sentry'
  name TEXT NOT NULL,
  config JSONB NOT NULL,        -- non-secret config (repo names, project ids)
  encrypted_secret BYTEA,       -- KMS-envelope-encrypted secret blob
  encryption_key_id TEXT,       -- KMS CMK alias used
  is_demo BOOLEAN NOT NULL DEFAULT false,
  health TEXT NOT NULL DEFAULT 'unknown',
  last_checked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Services (the customer's services that NEXIS knows about)
CREATE TABLE services (
  id UUID PRIMARY KEY,
  org_id UUID NOT NULL REFERENCES orgs(id),
  name TEXT NOT NULL,
  repo_url TEXT,
  owner_team_id UUID REFERENCES teams(id),
  metadata JSONB,
  UNIQUE (org_id, name)
);

-- Incidents (the central object)
CREATE TABLE incidents (
  id UUID PRIMARY KEY,
  org_id UUID NOT NULL REFERENCES orgs(id),
  service_id UUID REFERENCES services(id),
  workflow_id TEXT NOT NULL,    -- Temporal workflow id
  run_id TEXT NOT NULL,         -- Temporal run id
  status TEXT NOT NULL,         -- 'detected','diagnosing','synthesising','validating','awaiting_approval','approved','deploying','deployed','rolled_back','rejected','failed'
  severity TEXT NOT NULL,       -- 'low','medium','high','critical'
  source TEXT NOT NULL,         -- 'sentry','github_actions','argocd','demo'
  source_event_id TEXT,
  title TEXT NOT NULL,
  summary TEXT,
  is_demo BOOLEAN NOT NULL DEFAULT false,
  scenario_id TEXT,             -- non-null for demo runs
  detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ
);
CREATE INDEX ON incidents (org_id, status, detected_at DESC);
CREATE INDEX ON incidents (org_id, is_demo, detected_at DESC);

-- Pipeline artifacts
CREATE TABLE root_cause_reports (
  id UUID PRIMARY KEY,
  incident_id UUID NOT NULL REFERENCES incidents(id),
  hypotheses JSONB NOT NULL,    -- ranked hypotheses with evidence
  blast_radius JSONB,
  confidence NUMERIC(3,2),
  produced_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE patch_candidates (
  id UUID PRIMARY KEY,
  incident_id UUID NOT NULL REFERENCES incidents(id),
  ordinal INT NOT NULL,
  diff TEXT NOT NULL,
  explanation TEXT NOT NULL,
  contracts_touched JSONB,
  llm_model TEXT NOT NULL,
  llm_tokens INT,
  llm_cache_hit BOOLEAN,
  produced_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE validation_runs (
  id UUID PRIMARY KEY,
  patch_candidate_id UUID NOT NULL REFERENCES patch_candidates(id),
  status TEXT NOT NULL,         -- 'queued','running','passed','failed','timeout'
  test_summary JSONB,           -- counts of tests passed/failed
  evidence_s3_key TEXT,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ
);

CREATE TABLE approval_decisions (
  id UUID PRIMARY KEY,
  incident_id UUID NOT NULL REFERENCES incidents(id),
  patch_candidate_id UUID REFERENCES patch_candidates(id),
  decided_by_user_id UUID REFERENCES users(id),  -- null when auto
  decision TEXT NOT NULL,       -- 'approve','reject','auto_approve','escalate'
  policy_id UUID,               -- the matched policy if auto
  rationale TEXT,
  decided_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE deployments (
  id UUID PRIMARY KEY,
  incident_id UUID NOT NULL REFERENCES incidents(id),
  patch_candidate_id UUID NOT NULL REFERENCES patch_candidates(id),
  pr_url TEXT NOT NULL,
  pr_number INT NOT NULL,
  commit_sha TEXT,
  argocd_app TEXT,
  health TEXT NOT NULL DEFAULT 'pending', -- 'pending','healthy','degraded','rolled_back'
  rollback_reason TEXT,
  opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  merged_at TIMESTAMPTZ,
  closed_at TIMESTAMPTZ
);

-- Approval policies (per org)
CREATE TABLE approval_policies (
  id UUID PRIMARY KEY,
  org_id UUID NOT NULL REFERENCES orgs(id),
  name TEXT NOT NULL,
  matcher JSONB NOT NULL,       -- pattern: severity, source, kind, env, etc.
  action TEXT NOT NULL,         -- 'auto_approve','auto_merge','require_human','escalate'
  required_role TEXT,
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Vector memory for synthesiser
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE memory_documents (
  id UUID PRIMARY KEY,
  org_id UUID NOT NULL REFERENCES orgs(id),
  kind TEXT NOT NULL,           -- 'incident','adr','runbook','contract','schema'
  ref_id TEXT,                  -- pointer back to source row
  content TEXT NOT NULL,
  embedding vector(1536),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON memory_documents USING ivfflat (embedding vector_cosine_ops);

-- Audit (append-only)
CREATE TABLE audit.audit_events (
  id BIGSERIAL PRIMARY KEY,
  org_id UUID NOT NULL,
  actor_user_id UUID,
  actor_kind TEXT NOT NULL,     -- 'user','system','workflow'
  action TEXT NOT NULL,
  resource_kind TEXT NOT NULL,
  resource_id TEXT,
  payload JSONB,
  ip INET,
  user_agent TEXT,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON audit.audit_events (org_id, occurred_at DESC);
```

### 3.2 Row-level security

Every tenant table has:

```sql
ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
CREATE POLICY incidents_tenant_isolation ON incidents
  USING (org_id = current_setting('app.current_org_id')::uuid);
```

Each request handler in `api-gateway` opens a transaction with:

```go
tx.Exec("SET LOCAL app.current_org_id = $1", orgID)
```

This is **defense-in-depth on top of WHERE clauses**, not a replacement for them.

### 3.3 Demo data isolation

- Demo incidents have `is_demo=true` AND live in the same tables — same code paths, no fork.
- Lists/dashboards default to `is_demo=false`. Live Pipeline Demo surface filters `is_demo=true`.
- `services`, `connectors` for demo are seeded under a NEXIS-internal demo `org_id` per scenario; demo users are members of that org with role `viewer`.

---

## 4. Recovery Loop — Temporal Workflow

The pipeline is **one Temporal workflow per incident**. Activities are pure functions that call out to side effects (DB, LLM, GitHub, K8s).

### 4.1 Workflow signature

```go
func RecoveryWorkflow(ctx workflow.Context, input RecoveryInput) (RecoveryResult, error)

type RecoveryInput struct {
  OrgID       uuid.UUID
  IncidentID  uuid.UUID
  Source      string         // 'sentry','github_actions','argocd','demo'
  RawEvent    json.RawMessage
  IsDemo      bool
  ScenarioID  string         // non-empty for demo
}
```

### 4.2 Steps

1. **NormalizeFault** — turn raw inbound event into a `FaultReport` row + write `incidents.status = 'detected'`.
2. **Diagnose** — call Pathfinder activity:
   - Pull related signals (recent deploys, error spike windows, dependency graph from Neo4j).
   - **Counterfactual causal inference (DoWhy)** — for each candidate cause node in the dependency graph, compute the counterfactual probability that removing that node's recent change would have prevented the observed fault. This score is one input to hypothesis ranking alongside graph-distance and recency.
   - Build LLM input bundle (capped to 32k tokens, trimmed by relevance) including counterfactual evidence.
   - Call Claude Haiku 4.5 with prompt-cached system prompt → return ranked hypotheses + blast radius.
   - Persist `root_cause_reports` (with counterfactual evidence vector). Status → `'diagnosing' → 'synthesising'`.
3. **Synthesise** — Synthesiser activity:
   - Retrieve top-N memory docs via pgvector (prior similar incidents, ADRs, schemas, runbooks).
   - Call Claude Sonnet 4.6 with cached system prompt + retrieved context → return 1–3 patch candidates as unified diffs + plain-English explanation.
   - Each candidate validated against contract registry (no API breaking, no schema breaking unless flagged).
   - Persist `patch_candidates`.
4. **Validate** (parallel for each candidate, fan-in for results) — Validator activity:
   - Build a per-candidate K8s Job spec: ephemeral pod, base image = customer service's CI image, mount the candidate diff applied on top of the latest commit.
   - Job runs the customer's existing test suite + a NEXIS-injected property-test harness derived from the failed signal (e.g., "the schema must accept both float and string for `total_amount`").
   - Capture stdout/stderr/junit output → S3, persist `validation_runs`.
   - Pick the best candidate: passing tests, smallest diff, highest contract-compliance score.
5. **ApprovalGate** — Approval activity (synchronous):
   - Match incident + patch against `approval_policies` for the org.
   - If policy says `auto_approve` (and not auto_merge to prod): record `approval_decisions` with `decided_by_user_id = NULL`, status → `'approved'`.
   - Else: status → `'awaiting_approval'`. Workflow waits on a Temporal **signal** named `human_decision`. Dashboard "Approve/Reject" button sends this signal via api-gateway → recovery-engine RPC → Temporal client.
   - Timeout: 7 days, after which workflow auto-rejects + closes the PR if one was opened.
6. **OpenPR** — GitOps activity:
   - Use GitHub App installation token, push the patch to a branch named `nexis/incident-<short-id>-<slug>` (see PR conventions §8.2), open a PR with the explanation + validation evidence summary + a deep link back to NEXIS.
   - Persist `deployments` row with PR URL.
7. **WatchRollout** — GitOps activity (long-running, uses heartbeats):
   - Wait for PR merge (webhook signals workflow).
   - On merge: watch ArgoCD sync status + Sentry error rate window for `health_window_minutes` (default 30, configurable per policy).
   - Compute health score: `synced AND argocd_healthy AND sentry_error_rate_delta < threshold`.
   - On healthy: deployments.health = `'healthy'`, incident status = `'deployed'`, workflow ends.
   - On breach: AutoRollback activity → revert merge commit via GitHub App, deployments.health = `'rolled_back'`, incident status = `'rolled_back'`, open a follow-up incident for the regression.

### 4.3 Why Temporal

- Human-in-the-loop is a first-class concept (signals).
- Long-running waits (PR merge, rollout health window) don't burn DB rows or cron jobs.
- Retries / backoff / heartbeats / activity timeouts are configuration, not code.
- Workflow versioning lets us evolve the pipeline without breaking in-flight incidents.
- Crash-safe by design — process restart resumes workflows.

---

## 5. Integrations

### 5.1 GitHub App (single NEXIS GitHub App, multi-installation)

Permissions requested:
- **Read & write**: contents, pull requests, workflows, statuses
- **Read**: actions, deployments, metadata
- **Webhook events**: workflow_run, deployment_status, pull_request, push, check_run

Onboarding:
1. User clicks "Connect GitHub" in dashboard.
2. Redirect to `https://github.com/apps/nexis-app/installations/new`.
3. After install, GitHub redirects to NEXIS callback with installation ID.
4. Integrations service stores installation ID + repo selection per org.
5. NEXIS verifies signature on every inbound webhook with `X-Hub-Signature-256` against the app's webhook secret.

### 5.2 Sentry

- Customer creates a Sentry **Internal Integration** with `event:read` + `project:read`.
- Pastes API token + organization slug into NEXIS connector setup.
- NEXIS subscribes to Sentry webhooks for issue alerts.
- Token KMS-envelope-encrypted on store.

### 5.3 ArgoCD

- Customer adds a webhook in ArgoCD pointing at `https://api.nexis.io/v1/webhooks/argocd/<connector-id>` with a shared secret.
- NEXIS verifies the secret on every event.
- Events of interest: `OperationFailed`, `Sync`, `Health` transitions.

### 5.4 Connector health

- Every 5 minutes, integrations service runs a connector health check (GitHub: get installation, Sentry: list projects, ArgoCD: ping with last secret). Updates `connectors.health`.
- Dashboard shows per-connector health badge.

### 5.5 Demo connectors

- `demo-github`, `demo-sentry`, `demo-argocd` — same connector interface, but the underlying client is a stub that reads from `demo-fixtures/scenarios/<scenario-id>/`.
- Demo connectors are seeded automatically into the NEXIS-internal demo org on first deploy.

---

## 6. Synthesiser — LLM Strategy

### 6.1 Provider abstraction

```go
type LLMProvider interface {
  Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
}

type GenerateRequest struct {
  Model        string
  SystemPrompt string         // CACHED
  Messages     []Message
  Tools        []Tool
  MaxTokens    int
  Temperature  float64
}
```

Concrete: `AnthropicProvider`, with prompt caching via `cache_control: {"type": "ephemeral"}` on the system prompt and the retrieved-context block.

### 6.2 Prompts

- **Pathfinder system prompt** (cached): role, output schema (JSON), examples, constraint: "never propose a fix, only diagnose."
- **Synthesiser system prompt** (cached): role, output schema (unified diff + explanation + risk assessment), constraints (preserve API contracts, prefer minimal diff, must pass linked test harness).
- Per-incident user message: signal payload + RCA report + retrieved memory docs.

### 6.3 Cost discipline

- All Pathfinder calls use Haiku 4.5 (cheap).
- Synthesiser uses Sonnet 4.6 by default; falls back to Haiku 4.5 on retries.
- Prompt caching: target ≥80% cache hit on system prompts.
- Per-org per-day token budget enforced in synthesiser activity; exceeding budget pauses the workflow with `incidents.status='budget_exceeded'` and notifies admins.

### 6.4 Memory retrieval

- Embedding model: `voyage-code-3` (or fallback to OpenAI `text-embedding-3-small` if Voyage unavailable in MVP).
- Top-K retrieval (K=8) per call, filtered by `org_id` and `kind`.
- Index built async on incident close (the resolved incident becomes future memory).

---

## 7. Validator — Sandbox Design

### 7.1 Sandbox topology

- Dedicated EKS namespace `nexis-validator`.
- NetworkPolicy: deny all egress except:
  - ECR (pull base images)
  - Internal S3 endpoint (test fixtures, output upload)
  - Temporal cluster (heartbeat)
- ResourceQuota: max 50 concurrent validation pods per cluster, 2 vCPU + 4 GiB per pod, 10 min timeout.
- Each pod runs as non-root, read-only root FS, no Docker socket, gVisor runtime where available.

### 7.2 Per-validation flow

1. Validator activity creates a K8s Job with:
   - Init container: pulls base image (customer service CI image, identified via integrations).
   - Init container: applies the candidate diff to a copy of the repo at `commit_sha`.
   - Main container: runs `make test` (or customer-configured test command) + injected harness.
2. Pod streams logs to a per-job S3 prefix.
3. On completion: validator activity downloads JUnit XML + summary, parses, returns to workflow.

### 7.3 Test harness injection

- Per-incident, NEXIS generates a property test from the failure signal: e.g. "any value of type X must round-trip through serialisation." The test is written into a tmp file inside the pod and added to the test command.
- For schema-drift incidents: a synthetic dataset with both old and new shapes is used.

### 7.4 Distributed shadow replay (Spark)

For high-volume incidents (≥ 10M events in baseline window) the validator activity offloads replay to **EMR Serverless Spark**:
- Reads historical events from S3 (writeahead from Kafka via Kinesis Firehose).
- Applies the patched code (mounted as a UDF jar / wheel) to each event in parallel.
- Compares output distribution against the baseline (Wasserstein distance on numeric fields, set-distance on categorical, exact-match on PKs).
- Writes per-partition diff report to S3.
- Validator activity polls the EMR job, ingests the report, returns structured ValidationReport.

### 7.5 Layer 1 Execution Agents (services)

The five Layer-1 agents are implemented as their own services (or as identified capabilities inside an existing platform service). Each L1 agent exposes a typed delegation interface that Synthesiser (L2) calls to execute repairs in the agent's domain.

| Agent | Capability | Inputs | Outputs |
|---|---|---|---|
| **Architect** (`arch-governance`) | `EnforceContracts(repairPlan) -> {accepted, violations[]}` | RepairPlan (target system + intent + diff hint) | Block reasons (API contract violation, ADR conflict, ownership mismatch) or proceed signal |
| **Backend** (`backend-api-integration`) | `WriteCode(repairPlan, contracts) -> PatchCandidate` | RepairPlan + relevant contracts + recent code context | Unified diff + explanation + risk assessment |
| **QA** (`qa-automation`) | `GenerateTests(faultReport, contracts) -> TestSuite` | FaultReport + contracts + invariants from prior incidents | Property-based test suite (Hypothesis or fast-check) + assertion list |
| **DevOps** (`gitops`) | `WriteOpsPatch(repairPlan) -> PatchCandidate`, `OpenPR(...)`, `WatchRollout(...)` | RepairPlan for K8s/Terraform/CI manifests | Patched manifests + PR lifecycle events |
| **Data Engineer** (`dataops-integration`) | `WriteDataPatch(repairPlan, schemaContracts) -> PatchCandidate` | RepairPlan + current schema + lineage | Migration script (Alembic / Liquibase) + dbt model diff + downstream impact list |

#### 7.5.1 Synthesiser delegation flow (updated)

```
Synthesiser activity (L2)
  → produces RepairPlan{target_domain, intent, evidence_pack}
  → calls Architect.EnforceContracts(plan)
      ├─ violations? → emit AmendedPlan or fail with rationale
      └─ accepted → continue
  → routes by target_domain to one or more L1 agents:
      target_domain = 'devops'        → DevOps.WriteOpsPatch
      target_domain = 'data'          → DataEngineer.WriteDataPatch
      target_domain = 'backend_code'  → Backend.WriteCode
  → collects PatchCandidate(s) from L1 agents
  → calls QA.GenerateTests in parallel
  → returns {patch_candidates[], test_suite} to Validator
```

For Domain 1 (DevOps) v1, the most common path is DevOps.WriteOpsPatch (manifest/Terraform/CI patches). Backend.WriteCode and DataEngineer.WriteDataPatch are wired but lit up incrementally as Domain 2/3 onboard.

### 7.6 Shadow Execution as a Correctness Primitive (formal)

This is **RC4** in the research proposal. Formal definition used to decide when a patch is "validated":

**Let** `B` = baseline output stream over window `W` of length `T`, `S` = shadow output stream from patched system over the same `T`. Let `D(B, S)` be a domain-appropriate distance (Wasserstein for numeric, Jaccard for sets, edit-distance for sequences). Let `θ` be the per-domain agreement threshold.

A patch is **shadow-validated** iff:

```
∀ output channel c ∈ C: D(B_c, S_c) ≤ θ_c           [agreement bound]
∧
sample_size(W) ≥ N_min                              [sufficient evidence]
∧
∀ failed_property p ∈ P_failed: SatisfiedByShadow(p) [the original failure no longer reproduces]
∧
∀ invariant i ∈ I_baseline: SatisfiedByShadow(i)    [no new violations introduced]
```

Default thresholds:
- `θ_numeric = 0.05` (Wasserstein, normalised)
- `θ_categorical = 0.02` (set distance)
- `θ_sequence = 1` (edit distance per record)
- `N_min = max(1000, 5% of W)`

These are configurable per-org via approval policies. Validator emits `shadow_confidence ∈ [0,1]` derived from the slack against thresholds; ApprovalGate uses this in its policy match.

### 7.7 Property-Based Testing

QA Agent generates property-based tests using:
- **Python**: [Hypothesis](https://hypothesis.readthedocs.io) — used for data-pipeline patches and Python service patches
- **JavaScript / TypeScript**: [fast-check](https://fast-check.dev) — used for Node service patches
- **Go**: `testing/quick` + [`gopter`](https://github.com/leanovate/gopter) — used for Go service patches
- **JVM**: jqwik — when JVM services are in scope

Properties are derived from:
1. The failed signal (the one that triggered the incident becomes the first property).
2. Architect-managed contracts (every API contract maps to a property: "for all valid inputs, output schema validates").
3. Historical regressions in vector memory (similar past incidents contribute property templates).

Properties are stored in S3 keyed by `org_id/service_id/property_id`, versioned, replayable across future incidents.

### 7.8 Inter-Agent Messaging Contracts (formal protocol)

This is **RC1** in the research proposal. Every agent-to-agent call uses a typed message contract. Contracts are:

- **Defined as Avro schemas** in `internal/services/_shared/events/schema/` and registered in AWS Glue Schema Registry.
- **Versioned** — backward-compatible changes increment minor; breaking changes require a v2 topic.
- **Validated at producer and consumer** — no untyped maps cross agent boundaries.

Core message types:

| Message | Producer | Consumer | Purpose |
|---|---|---|---|
| `FaultReport` v1 | Sentinel | Pathfinder | Normalised anomaly signal with severity + confidence |
| `RootCauseReport` v1 | Pathfinder | Synthesiser | Ranked hypotheses + blast radius + counterfactual evidence |
| `RepairPlan` v1 | Synthesiser | Architect → L1 agent | Intent, target domain, evidence pack, constraints |
| `ContractDecision` v1 | Architect | Synthesiser | accepted / violations[] |
| `PatchCandidate` v1 | L1 agent | Validator | Unified diff + explanation + risk assessment |
| `TestSuite` v1 | QA | Validator | Property-based tests + invariants |
| `ValidationReport` v1 | Validator | ApprovalGate | shadow_confidence + evidence URI + reproducer status |
| `ApprovalDecision` v1 | ApprovalGate or human | DevOps Agent | Approve / Reject / Escalate + policy id |
| `DeploymentEvent` v1 | DevOps Agent | recovery-engine | open / merge / sync / health / rollback |

**Conflict-resolution protocol** (when two agents claim authority over a repair domain — e.g. a patch touches both backend code and a data schema): Synthesiser routes to the **most specific** owner per Architect's ownership registry; if both are equally specific, the Architect Agent arbitrates (the contract registry is the source of truth) and emits a `RoutingDecision` event for audit.

**Negotiation** (when an L1 agent rejects a RepairPlan because it cannot produce a contract-compliant patch): the L1 agent returns `RepairPlanRejected{reason, alternatives[]}`; Synthesiser retries with an amended plan (max 2 rounds before escalating to human).

### 7.9 RLHF Feedback Loop

Engineer approval / rejection decisions and post-deployment health outcomes feed an RLHF corpus per agent:

- **Storage**: `feedback_examples` table (org_id, agent, decision, reward signal: +1 approve, -1 reject, +0.5 healthy-deploy, -0.5 rollback). Plus the input artifact (RepairPlan, PatchCandidate, ValidationReport) snapshot.
- **Training**: weekly Spark job (`failure-classifier-train` extension) builds a per-agent **few-shot prompt corpus** (NOT model fine-tuning — too expensive at MVP scale). For each agent, picks the top-k highest-reward, most-recent examples to inject into the system prompt's `<examples>` block.
- **Rollout**: prompts are versioned and behind a flag — new prompt corpus is shadow-tested for 7 days against a hold-out incident set before being promoted.
- **Audit**: every prompt update is recorded in `prompt_versions` with diff vs prior, dataset hash, and approval by the NEXIS-internal admin role.

Future (post-MVP): graduate to LoRA fine-tunes per agent on Bedrock or Anthropic batch fine-tuning when API supports it.

---

## 8. Approval Gate, GitOps, Rollback

### 8.1 Policy match order

1. Most specific matcher first (kind + severity + service + source).
2. If no match: default policy = `require_human` with required role from connector setting.
3. Auto-merge requires explicit `auto_merge` action AND `env != 'production'` AND signed-off by org admin role on the policy.

### 8.2 PR conventions

- Branch: `nexis/incident-<short-id>-<slug>`
- PR title: `[NEXIS] <short summary>`
- PR body sections: TL;DR, Root cause, Patch explanation, Validation results (table), Blast radius, Approve in NEXIS dashboard link, Reject link.
- Labels: `nexis`, `severity:<level>`, `auto-generated`.

### 8.3 Health gate

- After merge → ArgoCD sync → wait `health_window_minutes` (default 30).
- Health = healthy if: ArgoCD reports `Synced + Healthy` AND Sentry error-rate delta over baseline < 25%.
- On unhealthy: GitOps service opens a revert PR via GitHub App, marks deployment `rolled_back`, posts comment on original PR, opens follow-up incident.

---

## 9. Dashboard (shadcn/ui)

### 9.0 Visual System — "Workspace" aesthetic

The dashboard takes its visual cue from **ElevenLabs / Linear app / Vercel dashboard** — a calm, premium, friendly workspace. **Light mode is default**, dark mode fully supported, system-preference auto-detected, persisted per user.

> The marketing landing page (`site/app/page.tsx`) is **rebuilt in a Novita.ai-style light aesthetic** — bold black display typography on white, animated SVG isometric wireframes (drawing-stroke + floating green dots), emerald-500 CTA. Not dark-locked anymore. Console and landing share Inter + JetBrains Mono and the Nexis wordmark; everything else is independent.

#### 9.0.1 Theming

- Token system: HSL CSS variables (shadcn convention) on `:root` and `.dark`.
- Toggle library: **`next-themes`** with `class` strategy on `<html>`. Default `system`, override per user.
- Theme toggle UI: icon button in the top header (sun / moon / system), persisted via localStorage + synced to user profile in DB.
- All custom components consume tokens, never hard-coded hex.

#### 9.0.2 Tokens (light)

```
--background:      0 0% 100%        /* #ffffff */
--foreground:      240 10% 4%       /* #0a0b0e — near-black ink */
--muted:           240 5% 96%       /* #f4f4f5 — sidebar / surface alt */
--muted-foreground:240 4% 46%       /* #71717a — secondary text */
--card:            0 0% 100%
--card-foreground: 240 10% 4%
--popover:         0 0% 100%
--popover-foreground: 240 10% 4%
--border:          240 6% 90%       /* #e4e4e7 */
--input:           240 6% 90%
--ring:            240 5% 65%
--primary:         240 6% 10%       /* near-black buttons */
--primary-foreground: 0 0% 98%
--secondary:       240 5% 96%
--secondary-foreground: 240 6% 10%
--accent:          240 5% 96%
--accent-foreground: 240 6% 10%

/* status colors (semantic, work in both modes) */
--success:         142 71% 45%      /* #22c55e */
--warning:         38 92% 50%       /* #f59e0b */
--danger:          0 84% 60%        /* #ef4444 */
--info:            217 91% 60%      /* #3b82f6 */

/* signature accent — soft violet, used sparingly */
--brand:           261 83% 58%      /* #7c3aed */
--brand-foreground: 0 0% 100%

--radius:          0.625rem         /* 10px — softer than landing's 6px */
```

#### 9.0.3 Tokens (dark)

```
--background:      240 10% 4%       /* #0a0b0e */
--foreground:      0 0% 98%
--muted:           240 6% 10%
--muted-foreground:240 5% 65%
--card:            240 8% 7%        /* #111114 */
--card-foreground: 0 0% 98%
--popover:         240 8% 7%
--popover-foreground: 0 0% 98%
--border:          240 6% 16%       /* #26262b */
--input:           240 6% 16%
--ring:            240 5% 50%
--primary:         0 0% 98%         /* white buttons in dark */
--primary-foreground: 240 6% 10%
--secondary:       240 6% 12%
--secondary-foreground: 0 0% 98%
--accent:          240 6% 12%
--accent-foreground: 0 0% 98%
--brand:           261 83% 65%      /* lifted in dark */
--brand-foreground: 240 10% 4%
```

#### 9.0.4 Typography & spacing

- Body: **Inter** (already loaded), 14px base, line-height 1.55
- Display: Inter at 28/22/18/16, weights 500 / 600 only
- Mono: **JetBrains Mono** (already loaded) for IDs, codes, mono-in-table cells
- Spacing scale: Tailwind defaults; section gaps 24/32/48
- Radius: cards 10px (`rounded-xl`), inputs/buttons 8px (`rounded-lg`), pills full
- Borders: 1px, token-driven; **no drop shadows on cards** (shadows reserved for popover/dropdown)
- Subtle elevation in light mode: `bg-white` on `bg-muted` page background (the "card-on-canvas" feel ElevenLabs uses)

#### 9.0.5 Layout chrome

- **Sidebar**: 240px fixed on desktop, collapsible to 64px (icon-only), drawer on mobile (Sheet). Surface = `bg-muted`.
  - Sections: workspace switcher (top), primary nav, "Library" group (Live Demo, Audit), "Workspace" group (Integrations, Settings, Members), bottom = invite teammate CTA + plan tier
- **Top bar**: 56px, sticky, `bg-background/80 backdrop-blur`. Left: breadcrumbs + page title. Right: global search (cmdk via `command`), theme toggle, notifications (popover), avatar menu.
- **Page container**: max-w `1280px`, padding `px-6 py-8`, vertical rhythm `space-y-8`.

#### 9.0.6 Signature components (built once, reused)

- `WorkspaceShell` — sidebar + topbar + outlet, handles theme & responsive
- `PageHeader` — title, description, action row (right-aligned buttons)
- `StatTile` — label, big number, delta arrow, optional sparkline (Recharts)
- `QuickActionCard` — icon + title + 1-line description, large click target, soft hover lift; mirrors ElevenLabs "Instant speech / Audiobook" cards
- `SectionHeader` — small uppercase eyebrow + title + optional "see all" link
- `EntityRow` — avatar/icon + title + subtitle + tags + right-aligned meta (used in Latest activity, Approval queue, Audit, Members)
- `EmptyState` — illustration + title + description + primary CTA
- `Greeting` — time-of-day-aware "Good morning, Ratul" header for the Home page
- `IncidentStatusBadge`, `SeverityPill`, `ConnectorHealthDot`, `PipelineStepIndicator`, `PatchDiffViewer` (Monaco), `LivePipelineCanvas`, `EvidencePanel`, `ApprovalActionBar`, `AuditEntryRow`, `MetricsTile`

### 9.1 Stack

- Next.js 16.2.2 App Router (read `node_modules/next/dist/docs/` before writing route code)
- Tailwind v4 (CSS-first config, `@theme` directive for tokens above)
- shadcn/ui (Radix primitives), components copied into repo under `site/components/ui/`
- **next-themes** (theme toggle, class strategy)
- TanStack Query v5 (server state)
- Zustand (UI state)
- Recharts (metrics charts + sparklines)
- react-hook-form + zod (forms)
- **Clerk** (`@clerk/nextjs`) — identity, session, OAuth/passkey/MFA/SAML, organization switcher, invite UI, device list (see §11)
- Monaco editor (patch diff viewer)
- xterm.js (Live Pipeline Demo terminal pane)
- Framer Motion (kept for marketing site; minimal use in console — `transition` utilities only)
- **lucide-react** (icon set — matches shadcn defaults)
- **cmdk** (global ⌘K palette for search + nav)

### 9.2 shadcn/ui components used (initial set)

`button`, `input`, `label`, `form`, `card`, `dialog`, `sheet`, `dropdown-menu`, `popover`, `tooltip`, `toast` (or `sonner`), `select`, `separator`, `tabs`, `table`, `badge`, `avatar`, `skeleton`, `command` (cmdk), `breadcrumb`, `scroll-area`, `progress`, `alert`, `alert-dialog`, `accordion`, `switch`, `radio-group`, `checkbox`, `data-table` (with TanStack Table), `hover-card`, `navigation-menu`, `resizable` (for Live Demo split panes), `sidebar` (the new shadcn `sidebar` block).

### 9.3 Surface specs

#### 9.3.0 Home (new — replaces a generic "dashboard" landing)

ElevenLabs-style home page, the first thing users see after login:

- **Greeting**: `Good morning, <name>` / `Good afternoon, <name>` / `Good evening, <name>`. Subtitle: `<org-name> Workspace`.
- **Announcement chip** (optional, dismissible): "What's new in NEXIS" rotating message.
- **Quick action grid** (5 `QuickActionCard`s):
  1. **Run Live Demo** → `/console/demo`
  2. **View open incidents** (count badge) → `/console/incidents?status=open`
  3. **Approve queue** (count badge) → `/console/approvals`
  4. **Connect integration** → `/console/integrations`
  5. **Invite teammate** → `/console/settings/members`
  Each card has a soft pastel illustration / icon; hover lifts via subtle scale + shadow.
- **"Latest activity"** section — `EntityRow` list of last 8 incidents/audit events with status badge + timestamp + actor.
- **"Pipeline health"** section — 3 `StatTile`s: MTTR (with delta vs last week), Auto-recovery rate, Validation pass rate. Each tile has a 7-day sparkline.
- **"Get started"** section (collapsible, only shows until org has ≥1 connector + ≥1 incident) — checklist:
  - [ ] Connect GitHub
  - [ ] Connect Sentry
  - [ ] Run your first Live Demo
  - [ ] Invite a teammate
  - [ ] Set up an approval policy

#### 9.3.1 Incidents

- **Index**: data-table with filters chip-row above (severity, status, source, service, time range). Header row of `MetricsTile`s. Default filter: `is_demo=false`. Empty state shows `EmptyState` with "Run Live Demo" CTA.
- **Detail**: 2-column layout (`resizable`).
  - Left (40%): incident header (title, status badge, severity pill, service link, source link, timestamps), recovery `PipelineStepIndicator` (vertical) with current step highlighted, side facts (assigned reviewer, policy matched).
  - Right (60%): tabs — RCA, Patch diff (Monaco, supports light + dark theme via `monaco-editor` themes), Validation results (per-candidate cards with pass/fail breakdown), Audit (entry list).
  - Bottom action bar: `ApprovalActionBar` — Approve / Reject (with reason), Comment, Re-run validation. RBAC-aware (disabled with tooltip when user lacks role).
- Live updates via SSE.

#### 9.3.2 Approval Queue

- Filtered list of `awaiting_approval` incidents the current user can decide.
- `EntityRow` with inline Approve / Reject buttons + deep-link to detail.
- Bulk select for batch approve with confirmation `alert-dialog`.

#### 9.3.3 Audit Log

- Append-only data-table view of `audit.audit_events`.
- Filters: actor, action, resource kind, date range (date picker popover).
- Row click opens side `sheet` with full payload JSON (read-only).
- "Export CSV" button → toast progress → email link to signed S3 URL.

#### 9.3.4 Integrations

- Connector cards (GitHub, Sentry, ArgoCD) in a 3-col grid.
- Each card: provider logo, name, `ConnectorHealthDot`, last-sync time, "Configure" / "Disconnect" menu.
- Empty state per connector: "Connect" button opens `dialog` with step-by-step instructions + OAuth button or token paste form.
- "Add another" tile at end (for future connectors).

#### 9.3.5 Settings

- Sidebar-secondary nav (sub-sidebar inside the page) with sections: **Profile**, **Org**, **Members & Roles**, **Approval Policies**, **Connectors** (link to 9.3.4), **Notifications**, **Theme** (light/dark/system), **Billing** (placeholder card "Billing coming in v1.5").
- Members & Roles: data-table of org members + invite dialog + role-change dropdowns (RBAC-guarded).
- Approval Policies: list + create + edit with zod-validated form; matcher UI built from selectors (severity, source, service, env).
- Theme settings tab is a UI mirror of the topbar toggle for discoverability.

#### 9.3.6 Agents (Admin) — Fleet panel

Visible to `admin` role (and `sre` read-only). Surfaces the **9 agents** as first-class entities so engineers, customers, and the FYP/MS examiner can see the multi-agent system working in real time.

- **Index — Fleet view**:
  - 9 agent cards in a 3×3 grid (5 L1 cards + 4 L2 cards) with **layer badge** (L1 Execution / L2 Self-Healing).
  - Per card: agent name, layer, current status (`active` / `idle` / `degraded` / `down`), 7-day **task volume** sparkline, **success rate**, **p95 latency**, **cost** (LLM tokens for Pathfinder/Synthesiser, pod-minutes for Validator), **last task at**.
  - **Delegation graph** (mini visualization) showing agent→agent message flow over the last hour (Synthesiser → Architect → DevOps Agent etc.). Built from Kafka workflow.steps events.
- **Detail — Per-agent page** (tabs):
  1. **Overview** — role, owns, layer, hosting service, version, deployed image SHA, dependencies (LLM / Neo4j / Pinecone / etc.), config (read-only).
  2. **Tasks** — paginated list of every agent invocation (org-scoped) with input summary, output summary, duration, status, related incident link. Filters: status, time, related service, related incident.
  3. **Performance** — Recharts panel: tasks/min, p50/p95 latency, success rate, cost over time. Per-step breakdown for Synthesiser (LLM call vs. retrieval vs. delegation).
  4. **Reasoning** (Pathfinder/Synthesiser only) — sample of recent decisions with hypothesis ranking, counterfactual evidence (Pathfinder), retrieved memory docs (Synthesiser), prompt + response (Synthesiser), reward signal (RLHF).
  5. **Audit** — every state-changing decision the agent has made (subset of audit log filtered to agent).
  6. **Delegation** (L2 agents only) — graph + table of delegations to L1 agents with acceptance / rejection counts, conflict-resolution events.
  7. **RLHF** — current prompt corpus version, examples in active corpus, recent reward distribution, pending shadow-tested next version.
- Each row links back to its incident; deep-linkable URLs for examiner walkthrough.

This panel is the **demonstration centerpiece** for the academic defense — it makes the multi-agent architecture visible and inspectable, not just an architecture diagram.

#### 9.3.7 Live Pipeline Demo

- **Top**: scenario picker — `QuickActionCard`-style cards per scenario (icon, title, description, expected outcome chip).
- **Center canvas**: `LivePipelineCanvas` — horizontal pipeline graph (Sentinel → Pathfinder → Synthesiser → Validator → ApprovalGate → GitOps). Nodes pulse / fill as the Temporal workflow advances. Driven by real workflow events, not fake animation.
- **Bottom split** (`resizable`): left = live xterm terminal showing service log lines (NEXIS-internal log stream filtered by workflow id); right = active artifact panel (RCA hypotheses → patch diff → validation results) updating as steps complete.
- **Controls bar**: Run, Pause (sends `pause` workflow signal), Reset (closes workflow + hard-deletes demo incident + resets fixture repo), Showcase Mode toggle (`switch`).
- **Showcase Mode**: 2s pacing between steps, narration captions render below each completed node, "Next Step" manual button appears (gated workflow signal) for paced academic defense walk-through.
- Each demo run creates a real `incidents` row with `is_demo=true`. Reset hard-deletes those rows + S3 artifacts.

### 9.4 Data fetching pattern

- All reads via TanStack Query against api-gateway REST endpoints generated from OpenAPI 3.1 spec (oapi-codegen for Go server, openapi-typescript for TS client).
- Live updates via Server-Sent Events (`/v1/incidents/{id}/stream`, `/v1/demo/runs/{id}/stream`) for incident detail and Live Pipeline Demo.
- Mutations via TanStack Query mutations + optimistic updates where safe.
- Cache invalidation: org-scoped query keys, invalidated on workflow signal events.

### 9.5 Auth glue

- **Clerk** (`@clerk/nextjs`) handles all identity flows (sign-in/up, passkey, MFA, SAML, OAuth, invite acceptance, org switcher, session/device UI). Components themed via shadcn-compatible appearance config.
- Clerk session JWT (in `__session` HttpOnly cookie) sent to api-gateway on every request; api-gateway verifies + injects org/role context into Postgres RLS (§11.3).
- `useUser()` / `useOrganization()` / `useSession()` from `@clerk/nextjs` for client-side session reads.
- Next.js middleware guards `/console/*`: `clerkMiddleware()` + custom check for `org_id` claim; redirects to `/sign-in` if absent.
- Org switcher in topbar uses Clerk's `OrganizationSwitcher` component (themed).
- "Sign out" calls `signOut()` → Clerk revokes session, dashboard cookie cleared.

### 9.6 Existing scaffolding

- The current `site/app/(console)/` route group keeps its layout shell — replaced with the new `WorkspaceShell` chrome. Routes:
  - `/console` → Home (9.3.0)
  - `/console/incidents` → Incidents index
  - `/console/incidents/[id]` → Incident detail
  - `/console/approvals` → Approval queue
  - `/console/audit` → Audit log
  - `/console/integrations` → Integrations
  - `/console/settings/*` → Settings nested routes
  - `/console/demo` → Live Pipeline Demo
- The marketing landing page at `site/app/page.tsx` is **rebuilt** in a Novita.ai-style light aesthetic (Phase 5 deliverable, partially shipped already): big black display headline, animated SVG wireframes (`components/visuals/IsometricWireframe.tsx`), emerald-500 CTA, agent-status live card. Below-fold sections (Problem / HowItWorks / Agents / Metrics / CTA / Footer) are ported to the same light system in a follow-up pass. **Theme system is scoped to `/console/*` only** (dark-mode toggle); the landing page stays light by default with no toggle.
- The empty `app/dashboard/` and `app/admin/` stubs are deleted.

### 9.7 Accessibility

- All interactive elements keyboard-navigable (Radix gives us this for free).
- Color contrast AA in both modes (token palette designed for it).
- Focus-visible rings on all focusable elements (`ring` token).
- `prefers-reduced-motion`: existing hook reused; pipeline canvas animations skip transitions when set.

---

## 10. Live Pipeline Demo — Subsystem Detail

### 10.1 Goal

Run the **real** pipeline against deterministic, NEXIS-owned fixtures so professors / clients see the actual product. Not a slideshow, not a mock.

### 10.2 Fixture format

`demo-fixtures/scenarios/<scenario-id>/`:

- `scenario.yaml` — id, title, description, expected outcome, tags
- `inbound-event.json` — the synthetic Sentry / GitHub Actions / ArgoCD payload
- `repo/` — a tiny git repo NEXIS owns (committed in fixtures) containing the broken code
- `expected-patch.diff` — for regression testing, not shown to the LLM
- `narration.md` — captions used in Showcase Mode

### 10.3 Demo run lifecycle

1. User clicks Run on scenario X.
2. Dashboard calls `POST /v1/demo/scenarios/{id}/run`.
3. Api-gateway routes to integrations service, which:
   - Creates a fresh demo `incidents` row (`is_demo=true`, `scenario_id=X`).
   - Replays the fixture's `inbound-event.json` through the same normalization path real connectors use.
   - Starts Temporal workflow with the demo input.
4. Workflow runs end-to-end. Validator activity is configured to use a local Docker runtime in dev / a small Fargate pool in prod (still real validation, just on the fixture repo).
5. PR opens against the **fixture repo** (which lives inside NEXIS infra, not on github.com — uses Gitea or in-process git for full isolation; OR uses a real github.com org NEXIS owns called `nexis-demos` if simpler).
6. Workflow result drives the dashboard live view.
7. Reset endpoint: `POST /v1/demo/scenarios/{id}/reset` — terminates workflow, deletes incident + artifacts, resets the fixture repo to baseline.

**Decision needed during implementation**: Gitea (in-cluster, fully isolated, reproducible) vs. real GitHub org `nexis-demos` (more realistic, network dependency). Plan defaults to **Gitea** for v1 (zero network coupling = reliable demos under bad WiFi at examiner desk).

### 10.4 Showcase Mode

- Workflow input has `showcase_mode: true` → activities call `workflow.Sleep(2*time.Second)` between major steps.
- Dashboard subscribes to a different SSE channel that includes narration text from `narration.md` for each step.
- "Next Step" button sends a `step_advance` workflow signal; in showcase mode the workflow waits for this signal between steps instead of auto-advancing.

### 10.5 Determinism

- LLM calls in demo mode use a fixed temperature 0 and a per-scenario seed via `metadata` field, so outputs are reproducible enough for academic demos.
- For 100% determinism (offline FYP defense): a per-scenario `recorded-llm-responses.json` can be replayed instead of calling the LLM. Toggle: `NEXIS_DEMO_REPLAY_LLM=true` env var.

---

## 11. Auth + RBAC + Multi-tenancy (Enterprise-grade)

### 11.0 Architecture

NEXIS uses a **hybrid identity model**:

- **Clerk** (hosted identity provider) owns the user-facing identity surface: passkeys, MFA, OAuth (GitHub/Google/Microsoft), magic link, SAML SSO, email/password, organization invites, session management UI.
- **Auth Service** (NEXIS-owned) syncs from Clerk via signed webhooks, owns the **authoritative** RBAC + ABAC + API key + audit + brute-force-policy state in Postgres. Every internal request decision uses NEXIS DB, not Clerk live calls.

This split gives us SOC 2 / SAML / passkey support **on day one** without owning the password-storage / session-management blast radius, while keeping fine-grained RBAC and API keys under our control.

### 11.1 Identity layer (Clerk)

| Capability | Source |
|---|---|
| **Passkeys / WebAuthn** | Clerk (built-in, FIDO2) |
| **MFA: TOTP + backup codes + SMS (optional)** | Clerk policy, **enforced per-org** (admin can require MFA org-wide) |
| **OAuth providers** | GitHub (v1), Google (v1), Microsoft (v1.5) |
| **Magic link / OTP email** | Clerk (passwordless) |
| **Email + password** (with breach detection via HIBP) | Clerk |
| **SAML / SSO (Okta, Azure AD, Google Workspace)** | Clerk Pro |
| **SCIM provisioning** | Clerk Pro (v1.5 — when first SAML customer lands) |
| **Bot protection** (CAPTCHA, IP intelligence) | Clerk |
| **Session management UI** (active devices, revoke) | Clerk + mirrored in NEXIS Settings |
| **Device fingerprinting + impossible-travel** | Clerk |
| **Email verification + password reset** | Clerk |
| **Account lockout after N failed attempts** | Clerk |

**Why Clerk (not WorkOS / Auth0 / build):** B2B SaaS organization model maps natively to our `orgs` table; passkeys + SAML in one product; per-MAU pricing aligns with growth; React components ship with shadcn-compatible styling. WorkOS becomes worth it when enterprise-only features dominate (~$50k+ ARR deals); we can migrate later if needed because our Auth Service holds the authoritative state.

### 11.2 Sync from Clerk → NEXIS

Auth Service exposes `POST /v1/webhooks/clerk` consuming verified Svix signatures. Events handled:

| Clerk event | Action in NEXIS |
|---|---|
| `user.created` | Insert into `users` (email, clerk_user_id, name, avatar) |
| `user.updated` | Update mirrored fields |
| `user.deleted` | Soft-delete (keep audit trail), revoke all org_members rows + sessions + API keys |
| `organization.created` | Insert into `orgs` (name, slug, clerk_org_id), seed default roles + admin policy + KMS DEK |
| `organization.updated` | Update mirrored fields |
| `organization.deleted` | Cascade soft-delete |
| `organizationMembership.created` | Insert `org_members` with role mapped from Clerk org role |
| `organizationMembership.updated` | Update role |
| `organizationMembership.deleted` | Delete row + revoke API keys scoped to that user-org |
| `session.created` / `session.removed` | Append to `auth_sessions` for audit + active-device list |

All webhook handlers are idempotent (use `clerk_event_id` unique constraint).

### 11.3 Per-request authentication (api-gateway)

Every inbound request:

1. Extract Clerk session JWT from `__session` cookie OR `Authorization: Bearer <token>` (programmatic).
2. **Verify JWT signature** against Clerk JWKS (cached, refreshed every 60 min).
3. Verify `iss`, `aud`, `exp`, `nbf`. Reject if invalid.
4. Pull `clerk_user_id` + active `clerk_org_id` claim → look up local `users.id` + `org_members.role` from cache (Redis, 60s TTL) or Postgres.
5. Attach to context: `user_id`, `org_id`, `role`, `permissions[]`, `is_authenticated=true`, request id.
6. Set Postgres session var `app.current_org_id` for RLS.

For programmatic API key requests: extract API key, verify hash, look up `api_keys` row → resolve `org_id`, `created_by_user_id`, `scopes[]`, then same context attach. API keys never share Clerk JWT path.

### 11.4 Roles + ABAC

| Role | Default permissions |
|---|---|
| `owner` | Everything in org. Cannot be removed. Org has exactly one. |
| `admin` | Everything except billing transfer + delete org. |
| `sre` | All incident + approval actions, manage policies, manage connectors, view audit, manage members (no role escalation to admin). |
| `engineer` | Approve incidents for own services, view all org incidents, run Live Demo, edit own profile. |
| `reviewer` | Approve incidents for **assigned** services only (ABAC scope), view assigned incidents. |
| `viewer` | Read-only on incidents + audit + agents fleet. |
| `bot` | Reserved for service accounts (API keys). Permissions explicitly scoped per key. |

**ABAC layer**: every permission check is `Allow(ctx, action, resource)` where `resource` carries `org_id`, optional `service_id`, optional `team_id`. The service-level scoping for `reviewer` is enforced in `Allow()` — not in the route declaration — so adding new endpoints can't accidentally bypass scope.

**Permission registry** lives in code (`internal/platform/authz/permissions.go`) seeded into `permissions` table on migration. Tests assert every API endpoint has at least one required permission.

### 11.5 API keys (service accounts)

For programmatic access (CLI, CI integrations, customer scripts):

- Created by `admin` or `owner` in Settings → API Keys.
- Stored as **`bcrypt(api_key)`** in `api_keys` table; key shown to user **once** at creation.
- Each key has: name, scopes (subset of role permissions), optional service-level scope, optional IP allowlist, expiry (default 365 days, enforced rotation reminder at 90 days), `created_by_user_id`, `last_used_at`.
- Key format: `nxs_live_<env>_<random_32_bytes_base62>`. `nxs_test_*` for sandbox.
- Rate limited per-key (default 60 rpm, configurable per key via Redis token bucket).
- Revocable instantly; revocation propagates via Redis pub/sub to all api-gateway pods (cache invalidation).
- All API key usage logged to audit with key id + action + resource.

### 11.6 Sessions, devices, brute force

- Active sessions surfaced in dashboard Settings → Security: device, IP, last-active, "Revoke" button (sends Clerk session revoke).
- Brute force: Clerk handles the password-attempt rate limit. NEXIS adds a **per-org failed-API-key-attempt** counter (Redis, sliding window) — auto-suspends an org's API keys after 100 failed attempts in 5 min, alerting admins.
- **Impossible-travel** alerts: surfaces an audit event when the same user's session pops up from a geographically improbable location within a window (Clerk feature, mirrored to our audit).

### 11.7 CSRF / CORS / headers

- Clerk session cookie is `HttpOnly + Secure + SameSite=Lax`.
- State-changing requests require either: same-origin (browser fetch) **plus** a **double-submit anti-CSRF token** read from a separate non-HttpOnly cookie and echoed in `X-CSRF-Token` header; or a valid API key in `Authorization`.
- CORS: dashboard origin (`app.nexis.io`) only. No `*`.
- Security headers (set in api-gateway middleware): `Content-Security-Policy`, `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Strict-Transport-Security`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy`.

### 11.8 Auth audit

Every auth event logged to `audit.audit_events` with `actor_kind = 'auth'`:
- login.success / login.failure
- mfa.enrolled / mfa.removed / mfa.challenge_failed
- session.created / session.revoked
- password.changed / password.reset_requested
- api_key.created / api_key.revoked / api_key.used (sampled at 1% to control volume)
- role.changed / member.added / member.removed
- saml.assertion_received (with IdP fingerprint)

### 11.9 Compliance hooks

- All auth flows pass through a single audit middleware (no bypass).
- Data-export endpoint (`GET /v1/me/export`) — GDPR / CCPA — returns user's data archive (signed S3 URL).
- Account deletion endpoint (`DELETE /v1/me`) — soft-deletes, hard-purges after 30-day grace, audit row retained 7 years.
- SOC 2 controls implemented: CC6.x (logical access — Clerk + RBAC), CC7.x (system monitoring — observability), CC8.x (change management — Argo + audit), CC9.x (risk mitigation — backups + incident process).

---

## 12. Secrets + KMS Envelope Encryption

### 12.1 Customer-supplied secrets

```
plaintext_secret
  → Generate per-org Data Encryption Key (DEK) at org creation, encrypted by org-CMK in KMS, stored in `orgs.encrypted_dek`
  → Encrypt secret with DEK (AES-256-GCM) → store ciphertext in `connectors.encrypted_secret`
  → Decrypt at use time: load encrypted DEK → KMS Decrypt → AES-GCM decrypt secret → use immediately, never log
```

KMS CMK aliases: `alias/nexis-org-<org-id>` per org (created on org provisioning via Terraform null_resource or auth service bootstrap).

### 12.2 App secrets

- All app-level secrets (DB password, Sentry DSN, GitHub App private key, JWT signing secret, LLM API key) in AWS Secrets Manager.
- Pulled into pods via External Secrets Operator → mounted as env vars.
- No secrets in repo, no secrets in `.env.local` checked in (existing `.env.local.example` is the only template).

### 12.3 GitHub App private key

- Generated once, stored in Secrets Manager.
- Per-installation tokens minted on demand, cached in Redis with 50-minute TTL (GitHub tokens last 60).

---

## 13. Observability + Audit

### 13.1 Telemetry

Every Go service:
- OpenTelemetry SDK with auto-instrumentation for `chi`, `pgx`, `redis`, `aws-sdk-go-v2`, `temporal-sdk`.
- Exporter: OTLP → Grafana Cloud OTLP endpoint.
- Logs: structured JSON via `slog`. Correlated via trace_id/span_id.
- Metrics: standard runtime metrics + custom counters (incidents_started, validations_passed, llm_tokens_in/out, llm_cache_hits).

### 13.2 Dashboards (Grafana)

Pre-built and committed under `infra/grafana/dashboards/`:
- Recovery Loop Health: workflow start rate, success rate, p50/p95 duration per step.
- Cost: LLM tokens by model and org, validator pod minutes.
- API: request rate, p95 latency, 5xx by endpoint.
- Connector Health: healthy/degraded/down counts per connector kind.

### 13.3 Alerting

- PagerDuty integration (NEXIS's own ops). Alerts:
  - 5xx rate > 1% for 5 min
  - Workflow failure rate > 5% for 10 min
  - LLM provider error rate > 10% for 5 min
  - Postgres connection pool > 80% saturated for 5 min
  - Validator pod failure rate > 20% for 10 min

### 13.4 Audit pipeline

- Every state mutation in api-gateway / auth / gitops calls `audit.Log(event)` synchronously in the same transaction.
- Workflow activities also log audit events (actor_kind = `workflow`).
- Nightly job exports the previous day's audit_events to S3 (Parquet) and prunes Postgres rows older than 90 days. S3 retention: 7 years (compliance hedge).

---

## 14. Infrastructure (AWS / Terraform)

### 14.1 Layout

```
infra/
  envs/
    dev/        # individual dev sandboxes
    staging/    # shared
    prod/       # production
  modules/
    networking/
    eks/
    rds/
    redis/
    s3/
    kms/
    secrets/
    iam/
    grafana-cloud-link/
```

### 14.2 Resources

- **VPC**: 3 AZs, public + private + database subnets.
- **EKS**: 1 cluster per env. Node groups: `system` (m6i.large), `apps` (m6i.xlarge), `validator` (Fargate profile).
- **RDS Postgres 16**: Multi-AZ in prod, single-AZ in staging/dev. `db.r6i.large` prod, `db.t4g.medium` else.
- **ElastiCache Redis 7**: cluster-mode disabled in MVP, replication enabled in prod.
- **S3 buckets**: `nexis-artifacts-<env>`, `nexis-audit-export-<env>`, `nexis-validator-fixtures-<env>`. All with versioning + SSE-KMS + bucket policy denying non-TLS.
- **KMS**: per-env CMK + per-org alias.
- **Secrets Manager**: per-env secret tree.
- **ALB**: terminating TLS via ACM cert.
- **Route53**: `app.nexis.io`, `api.nexis.io`, `demo.nexis.io`.

### 14.3 ArgoCD (deploys NEXIS itself)

- ArgoCD installed in cluster.
- Per-service Application pointing at `infra/k8s/<service>` Helm chart.
- Sync auto-on for non-prod, manual for prod (matches our customer-facing safety story).

### 14.4 Cost guardrails

- AWS Budgets: alert at 80% of monthly budget per env.
- Per-org quota check before scheduling validator pods.
- LLM daily token cap per org + global cap.

---

## 15. CI/CD

### 15.1 GitHub Actions per repo

- **Lint**: `golangci-lint`, `gosec`, `eslint`, `tsc --noEmit`.
- **Test**: Go unit + Go integration (Postgres + Temporal via testcontainers-go) + Vitest unit.
- **Build**: docker build per service + Trivy scan + push to ECR (immutable tag = git sha).
- **E2E**: Playwright suite against ephemeral env (per PR — namespace per PR, torn down on close).
- **Deploy**: on merge to `main` → bump image tag in `infra/k8s/<service>/values.yaml` → ArgoCD picks it up → staging auto-syncs, prod requires manual sync.

### 15.2 Required checks

- All lint + test + build green
- E2E suite green
- Trivy: no HIGH/CRITICAL CVEs
- gosec: no HIGH issues
- License scan (FOSSA or `go-licenses`) clean
- Coverage delta ≥ 0% (no regression)

---

## 16. Testing Strategy

### 16.1 Unit

- Pure Go: `testing` + `testify` (assertions only, no mocks-of-mocks).
- Frontend: Vitest + React Testing Library.

### 16.2 Integration (the critical layer)

- Go: `testcontainers-go` spins up real Postgres + Redis + Temporal dev server per test package.
- Each pipeline activity has an integration test against real dependencies.
- LLM provider mocked with a fixture-driven fake at this layer (real LLM calls are too slow / costly per CI run); a separate nightly job runs a small LLM-real subset.

### 16.3 Workflow

- Temporal's `testsuite` package: workflow tests with mocked activities, asserting decision flow.

### 16.4 E2E

- Playwright: golden flow per surface
  1. Sign up via GitHub OAuth (mock GitHub via WireMock fixture).
  2. Connect a demo connector.
  3. Trigger Live Pipeline Demo "schema-drift" scenario.
  4. Wait for `awaiting_approval` status.
  5. Click Approve.
  6. Verify deployment row reaches `healthy`.

### 16.5 Coverage targets

- Go: ≥ 75% line, ≥ 80% in `internal/services/*`.
- Frontend: ≥ 60% line on `app/(console)/**` (lower because Playwright covers integration).
- Workflow definitions: 100% (small, critical).

### 16.6 Demo scenarios are tests

- Each Live Pipeline Demo scenario runs in CI nightly as an end-to-end test using `expected-patch.diff` for assertion. Failing scenarios block release.

---

## 17. Security Model

| Threat | Mitigation |
|---|---|
| Cross-tenant read | Postgres RLS + WHERE clause + per-request `current_org_id` setting |
| Stolen JWT | Short-lived access token, refresh rotation, server-side revocation list |
| GitHub App key compromise | Stored in Secrets Manager, mounted via ESO, never logged. Quarterly rotation. |
| Customer secret leak | KMS envelope encryption, per-org DEK, decrypt only at use, never logged |
| Validator pod escape | Non-root, read-only FS, no Docker socket, NetworkPolicy deny-all-egress, gVisor where available, ResourceQuota |
| LLM prompt injection from incident text | All inbound text passed as `user` message, never concatenated into system prompt; system prompt explicitly rejects instruction-overrides |
| Webhook replay | Verify signature + timestamp window (5 min) on every inbound webhook |
| CSRF | Double-submit cookie + SameSite=Lax |
| XSS | React default escaping + `dangerouslySetInnerHTML` lint ban + CSP header |
| Audit gap | Audit-log call inside same DB transaction as the mutation |

---

## 18. Production Readiness Gates

A service is "production-ready" only when ALL of:

- [ ] `/healthz` (liveness) and `/readyz` (readiness) implemented
- [ ] Structured JSON logging via `slog` with trace correlation
- [ ] OpenTelemetry traces + metrics emitted
- [ ] All public endpoints in OpenAPI spec
- [ ] Integration tests cover happy path + failure modes
- [ ] Helm chart with resource requests/limits, HPA, PDB
- [ ] Audit log calls on all mutations
- [ ] No customer secret in logs (verified via lint rule)
- [ ] Runbook in `docs/runbooks/<service>.md`
- [ ] Dashboard tile in Grafana

---

## 19. Phased Delivery (full-stack, ~28 weeks)

Aligns with Masters proposal Phase 2 timeline (10 weeks core prototype) but extends to launch-grade. Implementation plan (writing-plans skill) decomposes each phase further.

**Phase 0 — Foundations (week 1–3)**
- Refactor backend into the **9-service layout** per §2.7 (every service follows folder standard).
- Postgres schema migration: multi-tenancy + RLS + auth tables + agent_runs + approval_policies + memory_documents (pgvector) + audit.
- OpenAPI 3.1 spec scaffold + oapi-codegen wiring + Makefile `gen` target.
- Temporal cluster on local Docker Compose; EKS later in Phase 7.
- `platform/` package built out (env, otel, httpserver, pg, redis, kafka stub, authz, audit).
- CI scaffolds: lint + unit test + Trivy + golangci-lint + gosec on every push.

**Phase 1 — Auth + Multi-tenancy (week 4–6)**
- Clerk integration: organization model, Clerk webhooks → `auth` service sync.
- RBAC + ABAC enforcement in api-gateway + service-layer guards.
- API key issuance, hashing, scoping, rate-limit per key.
- Dashboard: Clerk React components, login, sign-up, invite acceptance, session/device UI, theme toggle.
- All endpoints under `/console/*` middleware-guarded.

**Phase 2 — Recovery loop on Temporal + Kafka (week 7–10)**
- MSK Serverless provisioned; Glue Schema Registry seeded with v1 message contracts (§7.8).
- Temporal workflow + activities for Sentinel/Pathfinder/Synthesiser/Validator/ApprovalGate.
- Pathfinder: Neo4j AuraDB + DoWhy counterfactual scoring.
- Synthesiser: Anthropic provider with prompt caching, Pinecone retrieval, Architect-contract gate, L1 delegation routing.
- Validator: real K8s Jobs (local Kind first, then EKS Fargate), property-test injection (Hypothesis/fast-check), shadow-correctness primitive (§7.6) implementation.
- Layer-1 agent stubs: each L1 service exposes its delegation API; only DevOps Agent fully implemented in Phase 4.

**Phase 3 — Integrations + Stream processing (week 11–13)**
- GitHub App (read+write) + installation flow.
- Sentry connector + webhook receiver.
- ArgoCD webhook + connector health checks.
- KMS envelope encryption per-org DEK.
- Flink job: `sentinel-anomaly` (windowed anomaly detection) deployed to EKS via FlinkKubernetesOperator.
- OpenLineage receiver in `dataops-integration` (stub).

**Phase 4 — GitOps + Rollback + DevOps Agent (week 14–15)**
- DevOps Agent (gitops service) `WriteOpsPatch` capability — generates K8s/Terraform/CI manifest patches from Synthesiser RepairPlan.
- PR open / merge / comment via GitHub App.
- ArgoCD watch + Sentry health-window correlation (§8.3).
- Auto-rollback on SLO breach.

**Phase 5 — Dashboard rebuild on shadcn/ui (week 16–19)**
- shadcn/ui install + light/dark token system (§9.0); next-themes wired.
- WorkspaceShell, sidebar, header with cmdk + theme toggle.
- Surfaces: Home (9.3.0), Incidents (index + detail), Approval Queue, Audit Log, Integrations, Settings.
- **Agents fleet panel (9.3.7)** — full 9-agent overview + per-agent detail tabs (Tasks, Performance, Reasoning, Audit, Delegation, RLHF).
- TanStack Query + SSE live updates from `workflow.steps` Kafka topic via api-gateway pump.

**Phase 6 — Live Pipeline Demo + remaining L1 agents (week 20–22)**
- Gitea in-cluster.
- Demo connector adapters (`demo-github`, `demo-sentry`, `demo-argocd`).
- 5 baseline scenarios in `demo-fixtures/scenarios/` + Showcase Mode + replay-LLM toggle.
- Live Pipeline Demo surface with `LivePipelineCanvas` + xterm + scenario picker.
- Architect Agent (arch-governance) — contract registry implementation + EnforceContracts gate.
- QA Agent (qa-automation) — property-test generation pipeline.
- Backend Agent stub (real impl Phase 8+ when Domain 3 onboards).
- Data Engineer Agent stub (real impl when Domain 2 onboards).

**Phase 7 — Production infra + observability + Spark (week 23–25)**
- Terraform for AWS (VPC, EKS, RDS, MSK Srvls, Pinecone PrivateLink, Neo4j Aura peering, ElastiCache, S3, KMS, Secrets Manager, Linkerd service mesh).
- Helm charts per service; ArgoCD installed; NEXIS deploys NEXIS.
- Grafana Cloud + dashboards (Recovery Loop Health, Cost, API, Connector Health, Kafka Lag, Flink Checkpoints, Spark Job State) + alert rules.
- EMR Serverless Spark — `validator-replay`, `memory-reembed`, `failure-classifier-train`, `audit-export` jobs.
- Playwright E2E in CI against ephemeral per-PR namespaces.

**Phase 8 — RLHF loop, hardening, design partner onboarding (week 26–28+)**
- RLHF feedback corpus pipeline (§7.9): `feedback_examples` table, weekly Spark training job, per-agent prompt corpus versioning + shadow test promotion.
- Penetration test pass (third-party).
- Disaster recovery drill (Postgres restore from snapshot, Temporal cluster rebuild, Kafka topic recovery).
- SOC 2 Type 1 audit kickoff.
- Onboarding doc + first 3 design partners.
- Bug fix bandwidth.

**Eval Track (parallel, Phases 5–8)** — Appendix A
- Build fault-injection harness.
- Recruit B3 cohort + IRB if applicable.
- Run experiments against B1/B2/B3.
- Author thesis + paper draft.

---

## 20. Out of Scope for v1

(Drastically smaller now that we're shipping the full architecture.)

- **Stripe billing** — manual invoicing for design partners. Stripe lands v1.5.
- **SCIM provisioning** — SAML SSO ships in v1 (Clerk Pro), SCIM lands when first SAML customer demands it.
- **Multi-region** — single region (us-east-1) at launch; multi-region in roadmap.
- **Self-hosted / on-prem deployment mode** — SaaS first; Helm chart for self-hosted lands when first enterprise customer requires data residency.
- **Slack-native approval** — notification with deep-link to dashboard only; in-Slack approve/reject button v1.5.
- **Auto-merge to production** — policy can opt in for non-prod environments only in v1; production auto-merge requires v2 trust framework.
- **Bedrock / OpenAI fine-tuning** — RLHF loop in v1 updates **prompt corpora**, not weights. Weight fine-tunes wait for cost justification.
- **Mobile app** — responsive web only.
- **Customer-side LLM proxy** — customers cannot bring their own LLM key in v1; v1.5.
- **Domains 2–6 full coverage** — services and contracts are scaffolded; full domain implementation rolls in incrementally post-launch with each design-partner demand.

---

## 21. Open Items to Confirm at Implementation Time

These are decisions we deferred because they're cheap to make later but locked in here as defaults:

- Embedding model: default Voyage `voyage-code-3`, fallback OpenAI `text-embedding-3-small`. Confirm Voyage account during Phase 2.
- Demo git host: default Gitea in-cluster. Confirm during Phase 6.
- Validator runtime: default EKS Fargate Jobs. Could move to Kata Containers for stronger isolation in v2.
- Email vendor: default Resend. Could move to SES if cost matters at scale.
- PagerDuty integration: NEXIS's own ops alerting; customer-facing PagerDuty is a v2 connector.
- Service mesh: default **Linkerd** for mTLS + traffic policy (lower complexity than Istio). Confirm in Phase 7.
- Schema registry: default **AWS Glue Schema Registry** (matches MSK Serverless). Confluent Schema Registry as fallback if we move to Confluent Cloud.

---

## Appendix A — Research Evaluation Track (Masters Thesis)

This appendix supports **RC1–RC5** in the Masters Research Proposal (`plantt.html`). It is intentionally separated from the product implementation phases so the eval track can run in parallel during Phases 5–8.

### A.1 Datasets

| Dataset | Source | Use |
|---|---|---|
| **Airflow incident corpus** | Public GitHub issues from `apache/airflow` labeled `kind:bug` + dataset of past failed DAG runs | Fault detection + RCA evaluation |
| **dbt test failures** | `dbt-labs/dbt-core` issues + community-contributed failure logs | Schema-drift recovery evaluation |
| **K8s rollout failures** | Synthetic — fault-inject malformed manifests + bad image tags into a NEXIS-owned reference app deployed across 12 K8s versions | DevOps recovery evaluation (primary) |
| **NEXIS synthetic fault corpus** | Generated via `tools/fault-injector/` — 4 fault classes × 50 instances each: schema drift, API contract violation, dependency breakage, deploy failure | Controlled benchmarking |

All datasets versioned in S3 under `s3://nexis-research-datasets/<name>/<version>/`.

### A.2 Baselines

| Baseline | Implementation | Comparison axis |
|---|---|---|
| **B1 — Rule-based alerting** | PagerDuty + custom Datadog monitors (representative SOTA for "alert-only" ops) | Detection latency, false-positive rate |
| **B2 — Single-agent LLM repair** | One Claude Sonnet call per incident: input = raw signal + repo, output = patch. No multi-agent structure, no validator. | Patch correctness, repair latency |
| **B3 — Human-only engineering team** | Recruited engineer cohort (n=8) given the same incidents in an isolated sandbox, instrumented for time-to-resolution + cognitive load | MTTR, NASA-TLX score |

### A.3 Target metrics (hypothesis from §4.1 of proposal)

| Metric | Target | Measurement |
|---|---|---|
| MTTR reduction vs B3 | ≥ 60% | Median wall-clock from fault detection to PR merged & validated |
| Patch correctness rate | ≥ 80% | Fraction of NEXIS-generated patches that pass the held-out test suite AND survive 7-day post-deploy health window without rollback |
| Engineer cognitive load reduction | ≥ 50% | NASA-TLX score (lower is better), self-reported after each task |
| Detection latency | < 90s p95 | Time from fault occurrence to FaultReport emit |
| False-positive rate (Sentinel) | < 5% | NEXIS-flagged faults that postmortem reveals as non-faults |
| Counterfactual hypothesis precision (Pathfinder) | ≥ 0.7 | Top-1 hypothesis matches ground-truth root cause |
| Shadow validation precision | ≥ 0.95 | Validator "validated" patches that pass post-deploy without rollback |
| Inter-agent round-trips per incident | ≤ 3 median | Count of L1↔L2 delegation hops |

### A.4 Experiment design

- **Within-subject for B3**: each engineer receives 4 incidents (2 NEXIS-assisted, 2 unassisted), Latin-square ordering to control for fatigue + learning.
- **Between-subject for B1/B2 vs NEXIS**: identical incidents from synthetic corpus, automated execution.
- Pre-registered analysis plan committed to `docs/research/` before evaluation begins.
- Statistical tests: Wilcoxon signed-rank for paired (NEXIS vs B3), Mann-Whitney U for unpaired, with Bonferroni correction across metrics.

### A.5 Reproducibility

- All NEXIS code released under permissive license at thesis submission.
- Evaluation harness (`tools/eval/`) packaged as a Docker image with seeded LLM responses (deterministic mode) so reviewers can reproduce results without API costs.
- Datasets and ground-truth labels published alongside thesis.

### A.6 Target publication venues

| Venue | Type | Contribution mapping |
|---|---|---|
| **ICSE** / **FSE** | Conference | RC1, RC2, RC3 |
| **VLDB** / **SIGMOD** | Conference | RC2 (data-pipeline self-healing slice) |
| **NeurIPS** / **ICML** | Conference | RC1 (multi-agent coordination), RC5 (causal localisation) |
| **ASE** | Conference | RC2, RC4 (shadow-execution primitive) |
| **AAAI** | Conference | RC1, RC3 |
