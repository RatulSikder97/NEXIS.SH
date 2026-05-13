# Phase 4 — Pipeline Substrate: Temporal + Sandbox — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the Temporal-backed `RecoveryPipeline` workflow with 9 stub activities (matching the agent fleet), the `services/validator` local-Docker sandbox, MinIO patch storage with envelope encryption, and the SSE-driven pipeline timeline UI in the console.

**Architecture:** Port/adapter pattern continues. New `domain/workflow.go` + `domain/patchstore.go` ports; `internal/workflow/` package holds the only Temporal SDK imports; `internal/adapter/workflow/` bridges Temporal + DB + SSE; `internal/adapter/patchstore/minio/` implements envelope-encrypted MinIO storage on top of the existing `domain.KeyVault`. `services/validator/` becomes a real Go service that shells out to `docker run --rm --network=none --read-only --tmpfs /tmp`. Web replaces two Phase 3 placeholders (`/console/incidents`, `/console/live-demo`) with real surfaces driven by an SSE stream.

**Tech Stack:** Go 1.25, `go.temporal.io/sdk` v1.32+, `github.com/minio/minio-go/v7` v7.0+, Next.js 16.2.2 + React 19, Postgres 16 RLS, Temporal dev server (already in compose), MinIO (already in compose), local Docker daemon (mounted into validator container).

**Spec:** `docs/superpowers/specs/2026-05-13-phase-4-pipeline-substrate.md`.

---

## Salvage / Reuse from Phases 1–3.5

- `internal/domain/{auth,audit,errors,keyvault,workspace}.go` — extend; never rewrite.
- `internal/adapter/keyvault/local.go` (AES-256-GCM) — reused **as-is** for envelope encryption. The Phase 4 patchstore depends on `domain.KeyVault`, not on the local adapter directly.
- `internal/platform/sse/broker.go` — the same generic broker handles `domain.ActivityEvent` topics keyed on `workflow_run_id`. No code changes.
- `internal/platform/cron/cron.go` — unchanged; Phase 4 adds no new cron jobs.
- `internal/transport/http/middleware/{auth,rls,rbac,cors}.go` — unchanged.
- `internal/transport/http/handler/workspaces.go` SSE pattern — reused verbatim for the pipeline events handler (`text/event-stream`, `X-Accel-Buffering: no`, manual `http.Flusher` after each write).
- `apps/web/lib/workspaces.ts` SDK pattern — copied to `apps/web/lib/pipelines.ts`.
- `apps/web/components/onboarding/ProvisioningAnimation.tsx` — pattern reused for the activity timeline (subscribe via EventSource, advance state, render check icons).
- `docker-compose.yml` — extend; do not rewrite. Temporal + MinIO services already exist (added in Phase 1).
- `services/validator/` skeleton — replace the placeholder `main.go` with a real handler; reuse the Dockerfile shape.

**Coordinator-owned files** (the controller must serialize edits to these — sub-agents may not touch them concurrently):

- `services/control-plane/cmd/server/main.go`
- `services/control-plane/internal/transport/http/server.go`
- `services/control-plane/internal/platform/config/config.go`
- `services/control-plane/.arch.yaml`
- `packages/db/schema.ts`
- `docker-compose.yml`
- `apps/web/proxy.ts`
- `apps/web/app/layout.tsx`

---

## Stage 0 — Schema + RLS + config + env

### Task 0.1: Drizzle schema additions

**Files:**
- Modify: `packages/db/schema.ts`

- [ ] **Step 1: Add 2 tables**

Add `workflow_runs` and `activity_events` after the Phase 3.5 `usage_records` block:

```ts
const workflowRunStatus  = ["queued", "running", "succeeded", "failed", "timed_out", "cancelled"] as const;
const activityStatus     = ["started", "succeeded", "failed", "retrying", "timed_out"] as const;

export const workflowRuns = pgTable("workflow_runs", {
  id:              uuid("id").primaryKey().defaultRandom(),
  orgId:           uuid("org_id").notNull().references(() => organizations.id),
  workspaceId:     uuid("workspace_id").notNull().references(() => workspaces.id),
  workflowType:    text("workflow_type").notNull(),
  temporalRunId:   text("temporal_run_id").notNull(),
  temporalWfId:    text("temporal_wf_id").notNull(),
  status:          text("status", { enum: workflowRunStatus }).notNull(),
  currentStep:     text("current_step"),
  input:           jsonb("input"),
  output:          jsonb("output"),
  error:           text("error"),
  startedAt:       timestamp("started_at",   { withTimezone: true }).notNull().defaultNow(),
  completedAt:     timestamp("completed_at", { withTimezone: true }),
  durationMs:      bigint("duration_ms", { mode: "number" }),
  createdBy:       uuid("created_by").references(() => users.id),
}, t => ({
  uniqOrgTemporal: uniqueIndex("workflow_runs_org_temporal_uniq").on(t.orgId, t.temporalRunId),
  orgWsStartedIdx: index("workflow_runs_org_ws_started_idx").on(t.orgId, t.workspaceId, t.startedAt),
}));

export const activityEvents = pgTable("activity_events", {
  id:              uuid("id").primaryKey().defaultRandom(),
  orgId:           uuid("org_id").notNull().references(() => organizations.id),
  workflowRunId:   uuid("workflow_run_id").notNull().references(() => workflowRuns.id, { onDelete: "cascade" }),
  seq:             integer("seq").notNull(),
  agentRole:       text("agent_role").notNull(),
  activityName:    text("activity_name").notNull(),
  status:          text("status", { enum: activityStatus }).notNull(),
  attempt:         integer("attempt").notNull().default(1),
  message:         text("message"),
  payload:         jsonb("payload"),
  ts:              timestamp("ts", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqRunSeq: uniqueIndex("activity_events_run_seq_uniq").on(t.workflowRunId, t.seq),
}));
```

Imports already cover `bigint`, `integer`, `jsonb`, `uniqueIndex`, `index` from Phase 3+3.5.

- [ ] **Step 2: Generate**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm exec drizzle-kit generate --name=phase4_workflows
```

Inspect `packages/db/migrations/0004_*_phase4_workflows.sql` — should contain two `CREATE TABLE` plus the two indexes.

### Task 0.2: Control-plane mirror migration + RLS

**Files:**
- Create: `services/control-plane/migrations/0009_phase4_workflows.up.sql`
- Create: `services/control-plane/migrations/0009_phase4_workflows.down.sql`
- Create: `services/control-plane/migrations/0010_phase4_rls.up.sql`
- Create: `services/control-plane/migrations/0010_phase4_rls.down.sql`

- [ ] **Step 1: Up**

`0009_phase4_workflows.up.sql`:

```sql
CREATE TABLE workflow_runs (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id            uuid NOT NULL REFERENCES organizations(id),
  workspace_id      uuid NOT NULL REFERENCES workspaces(id),
  workflow_type     text NOT NULL,
  temporal_run_id   text NOT NULL,
  temporal_wf_id    text NOT NULL,
  status            text NOT NULL CHECK (status IN ('queued','running','succeeded','failed','timed_out','cancelled')),
  current_step      text,
  input             jsonb,
  output            jsonb,
  error             text,
  started_at        timestamptz NOT NULL DEFAULT now(),
  completed_at      timestamptz,
  duration_ms       bigint,
  created_by        uuid REFERENCES users(id),
  UNIQUE (org_id, temporal_run_id)
);
CREATE INDEX workflow_runs_org_ws_started_idx ON workflow_runs (org_id, workspace_id, started_at DESC);

CREATE TABLE activity_events (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id            uuid NOT NULL REFERENCES organizations(id),
  workflow_run_id   uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
  seq               int  NOT NULL,
  agent_role        text NOT NULL,
  activity_name     text NOT NULL,
  status            text NOT NULL CHECK (status IN ('started','succeeded','failed','retrying','timed_out')),
  attempt           int  NOT NULL DEFAULT 1,
  message           text,
  payload           jsonb,
  ts                timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workflow_run_id, seq)
);
```

`0009_phase4_workflows.down.sql`:

```sql
DROP TABLE IF EXISTS activity_events;
DROP TABLE IF EXISTS workflow_runs;
```

- [ ] **Step 2: RLS**

`0010_phase4_rls.up.sql`:

```sql
ALTER TABLE workflow_runs    ENABLE ROW LEVEL SECURITY;
ALTER TABLE activity_events  ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON workflow_runs
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON activity_events
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON workflow_runs   TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON activity_events TO nexis_app;
```

`0010_phase4_rls.down.sql`:

```sql
DROP POLICY IF EXISTS tenant_isolation ON activity_events;
DROP POLICY IF EXISTS tenant_isolation ON workflow_runs;
ALTER TABLE activity_events DISABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_runs   DISABLE ROW LEVEL SECURITY;
```

- [ ] **Step 3: Apply**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm exec drizzle-kit migrate
docker compose cp services/control-plane/migrations/0010_phase4_rls.up.sql postgres:/tmp/0010.sql
docker compose exec -T postgres psql -U nexis -d nexis -f /tmp/0010.sql
docker compose exec -T postgres psql -U nexis -d nexis -c "\dt"
```

Expected: 17 tables visible (15 prior + 2 new). All RLS-enabled tables count = 14 (12 prior + 2 new).

```bash
docker compose exec -T postgres psql -U nexis -d nexis -c \
  "SELECT count(*) FROM pg_tables WHERE schemaname='public' AND rowsecurity=true;"
# expect 14
```

### Task 0.3: Config + env

**Files:**
- Modify: `services/control-plane/internal/platform/config/config.go`
- Modify: `docker-compose.yml`
- Modify: `.env.example`
- Modify: `services/validator/go.mod` (add deps)
- Modify: `services/control-plane/go.mod` (add deps)

- [ ] **Step 1: Extend Config**

Append to the Config struct, after the Phase 3.5 fields:

```go
    // Phase 4
    TemporalHostPort     string // 'temporal:7233' in compose
    TemporalNamespace    string // 'default' for dev; 'nexis-prod' in Phase 7
    TemporalTaskQueue    string // 'nexis-recovery'
    ValidatorURL         string // 'http://validator:8081'
    ValidatorToken       string // shared bearer
    PatchStore           string // 'minio' | 's3'
    MinIOEndpoint        string
    MinIOAccessKey       string
    MinIOSecretKey       string
    MinIOUseSSL          bool
```

Append to `Load()`:

```go
    TemporalHostPort:     env("TEMPORAL_HOST_PORT", "temporal:7233"),
    TemporalNamespace:    env("TEMPORAL_NAMESPACE", "default"),
    TemporalTaskQueue:    env("TEMPORAL_TASK_QUEUE", "nexis-recovery"),
    ValidatorURL:         env("VALIDATOR_URL", "http://validator:8081"),
    ValidatorToken:       env("VALIDATOR_TOKEN", "dev-validator-token-32byte"),
    PatchStore:           env("PATCH_STORE", "minio"),
    MinIOEndpoint:        env("MINIO_ENDPOINT", "minio:9000"),
    MinIOAccessKey:       env("MINIO_ACCESS_KEY", "nexis"),
    MinIOSecretKey:       env("MINIO_SECRET_KEY", "nexis_dev_password"),
    MinIOUseSSL:          parseBool(env("MINIO_USE_SSL", "0")),
```

- [ ] **Step 2: Add to compose (control-plane block)**

