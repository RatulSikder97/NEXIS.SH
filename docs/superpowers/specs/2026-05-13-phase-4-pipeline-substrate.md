# Phase 4 — Pipeline Substrate: Temporal + Sandbox — Design

**Date:** 2026-05-13
**Phase:** 4 (Weeks 10–12 per `docs/PROJECT_PLAN.md`).
**Dependencies:** Phase 3.5 (workspaces + billing, completed 2026-05-13).
**External services:** all local — Temporal dev server + MinIO + a local `docker run` validator sandbox. No cloud calls. Phase 7 swaps Temporal → Temporal Cloud, MinIO → S3, sandbox → Modal.

---

## 1. Goals

1. **Temporal worker scaffolding** inside the control-plane process. One worker connects to the `temporal:7233` dev server on a `nexis-recovery` task queue, registers the `RecoveryPipeline` workflow + its 9 activities, and shuts down cleanly on SIGTERM.
2. **`RecoveryPipeline` workflow** — 9-step DAG mirroring the agent fleet (Sentinel → Pathfinder → Synthesiser → Architect → Backend → QA → DevOps → DataEngineer → ApprovalGate). Phase 4 ships only stub activities (log + sleep 1–2 s + return success), but the DAG, retry policies, activity timeouts, and parallelism boundaries are the real ones — Phases 5 + 6 swap stub bodies, not the orchestration.
3. **Validator sandbox as `docker run`** — `services/validator` exposes `POST /v1/validate` accepting `{repo_sha, patch_diff, image}` and spawns `docker run --rm --network=none --read-only --tmpfs /tmp <image> pytest`, capturing stdout/stderr/exit code into `{tests_passed, coverage, logs}`. Same wire interface as the Phase 7 Modal adapter so swapping is one file.
4. **Patch storage — MinIO bucket per org** — patches and sandbox logs land in `nexis-org-<org_id>` buckets. Bodies are envelope-encrypted via the existing `domain.KeyVault` port + `keyvault.LocalKeyVault` adapter from Phase 3 (the same component that seals integration secrets). Phase 7 KMS swap requires zero call-site changes.
5. **Pipeline timeline UI** — replace the Phase 3 `incidents` placeholder with two surfaces: a list of recent workflow runs (table) and a per-run timeline page that streams activity events via SSE, lighting up the 9 steps as they progress. The "Run synthetic incident" CTA on Live Demo triggers a `RecoveryPipeline` start using a fixture incident.
6. **Workspace scoping** — pipelines belong to a workspace. SSE URLs and IDs are `workspace_id`-scoped: `/v1/workspaces/{ws}/pipelines` and `/v1/workspaces/{ws}/pipelines/{run}/events`. RLS continues to gate on `org_id`; workspace membership is verified before the SSE upgrade.

## 2. Non-goals

- **Real agent logic.** All 9 activities are stubs in Phase 4. Phase 5 fills L1 (Architect/Backend/QA/DevOps/DataEngineer); Phase 6 fills L2 (Sentinel/Pathfinder/Synthesiser/ApprovalGate).
- **Real LLM calls.** The activities don't import the `llm.Provider` interface yet — Phase 5 wires it in.
- **GitHub PR opening / GitOps.** The DevOps + ApprovalGate stubs emit log lines only. `services/gitops/` remains a `/healthz` skeleton.
- **Sentry → workflow auto-trigger.** Phase 4 triggers workflows manually from the console or via the synthetic-incident CTA. Streaming subscription on `incidents_raw` lands in Phase 6 (Sentinel).
- **Hypothesis property-based tests.** The sandbox runs vanilla `pytest` on a fixture repo. Phase 6 swaps Hypothesis in.
- **Argo Rollouts / preview env.** Out of scope until Phase 6/7.
- **Per-workspace RLS.** Pipelines live in tables that still RLS on `org_id`. A `workspace_id` column on the same row lets handlers verify workspace ownership; we don't introduce a second GUC.
- **Cross-org workflow visibility (multi-tenant Temporal namespace split).** Phase 4 uses a single shared `default` Temporal namespace; isolation is enforced by `(org_id, workspace_id, run_id)` lookups before any signal/query. Per-namespace split lands in Phase 7.

## 3. Architecture

Same port/adapter pattern as Phases 1–3.5. The `.arch.yaml` already names a `workflow` component allowed to depend on `usecase + domain + adapter + platform` — Phase 4 populates it for the first time.

### 3.1 control-plane Go layout

