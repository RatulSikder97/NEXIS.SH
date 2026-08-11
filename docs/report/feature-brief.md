# NEXIS — Comprehensive Feature Brief (source of truth for the report)

Project title (from the original FYP proposal, `fyp.pdf`): **"Nexis: A Multi-Agent
Autonomous Engineering Platform with Closed-Loop Fault Recovery"**
Student: Ratul Sikder, Roll 2506102, Institute of Information Technology (IIT),
University of Dhaka. Supervisor: left blank on the original proposal — leave the
signature line blank in the report too, do not invent a name.
Live domain: **nexis.sh** — registered, not yet deployed live (say so honestly).

Motivation (from fyp.pdf, use near-verbatim in Ch.1): engineering teams spend
40–60% of their time on maintenance/debugging/incident response rather than
building features. Existing AI coding tools (Copilot etc.) address isolated
tasks and never close the loop between detecting a failure and autonomously
repairing it. NEXIS models a software engineering team as nine role-specialised
agents across two layers and closes that loop end-to-end, with human approval
as a one-click gate rather than a bottleneck.

## 1. Tech stack (ground truth — do NOT use the MERN stack language from the
   EduRide sample; NEXIS is a different, polyglot stack)
- Monorepo: pnpm + Turborepo. `apps/web` (Next.js 16, React 19, TypeScript,
  Tailwind v4, Radix UI, Monaco editor, Recharts). `services/control-plane`,
  `services/validator`, `services/gitops`, `services/deploy-engine` are
  independent Go 1.25 modules. `services/causal-inference` is Python/FastAPI.
- Orchestration: Temporal (durable workflow engine) drives the 9-agent
  RecoveryPipeline.
- Data: PostgreSQL 17 + pgvector (multi-tenant via Postgres Row-Level Security,
  33 migrations), Neo4j (code dependency graph), Redis (rate limiting/cache),
  MinIO/S3 (encrypted patch blobs), Temporal's own persistence.
- LLM: OpenAI (gpt-4o family) in production; Ollama (local, gpt-oss:20b +
  qwen2.5-coder:14b) as a fully-supported alternate provider — the report's
  live-demo run actually used Ollama end-to-end.
- Container runtime for deploy-engine and the validator sandbox: Docker API
  (Podman-compatible).
- IaC: Terraform (AWS ECS + ALB). Observability: OpenTelemetry → Prometheus +
  Loki + Tempo → Grafana.
- Auth: local JWT-based sessions (HttpOnly cookie) + optional WorkOS SSO;
  MFA (TOTP); API keys; RBAC with three roles (owner, admin, member — the
  console additionally exposes a read-only "Viewer" experience via RBAC
  gating even though the DB role enum is owner/admin/member).

## 2. The 9-agent self-healing loop (THE central contribution — give it the
   most weight in every chapter; this is what "closed-loop fault recovery"
   in the project title means)

**Layer 1 — Execution Team** (do the engineering work):
- **Architect** — decomposes the incident into a structured plan (`plan_steps`,
  `affected_files`, `risk_level`), acts as the source of truth other agents
  must not silently violate. Contract-violation detection: if Backend's actual
  diff touches a file outside `affected_files`, the workflow flags a
  contract-violation and forces HIGH severity so it can never auto-deploy
  unreviewed.
- **Backend** — LLM-guided minimal-diff code generation (unified diff format,
  parsed by `patch_extract.go`), applies the "smallest valid change" principle
  rather than full-file rewrites.
- **QA** — auto-generates property-based tests (pytest/Hypothesis) from the
  incident + the Backend diff; a background QA continuous-loop cron replays
  previously generated suites against the sandbox and raises a fresh incident
  on a pass→fail regression (closing a second, independent loop).
- **DevOps** — emits CI/CD + ArgoCD manifests, drives deployment health
  monitoring; owns the ArgoCD Sync/Rollback client integration.
- **Data Engineer** — proposes forward+reverse SQL migrations; also runs a
  proactive schema-drift detector (introspects `information_schema`, diffs
  against a captured baseline, raises a real incident — not just a canned
  fixture label) and emits OpenLineage-spec-compatible RunEvents for every
  migration propose/apply.

**Layer 2 — Self-Healing Loop** (detect, diagnose, decide, verify):
- **Sentinel** — always-on detector goroutine; two rules: per-row fatal-level
  trigger, and a genuine statistical-process-control spike rule (EWMA mean +
  variance baseline per org, fires on a breach of mean + 3σ with a Poisson
  floor for low-count noise and a warm-up fallback to an absolute floor) —
  NOT a fixed magic-number threshold.
