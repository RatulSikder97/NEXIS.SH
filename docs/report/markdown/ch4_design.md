# Chapter 4: System Design and Architecture

This chapter presents the design of Nexis at complementary levels of abstraction. Sections 4.1 through 4.5 give the structural view across five diagrams: the system context, the component architecture, the data and storage architecture, the self-healing agent pipeline, and the deployment and infrastructure architecture. Section 4.6 gives the functional view through Level 0 and Level 1 use case diagrams, covering the three human roles and the nine autonomous agents. Section 4.7 gives the data view through a context-level Data Flow Diagram (DFD) and two Level 1 decompositions — one for the Owner/Administrator control surface and one for the self-healing loop itself. Section 4.8 closes with a Unified Modeling Language (UML) sequence diagram that traces a single complete recovery run, message by message, from fault detection to post-deploy verification. The self-healing loop is deliberately given the most space, because it is the central contribution of the project.

## 4.1 System Context

Figure 4.1 places Nexis — the multi-agent autonomous engineering platform — at the centre of its environment and shows every actor and external system it exchanges data with. On the human side stand the three platform roles (Owner/Administrator, Engineer/Member, and Viewer), joined by the nine-agent self-healing fleet, which the diagram draws as a fourth, autonomous actor operating inside the platform boundary on the humans' behalf.

![](../assets/diagrams/v2-context.png)

**Figure 4.1: System Context Diagram.** ( Nexis and the external actors and systems it exchanges data with ).

Every entry point into the platform converges on the same boundary: the web console (`apps/web`, Next.js 16), inbound GitHub webhooks, inbound alert webhooks from Sentry [14], Datadog [16], and PagerDuty [15], Slack interactivity callbacks [17], and plain Hypertext Transfer Protocol (HTTP) clients — Command-Line Interface (CLI) tools such as `curl` — that address the deploy-engine and the public Application Programming Interface (API) directly. All of these converge on the control-plane; no client talks to a data store or an external integration directly.

On the external side, the diagram lists every third-party system Nexis touches: GitHub (source repositories and pull requests), Slack (one-click approvals), the Large Language Model (LLM) provider (OpenAI or Ollama), Argo CD (deployment synchronisation and rollback), Stripe billing and invoicing [13], the incident sources (Sentry, Datadog, and PagerDuty, arriving as webhooks), the WorkOS identity provider for Single Sign-On (SSO) [12], and Amazon Simple Email Service (SES) or Simple Mail Transfer Protocol (SMTP) mail delivery. The property the diagram makes explicit in its footer is mediation: all external calls pass through the platform — no human, and no agent, talks to GitHub, the LLM provider, or Argo CD directly. The diagram also carries an honest status note: the `nexis.sh` domain is registered, but the platform is not yet publicly live (Section 4.5 returns to this point).

## 4.2 Component Architecture

Figure 4.2 opens the platform boundary and shows the components inside it: the web console, the control-plane at the centre, the sidecar services around it, and the LLM provider they draw on.

![](../assets/diagrams/v2-components.png)

**Figure 4.2: Component Architecture.** ( the control-plane, sidecar services, and web console that make up the platform boundary ).

**Web console.** The console (`apps/web`) is built with Next.js 16, React, TypeScript, and Tailwind CSS, and speaks to the control-plane exclusively through Application Programming Interface calls.

**Control-plane.** The heart of the system is a single Go (Golang) service (`:8080`) that hosts the HTTP API layer (the chi router, with authentication, Role-Based Access Control (RBAC), Row-Level Security (RLS) binding, and rate limiting), the always-on Sentinel detector goroutine, which applies an exponentially weighted moving average (EWMA) +3σ spike-detection rule, and the scheduled task runner (the daily digest, the Quality Assurance (QA) continuous loop, billing tickers, and schema-drift detection). Inside it, the Temporal RecoveryPipeline workflow [2] orchestrates the nine-agent self-healing loop as a durable workflow: detect → diagnose → plan → design → code → test → package/migrate → approve (severity-routed) → deploy → verify → rollback if needed.