In `docker-compose.yml`, append to the `control-plane.environment` block:

```yaml
      TEMPORAL_HOST_PORT: temporal:7233
      TEMPORAL_NAMESPACE: default
      TEMPORAL_TASK_QUEUE: nexis-recovery
      VALIDATOR_URL: http://validator:8081
      VALIDATOR_TOKEN: ${VALIDATOR_TOKEN:-dev-validator-token-32byte}
      PATCH_STORE: minio
      MINIO_ENDPOINT: minio:9000
      MINIO_ACCESS_KEY: nexis
      MINIO_SECRET_KEY: nexis_dev_password
      MINIO_USE_SSL: "0"
```

Add `temporal` + `minio` to `depends_on`:

```yaml
    depends_on:
      postgres:
        condition: service_healthy
      otel-collector:
        condition: service_started
      mailhog:
        condition: service_started
      temporal:
        condition: service_healthy
      minio:
        condition: service_healthy
```

- [ ] **Step 3: Replace the validator service block**

Find the existing `validator:` block (currently just `PORT: 8081`) and replace with:

```yaml
  validator:
    build:
      context: services/validator
      dockerfile: Dockerfile
    environment:
      PORT: "8081"
      VALIDATOR_TOKEN: ${VALIDATOR_TOKEN:-dev-validator-token-32byte}
      NEXIS_VALIDATOR_IMAGE: nexis/validator-fixture:latest
      DOCKER_HOST: unix:///var/run/docker.sock
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    ports:
      - "8081:8081"
    networks: [nexis]
```

- [ ] **Step 4: Add to `.env.example`**

```
# === Phase 4 — pipeline substrate ===
TEMPORAL_HOST_PORT=temporal:7233
TEMPORAL_NAMESPACE=default
TEMPORAL_TASK_QUEUE=nexis-recovery
VALIDATOR_URL=http://validator:8081
VALIDATOR_TOKEN=dev-validator-token-32byte
PATCH_STORE=minio
MINIO_ENDPOINT=minio:9000
MINIO_ACCESS_KEY=nexis
MINIO_SECRET_KEY=nexis_dev_password
MINIO_USE_SSL=0
```

- [ ] **Step 5: Pin Go SDK dependencies**

In `services/control-plane/`:

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane
go get go.temporal.io/sdk@v1.32.1
go get github.com/minio/minio-go/v7@v7.0.78
go mod tidy
```

In `services/validator/`:

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/validator
go get github.com/go-chi/chi/v5@v5.2.5
go mod tidy
```

Bump the validator Dockerfile Go base from `golang:1.22-alpine` to `golang:1.25-alpine` to match control-plane.

### Task 0.4: Commit Stage 0

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
git add packages/db apps/web/drizzle apps/web/packages services/control-plane/migrations services/control-plane/internal/platform/config services/control-plane/go.mod services/control-plane/go.sum services/validator/go.mod services/validator/go.sum services/validator/Dockerfile docker-compose.yml .env.example
git commit -m "feat(db): phase 4 schema (workflow_runs + activity_events) + RLS + temporal/minio config (stage 0)"
```

---

## Stage 1 — Domain types + repos

### Task 1.1: Workflow domain

**Files:**
- Create: `services/control-plane/internal/domain/workflow.go`

```go
package domain

import (
    "context"
    "time"
)

// AgentRole names the 9-step agent fleet exactly once. The repo writes
// agent_events.agent_role from this string; the timeline UI keys icons by
// the same value.
type AgentRole string

const (
    AgentSentinel      AgentRole = "sentinel"
    AgentPathfinder    AgentRole = "pathfinder"
    AgentSynthesiser   AgentRole = "synthesiser"
    AgentArchitect     AgentRole = "architect"
    AgentBackend       AgentRole = "backend"
    AgentQA            AgentRole = "qa"
    AgentDevOps        AgentRole = "devops"
    AgentDataEngineer  AgentRole = "data_engineer"
    AgentApprovalGate  AgentRole = "approval_gate"
    AgentPipeline      AgentRole = "pipeline" // synthetic — workflow-level events
)

// AllAgents returns the 9 in-order roles used to render an empty timeline
// before any events arrive. Order matches the DAG fan-out:
//   1 → 2 → 3 → 4 → 5 → 6 → (7 || 8) → 9
var AllAgents = []AgentRole{
    AgentSentinel, AgentPathfinder, AgentSynthesiser,
    AgentArchitect, AgentBackend, AgentQA,
    AgentDevOps, AgentDataEngineer, AgentApprovalGate,
}

type WorkflowRunStatus string

const (
    WRQueued     WorkflowRunStatus = "queued"
    WRRunning    WorkflowRunStatus = "running"
    WRSucceeded  WorkflowRunStatus = "succeeded"
    WRFailed     WorkflowRunStatus = "failed"
    WRTimedOut   WorkflowRunStatus = "timed_out"
    WRCancelled  WorkflowRunStatus = "cancelled"
)

type ActivityStatus string

const (
    ActStarted   ActivityStatus = "started"
    ActSucceeded ActivityStatus = "succeeded"
    ActFailed    ActivityStatus = "failed"
    ActRetrying  ActivityStatus = "retrying"
    ActTimedOut  ActivityStatus = "timed_out"
)

// WorkflowRun is one tenant-scoped execution of a workflow type. ID equals
// the Temporal WorkflowID (we generate it client-side); TemporalRunID is
// the Temporal-assigned per-attempt id.
type WorkflowRun struct {
    ID            string
    OrgID         string
    WorkspaceID   string
    WorkflowType  string
    TemporalRunID string
    TemporalWfID  string
    Status        WorkflowRunStatus
    CurrentStep   string
    Input         []byte
    Output        []byte
    Error         string
    StartedAt     time.Time
    CompletedAt   *time.Time
    DurationMs    *int64
    CreatedBy     string
}

// ActivityEvent is one row in the activity_events table and one frame in the
// SSE stream. Seq is monotonic per run; SSE consumers dedup on (RunID, Seq).
type ActivityEvent struct {
    ID            string
    OrgID         string
    WorkflowRunID string
    Seq           int
    AgentRole     AgentRole
    ActivityName  string
    Status        ActivityStatus
    Attempt       int
    Message       string
    Payload       []byte
    TS            time.Time
}

// ActivityResult is the in-process value the workflow function reads back
// from each activity. It does NOT hit Postgres directly; the workflow
// brackets each call with a RecordActivityEvent activity that persists +
// publishes to the SSE broker.
type ActivityResult struct {
    AgentRole AgentRole
    Status    ActivityStatus
    Message   string
    Payload   map[string]any
}

// WorkflowService is the port HTTP handlers depend on. The adapter in
// internal/adapter/workflow implements it on top of a Temporal client + the
// workflow repo + an SSE broker.
type WorkflowService interface {
    Start(ctx context.Context, p Principal, workspaceID, workflowType string, input []byte) (WorkflowRun, error)
    Get(ctx context.Context, p Principal, runID string) (WorkflowRun, []ActivityEvent, error)
    List(ctx context.Context, p Principal, workspaceID string, limit int, before time.Time) ([]WorkflowRun, error)
    Subscribe(ctx context.Context, runID string) <-chan ActivityEvent
}
```

### Task 1.2: PatchStore domain port

**Files:**
- Create: `services/control-plane/internal/domain/patchstore.go`

```go
package domain

import (
    "context"
    "time"
)