- **Pathfinder** — walks the real Neo4j code-dependency graph from the fault
  symptom, forwards candidate root-cause nodes (with graph position metadata:
  hop distance, degree) to the causal-inference sidecar.
- **causal-inference sidecar** (Python/FastAPI, :8090) — replaced a hardcoded
  scenario→answer lookup table with a real graph-evidence ranking algorithm:
  scores each candidate on evidence-chain specificity, graph-structural
  proximity to the symptom, textual overlap with incident text, and a small
  scenario-prior tiebreaker; returns a transparent per-component confidence
  breakdown. Explicitly documented as a defensible proxy methodology (no
  production interventional data exists to fit a full DoWhy causal model —
  the report should be honest about this rather than overclaiming).
- **Synthesiser** — classifies the incident into a scenario (regex fast-path +
  LLM fallback) and computes an ordered `selected_agents` list; the workflow
  now actually enforces this routing (skips non-selected L1 agents with a
  visible "skipped" timeline state) rather than just greying out UI.
- **Validator** — Docker-sandboxed shadow execution of every candidate patch:
  `--network=none --read-only --cap-drop=ALL --pids-limit=128
  --security-opt=no-new-privileges`, runs the QA-generated Hypothesis property
  tests, reports pass/fail + coverage back to the workflow. No unvalidated
  patch ever reaches the approval gate.
- **Approval Gate** — severity classifier (HIGH: schema_drift scenario,
  sensitive path glob match [*.sql, migrations/, auth/, security/, crypto/],
  unknown scenario, or a contract violation; MEDIUM: everything else with a
  120s auto-approve countdown unless overridden; LOW: tiny UI-only diffs
  auto-approve immediately). Human decision options: **Approve**, **Reject**,
  or **Modify** (RLHF — the engineer edits the diff before it ships; every
  terminal decision, including the edited diff, is persisted to
  `feedback_examples` for a JSONL export endpoint intended for future
  fine-tuning — triggering an actual paid fine-tune job is explicitly a human
  step, not automated).
- **GitOps deploy** — on approval, opens a real GitHub PR via a GitHub App
  installation (blob→tree→commit→ref→PR, `services/gitops`); on the specific
  case where the triggering incident's source was the deploy-engine (see §4),
  a successful merge triggers an automatic redeploy.
- **Rollback probe** — after a GitOps deploy, watches `incidents_raw` for a
  fresh fatal on the same org/service within a bounded post-deploy window;
  if `Policy.RollbackOnSLOBreach` is set, calls ArgoCD's real Rollback client
  to the prior-known-good revision.

Every one of the above steps is recorded as a Temporal-durable ActivityEvent
and streamed live to the incident detail page over Server-Sent Events — the
report's live-demo screenshots show this end-to-end, including a genuine
timeout-rejected run (the approval window elapsed) and a genuine fully
Approved→Succeeded run, both driven by a real local LLM (Ollama
qwen2.5-coder:14b / gpt-oss:20b), not a stub.