**Large Language Model spine.** Alongside the workflow sits the model layer: OpenAI (the `gpt-4o` family) as the production default and Ollama [20] (local `gpt-oss:20b` and `qwen2.5-coder:14b`) as a fully supported alternate provider, a pgvector retrieval store [19] that supplies code-context passages to the Architect, Backend, and Quality Assurance agents, and a structured-output stage that validates every agent completion against a per-agent JavaScript Object Notation (JSON) Schema and retries on mismatch.

**Sidecar services.** Around the control-plane sit the independently deployable modules, each reached over HTTP with bearer-token authentication from the control-plane:

- **Validator** (`:8081`) — the Docker-sandboxed test runner [10] that shadow-executes every candidate patch under `--network=none`, `--read-only`, and `--cap-drop=ALL` and runs the Quality Assurance-generated Hypothesis property tests [9];
- **GitOps service** (`:8082`) — GitHub App pull request (PR) automation (blob → tree → commit → ref → PR), bearer-authenticated from the control-plane;
- **Causal-inference service** (`:8090`) — the Python/FastAPI graph-evidence ranker that scores Pathfinder's candidate root causes by evidence-chain specificity, structural proximity, and textual overlap (an honest statistical proxy rather than a fitted structural causal model [4]);
- **Deploy-engine** (`:8091`) — clone, stack detection, Dockerfile generation, build, and health-checked container run for preview deployments.

Alongside these four, an Argo CD client module provides the Sync and Rollback integration [6] used by the post-deploy Service-Level Objective (SLO) probe. The diagram's footer states the same mediation rule as the system context: the control-plane mediates every interaction — the web console, the sidecar services, and the model provider never communicate with one another directly.

The component architecture directly reflects the polyglot stack described in Chapter 3: Go (Golang) for every latency- and concurrency-sensitive service (the control-plane, Validator, GitOps service, and deploy-engine), Python for the statistics-oriented causal-inference sidecar, and TypeScript for the web console.

## 4.3 Data and Storage Architecture

Figure 4.3 shows the four storage systems plus the workflow persistence layer, and what each one is responsible for. The diagram also distinguishes the two classes of write initiator — human-initiated writes arriving through the web console and the Application Programming Interface, and agent-initiated writes from the autonomous fleet — and makes a strong isolation claim in its footer: the control-plane is the only component holding credentials to any store; humans and agents never connect to a database directly, and reads flow back over the same mediated paths.

![](../assets/diagrams/v2-data-storage.png)

**Figure 4.3: Data and Storage Architecture.** ( PostgreSQL, Neo4j, Redis, and MinIO, and what each one is responsible for ).

**PostgreSQL** 17 with the pgvector extension is the relational system of record, made multi-tenant through Row-Level Security policies [7] across 33 migrations. The key tenant-scoped tables — `users`, `org_members`, `projects`, `incidents_raw`, `workflow_runs`, `approval_decisions`, `deployments`, `lineage_events`, `audit_log`, `feedback_examples`, `role_recommendations`, and `digest_reports` — reappear as the data stores of the Data Flow Diagrams in Section 4.7. **Neo4j** [3] holds the code dependency graph of each connected repository, walked by the Pathfinder agent to trace a fault symptom back to candidate root-cause nodes. **Temporal** maintains its own persistence for workflow durability, so every step of a running or completed recovery pipeline survives a process restart. **Redis** serves request rate limiting and short-lived caching. **MinIO** is the encrypted patch store — Amazon Simple Storage Service (S3)-compatible object storage holding every unified-diff patch the Backend agent generates.

## 4.4 Self-Healing Loop Pipeline