```
internal/
  domain/
    workflow.go              # WorkflowRun, ActivityEvent, ActivityStatus, AgentRole types + ports
    patchstore.go            # PatchStore port (PutObject/GetObject/SignURL)
  adapter/
    workflow/
      service.go             # WorkflowService impl — Start/Get/List/Subscribe; bridges Temporal + DB + SSE
      service_test.go
    patchstore/
      factory.go              # picks minio | s3 by PATCH_STORE env
      minio/
        store.go             # MinIO PutObject/GetObject + per-org bucket bootstrap
        store_test.go
      s3/                    # Phase 7 — empty stub returning ErrNotImplemented
        store.go
    repo/
      workflow_repo.go       # workflow_runs + activity_events repos
  platform/
    temporal/
      client.go              # connects to TEMPORAL_HOST_PORT, returns *temporal.Client
      worker.go              # registers workflows + activities on the task queue, runs until ctx done
  workflow/
    pipeline.go              # RecoveryPipeline workflow function — the 9-step DAG
    activities.go            # 9 stub activities — one Go func per agent role
    types.go                 # PipelineInput, PipelineOutput, ActivityResult — shared by activities
    pipeline_test.go         # uses temporaltest.NewTestWorkflowEnvironment for unit tests
  transport/http/
    handler/
      pipelines.go           # list / get / trigger / SSE events
  usecase/
    pipeline_demo.go         # builds the synthetic-incident PipelineInput consumed by /live-demo CTA
```

The `workflow` package is the only place that imports `go.temporal.io/sdk/workflow` + `…/activity`; everything outside it sees `domain.WorkflowService` only. This keeps the rest of the codebase Temporal-free for Phase 7 namespace cutover and for in-memory unit tests.

### 3.2 Web (Next.js 16) layout

```
apps/web/
  app/
    (app)/
      console/
        incidents/
          page.tsx               # REWRITE — server fetches /v1/workspaces/{ws}/pipelines
          [id]/
            page.tsx             # NEW — timeline shell (server component, hydrates id)
            client.tsx           # NEW — SSE consumer + animated 9-step timeline
        live-demo/
          page.tsx               # REWRITE — "Run synthetic incident" CTA + recent runs list
          client.tsx              # NEW — trigger button + redirect to incidents/[id]
  components/
    pipelines/
      PipelineRunsTable.tsx       # NEW — table with id, started_at, status, current_step, duration
      ActivityTimeline.tsx        # NEW — vertical 9-step list; current pulses; done = check; future = grey
      StatusPill.tsx              # NEW — { running | succeeded | failed | timed_out } pill
      AgentIcon.tsx               # NEW — emoji/lucide icon per AgentRole
  lib/
    pipelines.ts                  # NEW — SDK: list, get, trigger, eventsSSE
```

## 4. Database

### 4.1 New tables

```
workflow_runs
  id              uuid PK
  org_id          uuid NOT NULL REFERENCES organizations(id)
  workspace_id    uuid NOT NULL REFERENCES workspaces(id)
  workflow_type   text NOT NULL                   -- 'RecoveryPipeline' (extensible)
  temporal_run_id text NOT NULL                   -- Temporal's run id (returned by StartWorkflow)
  temporal_wf_id  text NOT NULL                   -- caller-supplied workflow id; we use workflow_runs.id
  status          text NOT NULL                   -- 'queued'|'running'|'succeeded'|'failed'|'timed_out'|'cancelled'
  current_step    text                            -- last activity name observed (advisory; truth is activity_events)
  input           jsonb                           -- PipelineInput body
  output          jsonb                           -- PipelineOutput on success
  error           text                            -- error message on failure
  started_at      timestamptz NOT NULL DEFAULT now()
  completed_at    timestamptz
  duration_ms     bigint                          -- denormalized on completion
  created_by      uuid REFERENCES users(id)       -- the principal who triggered it
  UNIQUE (org_id, temporal_run_id)

activity_events
  id              uuid PK
  org_id          uuid NOT NULL REFERENCES organizations(id)
  workflow_run_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE
  seq             int NOT NULL                    -- monotonically increasing per run
  agent_role      text NOT NULL                   -- 'sentinel'|'pathfinder'|...|'approval_gate'
  activity_name   text NOT NULL                   -- 'Sentinel.Detect' etc.
  status          text NOT NULL                   -- 'started'|'succeeded'|'failed'|'retrying'|'timed_out'
  attempt         int NOT NULL DEFAULT 1
  message         text
  payload         jsonb                           -- activity output preview (small JSON, no large artifacts)
  ts              timestamptz NOT NULL DEFAULT now()
  UNIQUE (workflow_run_id, seq)
```