// PatchStore stores envelope-encrypted artifacts (patches, sandbox logs,
// reports) in object storage. Implementations in internal/adapter/patchstore/:
//   - minio (Phase 4, local dev)
//   - s3    (Phase 7, AWS)
// Both wrap the body in a versioned envelope sealed by a KeyVault data key.
type PatchStore interface {
    // EnsureBucket is idempotent; safe to call before every Put.
    EnsureBucket(ctx context.Context, name string) error

    // Put writes plaintext after envelope-encrypting it. Body is opaque bytes;
    // ContentType is recorded on the object metadata.
    Put(ctx context.Context, opts PutOptions) error

    // Get reverses Put — fetches the object, strips the envelope, returns plaintext.
    Get(ctx context.Context, bucket, key string) ([]byte, error)

    // SignURL issues a time-limited GET URL. The data is still encrypted at
    // rest; the URL grants ciphertext access only. Decryption happens
    // server-side via Get.
    SignURL(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
}

type PutOptions struct {
    Bucket      string
    Key         string
    Body        []byte
    ContentType string
}

// BucketForOrg returns the canonical bucket name "nexis-org-<orgID>".
// Stored separately so handlers / activities don't reimplement the convention.
func BucketForOrg(orgID string) string { return "nexis-org-" + orgID }
```

### Task 1.3: Workflow repo

**Files:**
- Create: `services/control-plane/internal/adapter/repo/workflow_repo.go`

Repo pattern mirrors `workspaces_repo.go` from Phase 3.5: dual-pool — `appPool` for RLS-scoped per-request queries (`db.FromCtx`), `adminPool` for system-job paths (the Temporal activity that records events, which runs outside any HTTP request).

Methods:

```go
type WorkflowRepo struct {
    app   *pgxpool.Pool
    admin *pgxpool.Pool
}

func NewWorkflowRepo(app, admin *pgxpool.Pool) *WorkflowRepo
```

- `InsertRun(ctx, *WorkflowRun) error` — uses `db.FromCtx(ctx, r.app)`. Caller is the HTTP handler (RLS-bound).
- `UpdateRunStatus(ctx, runID, status, currentStep, error string, output []byte, completedAt *time.Time) error` — admin pool (called from activity).
- `GetRun(ctx, orgID, runID string) (*WorkflowRun, error)` — app pool via FromCtx.
- `ListRuns(ctx, orgID, workspaceID, limit int, before time.Time) ([]WorkflowRun, error)` — app pool.
- `InsertEvent(ctx, *ActivityEvent) error` — admin pool (called from activity).
- `ListEvents(ctx, runID string, sinceSeq int, limit int) ([]ActivityEvent, error)` — admin pool (SSE handler bypasses RLS after it has verified ownership; this is intentional so the SSE goroutine doesn't need a request-scoped tx).
- `NextSeq(ctx, runID string) (int, error)` — admin pool. `SELECT COALESCE(MAX(seq), 0) + 1 FROM activity_events WHERE workflow_run_id=$1`.

Audit lesson from Phase 2 (HMAC chain): `time.Now().UTC().Truncate(time.Microsecond)` everywhere a timestamp is written. activity_events isn't HMAC'd but it sorts on ts so we still truncate to keep ordering reproducible across replays.

### Task 1.4: Add `OwnsWorkspace` to WorkspacesRepo

**Files:**
- Modify: `services/control-plane/internal/adapter/repo/workspaces_repo.go`

Append one method:

```go
// OwnsWorkspace returns true if (orgID, workspaceID) exists. Uses the admin
// pool so it can be called from contexts outside a request tx (the SSE
// handler runs the check before opening the stream, before RLS would be
// useful anyway).
func (r *WorkspacesRepo) OwnsWorkspace(ctx context.Context, orgID, workspaceID string) (bool, error) {
    var n int
    err := r.admin.QueryRow(ctx,
        `SELECT count(*) FROM workspaces WHERE org_id=$1 AND id=$2`, orgID, workspaceID).Scan(&n)
    if err != nil { return false, err }
    return n > 0, nil
}
```

### Task 1.5: Commit

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make vet && make arch
git add internal/domain internal/adapter/repo
git commit -m "feat(domain): phase 4 workflow + patchstore ports + workflow_repo (stage 1)"
```

---

## Stage 2 — Temporal platform plumbing

### Task 2.1: Temporal client

**Files:**
- Create: `services/control-plane/internal/platform/temporal/client.go`

```go
// Package temporal wires the Temporal Go SDK client. The dev server runs in
// docker-compose at temporal:7233; production deployments target Temporal
// Cloud via the same dial-string by swapping env. The single client is
// shared across worker registration + WorkflowService.StartWorkflow.
package temporal

import (
    "fmt"
    "log/slog"
    "time"

    "go.temporal.io/sdk/client"
)

type Config struct {
    HostPort  string
    Namespace string
}

// Dial returns a connected client. Retries are handled by the SDK
// internally; we surface an error only if the initial namespace lookup
// fails. Callers should defer c.Close() on shutdown.
func Dial(cfg Config, logger *slog.Logger) (client.Client, error) {
    if cfg.HostPort == "" { return nil, fmt.Errorf("TEMPORAL_HOST_PORT empty") }
    opts := client.Options{
        HostPort:           cfg.HostPort,
        Namespace:          cfg.Namespace,
        ConnectionOptions:  client.ConnectionOptions{
            // Keepalives — Temporal dev server is happy with defaults; we set
            // explicitly so Phase 7 cloud cutover doesn't surprise us.
            DisableHealthCheck: false,
            HealthCheckTimeout: 5 * time.Second,
        },
    }
    c, err := client.Dial(opts)
    if err != nil {
        return nil, fmt.Errorf("temporal dial: %w", err)
    }
    logger.Info("temporal client connected", "host_port", cfg.HostPort, "namespace", cfg.Namespace)
    return c, nil
}
```

### Task 2.2: Worker registration

**Files:**
- Create: `services/control-plane/internal/platform/temporal/worker.go`

```go
package temporal

import (
    "context"
    "log/slog"

    "go.temporal.io/sdk/client"
    sdkworker "go.temporal.io/sdk/worker"
)

// WorkerSpec holds the workflow + activity registrations passed to Start.
// Keeping a struct (rather than func args) means the workflow package can
// produce a single value carrying both registrations and the platform
// package never imports the workflow package directly — that would loop
// since the workflow package depends on adapter (which lives below platform
// in the arch graph).
//
// We accept any/any for the slices so the workflow package can populate
// them with whatever types it owns; the Temporal SDK accepts interface{}
// for both.
type WorkerSpec struct {
    TaskQueue  string
    Workflows  []any
    Activities []any
}

// Start launches a worker on tq, registers the workflows + activities, and
// blocks until ctx is cancelled. Returns the first fatal error from
// w.Run(); transient errors are surfaced via the SDK logger.
func Start(ctx context.Context, c client.Client, spec WorkerSpec, logger *slog.Logger) error {
    if spec.TaskQueue == "" { return nil }
    w := sdkworker.New(c, spec.TaskQueue, sdkworker.Options{
        MaxConcurrentActivityExecutionSize:     16,
        MaxConcurrentWorkflowTaskExecutionSize: 16,
    })
    for _, wf := range spec.Workflows  { w.RegisterWorkflow(wf) }
    for _, ac := range spec.Activities { w.RegisterActivity(ac)  }
    logger.Info("temporal worker registered",
        "task_queue", spec.TaskQueue,
        "workflows", len(spec.Workflows),
        "activities", len(spec.Activities))
    return w.Run(sdkworker.InterruptCh())
}
```

Note: `worker.InterruptCh()` is the SDK's process-signal channel. We don't use `ctx` for cancellation because the SDK's own `Run` handles SIGINT/SIGTERM directly — passing it would double-up. The control-plane's `main.go` already coordinates shutdown via its own signal handler; both paths are equivalent and they don't deadlock each other.

### Task 2.3: Smoke test the dial

**Files:**
- Create: `services/control-plane/internal/platform/temporal/client_test.go`

Skip if `TEMPORAL_HOST_PORT` env is unset (CI without temporal). Otherwise dial + close + assert no error.

```go
//go:build integration

package temporal

// Run with `TEMPORAL_HOST_PORT=localhost:7233 go test -tags=integration ./internal/platform/temporal/...`.
```

### Task 2.4: Arch.yaml update

**Files:**
- Modify: `services/control-plane/.arch.yaml`

No changes needed — `workflow` and `platform` components are already declared, and `workflow.mayDependOn: [usecase, domain, adapter, platform]` covers what we need. Confirm by running `make arch` after Stage 2.

### Task 2.5: Commit

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make vet
git add internal/platform/temporal
git commit -m "feat(platform/temporal): SDK client + worker scaffolding (stage 2)"
```

---

## Stage 3 — Workflow package: `RecoveryPipeline` + 9 stub activities

### Task 3.1: Activity types

**Files:**
- Create: `services/control-plane/internal/workflow/types.go`

```go
package workflow

import "github.com/nexis-eco/nexis/services/control-plane/internal/domain"

// PipelineInput is the StartWorkflow input. Carries the org + workspace +
// optional incident id (Phase 4 uses 'manual' or 'demo'; Phase 6 fills in
// from a Sentinel emission).
type PipelineInput struct {
    OrgID       string `json:"org_id"`
    WorkspaceID string `json:"workspace_id"`
    RunID       string `json:"run_id"`        // our workflow_runs.id, also the WorkflowID
    IncidentID  string `json:"incident_id"`
    TriggeredBy string `json:"triggered_by"`  // 'manual' | 'demo' | 'sentinel'
}

// PipelineOutput is the workflow return value. Used by GetRun for the
// trailing summary in the timeline header.
type PipelineOutput struct {
    DurationMS int64           `json:"duration_ms"`
    Results    []domain.ActivityResult `json:"results"`
}

// RecordEventInput is the input shape for RecordActivityEvent. We keep it
// as a tiny named struct (not bare args) because Temporal serializes
// activity inputs to history — typed structs are auditable.
type RecordEventInput struct {
    OrgID         string                `json:"org_id"`
    WorkflowRunID string                `json:"workflow_run_id"`
    AgentRole     domain.AgentRole      `json:"agent_role"`
    ActivityName  string                `json:"activity_name"`
    Status        domain.ActivityStatus `json:"status"`
    Attempt       int                   `json:"attempt"`
    Message       string                `json:"message"`
    Payload       map[string]any        `json:"payload,omitempty"`
}
```

### Task 3.2: Stub activities + recorder

**Files:**
- Create: `services/control-plane/internal/workflow/activities.go`

```go
package workflow

import (
    "context"
    "encoding/json"
    "time"

    "go.temporal.io/sdk/activity"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/sse"
)

// Activities holds the dependencies needed by every activity function. One
// instance is registered with the worker via RegisterActivity(a); Temporal
// dispatches method calls on it.
type Activities struct {
    Repo   *repo.WorkflowRepo
    Broker *sse.Broker[domain.ActivityEvent]
}

func NewActivities(r *repo.WorkflowRepo, b *sse.Broker[domain.ActivityEvent]) *Activities {
    return &Activities{Repo: r, Broker: b}
}

// RecordActivityEvent persists a row to activity_events + publishes to the
// SSE broker. Called explicitly from the workflow function (NOT triggered
// by Temporal heartbeats) so we control the seq numbering and the wire
// shape.
func (a *Activities) RecordActivityEvent(ctx context.Context, in RecordEventInput) error {
    seq, err := a.Repo.NextSeq(ctx, in.WorkflowRunID)
    if err != nil { return err }
    var payload []byte
    if in.Payload != nil { payload, _ = json.Marshal(in.Payload) }
    evt := &domain.ActivityEvent{
        OrgID:         in.OrgID,
        WorkflowRunID: in.WorkflowRunID,
        Seq:           seq,
        AgentRole:     in.AgentRole,
        ActivityName:  in.ActivityName,
        Status:        in.Status,
        Attempt:       in.Attempt,
        Message:       in.Message,
        Payload:       payload,
        TS:            time.Now().UTC().Truncate(time.Microsecond),
    }
    if err := a.Repo.InsertEvent(ctx, evt); err != nil { return err }
    a.Broker.Publish(in.WorkflowRunID, *evt)
    return nil
}

// stubSleep is the body shared by every Phase 4 activity. Phases 5+6 replace
// each per-agent function with real work while keeping this signature.
func stubSleep(ctx context.Context, role domain.AgentRole, name string, d time.Duration) (domain.ActivityResult, error) {
    activity.GetLogger(ctx).Info("stub start", "agent", role, "activity", name)
    select {
    case <-time.After(d):
    case <-ctx.Done(): return domain.ActivityResult{}, ctx.Err()
    }
    activity.GetLogger(ctx).Info("stub end", "agent", role, "activity", name)
    return domain.ActivityResult{
        AgentRole: role,
        Status:    domain.ActSucceeded,
        Message:   "stub completed",
    }, nil
}

// One method per agent. Phase 5 swaps bodies; orchestration stays put.
func (a *Activities) SentinelDetect      (ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) { return stubSleep(ctx, domain.AgentSentinel,     "Sentinel.Detect",         1000*time.Millisecond) }
func (a *Activities) PathfinderDiagnose  (ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) { return stubSleep(ctx, domain.AgentPathfinder,   "Pathfinder.Diagnose",     1500*time.Millisecond) }
func (a *Activities) SynthesiserPlan     (ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) { return stubSleep(ctx, domain.AgentSynthesiser,  "Synthesiser.Plan",        1500*time.Millisecond) }
func (a *Activities) ArchitectSolution   (ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) { return stubSleep(ctx, domain.AgentArchitect,    "Architect.Solution",      1000*time.Millisecond) }
func (a *Activities) BackendCodegen      (ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) { return stubSleep(ctx, domain.AgentBackend,      "Backend.Codegen",         2000*time.Millisecond) }
func (a *Activities) QATestGen           (ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) { return stubSleep(ctx, domain.AgentQA,           "QA.TestGen",              1000*time.Millisecond) }
func (a *Activities) DevOpsPipeline      (ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) { return stubSleep(ctx, domain.AgentDevOps,       "DevOps.Pipeline",         1000*time.Millisecond) }
func (a *Activities) DataEngineerMigrate (ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) { return stubSleep(ctx, domain.AgentDataEngineer, "DataEngineer.Migrations", 1000*time.Millisecond) }
func (a *Activities) ApprovalGateRoute   (ctx context.Context, _ PipelineInput) (domain.ActivityResult, error) { return stubSleep(ctx, domain.AgentApprovalGate, "ApprovalGate.Route",       500*time.Millisecond) }
```

### Task 3.3: `RecoveryPipeline` workflow function

**Files:**
- Create: `services/control-plane/internal/workflow/pipeline.go`

```go
package workflow

import (
    "time"

    "go.temporal.io/sdk/temporal"
    "go.temporal.io/sdk/workflow"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// stdActivityOpts is the default retry + timeout policy applied to every
// stub activity in Phase 4. Phase 5's LLM-bound activities override
// StartToCloseTimeout to 5 minutes via a per-call ActivityOptions context.
var stdActivityOpts = workflow.ActivityOptions{
    StartToCloseTimeout:    30 * time.Second,
    ScheduleToCloseTimeout: 2 * time.Minute,
    HeartbeatTimeout:       10 * time.Second,
    RetryPolicy: &temporal.RetryPolicy{
        InitialInterval:        1 * time.Second,
        BackoffCoefficient:     2.0,
        MaximumInterval:        30 * time.Second,
        MaximumAttempts:        3,
        NonRetryableErrorTypes: []string{"ValidationError", "ForbiddenError"},
    },
}

// llmActivityOpts overrides start-to-close for codegen/test-gen activities.
// Phase 4 stubs don't need the 5-minute window but we set it now so Phase 5
// inherits the orchestration unchanged.
var llmActivityOpts = func() workflow.ActivityOptions {
    o := stdActivityOpts
    o.StartToCloseTimeout = 5 * time.Minute
    return o
}()

// RecoveryPipeline is the 9-step DAG. The shape MUST match the spec table
// (4 sequential, 4 sequential, 2 parallel, 1 join) — Phases 5+6 only swap
// the activity bodies.
func RecoveryPipeline(ctx workflow.Context, in PipelineInput) (PipelineOutput, error) {
    start := workflow.Now(ctx)
    results := make([]domain.ActivityResult, 0, 9)

    // ---- L2 detect → diagnose → plan (sequential) ----
    actCtx := workflow.WithActivityOptions(ctx, stdActivityOpts)
    a := func(name string, fn any, opts workflow.ActivityOptions, role domain.AgentRole, activityName string) error {
        // bracket every activity with started/succeeded events; if it errors,
        // record one failed event with the error message.
        c := workflow.WithActivityOptions(ctx, opts)
        record(c, in, role, activityName, domain.ActStarted, "", nil, 1)
        var r domain.ActivityResult
        if err := workflow.ExecuteActivity(c, fn, in).Get(c, &r); err != nil {
            record(c, in, role, activityName, domain.ActFailed, err.Error(), nil, 1)
            return err
        }
        record(c, in, role, activityName, domain.ActSucceeded, r.Message, r.Payload, 1)
        results = append(results, r)
        return nil
    }
    _ = actCtx // keep golint happy; sub-calls use their own ctx

    if err := a("Sentinel.Detect",      (*Activities).SentinelDetect,     stdActivityOpts, domain.AgentSentinel,     "Sentinel.Detect");      err != nil { return PipelineOutput{}, err }
    if err := a("Pathfinder.Diagnose",  (*Activities).PathfinderDiagnose, stdActivityOpts, domain.AgentPathfinder,   "Pathfinder.Diagnose");  err != nil { return PipelineOutput{}, err }
    if err := a("Synthesiser.Plan",     (*Activities).SynthesiserPlan,    stdActivityOpts, domain.AgentSynthesiser,  "Synthesiser.Plan");     err != nil { return PipelineOutput{}, err }

    // ---- L1 architect → backend → qa (sequential) ----
    if err := a("Architect.Solution", (*Activities).ArchitectSolution, stdActivityOpts, domain.AgentArchitect, "Architect.Solution"); err != nil { return PipelineOutput{}, err }
    if err := a("Backend.Codegen",    (*Activities).BackendCodegen,    llmActivityOpts, domain.AgentBackend,   "Backend.Codegen");    err != nil { return PipelineOutput{}, err }
    if err := a("QA.TestGen",         (*Activities).QATestGen,         llmActivityOpts, domain.AgentQA,        "QA.TestGen");         err != nil { return PipelineOutput{}, err }

    // ---- DevOps || DataEngineer (parallel) ----
    record(ctx, in, domain.AgentDevOps,       "DevOps.Pipeline",         domain.ActStarted, "", nil, 1)
    record(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActStarted, "", nil, 1)

    devopsFut := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, stdActivityOpts), (*Activities).DevOpsPipeline,      in)
    dataFut   := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, stdActivityOpts), (*Activities).DataEngineerMigrate, in)

    var rDev, rData domain.ActivityResult
    if err := devopsFut.Get(ctx, &rDev); err != nil {
        record(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", domain.ActFailed, err.Error(), nil, 1)
        return PipelineOutput{}, err
    }
    record(ctx, in, domain.AgentDevOps, "DevOps.Pipeline", domain.ActSucceeded, rDev.Message, rDev.Payload, 1)
    results = append(results, rDev)

    if err := dataFut.Get(ctx, &rData); err != nil {
        record(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActFailed, err.Error(), nil, 1)
        return PipelineOutput{}, err
    }
    record(ctx, in, domain.AgentDataEngineer, "DataEngineer.Migrations", domain.ActSucceeded, rData.Message, rData.Payload, 1)
    results = append(results, rData)

    // ---- ApprovalGate.Route ----
    if err := a("ApprovalGate.Route", (*Activities).ApprovalGateRoute, stdActivityOpts, domain.AgentApprovalGate, "ApprovalGate.Route"); err != nil {
        return PipelineOutput{}, err
    }

    out := PipelineOutput{
        DurationMS: workflow.Now(ctx).Sub(start).Milliseconds(),
        Results:    results,
    }
    // Terminal pipeline event — UI uses this to close EventSource.
    record(ctx, in, domain.AgentPipeline, "Pipeline.Complete", domain.ActSucceeded, "duration_ms="+itoa(out.DurationMS), map[string]any{"duration_ms": out.DurationMS}, 1)
    return out, nil
}

