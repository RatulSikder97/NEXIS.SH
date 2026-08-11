# Chapter 4: System Design and Architecture

This chapter presents the design of Nexis at complementary levels: the structural view (Sections 4.1–4.5), the functional view via use case diagrams (4.6), the data view via Data Flow Diagrams (4.7), and a UML sequence diagram of one complete recovery run (4.8). The self-healing loop is given the most space, because it is the central contribution of the project.

## 4.1 System Context

Figure 4.1 places Nexis at the centre of its environment: the three human roles (Owner/Administrator, Engineer/Member, Viewer), the nine-agent fleet as a fourth, autonomous actor, and the external systems — GitHub, Slack [17], the LLM provider (OpenAI or Ollama), Argo CD, Stripe [13], alert sources Sentry [14], Datadog [16], and PagerDuty [15], WorkOS SSO [12], and SES/SMTP mail.

![](../../assets/diagrams/v2-context.png){width=74%}

**Figure 4.1: System Context Diagram.** ( Nexis and the external actors and systems it exchanges data with ).

Every entry point (web console, webhooks, Slack callbacks, plain HTTP clients) converges on the control-plane — no human or agent talks to a data store or external integration directly. An honest status note: the `nexis.sh` domain is registered, but the platform is not yet publicly live (Section 4.5).

## 4.2 Component Architecture

Figure 4.2 opens the platform boundary: the Next.js 16 web console, and at the centre a single Go control-plane (`:8080`) hosting the chi-routed API layer (authentication, RBAC, RLS binding, rate limiting), the always-on Sentinel detector (EWMA +3σ spike rule), and the durable Temporal RecoveryPipeline workflow [2] orchestrating the nine-agent loop.

![](../../assets/diagrams/v2-components.png){width=74%}

**Figure 4.2: Component Architecture.** ( the control-plane, sidecar services, and web console that make up the platform boundary ).

Around it sit four bearer-authenticated sidecars: the Docker-sandboxed Validator (`:8081`) [10], the GitOps PR service (`:8082`), the Python/FastAPI causal-inference ranker (`:8090` — an honest statistical proxy, not a fitted structural causal model [4]), and the deploy-engine (`:8091`), plus an Argo CD Sync/Rollback client [6]. The LLM spine pairs OpenAI (`gpt-4o` family) with Ollama [20] (`gpt-oss:20b`, `qwen2.5-coder:14b`) as a fully supported alternate, backed by pgvector retrieval [19] and per-agent JSON Schema validation with retry; the control-plane mediates every interaction.

## 4.3 Data and Storage Architecture

Figure 4.3 shows the four storage systems plus Temporal's workflow persistence layer.

![](../../assets/diagrams/v2-data-storage.png){width=74%}

**Figure 4.3: Data and Storage Architecture.** ( PostgreSQL, Neo4j, Redis, and MinIO, and what each one is responsible for ).

PostgreSQL 17 with pgvector is the system of record, multi-tenant via Row-Level Security [7] across 33 migrations; Neo4j [3] holds each repository's dependency graph for Pathfinder; Redis serves rate limiting and caching; MinIO is the encrypted patch store. The footer's isolation claim is the key point: only the control-plane holds store credentials — humans and agents never connect to a database directly.

## 4.4 Self-Healing Loop Pipeline

Figure 4.4 presents the central contribution as a single flow — the nine agents in order: Sentinel (detect), Pathfinder (localise via the Neo4j graph), Synthesiser (classify, select agents), Architect (plan and file-level contract), Backend (minimal patch), Quality Assurance (property-based tests), DevOps in parallel with Data Engineer (manifests and migrations), and Validator (sandboxed shadow-execution).

![](../../assets/diagrams/v2-selfheal-pipeline.png){width=74%}

**Figure 4.4: Self-Healing Loop — Agent Pipeline.** ( the nine-agent detect-to-deploy pipeline as a single flow ).

Only then does a person enter: the Approval Gate (approve, reject, or modify; auto-approved for low-risk changes) is the single human decision point. GitOps Deploy then opens a real pull request or redeploys a preview container, and the Rollback Probe rolls back via Argo CD on a fresh post-deploy fault, feeding the breach back into detection.

## 4.5 Deployment and Infrastructure

Figure 4.5 shows how the platform is built, delivered, and observed: GitHub Actions CI, Docker (Podman-compatible) as the universal runtime, Argo CD GitOps delivery, and Terraform declaring the AWS infrastructure as code.

![](../../assets/diagrams/v2-deployment-infra.png){width=74%}

**Figure 4.5: Deployment and Infrastructure Architecture.** ( containers, Infrastructure as Code, continuous delivery, and observability ).

The diagram is deliberately honest about status: the AWS ECS cluster behind an Application Load Balancer is the *intended* production target declared in Terraform — not yet deployed live — and the registered `nexis.sh` domain does not yet serve the platform. Observability follows OpenTelemetry [5] into Prometheus, Loki, and Tempo, visualised in Grafana; the evaluation environment ran the sandbox and deploy-engine on Podman [11], exercising its Docker-API compatibility in practice.

## 4.6 Use Case Diagrams

### 4.6.1 Use Case Diagram, Level 0

Figure 4.6 shows the coarsest functional view: the three human actors and the autonomous nine-agent fleet against a single system boundary, with the mediated external systems on the right.

![](../../assets/diagrams/v2-usecase-l0.png){width=74%}

**Figure 4.6: Use Case Diagram, Level 0.** ( the three human actors and the autonomous agent fleet against the system boundary ).

The essential property is mediation: every association terminates at the system boundary — no human ever calls GitHub, the LLM provider, or Argo CD directly; the agents act on the external systems on the humans' behalf, surfacing back to a person only at the approval gate.