Both tables RLS-protected on `org_id` with the standard `tenant_isolation` policy using `NULLIF(current_setting('app.current_org_id', true), '')::uuid`. `GRANT SELECT, INSERT, UPDATE, DELETE` to `nexis_app` for both.

Indexes:
- `workflow_runs (org_id, workspace_id, started_at DESC)` — drives the list view.
- `activity_events (workflow_run_id, seq)` — primary read pattern is "give me all events for this run in order".

### 4.2 Modifications

None. No new columns on existing tables.

## 5. Bucket / object storage layout

Per §3.5 of `docs/PROJECT_PLAN.md` ("MinIO bucket per tenant"):

```
nexis-org-<org_id>/
  patches/<workflow_run_id>/<activity_name>.patch.enc
  reports/<workflow_run_id>/<activity_name>.report.json.enc
  sandbox/<workflow_run_id>/<seq>/logs.txt.enc
```

- One bucket per org, created lazily on first write (`MakeBucket` is idempotent).
- Every object body is **envelope-encrypted** before `PutObject`: a fresh 32-byte AES-256-GCM data key encrypts the body; the data key itself is sealed via `domain.KeyVault.Encrypt(...)` and prepended to the ciphertext as a length-prefixed header:

  ```
  [4 bytes: be uint32 wrapped_key_len][wrapped_key][12 bytes nonce][gcm ciphertext+tag]
  ```

- `Get` reverses that: read header → `KeyVault.Decrypt(wrapped_key)` → AES-GCM open → return plaintext.
- Phase 7 swaps `keyvault.LocalKeyVault` → `keyvault.KMSVault` and `patchstore/minio` → `patchstore/s3`; the wire format is byte-identical because both use the KeyVault port.

This is exactly the §3.5 pattern from `docs/PROJECT_PLAN.md` — local dev = MinIO + LocalKeyVault, Phase 7 = S3 + KMS, same envelope structure.

## 6. Temporal workflow design

### 6.1 Identifiers

- **Task queue:** `nexis-recovery`. One worker pool inside the control-plane process; future scale-out splits to dedicated worker pods.
- **Workflow id:** the UUID we generate for `workflow_runs.id` — gives us a one-to-one mapping between Temporal-side state and our row, and makes the SSE URL `/v1/workspaces/{ws}/pipelines/{id}/events` natural.
- **Run id:** Temporal-assigned; stored in `workflow_runs.temporal_run_id` for signal/query.
- **Namespace:** `default` (dev server preset). Phase 7 cuts over to `nexis-prod` on Temporal Cloud.

### 6.2 The 9-activity DAG

| # | AgentRole | Activity name | Stub behavior |
|---|---|---|---|
| 1 | `sentinel` | `Sentinel.Detect` | sleep 1.0 s |
| 2 | `pathfinder` | `Pathfinder.Diagnose` | sleep 1.5 s |
| 3 | `synthesiser` | `Synthesiser.Plan` | sleep 1.5 s |
| 4 | `architect` | `Architect.Solution` | sleep 1.0 s |
| 5 | `backend` | `Backend.Codegen` | sleep 2.0 s |
| 6 | `qa` | `QA.TestGen` | sleep 1.0 s |
| 7 | `devops` | `DevOps.Pipeline` | sleep 1.0 s |
| 8 | `data_engineer` | `DataEngineer.Migrations` | sleep 1.0 s |
| 9 | `approval_gate` | `ApprovalGate.Route` | sleep 0.5 s |

**Ordering:**

- Steps 1 → 2 → 3 run **sequentially** (each consumes the previous output).
- Steps 4 → 5 → 6 run **sequentially** (Architect produces a plan, Backend consumes it, QA consumes the patch).
- Steps 7 + 8 run **in parallel** after step 6 (DevOps generates CI yaml, DataEngineer drafts migrations — independent of each other, both depend only on the patch from step 5).
- Step 9 runs after both 7 + 8 complete (the gate routes based on severity + patch fingerprint).

Total walltime budget with stubs: ~6 s (sleep 1 + 1.5 + 1.5 + 1 + 2 + 1 + max(1, 1) + 0.5 + barrier overhead) — comfortably under the 60-second acceptance criterion.

