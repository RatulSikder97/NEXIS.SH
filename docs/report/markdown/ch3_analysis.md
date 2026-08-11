# Chapter 3: System Analysis and Requirements

## 3.1 Requirement Analysis

Nexis is a role-based, multi-tenant platform. Every piece of data in the system belongs to exactly one organisation (tenant), and every request is evaluated against both the caller's organisation and the caller's role within it. The requirement analysis therefore begins by identifying the actors who interact with the system boundary.

Three *human* actor types were identified:

- **Owner/Administrator** — the organisation's administrator, responsible for membership, roles, billing, policy, integrations, and the final human decision on high-stakes automated repairs.
- **Engineer/Member** — the working engineer who investigates incidents, exercises the evaluation tooling, and interacts with the recovery pipeline day to day.
- **Viewer** — a read-only observer (for example a stakeholder or an auditor) who may inspect the system's state but may not change it.

The role model is *additive*: an Administrator holds every permission an Engineer holds, and an Engineer holds every permission a Viewer holds. Consequently, the functional requirements in Section 3.2 list only the permissions each role *adds* to the tier below it, without repetition. (Internally the database role enumeration is `owner`/`admin`/`member`; the console additionally presents the read-only Viewer experience through Role-Based Access Control (RBAC) gating on the frontend and Application Programming Interface (API).)

Unusually for a requirement analysis, a fourth actor category must be modelled that is not human at all: the **nine-agent self-healing fleet**. Most requirement-analysis chapters enumerate only human actors, because in a conventional system every state change ultimately traces back to a person clicking something. That assumption does not hold for Nexis. The agent fleet acts autonomously inside the system boundary: it detects faults without being asked, reads and writes the same PostgreSQL, Neo4j, and object-storage data stores the human-facing console uses, and triggers real external side effects — opening GitHub pull requests, applying rollbacks through Argo CD [6], and redeploying preview containers. An actor model that omitted the fleet would misrepresent the system: roughly half of all writes to the incident, patch, and audit tables originate from agents rather than people. Treating the fleet as a first-class actor also makes the safety analysis explicit — every autonomous capability listed in Section 3.2.4 is bounded by a corresponding control (sandboxing, severity classification, or the human approval gate) listed in Section 3.3.

The Level 0 and Level 1 use case diagrams for these four actors are presented together with the rest of the system's Unified Modeling Language (UML) design in Section 4.6 (Figures 4.6 and 4.7), alongside the data flow diagrams (Section 4.7) that show how the same actors move data through the platform.

## 3.2 Functional Requirements

The functional requirements are grouped by actor. Because the role model is additive, each human tier is listed as a delta over the tier below it.

### 3.2.1 Owner/Administrator can:

- Do everything an Engineer/Member and a Viewer can do (role additivity).
- Manage organisation membership: generate invite codes, send email invites, and remove members.
- Assign and change member roles, and act on the intelligent role-recommendation banner on the Members settings page — accepting a recommendation (which applies the role change under an optimistic-concurrency guard) or dismissing it (which starts a 30-day per-rule cool-down).
- Edit organisation settings (name, profile, and workspace-level configuration).
- Manage billing through Stripe [13]: view usage, manage payment methods, and review invoices produced by the usage-ticker and invoice-roller cron jobs; set the organisation's daily token budget on the cost tracker.
- View and export the append-only, hash-chained audit log (Comma-Separated Values (CSV) export) and invoke its cryptographic verification endpoint to prove the chain has not been tampered with.
- Decide pending approvals from the recovery pipeline: **Approve** a validated patch, **Reject** it, or **Modify** it — edit the unified diff in place before it ships, with the edited diff persisted as a Reinforcement Learning from Human Feedback (RLHF) feedback example.
- Configure each project's recovery policy, including whether the platform rolls back automatically on a Service-Level Objective (SLO) breach.
- Operate the Docker preview-deploy engine from a project's *Ops* tab: trigger Deploy and Stop, watch live status, open the returned preview Uniform Resource Locator (URL), inspect build and container logs, and review deployment history.
- Install and manage integrations: the GitHub App (installation, repository listing, webhook-driven incident creation on merged pull requests), Sentry [14], Datadog [16], and PagerDuty [15] webhook sources, Slack [17] (OAuth install plus interactive one-click approve/reject directly from a Slack message), and Argo CD [6].
- Export evaluation-harness results as CSV (an administrator-only action).
- Receive the daily administrator digest email summarising the last 24 hours of incidents, repairs, approvals, and Mean Time To Recovery (MTTR) for the organisation.
- Monitor platform operations pages: System Health (parallel dependency probes of PostgreSQL, Redis, Neo4j, MinIO, and Temporal), webhook activity, and integration connection health.

