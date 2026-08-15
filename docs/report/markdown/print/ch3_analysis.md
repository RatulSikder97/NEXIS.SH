# Chapter 3: System Analysis and Requirements

Requirements for Nexis were derived from its actors rather than from a feature wish-list, because the platform's defining property is *who* is allowed to act and *under what control*. This chapter therefore begins with the actors at the system boundary, three human roles and one autonomous nine-agent fleet (Section 3.1), states what each of them may do (Section 3.2), sets out the qualities the platform must hold while they do it (Section 3.3), and closes by testing the design for technical, economic, and operational feasibility against what was actually built (Section 3.4). Every capability granted in Section 3.2 is deliberately paired with a control in Section 3.3, so the requirements and the safety argument are read together rather than separately.

## 3.1 Requirement Analysis

Nexis was built as a role-based, multi-tenant platform in which every datum belongs to exactly one organisation, and every request is evaluated against the caller's organisation and role. Given this, the requirement analysis begins naturally with the actors sitting at the system boundary. Three human actor types were identified:

- **Owner/Administrator** — owns membership, roles, billing, policy, integrations, and the final human decision on high-stakes automated repairs.
- **Engineer/Member** — investigates incidents, exercises the evaluation tooling, works the recovery pipeline daily.
- **Viewer** — a read-only observer (stakeholder or auditor): inspects system state, never changes it.

The role model itself is *additive*: each tier holds every permission of the tier below it, so Section 3.2 lists only what each role *adds*. (Internally, this maps to `owner`/`admin`/`member`; Viewer is RBAC-gated on the frontend and API.)

A fourth actor here is not human at all: the **nine-agent self-healing fleet**. Most requirement analyses enumerate only human actors, since in a conventional system every state change traces back to a person clicking something; that assumption does not hold here. The fleet acts autonomously inside the system boundary, detecting faults without being prompted and writing directly to the console's own PostgreSQL, Neo4j, and object-storage stores, and its actions carry real external consequences: GitHub pull requests, Argo CD rollbacks [6], preview redeploys. Roughly half of all incident, patch, and audit-table writes are agent-authored, so leaving the fleet out would misrepresent the system; modelling it instead makes the safety analysis explicit, since every Section 3.2.4 capability is bounded by a Section 3.3 control — sandboxing, severity classification, or the human approval gate.

Level 0/1 use case and data flow diagrams for all four actors appear in Sections 4.6–4.7.

## 3.2 Functional Requirements

Functional requirements are stated per actor and, for the three human roles, *additively*: each subsection lists only what its actor gains over the tier beneath it, so an Owner/Administrator holds everything in Sections 3.2.1 through 3.2.3 and a Member holds everything in Sections 3.2.2 and 3.2.3. The fourth subsection is different in kind, since it describes an actor that acts without being asked; it is written as a list of autonomous capabilities, each bounded by one of the controls in Section 3.3.

### 3.2.1 Owner/Administrator can:

- Manage organisation membership: generate invite codes, send email invites, and remove members.
- Assign and change member roles, and act on the intelligent role-recommendation banner on the Members settings page: accept a recommendation (applying the role change under an optimistic-concurrency guard) or dismiss it (starting a 30-day per-rule cool-down).
- Edit organisation settings (name, profile, and workspace-level configuration).
- Manage billing through Stripe [13]: view usage, manage payment methods, and review invoices produced by the usage-ticker and invoice-roller cron jobs; set the organisation's daily token budget on the cost tracker.
- View and export the append-only, hash-chained audit log (CSV export) and invoke its cryptographic verification endpoint to prove the chain has not been tampered with.
- Decide pending approvals from the recovery pipeline: **Approve** a validated patch, **Reject** it, or **Modify** it by editing the unified diff in place before it ships, with the edited diff persisted as a Reinforcement Learning from Human Feedback (RLHF) feedback example.
- Configure each project's recovery policy, including whether the platform rolls back automatically on a Service-Level Objective (SLO) breach.
- Operate the Docker preview-deploy engine from a project's *Ops* tab: trigger Deploy and Stop, watch live status, open the returned preview Uniform Resource Locator (URL), inspect build and container logs, and review deployment history.
- Install and manage integrations: the GitHub App (installation, repository listing, webhook-driven incident creation on merged pull requests), Sentry [14], Datadog [16], and PagerDuty [15] webhook sources, Slack [17] (OAuth install plus interactive one-click approve/reject directly from a Slack message), and Argo CD [6].
- Export evaluation-harness results as CSV (an administrator-only action), and receive the daily administrator digest email summarising the last 24 hours of incidents, repairs, approvals, and Mean Time To Recovery (MTTR).
- Monitor platform operations pages: System Health (parallel dependency probes of PostgreSQL, Redis, Neo4j, MinIO, and Temporal), webhook activity, and integration connection health.