Figure 4.4 presents the central contribution of the project as a single flow: the nine-agent detect-to-deploy pipeline, running as the durable Temporal RecoveryPipeline workflow introduced in Section 4.2. Reading the numbered stages in order: **Sentinel** detects the fault (a statistical spike or a fatal-level event); **Pathfinder** walks the Neo4j code graph to find candidate root causes; **Synthesiser** classifies the incident and selects which agents run; **Architect** writes the repair plan and the file-level contract; **Backend** generates the minimal code patch; **Quality Assurance** writes property-based tests for the patch; **DevOps** prepares the deployment manifests — the Continuous Integration / Continuous Deployment (CI/CD) and Argo CD manifests — in parallel with **Data Engineer**, which prepares any schema migration; and **Validator** runs the patch inside an isolated Docker sandbox together with the generated tests.

![](../assets/diagrams/v2-selfheal-pipeline.png)

**Figure 4.4: Self-Healing Loop — Agent Pipeline.** ( the nine-agent detect-to-deploy pipeline as a single flow ).

Only then does a person enter the flow: at the **Approval Gate**, a human approves, rejects, or modifies (accepts, declines, or edits and resubmits) the patch — or it is auto-approved for low-risk changes. On approval, **GitOps Deploy** opens a real GitHub pull request via a GitHub App installation, or, for a project bound to the Docker preview-deploy engine, redeploys the running preview container. Finally, the **Rollback Probe** watches for a fresh fault after deployment; if one appears, Argo CD rolls back to the prior known-good revision, and the breach re-enters the pipeline as a fresh incident. The nine agents act autonomously — the Approval Gate is the single human decision point in the loop, and the closing arc of the figure is the property that gives the project its title: detect → localise → classify → plan → patch → test → prepare → validate → approve → deploy → watch, with the watch stage feeding back into detection. Sections 4.7.3 and 4.8 decompose this flow into its data stores and its message-by-message timing.

## 4.5 Deployment and Infrastructure

Figure 4.5 shows how the platform is built, delivered, and observed. On the delivery side, GitHub Actions provides Continuous Integration, building and testing every commit and producing the container images; Docker (Podman-compatible runtime) is the runtime for every service and for the preview-deploy engine; and Argo CD provides GitOps-style continuous delivery, syncing desired state and rolling back on failure. Terraform declares the infrastructure as code, provisioning the production environment on Amazon Web Services (AWS).

![](../assets/diagrams/v2-deployment-infra.png)

**Figure 4.5: Deployment and Infrastructure Architecture.** ( containers, Infrastructure as Code, continuous delivery, and observability ).

The diagram is deliberately honest about deployment status: the AWS Elastic Container Service (ECS) cluster behind an Application Load Balancer (ALB) is the *intended* production target declared in the Terraform code — it is not yet deployed live, and the `nexis.sh` domain, while registered, does not yet serve the platform. Observability across all layers follows the OpenTelemetry pipeline [5]: traces, metrics, and logs are collected from every service and flow into Prometheus (metrics), Loki (logs), and Tempo (traces), visualised in Grafana as the operator-facing dashboards for the entire platform.

The container runtime boundary is the Docker Engine Application Programming Interface, which Podman [11] satisfies identically — a property that was exercised in practice, since the evaluation environment for this report ran the Validator sandbox and the deploy-engine on Podman.

## 4.6 Use Case Diagrams

### 4.6.1 Use Case Diagram, Level 0

Figure 4.6 shows the system at its coarsest functional granularity: a single system boundary, the three human actors and the autonomous nine-agent fleet on the left, and the external systems on the right.

![](../assets/diagrams/v2-usecase-l0.png)

**Figure 4.6: Use Case Diagram, Level 0.** ( the three human actors and the autonomous agent fleet against the system boundary ).