### 3.2.2 Engineer/Member can:

- Do everything a Viewer can do (role additivity).
- Sign up and sign in with email and password (a JSON Web Token (JWT) session in an HttpOnly cookie), sign in passwordlessly via magic link, or sign in through WorkOS Single Sign-On (SSO) [12]; recover access through the password-reset flow (hashed, time-limited reset tokens).
- Enrol Multi-Factor Authentication (MFA): Time-based One-Time Password (TOTP) with Quick Response (QR) code provisioning.
- Manage their own active sessions: list signed-in devices with Internet Protocol (IP) address, user agent, and last-seen time (the caller's own session carries a "This device" badge), and revoke any session remotely.
- Create, list, and revoke scoped API keys (`nx_live_...` bearer tokens) for programmatic access.
- Create projects through the new-project wizard and connect them to GitHub repositories.
- Browse the incidents list and open an incident's detail page, where the nine-agent pipeline timeline streams live over Server-Sent Events (SSE), including per-agent status, token counts, and cost.
- Inspect each agent's structured payloads (for example the Backend agent's raw generated diff) from the timeline.
- Trigger fault-injection runs from the Live-Demo console page, which presents 27 curated scenario cards drawn from a broader fixture-scenario taxonomy (one scenario is fully wired end-to-end today; the remainder are labelled "Coming soon").
- Run the evaluation harness: side-by-side OpenAI-versus-Ollama provider comparisons over fixture incidents, with persisted transcripts and cost figures.
- Query the knowledge base (pgvector-backed retrieval [19]) and view its retrieval status.
- Complete the post-incident NASA Task Load Index (NASA-TLX) workload survey [18], the platform's instrument for measuring operator cognitive load.
- Use the operational consoles: the agents fleet page and per-agent detail views, the workflows list, the activity stream, the validator sandbox page, and the recovery-pipeline view.

### 3.2.3 Viewer can:

- Sign in and view the dashboard: Key Performance Indicator (KPI) tiles (MTTR, recovery success rate, open incidents, mean tokens per run), the incidents-over-time chart, the system status panel, the projects health grid, and the recent activity feed.
- Read the incidents list and incident detail timelines, including the live SSE stream, without any decision or mutation controls.
- Read project pages (overview, activity, and status) and the performance page.
- View the agents, workflows, and activity-stream pages in read-only form.
- Take no mutating action of any kind: no approvals, no deployments, no settings changes, no key or session management beyond their own account.

### 3.2.4 The Nine-Agent Fleet autonomously:

- **Detects** faults (Sentinel): an always-on detector applies two rules — a per-row fatal-level trigger, and a statistical-process-control spike rule that maintains a per-organisation Exponentially Weighted Moving Average (EWMA) mean and variance baseline and fires on a breach of mean + 3σ, with a Poisson floor for low-count noise and an absolute-floor warm-up fallback.
- **Diagnoses** root causes (Pathfinder): walks the Neo4j code dependency graph [3] outward from the fault symptom and forwards candidate root-cause nodes, annotated with hop distance and degree, to the causal-inference sidecar, which ranks candidates by evidence-chain specificity, structural proximity, textual overlap, and a scenario prior, returning a per-component confidence breakdown [4].
- **Classifies and routes** (Synthesiser): assigns the incident a scenario — a regular-expression (regex) fast path with a Large Language Model (LLM) fallback — and computes an ordered `selected_agents` list; the workflow enforces this routing, skipping non-selected execution agents with a visible "skipped" timeline state.
- **Plans** the repair (Architect): decomposes the incident into `plan_steps`, `affected_files`, and a `risk_level`, and serves as a contract other agents may not silently violate — if the generated diff touches a file outside `affected_files`, the workflow flags a contract violation and forces HIGH severity so the patch can never auto-deploy unreviewed.
- **Codes** the fix (Backend): generates an LLM-guided minimal unified diff — the smallest valid change rather than a full-file rewrite.
- **Tests** the fix (QA, the Quality Assurance agent): auto-generates property-based test suites (pytest/Hypothesis [9]) from the incident and the diff; a background continuous-QA loop replays previously generated suites against the sandbox and raises a fresh incident on any pass-to-fail regression, closing a second, independent loop.
- **Packages** the deployment (DevOps): emits Continuous Integration / Continuous Deployment (CI/CD) and Argo CD manifests [6] and drives post-deployment health monitoring.
- **Handles data changes** (Data Engineer): proposes paired forward and reverse Structured Query Language (SQL) migrations, runs a proactive schema-drift detector that introspects `information_schema` against a captured baseline and raises a real incident on drift, and emits OpenLineage-compatible run events [8] for every migration proposal and application.
- **Validates** every candidate patch (Validator): executes it in a locked-down Docker sandbox [10] — no network access, a read-only filesystem, all capabilities dropped, a Process Identifier (PID) limit, and `no-new-privileges` — runs the QA-generated property tests, and reports pass/fail and coverage; no unvalidated patch ever reaches the approval gate.
- **Classifies severity** at the approval gate: HIGH for schema-drift scenarios, sensitive-path matches (SQL, migrations, authentication, security, and cryptography paths), unknown scenarios, or contract violations; MEDIUM otherwise, with a 120-second auto-approve countdown unless a human intervenes; LOW (tiny User Interface (UI)-only diffs) auto-approves immediately.
- **Deploys** approved patches (GitOps): opens a real GitHub pull request through a GitHub App installation (blob → tree → commit → ref → pull request); when the triggering incident originated from the preview-deploy engine, a successful merge additionally triggers an automatic redeploy.
- **Rolls back** on regression: a post-deploy probe watches for a fresh fatal incident on the same organisation and service within a bounded window and, when the project policy permits, invokes Argo CD's rollback client to restore the prior known-good revision.
- **Records** every step as a Temporal-durable activity event [2] and streams it live to the incident detail page over Server-Sent Events.
- **Feeds the deploy engine's failures back into itself**: when a preview deployment fails to build or pass its health check, the deploy engine files a real incident (`source: "deploy_engine"`) into the same Sentinel-to-pipeline path as every other incident source, making a broken build a self-healing target like any production fault.

## 3.3 Non-Functional Requirements

**Table 3.1: Summary of non-functional requirements and their realisation.**

| Requirement | Realisation in Nexis |
|---|---|
| Multi-tenancy | PostgreSQL Row-Level Security (RLS) on essentially every table [7] |
| Durability | Temporal-orchestrated workflows survive crashes and restarts [2] |
| Security | Sandboxed patch execution, encrypted patch storage, hashed tokens, hash-chained audit log |
| Scalability | Stateless Go (Golang) services, queue-decoupled workers, parallel dependency probes |
| Responsiveness | Server-Sent Events stream the live incident timeline |
| Usability | Role-scoped console, one-click approvals, plain-language rationales |

**Multi-tenancy and isolation.** Tenant isolation must not depend on application code remembering to add a `WHERE org_id = ...` clause. Nexis enforces isolation in the database itself using PostgreSQL Row-Level Security policies [7]: a request-scoped transaction binds `app.current_org_id` for every request, and the database refuses to return or mutate rows belonging to any other organisation, regardless of what the application layer asks for. This applies uniformly to human requests and to agent writes.

**Durability of long-running work.** A recovery run spans many minutes, crosses several services, waits on a human decision, and must survive process crashes and restarts without losing its place. All pipeline state therefore lives in Temporal [2], a durable workflow engine: every agent step is a journaled activity, and a resumed worker replays deterministically to exactly where it stopped. An in-memory orchestrator was rejected because a single crash mid-repair would strand an incident in an unknown state.

