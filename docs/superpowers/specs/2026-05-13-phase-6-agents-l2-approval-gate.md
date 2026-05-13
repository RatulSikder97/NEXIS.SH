# Phase 6 — Agents L2 + Approval Gate + GitOps — Design

**Date:** 2026-05-13
**Phase:** 6 (Weeks 16–18 per `docs/PROJECT_PLAN.md`). The **MVP cut line** — at end of Phase 6 we have an end-to-end demoable recovery loop: inject fault → detect → diagnose → synthesise → patch → tests → approve → PR.
**Dependencies:** Phase 4 (Pipeline Substrate, in flight under Pattern A) + Phase 5 (L1 Agents + LLM Spine, in flight under Pattern A). Phase 6 plugs into Phase 5's `Agent` port + `Registry` and Phase 4's `RecoveryPipeline` DAG. No rewrite of either.
**External services:** all local for Phase 6 — the GitHub App is already provisioned by Phase 3 (`installation_id` + webhook secret live in the existing `integrations` row); the demo uses a **synthetic fixture repo we control**. Slack incoming-webhook URL is per-org and stored encrypted via the existing `domain.KeyVault`. Email goes through MailHog reusing Phase 2's SMTP plumbing. Neo4j is already running in compose since Phase 1 — Phase 6 wires its consumer for the first time. Python DoWhy sidecar is a new local container (`services/causal-inference/`). **ArgoCD + Argo Rollouts are docs-only in Phase 6**; Phase 7 wires them at runtime.

---

## 1. Goals

1. **Sentinel detector goroutine** — an always-on goroutine inside the control-plane (`internal/sentinel/`) polls `incidents_raw` every 10 s (LISTEN/NOTIFY in Phase 7) and triggers a `RecoveryPipeline` workflow when a `level='fatal'` row arrives for any org with a connected Sentry integration. The detector talks to the existing `WorkflowService.Start` adapter — no new HTTP surface. The Temporal activity `Sentinel.Detect` becomes a thin **acknowledgement** step (records `{incident_id, detected_at, detector_version}` into the activity payload) — the heavy lifting moved into the always-on detector.
2. **Pathfinder L2 agent** — replaces the Phase 4 stub of `Pathfinder.Diagnose`. The activity reads the incident, queries a Neo4j codegraph (seeded synthetic edges over the fixture repo), then calls the new Python DoWhy sidecar (`services/causal-inference/`) over gRPC for a canned causal estimand. Returns `{root_cause_node, hypothesis, confidence, evidence_chain}` for the workflow to pass into Synthesiser.
3. **Synthesiser L2 agent** — replaces the Phase 4 stub of `Synthesiser.Plan`. Given a root cause, picks which L1 agents to invoke and in what order. Phase 6 ships three canned routes — `schema_drift` → DataEngineer + Backend; `null_deref` → Backend + QA; `oom` → DevOps + Backend. Emits a `synthesiser_plan` field on `PipelineOutput` that the workflow consumes to refactor away Phase 4's hard-coded order.
4. **Validator L2 agent (extended)** — replaces the Phase 4 stub `services/validator/` body with an additional **property-based test** runner (Hypothesis). Phase 6 adds a Python sidecar inside the existing validator container that runs `hypothesis` on the synthesised patch — invoked over an internal Unix socket so the Go HTTP wire stays the same. The validator response gains `hypothesis_failures: []` alongside the existing `tests_passed`/`fail_count`/`coverage` fields.
5. **Approval Gate** — `ApprovalGate.Route` becomes a real Temporal workflow signal pattern. Severity (`low` | `medium` | `high`) is computed from the synthesised plan + patch fingerprint; the workflow blocks on a signal channel with a 2-minute sleep race for `medium`. `low` auto-approves immediately, `high` always requires a human. Approvals are notified via Slack + email + console; the decision row lands in a new `approval_decisions` table (RLS-protected). New routes: `POST /v1/workspaces/{ws}/pipelines/{run}/approve` + `GET /v1/workspaces/{ws}/pipelines/{run}/decision`.
6. **GitOps service** — `services/gitops/` graduates from a `/healthz` skeleton to a full PR opener. Reads the per-org GitHub App `installation_id` from `integrations`, mints an installation access token via `bradleyfalzon/ghinstallation/v2`, creates a branch `nexis/fix-<run_id_short>`, commits the patch, opens a PR using `google/go-github/v60` with a body that includes the agent transcript table, risk score, and (placeholder) ArgoCD app link. The PR-open event audits to `gitops.pr_opened`.
7. **Slack integration** — adds `slack` as the 4th integration alongside `github`/`sentry`/`argocd`. Stores the incoming-webhook URL encrypted at rest via `domain.KeyVault` in the existing `integrations` table (no schema change — the `provider` CHECK constraint widens to include `'slack'`). The approval-gate notifier posts a Block Kit message with approve/reject buttons.
8. **Live Pipeline Demo (real)** — replaces the Phase 4 Live Demo placeholder. Adds an **"Inject fault" button** that triggers a `RecoveryPipeline` run on the synthetic fixture repo using a real incident payload from `services/validator/fixtures/incidents/`, watches the full timeline + approval gate + GitOps PR creation, and links out to the resulting PR URL.
9. **Approvals surface (real)** — replaces the Phase 3 placeholder under `/console/approvals`. Lists pending decisions across all of the user's workspaces, links to the per-run timeline with inline approve/reject buttons; renders the severity badge + the synthesised plan summary.
10. **ArgoCD design docs** — `infra/argocd/{app-of-apps.yaml, rollouts.yaml, README.md}` documents the runtime plan (Phase 7) for ArgoCD app-of-apps + Argo Rollouts CR with SLO-driven analysis step. Phase 6 ships docs; demo pretends ArgoCD takes the PR from here. **Documented deferral.**
11. **Audit + observability parity.** Every new event audits via the existing HMAC-chained `audit_log` writer: `incident.sentinel_triggered`, `pathfinder.diagnosis`, `synthesiser.plan`, `approval.requested`, `approval.decided`, `gitops.pr_opened`, `slack.notification_sent`. Slog records carry `agent_l2=true` for Grafana grouping.

## 2. Non-goals

* **Real OTel-metric anomaly detection.** Phase 6's Sentinel reads `incidents_raw` only. Streaming metric anomaly detectors over OTel histograms land in Phase 7. Documented in the Sentinel design as a deferred trigger source.
* **Real DoWhy training / causal-graph learning.** The Python sidecar returns canned causal estimands keyed off the incident's stacktrace signature. The wire shape is the real one — Phase 7 swaps the impl behind it.
* **Real Hypothesis fuzz budgets.** Phase 6 ships Hypothesis with `max_examples=20` and a 10-second per-test deadline. Production-grade fuzzing (CI minutes, shrink to minimal counter-example, persisted DB of failures) is Phase 7.
* **ArgoCD runtime + Argo Rollouts runtime.** Compose stays as-is; no ArgoCD container starts in Phase 6. The PR opens, the demo says "imagine ArgoCD takes it from here", a Phase 7 task wires it for real.
* **Auto-merging PRs.** Phase 6 only opens the PR. A human merges in GitHub; ArgoCD (Phase 7) detects the merge and deploys.
* **GitHub App write to real tenant repos.** The Phase 6 demo uses a **synthetic fixture repo we control** (we own the GitHub App installation_id). Production tenants connect their own installation via Phase 3's existing flow — works end-to-end but is not part of the acceptance criteria.
* **Slack OAuth app.** Phase 6 uses Slack incoming webhooks (per-channel URL). Phase 7 swaps to a Slack app with bot tokens for richer message updates.
* **Approval Gate role-based routing (e.g. "this severity requires lead-eng only").** Phase 6's role is binary: workspace owner|admin can decide; member cannot. Severity-to-role mapping (e.g. `high` requires `owner`) lands in Phase 7 once we have RBAC scopes.
* **Cross-workflow approval batches.** One pipeline run → one decision. Bulk approve of multiple runs lands in Phase 7.
* **Real metric SLO calculation.** Severity rules use a static lookup table (changes to migrations or security files → `high`, trivial frontend-only → `low`, everything else → `medium`). Risk scoring from real SLO breach probability is Phase 7.
* **A second GUC for workspace-scoped RLS.** `approval_decisions` is org-scoped (workspace_id is a column for join-back-to-runs, not an RLS dimension). Matches the Phase 4 pipeline RLS pattern.
* **Sentinel-the-agent as a separate provider in the L2 agent registry.** The detector goroutine triggers the workflow directly; the activity `Sentinel.Detect` is a thin acknowledgement step (no LLM call) implemented inline in `activities.go`. There is no `internal/adapter/agents/sentinel/` package — see Risk 13.1.

## 3. Architecture

Same port/adapter pattern as Phases 1–5. Phase 6 introduces:

* Two new components — `sentinel` (always-on detector, peer of `usecase`) and `graphstore` (Neo4j adapter, peer of `repo`).
* One new external service container — `causal-inference` (Python sidecar).
* One refactor — `services/gitops/` from skeleton to full service.
* Three new L2 agents in `internal/adapter/agents/{pathfinder,synthesiser,validator}/` matching the Phase 5 layout (one Provider per agent + a Registry entry).
* One new HTTP route group — approvals.