The three human actors correspond to the platform's role-based access model. The **Owner/Administrator** administers the organisation: membership, roles, integrations, recovery policy, billing, and — most importantly — the approval gate through which every autonomous repair must pass. The **Engineer/Member** performs day-to-day engineering work on the platform: running demo scenarios, browsing incidents, inspecting agent runs, and participating in the approval flow. The **Viewer** is a read-only experience surfaced through Role-Based Access Control gating in the console; it can observe dashboards, incidents, and system health but can change nothing. The fourth actor is the **nine-agent self-healing fleet** — an autonomous software actor to which the human roles delegate repair work, and which surfaces back to a person only at the approval gate.

On the right-hand side sit the external systems the platform mediates: GitHub (source and pull requests), Slack (one-click approvals), the Large Language Model provider (OpenAI or Ollama), Argo CD (deployment sync and rollback), and the platform's own stores, PostgreSQL and Neo4j. (The remaining integrations — the alert sources Sentry, Datadog, and PagerDuty, Stripe billing, and the MinIO patch store — appear in the system context of Section 4.1 and in the Data Flow Diagrams of Section 4.7.) The essential property the diagram expresses is mediation: every association from an actor terminates at the system boundary, and every line to an external system originates from it. No human ever calls GitHub, the Large Language Model provider, or Argo CD directly — the nine autonomous agents act on the external systems on the humans' behalf. Each association is elaborated into concrete use cases in the Level 1 diagram.

### 4.6.2 Use Case Diagram, Level 1

Figure 4.7 decomposes the single Level 0 bubble into the concrete use cases of each actor. The diagram groups use cases by the minimum role required — an administrative tier for the Owner/Administrator, a shared approval flow, an engineering tier for the Engineer/Member, a read-only tier for the Viewer, and the autonomous nine-agent fleet on the right — with hollow-triangle generalisation arrows making the role inheritance explicit.

![](../assets/diagrams/v2-usecase-l1.png)

**Figure 4.7: Use Case Diagram, Level 1.** ( expanded use cases per actor ).

**Owner/Administrator.** The administrative surface covers fifteen use cases, which the diagram condenses into its grouped administrative tier: managing members and roles; accepting or dismissing role recommendations produced by the heuristic recommender; connecting integrations; creating and configuring projects; setting the recovery policy and Service-Level Objective; toggling the kill switch; issuing invites and Application Programming Interface keys; managing billing and plan; exporting the Reinforcement Learning from Human Feedback (RLHF) training data (the JSON Lines (JSONL) feedback export); viewing the audit log; triggering a manual Sentinel run; exporting evaluation-harness and audit Comma-Separated Values (CSV) files; revoking sessions; resetting passwords; and enrolling or verifying Multi-Factor Authentication (MFA).

**Shared approval flow.** Four use cases are shared between Owner/Administrator and Engineer/Member, because both roles participate in reviewing autonomous work: approving, rejecting, or modifying a proposed patch; watching the live recovery timeline over Server-Sent Events; viewing the patch difference (diff) in the Monaco viewer; and deploying or stopping a preview container from the project's Operations (Ops) tab.

**Engineer/Member.** The engineering surface covers running a Live-Demo fault-injection scenario, browsing the incidents list, inspecting agent runs, submitting the NASA Task Load Index (NASA-TLX) workload survey [18], browsing evaluation harness runs, querying the knowledge base, viewing cost and performance pages, managing the member's own Application Programming Interface keys, updating profile and preferences, managing the member's own sessions and devices, and the account lifecycle itself (sign up, sign in, Multi-Factor Authentication verification).

**Viewer.** The Viewer holds exactly three use cases: read-only dashboard, read-only incident view, and read-only system health.

**The nine-agent fleet.** The fleet appears as a single autonomous actor whose grouped use cases summarise the recovery pipeline: detecting and diagnosing an incident (Sentinel and Pathfinder); planning and synthesising the repair (Synthesiser and Architect); generating code and tests (Backend and Quality Assurance) while packaging deployment and migrations (DevOps in parallel with Data Engineer); shadow-validating the patch (the Validator sandbox); and opening the pull request, redeploying, or rolling back on a Service-Level Objective breach (GitOps deploy and the rollback probe). Its only connection to a human use case is the approval gate — the fleet executes autonomously inside the boundary and surfaces to a person exactly once per run.