// record is the workflow-side helper that calls RecordActivityEvent as a
// side-effect activity. Using an activity (rather than a SideEffect or
// direct DB call) keeps the workflow deterministic + replay-safe.
func record(ctx workflow.Context, in PipelineInput, role domain.AgentRole, name string, status domain.ActivityStatus, message string, payload map[string]any, attempt int) {
    recCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        StartToCloseTimeout: 5 * time.Second,
        RetryPolicy: &temporal.RetryPolicy{
            InitialInterval: 200 * time.Millisecond,
            MaximumAttempts: 3,
        },
    })
    _ = workflow.ExecuteActivity(recCtx, (*Activities).RecordActivityEvent, RecordEventInput{
        OrgID:         in.OrgID,
        WorkflowRunID: in.RunID,
        AgentRole:     role,
        ActivityName:  name,
        Status:        status,
        Attempt:       attempt,
        Message:       message,
        Payload:       payload,
    }).Get(ctx, nil)
}

func itoa(n int64) string {
    return time.Duration(n*int64(time.Millisecond)).String()
}
```

### Task 3.4: Unit-test the DAG with `temporaltest`

**Files:**
- Create: `services/control-plane/internal/workflow/pipeline_test.go`

Use Temporal's in-memory test env. Mock `Activities` with a fake `Repo` (or nil repo + nil broker — the test env intercepts activity bodies anyway). Assert:

- `TestRecoveryPipeline_AllStubsSucceed` — wires the real workflow + the stub activities (no DB); asserts the workflow completes successfully and returns 9 results.
- `TestRecoveryPipeline_BackendFailsTwiceThenSucceeds` — uses test env's `OnActivity` to fail `BackendCodegen` twice then succeed; assert 3 attempts recorded.
- `TestRecoveryPipeline_DAGOrder` — captures the order of `RecordActivityEvent` calls and asserts: 1→2→3→4→5→6→{7,8 in any order}→9.

Skeleton:

```go
import (
    "testing"
    "go.temporal.io/sdk/testsuite"
)

func TestRecoveryPipeline_AllStubsSucceed(t *testing.T) {
    s := &testsuite.WorkflowTestSuite{}
    env := s.NewTestWorkflowEnvironment()
    a := NewActivities(nil, nil)            // OK — we override the recorder below.
    env.RegisterActivity(a)
    env.OnActivity((*Activities).RecordActivityEvent, mock.Anything, mock.Anything).Return(nil)
    env.ExecuteWorkflow(RecoveryPipeline, PipelineInput{
        OrgID: "org-1", WorkspaceID: "ws-1", RunID: "run-1", IncidentID: "manual", TriggeredBy: "manual",
    })
    require.True(t, env.IsWorkflowCompleted())
    require.NoError(t, env.GetWorkflowError())
    var out PipelineOutput
    require.NoError(t, env.GetWorkflowResult(&out))
    require.Len(t, out.Results, 9)
}
```

Add `github.com/stretchr/testify` (already a transitive dep via Temporal) explicitly if not present.

### Task 3.5: Commit

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add internal/workflow
git commit -m "feat(workflow): RecoveryPipeline workflow + 9 stub activities + recorder (stage 3)"
```

---

## Stage 4 — WorkflowService adapter + HTTP routes + SSE

### Task 4.1: WorkflowService implementation

**Files:**
- Create: `services/control-plane/internal/adapter/workflow/service.go`
- Create: `services/control-plane/internal/adapter/workflow/service_test.go`

```go
package workflow

import (
    "context"
    "encoding/json"
    "log/slog"
    "time"

    "github.com/google/uuid"
    "go.temporal.io/sdk/client"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/sse"
    wfpkg "github.com/nexis-eco/nexis/services/control-plane/internal/workflow"
)

type Config struct {
    Repo      *repo.WorkflowRepo
    Workspaces *repo.WorkspacesRepo  // for ownership checks
    Temporal  client.Client
    Broker    *sse.Broker[domain.ActivityEvent]
    TaskQueue string
    Logger    *slog.Logger
}

type Service struct{ cfg Config }

func New(cfg Config) *Service { return &Service{cfg: cfg} }

func (s *Service) Start(ctx context.Context, p domain.Principal, workspaceID, workflowType string, inputJSON []byte) (domain.WorkflowRun, error) {
    if ok, _ := s.cfg.Workspaces.OwnsWorkspace(ctx, p.OrgID, workspaceID); !ok {
        return domain.WorkflowRun{}, domain.ErrNotFound
    }
    runID := uuid.NewString()
    in := wfpkg.PipelineInput{
        OrgID: p.OrgID, WorkspaceID: workspaceID, RunID: runID,
        IncidentID: "manual", TriggeredBy: "manual",
    }
    // Insert the row inside the request tx so RLS sees app.current_org_id.
    row := &domain.WorkflowRun{
        ID: runID, OrgID: p.OrgID, WorkspaceID: workspaceID, WorkflowType: workflowType,
        Status: domain.WRQueued, Input: inputJSON, StartedAt: time.Now().UTC().Truncate(time.Microsecond),
        CreatedBy: p.UserID,
    }
    if err := s.cfg.Repo.InsertRun(ctx, row); err != nil { return domain.WorkflowRun{}, err }

    // StartWorkflow runs OUTSIDE the request tx (it's not a Postgres call) —
    // safe to do after the row exists.
    wf, err := s.cfg.Temporal.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
        ID: runID, TaskQueue: s.cfg.TaskQueue,
        WorkflowExecutionTimeout: 10 * time.Minute,
        WorkflowRunTimeout:       10 * time.Minute,
        WorkflowTaskTimeout:      10 * time.Second,
    }, wfpkg.RecoveryPipeline, in)
    if err != nil {
        _ = s.cfg.Repo.UpdateRunStatus(ctx, runID, domain.WRFailed, "", err.Error(), nil, ptrTime(time.Now()))
        return domain.WorkflowRun{}, err
    }
    row.TemporalWfID  = wf.GetID()
    row.TemporalRunID = wf.GetRunID()
    row.Status        = domain.WRRunning
    if err := s.cfg.Repo.UpdateRunStatus(ctx, runID, domain.WRRunning, "", "", nil, nil); err != nil {
        s.cfg.Logger.Warn("update run status", "err", err)
    }

    // Reaper goroutine — waits for Temporal-side completion, updates the row.
    go s.reap(runID, wf)
    return *row, nil
}

// reap blocks on wf.Get and updates workflow_runs to the terminal status.
// We use the admin pool for this update because the reaper runs outside any
// request tx.
func (s *Service) reap(runID string, wf client.WorkflowRun) {
    ctx := context.Background()
    var out wfpkg.PipelineOutput
    err := wf.Get(ctx, &out)
    now := time.Now().UTC().Truncate(time.Microsecond)
    status := domain.WRSucceeded
    errStr := ""
    if err != nil { status = domain.WRFailed; errStr = err.Error() }
    var outBytes []byte
    if err == nil { outBytes, _ = json.Marshal(out) }
    _ = s.cfg.Repo.UpdateRunStatus(ctx, runID, status, "", errStr, outBytes, &now)
}

func (s *Service) Get(ctx context.Context, p domain.Principal, runID string) (domain.WorkflowRun, []domain.ActivityEvent, error) {
    r, err := s.cfg.Repo.GetRun(ctx, p.OrgID, runID)
    if err != nil { return domain.WorkflowRun{}, nil, err }
    evts, err := s.cfg.Repo.ListEvents(ctx, runID, 0, 200)
    return *r, evts, err
}

func (s *Service) List(ctx context.Context, p domain.Principal, workspaceID string, limit int, before time.Time) ([]domain.WorkflowRun, error) {
    if ok, _ := s.cfg.Workspaces.OwnsWorkspace(ctx, p.OrgID, workspaceID); !ok {
        return nil, domain.ErrNotFound
    }
    return s.cfg.Repo.ListRuns(ctx, p.OrgID, workspaceID, limit, before)
}

func (s *Service) Subscribe(ctx context.Context, runID string) <-chan domain.ActivityEvent {
    ch, unsub := s.cfg.Broker.Subscribe(runID, 64)
    go func() { <-ctx.Done(); unsub() }()
    return ch
}

func ptrTime(t time.Time) *time.Time { return &t }
```