### 4.6.2 Use Case Diagram, Level 1

Figure 4.7 expands the Level 0 bubble into concrete use cases grouped by minimum role, with generalisation arrows making role inheritance explicit.

![](../../assets/diagrams/v2-usecase-l1.png){width=74%}

**Figure 4.7: Use Case Diagram, Level 1.** ( expanded use cases per actor ).

The administrative tier holds fifteen use cases (membership, role recommendations, integrations, recovery policy and kill switch, billing, the RLHF/JSONL feedback export, audit, MFA); four approval-flow use cases are shared with the Engineer/Member, whose tier includes demo scenarios, agent runs, and the NASA-TLX survey [18]; the Viewer holds exactly three read-only use cases. The model is role-additive — each tier inherits every use case of the tier below — and the read-only Viewer experience is enforced by RBAC gating in the console even though the database role enumeration is `owner`/`admin`/`member`.

## 4.7 Data Flow Diagrams

### 4.7.1 Data Flow Diagram, Level 0 (Context)

Figure 4.8 is the context diagram: the entire platform as a single process 0 with three top-level stores — D1 the tenant database (PostgreSQL, under Row-Level Security), D2 the code graph (Neo4j), and D3 the encrypted patch store (MinIO).

![](../../assets/diagrams/v2-dfd-l0.png){width=74%}

**Figure 4.8: Data Flow Diagram, Level 0.** ( context-level data flow ).

Four entity flows cross the boundary — the user, GitHub, the alert sources (producers of incidents, not consumers), and the LLM provider — and every one is mediated by process 0: there is no direct path from a human to an integration.

### 4.7.2 Data Flow Diagram, Level 1 — Owner/Administrator

Figure 4.9 decomposes the administrative surface into ten numbered processes — spanning identity and access, platform administration, and incident and approval operations — over the tenant-scoped PostgreSQL stores; Table 4.1 summarises the most essential.

![](../../assets/diagrams/v2-dfd-l1-admin.png){width=74%}

**Figure 4.9: Data Flow Diagram, Level 1 — Owner / Administrator.** ( administrative processes and their data stores ).

**Table 4.1: Owner/Administrator Data Flow Diagram Level 1 processes and their primary data stores.**

| Process | Responsibility | Primary store written |
|---|---|---|
| 1.0 Authentication and RBAC | login, JWT session issue, role checks | `users`, `sessions` |
| 5.0 Project and Recovery Policy | recovery policy, SLO, kill switch | `projects` |
| 6.0 Approval Gate | approve / reject / modify decisions | `approval_decisions` |
| 8.0 Audit and Export | hash-chained audit writes, CSV export | `audit_log` |
| 10.0 Sentinel Manual Trigger | administrator-initiated detection run | `incidents_raw` |

The most important flow is 10.0: a manual detection run writes into the *same* `incidents_raw` table as the automatic detectors — no separate code path — so every autonomous-path guarantee (durability, validation, severity routing, auditability) holds for manual runs for free. 5.0 carries the kill switch that suspends autonomous recovery, and every state change lands in the hash-chained `audit_log`.

### 4.7.3 Data Flow Diagram, Level 1 — Self-Healing Loop

Figure 4.10 decomposes the loop into twelve numbered processes — detection and decision, the Layer-1 execution team, and the validation–approval–deployment–verification tail — over five stores (D1 incidents, D2 code graph, D3 patch store, D4 approvals and feedback, D5 deployments and lineage).

![](../../assets/diagrams/v2-dfd-l1-selfheal.png){width=74%}

**Figure 4.10: Data Flow Diagram, Level 1 — Self-Healing Loop.** ( the nine-agent fleet's processes and data stores ).

The closing annotation states the property that gives the project its title: the loop is closed. An SLO breach seen by the rollback probe (12.0) lands in `incidents_raw` as a fresh fatal and re-enters at Sentinel (1.0) — with an Argo CD rollback to the prior known-good revision when `RollbackOnSLOBreach` is set — so recovery, verification, and re-detection form one cycle over one shared data path.

## 4.8 Self-Healing Loop Sequence Diagram

Figure 4.11 traces one complete recovery run across all nine agent lifelines in eighteen numbered messages — each a named Temporal activity, durably recorded and streamed to the incident timeline over Server-Sent Events, the exact order the Chapter 5 screenshots show.

![](../../assets/diagrams/v2-sequence-selfheal.png){width=74%}

**Figure 4.11: Self-Healing Loop — Sequence Diagram.** ( one complete recovery run, message by message, across all nine agent lifelines ).

No unvalidated patch passes the sandboxed Validator (`--network=none`, `--read-only`, `--cap-drop=ALL`, pytest/Hypothesis suite [9]); severity routing then forces an explicit human decision for HIGH — schema drift, sensitive-path diffs (`*.sql`, `migrations/`, `auth/`, `security/`, `crypto/`), unknown scenarios, or a contract violation (a diff outside the Architect's `affected_files`) — while LOW auto-approves and MEDIUM auto-approves after 120 seconds; one of the two live runs in Chapter 5 genuinely timed out at this gate — the safety mechanism working as designed. Observed wall-clock time: approximately 35 seconds to 2 minutes on local Ollama models, corroborated by Chapter 5's fully successful 1 minute 40 second run.

Taken together, these views describe one coherent design: a mediated boundary, a control-plane as sole gateway to every component and store, a role-additive human surface beside an autonomous fleet, one shared data path for manual and automatic operation, and a recovery cycle that feeds its own failures back into itself. Chapter 5 presents the implementation; Chapter 6 the evidence that the loop closes in practice.