### 3.2.2 Engineer/Member can:

- Sign up and sign in with email and password (a JSON Web Token (JWT) session in an HttpOnly cookie), sign in passwordlessly via magic link, or sign in through WorkOS Single Sign-On (SSO) [12]; recover access through the password-reset flow (hashed, time-limited reset tokens).
- Enrol Multi-Factor Authentication (MFA): Time-based One-Time Password (TOTP) with Quick Response (QR) code provisioning.
- Manage their own active sessions: list signed-in devices with Internet Protocol (IP) address, user agent, and last-seen time (the caller's own session carries a "This device" badge), and revoke any session remotely.
- Create, list, and revoke scoped API keys (`nx_live_...` bearer tokens) for programmatic access.
- Create projects through the new-project wizard and connect them to GitHub repositories.
- Browse the incidents list and open an incident's detail page, where the nine-agent pipeline timeline streams live over Server-Sent Events (SSE), including per-agent status, token counts, cost, and structured payloads such as the Backend agent's raw generated diff.
- Trigger fault-injection runs from the Live-Demo console page, which presents 27 curated scenario cards drawn from a broader fixture-scenario taxonomy; only one scenario is fully wired end-to-end at present, and the remainder are labelled "Coming soon".
- Run the evaluation harness: side-by-side OpenAI-versus-Ollama provider comparisons over fixture incidents, with persisted transcripts and cost figures; query the pgvector-backed knowledge base [19] and view its retrieval status.
- Complete the post-incident NASA Task Load Index (NASA-TLX) workload survey [18], the platform's instrument for measuring operator cognitive load.
- Use the operational consoles: the agents fleet page and per-agent detail views, the workflows list, the activity stream, the validator sandbox page, and the recovery-pipeline view.

### 3.2.3 Viewer can:

- Sign in and view the dashboard: KPI tiles (MTTR, recovery success rate, open incidents, mean tokens per run), the incidents-over-time chart, the system status panel, the projects health grid, and the recent activity feed.
- Read the incidents list and incident detail timelines, including the live SSE stream, without any decision or mutation controls.
- Read project pages (overview, activity, and status), the performance page, and the agents, workflows, and activity-stream pages in read-only form.
- Take no mutating action of any kind: no approvals, no deployments, no settings changes, and no key or session management beyond their own account.

### 3.2.4 The Nine-Agent Fleet autonomously:

- **Detects** faults (Sentinel): an always-on detector applies two rules, a per-row fatal-level trigger and a statistical-process-control spike rule that maintains a per-organisation Exponentially Weighted Moving Average (EWMA) mean and variance baseline and fires on a breach of mean + 3σ, with a Poisson floor for low-count noise and an absolute-floor warm-up fallback.
- **Diagnoses** root causes (Pathfinder): walks the Neo4j code dependency graph [3] outward from the fault symptom and forwards candidate root-cause nodes, annotated with hop distance and degree, to the causal-inference sidecar, which ranks candidates by evidence-chain specificity, structural proximity, textual overlap, and a scenario prior, returning a per-component confidence breakdown [4].
- **Classifies and routes** (Synthesiser): assigns the incident a scenario, using a regular-expression fast path with an LLM fallback, and computes an ordered `selected_agents` list; the workflow enforces this routing, skipping non-selected execution agents with a visible "skipped" timeline state.
- **Plans** the repair (Architect): decomposes the incident into `plan_steps`, `affected_files`, and a `risk_level`, and serves as a contract other agents may not silently violate: if the generated diff touches a file outside `affected_files`, the workflow flags a contract violation and forces HIGH severity, so the patch can never auto-deploy unreviewed.
- **Codes** the fix (Backend): generates an LLM-guided minimal unified diff, the smallest valid change rather than a full-file rewrite.
- **Tests** the fix (QA): auto-generates property-based test suites (pytest/Hypothesis [9]) from the incident and the diff; a background continuous-QA loop replays previously generated suites against the sandbox and raises a fresh incident on any pass-to-fail regression, closing a second, independent loop.
- **Packages** the deployment (DevOps): emits CI/CD and Argo CD manifests [6] and drives post-deployment health monitoring.
- **Handles data changes** (Data Engineer): proposes paired forward and reverse SQL migrations, runs a proactive schema-drift detector that introspects `information_schema` against a captured baseline and raises a real incident on drift, and emits OpenLineage-compatible run events [8] for every migration proposal and application.
- **Validates** every candidate patch (Validator): executes it in a locked-down Docker sandbox [10] (no network access, a read-only filesystem, all capabilities dropped, a process limit, and `no-new-privileges`), runs the QA-generated property tests, and reports pass/fail and coverage; no unvalidated patch ever reaches the approval gate.
- **Classifies severity** at the approval gate: HIGH for schema-drift scenarios, sensitive-path matches (SQL, migrations, authentication, security, and cryptography paths), unknown scenarios, or contract violations; MEDIUM otherwise, with a 120-second auto-approve countdown unless a human intervenes; LOW (tiny UI-only diffs) auto-approves immediately.
- **Deploys** approved patches (GitOps): opens a real GitHub pull request through a GitHub App installation (blob → tree → commit → ref → pull request); when the triggering incident originated from the preview-deploy engine, a successful merge additionally triggers an automatic redeploy.
- **Rolls back** on regression: a post-deploy probe watches for a fresh fatal incident on the same organisation and service within a bounded window and, when project policy permits, invokes Argo CD's rollback client to restore the prior known-good revision.
- **Records** every step as a Temporal-durable activity event [2], streamed live to the incident detail page over Server-Sent Events, and **feeds the deploy engine's failures back into itself**: a preview deployment that fails to build or health-check files a real incident (`source: "deploy_engine"`) into the same Sentinel-to-pipeline path as every other source, making a broken build a self-healing target like any production fault.

## 3.3 Non-Functional Requirements

Functional capability is only half the specification; the properties below are the ones the platform must hold *while* exercising it, and each is realised by a concrete mechanism rather than by convention. Table 3.1 summarises them, and the paragraphs that follow give the reasoning and the rejected alternatives.

**Table 3.1: Non-functional requirements and realisation.**

| Requirement | Realisation in Nexis |
|---|---|
| Multi-tenancy | PostgreSQL Row-Level Security (RLS) on essentially every table [7] |
| Durability | Temporal-orchestrated workflows survive crashes and restarts [2] |
| Security | Sandboxed patch execution, encrypted patch storage, hashed tokens, hash-chained audit log |
| Scalability | Stateless Go services, queue-decoupled workers, parallel dependency probes |
| Responsiveness | Server-Sent Events stream the live incident timeline |
| Usability | Role-scoped console, one-click approvals, plain-language rationales |

**Multi-tenancy and isolation.** Tenant isolation cannot be left to application code remembering to add a `WHERE org_id = ...` clause every time, since that approach fails the moment one query is missed. Nexis enforces isolation inside the database itself, through PostgreSQL Row-Level Security policies [7]: a request-scoped transaction binds `app.current_org_id`, and the database refuses to return or mutate rows belonging to any other organisation, whatever the application layer asks for. This holds uniformly for human requests and for agent writes.

**Durability of long-running work.** A recovery run can span many minutes, crosses several services, and often waits on a human decision, so it must survive crashes and restarts without losing its place. All pipeline state therefore lives in Temporal [2], where every agent step is a journaled activity and a resumed worker replays deterministically to exactly where it stopped. An in-memory orchestrator was considered and rejected: a single crash mid-repair would strand an incident in an unknown state.

**Security.** Autonomously generated code is treated as untrusted by construction. Candidate patches execute only inside a Docker sandbox [10], with networking disabled, a read-only filesystem, all Linux capabilities dropped, a process-count limit, and `no-new-privileges`. Patch blobs are encrypted in object storage, and password-reset and API tokens are stored only as hashes (reset tokens are 32-byte random values hashed with SHA-256, carrying a 30-minute time-to-live and a non-leaky 202 response). The audit log is append-only and hash-chained, so tampering with a past entry breaks the chain and is detectable through the verification endpoint.

**Scalability and responsiveness.** The control plane, validator, GitOps, and deploy-engine services are independent, stateless Go modules that scale horizontally on their own; Temporal decouples workflow load from any single process, Redis handles rate limiting and caching, and the System Health page's dependency probes fan out in parallel under a bounded timeout, so one slow dependency cannot stall the page. An engineer watching a live repair, meanwhile, needs agent transitions as they happen rather than after a polling delay, so the incident detail page subscribes to a Server-Sent Events stream and each Temporal-recorded event reaches the browser within moments; the live runs in Chapter 6 were observed exactly this way.

**Usability.** The console has to make a genuinely complex system operable by an ordinary engineer with a browser. Role-scoped navigation shows each person only what they can act on, approvals reduce to a one-click decision with the full diff in view, severity and countdown state stay visible throughout, and role recommendations carry a plain-language rationale naming the concrete evidence behind them.

## 3.4 System Feasibility

Feasibility is argued here against the delivered system rather than against a proposal, since the platform described in Chapters 4 to 6 was built, run, and measured before this section was written. Three questions are answered in turn: whether the technology choices can carry the design (Section 3.4.1), whether the system can be built and operated at a defensible cost (Section 3.4.2), and whether the people who must live with it can actually do so (Section 3.4.3).

### 3.4.1 Technical Feasibility

Nexis was deliberately built as a polyglot system. The four independent backend services are written in Go, for static binaries, cheap concurrency, and a first-class Docker API client on which the webhook and container work depends. The console, however, is Next.js/React/TypeScript, chosen for its role-gated, SSE-streaming, Monaco-editing interface, while the causal sidecar runs on Python/FastAPI, on the scientific-Python/Hypothesis [9] stack. PostgreSQL 17 with pgvector [19] supplies transactions, RLS, and vector retrieval in one engine, whereas Neo4j [3] holds the dependency graph because diagnosis is graph traversal, which relational queries express poorly. Temporal [2] was proven by both live recovery runs in Chapter 6, and the whole system was built and verified on one development machine, with Podman [11] substituting for Docker [10].

### 3.4.2 Economic Feasibility

Every dependency Nexis relies on is open source, namely PostgreSQL, Neo4j Community, Redis, MinIO, Temporal, OpenTelemetry [5], and Grafana, so there is no licence cost. The one significant cost is LLM inference, handled as a dual path: OpenAI models for production, local Ollama [20] models (`gpt-oss:20b`, `qwen2.5-coder:14b`) for development. Every live run in this report in fact used the local provider, so the complete system was exercised at zero token cost; the cost tracker and daily budgets bound the paid path.

### 3.4.3 Operational Feasibility

On logging in, administrators see organisation-wide KPIs, approvals, and system health; engineers see incidents and the live pipeline; viewers see the same picture, read-only. High-stakes autonomy, in this regard, reduces to a single click, Approve, Reject, or Modify, optionally from Slack [17], while severity classification lets low-risk fixes pass unattended but never the sensitive ones. The built-in NASA-TLX instrument [18] lets the operator-workload claim eventually be measured rather than simply asserted; the intended operator, furthermore, is an ordinary engineer with a browser, not a dedicated operations team.