The Phase 5 `Agent` port + `Registry` are unchanged. Phase 6 L2 agents implement `domain.Agent` exactly the way Phase 5 L1 agents do — same `AgentInput/AgentOutput`, same registry, same prompt-caching + token-ledger surface (Pathfinder + Synthesiser are LLM-ish; the Validator L2 is the Hypothesis runner and is **not** LLM-backed — it implements `Agent` with `Provider="hypothesis"` and `Model="hypothesis-0.x"` so the ledger keeps a uniform shape).

### 3.1 control-plane Go layout

```
internal/
  domain/
    sentinel.go              # NEW — IncidentTrigger + DetectorConfig + the SentinelDetector port
    graph.go                 # NEW — Graph port + Node + Edge + Query types
    causal.go                # NEW — CausalEngine port + CausalQuery + CausalResult
    approval.go              # NEW — Severity + ApprovalDecision + ApprovalGate port
    notifier.go              # NEW — Notifier port + NotificationKind + ChannelTarget
    agent.go                 # MODIFY — add AgentNamePathfinder, AgentNameSynthesiser, AgentNameValidatorL2 constants
    workflow.go              # MODIFY — PipelineInput gains SynthesiserPlan; PipelineOutput gains ApprovalDecisionID
    errors.go                # MODIFY — ErrApprovalRejected, ErrApprovalTimeout, ErrPathfinderUnavailable

  adapter/
    graphstore/
      neo4j/
        store.go             # NEW — Graph impl on top of neo4j-go-driver
        seed.go              # NEW — synthetic fixture graph builder for cmd/seed-neo4j
        store_test.go
    causal/
      grpc.go                # NEW — CausalEngine client over gRPC to services/causal-inference
      grpc_test.go
    agents/
      pathfinder/
        provider.go          # NEW — implements domain.Agent; runs graph + causal + (optional) LLM hypothesis refinement
        prompts.go
        schema.go
        provider_test.go
      synthesiser/
        provider.go          # NEW — implements domain.Agent; canned routing + LLM scenario classification (optional)
        prompts.go
        schema.go
        routes.go            # NEW — canned routing table: scenario → []AgentName
        provider_test.go
      validator_l2/
        provider.go          # NEW — implements domain.Agent; calls services/validator with hypothesis=true
        provider_test.go
    integration/
      slack/
        provider.go          # NEW — Slack incoming-webhook adapter; Connect/Disconnect/Status + Notify
        provider_test.go
      github/
        installation.go      # NEW — ghinstallation-based authenticated client builder
        installation_test.go
    approval/
      service.go             # NEW — ApprovalGate orchestrator: severity → route → notify → wait/signal/auto
      severity.go            # NEW — pure-Go severity classifier from synthesiser plan + patch fingerprint
      service_test.go
    notifier/
      slack.go               # NEW — Notifier impl via slack adapter
      email.go               # NEW — Notifier impl via SMTPMailer (Phase 2 plumbing)
      console.go             # NEW — Notifier impl that no-ops on send (decisions surface in /console/approvals on poll)
      multi.go               # NEW — fan-out wrapper used by ApprovalGate.service
    repo/
      approval_repo.go       # NEW — approval_decisions CRUD; dual-pool (admin for activity, app for HTTP reads)
      incidents_repo.go      # NEW — extends Phase 3's read paths with PollFatalSince(orgID, since) for Sentinel
      integrations_repo.go   # MODIFY — widen provider CHECK + add Slack helpers

  usecase/
    approval_signaler.go     # NEW — receives HTTP approve/reject → signals Temporal workflow + updates approval_decisions
    pipeline_demo.go         # MODIFY — extend the demo PipelineInput to include the fixture incident the L2 agents will read

  sentinel/                  # NEW component peer of usecase
    detector.go              # NEW — Sentinel always-on goroutine; polls incidents_raw + triggers workflow
    detector_test.go
    rules.go                 # NEW — pure-go "is this an incident" rule set (level=fatal etc.)

  workflow/
    recovery/
      activities.go          # MODIFY — Activities gains *agents.Registry (L2 + L1 set), *approval.Service, *gitops.Client
                             #            replace PathfinderDiagnose / SynthesiserPlan / ApprovalGateRoute / SentinelDetect bodies
                             #            BackendCodegen calls Validator L2 (with hypothesis) instead of Phase 4 Validator
      activities_test.go     # MODIFY — add per-activity test paths for the new bodies
      types.go               # MODIFY — PipelineInput.SynthesiserPlan, PipelineOutput.ApprovalDecisionID, PipelineOutput.PRURL
      workflow.go            # MODIFY — refactor the hard-coded L1 ordering to read SynthesiserPlan; add ApprovalGate WaitForSignal pattern + 2-min sleep race

  transport/http/
    handler/
      approvals.go           # NEW — POST/GET /v1/workspaces/{ws}/pipelines/{run}/approve|decision + GET /v1/approvals (cross-ws list)
      pipelines.go           # MODIFY — GetRun now includes approval_decision + pr_url fields in the response

  platform/
    config/
      config.go              # MODIFY — Phase 6 fields: SentinelEnabled, SentinelPollInterval, Neo4jURI/User/Pass, CausalGRPCEndpoint, GitOpsURL/Token, GitHubAppID/PrivateKeyPath, SlackEnabled, ApprovalMediumTimeoutSeconds, FixtureRepoOwner/Name/InstallationID
    neo4j/
      neo4j.go               # NEW — connection pool helper around github.com/neo4j/neo4j-go-driver/v5

cmd/
  server/main.go             # MODIFY (COORDINATOR) — wire Neo4j pool, CausalEngine client, ApprovalService, Slack adapter, GitOps client; start Sentinel goroutine
  seed-neo4j/main.go         # NEW — seeds a synthetic codegraph for the fixture repo under a config-provided ORG_ID
```

### 3.2 services/causal-inference layout (new Python sidecar)

```
services/causal-inference/
  Dockerfile                 # python:3.12-slim + dowhy + grpcio + protobuf
  pyproject.toml
  src/
    nexis_causal/
      __init__.py
      server.py              # gRPC server entrypoint; binds 0.0.0.0:8090
      handlers.py            # InferImpl — picks a canned estimand based on stacktrace fingerprint
      canned.py              # canned causal results for the demo fixtures
      proto/
        causal_pb2.py        # generated
        causal_pb2_grpc.py   # generated
  proto/
    causal.proto             # source-of-truth proto file (also imported by Go via buf or manual gen)
  Makefile                   # gen-proto + run-dev + test
  tests/
    test_handlers.py
```

The proto:

```proto
syntax = "proto3";
package nexis.causal.v1;
option go_package = "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/causal/causalpb";

message InferRequest {
  string incident_id        = 1;
  string stacktrace         = 2;
  string root_cause_node    = 3;  // hint from Pathfinder graph query
  repeated string features  = 4;
}

message InferResponse {
  string hypothesis          = 1;
  double confidence          = 2;  // 0.0..1.0
  repeated string evidence   = 3;
  string estimand_name       = 4;
  int64  duration_ms         = 5;
}

service Causal {
  rpc Infer (InferRequest) returns (InferResponse);
}
```

The Go-side stub is generated into `services/control-plane/internal/adapter/causal/causalpb/`. `internal/adapter/causal/grpc.go` is the thin client.

### 3.3 services/validator extension (Hypothesis runner)

```
services/validator/
  cmd/server/main.go         # MODIFY — adds /v1/validate?hypothesis=1 branch
  internal/
    sandbox/
      docker.go              # UNCHANGED — Phase 4 path
      hypothesis.go          # NEW — invokes the Python sidecar via Unix socket
    transport/http/
      handler.go             # MODIFY — when req.Hypothesis=true, fans out to docker + hypothesis sidecar and aggregates
  fixtures/                  # UNCHANGED from Phase 4
  hypothesis-sidecar/        # NEW
    Dockerfile               # python:3.12-slim + hypothesis + pytest + the fixture
    sidecar.py               # listens on /tmp/hypothesis.sock; reads {patch_diff} → writes JSON result
    runners/
      base_runner.py         # generates property tests by sniffing function signatures in the patched repo
```

The validator container's Dockerfile is extended to multi-stage build: the existing distroless Go binary stage, plus the Python sidecar stage; the runtime image embeds both, with a small `entrypoint.sh` that starts the Python sidecar (background) then the Go server (foreground). One container, one port, two internal processes — keeps compose unchanged.

### 3.4 services/gitops full implementation

```
services/gitops/
  cmd/server/main.go         # REWRITE — boots a real chi-based HTTP server; wires the deps
  go.mod                     # MODIFY — bump to go 1.25; add ghinstallation/v2, go-github/v60, pgx, slog
  internal/
    transport/http/
      handler.go             # NEW — POST /v1/prs (open) + GET /healthz; receives the patch+metadata payload from the control-plane
    domain/
      pr.go                  # NEW — PROpenRequest + PROpenResponse
    adapter/
      github/
        client.go            # NEW — ghinstallation-based client builder
        client_test.go
      repo/
        integrations_repo.go # NEW — read-only access to integrations.installation_id + secret_ciphertext
        audit_repo.go        # NEW — append-only audit_log writer reusing the Phase 2 chain pattern
    usecase/
      open_pr.go             # NEW — orchestrates branch create → commit → PR open → audit
    platform/
      config/config.go       # NEW — env loader (PORT, DATABASE_URL, GITHUB_APP_ID, GITHUB_APP_PRIVATE_KEY_PATH, GITOPS_TOKEN)
```