### Task 4.2: HTTP handler — pipelines

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/pipelines.go`
- Create: `services/control-plane/internal/transport/http/dto/pipelines.go`

Handlers:

- `PipelinesList(svc domain.WorkflowService)` — `GET /v1/workspaces/{ws_id}/pipelines`. Parses `limit` (default 50, max 200), `before` (RFC3339); calls `svc.List`; returns `[]WorkflowRunResp`.
- `PipelineGet(svc domain.WorkflowService)` — `GET /v1/workspaces/{ws_id}/pipelines/{run_id}`. Returns `{ run, events: [...] }`.
- `PipelineCreate(svc, aud)` — `POST /v1/workspaces/{ws_id}/pipelines`. Body `{ workflow_type: "RecoveryPipeline", input: {...} }`. Audit `pipelines.create`. 202 + WorkflowRunResp.
- `PipelineDemo(svc, aud, cfg)` — `POST /v1/workspaces/{ws_id}/pipelines/demo`. Gated `cfg.AppEnv=="dev"`. Builds a synthetic `PipelineInput` via the `usecase/pipeline_demo.go` helper. 202 + WorkflowRunResp.
- `PipelineEvents(svc, repo, workspaces)` — `GET /v1/workspaces/{ws_id}/pipelines/{run_id}/events`. SSE.

SSE handler structure (mirror `workspaces.go` SSE pattern):

```go
func PipelineEvents(svc domain.WorkflowService, wfRepo *repo.WorkflowRepo, wsRepo *repo.WorkspacesRepo) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        princ, _ := appmw.PrincipalFrom(r.Context())
        wsID := chi.URLParam(r, "ws_id")
        runID := chi.URLParam(r, "run_id")
        if ok, _ := wsRepo.OwnsWorkspace(r.Context(), princ.OrgID, wsID); !ok {
            http.Error(w, "not found", 404); return
        }
        if _, _, err := svc.Get(r.Context(), princ, runID); err != nil {
            http.Error(w, "not found", 404); return
        }

        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("X-Accel-Buffering", "no")
        flusher, _ := w.(http.Flusher)

        // 1. Replay existing events from DB.
        existing, _ := wfRepo.ListEvents(r.Context(), runID, 0, 500)
        for _, e := range existing { writeSSE(w, e); flusher.Flush() }

        // 2. Tail the broker.
        ch := svc.Subscribe(r.Context(), runID)
        for {
            select {
            case <-r.Context().Done(): return
            case ev, ok := <-ch:
                if !ok { return }
                writeSSE(w, ev); flusher.Flush()
                if ev.AgentRole == domain.AgentPipeline { // terminal
                    fmt.Fprintf(w, "event: close\ndata: {}\n\n"); flusher.Flush()
                    return
                }
            }
        }
    }
}
```

DTO shape (snake_case JSON tags per Phase 3 lesson):

```go
type WorkflowRunResp struct {
    ID            string `json:"id"`
    OrgID         string `json:"org_id"`
    WorkspaceID   string `json:"workspace_id"`
    WorkflowType  string `json:"workflow_type"`
    Status        string `json:"status"`
    CurrentStep   string `json:"current_step,omitempty"`
    StartedAt     string `json:"started_at"`
    CompletedAt   string `json:"completed_at,omitempty"`
    DurationMs    int64  `json:"duration_ms,omitempty"`
    Error         string `json:"error,omitempty"`
}

type ActivityEventResp struct {
    WorkflowRunID string         `json:"workflow_run_id"`
    Seq           int            `json:"seq"`
    AgentRole     string         `json:"agent_role"`
    ActivityName  string         `json:"activity_name"`
    Status        string         `json:"status"`
    Attempt       int            `json:"attempt"`
    Message       string         `json:"message,omitempty"`
    Payload       map[string]any `json:"payload,omitempty"`
    TS            string         `json:"ts"`
}
```

### Task 4.3: Mount routes + extend Deps

**Files:**
- Modify: `services/control-plane/internal/transport/http/server.go`
- Modify: `services/control-plane/cmd/server/main.go`

In `server.go` Deps struct, add:

```go
    Workflows     domain.WorkflowService
    WorkflowsRepo *repo.WorkflowRepo
```

Mount block — inside the existing protected group, after the workspace SSE route, before the owner|admin sub-group:

```go
            if deps.Workflows != nil {
                g.Get ("/v1/workspaces/{ws_id}/pipelines",                       handler.PipelinesList   (deps.Workflows))
                g.Get ("/v1/workspaces/{ws_id}/pipelines/{run_id}",              handler.PipelineGet      (deps.Workflows))
                g.Get ("/v1/workspaces/{ws_id}/pipelines/{run_id}/events",       handler.PipelineEvents   (deps.Workflows, deps.WorkflowsRepo, deps.WorkspacesRepo))
            }
```

Inside the existing `owner|admin` sub-group `g2`:

```go
            if deps.Workflows != nil {
                g2.Post("/v1/workspaces/{ws_id}/pipelines",      handler.PipelineCreate(deps.Workflows, aud))
                if cfg.AppEnv == "dev" {
                    g2.Post("/v1/workspaces/{ws_id}/pipelines/demo", handler.PipelineDemo(deps.Workflows, aud, cfg))
                }
            }
```

In `main.go`, after the workspace adapter wiring, add the Temporal client + worker boot:

```go
    // Phase 4 — Temporal worker + WorkflowService.
    var wfService domain.WorkflowService
    var wfRepo *repo.WorkflowRepo
    if appPool != nil && adminPool != nil {
        wfRepo = repo.NewWorkflowRepo(appPool, adminPool)

        tc, err := temporalplatform.Dial(temporalplatform.Config{
            HostPort:  cfg.TemporalHostPort,
            Namespace: cfg.TemporalNamespace,
        }, logger)
        if err != nil {
            if cfg.AppEnv != "dev" {
                logger.Error("temporal dial", "err", err); os.Exit(1)
            }
            logger.Warn("temporal disabled (dev fallback)", "err", err)
        } else {
            defer tc.Close()
            wfBroker := sse.New[domain.ActivityEvent]()
            acts := wfpkg.NewActivities(wfRepo, wfBroker)

            wfService = adapterworkflow.New(adapterworkflow.Config{
                Repo: wfRepo, Workspaces: wsRepo,
                Temporal: tc, Broker: wfBroker,
                TaskQueue: cfg.TemporalTaskQueue, Logger: logger,
            })

            // Launch the worker on its own goroutine.
            go func() {
                if err := temporalplatform.Start(context.Background(), tc,
                    temporalplatform.WorkerSpec{
                        TaskQueue:  cfg.TemporalTaskQueue,
                        Workflows:  []any{wfpkg.RecoveryPipeline},
                        Activities: []any{acts},
                    }, logger); err != nil {
                    logger.Error("temporal worker", "err", err)
                }
            }()
        }
    }
```

Pass `wfService` + `wfRepo` into `httpserver.New(... Deps{ ... Workflows: wfService, WorkflowsRepo: wfRepo })`.

Imports to add at the top of `main.go`:

```go
adapterworkflow "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/workflow"
temporalplatform "github.com/nexis-eco/nexis/services/control-plane/internal/platform/temporal"
wfpkg "github.com/nexis-eco/nexis/services/control-plane/internal/workflow"
```

### Task 4.4: Commit

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add .
git commit -m "feat(workflow): adapter service + HTTP routes + SSE + main wiring (stage 4)"
```

---

## Stage 5 — Patch storage: MinIO adapter + envelope encryption

### Task 5.1: MinIO adapter

**Files:**
- Create: `services/control-plane/internal/adapter/patchstore/factory.go`
- Create: `services/control-plane/internal/adapter/patchstore/minio/store.go`
- Create: `services/control-plane/internal/adapter/patchstore/minio/store_test.go`
- Create: `services/control-plane/internal/adapter/patchstore/s3/store.go`

`minio/store.go`:

```go
// Package minio implements domain.PatchStore on top of MinIO. Every Put
// envelope-encrypts the body before upload; Get reverses the process. The
// wire format is versioned:
//
//   [4 bytes magic "NX\x01\x00"][4 bytes BE wrapped_key_len][wrapped_key][12 bytes nonce][gcm ciphertext+tag]
//
// Phase 7's S3 adapter re-uses the same format byte-for-byte; only the
// transport changes.
package minio

import (
    "bytes"
    "context"
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/binary"
    "errors"
    "fmt"
    "io"
    "time"

    minio "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

var envelopeMagic = []byte{'N', 'X', 0x01, 0x00}

type Store struct {
    client *minio.Client
    kv     domain.KeyVault
}

type Config struct {
    Endpoint, AccessKey, SecretKey string
    UseSSL                         bool
    KeyVault                       domain.KeyVault
}

func New(cfg Config) (*Store, error) {
    c, err := minio.New(cfg.Endpoint, &minio.Options{
        Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
        Secure: cfg.UseSSL,
    })
    if err != nil { return nil, err }
    return &Store{client: c, kv: cfg.KeyVault}, nil
}

func (s *Store) EnsureBucket(ctx context.Context, name string) error {
    exists, err := s.client.BucketExists(ctx, name)
    if err != nil { return err }
    if exists { return nil }
    if err := s.client.MakeBucket(ctx, name, minio.MakeBucketOptions{}); err != nil {
        // Race: another goroutine created it just now — tolerate.
        if exists2, _ := s.client.BucketExists(ctx, name); exists2 { return nil }
        return err
    }
    return nil
}

func (s *Store) Put(ctx context.Context, opts domain.PutOptions) error {
    if err := s.EnsureBucket(ctx, opts.Bucket); err != nil { return err }
    envelope, err := s.encrypt(ctx, opts.Body)
    if err != nil { return err }
    _, err = s.client.PutObject(ctx, opts.Bucket, opts.Key,
        bytes.NewReader(envelope), int64(len(envelope)),
        minio.PutObjectOptions{ContentType: opts.ContentType})
    return err
}

func (s *Store) Get(ctx context.Context, bucket, key string) ([]byte, error) {
    obj, err := s.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
    if err != nil { return nil, err }
    defer obj.Close()
    envelope, err := io.ReadAll(obj)
    if err != nil { return nil, err }
    return s.decrypt(ctx, envelope)
}

func (s *Store) SignURL(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
    u, err := s.client.PresignedGetObject(ctx, bucket, key, ttl, nil)
    if err != nil { return "", err }
    return u.String(), nil
}

// envelope: [magic(4)][wrapped_key_len BE uint32(4)][wrapped_key][nonce(12)][ciphertext+tag]
func (s *Store) encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
    dataKey := make([]byte, 32)
    if _, err := rand.Read(dataKey); err != nil { return nil, err }
    wrapped, err := s.kv.Encrypt(ctx, dataKey)
    if err != nil { return nil, err }
    block, err := aes.NewCipher(dataKey); if err != nil { return nil, err }
    gcm, err := cipher.NewGCM(block);     if err != nil { return nil, err }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := rand.Read(nonce); err != nil { return nil, err }
    ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

    var hdr [8]byte
    copy(hdr[0:4], envelopeMagic)
    binary.BigEndian.PutUint32(hdr[4:8], uint32(len(wrapped)))

    out := make([]byte, 0, len(hdr)+len(wrapped)+len(nonce)+len(ciphertext))
    out = append(out, hdr[:]...)
    out = append(out, wrapped...)
    out = append(out, nonce...)
    out = append(out, ciphertext...)
    return out, nil
}

func (s *Store) decrypt(ctx context.Context, env []byte) ([]byte, error) {
    if len(env) < 8 { return nil, errors.New("envelope: truncated header") }
    if !bytes.Equal(env[:4], envelopeMagic) {
        return nil, fmt.Errorf("envelope: bad magic %x", env[:4])
    }
    wrappedLen := binary.BigEndian.Uint32(env[4:8])
    if int(8+wrappedLen+12) > len(env) { return nil, errors.New("envelope: truncated body") }
    wrapped := env[8 : 8+wrappedLen]
    nonce   := env[8+wrappedLen : 8+wrappedLen+12]
    ct      := env[8+wrappedLen+12:]

    dataKey, err := s.kv.Decrypt(ctx, wrapped)
    if err != nil { return nil, err }
    block, err := aes.NewCipher(dataKey); if err != nil { return nil, err }
    gcm, err := cipher.NewGCM(block);     if err != nil { return nil, err }
    return gcm.Open(nil, nonce, ct, nil)
}
```

### Task 5.2: Factory + s3 stub

`patchstore/factory.go`:

```go
package patchstore

import (
    "fmt"
    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/patchstore/minio"
    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/patchstore/s3"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

func NewFromConfig(cfg config.Config, kv domain.KeyVault) (domain.PatchStore, error) {
    switch cfg.PatchStore {
    case "minio", "":
        return minio.New(minio.Config{
            Endpoint:  cfg.MinIOEndpoint,
            AccessKey: cfg.MinIOAccessKey,
            SecretKey: cfg.MinIOSecretKey,
            UseSSL:    cfg.MinIOUseSSL,
            KeyVault:  kv,
        })
    case "s3":
        return s3.New(s3.Config{KeyVault: kv})
    default:
        return nil, fmt.Errorf("unknown PATCH_STORE %q", cfg.PatchStore)
    }
}
```

`s3/store.go`: stub returning `domain.ErrNotImplemented` for every method. Phase 7 fills in.

### Task 5.3: Round-trip tests

`minio/store_test.go` — uses a tiny in-memory MinIO via dockertest? Skip — use `go.uber.org/mock`-style fake `domain.PatchStore`? Actually simpler: write integration-tagged tests that hit the dev MinIO:

```go
//go:build integration

package minio

// Set MINIO_ENDPOINT etc. and run with -tags=integration.
```

Tests:
- `TestMinIO_PutGetRoundTrip` — plaintext "hello"; Put; Get; assert equal.
- `TestMinIO_TamperedCiphertextFails` — Put; manually fetch raw; flip last byte; Put back; Get returns gcm.Open error.
- `TestMinIO_EnvelopeMagicRejected` — Put a body with the wrong magic; Get returns "bad magic".
- `TestMinIO_BucketIdempotent` — call EnsureBucket twice; both succeed.

Skip these in CI without MinIO (use the integration tag).

### Task 5.4: Wire PatchStore into Activities

**Files:**
- Modify: `services/control-plane/internal/workflow/activities.go`
- Modify: `services/control-plane/internal/adapter/workflow/service.go`
- Modify: `services/control-plane/cmd/server/main.go`

Extend `Activities`:

```go
type Activities struct {
    Repo       *repo.WorkflowRepo
    Broker     *sse.Broker[domain.ActivityEvent]
    PatchStore domain.PatchStore        // NEW
    Validator  ValidatorClient          // NEW — see Stage 6
}
```

Modify `BackendCodegen` to do a smoke-test round-trip:

```go
func (a *Activities) BackendCodegen(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
    activity.GetLogger(ctx).Info("Backend.Codegen begin", "run", in.RunID)
    // Phase 4 stub: produce empty patch + record it.
    patch := []byte("")
    bucket := domain.BucketForOrg(in.OrgID)
    key    := "patches/" + in.RunID + "/Backend.Codegen.patch.enc"
    if err := a.PatchStore.Put(ctx, domain.PutOptions{
        Bucket: bucket, Key: key, Body: patch, ContentType: "application/octet-stream",
    }); err != nil {
        return domain.ActivityResult{}, err
    }
    // Validator round-trip (Stage 6 wiring; for now log + sleep).
    rep, err := a.Validator.Validate(ctx, ValidateRequest{
        RepoSHA: "fixture", PatchDiff: string(patch),
    })
    if err != nil { return domain.ActivityResult{}, err }
    reportJSON, _ := json.Marshal(rep)
    _ = a.PatchStore.Put(ctx, domain.PutOptions{
        Bucket: bucket,
        Key:    "reports/" + in.RunID + "/Backend.Codegen.report.json.enc",
        Body:   reportJSON, ContentType: "application/json",
    })
    return domain.ActivityResult{
        AgentRole: domain.AgentBackend, Status: domain.ActSucceeded,
        Message: "patch stored + validated",
        Payload: map[string]any{
            "tests_passed": rep.TestsPassed,
            "coverage":     rep.Coverage,
            "patch_key":    key,
        },
    }, nil
}
```

In `main.go`, after building `kv` and before constructing `Activities`:

```go
patchStore, err := patchstore.NewFromConfig(cfg, kv)
if err != nil { logger.Error("patchstore", "err", err); os.Exit(1) }
validatorClient := validatorclient.New(validatorclient.Config{
    BaseURL: cfg.ValidatorURL, Token: cfg.ValidatorToken,
})
acts := wfpkg.NewActivities(wfRepo, wfBroker, patchStore, validatorClient)
```

### Task 5.5: Commit

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add internal/adapter/patchstore internal/workflow internal/adapter/workflow cmd/server
git commit -m "feat(patchstore): MinIO adapter + envelope encryption + Backend.Codegen wiring (stage 5)"
```

---

## Stage 6 — Validator service: `POST /v1/validate` + Docker spawn + fixture image

### Task 6.1: Validator service code

**Files:**
- Modify: `services/validator/cmd/server/main.go`
- Create: `services/validator/internal/transport/http/handler/validate.go`
- Create: `services/validator/internal/runner/docker.go`
- Create: `services/validator/internal/runner/docker_test.go`

`main.go` rewrite:

```go
package main

import (
    "log/slog"
    "net/http"
    "os"

    "github.com/go-chi/chi/v5"

    "github.com/nexis-eco/nexis/services/validator/internal/runner"
    "github.com/nexis-eco/nexis/services/validator/internal/transport/http/handler"
)

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    slog.SetDefault(logger)
    port  := envOr("PORT", "8081")
    token := envOr("VALIDATOR_TOKEN", "")
    image := envOr("NEXIS_VALIDATOR_IMAGE", "nexis/validator-fixture:latest")
    if token == "" { logger.Error("VALIDATOR_TOKEN not set"); os.Exit(1) }

    r := chi.NewRouter()
    r.Get("/healthz", handler.Healthz("validator"))
    r.Post("/v1/validate", handler.Validate(runner.NewDocker(image, logger), token, logger))

    logger.Info("validator listening", "port", port)
    if err := http.ListenAndServe(":"+port, r); err != nil { logger.Error("listen", "err", err); os.Exit(1) }
}

func envOr(k, def string) string { if v := os.Getenv(k); v != "" { return v }; return def }
```

`handler/validate.go`:

```go
package handler

import (
    "encoding/json"
    "net/http"

    "github.com/nexis-eco/nexis/services/validator/internal/runner"
)

type ValidateReq struct {
    RepoSHA   string `json:"repo_sha"`
    PatchDiff string `json:"patch_diff"`
    Image     string `json:"image,omitempty"`
    TimeoutMs int    `json:"timeout_ms,omitempty"`
}

func Validate(r runner.Runner, expectedToken string, logger *slog.Logger) http.HandlerFunc {
    return func(w http.ResponseWriter, req *http.Request) {
        if req.Header.Get("Authorization") != "Bearer "+expectedToken {
            http.Error(w, "unauthorized", 401); return
        }
        var in ValidateReq
        if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
            http.Error(w, "bad body", 400); return
        }
        res, err := r.Run(req.Context(), runner.RunRequest{
            RepoSHA: in.RepoSHA, PatchDiff: in.PatchDiff, Image: in.Image, TimeoutMs: in.TimeoutMs,
        })
        if err != nil {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(500)
            _ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "logs": res.Logs})
            return
        }
        w.Header().Set("Content-Type", "application/json")
        if !res.TestsPassed { w.WriteHeader(422) }
        _ = json.NewEncoder(w).Encode(res)
    }
}
```

`runner/docker.go`:

```go
package runner

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "os"
    "os/exec"
    "path/filepath"
    "time"

    "github.com/google/uuid"
)

type Runner interface {
    Run(ctx context.Context, in RunRequest) (RunResult, error)
}

type RunRequest struct {
    RepoSHA   string
    PatchDiff string
    Image     string  // optional override
    TimeoutMs int     // optional
}

type RunResult struct {
    TestsPassed bool   `json:"tests_passed"`
    TestCount   int    `json:"test_count"`
    FailCount   int    `json:"fail_count"`
    Coverage    float64 `json:"coverage"`
    DurationMs  int64  `json:"duration_ms"`
    Logs        string `json:"logs"`
}

type Docker struct {
    defaultImage string
    logger       *slog.Logger
}

func NewDocker(image string, logger *slog.Logger) *Docker {
    return &Docker{defaultImage: image, logger: logger}
}