### 6.3 Retry policies + timeouts

Every activity uses these `ActivityOptions`:

```go
StartToCloseTimeout:    30 * time.Second,  // wallclock for one attempt
ScheduleToCloseTimeout: 2  * time.Minute,  // wallclock across retries
HeartbeatTimeout:       10 * time.Second,  // unused by stubs; real activities heartbeat
RetryPolicy: &temporal.RetryPolicy{
    InitialInterval:    1 * time.Second,
    BackoffCoefficient: 2.0,
    MaximumInterval:    30 * time.Second,
    MaximumAttempts:    3,
    NonRetryableErrorTypes: []string{"ValidationError", "ForbiddenError"},
},
```

`Backend.Codegen` and `QA.TestGen` override `StartToCloseTimeout` to 5 minutes (Phase 5 LLM calls); Phase 4 stubs don't need it but we set it now so the DAG is final.

Workflow-level options on `StartWorkflow`:

```go
WorkflowExecutionTimeout: 10 * time.Minute,
WorkflowRunTimeout:        10 * time.Minute,
WorkflowTaskTimeout:        10 * time.Second,
```

### 6.4 Activity → DB event recording

Activities don't talk to Postgres directly — that would couple the workflow package to the repo layer and break our `.arch.yaml` (workflow can't depend on transport, and we keep DB writes off the activity hot path for Phase 5+6 when activities become LLM-bound). Instead:

1. Each activity returns an `ActivityResult{ AgentRole, Status, Message, Payload }` value.
2. The workflow function records the result by calling a side-effect activity `RecordActivityEvent(ctx, evt)` that the workflow service registers — this *does* hit the repo through a small adapter passed at worker registration. Using a Temporal activity (rather than a direct DB call from workflow code) preserves replay safety.
3. Status transitions (`started`, `succeeded`, `failed`, `retrying`) are bracketed around each user-level activity call inside the workflow function.

The same `RecordActivityEvent` activity publishes the event to the in-process SSE broker keyed on `workflow_run_id`, so the timeline UI sees the same row that hits Postgres.

### 6.5 Workflow startup + signal surface

Phase 4 ships **trigger** but not signals — the workflow runs to completion without external input. The handler signature stays open for Phase 6:

- `WorkflowService.Start(ctx, p, workspaceID, input)` → inserts `workflow_runs` row → `StartWorkflow` on Temporal → returns the run.
- `WorkflowService.Get(ctx, p, runID)` → reads the row + last 50 events.
- `WorkflowService.List(ctx, p, workspaceID, limit, beforeTS)` → paginated list.
- `WorkflowService.Subscribe(ctx, runID) <-chan ActivityEvent` → SSE feed.

Phase 6 will add `Approve(ctx, p, runID, decision)` and a corresponding workflow signal.

## 7. Validator sandbox

### 7.1 `services/validator` runtime

The validator is a tiny Go HTTP server (one binary, distroless container in compose, port 8081). It exposes:

```
POST /v1/validate
Content-Type: application/json
Authorization: Bearer <VALIDATOR_TOKEN>

Request:
{
  "repo_sha":   "abc1234",                  // fixture repo identifier (Phase 4 uses a baked fixture)
  "patch_diff": "diff --git ...",           // unified diff body
  "image":      "nexis/validator-fixture:latest",  // optional override; defaults to NEXIS_VALIDATOR_IMAGE
  "timeout_ms": 60000                       // optional
}

Response (200):
{
  "tests_passed": true,
  "test_count":   12,
  "fail_count":    0,
  "coverage":      0.84,
  "duration_ms":  4120,
  "logs":         "...pytest stdout..."
}
```