The control-plane calls `services/gitops` over HTTP (not gRPC) — keeps the dep surface tiny. The shared bearer `GITOPS_TOKEN` gates the POST; the GitHub App private key never leaves the gitops container.

### 3.5 Web layout (Next.js 16)

```
apps/web/
  app/
    (app)/
      console/
        approvals/
          page.tsx                 # REWRITE — cross-workspace pending list
          [run_id]/
            page.tsx               # NEW — single decision detail; loads plan + transcript + buttons
            client.tsx             # NEW — approve/reject form; calls /v1/workspaces/{ws}/pipelines/{run}/approve
        live-demo/
          client.tsx               # REWRITE — "Inject fault" button is now a real CTA wired to /pipelines/demo with a fixture-incident
        incidents/
          [id]/
            client.tsx             # MODIFY — render ApprovalDecision badge + PR URL link in the timeline header
  components/
    approvals/
      ApprovalBadge.tsx            # NEW — severity badge + decision pill
      ApprovalForm.tsx             # NEW — approve / reject buttons + notes textarea
      PendingApprovalsTable.tsx    # NEW — table for /console/approvals
    pipelines/
      SynthesiserPlan.tsx          # NEW — renders the synthesiser scenario + chosen L1 agents
      GitOpsPRLink.tsx             # NEW — small badge with PR url + check icon
  lib/
    approvals.ts                   # NEW — SDK: list, get, decide
```

## 4. Database

### 4.1 New tables

```
approval_decisions
  id              uuid PK
  org_id          uuid NOT NULL REFERENCES organizations(id)
  workspace_id    uuid NOT NULL REFERENCES workspaces(id)
  workflow_run_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE
  severity        text NOT NULL CHECK (severity IN ('low','medium','high'))
  decision        text NOT NULL CHECK (decision IN ('pending','approved','rejected','auto_approved','timeout_rejected'))
  decided_by      uuid REFERENCES users(id)
  decided_at      timestamptz
  notes           text
  scenario        text                                -- 'schema_drift'|'null_deref'|'oom'|'unknown'
  risk_score      numeric(5, 2)                       -- 0..100, Phase 6 set to bucket midpoint (low=15, med=50, high=85)
  created_at      timestamptz NOT NULL DEFAULT now()
  UNIQUE (workflow_run_id)

slack_notifications
  id              uuid PK
  org_id          uuid NOT NULL REFERENCES organizations(id)
  workflow_run_id uuid REFERENCES workflow_runs(id) ON DELETE SET NULL
  channel         text                                -- ledger-only; the webhook URL implies the channel
  kind            text NOT NULL                        -- 'approval_requested'|'pipeline_complete'
  status          text NOT NULL CHECK (status IN ('queued','sent','failed'))
  http_status     int
  attempted_at    timestamptz NOT NULL DEFAULT now()
  error           text
```

Both tables RLS-protected on `org_id` with the standard `tenant_isolation` policy using `NULLIF(current_setting('app.current_org_id', true), '')::uuid`. `GRANT SELECT, INSERT, UPDATE, DELETE` to `nexis_app` on both.

Indexes:
- `approval_decisions (org_id, workspace_id, decision, created_at DESC)` — drives the pending list.
- `approval_decisions (workflow_run_id)` — unique upfront; also the GET-by-run lookup.
- `slack_notifications (org_id, workflow_run_id)` — ledger lookups.

### 4.2 Modifications

* `integrations.provider` CHECK widens from `IN ('github','sentry','argocd')` to `IN ('github','sentry','argocd','slack')`. One ALTER per up-migration. The Phase 3 Drizzle schema also extends its `provider` enum.
* `workflow_runs` — no schema change. The `output` jsonb column already accepts the new `approval_decision_id` + `pr_url` fields the workflow writes.

## 5. Sentinel design

### 5.1 Detection rules (`internal/sentinel/rules.go`)

Phase 6 ships exactly two rules; both apply per-org:

1. **Fatal-level Sentry event.** Any `incidents_raw` row where `source='sentry'` AND `level='fatal'` AND `received_at > sentinel_state.last_seen_at` for the org → trigger workflow.
2. **Error rate spike (synthetic).** When `count(*) FROM incidents_raw WHERE org_id=$1 AND received_at > now()-interval '5 minutes'` is **≥ 5 events**, and no Sentinel workflow has been triggered for that org in the last 5 minutes → trigger workflow with the most recent fatal-or-error row as the incident.

Both rules are pure functions over `(rows, now) → []IncidentTrigger`. The detector goroutine evaluates them once per tick.

### 5.2 Detector goroutine (`internal/sentinel/detector.go`)

* Started from `cmd/server/main.go` after the HTTP server bootstraps, alongside the existing cron runner.
* Tick interval: `SENTINEL_POLL_INTERVAL_MS` (default 10000). Each tick:
  1. Read the in-memory `last_seen_at` map keyed by `org_id` (initialized at startup from `MAX(received_at) FROM incidents_raw GROUP BY org_id` via the admin pool).
  2. For each org with a connected Sentry integration (`SELECT org_id FROM integrations WHERE provider='sentry' AND status='connected'`), call `IncidentsRepo.PollFatalSince(orgID, last_seen_at)` via the admin pool (bypasses RLS — system-job path).
  3. Apply rules; collect `[]IncidentTrigger`.
  4. For each trigger, look up the org's default workspace (`SELECT id FROM workspaces WHERE org_id=$1 ORDER BY created_at LIMIT 1`) and call `WorkflowService.Start(ctx, princ_system, workspaceID, PipelineInput{IncidentID: trigger.IncidentID, ...})`.
  5. Update `last_seen_at[orgID]` to the trigger's `received_at`.
* Exit on `ctx.Done()`; tick errors slog at WARN but never panic.
* `SENTINEL_ENABLED=0` skips the goroutine entirely (used in tests).

### 5.3 Workflow trigger principal

The detector creates a synthetic `domain.Principal` with `Role="system"` + `OrgID=<the triggered org>`. The principal is **not** persisted as a user; the workflow's `created_by` is left NULL (the `workflow_runs.created_by` column already accepts NULL). The audit row uses `actor="sentinel-detector"` instead of a UUID.

### 5.4 Sentinel.Detect activity (acknowledgement)

The Temporal activity `Sentinel.Detect` becomes a no-op acknowledgement: it reads the incident row from `incidents_raw` using the org's admin pool, returns `{incident_id, title, service, environment, level, received_at}` as the payload, and slogs `sentinel.detect.ack`. No external calls; no LLM; ~50 ms wallclock. Activity body lives inline in `activities.go` — there is no `internal/adapter/agents/sentinel/` package (see Risk 13.1).

## 6. Pathfinder design

### 6.1 Graph store

Neo4j is already running in compose. The fixture codegraph schema:

```
(:Module {repo_sha, file_path})          - one node per source file
(:Symbol {repo_sha, file_path, name})    - one node per top-level function/class
(:Symbol)-[:DEFINED_IN]->(:Module)
(:Symbol)-[:CALLS]->(:Symbol)            - synthetic call edges
(:Symbol)-[:IMPORTS]->(:Module)
(:Symbol)-[:RAISED]->(:ExceptionType)    - if the symbol can raise
```

`cmd/seed-neo4j/main.go` walks `services/validator/fixtures/`, parses each `.py` via a tiny regex-based AST (Phase 7 swaps to a real parser), and writes the nodes + edges via the Neo4j Go driver. The seed is org-scoped: `MERGE (m:Module {repo_sha, file_path, org_id})`.

### 6.2 Activity flow

`PathfinderDiagnose(ctx, in PipelineInput) → ActivityResult`:

1. Read the incident payload from `in.Incident` (passed in by the workflow from `Sentinel.Detect`'s output).
2. Extract a candidate stack frame: the last frame of `incident.stacktrace` whose `file_path` matches a `Module` node in the graph. Cypher:
   ```cypher
   MATCH (m:Module {file_path:$file, org_id:$org}) RETURN m
   ```
3. Query the symbol that *contains* that frame (synthetic line ranges baked at seed time):
   ```cypher
   MATCH (s:Symbol {file_path:$file, org_id:$org})
   WHERE s.line_start <= $line AND s.line_end >= $line
   RETURN s LIMIT 1
   ```
4. Walk one hop of `CALLS` and `RAISED` to collect evidence:
   ```cypher
   MATCH (s)-[r:CALLS|RAISED*1..2]-(t) RETURN type(r), t LIMIT 20
   ```
5. Call `CausalEngine.Infer(ctx, InferRequest{stacktrace, root_cause_node: s.name, features: evidence})` over gRPC.
6. (Optional, Phase 6 toggles off by default) — invoke the Phase 5 LLM spine via the agent's `LLMProvider` to produce a one-sentence human hypothesis from `causal.hypothesis + causal.evidence`. Toggleable via `PATHFINDER_LLM_REFINE=1`.
7. Return `AgentOutput.Structured = {root_cause_node, hypothesis, confidence, evidence_chain[]}` + the standard token/cost telemetry (zero when LLM refinement is off).

### 6.3 Causal sidecar contract

`services/causal-inference/src/nexis_causal/handlers.py` implements `Infer`:

* Hashes the stacktrace via `sha256`, takes the first 8 bytes as a fingerprint.
* Looks up the fingerprint in `canned.py` — a dict keyed off the three demo fixtures (`fixture-null-pointer`, `fixture-schema-drift`, `fixture-oom`).
* Returns the canned `(hypothesis, confidence, evidence, estimand_name)`.
* Falls back to `{hypothesis:"<unknown>", confidence:0.0}` for unknown fingerprints; Phase 6 still treats `confidence<0.3` as a `scenario:"unknown"` synthesiser route.

The sidecar's actual DoWhy import + a `dowhy.CausalModel` placeholder call run in `handlers.py` as well, but the placeholder operates on a 5-row in-memory pandas frame — the call is real (so dependencies are exercised) but the output is overridden by the canned table. Phase 7 wires a real causal model.

## 7. Synthesiser design

### 7.1 Routing table (`internal/adapter/agents/synthesiser/routes.go`)

```go
type Scenario string

const (
    ScenarioNullDeref   Scenario = "null_deref"
    ScenarioSchemaDrift Scenario = "schema_drift"
    ScenarioOOM         Scenario = "oom"
    ScenarioUnknown     Scenario = "unknown"
)

// Plan maps a scenario to the ordered L1 invocation list. The workflow
// consumes this list to invoke the L1 agents in the chosen order; agents
// not in the list are skipped (their activities are still recorded but
// emit a 'skipped' status so the timeline UI greys them out).
var routingTable = map[Scenario][]domain.AgentName{
    ScenarioNullDeref:   {domain.AgentNameArchitect, domain.AgentNameBackend, domain.AgentNameQA},
    ScenarioSchemaDrift: {domain.AgentNameArchitect, domain.AgentNameDataEngineer, domain.AgentNameBackend, domain.AgentNameQA},
    ScenarioOOM:         {domain.AgentNameArchitect, domain.AgentNameDevOps, domain.AgentNameBackend},
    ScenarioUnknown:     {domain.AgentNameArchitect, domain.AgentNameBackend, domain.AgentNameQA},
}
```

### 7.2 Scenario classification

Two-stage:

1. **Fast path.** If `pathfinder.evidence_chain` contains tokens that match known signatures (`OperationalError` + `column` → `schema_drift`; `NoneType` + `attribute` → `null_deref`; `MemoryError`|`OOMKilled` → `oom`), return that scenario without calling the LLM.
2. **LLM fallback.** When the fast path returns nothing, the Synthesiser's `provider.go` invokes the Phase 5 `LLMProvider` with a JSON-mode classification prompt (system + the pathfinder output + the routing table as inline schema). Returns one of the four scenario strings. Falls back to `ScenarioUnknown` on schema-mismatch (after the standard 2-retry from Phase 5).

### 7.3 Activity output

```json
{
  "scenario":       "null_deref",
  "confidence":     0.82,
  "selected_agents": ["architect", "backend", "qa"],
  "skipped_agents": ["devops", "data_engineer"],
  "rationale":      "stacktrace ends in NoneType attribute access; no schema changes detected; no memory pressure",
  "estimated_duration_ms": 240000
}
```

The workflow stores `selected_agents` on `PipelineInput.SynthesiserPlan` and uses it to drive the L1 ordering.

## 8. Validator L2 design

### 8.1 Wire change

`POST /v1/validate` request body gains an optional `hypothesis: bool` field. The response body gains:

```json
{
  "tests_passed":      true,
  "test_count":        12,
  "fail_count":        0,
  "coverage":          0.84,
  "duration_ms":       4120,
  "hypothesis_failures": [
    { "test": "test_safe_div_property", "counterexample": "b=0", "shrunk": true }
  ],
  "logs":              "...pytest stdout..."
}
```

`hypothesis_failures` is **always** present in the response (empty array when no property tests ran or no failures observed).

### 8.2 Hypothesis sidecar protocol

* Sidecar binds Unix socket `/tmp/hypothesis.sock` inside the validator container.
* Wire: 4-byte BE length prefix + JSON body `{patch_diff, repo_path, max_examples, deadline_ms}`.
* Response: 4-byte BE length prefix + JSON `{failures: [...], duration_ms}`.
* Default `max_examples=20`, `deadline_ms=10000`. Tunable via env on the validator container.

### 8.3 Property test generation

`sidecar.py` walks `repo_path` after applying the patch and, for each top-level function with a return type hint, emits a property test like:

```python
from hypothesis import given, strategies as st
from nexis_fixture.api import safe_div

@given(st.integers(), st.integers())
def test_safe_div_property(a, b):
    if b == 0:
        try:
            safe_div(a, b)
        except ValueError:
            return
        assert False, "expected ValueError on b=0"
    else:
        assert safe_div(a, b) == a // b
```

A generated test runs under `hypothesis` with the configured budget. Phase 7 swaps to typed-Python static-analysis-driven generation.

### 8.4 Validator L2 agent wrapper

`internal/adapter/agents/validator_l2/provider.go` is a thin shim that calls the existing `services/validator` HTTP API with `hypothesis=true` and packages the response as `AgentOutput`. `AgentName = AgentNameValidatorL2` (`"validator_l2"`); ledger rows record `Provider="hypothesis"`, `Model="hypothesis-0.x"`, `TokensIn=TokensOut=0`, `CostCents=0`. Status flows: `tests_passed && fail_count==0 && len(hypothesis_failures)==0` → `success`; any failure → `success=false` with the failures inlined in `Structured.hypothesis_failures`.

## 9. Approval Gate design

### 9.1 Severity rules (`internal/adapter/approval/severity.go`)

Pure function over the synthesiser plan + the unified-diff patch fingerprint:

```
if scenario == "schema_drift" OR patch touches files matching:
    *.sql, **/migrations/**, **/auth/**, **/security/**, **/crypto/**
  → high
else if scenario == "unknown"
  → high
else if scenario == "oom"
  → medium
else if patch_lines_changed < 10 AND only_files_under("apps/web/components/")
  → low
else
  → medium
```

Risk score (Phase 6 simple bucket midpoint): `{low: 15, medium: 50, high: 85}`.

### 9.2 Severity routing

| Severity | Behaviour |
|---|---|
| `low` | Auto-approves immediately. `approval_decisions.decision = 'auto_approved'`. No notification. Workflow proceeds without signal wait. |
| `medium` | Workflow blocks on signal channel with a 2-minute sleep race. If a human signals approve/reject within 2 min → decision recorded as `approved` or `rejected`. If no signal in 2 min → `auto_approved` (timeout-to-approve). Notifies via Slack + email + console. |
| `high` | Workflow blocks on signal channel indefinitely (no auto-approve). Notifies via Slack + email + console. A human MUST decide. The workflow execution timeout (Phase 4: 10 min) bounds the wait — past 10 min the workflow times out and the decision row gets `timeout_rejected`. |

### 9.3 Temporal pattern

```go
// Inside RecoveryPipeline, after the L1 agents complete:

// 1. Compute severity from synthesiser plan + backend patch.
severity := approval.Classify(synthesiserPlan, backendOutput.Patch)

// 2. Auto-approve low immediately.
if severity == "low" {
    decisionID, _ := workflow.ExecuteActivity(...,
        (*Activities).ApprovalAutoApprove,
        ApprovalAutoApproveInput{...},
    ).Get(ctx, ...)
    // proceed
}

// 3. medium|high — create the pending row, notify, wait for signal.
if severity != "low" {
    _, _ = workflow.ExecuteActivity(...,
        (*Activities).ApprovalRequestNotify,
        ApprovalRequestInput{Severity: severity, ...},
    ).Get(ctx, ...)

    var sig ApprovalSignal
    ch := workflow.GetSignalChannel(ctx, "approve")

    if severity == "medium" {
        // 2-minute race: signal vs timeout-to-approve.
        sel := workflow.NewSelector(ctx)
        sel.AddReceive(ch, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, &sig) })
        sel.AddFuture(workflow.NewTimer(ctx, 2*time.Minute), func(workflow.Future) {
            sig = ApprovalSignal{Decision: "auto_approved"}
        })
        sel.Select(ctx)
    } else {
        // high — block until a signal arrives (workflow execution timeout bounds this).
        ch.Receive(ctx, &sig)
    }

    _, _ = workflow.ExecuteActivity(...,
        (*Activities).ApprovalRecordDecision,
        ApprovalRecordInput{Signal: sig, ...},
    ).Get(ctx, ...)

    if sig.Decision == "rejected" || sig.Decision == "timeout_rejected" {
        return PipelineOutput{}, temporal.NewNonRetryableApplicationError(
            "approval rejected", "ApprovalRejected", nil,
        )
    }
}
```

### 9.4 Signal handler (HTTP → Temporal)

`POST /v1/workspaces/{ws}/pipelines/{run}/approve`:

1. Verify workspace ownership + RBAC (owner|admin).
2. RLS-bind to org; read the pending `approval_decisions` row for the run.
3. Reject if `decision != 'pending'`.
4. Call `WorkflowService.SignalApproval(ctx, runID, ApprovalSignal{Decision: ..., DecidedBy: princ.UserID, Notes: req.Notes})` which under the hood is `temporalClient.SignalWorkflow(ctx, workflowID, runID, "approve", signal)`.
5. Update `approval_decisions` row to `decision=approved|rejected`, `decided_at=now()`, `decided_by=princ.UserID`, `notes=req.Notes`.
6. Audit `approval.decided` with `{decision, severity, run_id, decided_by}`.

The handler returns `202` immediately — the workflow picks the signal up async.

### 9.5 Notifier wiring

`approval.Service.Notify(ctx, run, severity)` fans out via `notifier.Multi` to:
* `notifier.Slack` — when org has a connected Slack integration.
* `notifier.Email` — to every workspace owner|admin user via `SMTPMailer`.
* `notifier.Console` — no-op send (the UI just queries the pending list).

Failures in any channel slog at WARN and write `slack_notifications` rows for the Slack path; the workflow continues regardless — a failed notification does not block the decision (a user can still approve via the console).

## 10. GitOps service design

### 10.1 HTTP contract (`services/gitops/`)

```
POST /v1/prs
Content-Type: application/json
Authorization: Bearer <GITOPS_TOKEN>

Request:
{
  "org_id":          "<uuid>",
  "workflow_run_id": "<uuid>",
  "repo": {
    "owner": "nexis-eco",
    "name":  "fixture-recovery-demo",
    "default_branch": "main"
  },
  "patch_diff":      "diff --git ...",
  "branch_name":     "nexis/fix-<run_id_short>",     // caller-supplied; gitops doesn't mint
  "pr_title":        "Recover from null_deref",
  "pr_body":         "...markdown with agent transcript..."
}

Response (201):
{
  "pr_number":  47,
  "pr_url":     "https://github.com/nexis-eco/fixture-recovery-demo/pull/47",
  "branch":     "nexis/fix-ab12cd34",
  "head_sha":   "9f4c...",
  "opened_at":  "2026-05-13T14:22:01Z"
}
```

Errors return `502` for upstream GitHub failures, `404` for missing installation, `400` for malformed patches.

### 10.2 Installation auth

`internal/adapter/github/client.go`:

```go
type ClientBuilder struct {
    AppID         int64
    PrivateKeyPEM []byte
}

func (b *ClientBuilder) ForOrg(ctx context.Context, orgID string, integrationsRepo *repo.IntegrationsRepo) (*github.Client, error) {
    installID := integrationsRepo.GetInstallationID(ctx, orgID, "github") // int64
    transport, err := ghinstallation.New(http.DefaultTransport, b.AppID, installID, b.PrivateKeyPEM)
    if err != nil { return nil, err }
    return github.NewClient(&http.Client{Transport: transport}), nil
}
```

### 10.3 Branch + commit + PR flow

`internal/usecase/open_pr.go`:

```go
func (u *OpenPRUsecase) Run(ctx context.Context, in domain.PROpenRequest) (domain.PROpenResponse, error) {
    cli, _ := u.builder.ForOrg(ctx, in.OrgID, u.integrationsRepo)

    // 1. Read default branch SHA.
    ref, _, _ := cli.Git.GetRef(ctx, in.Repo.Owner, in.Repo.Name, "heads/"+in.Repo.DefaultBranch)
    baseSHA := *ref.Object.SHA

    // 2. Create the new branch.
    newRef := &github.Reference{
        Ref:    github.String("refs/heads/" + in.BranchName),
        Object: &github.GitObject{SHA: github.String(baseSHA)},
    }
    _, _, err := cli.Git.CreateRef(ctx, in.Repo.Owner, in.Repo.Name, newRef)
    if err != nil { return resp, err }

    // 3. Apply the patch as a single commit using the raw GitHub API.
    //    Strategy: parse the unified diff into {file_path → new_content},
    //    write each as a Blob, build a Tree referencing baseSHA, create a Commit
    //    with parent=baseSHA, point the branch ref at the new commit.
    files := parseUnifiedDiff(in.PatchDiff, cli, in.Repo, baseSHA)
    treeEntries := []*github.TreeEntry{}
    for path, content := range files {
        blob, _, _ := cli.Git.CreateBlob(ctx, in.Repo.Owner, in.Repo.Name, &github.Blob{
            Content:  github.String(base64.StdEncoding.EncodeToString([]byte(content))),
            Encoding: github.String("base64"),
        })
        treeEntries = append(treeEntries, &github.TreeEntry{
            Path: github.String(path),
            Mode: github.String("100644"),
            Type: github.String("blob"),
            SHA:  blob.SHA,
        })
    }
    tree, _, _ := cli.Git.CreateTree(ctx, in.Repo.Owner, in.Repo.Name, baseSHA, treeEntries)
    commit, _, _ := cli.Git.CreateCommit(ctx, in.Repo.Owner, in.Repo.Name, &github.Commit{
        Message: github.String(in.PRTitle),
        Tree:    tree,
        Parents: []*github.Commit{{SHA: github.String(baseSHA)}},
    })
    cli.Git.UpdateRef(ctx, in.Repo.Owner, in.Repo.Name, &github.Reference{
        Ref:    github.String("refs/heads/" + in.BranchName),
        Object: &github.GitObject{SHA: commit.SHA},
    }, false)

    // 4. Open the PR.
    pr, _, _ := cli.PullRequests.Create(ctx, in.Repo.Owner, in.Repo.Name, &github.NewPullRequest{
        Title: github.String(in.PRTitle),
        Head:  github.String(in.BranchName),
        Base:  github.String(in.Repo.DefaultBranch),
        Body:  github.String(in.PRBody),
    })

    // 5. Audit.
    u.auditRepo.Append(ctx, "gitops.pr_opened", map[string]any{
        "org_id":  in.OrgID, "run_id": in.WorkflowRunID, "repo": in.Repo.Owner+"/"+in.Repo.Name,
        "pr_number": *pr.Number, "pr_url": *pr.HTMLURL,
    })

    return domain.PROpenResponse{
        PRNumber: *pr.Number, PRURL: *pr.HTMLURL,
        Branch: in.BranchName, HeadSHA: *commit.SHA,
        OpenedAt: time.Now().UTC().Truncate(time.Microsecond),
    }, nil
}
```

### 10.4 Patch-diff parsing

`parseUnifiedDiff` (defined in `internal/usecase/open_pr.go`) is intentionally minimal: it reads the unified diff hunks the Backend L1 produces (single-file or multi-file `diff --git` headers + `@@` hunks), reconstructs the new file contents by replaying the hunks against the current `baseSHA` content (fetched via `Repositories.GetContents`). Edge cases: file creation (no parent blob → start from empty), file deletion (omitted from tree → not supported in Phase 6; backend agent never emits deletions). Binary patches are rejected with `400`.

### 10.5 Control-plane → GitOps wire

After `ApprovalGate` resolves with `decision=approved|auto_approved`, a new activity `GitOpsOpenPR` invokes `services/gitops/POST /v1/prs` with the synthesised payload. The control-plane writes the returned `pr_url` to `workflow_runs.output.pr_url` so the timeline UI can render the link. Failure to open the PR fails the workflow (the patch never gets to a human).

## 11. Slack integration design

### 11.1 Connection model

* `provider='slack'` row in `integrations`.
* `installation_id` field is reused as the **incoming-webhook URL** (sealed via `domain.KeyVault` like every other secret). We accept the type-mismatch (text vs id) because the Phase 3 schema is `installation_id text` — already a string column. No schema migration needed.
* `metadata.channel_name` carries the human-readable channel for display.
* Connect flow: user pastes a webhook URL into `/console/integrations`; provider validates by sending a "test" message; on 2xx the row is `connected`.

### 11.2 Notification format (Block Kit)

```json
{
  "blocks": [
    { "type": "header", "text": { "type": "plain_text", "text": "🛠 Approval required — null_deref" } },
    { "type": "section", "fields": [
        { "type": "mrkdwn", "text": "*Workspace*\n<workspace-name>" },
        { "type": "mrkdwn", "text": "*Severity*\nmedium" },
        { "type": "mrkdwn", "text": "*Run ID*\n<run-id-short>" },
        { "type": "mrkdwn", "text": "*Patch*\n12 lines changed across 1 file" }
    ] },
    { "type": "section", "text": { "type": "mrkdwn", "text": "*Plan:* Architect → Backend → QA\n*Hypothesis:* NoneType attribute access on safe_div(b=0)" } },
    { "type": "actions", "elements": [
        { "type": "button", "url": "<console-url>/console/approvals/<run-id>", "text": { "type": "plain_text", "text": "Review in console" }, "style": "primary" }
    ] }
  ]
}
```

Phase 6 uses incoming webhooks only — the buttons link out to the console; we don't yet handle Slack interactivity callbacks (Phase 7).

## 12. ArgoCD design (documentation-only for Phase 6)

`infra/argocd/app-of-apps.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: nexis-fixture-demo
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/nexis-eco/fixture-recovery-demo
    path: manifests
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: nexis-fixture
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

`infra/argocd/rollouts.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: nexis-fixture
spec:
  replicas: 3
  strategy:
    canary:
      steps:
        - setWeight: 10
        - pause: { duration: 60 }
        - analysis:
            templates:
              - templateName: slo-latency-p95
        - setWeight: 50
        - pause: { duration: 60 }
        - analysis:
            templates:
              - templateName: slo-latency-p95
        - setWeight: 100
---
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: slo-latency-p95
spec:
  args: [{name: service}]
  metrics:
    - name: latency-p95
      provider:
        prometheus:
          address: http://prometheus.monitoring:9090
          query: |
            histogram_quantile(0.95,
              sum(rate(http_request_duration_seconds_bucket{service="{{args.service}}"}[2m])) by (le))
      successCondition: result[0] < 0.3
      failureLimit: 1
```

`infra/argocd/README.md` documents the integration plan: Phase 7 starts ArgoCD in compose (or k3d), points the `app-of-apps` at the fixture repo, and the Argo Rollouts CR auto-aborts on SLO breach. Phase 6's demo PR + audit trail is the input the Phase 7 wiring consumes — no breaking changes between phases.

## 13. HTTP surface

### 13.1 New routes (control-plane)

```
POST   /v1/workspaces/{ws}/pipelines/{run_id}/approve   → 202 ApprovalDecision   [auth, RLS, owner|admin]
       body: { "decision": "approve"|"reject", "notes": "..." }

GET    /v1/workspaces/{ws}/pipelines/{run_id}/decision  → 200 ApprovalDecision   [auth, RLS, any role]

GET    /v1/approvals                                    → 200 [ApprovalDecision] [auth, RLS, any role]
       query: ?status=pending&limit=50&before=<iso>     - cross-workspace pending list for the current org

POST   /v1/admin/sentinel/trigger                       → 202 WorkflowRun         [auth, RLS, owner|admin]
       body: { "workspace_id": "...", "incident_label": "fixture-null-pointer" }
       Manual trigger of a Sentinel-style workflow start using a fixture incident. Powers the Live Demo CTA.
```

### 13.2 New routes (gitops)

```
POST   /v1/prs                                          → 201 PROpenResponse    [Bearer GITOPS_TOKEN]
GET    /healthz                                         → 200 (unchanged)
```

### 13.3 Modified routes (control-plane)

```
GET    /v1/workspaces/{ws}/pipelines/{run_id}           # response gains:
                                                        #   "approval_decision": { id, severity, decision, decided_by, decided_at, notes },
                                                        #   "pr_url": "<github pr url or null>"
```

### 13.4 RBAC

| Endpoint | owner | admin | member |
|---|---|---|---|
| `POST /v1/workspaces/{ws}/pipelines/{run}/approve` | ✓ | ✓ | ✗ |
| `GET  /v1/workspaces/{ws}/pipelines/{run}/decision` | ✓ | ✓ | ✓ |
| `GET  /v1/approvals` | ✓ | ✓ | ✓ |
| `POST /v1/admin/sentinel/trigger` | ✓ | ✓ | ✗ |

All writes audit via the existing `domain.AuditWriter`.

## 14. Workflow refactor

The Phase 4 `RecoveryPipeline` hard-codes the L1 agent order (Architect → Backend → QA, then DevOps||DataEngineer in parallel). Phase 6 refactors this to consume `PipelineInput.SynthesiserPlan.SelectedAgents`:

```go
// After Synthesiser.Plan:
plan := synthOutput.Structured["selected_agents"].([]string)
// Materialize the activity map once.
table := map[domain.AgentName]any{
    domain.AgentNameArchitect:     (*Activities).ArchitectSolution,
    domain.AgentNameBackend:       (*Activities).BackendCodegen,
    domain.AgentNameQA:            (*Activities).QATestGen,
    domain.AgentNameDevOps:        (*Activities).DevOpsPipeline,
    domain.AgentNameDataEngineer:  (*Activities).DataEngineerMigrate,
}

// Sequential walk in the order the Synthesiser picked.
for _, name := range plan {
    fn := table[domain.AgentName(name)]
    if fn == nil { continue }
    r, err := runActivity(roleFor(name), nameFor(name), fn, llmActivityOpts)
    if err != nil { return PipelineOutput{}, err }
    results = append(results, r)
}
```

Skipped agents (those not in `plan`) still record one `activity_events` row with `status='skipped'` so the timeline UI greys them out — emitted via a small `recordSkipped` helper. The parallel DevOps||DataEngineer fork in Phase 4 is gone — the Synthesiser is allowed to pick parallel groups in future iterations, but Phase 6 ships strict sequential ordering and revisits parallelism in Phase 7.

After the L1 walk completes, the workflow runs:
1. `Validator L2` activity (the L2 wrapper around `services/validator` with `hypothesis=true`) — records as `ValidatorL2.Validate` in `activity_events`. Failures here block approval; the workflow returns `failed` immediately.
2. `ApprovalGate` block (severity classification + signal wait — see §9.3).
3. `GitOpsOpenPR` activity — fires only on approve. On rejection / timeout-reject the workflow returns without opening a PR.

The total walltime budget for Phase 6 demo runs: L2 detect+diagnose+plan (~5–10 s with the LLM + gRPC calls) + L1 walk (~60–120 s on OpenAI per Phase 5) + ValidatorL2 (~10–15 s with Hypothesis) + ApprovalGate (≤2 min for medium auto, faster for low) + GitOpsOpenPR (~3–5 s). The acceptance criterion is **PR opened in < 5 minutes** from inject-fault click — matches the §1 demo budget.

## 15. Acceptance criteria

1. **Inject fault → PR opened in < 5 min.** Click "Inject fault" on `/console/live-demo` with `fixture-null-pointer` selected → workflow starts within 1 s → all 9 activity rows recorded → `approval_decisions` row created with `severity='medium'` → console approval click → workflow signals + completes → `workflow_runs.output.pr_url` non-null → a PR exists on `github.com/nexis-eco/fixture-recovery-demo` with the expected title and body within 5 minutes wallclock.
2. **Severity routing works.** Inject `fixture-schema-drift` (which touches a `.sql` migration file) → `approval_decisions.severity='high'` → no auto-approve fires after 2 minutes → workflow remains `running` until a human approves → audit `approval.decided` row present after the click.
3. **Low-severity auto-approve.** Inject a fixture whose synthesised patch is `< 10` lines in `apps/web/components/` (we ship `fixture-trivial-ui-fix` for this) → `approval_decisions.decision='auto_approved'` within ~1 s of plan synthesis → no notification sent → PR opens.
4. **Medium-severity timeout-to-approve.** Inject `fixture-null-pointer` → wait 2 minutes without clicking → `approval_decisions.decision='auto_approved'` (the timeout branch) → workflow proceeds to GitOps → PR opens.
5. **Rejection blocks PR.** Inject `fixture-schema-drift` → click reject → `approval_decisions.decision='rejected'` → workflow `status='failed'` with error containing `ApprovalRejected` → no PR opened → no audit `gitops.pr_opened` row.
6. **ArgoCD rollback simulation.** **Documented**: the demo guide instructs the reviewer to manually merge the PR, then simulate an SLO breach by editing the rollouts.yaml AnalysisTemplate threshold, redeploy, observe rollback. Phase 6 ships the docs; runtime is Phase 7. **Pass criterion = the docs in `infra/argocd/README.md` walk through this end-to-end without ambiguity** (reviewed manually at DoD).
7. **Sentinel auto-trigger.** With `SENTINEL_ENABLED=1` set, POST a Sentry webhook with `level='fatal'` for an org with a connected Sentry integration → within 15 s a new `workflow_runs` row exists with `created_by=NULL` and `current_step='Sentinel.Detect'`.
8. **Pathfinder graph hit.** `SELECT count(*) FROM relationships WHERE org_id=$1` (via the Neo4j `cypher-shell`) returns ≥ 20 after `cmd/seed-neo4j --org-id=$ORG_ID` runs. The Pathfinder activity payload for the null-pointer fixture includes `root_cause_node='safe_div'`.
9. **Causal sidecar reachable.** `grpcurl -plaintext causal-inference:8090 nexis.causal.v1.Causal/Infer` with `{"stacktrace":"<fixture-stack>"}` returns a canned `confidence > 0.5` for the demo fingerprints.
10. **Validator L2 returns Hypothesis results.** `POST /v1/validate` with `hypothesis=true` against the fixture+null-deref patch returns `hypothesis_failures: []` (the patch is correct) and `duration_ms < 30000`.
11. **Slack notification sent.** With a connected Slack integration, after step 4 the `slack_notifications` ledger shows `status='sent'` and `http_status=200`.
12. **Email notification sent.** MailHog UI (`http://localhost:8025`) shows an email titled `[NEXIS] Approval required — null_deref` to the workspace owner.
13. **3 design partners self-serve.** External criterion outside the scope of code verification, but the demo flow MUST be re-runnable without a developer present using the deployed `/console/live-demo` surface.
14. **Auditability.** `SELECT count(*) FROM audit_log WHERE target=<run_id> AND action LIKE 'approval.%'` returns 2 (`approval.requested`, `approval.decided`). The audit chain HMAC verifies green via the existing `audit chain verify` admin endpoint.
15. **RBAC + RLS.** A `member`-role user gets 403 on `POST /v1/workspaces/{ws}/pipelines/{run}/approve`. A user in org A cannot read/list approval decisions in org B (repeat the Phase 3 RLS verification curls with two principals).
16. **`make build && make test && make vet && make arch` green** on control-plane; `pnpm typecheck && pnpm build` green on web; `cd services/validator && go build && go test ./...` green; `cd services/gitops && go build && go test ./...` green; `cd services/causal-inference && python -m pytest tests/` green.

## 16. Stage breakdown

| Stage | Title | Pattern |
|---|---|---|
| 0 | Schema (`approval_decisions`, `slack_notifications`) + RLS + grants + `integrations` provider widening + config + env | sequential (coordinator-owned files) |
| 1 | Sentinel detector goroutine + `IncidentsRepo.PollFatalSince` + rules + main.go wiring | sequential |
| 2 | Neo4j adapter (`internal/adapter/graphstore/neo4j`) + `internal/platform/neo4j` pool + `cmd/seed-neo4j` (Pattern A: ships in parallel with Phase 5 pgvector seed) | sequential |
| 3 | Pathfinder L2 agent (graph + causal sidecar gRPC client) + Python `services/causal-inference/` sidecar | **Pattern C shard 1** of the 4 L2 agent shards |
| 4 | Synthesiser L2 agent (routing table + optional LLM classification) | **Pattern C shard 2** |
| 5 | Validator L2 agent (`internal/adapter/agents/validator_l2/`) + `services/validator` Hypothesis sidecar extension | **Pattern C shard 3** |
| 6 | Approval Gate domain + repo + severity classifier + Temporal signal pattern + auto-timeout race | **Pattern C shard 4** (kicks off in parallel with Stages 3-5 once the coordinator has pre-written shared types in Stage 0/1) |
| 7 | Slack adapter (`internal/adapter/integration/slack/`) + email notifier + console notifier + multi-fanout | **Pattern B parallel** with Stage 8 + 9 |
| 8 | GitOps service full implementation (`services/gitops/` rewrite — ghinstallation, branch/commit/PR, audit) | **Pattern B parallel** with Stage 7 + 9 |
| 9 | Approvals UI surface (`/console/approvals/*`) + Live Demo CTA real impl + Synthesiser/PR badges in incident timeline | **Pattern B parallel** with Stage 7 + 8 |
| 10 | ArgoCD design docs (`infra/argocd/{app-of-apps,rollouts,README}.yaml/md`) — documentation only | sequential |
| 11 | Workflow refactor (`RecoveryPipeline` consumes `SynthesiserPlan`) + activities glue (replace stubs with L2 + ApprovalGate + GitOpsOpenPR) + new HTTP routes wiring + E2E + DoD | sequential (coordinator-only — touches workflow + transport + main.go) |

Stages 3-6 form the **Pattern C** wave — four backend-engineer agents in one dispatch message, each owning one L2 shard with cleanly disjoint file sets:

| Shard | Owned paths |
|---|---|
| 3 (Pathfinder) | `internal/adapter/graphstore/neo4j/**`, `internal/adapter/causal/**`, `internal/adapter/agents/pathfinder/**`, `services/causal-inference/**`, `cmd/seed-neo4j/**` |
| 4 (Synthesiser) | `internal/adapter/agents/synthesiser/**` |
| 5 (Validator L2) | `internal/adapter/agents/validator_l2/**`, `services/validator/internal/sandbox/hypothesis.go`, `services/validator/hypothesis-sidecar/**` |
| 6 (Approval Gate) | `internal/adapter/approval/**`, `internal/adapter/repo/approval_repo.go`, `internal/adapter/notifier/**`, `internal/usecase/approval_signaler.go` |

Stages 7-9 form the **Pattern B** wave — three parallel agents (one backend for Slack, one backend for GitOps service, one frontend for the Approvals UI). All three depend only on Phase 6's shared types (domain ports + repo signatures), so they don't contend on shared files.

The coordinator merges each wave by running `make build && make test && make vet && make arch` once after all shards finish, then proceeds to the next wave. Stage 11 — the workflow refactor that wires everything together — is the only stage that *must* be sequential because it touches `cmd/server/main.go`, `internal/workflow/recovery/{activities,workflow,types}.go`, and `internal/transport/http/server.go` simultaneously.

## 17. Risks / open items

### 13.1 Sentinel-the-agent vs Sentinel-the-detector

The detector goroutine and an "L2 agent named sentinel" are slightly different concerns. Phase 6 chooses **only the detector**: `internal/sentinel/` is the always-on monitor; the activity `Sentinel.Detect` is an inline no-op acknowledgement implemented directly in `activities.go`; there is **no** `internal/adapter/agents/sentinel/` package. The token ledger never records a row for `agent='sentinel'`. Why: an L2 agent that "decides if this is an incident" is the same logic as the detector — splitting them would double-write the same rule set in two places. Phase 7 can re-introduce a Sentinel agent when we layer streaming OTel detection — at that point a real LLM decision sits behind the activity, and the goroutine becomes a thin "is this row noisy?" filter.

### 13.2 ghinstallation token caching + refresh

GitHub App installation access tokens expire after 1 hour. `ghinstallation.New` returns an `http.RoundTripper` that auto-refreshes — we rely on that. Risk: if our minted token is cached past expiry by a buggy `http.Client` middleware, the PR-open call fails with 401. Mitigation: every `OpenPRUsecase.Run` call mints a fresh transport (cheap — JWT signing is sub-ms). Acceptance test 1 confirms PRs open across cold + warm code paths.

### 13.3 Neo4j cold-start in compose

The neo4j 5-community image takes ~30 s to be query-ready on a cold compose. The Sentinel goroutine waits for it via a 60-second startup probe (Neo4j Go driver's `VerifyConnectivity`). If the probe fails the goroutine logs WARN and disables the Pathfinder LLM-refine path for the rest of the process lifetime — the activity falls back to the canned causal output without graph evidence (degraded but functional). Documented in `internal/sentinel/detector.go`'s startup block.

### 13.4 Python DoWhy dependency footprint

`dowhy + pandas + numpy + statsmodels` is ~500 MB of wheels. The `services/causal-inference/Dockerfile` uses `python:3.12-slim` + a wheelhouse-prebuild stage to keep the runtime image under 800 MB. If a contributor's machine is space-constrained, `CAUSAL_ENABLED=0` disables the gRPC client and Pathfinder falls back to canned-from-fingerprint inline (no sidecar call). Documented in README.

### 13.5 Hypothesis flakiness budget

Property-based tests can find *real* counterexamples on the L1 backend's patch — sometimes correctly. A failing Hypothesis result blocks the approval. Phase 6 sets `max_examples=20` and a 10-second per-test deadline to keep walltime bounded, but the price is a small probability of a flaky pass. Mitigation: the demo fixtures' patches are deterministic (the L1 backend will produce the same patch every time on the seeded prompts), so the property tests are deterministic too. Production-grade fuzzing budgets are Phase 7.

### 13.6 Approval Gate signal race

Two humans could approve and reject within the same Temporal millisecond. The workflow's `WaitForSignal` resolves on the first signal received; the second arrives at a completed workflow and is rejected by the signal handler (HTTP layer checks `decision != 'pending'` before signalling). Race window is < 5 ms in practice — acceptable. Documented in `approval/service.go`.

### 13.7 Severity classifier file-path patterns

The patch-fingerprint classifier uses simple glob patterns (`*.sql`, `**/auth/**`). A new sensitive area (e.g. a future `**/payments/**` directory) would be wrongly classified `medium`. Mitigation: the patterns live in one const block in `severity.go`; reviewers extend it at code-review time. Pattern revision is a one-line PR.

### 13.8 Slack webhook URL leak in audit metadata

We must NOT write the webhook URL to `audit_log.metadata`. The Slack adapter writes only `{kind, http_status, channel_name}` — never the URL. Tested in `slack/provider_test.go` with a metadata-shape assertion. Documented in the adapter comment.

### 13.9 GitOps service has DB read access

The gitops service reads `integrations.installation_id` + `secret_ciphertext` directly from Postgres. This widens the secret blast radius: a compromise of the gitops container gains every org's GitHub App secret. Mitigation: gitops uses a **separate DB user** (`nexis_gitops`) with `SELECT` ONLY on `integrations` and `INSERT` on `audit_log`; configured via a Phase 6-introduced role grant in `0014_phase6_rls.up.sql`. Phase 7 swaps to a control-plane → gitops authenticated proxy so gitops never reads Postgres directly.

### 13.10 Argo Rollouts SLO templates are env-coupled

The `infra/argocd/rollouts.yaml` AnalysisTemplate references a Prometheus address. Phase 6 ships the yaml without a running Prometheus — the template would fail-open if applied as-is. Documented in `infra/argocd/README.md` Phase 7 prep checklist: a contributor must wire a Prometheus first, then enable the AnalysisTemplate. Phase 6 acceptance criterion 6 explicitly checks the docs, not the runtime.

### 13.11 Sentinel detector single-org concurrency

Phase 6's detector runs one goroutine total — concurrently fires across orgs, but a single tick is sequential per org. With N orgs each emitting 1 fatal/min, the worst-case backlog at a 10-s tick is `N` workflow starts per tick — fine until N > ~50. Phase 7 splits to a per-org goroutine pool if backlog becomes real.

### 13.12 ArgoCD docs are aspirational

`infra/argocd/*.yaml` is documented behaviour, not running behaviour. Risk: a developer assumes ArgoCD is "wired" and merges a PR expecting an auto-deploy. Mitigation: the `infra/argocd/README.md` begins with a **bold note** `THIS IS A PHASE 7 BLUEPRINT — NOTHING HERE RUNS YET IN COMPOSE`. The Live Demo UI also renders an explicit "ArgoCD will pick this up — Phase 7" footer line under the PR link.

### 13.13 Patch parser limitations

`parseUnifiedDiff` in the gitops service handles **single-hunk single-file** + **multi-file additive** diffs. It does NOT handle file renames, binary patches, mode changes, or unicode BOM diffs. The Phase 5 backend L1 agent prompt explicitly constrains the model to "additive or contiguous-hunk diffs only" — that constraint flows through to Phase 6 unchanged. A backend agent that returns a more exotic diff fails the PR-open with `400` and the workflow records `failed`. Phase 7 swaps to `go-git`'s patch applier for the full diff grammar.

### 13.14 Fixture repo is publicly readable

`github.com/nexis-eco/fixture-recovery-demo` is a public repo for the design-partner demo. The GitHub App's installation has write access to it. Risk: a malicious workflow could write garbage PRs. Mitigation: the demo CTA only accepts incident labels from the fixture catalog (`services/validator/fixtures/incidents/*.json`); unknown labels return 400. Phase 7 swaps the demo repo to a private clone per design-partner org.

---

## Appendix A: Env vars added

```
# control-plane
SENTINEL_ENABLED=1                              # 0 disables the detector goroutine
SENTINEL_POLL_INTERVAL_MS=10000                 # detector tick
NEO4J_URI=bolt://neo4j:7687
NEO4J_USER=neo4j
NEO4J_PASS=nexis_dev_password
CAUSAL_GRPC_ENDPOINT=causal-inference:8090
CAUSAL_ENABLED=1                                # 0 short-circuits to inline canned
PATHFINDER_LLM_REFINE=0                         # 1 calls the LLM after causal for a one-sentence hypothesis

GITOPS_URL=http://gitops:8082
GITOPS_TOKEN=dev-gitops-token-32byte
GITHUB_APP_ID=12345                             # fixture app for the demo
GITHUB_APP_PRIVATE_KEY_PATH=/run/secrets/github-app.pem
FIXTURE_REPO_OWNER=nexis-eco
FIXTURE_REPO_NAME=fixture-recovery-demo
FIXTURE_REPO_DEFAULT_BRANCH=main
FIXTURE_REPO_INSTALLATION_ID=98765              # seed-known; demo CTAs use this

SLACK_ENABLED=1
APPROVAL_MEDIUM_TIMEOUT_SECONDS=120             # 2-minute auto-approve race

SEED_NEO4J_ORG_ID=                              # consumed only by cmd/seed-neo4j; not read in server bootstrap

# gitops service
PORT=8082
DATABASE_URL=postgres://nexis_gitops:nexis_dev_password@postgres:5432/nexis
GITOPS_TOKEN=dev-gitops-token-32byte
GITHUB_APP_ID=12345
GITHUB_APP_PRIVATE_KEY_PATH=/run/secrets/github-app.pem
LOG_LEVEL=info

# validator (additions)
HYPOTHESIS_MAX_EXAMPLES=20
HYPOTHESIS_DEADLINE_MS=10000
HYPOTHESIS_SOCKET=/tmp/hypothesis.sock

# causal-inference
PORT=8090
PYTHONUNBUFFERED=1
```

## Appendix B: HTTP route map (full)

```
# Control-plane — new in Phase 6:
POST   /v1/workspaces/{ws}/pipelines/{run_id}/approve   [auth, RLS, owner|admin]
GET    /v1/workspaces/{ws}/pipelines/{run_id}/decision  [auth, RLS, any role]
GET    /v1/approvals                                    [auth, RLS, any role]
POST   /v1/admin/sentinel/trigger                       [auth, RLS, owner|admin]

# Control-plane — modified in Phase 6:
GET    /v1/workspaces/{ws}/pipelines/{run_id}           # adds approval_decision + pr_url
POST   /v1/integrations/slack/connect                   # widened from Phase 3 to include 'slack' provider

# gitops service — new in Phase 6:
POST   /v1/prs                                          [Bearer GITOPS_TOKEN]
GET    /healthz                                         [public]

# causal-inference — gRPC:
nexis.causal.v1.Causal/Infer                            [internal compose network only]
```

All Phase 4-5 routes survive unchanged.

## Appendix C: ApprovalDecision JSON shape

```json
{
  "id":              "uuid",
  "org_id":          "uuid",
  "workspace_id":    "uuid",
  "workflow_run_id": "uuid",
  "severity":        "medium",
  "decision":        "pending",
  "decided_by":      null,
  "decided_at":      null,
  "notes":           null,
  "scenario":        "null_deref",
  "risk_score":      50.0,
  "created_at":      "2026-05-13T14:22:01Z"
}
```

Terminal states fill `decided_by`, `decided_at`, and (optionally) `notes`.

## Appendix D: PROpenRequest / PROpenResponse JSON shapes

(see §10.1 for the wire bodies)

## Appendix E: Audit metadata shapes

```json
// incident.sentinel_triggered
{ "incident_id": "...", "org_id": "...", "rule": "fatal_level", "detected_at": "..." }

// pathfinder.diagnosis
{ "run_id": "...", "root_cause_node": "safe_div", "confidence": 0.82, "evidence_count": 4 }

// synthesiser.plan
{ "run_id": "...", "scenario": "null_deref", "selected_agents": ["architect","backend","qa"], "rationale": "..." }

// approval.requested
{ "run_id": "...", "severity": "medium", "scenario": "null_deref", "risk_score": 50 }

// approval.decided
{ "run_id": "...", "decision": "approved", "decided_by": "user-uuid", "severity": "medium",
  "elapsed_ms": 47213, "auto": false }

// gitops.pr_opened
{ "run_id": "...", "repo": "nexis-eco/fixture-recovery-demo", "pr_number": 47,
  "pr_url": "https://github.com/nexis-eco/fixture-recovery-demo/pull/47", "branch": "nexis/fix-ab12cd34" }

// slack.notification_sent
{ "run_id": "...", "kind": "approval_requested", "http_status": 200, "channel": "#engineering-alerts" }
```

## Appendix F: Fixture incident catalog (Phase 6 demo)

```
services/validator/fixtures/incidents/
  fixture-null-pointer.json          # routes → null_deref; severity → medium
  fixture-schema-drift.json          # routes → schema_drift; severity → high
  fixture-oom.json                   # routes → oom; severity → medium
  fixture-trivial-ui-fix.json        # routes → null_deref (overridden by patch-size rule); severity → low
```

Each fixture is a real `IncidentPayload` (title, service, environment, stacktrace, logs). The Live Demo dropdown lists the four labels with their predicted severity badges.

## Appendix G: Phase 4 → Phase 5 → Phase 6 evolution

| Phase 4 stub | Phase 5 (L1 real) | Phase 6 (L2 real) |
|---|---|---|
| `Sentinel.Detect` | unchanged stub | inline ack — heavy lifting moved to `internal/sentinel/` goroutine |
| `Pathfinder.Diagnose` | unchanged stub | Neo4j codegraph + DoWhy sidecar |
| `Synthesiser.Plan` | unchanged stub | scenario classification + L1 agent routing table |
| `Architect.Solution` | LLM-driven plan | unchanged from Phase 5; ordering may be skipped per Synthesiser plan |
| `Backend.Codegen` | LLM patch + validator | unchanged from Phase 5; Validator L2 step runs after |
| `QA.TestGen` | LLM tests | unchanged from Phase 5 |
| `DevOps.Pipeline` | LLM CI yaml | unchanged from Phase 5 |
| `DataEngineer.Migrations` | LLM SQL migrations | unchanged from Phase 5 |
| `ValidatorL2` (new) | — | calls services/validator with hypothesis=true |
| `ApprovalGate.Route` | unchanged stub | severity classifier + Temporal signal + auto-timeout |
| `GitOpsOpenPR` (new) | — | services/gitops PR open |

Phase 7 wires runtime ArgoCD, real OTel metric anomaly detection, real DoWhy training, real Hypothesis budgets, real Slack interactivity, and per-org separate GitHub App installations.