func (d *Docker) Run(ctx context.Context, in RunRequest) (RunResult, error) {
    img := d.defaultImage
    if in.Image != "" { img = in.Image }
    timeout := 60 * time.Second
    if in.TimeoutMs > 0 { timeout = time.Duration(in.TimeoutMs) * time.Millisecond }

    runID := uuid.NewString()
    tmpDir := filepath.Join(os.TempDir(), "nexis-validate-"+runID)
    if err := os.MkdirAll(tmpDir, 0o700); err != nil { return RunResult{}, err }
    defer os.RemoveAll(tmpDir)
    patchPath := filepath.Join(tmpDir, "patch.diff")
    if err := os.WriteFile(patchPath, []byte(in.PatchDiff), 0o600); err != nil {
        return RunResult{}, err
    }

    cctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    started := time.Now()
    cmd := exec.CommandContext(cctx, "docker", "run",
        "--rm",
        "--network=none",
        "--read-only",
        "--tmpfs", "/tmp:rw,nosuid,size=64m",
        "--tmpfs", "/workspace:rw,nosuid,size=128m",
        "--memory=512m",
        "--cpus=1.0",
        "--pids-limit=128",
        "--security-opt=no-new-privileges",
        "--cap-drop=ALL",
        "-v", patchPath+":/workspace/patch.diff:ro",
        "-e", "CI=true",
        "--label", "nexis.validator.run-id="+runID,
        img,
        "/bin/sh", "-c",
        // git apply tolerates an empty patch; pytest-json-report writes the report.
        "cp -r /app/* /workspace/ 2>/dev/null; cd /workspace && (test -s patch.diff && git apply patch.diff || true) && pytest --json-report --json-report-file=/tmp/report.json -q ; cat /tmp/report.json",
    )
    var out, stderr bytes.Buffer
    cmd.Stdout = &out
    cmd.Stderr = &stderr
    runErr := cmd.Run()
    dur := time.Since(started).Milliseconds()

    res := RunResult{
        Logs:       out.String() + "\n--- stderr ---\n" + stderr.String(),
        DurationMs: dur,
    }
    // Parse pytest-json-report JSON from stdout.
    if dec := json.NewDecoder(bytes.NewReader(out.Bytes())); dec.More() {
        var report struct {
            Summary struct{ Passed, Failed, Total int } `json:"summary"`
        }
        if err := dec.Decode(&report); err == nil {
            res.TestCount   = report.Summary.Total
            res.FailCount   = report.Summary.Failed
            res.TestsPassed = report.Summary.Failed == 0 && report.Summary.Total > 0
            res.Coverage    = 0.0 // Phase 4 fixture has no coverage gen; phase 5 turns on coverage.py
        }
    }
    if runErr != nil {
        return res, fmt.Errorf("docker run: %w", runErr)
    }
    return res, nil
}
```

### Task 6.2: Fixture image

**Files:**
- Create: `services/validator/fixtures/Dockerfile`
- Create: `services/validator/fixtures/pyproject.toml`
- Create: `services/validator/fixtures/pytest.ini`
- Create: `services/validator/fixtures/src/nexis_fixture/__init__.py`
- Create: `services/validator/fixtures/src/nexis_fixture/api.py`
- Create: `services/validator/fixtures/tests/test_api.py`
- Create: `services/validator/fixtures/Makefile`

`fixtures/Dockerfile`:

```dockerfile
FROM python:3.12-slim
RUN apt-get update && apt-get install -y --no-install-recommends git && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir pytest pytest-json-report
WORKDIR /app
COPY pyproject.toml pytest.ini /app/
COPY src /app/src
COPY tests /app/tests
RUN cd /app && git init -q && git add . && git -c user.email=fix@nx -c user.name=fix commit -q -m "fixture base"
CMD ["pytest", "-q"]
```

`fixtures/src/nexis_fixture/api.py`:

```python
def add(a, b): return a + b
def sub(a, b): return a - b
def mul(a, b): return a * b
def safe_div(a, b):
    if b == 0: raise ValueError("division by zero")
    return a / b
```

`fixtures/tests/test_api.py` — 12 trivial pytest cases covering add/sub/mul/safe_div incl. one ValueError check.

`fixtures/Makefile`:

```makefile
.PHONY: build
build:
	docker build -t nexis/validator-fixture:latest .
```

- [ ] **Step: Build the fixture image**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/validator/fixtures && make build
docker images | grep nexis/validator-fixture  # expect 1 row
```

### Task 6.3: Validator client (control-plane side)

**Files:**
- Create: `services/control-plane/internal/adapter/validator/client.go`
- Create: `services/control-plane/internal/adapter/validator/client_test.go`

```go
package validator

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

type Config struct{ BaseURL, Token string; HTTPClient *http.Client }

type Client struct{ cfg Config }

func New(cfg Config) *Client {
    if cfg.HTTPClient == nil { cfg.HTTPClient = &http.Client{Timeout: 90 * time.Second} }
    return &Client{cfg: cfg}
}

type ValidateRequest struct {
    RepoSHA   string `json:"repo_sha"`
    PatchDiff string `json:"patch_diff"`
    Image     string `json:"image,omitempty"`
    TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type ValidateResponse struct {
    TestsPassed bool    `json:"tests_passed"`
    TestCount   int     `json:"test_count"`
    FailCount   int     `json:"fail_count"`
    Coverage    float64 `json:"coverage"`
    DurationMs  int64   `json:"duration_ms"`
    Logs        string  `json:"logs"`
}

func (c *Client) Validate(ctx context.Context, in ValidateRequest) (ValidateResponse, error) {
    body, _ := json.Marshal(in)
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/validate", bytes.NewReader(body))
    req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
    req.Header.Set("Content-Type", "application/json")
    resp, err := c.cfg.HTTPClient.Do(req)
    if err != nil { return ValidateResponse{}, err }
    defer resp.Body.Close()
    var out ValidateResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return ValidateResponse{}, fmt.Errorf("decode: %w", err)
    }
    if resp.StatusCode != 200 && resp.StatusCode != 422 {
        return out, fmt.Errorf("validator http %d", resp.StatusCode)
    }
    return out, nil
}
```

The Activities struct (Stage 5) imported this as `ValidatorClient` — that was a typedef shortcut; the real type is `*validator.Client`. Update the field name + factory accordingly.

### Task 6.4: Commit

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/validator && go build ./... && go test ./...
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add services/validator services/control-plane/internal/adapter/validator services/control-plane/internal/workflow services/control-plane/cmd/server
git commit -m "feat(validator): docker-run sandbox + fixture image + control-plane client (stage 6)"
```

---

## Stage 7 — Web: Incidents list + detail timeline + Live Demo CTA

### Task 7.1: Pipelines SDK

**Files:**
- Create: `apps/web/lib/pipelines.ts`

```ts
const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type WorkflowRunStatus = "queued"|"running"|"succeeded"|"failed"|"timed_out"|"cancelled";

export type WorkflowRun = {
  id: string;
  org_id: string;
  workspace_id: string;
  workflow_type: string;
  status: WorkflowRunStatus;
  current_step?: string;
  started_at: string;
  completed_at?: string;
  duration_ms?: number;
  error?: string;
};

export type ActivityEvent = {
  workflow_run_id: string;
  seq: number;
  agent_role: string;
  activity_name: string;
  status: "started"|"succeeded"|"failed"|"retrying"|"timed_out";
  attempt: number;
  message?: string;
  payload?: Record<string, unknown>;
  ts: string;
};

export const AGENTS_IN_ORDER = [
  "sentinel", "pathfinder", "synthesiser",
  "architect", "backend", "qa",
  "devops", "data_engineer", "approval_gate",
] as const;

export const pipelines = {
  list: async (wsId: string, limit = 50): Promise<WorkflowRun[]> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/pipelines?limit=${limit}`, { credentials: "include" });
    return r.json();
  },
  get: async (wsId: string, runId: string): Promise<{ run: WorkflowRun; events: ActivityEvent[] }> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/pipelines/${runId}`, { credentials: "include" });
    return r.json();
  },
  trigger: async (wsId: string, input: Record<string, unknown> = {}): Promise<WorkflowRun> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/pipelines`, {
      method: "POST", credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ workflow_type: "RecoveryPipeline", input }),
    });
    if (!r.ok) throw new Error(`trigger failed: ${r.status}`);
    return r.json();
  },
  triggerDemo: async (wsId: string): Promise<WorkflowRun> => {
    const r = await fetch(`${API}/v1/workspaces/${wsId}/pipelines/demo`, {
      method: "POST", credentials: "include",
    });
    if (!r.ok) throw new Error(`demo failed: ${r.status}`);
    return r.json();
  },
  events: (wsId: string, runId: string, onEvent: (e: ActivityEvent) => void, onClose?: () => void): EventSource => {
    const es = new EventSource(`${API}/v1/workspaces/${wsId}/pipelines/${runId}/events`, { withCredentials: true });
    es.onmessage = (e) => onEvent(JSON.parse(e.data));
    es.addEventListener("close", () => { es.close(); onClose?.(); });
    return es;
  },
};
```

### Task 7.2: Incidents list page

**Files:**
- Replace: `apps/web/app/(app)/console/incidents/page.tsx`
- Create: `apps/web/app/(app)/console/incidents/client.tsx`
- Create: `apps/web/components/pipelines/PipelineRunsTable.tsx`
- Create: `apps/web/components/pipelines/StatusPill.tsx`
- Create: `apps/web/components/pipelines/AgentIcon.tsx`

Server component reads the current workspace id from the `nexis_workspace` cookie + fetches `/v1/workspaces/{ws}/pipelines` server-side. Renders `<IncidentsClient initialRuns={...} />`.

Client component holds a small `useState<WorkflowRun[]>`, polls every 5 seconds (Phase 4 — no per-list SSE yet) to refresh, and renders `<PipelineRunsTable>`.

`PipelineRunsTable` columns:
- Run ID (mono, last 8 chars; click → `/console/incidents/[id]`)
- Started at (relative time)
- Status (`StatusPill`)
- Current step (`AgentIcon` + name)
- Duration (ms / s / m, formatted)

`StatusPill` colors:
- `running` → blue, pulsing
- `succeeded` → green
- `failed` / `timed_out` → red
- `cancelled` → grey
- `queued` → amber

`AgentIcon` maps `agent_role` → lucide icon (or emoji fallback): sentinel=Radar, pathfinder=Compass, synthesiser=Brain, architect=Ruler, backend=Code2, qa=Beaker, devops=Wrench, data_engineer=Database, approval_gate=GatewayShield, pipeline=Workflow.

### Task 7.3: Timeline detail page

**Files:**
- Create: `apps/web/app/(app)/console/incidents/[id]/page.tsx`
- Create: `apps/web/app/(app)/console/incidents/[id]/client.tsx`
- Create: `apps/web/components/pipelines/ActivityTimeline.tsx`

Server component reads `id` from route params + workspace from cookie; fetches `/v1/workspaces/{ws}/pipelines/{id}` server-side for initial run + events. Renders `<TimelineClient initialRun={...} initialEvents={...} workspaceId={...} />`.

Client component opens an EventSource via `pipelines.events(ws, id, onEvent, onClose)`. Maintains `events` state keyed by `seq` (dedup). Closes ES on `agent_role: 'pipeline'` terminal event or unmount.