The permission model is *role-additive*: Owner/Administrator holds every Engineer and Viewer permission, and Engineer holds every Viewer permission. This is why the diagram draws the tiers as a generalisation chain, with each tier inheriting every use case of the tier below it — a higher role never loses a capability that a lower role has, it only gains administrative ones. The read-only Viewer experience is enforced by Role-Based Access Control gating in the console even though the underlying database role enumeration is `owner`/`admin`/`member`, a design decision discussed in Chapter 3.

## 4.7 Data Flow Diagrams

### 4.7.1 Data Flow Diagram, Level 0 (Context)

Figure 4.8 is the context diagram: the entire platform (the control-plane plus the nine-agent loop) as a single process 0, its external entities, and the three top-level data stores.

![](../assets/diagrams/v2-dfd-l0.png)

**Figure 4.8: Data Flow Diagram, Level 0.** ( context-level data flow ).

The data stores are labelled D1–D3. **D1, the tenant database (PostgreSQL)**, receives every organisational record — users, memberships, projects, incidents, workflow runs, decisions, and audit entries — under Row-Level Security. **D2, the code graph (Neo4j)**, holds the dependency graph of each connected repository and is read by Pathfinder during diagnosis. **D3, the patch store (MinIO)**, receives the encrypted diff blob produced by each recovery run.

Four external entity flows cross the boundary. The **user** (any of the three roles) sends login credentials, commands, and approval decisions inward and receives dashboards, incident timelines, and approval requests outward. **GitHub** receives patch branches and pull request creation requests (and repository content reads, including the Dockerfile reads performed for the deploy-engine) and returns pull request Uniform Resource Locators (URLs), commit identifiers, and merge status. The **alert sources** (Sentry, Datadog, and PagerDuty) send fault and alert webhooks inward — they are producers of incidents, not consumers of data. The **Large Language Model provider** (OpenAI or Ollama) receives structured completion requests and returns patch and plan JavaScript Object Notation. As at Level 0 of the use case model, every one of these flows is mediated by the platform process — the diagram's notation note records that every data flow crosses the system boundary through process 0, so there is no direct path from a human to an integration.

### 4.7.2 Data Flow Diagram, Level 1 — Owner/Administrator

Figure 4.9 decomposes the administrative side of the platform into ten numbered processes, grouped into three clusters — identity and access administration, platform administration, and incident and approval operations — together with the tenant-scoped PostgreSQL stores they touch: D1 (identity and access: `users`, `org_members`, `sessions`, `role_recommendations`), D2 (platform data: `projects`, `integrations`, billing, `audit_log`), and D3 (incident and approval data: `incidents_raw`, `approval_decisions`, `workflow_runs`).

![](../assets/diagrams/v2-dfd-l1-admin.png)

**Figure 4.9: Data Flow Diagram, Level 1 — Owner / Administrator.** ( administrative processes and their data stores ).

Table 4.1 summarises each process and the primary data store it writes to.

**Table 4.1: Owner/Administrator Data Flow Diagram Level 1 processes and their primary data stores.**

| Process | Responsibility | Primary store written |
|---|---|---|
| 1.0 Authentication and Role-Based Access Control | login, JSON Web Token (JWT) session issue, role checks | `users`, `sessions` |
| 2.0 Members and Roles | invite, promote, demote, remove | `org_members` |
| 3.0 Role Recommendation Review | heuristic promote/downgrade suggestions | `role_recommendations` |
| 4.0 Integrations | connect GitHub / Slack / Sentry / Argo CD | `integrations` |
| 5.0 Project and Recovery Policy | project configuration, recovery policy, Service-Level Objective, kill switch | `projects` |
| 6.0 Approval Gate | approve / reject / modify decisions | `approval_decisions` |
| 7.0 Billing | Stripe usage ticker, invoices, plan | billing tables (Stripe) |
| 8.0 Audit and Export | hash-chained audit writes, Comma-Separated Values export | `audit_log` |
| 9.0 System Health | parallel dependency probes (read-only) | — (no persistent write) |
| 10.0 Sentinel Manual Trigger | administrator-initiated detection run | `incidents_raw` |