Errors return `{"error": "...", "logs": "..."}` with status 422 (sandbox ran but pytest failed) vs 500 (couldn't even start the container).

### 7.2 Sandbox invocation

For each `POST /v1/validate`, the validator:

1. Creates a temp dir `/tmp/nexis-validate-<run-id>` and writes `patch.diff` into it.
2. Pulls (or assumes pre-pulled) the `image` parameter — defaults to `nexis/validator-fixture:latest` baked from `services/validator/fixtures/` in Phase 4.
3. Runs:

   ```
   docker run \
     --rm \
     --network=none \
     --read-only \
     --tmpfs /tmp:rw,nosuid,size=64m \
     --tmpfs /workspace:rw,nosuid,size=128m \
     --memory=512m \
     --cpus=1.0 \
     --pids-limit=128 \
     --security-opt=no-new-privileges \
     --cap-drop=ALL \
     -v "/tmp/nexis-validate-<run-id>/patch.diff:/workspace/patch.diff:ro" \
     -e CI=true \
     --label nexis.validator.run-id=<run-id> \
     nexis/validator-fixture:latest \
     /bin/sh -c "cd /workspace && git apply patch.diff && pytest --json-report --json-report-file=/tmp/report.json && cat /tmp/report.json"
   ```

4. Captures `stdout` (the JSON pytest report) + `stderr` + exit code; deletes the temp dir; returns the JSON response above.
5. `--rm` + the lack of any persistent volume mount means the container is gone the instant the call returns. The validator host process never writes anything outside `/tmp/nexis-validate-<run-id>` (which it deletes itself).

The validator binary mounts `/var/run/docker.sock` (Docker-out-of-Docker) so it can spawn sibling containers. **This is acceptable for the local-dev story only**; the Phase 7 Modal swap replaces this entirely with a remote sandbox API, so we never ship docker-socket exposure to production.

### 7.3 Fixture repo

`services/validator/fixtures/` contains a tiny Python repo:

```
fixtures/
  Dockerfile               # builds nexis/validator-fixture:latest — python:3.12-slim + pytest + git
  src/
    nexis_fixture/
      __init__.py
      api.py               # 3 trivial functions
  tests/
    test_api.py            # 12 unit tests
  pyproject.toml
  pytest.ini
```

The fixture is intentionally minimal so a stub patch (or no patch) passes all tests; this is enough to exercise the validator round-trip from Temporal in Phase 4. Phase 6 swaps in larger fixtures + Hypothesis property tests.

### 7.4 Control-plane → validator wiring

The `Backend.Codegen` activity stub in Phase 4 calls the validator as a smoke test: it posts a no-op patch (`""`) against the fixture image and includes the JSON response in its `ActivityResult.Payload`. Phase 5 replaces the empty patch with a real LLM-produced diff.

The validator URL is `VALIDATOR_URL` env var on the control-plane (`http://validator:8081` in compose). Auth: shared bearer `VALIDATOR_TOKEN` (random 32-byte hex). Phase 7 swaps to mTLS + the Modal API.

## 8. Patch storage

### 8.1 Envelope encryption flow

`patchstore.Store` port:

```go
type PutOptions struct {
    OrgID, Bucket, Key string  // bucket = "nexis-org-<orgID>"; key = "patches/<run>/Backend.Codegen.patch.enc"
    Body               []byte
    ContentType        string  // typically "application/octet-stream"
}

type PatchStore interface {
    Put(ctx context.Context, opts PutOptions) error
    Get(ctx context.Context, bucket, key string) ([]byte, error)
    EnsureBucket(ctx context.Context, name string) error
    SignURL(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
}
```

`Put` flow:

```
plaintext --AES-256-GCM(data_key)--> ciphertext
data_key  --KeyVault.Encrypt-------> wrapped_key
[uint32 BE wrapped_key_len][wrapped_key][12-byte nonce][ciphertext+tag] --PutObject--> MinIO
```

`Get` flow is the inverse. The data key is single-use per object — a Put never reuses one. Lookup speed comes from the bucket/key index in MinIO; we do not maintain a separate KMS-call index.

### 8.2 Bucket naming + bootstrap

- Bucket per org: `nexis-org-<lower(uuid)>`.
- The first `Put` for an org calls `EnsureBucket` which is `MakeBucket(name)` wrapped in "ignore BucketAlreadyOwnedByYou".
- A second-level path prefix lets us list-by-run cheaply without growing the bucket list.
- Phase 7 S3 swap: same naming scheme — but with a Terraform-managed bucket policy denying cross-org reads even on misconfigured IAM.

### 8.3 What gets stored

For each workflow run:

| Object | Producer | Notes |
|---|---|---|
| `patches/<run>/Backend.Codegen.patch.enc` | activity 5 | the unified diff produced by the codegen stub (empty in Phase 4) |
| `reports/<run>/Backend.Codegen.report.json.enc` | activity 5 | the validator JSON response |
| `sandbox/<run>/<seq>/logs.txt.enc` | activity 5 | the validator stdout/stderr capture |
| `reports/<run>/QA.TestGen.report.json.enc` | activity 6 | QA stub output |

Activities 1–4 and 7–9 don't write objects in Phase 4. Phases 5 + 6 fill the rest in.

## 9. HTTP surface

All routes under `/v1/workspaces/{ws_id}/...` inherit the auth + RLS middleware and require workspace ownership (handler verifies `(princ.OrgID, ws_id)` against `workspaces` before any read/write).

```
GET    /v1/workspaces/{ws_id}/pipelines                      → 200 [WorkflowRun]
       query: limit (default 50, max 200), before (ISO8601), status (filter)
POST   /v1/workspaces/{ws_id}/pipelines                      → 202 WorkflowRun
       body: { "workflow_type": "RecoveryPipeline", "input": {...} }
GET    /v1/workspaces/{ws_id}/pipelines/{run_id}             → 200 WorkflowRunDetail (run + last 200 events)
GET    /v1/workspaces/{ws_id}/pipelines/{run_id}/events      → SSE text/event-stream of ActivityEvent
POST   /v1/workspaces/{ws_id}/pipelines/demo                 → 202 WorkflowRun (dev-only synthetic-incident shortcut)
```

### 9.1 RBAC

| Endpoint | owner | admin | member |
|---|---|---|---|
| `GET    /v1/workspaces/{ws}/pipelines` | ✓ | ✓ | ✓ |
| `GET    /v1/workspaces/{ws}/pipelines/{id}` | ✓ | ✓ | ✓ |
| `GET    /v1/workspaces/{ws}/pipelines/{id}/events` | ✓ | ✓ | ✓ |
| `POST   /v1/workspaces/{ws}/pipelines` | ✓ | ✓ | ✗ |
| `POST   /v1/workspaces/{ws}/pipelines/demo` | ✓ | ✓ | ✗ |

All routes audit-log on write (`pipelines.create`, `pipelines.demo`). The SSE handler audits `pipelines.subscribe` on stream open.

## 10. SSE event shape

```json
{
  "workflow_run_id": "uuid",
  "seq":             7,
  "agent_role":      "backend",
  "activity_name":   "Backend.Codegen",
  "status":          "succeeded",
  "attempt":         1,
  "message":         "patch generated; 0 lines changed",
  "payload":         { "tests_passed": true, "coverage": 0.84 },
  "ts":              "2026-05-13T12:34:56Z"
}
```

The SSE handler emits one event per `activity_events` row. On connect it replays all rows for the run (sorted by `seq`) before tailing the broker — so a refresh shows the existing timeline state instantly. A `pipeline.terminal` event is emitted when `workflow_runs.status` lands in a non-running state, letting the UI close the EventSource.

## 11. Acceptance criteria

1. **Worker boots clean.** `docker compose up control-plane` produces log lines `temporal worker registered` (workflow + 10 activities — 9 stubs + `RecordActivityEvent`). Restarting Temporal does not kill the control-plane (the worker reconnects with exponential backoff).
2. **`RecoveryPipeline` completes under 60 s with stubs.** Manually:
   ```
   docker compose exec control-plane /app/server  # already running
   temporal --address localhost:7233 workflow start \
     --task-queue nexis-recovery --type RecoveryPipeline \
     --workflow-id manual-$(date +%s) \
     --input '{"workspace_id":"<uuid>","incident_id":"manual","org_id":"<uuid>"}'
   temporal --address localhost:7233 workflow show --workflow-id manual-...
   ```
   exits with `WORKFLOW_EXECUTION_COMPLETED` in under 60 seconds.
3. **9 activity rows recorded.** `SELECT seq, activity_name, status FROM activity_events WHERE workflow_run_id=$1 ORDER BY seq` returns all 9 in order, all `succeeded`, plus the workflow-level `started`/`succeeded` bookends.
4. **MinIO bucket present.** `mc ls minio/nexis-org-<orgid>/patches/<runid>/` lists at least `Backend.Codegen.patch.enc` and a sandbox report. `mc cat` on a raw object is **not** plaintext (envelope header visible).
5. **Sandbox round-trip.** `POST /v1/validate` with the empty patch + fixture image returns `{"tests_passed": true, "test_count": 12}` and the spawned container is gone (`docker ps -a --filter label=nexis.validator.run-id=...` empty) within 1 second of the response.
6. **Console list page.** `GET /console/incidents` after triggering a run shows a row with status `succeeded`, current_step `ApprovalGate.Route`, duration < 60 s.
7. **Timeline SSE.** Opening `/console/incidents/<id>` while a run is in flight shows all 9 steps lighting up in order: future = grey, current = pulsing, done = green check. Refreshing mid-run renders the correct cumulative state (replay-from-DB on connect).
8. **RBAC.** A `member`-role user gets 403 on `POST /v1/workspaces/{ws}/pipelines`.
9. **RLS.** A user in org A cannot read/list/SSE workflow runs in org B — repeat the verification curl from Phase 3 with two principals.
10. **`make build && make test && make vet && make arch` green** on the control-plane; `pnpm typecheck && pnpm build` green on web; `cd services/validator && go build && go test ./...` green.

## 12. Stage breakdown

| Stage | Title |
|---|---|
| 0 | Schema (2 tables + RLS) + config + env + arch.yaml hooks |
| 1 | Domain + repos: `workflow.go`, `patchstore.go`, `workflow_repo.go` |
| 2 | Temporal platform: `platform/temporal/{client,worker}.go` + worker boot from `main.go` |
| 3 | Workflow package: `RecoveryPipeline` + 9 stub activities + `RecordActivityEvent` activity |
| 4 | `WorkflowService` adapter + SSE broker wiring + HTTP routes |
| 5 | Patch storage: MinIO adapter + envelope encryption + `Put`/`Get` round-trip tests |
| 6 | Validator service: `POST /v1/validate` + Docker spawn + fixture image |
| 7 | Web: Incidents list + detail timeline + Live Demo CTA |
| 8 | E2E + DoD + mark Phase 4 complete |

## 13. Risks / open items

- **Docker-out-of-Docker for the validator.** Mounting `/var/run/docker.sock` into the validator container is a fat security footgun. We accept it for local dev because every alternative (Sysbox, gVisor, rootless DinD) adds setup pain. The risk is bounded: the validator container only ever spawns ephemeral `--rm --network=none --read-only` children, the socket is not exposed outside the compose network, and Phase 7 replaces this entire path with a remote Modal call. Documented as a "DO NOT DEPLOY THIS COMPOSE TO PROD" line at the top of `services/validator/Dockerfile`.
- **Temporal dev-server restart races.** The `temporalio/auto-setup` image re-creates schemas on cold start; if the control-plane worker connects before Temporal is ready it loops with backoff. We rely on the compose `depends_on: condition: service_healthy` and a 60-second client-side retry budget. Watched in Stage 2 verification.
- **SSE replay correctness.** Subscribers that connect mid-run must replay from `activity_events` before tailing the broker — otherwise they miss the steps that already completed. Stage 4 ships a small replay-then-tail loop in the handler; race condition risk: a step completes between the replay query and the broker subscribe, and the event is double-emitted (replay + live). We accept that — the client is idempotent on `(run_id, seq)` and dedups.
- **`workflow_runs.temporal_run_id` ambiguity.** Temporal `WorkflowID` is caller-supplied; `RunID` is server-assigned and changes on retry/reset. We store both. Lookups in the SSE handler use our local `workflow_runs.id` (= WorkflowID) and only ever talk to Temporal via that. We never join on RunID.
- **MinIO bucket explosion.** One bucket per org is the spec but it scales linearly with tenants — fine for 100 design-partner tenants, ugly at 10k. Phase 7 S3 cutover keeps the same naming because S3 has no equivalent bucket-count limit per account up to 1000, and goes well above with quota increase. Documented for future revisit.
- **Envelope header drift between phases.** The 4-byte length-prefix wire format is the public ABI of patchstore objects. Any change breaks Phase 5+6 patches stored mid-phase. Lock it in a versioned magic byte: `0x4E 0x58 0x01 <reserved>` then the existing layout — Phase 4 stamps version `1`, decoder rejects unknown versions cleanly.
- **`Backend.Codegen` activity timeout.** 5-minute `StartToCloseTimeout` is set now anticipating Phase 5 LLM calls. The Phase 4 stub completes in ~2 s. Catching dev-time hangs (`Backend.Codegen` stuck on a busted validator) takes 5 minutes by default. We tolerate this; alternative is a feature flag we'd just remove later.
- **No retry-storm protection on validator.** Phase 4 has zero rate-limiting between the worker and the validator. With `MaximumAttempts: 3` and the workflow's parallel branch (steps 7+8), the worst case is 6 concurrent validator calls per run. Acceptable at one run at a time in dev; revisit in Phase 6 once concurrency goes up.
- **Member-role can see SSE.** RBAC table allows members to subscribe to event streams (read-only). If we want to lock this down to admin+ later it's a one-line `RequireRole` change.

---

## Appendix A: Env vars added

```
# control-plane
TEMPORAL_HOST_PORT=temporal:7233            # dev-server address
TEMPORAL_NAMESPACE=default                  # Phase 7 → nexis-prod
TEMPORAL_TASK_QUEUE=nexis-recovery          # worker pool id
VALIDATOR_URL=http://validator:8081         # validator service
VALIDATOR_TOKEN=dev-validator-token-32byte  # shared bearer
PATCH_STORE=minio                           # minio | s3 (Phase 7)
MINIO_ENDPOINT=minio:9000
MINIO_ACCESS_KEY=nexis
MINIO_SECRET_KEY=nexis_dev_password
MINIO_USE_SSL=0

# validator
PORT=8081
VALIDATOR_TOKEN=dev-validator-token-32byte
NEXIS_VALIDATOR_IMAGE=nexis/validator-fixture:latest
DOCKER_HOST=unix:///var/run/docker.sock     # explicit; matches the compose mount

# web
# (none — uses NEXT_PUBLIC_API_URL via SDK)
```

## Appendix B: HTTP route map (full)

```
GET    /v1/workspaces/{ws_id}/pipelines                      [auth, RLS, any role]
POST   /v1/workspaces/{ws_id}/pipelines                      [auth, RLS, owner|admin]
GET    /v1/workspaces/{ws_id}/pipelines/{run_id}             [auth, RLS, any role]
GET    /v1/workspaces/{ws_id}/pipelines/{run_id}/events      [auth, RLS, any role]
POST   /v1/workspaces/{ws_id}/pipelines/demo                 [auth, RLS, owner|admin, dev-only when APP_ENV=dev]
```

Workspace-ownership verification is the first step in every handler:

```go
if ok, _ := deps.WorkspacesRepo.OwnsWorkspace(ctx, princ.OrgID, wsID); !ok {
    httpJSON(w, http.StatusNotFound, map[string]string{"error":"workspace not found"})
    return
}
```

## Appendix C: ActivityEvent JSON shape (SSE wire)

```json
{
  "workflow_run_id": "8c4f8d9a-...",
  "seq":             3,
  "agent_role":      "synthesiser",
  "activity_name":   "Synthesiser.Plan",
  "status":          "succeeded",
  "attempt":         1,
  "message":         "stub completed",
  "payload":         null,
  "ts":              "2026-05-13T12:34:56.123Z"
}
```

A terminal event arrives with `agent_role: "pipeline"` and `status` ∈ `{succeeded, failed, timed_out, cancelled}`:

```json
{
  "workflow_run_id": "8c4f8d9a-...",
  "seq":             100,
  "agent_role":      "pipeline",
  "activity_name":   "Pipeline.Complete",
  "status":          "succeeded",
  "attempt":         1,
  "message":         "duration_ms=5821",
  "payload":         { "duration_ms": 5821 },
  "ts":              "2026-05-13T12:35:02.001Z"
}
```

The SSE handler emits one final `event: close` line after the terminal event so the client closes EventSource cleanly.

## Appendix D: AgentRole / activity catalog (Phase 4 → Phase 6 evolution)

| Phase 4 stub | Phase 5 (L1) | Phase 6 (L2) |
|---|---|---|
| `Sentinel.Detect` | — | streaming anomaly detector on `incidents_raw` |
| `Pathfinder.Diagnose` | — | Neo4j codegraph + DoWhy causal inference |
| `Synthesiser.Plan` | — | orchestrates retrieval + delegates Backend/DataEngineer |
| `Architect.Solution` | LLM-driven plan synthesis | (refined) |
| `Backend.Codegen` | LLM patch generation + validator round-trip | (refined) |
| `QA.TestGen` | LLM test generation | + Hypothesis property-based tests |
| `DevOps.Pipeline` | LLM-generated CI yaml | + ArgoCD app-of-apps generation |
| `DataEngineer.Migrations` | LLM-drafted SQL migrations | (refined) |
| `ApprovalGate.Route` | — | severity router + Slack/email notify |

Phase 4 ships only the orchestration. Stubs share a single helper:

```go
func stubSleep(ctx context.Context, role domain.AgentRole, d time.Duration) (domain.ActivityResult, error) {
    select {
    case <-time.After(d):
    case <-ctx.Done(): return domain.ActivityResult{}, ctx.Err()
    }
    return domain.ActivityResult{
        AgentRole: role,
        Status:    "succeeded",
        Message:   "stub completed",
    }, nil
}
```