`ActivityTimeline` renders a vertical list of 9 rows (`AGENTS_IN_ORDER`). For each agent:
- Look up most-recent event with `agent_role === role`.
- States:
  - No event yet → grey, empty circle, label dimmed.
  - `status: 'started'` → blue, pulsing dot (animate-pulse), label normal.
  - `status: 'succeeded'` → green, check icon, label normal, optional duration suffix.
  - `status: 'failed' | 'timed_out'` → red, X icon, message tooltip.
  - `status: 'retrying'` → amber, swirl icon, "attempt N/3".

Connector lines between rows light up as completed steps fan into the next.

### Task 7.4: Live Demo CTA

**Files:**
- Replace: `apps/web/app/(app)/console/live-demo/page.tsx`
- Create: `apps/web/app/(app)/console/live-demo/client.tsx`

Page header: "Live Demo — synthetic incident". A big primary button "Run synthetic incident". On click → `pipelines.triggerDemo(ws)` → on success, `router.push(`/console/incidents/${run.id}`)`. Below the button, show the last 5 demo runs (filter `triggered_by=='demo'` once Phase 6 stamps it; for Phase 4 just show last 5 runs).

### Task 7.5: Sidebar nav badge update

**Files:**
- Modify: `apps/web/components/console/Sidebar.tsx`

Remove the `Phase 4` placeholder badge next to "Incidents" (the surface is real now). Remove `Phase 6` next to "Live Demo" (the CTA works, even if the underlying workflow is stubs).

### Task 7.6: Commit

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
git add apps/web
git commit -m "feat(web): Incidents list + SSE-driven timeline + Live Demo synthetic-incident CTA (stage 7)"
```

---

## Stage 8 — E2E + Definition of Done

### Task 8.1: Boot the full stack

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
docker compose up --build -d
sleep 40   # temporal cold start
docker compose ps  # all up + healthy
```

Expected services: postgres, redis, minio, mailhog, temporal, temporal-ui, neo4j, otel-collector, prometheus, web, control-plane, validator, gitops.

Logs to grep for:

```bash
docker compose logs control-plane | grep "temporal worker registered"
# expect 1 hit with task_queue=nexis-recovery workflows=1 activities=10
docker compose logs control-plane | grep "billing provider initialised"
# Phase 3.5 sanity
docker compose logs validator | tail -10
# expect "validator listening port=8081"
```

### Task 8.2: End-to-end happy path

```bash
EMAIL="p4+$(date +%s)@example.com"
RESP=$(curl -s -i -X POST http://localhost:8080/v1/auth/signup -H "content-type: application/json" \
  -d "{\"email\":\"$EMAIL\",\"password\":\"correct-horse-battery-staple\",\"org_name\":\"P4 Co\"}")
CK=$(echo "$RESP" | grep -i 'set-cookie:.*nexis_session' | sed 's/.*nexis_session=\([^;]*\).*/\1/' | tr -d '\r')

# Create workspace (Phase 3.5)
curl -s -X POST -H "Cookie: nexis_session=$CK" -H "content-type: application/json" \
  -d '{"name":"prod","region":"us-east-1"}' http://localhost:8080/v1/workspaces | jq
sleep 7
WS=$(curl -s -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/workspaces | jq -r '.[0].id')
echo "workspace=$WS"

# Trigger a pipeline
RUN=$(curl -s -X POST -H "Cookie: nexis_session=$CK" -H "content-type: application/json" \
  -d '{"workflow_type":"RecoveryPipeline","input":{}}' \
  http://localhost:8080/v1/workspaces/$WS/pipelines | jq -r '.id')
echo "run=$RUN"

# Stream events for ~10 seconds — expect all 9 activities + Pipeline.Complete.
timeout 15 curl -s -H "Cookie: nexis_session=$CK" \
  http://localhost:8080/v1/workspaces/$WS/pipelines/$RUN/events | head -50

# Final state
curl -s -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/workspaces/$WS/pipelines/$RUN | jq '.run.status'
# expect "succeeded"
```

### Task 8.3: Verify activity rows

```bash
docker compose exec -T postgres psql -U nexis -d nexis -c \
  "SELECT seq, agent_role, activity_name, status FROM activity_events WHERE workflow_run_id='$RUN' ORDER BY seq;"
```

Expected: 19 rows = 9 `started` + 9 `succeeded` + 1 final `Pipeline.Complete` succeeded (or some retry brackets if a stub flaked).

### Task 8.4: Verify MinIO bucket + envelope encryption

```bash
docker compose exec -T minio mc alias set local http://localhost:9000 nexis nexis_dev_password
docker compose exec -T minio mc ls --recursive local/ | grep "nexis-org-"
# expect: nexis-org-<orgid>/patches/<run>/Backend.Codegen.patch.enc
#         nexis-org-<orgid>/reports/<run>/Backend.Codegen.report.json.enc

ORG=$(curl -s -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/me | jq -r '.org_id')
docker compose exec -T minio mc cat local/nexis-org-$ORG/reports/$RUN/Backend.Codegen.report.json.enc | head -c 16 | xxd
# expect first 4 bytes = 4e 58 01 00 (NX magic)
```

### Task 8.5: Verify validator sandbox isolation

```bash
# Issue a direct validator call, check the container is gone after.
curl -s -X POST http://localhost:8081/v1/validate \
  -H "Authorization: Bearer dev-validator-token-32byte" \
  -H "content-type: application/json" \
  -d '{"repo_sha":"fixture","patch_diff":""}' | jq '{tests_passed, test_count, duration_ms}'
# expect: tests_passed=true, test_count=12

docker ps -a --filter label=nexis.validator.run-id --format '{{.Status}}'
# expect empty (--rm cleared them)
```

### Task 8.6: Verify RBAC + RLS

```bash
# Create a member-role user via invite flow (reuse Phase 3 patterns).
# Then:
curl -s -o /dev/null -w "%{http_code}\n" -X POST \
  -H "Cookie: nexis_session=$MEMBER_CK" -H "content-type: application/json" \
  -d '{"workflow_type":"RecoveryPipeline"}' \
  http://localhost:8080/v1/workspaces/$WS/pipelines
# expect 403

# Cross-org leak check — second org's user fetches first org's run id.
curl -s -o /dev/null -w "%{http_code}\n" \
  -H "Cookie: nexis_session=$OTHER_ORG_CK" \
  http://localhost:8080/v1/workspaces/$WS/pipelines/$RUN
# expect 404 (RLS + workspace-ownership check)
```

### Task 8.7: Verify console UI

```bash
open http://localhost:3000/console/incidents
# expect: table with $RUN at the top, status=Succeeded.

open http://localhost:3000/console/incidents/$RUN
# expect: 9-step vertical timeline, all green checks; "duration_ms = 5...ms" in the header.

open http://localhost:3000/console/live-demo
# Click "Run synthetic incident" → redirected to /console/incidents/$NEWID,
# steps light up in order.
```

### Task 8.8: Stop-the-world checklists

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/validator    && go build ./... && go test ./...
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web              && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
```

### Task 8.9: Mark Phase 4 complete

```bash
# Edit docs/PROJECT_PLAN.md — append "— Completed YYYY-MM-DD" to the "Phase 4 — Pipeline Substrate" heading.
git add docs/PROJECT_PLAN.md
git commit -m "docs: mark Phase 4 complete"
```

---

## Definition of Done

- [ ] `docker compose up --build` brings the full stack (incl. temporal + validator) to a healthy state.
- [ ] Control-plane logs `temporal worker registered task_queue=nexis-recovery workflows=1 activities=10`.
- [ ] `POST /v1/workspaces/{ws}/pipelines` returns 202 with a run id; the workflow completes in under 60 seconds with all 9 activity_events rows succeeded.
- [ ] `temporal --address localhost:7233 workflow show --workflow-id <run-id>` shows `WORKFLOW_EXECUTION_COMPLETED`.
- [ ] MinIO bucket `nexis-org-<orgid>` exists; raw objects start with magic bytes `4e 58 01 00`; `PatchStore.Get` round-trips the original bytes.
- [ ] `POST /v1/validate` (direct to validator:8081) runs `docker run --rm --network=none --read-only --tmpfs /tmp` against the fixture image; returns `tests_passed=true, test_count=12`; the container is gone within 1 second.
- [ ] `/console/incidents` shows the workflow run row with status + current step + duration; clicking opens the per-run timeline page.
- [ ] Per-run timeline: 9 steps light up in real time via SSE; refresh mid-run shows correct cumulative state.
- [ ] Live Demo "Run synthetic incident" button triggers `/v1/workspaces/{ws}/pipelines/demo` and redirects to the timeline.
- [ ] Member-role user gets 403 on `POST /v1/workspaces/{ws}/pipelines`.
- [ ] Cross-org user gets 404 on any pipeline route for a foreign workspace (RLS + ownership check).
- [ ] `make build / test / vet / arch` green on control-plane.
- [ ] `go build / test ./...` green on validator.
- [ ] `pnpm typecheck / build` green on web.
- [ ] `docs/PROJECT_PLAN.md` Phase 4 heading carries a completion date.

---

## Risks captured

- **Docker-out-of-Docker in the validator.** Mounting `/var/run/docker.sock` into the validator container is a security footgun. Mitigation: only ever spawn `--rm --network=none --read-only` children, never expose the socket beyond compose, and Phase 7 swaps to Modal — `services/validator/Dockerfile` carries a top-of-file `# WARNING: do not deploy this container in production` comment.
- **Temporal dev-server cold start.** The auto-setup image races with `control-plane` on first boot. Mitigation: `depends_on.temporal.condition: service_healthy` in compose + the Temporal SDK's built-in retry budget (~60s) on the control-plane side. Stage 8 verification waits 40 seconds before hitting endpoints.
- **SSE replay/race when a step completes between `ListEvents` and `Subscribe`.** Mitigation: the SSE handler does replay-then-tail; clients dedup on `(run_id, seq)`. Acceptable in Phase 4; revisit in Phase 6 if double-emission becomes visible at higher throughput.
- **`workflow_runs.temporal_run_id` ambiguity.** Temporal assigns a fresh `RunID` on reset/restart; we store both `WorkflowID` (our row id) and the current `RunID`. All control-plane lookups go through the local id; we never join on `RunID`. Documented in `service.go`.
- **Bucket-per-org scaling.** Linear in tenants — fine for hundreds, ugly at 10k. S3 quota in Phase 7 supports 1000 buckets out of the box, more on request. Revisit at customer milestone.
- **Envelope wire format drift.** Locked by the 4-byte magic `NX\x01\x00`. Decoder rejects unknown magics — Phase 5+6 patches written today remain readable in Phase 7 unchanged.
- **5-minute `Backend.Codegen` timeout in dev.** Catching a dev-time hang takes the full timeout. Acceptable to keep the orchestration final for Phase 5; alternative (env-driven shrink in dev) is a tweak we can add cheaply if it bites.
- **No retry-storm protection on validator.** Worst case 6 concurrent validator calls per run (2 parallel activities × 3 attempts). Phase 4 single-user dev tolerates this. Phase 6 adds concurrency control if needed.
- **Pipeline UI uses 5-second polling for the list view.** A per-workspace SSE feed for the list would be nicer but quadruples broker complexity in Phase 4. Accepted; revisit if a customer asks.
- **Member-role can subscribe to event streams.** RBAC table allows read-only visibility. Tighten to admin+ via one-line change if a customer demands strict gating.
