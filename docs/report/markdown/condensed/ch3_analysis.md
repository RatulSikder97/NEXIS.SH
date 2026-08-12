# Chapter 3: System Analysis and Requirements

## 3.1 Requirement Analysis

Nexis was built as a role-based, multi-tenant platform in which every datum belongs to exactly one organisation, and every request is evaluated against the caller's organisation and role. Given this, the requirement analysis begins naturally with the actors sitting at the system boundary. Three human actor types were identified:

- **Owner/Administrator** — owns membership, roles, billing, policy, integrations, and the final human decision on high-stakes automated repairs.
- **Engineer/Member** — investigates incidents, exercises the evaluation tooling, works the recovery pipeline daily.
- **Viewer** — a read-only observer (stakeholder or auditor): inspects system state, never changes it.

The role model itself is *additive*: each tier holds every permission of the tier below it, so Section 3.2 lists only what each role *adds*. (Internally, this maps to `owner`/`admin`/`member`; Viewer is RBAC-gated on the frontend and API.)

A fourth actor here is not human at all: the **nine-agent self-healing fleet**. It acts autonomously inside the system boundary, detecting faults without being prompted and writing directly to the console's own PostgreSQL, Neo4j, and object-storage stores, and its actions carry real consequences: GitHub pull requests, Argo CD rollbacks [6], preview redeploys. Roughly half of all incident, patch, and audit-table writes are agent-authored, so leaving the fleet out would misrepresent the system; modelling it instead makes the safety analysis explicit, since every Section 3.2.4 capability is bounded by a Section 3.3 control — sandboxing, severity classification, or the human approval gate.

Level 0/1 use case and data flow diagrams for all four actors appear in Sections 4.6–4.7.

## 3.2 Functional Requirements

### 3.2.1 Owner/Administrator can:

- Manage membership and roles, including the role-recommendation banner: accept applies under an optimistic-concurrency guard, dismiss starts a 30-day cool-down.
- Manage Stripe billing [13] and daily token budgets; export/verify the hash-chained audit log; get the daily digest; monitor System Health.
- Decide approvals (**Approve**, **Reject**, or **Modify**, where diff edits persist as RLHF feedback) and set recovery policy, including auto-rollback on SLO breach.
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

Isolation cannot depend on a developer remembering to add `WHERE org_id` to every query. Instead, RLS policies [7], bound per request to `app.current_org_id`, make the database itself refuse cross-tenant access, whether the request comes from a human or from one of the agents. A recovery run can span several minutes and often waits on a human decision, so pipeline state lives in Temporal [2] as journaled activities replayed deterministically after a crash — an in-memory orchestrator was rejected for exactly this reason, since it would simply strand incidents mid-repair. Generated code, moreover, is untrusted by construction: patches execute only sandboxed, patch blobs are encrypted at rest, tokens exist only as SHA-256 hashes (30-minute TTL, non-leaky 202), and audit-log tampering detectably breaks the chain.

## 3.4 System Feasibility

### 3.4.1 Technical Feasibility

Nexis was deliberately built as a polyglot system. The four independent backend services are written in Go, for static binaries, cheap concurrency, and a first-class Docker API client on which the webhook and container work depends. The console, however, is Next.js/React/TypeScript, chosen for its role-gated, SSE-streaming, Monaco-editing interface, while the causal sidecar runs on Python/FastAPI, on the scientific-Python/Hypothesis [9] stack. PostgreSQL 17 with pgvector [19] supplies transactions, RLS, and vector retrieval in one engine, whereas Neo4j [3] holds the dependency graph because diagnosis is graph traversal, which relational queries express poorly. Temporal [2] was proven by both live recovery runs in Chapter 6, and the whole system was built and verified on one development machine, with Podman [11] substituting for Docker [10].

### 3.4.2 Economic Feasibility

Every dependency Nexis relies on is open source, namely PostgreSQL, Neo4j Community, Redis, MinIO, Temporal, OpenTelemetry [5], and Grafana, so there is no licence cost. The one significant cost is LLM inference, handled as a dual path: OpenAI models for production, local Ollama [20] models (`gpt-oss:20b`, `qwen2.5-coder:14b`) for development. Every live run in this report in fact used the local provider, so the complete system was exercised at zero token cost; the cost tracker and daily budgets bound the paid path.

### 3.4.3 Operational Feasibility

On logging in, administrators see organisation-wide KPIs, approvals, and system health; engineers see incidents and the live pipeline; viewers see the same picture, read-only. High-stakes autonomy, in this regard, reduces to a single click, Approve, Reject, or Modify, optionally from Slack [17], while severity classification lets low-risk fixes pass unattended but never the sensitive ones. The built-in NASA-TLX instrument [18] lets the operator-workload claim eventually be measured rather than simply asserted; the intended operator, furthermore, is an ordinary engineer with a browser, not a dedicated operations team.