## 3. Auth, RBAC, and account management
Signup/login (JWT + HttpOnly session cookie), magic-link passwordless login,
MFA enrolment (TOTP, QR), API keys (create/list/revoke, scoped bearer
`nx_live_...`), invite codes + email invites, password reset (32-byte random
token, SHA-256 hashed at rest, 30-minute TTL, non-leaky 202 response
regardless of whether the email exists), session management (list active
sessions/devices with IP + user-agent + last-seen, revoke any session
remotely — "This device" badge for the caller's own session), WorkOS SSO as
an alternate provider. RLS enforces org-scoped tenant isolation on essentially
every table; a request-scoped Postgres transaction binds
`app.current_org_id` per request.

## 4. Docker preview-deploy engine (new capability — a full chapter's worth)
A dedicated Go microservice (`services/deploy-engine`, :8091) that, given a
connected GitHub project: clones the repo via a short-lived GitHub App
installation token; checks for an existing `Dockerfile` at the repo root (and
uses it as-is if present); if absent, detects the stack from marker files
(package.json→Node, requirements.txt/pyproject.toml→Python, go.mod→Go,
index.html→static) and generates a working Dockerfile; runs `docker build`;
picks a free host port and runs the container with resource limits
(`--memory=512m --cpus=1.0 --pids-limit=256 --security-opt=no-new-privileges`);
polls the container over HTTP until it answers; returns a live
`http://localhost:<port>` preview URL. **This was proven live in this session**:
a real public repo (`heroku/node-js-getting-started`, no Dockerfile in the
repo) was cloned, a Dockerfile was auto-generated, built, and run — curling
the returned URL served the real rendered HTML page of that app. On build or
health-check failure, control-plane inserts a real incident
(`source: "deploy_engine"`) into the *exact same* Sentinel→RecoveryPipeline
pipeline every other incident source uses — an unrecognised stack or a broken
build becomes a self-healing target like any production fault, and the
Backend agent's job — generating a fix — naturally extends to "write a working
Dockerfile." A frontend "Ops" tab on the project page exposes Deploy/Stop,
live status, the preview URL, build/container logs, and deployment history.

## 5. Intelligent role recommendation (previously 0% implemented — now real)
A heuristic engine (not an LLM call) over real signal already captured in the
schema: (1) a member with ≥5 owner/admin-gated audit-log actions in 30 days
who isn't admin → recommend admin; (2) an admin with no login in 90+ days →
recommend downgrade; (3) an active admin (≥10 actions/60 days) with zero
admin-gated actions → recommend downgrade (over-privileged). Each
recommendation carries a plain-language rationale naming the concrete
evidence numbers. Admins accept (applies the role change with an optimistic-
concurrency guard) or dismiss (30-day per-rule cool-down) from the Members
settings page. A location/device-anomaly rule was explicitly considered and
dropped rather than faked, because no login IP/geo capture exists yet — this
is a documented, honest limitation, not an oversight.

## 6. Observability, billing, integrations (already-existing platform breadth
   — cover in the report but with less depth than the self-healing loop)
- Dashboard: KPI tiles (MTTR, recovery success rate, open incidents, mean
  tokens/run), incidents-over-time chart, system status panel, projects
  health grid, recent activity feed.
- Cost tracker (per-org token-ledger rollup, daily budget), Performance page,
  System Health (fan-out dependency probes: Postgres, Redis, Neo4j, MinIO,
  Temporal — all probed in parallel with a bounded timeout).
- Integrations: GitHub App (install, repo listing, webhook-driven incident
  creation on `pull_request.closed && merged`), Sentry, Datadog, PagerDuty
  (all webhook-driven incident sources), Slack (OAuth install + interactive
  one-click approve/reject from a Slack message), ArgoCD, Stripe billing
  (usage ticker + invoice roller crons, payment method management).
- Audit log: append-only, hash-chained for tamper-evidence, CSV export,
  cryptographic verify endpoint.
- Eval harness: side-by-side OpenAI-vs-Ollama provider comparison across
  fixture incidents, persisted transcripts + cost, admin-only CSV export.
- Fault-injection fixture library: 11 realistic scenarios (expanded from an
  original 2) spanning null-pointer, zero-division, schema drift, API
  contract violation, dependency breakage, connection-pool exhaustion,
  deadlock, slow memory leak, rate-limit cascade, disk exhaustion — each with
  internally consistent stacktrace/logs/metadata. The Live-Demo console page
  additionally lists 27 curated scenario cards (sourced from a broader
  fixture-scenario taxonomy spanning application/database/deploy/
  observability/security categories) with 1 fully wired end-to-end today and
  the rest labelled "Coming soon" as the fixture library grows — be honest
  about this ratio, don't claim all 27 run today.
- NASA-TLX workload survey (post-incident cognitive-load instrument, a real
  research evaluation tool, not a placeholder), MTTR computed and charted in
  multiple dashboard widgets.
- Daily admin digest (cron, aggregates 24h of incidents/repairs/approvals/
  MTTR per org, emails owners/admins), knowledge base (pgvector retrieval
  status), workflows list, validator sandbox console page.

## 7. What is explicitly NOT claimed (be honest in Limitations, this matters
   for academic integrity — do not let any agent overclaim)
- No real GitHub App / OpenAI production credentials exist in the evaluation
  environment used to write this report — the live-demo evidence uses local
  Ollama models and a public unauthenticated repo clone; a fully credentialed
  production deployment was not exercised end-to-end with real GitHub PR
  merges.
- nexis.sh is registered but the platform is not yet deployed to it — all
  evidence in this report is from local/native execution (Docker was
  unavailable in the build environment; Podman was substituted as a
  Docker-API-compatible runtime and proven to work identically).
- The RLHF loop's data-export pipeline is real and tested; actually
  submitting a fine-tuning job against the exported data is a deliberate
  manual step, not automated (real money, real external API call).