**Security.** Autonomously generated code is treated as untrusted by construction. Candidate patches execute only inside a Docker sandbox [10] with networking disabled, a read-only filesystem, all Linux capabilities dropped, a process-count limit, and `no-new-privileges`. Patch blobs are stored encrypted in object storage. Password-reset and API tokens are stored only as hashes (reset tokens are 32-byte random values hashed with Secure Hash Algorithm 256 (SHA-256), with a 30-minute time-to-live (TTL) and a non-leaky 202 response). The audit log is append-only and hash-chained, so any tampering with a past entry breaks the chain and is detectable through the verification endpoint.

**Scalability.** The control plane, validator, GitOps, and deploy-engine services are independent, stateless Go (Golang) modules that scale horizontally; Temporal decouples workflow load from any single process; Redis provides rate limiting and caching; and the System Health page's dependency probes fan out in parallel under a bounded timeout so a slow dependency cannot stall the page.

**Real-time responsiveness.** An engineer watching a live repair must see agent transitions as they happen, not on a polling delay. The incident detail page subscribes to a Server-Sent Events stream, and every Temporal-recorded activity event is pushed to the browser within moments of occurring — the live-demo runs in Chapter 6 were observed this way.

**Usability.** The console must make a genuinely complex system operable: role-scoped navigation shows each user only what they can act on, approvals are a one-click decision with the full diff in view, severity and countdown state are always visible, and role recommendations carry a plain-language rationale naming the concrete evidence behind them.

## 3.4 System Feasibility

### 3.4.1 Technical Feasibility

Nexis is deliberately polyglot, with each language chosen for the workload it carries rather than for uniformity. The four backend services (control-plane, validator, gitops, deploy-engine) are independent Go (Golang) modules: the language's static binaries, low-overhead concurrency, and first-class Docker API client suit long-lived services that juggle webhooks, container lifecycles, and parallel health probes. The console is Next.js/React with TypeScript, which is the practical choice for a large role-gated UI with live SSE streams, embedded Monaco diff editing, and chart-heavy dashboards. The causal-inference sidecar is Python/FastAPI because the scientific-Python ecosystem (and the Hypothesis property-testing stack [9] the QA agent depends on) lives there.

The data layer pairs PostgreSQL 17 with pgvector [19] — one engine provides transactional tenant data, Row-Level Security [7], and vector retrieval for the knowledge base, avoiding a separate vector database — while Neo4j [3] holds the code dependency graph, because root-cause diagnosis is a graph-traversal problem (hop distances, degrees, paths from symptom to cause) that a relational schema expresses poorly. Temporal [2] was proven in practice: both live recovery runs reported in Chapter 6 executed durably end-to-end. Every component is technology the team could run locally; the entire platform was built and verified on a single development machine, with Podman [11] standing in for Docker [10] as an API-compatible runtime where Docker itself was unavailable.

### 3.4.2 Economic Feasibility

The platform's dependency set is open source end to end — PostgreSQL, Neo4j Community, Redis, MinIO, Temporal, OpenTelemetry [5], and the Grafana observability stack — so no licence cost attaches to development or evaluation. The one economically significant input, LLM inference, is dual-path by design: production targets OpenAI's hosted models, while Ollama [20] serves fully supported local models (`gpt-oss:20b`, `qwen2.5-coder:14b`) as a zero-marginal-cost development and evaluation path. This is not a theoretical fallback: every live end-to-end run in this report was driven by the local Ollama provider, meaning the complete system — including its most expensive component — was exercised without incurring any per-token cost. The cost tracker, per-organisation token ledger, and daily budget controls exist precisely so that the paid path remains bounded and observable when it is used.

### 3.4.3 Operational Feasibility

Operationally, the system is designed to be run by the people it serves. The console presents role-based dashboards: an administrator lands on organisation-wide KPIs, approvals, and health; an engineer lands on incidents and the live pipeline; a viewer sees a read-only picture of the same truth. High-stakes autonomy is reduced to a one-click human decision (Approve/Reject/Modify), optionally taken directly from Slack [17] without opening the console at all. Severity classification means routine low-risk fixes demand no attention while sensitive changes always do. The NASA-TLX instrument [18] is built in so that the operator-workload claim can eventually be measured rather than asserted. Together these mean adopting Nexis does not require a dedicated operations team; the intended day-to-day operator is an ordinary engineer with a browser.