Walking the diagram briefly: process 1.0 authenticates the administrator, binds the request-scoped Row-Level Security context, and owns the session surface — session listing and revocation and Multi-Factor Authentication enrolment via Time-based One-Time Password (TOTP) both operate on the `sessions` table in the identity store; 2.0 and 3.0 together form the membership surface, with the role recommender writing its suggestions to `role_recommendations` and the accept action flowing back through 2.0 into `org_members`; 4.0 and 5.0 configure what the autonomous loop is allowed to act on — 5.0 also carries the kill switch, the safety brake that suspends autonomous recovery for a project (halting `workflow_runs` and writing the action to `audit_log`); 6.0 records every terminal approval decision; 7.0 and 8.0 handle billing and the append-only, hash-chained audit trail; and 9.0 fans out health probes to PostgreSQL, Redis, Neo4j, MinIO, and Temporal in parallel and persists nothing. The diagram's footer records the enclosing guarantee: all ten administrative processes run inside the Nexis control plane, and every state change is appended to the `audit_log` table.

The most important flow in the diagram is process **10.0, the Sentinel Manual Trigger**. When an administrator manually triggers a detection run, the process writes directly into `incidents_raw` — the *same* table the self-healing loop's automatic detectors write to. There is no separate "manual" code path, no parallel queue, and no privileged shortcut: an administrator-triggered run is indistinguishable downstream from an automatic one, and flows through the identical Temporal pipeline, Validator sandbox, and approval gate. This design decision keeps the system's behaviour uniform and means every guarantee established for the autonomous path (durability, validation, severity routing, auditability) holds for manual runs for free.

### 4.7.3 Data Flow Diagram, Level 1 — Self-Healing Loop

Figure 4.10 decomposes the nine-agent loop itself into twelve numbered processes, arranged by layer: the detection and decision agents at the head of the pipeline, the Layer-1 execution team in the middle, and the validation, approval, deployment, and verification tail. Around them sit the fleet's five data stores — D1 (`incidents_raw`), D2 (the Neo4j code graph), D3 (the MinIO patch store), D4 (`approval_decisions` and `feedback_examples`), and D5 (`deployments` and `lineage_events`) — and its four external entities: the fault telemetry sources, the Large Language Model provider, the Owner/Administrator, and GitHub.

![](../assets/diagrams/v2-dfd-l1-selfheal.png)