- Only 1 of 27 Live-Demo scenario cards is fully wired to a real fixture +
  classification path today; the rest are UI-complete but backend-pending.
- The causal-inference ranking algorithm is an honest statistical/graph
  proxy, not a fitted DoWhy structural causal model (no interventional
  production data exists to fit one against).
- A pre-existing, unrelated bug was found (not fixed, out of scope) in the
  workspace-onboarding progress poller (`/v1/workspaces/{id}/events` 404) —
  the workspace itself provisions successfully server-side regardless.

## 8. Screenshot inventory (docs/report/assets/screenshots/), pick the best
   subset — every filename below exists and is ready to embed
01-signup, 02-signin, 03-forgot-password — auth pages.
04-dashboard — admin console home (KPIs, charts).
05-agents — 9-agent fleet list. 06-workflows. 07-activity-stream.
08-validator-sandbox. 09-incidents — incidents list.
10-approvals — REAL pending approval row (Reject/Modify/Approve buttons).
11-audit-log. 12-projects — project cards.
13-settings-members — members list + role-recommendation banner.
14-settings-sessions — real session list with revoke.
15..17 — API keys, billing, organization settings.
18-performance, 19-cost-tracker, 20-system-health.
21-integrations, 22-webhook-activity, 23-connection-health.
24-eval-harness, 25-knowledge-base, 26-live-demo — 27-scenario picker.
27-recovery-pipeline.
28..33 — project detail tabs (Overview/Integrations/Recovery
Policy/Activity/Ops-empty/Ops-deploy-attempt).
34-incident-live-early / 35-incident-live-progress — a REAL running
9-agent pipeline mid-flight with live token counts.
36-incident-live-final — same run, all L1/L2 steps green, approval
"Pending" (this run then timed out — genuinely, the 120s window elapsed;
39-incident-approved-final shows that honest timeout-rejected outcome).
37-approval-modify-dialog — the RLHF modify/approve dialog.
38-approval-decision-dialog — the plain approve confirm dialog.
40-incident-full-success — **THE centerpiece**: a second real run,
Medium severity, Approved, Succeeded, full 1m40s timeline with real
token/cost figures per agent.
41-backend-payload-expanded — raw JSON evidence of the actual
qwen2.5-coder:14b-generated unified diff (provider="ollama").
42-new-project-wizard. 43-agent-detail-backend.

## 9. Live deploy-engine proof (paste as a code listing / terminal
   transcript in the report, this is strong, defensible evidence)
```
POST /v1/deploy  (repo: heroku/node-js-getting-started, no Dockerfile present)
→ 200 OK
  "status": "running", "dockerfile_source": "generated", "detected_stack": "node",
  "url": "http://localhost:54671", "image_tag": "nexis-preview-test-project:22d06076c357"
  build_log: "...npm ci... EXPOSE 3000... CMD [\"npm\",\"start\"]..."
  container_log: "Listening on 3000\nRendering 'pages/index' for route '/'"

GET http://localhost:54671/  → real rendered HTML of the Node app (verified)

POST /v1/deploy/test-e2e-001/stop → {"status":"stopped"}; container removed.
```

## 10. Testing evidence to report honestly (do not fabricate a formal test
    matrix like EduRide's — instead report what genuinely happened this
    session)
- `go build ./... && go vet ./... && go test ./...` clean across
  control-plane, validator, gitops, deploy-engine (all Go modules).
- `pytest` clean for causal-inference (38 tests) and the validator's
  Hypothesis sidecar.
- `pnpm typecheck && pnpm lint && pnpm build` clean for apps/web.
- Two live end-to-end RecoveryPipeline runs against a real local LLM: one
  timed out at the approval gate (safety mechanism working as designed),
  one fully succeeded (Approved → Succeeded, 1m40s).
- One live end-to-end deploy-engine run: real clone → generated Dockerfile →
  build → run → HTTP-verified → stop.
- A genuine pre-existing bug was found and root-caused (JWT `exp` validated
  against wall-clock instead of an injected test clock, silently failing 13
  tests) and fixed with a one-line change (`jwt.WithTimeFunc`), verified
  safe because production's default clock already equals the JWT library's
  own default.
- A genuine SQL bug was found and fixed: a migration used `current_role`,
  a reserved PostgreSQL keyword, as an unquoted column name.
Report these AS the evidence base — it is more credible than a fabricated
formal unit/integration/performance test matrix, and matches an honest
"what actually happened" section 6 (Testing and Results).
