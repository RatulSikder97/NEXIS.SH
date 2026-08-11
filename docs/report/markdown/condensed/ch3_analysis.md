# Chapter 3: System Analysis and Requirements

## 3.1 Requirement Analysis

Nexis is a role-based, multi-tenant platform: every datum belongs to exactly one organisation, and every request is evaluated against the caller's organisation and role; analysis begins with the actors at the system boundary. Three human actor types were identified:

- **Owner/Administrator** — owns membership, roles, billing, policy, integrations, and the final human decision on high-stakes automated repairs.
- **Engineer/Member** — investigates incidents, exercises the evaluation tooling, works the recovery pipeline daily.
- **Viewer** — a read-only observer (stakeholder or auditor): inspects system state, never changes it.

The role model is *additive* — each tier holds every permission of the tier below — so Section 3.2 lists only what each role *adds*. (Internally: `owner`/`admin`/`member`; Viewer is RBAC-gated on frontend and API.)

A fourth actor is not human: the **nine-agent self-healing fleet**. It acts autonomously inside the boundary — detecting faults unprompted, writing the console's own PostgreSQL, Neo4j, and object-storage stores, and triggering real side effects: GitHub pull requests, Argo CD rollbacks [6], preview redeploys. Roughly half of all incident, patch, and audit-table writes are agent-authored; omitting the fleet would misrepresent the system, and modelling it makes the safety analysis explicit: every Section 3.2.4 capability is bounded by a Section 3.3 control — sandboxing, severity classification, or the human approval gate.

The four actors' Level 0/1 use case and data flow diagrams appear in Sections 4.6–4.7.

## 3.2 Functional Requirements

### 3.2.1 Owner/Administrator can:

- Manage membership and roles, including the role-recommendation banner: accept applies under an optimistic-concurrency guard, dismiss starts a 30-day cool-down.
- Manage Stripe billing [13] and daily token budgets; export/verify the hash-chained audit log; get the daily digest; monitor System Health.
- Decide approvals — **Approve**, **Reject**, or **Modify** (diff edits persist as RLHF feedback) — and set recovery policy, including auto-rollback on SLO breach.
- Operate the preview-deploy engine; install GitHub App, Sentry [14], Datadog [16], PagerDuty [15], Slack [17] (in-message approve/reject), and Argo CD [6]; export evaluation CSVs.

### 3.2.2 Engineer/Member can:

- Authenticate via email/password (HttpOnly JWT cookie), magic link, or WorkOS SSO [12]; hashed, time-limited reset tokens; TOTP MFA; remote session revocation; scoped `nx_live_...` API keys.
- Create GitHub-connected projects; follow the nine-agent timeline over Server-Sent Events (SSE), inspecting per-agent status, tokens, cost, structured payloads.
- Trigger fault injection from 27 Live-Demo scenario cards (one wired end-to-end today, the rest "Coming soon").
- Run the OpenAI-versus-Ollama evaluation harness (persisted transcripts, costs); query the pgvector knowledge base [19]; complete the post-incident NASA-TLX survey [18].

### 3.2.3 Viewer can:

- Sign in and read: dashboard KPIs (MTTR, success rate, open incidents, token spend), live incident timelines, and project, performance, agents, and activity pages — never mutating anything.

### 3.2.4 The Nine-Agent Fleet autonomously:

- **Detects** (Sentinel): a per-row fatal trigger plus an EWMA spike rule (mean + 3σ, Poisson floor, warm-up fallback).
- **Diagnoses** (Pathfinder): walks the Neo4j graph [3]; the causal sidecar ranks candidates by evidence specificity, proximity, overlap, and scenario prior [4].
- **Routes** (Synthesiser): assigns a scenario (regex fast path, LLM fallback) and an enforced `selected_agents` list.
- **Plans** (Architect): `plan_steps`, `affected_files`, `risk_level` form a contract — diffs beyond `affected_files` force HIGH severity, never auto-deploying.
- **Codes** (Backend) a minimal LLM-guided unified diff; **Tests** (QA) with auto-generated pytest/Hypothesis property suites [9], replayed continuously, regressions raising fresh incidents.
- **Packages** (DevOps): CI/CD and Argo CD manifests [6]; **handles data** (Data Engineer): paired forward/reverse SQL migrations, `information_schema` drift detection, OpenLineage events [8].
- **Validates** (Validator): every patch runs in a Docker sandbox [10] (no network, read-only filesystem, dropped capabilities, PID limit, `no-new-privileges`) before the approval gate.
- **Classifies severity**: HIGH (schema drift, sensitive paths, unknown scenarios, contract violations); MEDIUM (120-second auto-approve countdown); LOW (tiny UI-only diffs) auto-approves.
- **Deploys** (GitOps) via real GitHub pull requests; **rolls back** via Argo CD on post-deploy regression, policy permitting; **records** each step as a Temporal-durable SSE-streamed event [2]; and refiles deploy-engine build failures as incidents: broken builds self-heal too.

## 3.3 Non-Functional Requirements

**Table 3.1: Non-functional requirements and realisation.**

| Requirement | Realisation in Nexis |
|---|---|
| Multi-tenancy | PostgreSQL Row-Level Security (RLS) on essentially every table [7] |
| Durability | Temporal-orchestrated workflows survive crashes and restarts [2] |
| Security | Sandboxed patch execution, encrypted patch storage, hashed tokens, hash-chained audit log |
| Scalability | Stateless Go services, queue-decoupled workers, parallel dependency probes |
| Responsiveness | Server-Sent Events stream the live incident timeline |
| Usability | Role-scoped console, one-click approvals, plain-language rationales |

Isolation must not depend on code remembering `WHERE org_id`: RLS policies [7], bound per request to `app.current_org_id`, make the database itself refuse cross-tenant access, humans and agents alike. A recovery run spans minutes and awaits a human decision, so pipeline state lives in Temporal [2] as journaled activities replayed deterministically after a crash — an in-memory orchestrator would strand incidents mid-repair, and was rejected. Generated code is untrusted by construction: patches execute only sandboxed, patch blobs are encrypted at rest, tokens exist only as SHA-256 hashes (30-minute TTL, non-leaky 202), and audit-log tampering detectably breaks the chain.

## 3.4 System Feasibility

### 3.4.1 Technical Feasibility

Nexis is deliberately polyglot. Four independent backend Go services get static binaries, cheap concurrency, and a first-class Docker API client for webhook and container work. Next.js/React/TypeScript serves the role-gated, SSE-streaming, Monaco-editing console; the Python/FastAPI causal sidecar sits on the scientific-Python/Hypothesis [9] stack. PostgreSQL 17 with pgvector [19] supplies transactions, RLS, and vector retrieval in one engine; Neo4j [3] holds the dependency graph because diagnosis is graph traversal, poorly expressed relationally. Temporal [2] was proven by both live recovery runs in Chapter 6; everything was built and verified on one development machine, Podman [11] substituting for Docker [10].

### 3.4.2 Economic Feasibility

Every dependency is open source (PostgreSQL, Neo4j Community, Redis, MinIO, Temporal, OpenTelemetry [5], Grafana) — no licence cost. LLM inference, the one significant cost, is dual-path: OpenAI models for production, local Ollama [20] models (`gpt-oss:20b`, `qwen2.5-coder:14b`) for development. Every live run in this report used the local provider — the complete system exercised at zero token cost; the cost tracker and daily budgets bound the paid path.

### 3.4.3 Operational Feasibility

Administrators land on organisation-wide KPIs, approvals, and health; engineers on incidents and the live pipeline; viewers on the same truth, read-only. High-stakes autonomy reduces to one-click Approve/Reject/Modify, optionally from Slack [17]; severity classification lets low-risk fixes pass unattended while sensitive changes never do. The built-in NASA-TLX instrument [18] lets the operator-workload claim eventually be measured, not asserted; the intended operator is an ordinary engineer with a browser — no dedicated operations team.