**Figure 4.10: Data Flow Diagram, Level 1 — Self-Healing Loop.** ( the nine-agent fleet's processes and data stores ).

The pipeline reads end-to-end as follows:

1. **1.0 Sentinel** receives fault signals — webhook payloads from Sentry, Datadog, or PagerDuty, or its own exponentially weighted moving average +3σ spike rule over the incident stream — and writes the normalised incident into `incidents_raw` (D1).
2. **2.0 Pathfinder** walks the Neo4j code graph (D2) outward from the fault symptom and forwards candidate root-cause nodes, with hop distance and degree metadata, to the causal-inference sidecar for graph-evidence ranking.
3. **3.0 Synthesiser** classifies the incident into a scenario (a regular-expression fast path with a Large Language Model fallback) and computes the ordered `selected_agents` list that the workflow subsequently enforces.
4. **4.0 Architect** produces the structured plan — `plan_steps`, `affected_files`, `risk_level` — that acts as the contract every downstream agent is checked against.
5. **5.0 Backend** generates the minimal unified diff, guided by the Large Language Model provider (shown as a dashed structured-JSON completion flow serving the reasoning processes 2.0 through 8.0).
6. **6.0 Quality Assurance** generates property-based tests from the incident and the Backend diff.
7. **7.0 DevOps** emits the Continuous Integration / Continuous Deployment and Argo CD manifests for the change.
8. **8.0 Data Engineer** proposes forward and reverse migrations where schema changes are involved and emits OpenLineage-compatible run events [8] into `lineage_events` (D5).
9. **9.0 Validator** shadow-executes the patch in the Docker sandbox, runs the Quality Assurance-generated test suite, and stores the encrypted candidate diff in the MinIO patch store (D3).
10. **10.0 Approval Gate** routes the validated patch by severity to the Owner/Administrator external entity (the approve/reject/modify flow in the diagram) and records the terminal decision in `approval_decisions` and `feedback_examples` (D4).
11. **11.0 GitOps Deploy** opens the real GitHub pull request — writing to the `deployments` store (D5) and out to the GitHub external entity (pull requests, merges, and the GitOps state repository) — and redeploys through the deploy-engine when the incident's source was a preview deployment failure.
12. **12.0 Rollback Probe** watches `incidents_raw` during a bounded post-deploy window for a fresh fatal on the same organisation and service.

The diagram's closing annotation states the property that gives the project its title: **the loop is closed**. If process 12.0 observes a Service-Level Objective breach after a deployment, it does not raise an alert into a separate escalation channel — the breach lands in `incidents_raw` as a fresh fatal, exactly as any external fault would, and re-enters the pipeline at process 1.0 as a new Sentinel-detected incident (with an Argo CD rollback to the prior known-good revision when the project's `RollbackOnSLOBreach` policy is set). Recovery, verification, and re-detection form a single cycle over one shared data path.

## 4.8 Self-Healing Loop Sequence Diagram

Figure 4.11 is the Unified Modeling Language sequence diagram of one complete recovery run. All nine agents appear as lifelines, together with the fault source, the approval gate, the Owner/Administrator, and the GitOps and Deploy-Engine services. Eighteen numbered messages trace the run in temporal order; each corresponds to a named Temporal activity, and every one of them is recorded as a durable ActivityEvent and streamed to the incident timeline over Server-Sent Events — which is why the live screenshots in Chapter 5 show this exact message order as a per-agent timeline.

![](../assets/diagrams/v2-sequence-selfheal.png)

**Figure 4.11: Self-Healing Loop — Sequence Diagram.** ( one complete recovery run, message by message, across all nine agent lifelines ).

The run proceeds as follows:

1. **Message 1 — fault arrival.** A webhook or statistical spike (fanned in from any incident source) reaches Sentinel. The resulting `incidents_raw` row is what the RecoveryPipeline trigger polls, so workflow start is decoupled from signal arrival.
2. **Message 2 — `Sentinel.Detect`.** The first workflow activity re-records the detection inside the durable workflow. It is effectively a stub: the detection logic itself already ran in the always-on detector goroutine, and the activity exists so that the workflow timeline owns the provenance of the run.
3. **Message 3 — `Pathfinder.Diagnose`.** Pathfinder walks the Neo4j dependency graph from the symptom and submits the candidates to the causal-inference sidecar for ranking.
4. **Message 4 — `Synthesiser.Plan`.** The incident is classified into a scenario and the agent set for this run is selected; non-selected Layer-1 agents will later appear as explicitly "skipped" in the timeline rather than silently omitted.
5. **Message 5 — Large Language Model completion.** Drawn as a dashed asynchronous flow spanning the agent lifelines: every reasoning agent obtains its output as a structured JavaScript Object Notation completion that is validated against that agent's schema, with a retry on mismatch.
6. **Message 6 — `Architect.Solution`.** The structured plan with `plan_steps` and `affected_files` is produced; this becomes the contract for the rest of the run.
7. **Message 7 — `Backend.Codegen`.** The minimal unified diff is generated. If the diff touches any file outside the Architect's `affected_files`, the workflow flags a contract violation, which forces HIGH severity at the approval gate.
8. **Message 8 — `QA.TestGen`.** Property-based tests are generated from the incident and the diff.
9. **Messages 9a/9b — `DevOps.Pipeline` in parallel with `DataEngineer.Migrations`.** The only parallel pair in the pipeline, drawn inside the diagram's parallel (par) block: the Continuous Integration / Continuous Deployment and Argo CD manifests are prepared concurrently with the forward and reverse migration proposal, since neither depends on the other's output.
10. **Message 10 — hand-off to `Validator.Validate`.** The Backend result, together with the Quality Assurance suite, is passed to the Validator.
11. **Message 11 — sandboxed execution.** The Validator runs the patch in the locked-down container (`--network=none`, `--read-only`, `--cap-drop=ALL`, `--pids-limit=128`, `--security-opt=no-new-privileges`), executes the pytest/Hypothesis suite, and reports pass/fail and coverage. No unvalidated patch can proceed past this point.
12. **Message 12 — `ApprovalGate.Route`.** Severity is computed as a function of the scenario, sensitive-path matches, and contract violations: HIGH for the schema-drift scenario, any diff touching a sensitive path glob (`*.sql`, `migrations/`, `auth/`, `security/`, `crypto/`), an unknown scenario, or a contract violation; MEDIUM for everything else; LOW for small User Interface (UI)-only diffs.
13. **Message 13 — notification.** For HIGH and MEDIUM runs the gate notifies humans over Slack and email and waits.
14. **Message 14 — human decision.** The Owner/Administrator's approve, reject, or modify action arrives as a Temporal signal. The subsequent alternative (alt) block encodes the automatic paths: LOW severity auto-approves immediately, and MEDIUM auto-approves after a 120-second countdown unless a human overrides it first. HIGH — or any contract violation — always requires an explicit human decision; one of the two live runs evidenced in Chapter 5 genuinely timed out at this gate, which is the safety mechanism working as designed.
15. **Message 15 — `GitOpsDeploy`.** On approval, the GitOps service performs the blob → tree → commit → ref → pull request sequence against GitHub.
16. **Message 16 — conditional `DeployEngine.Redeploy`.** Executed only when the triggering incident's source was the deploy-engine: a merged fix to a broken preview build triggers an automatic rebuild and redeploy of the preview container.
17. **Message 17 — post-deploy health window.** The rollback probe watches `incidents_raw` for a fresh fatal on the same organisation and service within the bounded window.
18. **Message 18 — conditional `ArgoCD.Rollback`.** Inside the closing alternative (alt) block, a Service-Level Objective breach detected during the window triggers the Argo CD Rollback client to restore the prior known-good revision — and, as established in Section 4.7.3, the breach itself re-enters the pipeline as a new incident.

The footer of the diagram records the observed steady-state wall-clock time of a full run: approximately 35 seconds to 2 minutes on local Ollama models, depending on scenario complexity and Large Language Model latency. This range is corroborated by the real timings visible in the Chapter 5 screenshots, including the fully successful run that completed in 1 minute 40 seconds from detection to verified deployment.

Taken together, the views of this chapter describe one coherent design: a mediated platform boundary (the system context and use case Level 0), a control-plane that is the sole gateway to every component and store (the component and data architectures), a role-additive human surface beside an autonomous fleet (use case Level 1), a single shared data path for manual and automatic operation (Data Flow Diagram Level 1, Owner/Administrator), and a detection–repair–verification cycle that feeds its own failures back into itself (the agent pipeline, Data Flow Diagram Level 1 for the self-healing loop, and the sequence diagram). Chapter 5 presents the implementation of each element, and Chapter 6 the evidence that the loop closes in practice.
