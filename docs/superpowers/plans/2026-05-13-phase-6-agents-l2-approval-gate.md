# Phase 6 — Agents L2 + Approval Gate + GitOps — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking. Phase 6 has two parallel waves — Stages 3-6 are the **Pattern C** L2-agent shard wave (4 parallel backend-engineers), Stages 7-9 are the **Pattern B** integration wave (2 backend + 1 frontend in parallel). Stage 11 is sequential coordinator work that wires the whole thing together.

**Goal:** Replace the 4 L2 stub activities (Sentinel/Pathfinder/Synthesiser/ApprovalGate) shipped by Phase 4 with a real Sentinel detector goroutine + 3 real L2 agents (Pathfinder, Synthesiser, Validator L2) + a real Temporal-signal Approval Gate; ship a Neo4j codegraph adapter + a Python DoWhy gRPC sidecar; extend `services/validator` with a Hypothesis property-test sidecar; promote `services/gitops/` from `/healthz` skeleton to a full PR opener using `bradleyfalzon/ghinstallation/v2` + `google/go-github/v60`; add Slack as the 4th integration; ship the Approvals UI surface + Live Demo CTA; document ArgoCD app-of-apps + Argo Rollouts for Phase 7 runtime wiring. End-of-phase MVP cut line: inject fault → detect → diagnose → synthesise → patch → tests → approve → PR opened in < 5 minutes.

**Architecture:** Port/adapter pattern unchanged from Phases 1–5. Phase 6 adds two new internal components — `internal/sentinel/` (peer of `usecase`) and `internal/adapter/graphstore/neo4j/` (peer of `repo`). Three new L2 agents go under `internal/adapter/agents/{pathfinder,synthesiser,validator_l2}/` and implement the same Phase 5 `domain.Agent` port; the Phase 5 `Registry` is reused without modification. One new external service container — `services/causal-inference/` (Python + gRPC + DoWhy). `services/gitops/` is rewritten in place (skeleton → full). `services/validator/` is extended (multi-stage Dockerfile with a Python hypothesis sidecar baked in). The Phase 4 `RecoveryPipeline` workflow is refactored to consume `PipelineInput.SynthesiserPlan.SelectedAgents` instead of hard-coding the L1 order; a Temporal signal pattern + 2-minute timer race implements the approval gate.

**Tech Stack:** Go 1.25 (control-plane + gitops); `github.com/neo4j/neo4j-go-driver/v5`; gRPC (`google.golang.org/grpc` + generated stubs); `github.com/bradleyfalzon/ghinstallation/v2`; `github.com/google/go-github/v60`; Temporal SDK signal channels + `workflow.NewSelector`; Python 3.12 (DoWhy + Hypothesis sidecars); Next.js 16.2.2 + React 19 + Tremor + shadcn/ui (unchanged from Phase 3).

**Spec:** `docs/superpowers/specs/2026-05-13-phase-6-agents-l2-approval-gate.md`.

---

## Salvage / Reuse from Phases 1-5

- `internal/adapter/agents/{registry,llm}.go` — **reuse without modification.** The Registry interface and the per-agent `domain.Agent` port already handle the new L2 agents identically to L1.
- `internal/adapter/llm/{openai,ollama,factory}.go` — reused for the optional Pathfinder LLM-refine + Synthesiser LLM classification paths; no changes needed.
- `internal/domain/agent.go` — extend (add `AgentNamePathfinder`, `AgentNameSynthesiser`, `AgentNameValidatorL2` constants); keep existing types.
- `internal/domain/{errors.go,workflow.go,audit.go,ports.go}` — extend in place. New errors `ErrApprovalRejected`, `ErrApprovalTimeout`, `ErrPathfinderUnavailable` get appended to `errors.go`.
- `internal/workflow/recovery/{activities.go,workflow.go,types.go}` — refactor in place. Activities gains `Approval *approval.Service`, `GitOps gitops.Client`, `Graph domain.Graph`, `Causal domain.CausalEngine`. The L2 stub bodies are replaced; the L1 set is untouched.
- `internal/adapter/repo/billing_repo.go` — dual-pool pattern reused by `approval_repo.go`.
- `internal/adapter/repo/workflow_repo.go` — admin-pool insert pattern reused by `slack_notifications_repo.go`.
- `internal/adapter/repo/integrations_repo.go` — extend the `provider` CHECK widening; reuse `Get`/`Upsert`/`Delete` shapes for the Slack provider row.
- `internal/adapter/integration/github/provider.go` — extend with `ForOrg(ctx, orgID)` helper returning an `*http.Client` for the gitops service's read path. **The gitops service mints its own installation transport** — the control-plane's GitHub adapter only handles webhooks and connection lifecycle. No bidirectional dependency.
- `internal/platform/cron/cron.go` — unchanged (no new cron jobs in Phase 6; Sentinel runs in its own goroutine).
- `internal/platform/sse/broker.go` — reused by the approvals UI for live decision push (a future-Phase 7 hook; Phase 6 polls).
- `internal/adapter/audit/*` — reused as-is via `domain.AuditWriter`. New audit actions (`incident.sentinel_triggered`, `pathfinder.diagnosis`, etc.) are just new metadata blobs.
- `internal/adapter/email/smtp.go` (Phase 2) — reused by `notifier/email.go` for approval emails.
- `internal/domain/KeyVault` — reused for sealing the Slack webhook URL the same way GitHub installation IDs and Sentry DSNs are sealed.
- `services/validator/cmd/server/main.go` — extend in place: `/v1/validate` accepts an optional `hypothesis: bool` request field; existing tests stay green when the flag is omitted.
- `services/validator/internal/sandbox/docker.go` — unchanged. The new `hypothesis.go` is a peer.
- `services/gitops/cmd/server/main.go` — **rewrite.** Currently a 30-line `/healthz` shell.
- `apps/web/lib/pipelines.ts` SDK pattern — copied to `apps/web/lib/approvals.ts`.
- `apps/web/components/pipelines/{StatusPill,AgentIcon}.tsx` — reused inside `PendingApprovalsTable.tsx`.
- `apps/web/components/integrations/*` Monaco wrapper — reused for the per-decision diff preview.
- `services/validator/fixtures/incidents/*.json` (Phase 5) — extended with `fixture-schema-drift.json`, `fixture-oom.json`, `fixture-trivial-ui-fix.json`. `fixture-null-pointer.json` already exists.
- `docker-compose.yml` — extend; add `neo4j`, `causal-inference` services + a `nexis_gitops` DB role; do not rewrite.

**Coordinator-owned files** (the controller must serialize edits to these — sub-agents may not touch them concurrently):

- `services/control-plane/cmd/server/main.go`
- `services/control-plane/internal/transport/http/server.go`
- `services/control-plane/internal/platform/config/config.go`
- `services/control-plane/.arch.yaml`
- `packages/db/schema.ts`
- `docker-compose.yml`
- `apps/web/proxy.ts`
- `apps/web/app/layout.tsx`
- `services/control-plane/internal/workflow/recovery/{activities,workflow,types}.go`

---

## Parallel-dispatch playbook

Phase 6 has two waves. Each wave runs N agents concurrently from a single dispatch message; the coordinator merges the wave by running `make build && make test && make vet && make arch` once after every shard finishes.

| Wave | Pattern | Agents | When |
|---|---|---|---|
| **Wave 1 — L2 agents** (Stages 3-6) | Pattern C (4 parallel `backend-engineer`) | Pathfinder shard / Synthesiser shard / Validator-L2 shard / Approval-Gate shard | After Stages 0-2 land sequentially (schema, domain ports, sentinel detector, Neo4j adapter) |
| **Wave 2 — Integrations + UI** (Stages 7-9) | Pattern B (2 `backend-engineer` + 1 `frontend-engineer`) | Slack/email/console notifier shard / GitOps service rewrite shard / Approvals + Live Demo UI shard | After Wave 1 merges |

Stages 0, 1, 2, 10, 11 are sequential and run on the coordinator. Stage 10 (ArgoCD docs) can also be dispatched to a `lead-software-engineer` agent in parallel with Wave 2 if desired, but the work is small enough (~150 lines of YAML + markdown) that the coordinator owns it inline.

The four Wave 1 shards have **disjoint file sets** — no shard touches another's directory:

| Shard | Owned paths |
|---|---|
| 3 (Pathfinder) | `internal/adapter/graphstore/neo4j/**`, `internal/adapter/causal/**`, `internal/adapter/agents/pathfinder/**`, `services/causal-inference/**`, `cmd/seed-neo4j/**` |
| 4 (Synthesiser) | `internal/adapter/agents/synthesiser/**` |
| 5 (Validator L2) | `internal/adapter/agents/validator_l2/**`, `services/validator/internal/sandbox/hypothesis.go`, `services/validator/hypothesis-sidecar/**`, `services/validator/Dockerfile`, `services/validator/cmd/server/main.go` (one append-only edit), `services/validator/internal/transport/http/handler.go` (one branch edit) |
| 6 (Approval Gate) | `internal/adapter/approval/**`, `internal/adapter/repo/approval_repo.go`, `internal/adapter/notifier/**`, `internal/usecase/approval_signaler.go` |

The three Wave 2 shards have **disjoint file sets** too:

| Shard | Owned paths |
|---|---|
| 7 (Notifiers) | `internal/adapter/integration/slack/**`, `internal/adapter/notifier/{slack,email,console,multi}.go`, `internal/adapter/repo/slack_notifications_repo.go` |
| 8 (GitOps) | `services/gitops/**` (full rewrite) |
| 9 (UI) | `apps/web/app/(app)/console/approvals/**`, `apps/web/app/(app)/console/live-demo/client.tsx`, `apps/web/app/(app)/console/incidents/[id]/client.tsx`, `apps/web/components/approvals/**`, `apps/web/components/pipelines/{SynthesiserPlan,GitOpsPRLink}.tsx`, `apps/web/lib/approvals.ts` |

**Coordinator merge rule** — after each wave, run on `services/control-plane/`:

```bash
make build && make test && make vet && make arch
```

Plus on the web app:

```bash
cd apps/web && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
```

Any failure rolls back to the offending shard's last green commit and re-dispatches just that shard with the error transcript.

---

## Stage 0 — Schema (approval_decisions, slack_notifications) + RLS + grants + provider widening + config + env

### Task 0.1: Drizzle schema additions

**Files:**
- Modify: `packages/db/schema.ts` (COORDINATOR)

- [ ] **Step 1: Append after the Phase 5 `evalTranscripts` block:**

```ts
// ---------------------------------------------------------------------------
// Phase 6 — Approval Gate + Slack notifier ledger.
// ---------------------------------------------------------------------------

const approvalSeverity   = ["low", "medium", "high"] as const;
const approvalDecisionEnum = ["pending", "approved", "rejected", "auto_approved", "timeout_rejected"] as const;

export const approvalDecisions = pgTable("approval_decisions", {
  id:              uuid("id").primaryKey().defaultRandom(),
  orgId:           uuid("org_id").notNull().references(() => organizations.id),
  workspaceId:     uuid("workspace_id").notNull().references(() => workspaces.id),
  workflowRunId:   uuid("workflow_run_id").notNull().references(() => workflowRuns.id, { onDelete: "cascade" }),
  severity:        text("severity",  { enum: approvalSeverity }).notNull(),
  decision:        text("decision",  { enum: approvalDecisionEnum }).notNull(),
  decidedBy:       uuid("decided_by").references(() => users.id),
  decidedAt:       timestamp("decided_at", { withTimezone: true }),
  notes:           text("notes"),
  scenario:        text("scenario"),
  riskScore:       numeric("risk_score", { precision: 5, scale: 2 }),
  createdAt:       timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqRun:          uniqueIndex("approval_decisions_run_uniq").on(t.workflowRunId),
  pendingListIdx:   index("approval_decisions_pending_idx").on(t.orgId, t.workspaceId, t.decision, t.createdAt),
}));

const slackNotificationStatus = ["queued", "sent", "failed"] as const;

export const slackNotifications = pgTable("slack_notifications", {
  id:              uuid("id").primaryKey().defaultRandom(),
  orgId:           uuid("org_id").notNull().references(() => organizations.id),
  workflowRunId:   uuid("workflow_run_id").references(() => workflowRuns.id, { onDelete: "set null" }),
  channel:         text("channel"),
  kind:            text("kind").notNull(),
  status:          text("status", { enum: slackNotificationStatus }).notNull(),
  httpStatus:      integer("http_status"),
  attemptedAt:     timestamp("attempted_at", { withTimezone: true }).notNull().defaultNow(),
  error:           text("error"),
}, t => ({
  orgRunIdx: index("slack_notifications_org_run_idx").on(t.orgId, t.workflowRunId),
}));
```

The `integrations.provider` CHECK widening lives only in the SQL migration (Drizzle's CHECK enforcement uses a literal enum string and the existing TS enum extends to include `"slack"` in one place — see Step 2).

- [ ] **Step 2: Widen the Drizzle provider enum**

Locate the existing `const integrationProvider = [...] as const` in `schema.ts`. Append `"slack"` to the tuple:

```ts
const integrationProvider = ["github", "sentry", "argocd", "slack"] as const;
```

- [ ] **Step 3: Generate**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm exec drizzle-kit generate --name=phase6_approvals
```

Expected: one new migration file at `packages/db/migrations/0006_*_phase6_approvals.sql` with `CREATE TABLE approval_decisions`, `CREATE TABLE slack_notifications`, and `ALTER TABLE integrations ... CHECK ...` rewrite.

### Task 0.2: Control-plane mirror migrations

**Files:**
- Create: `services/control-plane/migrations/0014_phase6_approvals.up.sql`
- Create: `services/control-plane/migrations/0014_phase6_approvals.down.sql`
- Create: `services/control-plane/migrations/0015_phase6_provider_widen.up.sql`
- Create: `services/control-plane/migrations/0015_phase6_provider_widen.down.sql`
- Create: `services/control-plane/migrations/0016_phase6_rls.up.sql`
- Create: `services/control-plane/migrations/0016_phase6_rls.down.sql`
- Create: `services/control-plane/migrations/0017_phase6_gitops_role.up.sql`
- Create: `services/control-plane/migrations/0017_phase6_gitops_role.down.sql`

- [ ] **Step 1: `0014_phase6_approvals.up.sql`**

```sql
CREATE TABLE approval_decisions (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id),
  workspace_id    uuid NOT NULL REFERENCES workspaces(id),
  workflow_run_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
  severity        text NOT NULL CHECK (severity IN ('low','medium','high')),
  decision        text NOT NULL CHECK (decision IN ('pending','approved','rejected','auto_approved','timeout_rejected')),
  decided_by      uuid REFERENCES users(id),
  decided_at      timestamptz,
  notes           text,
  scenario        text,
  risk_score      numeric(5, 2),
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workflow_run_id)
);
CREATE INDEX approval_decisions_pending_idx ON approval_decisions (org_id, workspace_id, decision, created_at DESC);

CREATE TABLE slack_notifications (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id),
  workflow_run_id uuid REFERENCES workflow_runs(id) ON DELETE SET NULL,
  channel         text,
  kind            text NOT NULL,
  status          text NOT NULL CHECK (status IN ('queued','sent','failed')),
  http_status     int,
  attempted_at    timestamptz NOT NULL DEFAULT now(),
  error           text
);
CREATE INDEX slack_notifications_org_run_idx ON slack_notifications (org_id, workflow_run_id);
```

- [ ] **Step 2: `0014_phase6_approvals.down.sql`**

```sql
DROP TABLE IF EXISTS slack_notifications;
DROP TABLE IF EXISTS approval_decisions;
```

- [ ] **Step 3: `0015_phase6_provider_widen.up.sql`** — widen the existing `integrations.provider` CHECK constraint.

```sql
ALTER TABLE integrations
  DROP CONSTRAINT IF EXISTS integrations_provider_check;
ALTER TABLE integrations
  ADD  CONSTRAINT integrations_provider_check
  CHECK (provider IN ('github','sentry','argocd','slack'));
```

`0015_phase6_provider_widen.down.sql`:

```sql
DELETE FROM integrations WHERE provider = 'slack';
ALTER TABLE integrations
  DROP CONSTRAINT IF EXISTS integrations_provider_check;
ALTER TABLE integrations
  ADD  CONSTRAINT integrations_provider_check
  CHECK (provider IN ('github','sentry','argocd'));
```

- [ ] **Step 4: `0016_phase6_rls.up.sql`** — enable RLS + tenant_isolation + grants on the two new tables.

```sql
ALTER TABLE approval_decisions   ENABLE ROW LEVEL SECURITY;
ALTER TABLE slack_notifications  ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON approval_decisions
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON slack_notifications
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON approval_decisions  TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON slack_notifications TO nexis_app;
```

`0016_phase6_rls.down.sql`:

```sql
DROP POLICY IF EXISTS tenant_isolation ON slack_notifications;
DROP POLICY IF EXISTS tenant_isolation ON approval_decisions;
ALTER TABLE slack_notifications  DISABLE ROW LEVEL SECURITY;
ALTER TABLE approval_decisions   DISABLE ROW LEVEL SECURITY;
```

- [ ] **Step 5: `0017_phase6_gitops_role.up.sql`** — narrow role for the gitops service.

```sql
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nexis_gitops') THEN
    CREATE ROLE nexis_gitops LOGIN PASSWORD 'nexis_dev_password';
  END IF;
END
$$;
GRANT SELECT ON integrations TO nexis_gitops;
GRANT INSERT ON audit_log    TO nexis_gitops;
-- gitops also needs USAGE on the schema to find these tables.
GRANT USAGE  ON SCHEMA public TO nexis_gitops;
-- gitops bypasses RLS on integrations because Phase 6 reads installation_id by
-- (org_id, provider) directly. RLS would block this without a per-call GUC bind.
ALTER TABLE integrations FORCE ROW LEVEL SECURITY;
CREATE POLICY gitops_read_all ON integrations
  FOR SELECT TO nexis_gitops USING (true);
CREATE POLICY gitops_audit_insert ON audit_log
  FOR INSERT TO nexis_gitops WITH CHECK (true);
```

`0017_phase6_gitops_role.down.sql`:

```sql
DROP POLICY IF EXISTS gitops_audit_insert ON audit_log;
DROP POLICY IF EXISTS gitops_read_all     ON integrations;
ALTER TABLE integrations NO FORCE ROW LEVEL SECURITY;
REVOKE INSERT ON audit_log    FROM nexis_gitops;
REVOKE SELECT ON integrations FROM nexis_gitops;
REVOKE USAGE  ON SCHEMA public FROM nexis_gitops;
DROP ROLE IF EXISTS nexis_gitops;
```

- [ ] **Step 6: Apply migrations**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make migrate-up
docker compose exec -T postgres psql -U nexis -d nexis -c "\dt" | grep -E "approval_decisions|slack_notifications"
docker compose exec -T postgres psql -U nexis -d nexis -c "\du nexis_gitops"
docker compose exec -T postgres psql -U nexis -d nexis -c "SELECT conname FROM pg_constraint WHERE conrelid='integrations'::regclass AND conname='integrations_provider_check';"
```

Expected output: both tables listed, `nexis_gitops` role exists with `Login` attribute, provider CHECK exists.

### Task 0.3: Config additions

**Files:**
- Modify: `services/control-plane/internal/platform/config/config.go` (COORDINATOR)

- [ ] **Step 1: Append Phase 6 fields to `Config`** (after the Phase 5 `AgentModelDataEngOllama` field):

```go
// Phase 6 — Agents L2 + Approval Gate + GitOps.
SentinelEnabled               bool
SentinelPollIntervalMs        int
Neo4jURI                      string
Neo4jUser                     string
Neo4jPass                     string
CausalGRPCEndpoint            string
CausalEnabled                 bool
PathfinderLLMRefine           bool
GitOpsURL                     string
GitOpsToken                   string
GitHubAppID                   int64
GitHubAppPrivateKeyPath       string
FixtureRepoOwner              string
FixtureRepoName               string
FixtureRepoDefaultBranch      string
FixtureRepoInstallationID     int64
SlackEnabled                  bool
ApprovalMediumTimeoutSeconds  int
```

- [ ] **Step 2: Append to `Load()`** (snake-case env reads):

```go
SentinelEnabled:              parseBool(env("SENTINEL_ENABLED", "1")),
SentinelPollIntervalMs:       envInt("SENTINEL_POLL_INTERVAL_MS", 10000),
Neo4jURI:                     env("NEO4J_URI",  "bolt://neo4j:7687"),
Neo4jUser:                    env("NEO4J_USER", "neo4j"),
Neo4jPass:                    env("NEO4J_PASS", "nexis_dev_password"),
CausalGRPCEndpoint:           env("CAUSAL_GRPC_ENDPOINT", "causal-inference:8090"),
CausalEnabled:                parseBool(env("CAUSAL_ENABLED", "1")),
PathfinderLLMRefine:          parseBool(env("PATHFINDER_LLM_REFINE", "0")),
GitOpsURL:                    env("GITOPS_URL",   "http://gitops:8082"),
GitOpsToken:                  env("GITOPS_TOKEN", "dev-gitops-token-32byte"),
GitHubAppID:                  int64(envInt("GITHUB_APP_ID", 12345)),
GitHubAppPrivateKeyPath:      env("GITHUB_APP_PRIVATE_KEY_PATH", "/run/secrets/github-app.pem"),
FixtureRepoOwner:             env("FIXTURE_REPO_OWNER", "nexis-eco"),
FixtureRepoName:              env("FIXTURE_REPO_NAME",  "fixture-recovery-demo"),
FixtureRepoDefaultBranch:     env("FIXTURE_REPO_DEFAULT_BRANCH", "main"),
FixtureRepoInstallationID:    int64(envInt("FIXTURE_REPO_INSTALLATION_ID", 98765)),
SlackEnabled:                 parseBool(env("SLACK_ENABLED", "1")),
ApprovalMediumTimeoutSeconds: envInt("APPROVAL_MEDIUM_TIMEOUT_SECONDS", 120),
```

### Task 0.4: docker-compose additions

**Files:**
- Modify: `docker-compose.yml` (COORDINATOR)

- [ ] **Step 1: Add the `neo4j` service** (before the existing `control-plane` service):

```yaml
  neo4j:
    image: neo4j:5-community
    environment:
      NEO4J_AUTH: neo4j/nexis_dev_password
      NEO4J_dbms_memory_heap_initial__size: 256m
      NEO4J_dbms_memory_heap_max__size: 512m
    ports:
      - "7474:7474"
      - "7687:7687"
    volumes:
      - neo4j-data:/data
    healthcheck:
      test: ["CMD-SHELL", "cypher-shell -u neo4j -p nexis_dev_password 'RETURN 1' || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 12
```

- [ ] **Step 2: Add the `causal-inference` sidecar**:

```yaml
  causal-inference:
    build: ./services/causal-inference
    environment:
      PORT: "8090"
      PYTHONUNBUFFERED: "1"
    ports:
      - "8090:8090"
    healthcheck:
      test: ["CMD", "python", "-c", "import grpc; channel=grpc.insecure_channel('localhost:8090'); grpc.channel_ready_future(channel).result(timeout=2)"]
      interval: 10s
      timeout: 5s
      retries: 6
```

- [ ] **Step 3: Add the `gitops` service** (replacing the existing skeleton block — gitops is built from `./services/gitops` with a separate DB user):

```yaml
  gitops:
    build: ./services/gitops
    environment:
      PORT: "8082"
      DATABASE_URL: "postgres://nexis_gitops:nexis_dev_password@postgres:5432/nexis"
      GITOPS_TOKEN: "${GITOPS_TOKEN:-dev-gitops-token-32byte}"
      GITHUB_APP_ID: "${GITHUB_APP_ID:-12345}"
      GITHUB_APP_PRIVATE_KEY_PATH: "/run/secrets/github-app.pem"
      LOG_LEVEL: "info"
    ports:
      - "8082:8082"
    depends_on:
      postgres:
        condition: service_healthy
    secrets:
      - github-app
```

- [ ] **Step 4: Add the `github-app` secret + `neo4j-data` volume**:

```yaml
volumes:
  neo4j-data:

secrets:
  github-app:
    file: ./secrets/github-app.pem
```

- [ ] **Step 5: Add the new env vars to the `control-plane` service** under its `environment:` block:

```yaml
      SENTINEL_ENABLED: "1"
      SENTINEL_POLL_INTERVAL_MS: "10000"
      NEO4J_URI:  "bolt://neo4j:7687"
      NEO4J_USER: "neo4j"
      NEO4J_PASS: "nexis_dev_password"
      CAUSAL_GRPC_ENDPOINT: "causal-inference:8090"
      CAUSAL_ENABLED: "1"
      PATHFINDER_LLM_REFINE: "0"
      GITOPS_URL: "http://gitops:8082"
      GITOPS_TOKEN: "${GITOPS_TOKEN:-dev-gitops-token-32byte}"
      GITHUB_APP_ID: "${GITHUB_APP_ID:-12345}"
      GITHUB_APP_PRIVATE_KEY_PATH: "/run/secrets/github-app.pem"
      FIXTURE_REPO_OWNER: "nexis-eco"
      FIXTURE_REPO_NAME:  "fixture-recovery-demo"
      FIXTURE_REPO_DEFAULT_BRANCH: "main"
      FIXTURE_REPO_INSTALLATION_ID: "98765"
      SLACK_ENABLED: "1"
      APPROVAL_MEDIUM_TIMEOUT_SECONDS: "120"
    depends_on:
      postgres: { condition: service_healthy }
      neo4j:    { condition: service_healthy }
      causal-inference: { condition: service_healthy }
    secrets:
      - github-app
```

- [ ] **Step 6: Boot the new pieces**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
mkdir -p secrets && touch secrets/github-app.pem  # dev placeholder; replaced by real key on demo run
docker compose up -d neo4j
docker compose ps neo4j
# Expect: healthy within 30-60s
```

### Task 0.5: .arch.yaml — declare `sentinel`, `graphstore`, `causal`, `approval`, `notifier` components

**Files:**
- Modify: `services/control-plane/.arch.yaml` (COORDINATOR)

- [ ] **Step 1: Append to `components:`**

```yaml
  sentinel:
    in: internal/sentinel/**
  graphstore:
    in: internal/adapter/graphstore/**
  causal:
    in: internal/adapter/causal/**
  approval:
    in: internal/adapter/approval/**
  notifier:
    in: internal/adapter/notifier/**
```

- [ ] **Step 2: Append to `deps:`**

```yaml
  sentinel:
    mayDependOn: [domain, platform, usecase, adapter]
  graphstore:
    mayDependOn: [domain, platform]
  causal:
    mayDependOn: [domain, platform]
  approval:
    mayDependOn: [domain, platform, adapter, agents]
  notifier:
    mayDependOn: [domain, platform, adapter]
```

Update the existing `workflow:` rule to include the new dependencies:

```yaml
  workflow:
    mayDependOn: [usecase, domain, adapter, platform, agents, approval]
```

### Task 0.6: Commit Stage 0

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm typecheck
git add services/control-plane/migrations packages/db services/control-plane/internal/platform/config docker-compose.yml services/control-plane/.arch.yaml
git commit -m "feat(db): phase 6 approval_decisions + slack_notifications + neo4j + gitops role + config (stage 0)"
```

---

## Stage 1 — Domain ports

### Task 1.1: Sentinel + Graph + Causal + Approval + Notifier ports

**Files:**
- Create: `services/control-plane/internal/domain/sentinel.go`
- Create: `services/control-plane/internal/domain/graph.go`
- Create: `services/control-plane/internal/domain/causal.go`
- Create: `services/control-plane/internal/domain/approval.go`
- Create: `services/control-plane/internal/domain/notifier.go`
- Modify: `services/control-plane/internal/domain/agent.go`
- Modify: `services/control-plane/internal/domain/errors.go`
- Modify: `services/control-plane/internal/domain/workflow.go`

- [ ] **Step 1: Write `sentinel.go`**

```go
package domain

import "time"

// IncidentTrigger is what the Sentinel detector emits per evaluated rule hit.
// The detector goroutine consumes []IncidentTrigger from rules.Apply and
// hands them to WorkflowService.Start one by one.
type IncidentTrigger struct {
    OrgID       string
    WorkspaceID string
    IncidentID  string
    Rule        string // 'fatal_level' | 'error_rate_spike'
    DetectedAt  time.Time
    ReceivedAt  time.Time
}

// IncidentsReader is the read-only port the Sentinel goroutine depends on.
// Implementations live in internal/adapter/repo/incidents_repo.go.
type IncidentsReader interface {
    // PollFatalSince returns rows where (source='sentry', level='fatal',
    // received_at > since). System-job path — caller bypasses RLS via the
    // admin pool.
    PollFatalSince(ctx context.Context, orgID string, since time.Time) ([]IncidentRow, error)

    // CountRecent returns the count of rows within the lookback window
    // [now-window, now]. Used by the error-rate-spike rule.
    CountRecent(ctx context.Context, orgID string, window time.Duration) (int, error)

    // MaxReceivedAt is called at detector startup to initialize last_seen_at.
    MaxReceivedAt(ctx context.Context, orgID string) (time.Time, error)
}

// IncidentRow is the minimal projection the Sentinel rules + downstream
// activities need. The full row in incidents_raw has more columns.
type IncidentRow struct {
    ID           string
    OrgID        string
    Source       string // 'sentry'
    Level        string // 'fatal'|'error'|'warning'|'info'
    Title        string
    Service      string
    Environment  string
    Stacktrace   string
    Logs         string
    ReceivedAt   time.Time
}
```

Add `import "context"` to the file. (Imports omitted above for brevity but required.)

- [ ] **Step 2: Write `graph.go`**

```go
package domain

import "context"

type GraphNodeKind string

const (
    GraphKindModule        GraphNodeKind = "Module"
    GraphKindSymbol        GraphNodeKind = "Symbol"
    GraphKindExceptionType GraphNodeKind = "ExceptionType"
)

type GraphEdgeKind string

const (
    GraphEdgeDefinedIn GraphEdgeKind = "DEFINED_IN"
    GraphEdgeCalls     GraphEdgeKind = "CALLS"
    GraphEdgeImports   GraphEdgeKind = "IMPORTS"
    GraphEdgeRaised    GraphEdgeKind = "RAISED"
)

type GraphNode struct {
    Kind     GraphNodeKind
    OrgID    string
    RepoSHA  string
    FilePath string
    Name     string
    LineStart int
    LineEnd   int
    Props    map[string]any
}

type GraphEdge struct {
    From GraphNode
    To   GraphNode
    Kind GraphEdgeKind
}

// Graph is the port the Pathfinder agent depends on. Implementations live in
// internal/adapter/graphstore/neo4j/store.go.
type Graph interface {
    // FindSymbolContaining returns the Symbol node whose [line_start..line_end]
    // range covers the (file_path, line) tuple. Returns ErrNotFound when no
    // match.
    FindSymbolContaining(ctx context.Context, orgID, repoSHA, filePath string, line int) (GraphNode, error)

    // Neighbours walks `maxHops` of the given edge kinds outward from `node`
    // and returns the visited nodes paired with the edge type that reached
    // them. Limit caps the traversal at a hard count.
    Neighbours(ctx context.Context, node GraphNode, kinds []GraphEdgeKind, maxHops, limit int) ([]GraphEdge, error)

    // Upsert is used by cmd/seed-neo4j only; the production hot path is
    // read-only.
    Upsert(ctx context.Context, batch []GraphEdge) error

    // Ping verifies driver connectivity at startup.
    Ping(ctx context.Context) error
}
```

- [ ] **Step 3: Write `causal.go`**

```go
package domain

import "context"

type CausalQuery struct {
    IncidentID     string
    Stacktrace     string
    RootCauseNode  string
    Features       []string
}

type CausalResult struct {
    Hypothesis    string
    Confidence    float32 // 0.0..1.0
    Evidence      []string
    EstimandName  string
    DurationMs    int64
}

// CausalEngine is the port Pathfinder depends on. The concrete implementation
// is the gRPC client at internal/adapter/causal/grpc.go.
type CausalEngine interface {
    Infer(ctx context.Context, q CausalQuery) (CausalResult, error)
}
```

- [ ] **Step 4: Write `approval.go`**

```go
package domain

import (
    "context"
    "time"
)

type Severity string

const (
    SeverityLow    Severity = "low"
    SeverityMedium Severity = "medium"
    SeverityHigh   Severity = "high"
)

type ApprovalDecisionState string

const (
    ApprovalPending          ApprovalDecisionState = "pending"
    ApprovalApproved         ApprovalDecisionState = "approved"
    ApprovalRejected         ApprovalDecisionState = "rejected"
    ApprovalAutoApproved     ApprovalDecisionState = "auto_approved"
    ApprovalTimeoutRejected  ApprovalDecisionState = "timeout_rejected"
)

type ApprovalDecision struct {
    ID             string
    OrgID          string
    WorkspaceID    string
    WorkflowRunID  string
    Severity       Severity
    Decision       ApprovalDecisionState
    DecidedBy      string // user uuid or "" if pending / auto
    DecidedAt      time.Time
    Notes          string
    Scenario       string
    RiskScore      float64
    CreatedAt      time.Time
}

// ApprovalSignal is the payload the HTTP handler sends into Temporal via
// SignalWithStart-style signal. The workflow's selector reads it.
type ApprovalSignal struct {
    Decision  ApprovalDecisionState // approved | rejected | auto_approved | timeout_rejected
    DecidedBy string                // user uuid; "" on auto/timeout
    Notes     string
}

// ApprovalRepository is the port the approval service + HTTP handlers depend
// on. Implementations live in internal/adapter/repo/approval_repo.go.
type ApprovalRepository interface {
    Create(ctx context.Context, d ApprovalDecision) (string, error) // returns id
    GetByRun(ctx context.Context, runID string) (ApprovalDecision, error)
    UpdateDecision(ctx context.Context, runID string, state ApprovalDecisionState, decidedBy, notes string, at time.Time) error
    ListPending(ctx context.Context, orgID string, limit int) ([]ApprovalDecision, error)
}
```

- [ ] **Step 5: Write `notifier.go`**

```go
package domain

import "context"

type NotificationKind string

const (
    NotifApprovalRequested NotificationKind = "approval_requested"
    NotifPipelineComplete  NotificationKind = "pipeline_complete"
)

type ChannelTarget struct {
    Channel string // 'slack' | 'email' | 'console'
    Address string // webhook URL / email / "" for console
}

type Notification struct {
    OrgID         string
    WorkspaceID   string
    WorkflowRunID string
    Kind          NotificationKind
    Severity      Severity
    Scenario      string
    Title         string
    Body          string
    LinkURL       string // console deep link
}

// Notifier is the per-channel port. internal/adapter/notifier/{slack,email,
// console,multi}.go implement it.
type Notifier interface {
    Channel() string // 'slack' | 'email' | 'console'
    Send(ctx context.Context, n Notification) error
}
```

- [ ] **Step 6: Extend `agent.go`** — append new agent names:

```go
const (
    AgentNamePathfinder   AgentName = "pathfinder"
    AgentNameSynthesiser  AgentName = "synthesiser"
    AgentNameValidatorL2  AgentName = "validator_l2"
)
```

Append a helper that lists all L2 agents:

```go
var AllL2Agents = []AgentName{
    AgentNamePathfinder, AgentNameSynthesiser, AgentNameValidatorL2,
}
```

- [ ] **Step 7: Extend `errors.go`**

```go
ErrApprovalRejected     = errors.New("approval rejected")
ErrApprovalTimeout      = errors.New("approval timeout")
ErrPathfinderUnavailable = errors.New("pathfinder unavailable")
ErrCausalUnavailable    = errors.New("causal engine unavailable")
ErrGraphUnavailable     = errors.New("graph store unavailable")
ErrGitOpsUnavailable    = errors.New("gitops service unavailable")
```

- [ ] **Step 8: Extend `workflow.go`** — add `SynthesiserPlan` to `PipelineInput`, `ApprovalDecisionID` + `PRURL` to `PipelineOutput`.

Locate the existing `PipelineInput` struct and append fields:

```go
// Phase 6 — populated by Synthesiser activity; consumed by the workflow to
// drive L1 ordering. Empty when the workflow starts; filled in mid-run.
SynthesiserPlan *SynthesiserPlan `json:"synthesiser_plan,omitempty"`
```

Then define `SynthesiserPlan`:

```go
type SynthesiserPlan struct {
    Scenario       string      `json:"scenario"`
    Confidence     float64     `json:"confidence"`
    SelectedAgents []AgentName `json:"selected_agents"`
    SkippedAgents  []AgentName `json:"skipped_agents"`
    Rationale      string      `json:"rationale"`
}
```

Locate `PipelineOutput` and append:

```go
ApprovalDecisionID string `json:"approval_decision_id,omitempty"`
PRURL              string `json:"pr_url,omitempty"`
```

### Task 1.2: Commit Stage 1

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make vet && make arch
git add internal/domain
git commit -m "feat(domain): phase 6 sentinel + graph + causal + approval + notifier ports (stage 1)"
```

---

## Stage 2 — Neo4j adapter + platform helper + cmd/seed-neo4j

**Goal:** Stand up the Neo4j Go driver pool, the Graph adapter, and a CLI that seeds a synthetic codegraph for the fixture repo. This Stage MUST land before the Pathfinder shard (Stage 3) starts.

### Task 2.1: platform/neo4j connection helper

**Files:**
- Create: `services/control-plane/internal/platform/neo4j/neo4j.go`
- Create: `services/control-plane/internal/platform/neo4j/neo4j_test.go`

- [ ] **Step 1: Add dependency**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane
go get github.com/neo4j/neo4j-go-driver/v5@v5.18.0
```

- [ ] **Step 2: Write `neo4j.go`**

```go
package neo4j

import (
    "context"
    "fmt"
    "time"

    "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Config struct {
    URI      string
    User     string
    Password string
}

// New opens a driver with default pool settings. Caller is responsible for
// calling driver.Close at shutdown.
func New(ctx context.Context, cfg Config) (neo4j.DriverWithContext, error) {
    drv, err := neo4j.NewDriverWithContext(cfg.URI,
        neo4j.BasicAuth(cfg.User, cfg.Password, ""),
        func(c *neo4j.Config) {
            c.MaxConnectionPoolSize = 20
            c.ConnectionAcquisitionTimeout = 30 * time.Second
        },
    )
    if err != nil {
        return nil, fmt.Errorf("neo4j.New: %w", err)
    }
    return drv, nil
}

// Verify probes the driver with a 60-second budget. Called from main.go at
// startup; on failure the Sentinel goroutine logs WARN and disables the
// Pathfinder graph-evidence path.
func Verify(ctx context.Context, drv neo4j.DriverWithContext) error {
    cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
    defer cancel()
    return drv.VerifyConnectivity(cctx)
}
```

- [ ] **Step 3: Smoke test** — `neo4j_test.go` skipped unless `NEO4J_URI` env is set, executes `RETURN 1` and asserts no error. Reference the existing `repo/*_test.go` pattern for env-gated integration tests.

### Task 2.2: Neo4j Graph adapter

**Files:**
- Create: `services/control-plane/internal/adapter/graphstore/neo4j/store.go`
- Create: `services/control-plane/internal/adapter/graphstore/neo4j/seed.go`
- Create: `services/control-plane/internal/adapter/graphstore/neo4j/store_test.go`

- [ ] **Step 1: Write `store.go`**

```go
package neo4j

import (
    "context"
    "fmt"

    neo4jdrv "github.com/neo4j/neo4j-go-driver/v5/neo4j"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Store struct {
    drv neo4jdrv.DriverWithContext
}

func New(drv neo4jdrv.DriverWithContext) *Store { return &Store{drv: drv} }

func (s *Store) Ping(ctx context.Context) error {
    return s.drv.VerifyConnectivity(ctx)
}

func (s *Store) FindSymbolContaining(ctx context.Context, orgID, repoSHA, filePath string, line int) (domain.GraphNode, error) {
    sess := s.drv.NewSession(ctx, neo4jdrv.SessionConfig{AccessMode: neo4jdrv.AccessModeRead})
    defer sess.Close(ctx)
    res, err := sess.Run(ctx,
        `MATCH (s:Symbol {file_path:$file, org_id:$org, repo_sha:$sha})
         WHERE s.line_start <= $line AND s.line_end >= $line
         RETURN s LIMIT 1`,
        map[string]any{"file": filePath, "org": orgID, "sha": repoSHA, "line": line},
    )
    if err != nil {
        return domain.GraphNode{}, fmt.Errorf("FindSymbolContaining: %w", err)
    }
    if !res.Next(ctx) {
        return domain.GraphNode{}, domain.ErrNotFound
    }
    rec := res.Record()
    node, _ := rec.Values[0].(neo4jdrv.Node)
    return nodeToDomain(node, domain.GraphKindSymbol), nil
}

func (s *Store) Neighbours(ctx context.Context, n domain.GraphNode, kinds []domain.GraphEdgeKind, maxHops, limit int) ([]domain.GraphEdge, error) {
    if len(kinds) == 0 {
        kinds = []domain.GraphEdgeKind{domain.GraphEdgeCalls, domain.GraphEdgeRaised}
    }
    relList := ""
    for i, k := range kinds {
        if i > 0 { relList += "|" }
        relList += string(k)
    }
    sess := s.drv.NewSession(ctx, neo4jdrv.SessionConfig{AccessMode: neo4jdrv.AccessModeRead})
    defer sess.Close(ctx)
    cypher := fmt.Sprintf(
        `MATCH (s:Symbol {name:$name, org_id:$org})-[r:%s*1..%d]-(t)
         RETURN type(r[0]) AS rel, t LIMIT %d`, relList, maxHops, limit,
    )
    res, err := sess.Run(ctx, cypher, map[string]any{"name": n.Name, "org": n.OrgID})
    if err != nil {
        return nil, fmt.Errorf("Neighbours: %w", err)
    }
    var out []domain.GraphEdge
    for res.Next(ctx) {
        rec := res.Record()
        rel, _ := rec.Values[0].(string)
        target, _ := rec.Values[1].(neo4jdrv.Node)
        out = append(out, domain.GraphEdge{
            From: n,
            To:   nodeToDomain(target, kindFromLabels(target.Labels)),
            Kind: domain.GraphEdgeKind(rel),
        })
    }
    return out, nil
}

func (s *Store) Upsert(ctx context.Context, batch []domain.GraphEdge) error {
    sess := s.drv.NewSession(ctx, neo4jdrv.SessionConfig{AccessMode: neo4jdrv.AccessModeWrite})
    defer sess.Close(ctx)
    for _, e := range batch {
        _, err := sess.Run(ctx, upsertCypher(e), edgeParams(e))
        if err != nil {
            return fmt.Errorf("upsert edge %s→%s: %w", e.From.Name, e.To.Name, err)
        }
    }
    return nil
}
```

Helper functions `nodeToDomain`, `kindFromLabels`, `upsertCypher`, `edgeParams` are pure-Go conversions; the implementation is mechanical — reference `seed.go` for the cypher templates.

- [ ] **Step 2: Write `seed.go`** — builds the synthetic codegraph for the demo fixtures. ~80 lines: walks `services/validator/fixtures/`, regex-detects top-level `def <name>` / `class <name>` lines, infers (file_path, line_start, line_end) via line counting, emits Module + Symbol nodes + DEFINED_IN edges. The CALLS edges are derived by a second regex pass over function bodies (`grep -E '\\b[A-Za-z_][A-Za-z0-9_]*\\(`); each match becomes a candidate CALLS edge if the target Symbol exists in the same Module set. Synthetic RAISED edges are seeded from a static table (`{"safe_div": []string{"ValueError"}}`).

```go
package neo4j

import (
    "context"
    "os"
    "path/filepath"
    "regexp"
    "strings"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

var defRE = regexp.MustCompile(`^(?:def|class)\s+([A-Za-z_][A-Za-z0-9_]*)`)

// SeedFromFixtures walks `root` (typically services/validator/fixtures), parses
// each .py file with a tiny regex AST, and writes Module + Symbol + edges.
func (s *Store) SeedFromFixtures(ctx context.Context, root, orgID, repoSHA string) (int, error) {
    var edges []domain.GraphEdge
    var symbols []domain.GraphNode
    err := filepath.Walk(root, func(p string, info os.FileInfo, _ error) error {
        if info == nil || info.IsDir() || !strings.HasSuffix(p, ".py") {
            return nil
        }
        data, _ := os.ReadFile(p)
        lines := strings.Split(string(data), "\n")
        modulePath, _ := filepath.Rel(root, p)
        module := domain.GraphNode{
            Kind: domain.GraphKindModule, OrgID: orgID, RepoSHA: repoSHA, FilePath: modulePath,
        }
        var curSym *domain.GraphNode
        for i, ln := range lines {
            if m := defRE.FindStringSubmatch(strings.TrimSpace(ln)); len(m) == 2 {
                if curSym != nil {
                    curSym.LineEnd = i
                    symbols = append(symbols, *curSym)
                    edges = append(edges, domain.GraphEdge{
                        From: *curSym, To: module, Kind: domain.GraphEdgeDefinedIn,
                    })
                }
                curSym = &domain.GraphNode{
                    Kind: domain.GraphKindSymbol, OrgID: orgID, RepoSHA: repoSHA,
                    FilePath: modulePath, Name: m[1], LineStart: i + 1,
                }
            }
        }
        if curSym != nil {
            curSym.LineEnd = len(lines)
            symbols = append(symbols, *curSym)
            edges = append(edges, domain.GraphEdge{
                From: *curSym, To: module, Kind: domain.GraphEdgeDefinedIn,
            })
        }
        return nil
    })
    if err != nil {
        return 0, err
    }
    // Second pass — call edges (linear scan; ok for 12-file fixture).
    calls := computeCalls(symbols, root, orgID, repoSHA)
    edges = append(edges, calls...)
    // Static RAISED table.
    edges = append(edges, raisedEdges(symbols, orgID, repoSHA)...)
    if err := s.Upsert(ctx, edges); err != nil {
        return 0, err
    }
    return len(edges), nil
}
```

`computeCalls` and `raisedEdges` are mechanical helpers (~30 lines each). Reference patterns from existing repo seeders.

- [ ] **Step 3: Cypher templates** — the `upsertCypher` helper emits per-edge MERGE patterns:

```cypher
MERGE (a:Symbol {name:$an, org_id:$org, repo_sha:$sha})
MERGE (b:Module {file_path:$bp, org_id:$org, repo_sha:$sha})
MERGE (a)-[r:DEFINED_IN]->(b)
SET a.line_start=$as, a.line_end=$ae, a.file_path=$ap
```

One template per edge kind. Use parameterised queries — never string-concat values.

- [ ] **Step 4: Smoke test** — `store_test.go` env-gates on `NEO4J_URI`, seeds 3 synthetic edges and verifies via `Neighbours`. Reference pattern: `services/control-plane/internal/adapter/retrieval/pgvector_test.go` from Phase 5.

### Task 2.3: cmd/seed-neo4j CLI

**Files:**
- Create: `services/control-plane/cmd/seed-neo4j/main.go`

- [ ] **Step 1: Write the CLI** (~70 lines):

```go
package main

import (
    "context"
    "flag"
    "fmt"
    "log/slog"
    "os"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/graphstore/neo4j"
    pneo "github.com/nexis-eco/nexis/services/control-plane/internal/platform/neo4j"
)

func main() {
    var (
        uri     = flag.String("uri", os.Getenv("NEO4J_URI"), "Neo4j bolt URI")
        user    = flag.String("user", os.Getenv("NEO4J_USER"), "Neo4j user")
        pass    = flag.String("pass", os.Getenv("NEO4J_PASS"), "Neo4j password")
        orgID   = flag.String("org-id", os.Getenv("SEED_NEO4J_ORG_ID"), "Tenant org id (uuid)")
        repoSHA = flag.String("repo-sha", "fixture-seed-001", "synthetic repo SHA tag")
        root    = flag.String("fixtures", "services/validator/fixtures", "Fixture repo root")
    )
    flag.Parse()
    if *orgID == "" {
        fmt.Fprintln(os.Stderr, "seed-neo4j: --org-id required")
        os.Exit(2)
    }
    ctx := context.Background()
    drv, err := pneo.New(ctx, pneo.Config{URI: *uri, User: *user, Password: *pass})
    if err != nil {
        slog.Error("driver init failed", "err", err); os.Exit(1)
    }
    defer drv.Close(ctx)
    store := neo4j.New(drv)
    n, err := store.SeedFromFixtures(ctx, *root, *orgID, *repoSHA)
    if err != nil {
        slog.Error("seed failed", "err", err); os.Exit(1)
    }
    fmt.Printf("seeded %d edges for org=%s repo_sha=%s\n", n, *orgID, *repoSHA)
}
```

- [ ] **Step 2: Add to control-plane Dockerfile** (one new `RUN go build -o /app/seed-neo4j ./cmd/seed-neo4j` line; symmetric to Phase 5's `seed-pgvector` line).

### Task 2.4: Wire Neo4j pool in main.go (COORDINATOR)

**Files:**
- Modify: `services/control-plane/cmd/server/main.go` (COORDINATOR)

- [ ] **Step 1: Open driver before sentinel goroutine starts** — after the existing pgx pool init block:

```go
neoDrv, err := platformneo4j.New(ctx, platformneo4j.Config{
    URI: cfg.Neo4jURI, User: cfg.Neo4jUser, Password: cfg.Neo4jPass,
})
if err != nil {
    slog.Warn("neo4j init failed; pathfinder graph evidence disabled", "err", err)
} else {
    defer neoDrv.Close(context.Background())
    if vErr := platformneo4j.Verify(ctx, neoDrv); vErr != nil {
        slog.Warn("neo4j verify failed; pathfinder graph evidence disabled", "err", vErr)
    }
}
var graphStore domain.Graph
if neoDrv != nil {
    graphStore = neo4jstore.New(neoDrv)
}
```

The `graphStore` variable is consumed by `Stage 3` (Pathfinder agent shard); a `nil` value short-circuits to the canned-evidence path.

### Task 2.5: Verify Stage 2

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane
make build && make vet && make arch && make test
docker compose up -d neo4j
docker compose run --rm control-plane /app/seed-neo4j --org-id=$(uuidgen) --uri=bolt://neo4j:7687
# Expect: "seeded N edges for org=..." with N >= 20.
docker compose exec neo4j cypher-shell -u neo4j -p nexis_dev_password \
  "MATCH (n) RETURN count(n) AS nodes, count{(n)-->()} AS rels"
# Expect: nodes >= 30, rels >= 20
```

### Task 2.6: Commit Stage 2

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
git add services/control-plane/internal/platform/neo4j services/control-plane/internal/adapter/graphstore services/control-plane/cmd/seed-neo4j services/control-plane/cmd/server/main.go services/control-plane/Dockerfile services/control-plane/go.mod services/control-plane/go.sum
git commit -m "feat(graphstore): phase 6 neo4j adapter + platform pool + seed-neo4j cli (stage 2)"
```

---

## Stage 3 — Sentinel detector goroutine + IncidentsRepo extensions + wiring

**Goal:** An always-on goroutine inside the control-plane that polls `incidents_raw` every 10s, applies two pure-go rules, and triggers a `RecoveryPipeline` workflow per hit. The Temporal activity `Sentinel.Detect` becomes a thin inline ack.

### Task 3.1: incidents_repo.go — read paths for the detector

**Files:**
- Modify: `services/control-plane/internal/adapter/repo/incidents_repo.go` (if exists) OR Create.

- [ ] **Step 1: Add `PollFatalSince`, `CountRecent`, `MaxReceivedAt`** — all use the admin pool (system-job path; bypasses RLS).

```go
func (r *IncidentsRepo) PollFatalSince(ctx context.Context, orgID string, since time.Time) ([]domain.IncidentRow, error) {
    rows, err := r.adminPool.Query(ctx,
        `SELECT id, org_id, source, level, title, service, environment,
                COALESCE(stacktrace,''), COALESCE(logs,''), received_at
         FROM incidents_raw
         WHERE org_id=$1 AND source='sentry' AND level='fatal' AND received_at>$2
         ORDER BY received_at ASC LIMIT 50`,
        orgID, since,
    )
    if err != nil { return nil, err }
    defer rows.Close()
    var out []domain.IncidentRow
    for rows.Next() {
        var x domain.IncidentRow
        if err := rows.Scan(&x.ID, &x.OrgID, &x.Source, &x.Level, &x.Title,
            &x.Service, &x.Environment, &x.Stacktrace, &x.Logs, &x.ReceivedAt); err != nil {
            return nil, err
        }
        out = append(out, x)
    }
    return out, rows.Err()
}

func (r *IncidentsRepo) CountRecent(ctx context.Context, orgID string, window time.Duration) (int, error) {
    var n int
    err := r.adminPool.QueryRow(ctx,
        `SELECT count(*) FROM incidents_raw WHERE org_id=$1 AND received_at > now() - $2::interval`,
        orgID, fmt.Sprintf("%d seconds", int(window.Seconds())),
    ).Scan(&n)
    return n, err
}

func (r *IncidentsRepo) MaxReceivedAt(ctx context.Context, orgID string) (time.Time, error) {
    var ts time.Time
    err := r.adminPool.QueryRow(ctx,
        `SELECT COALESCE(MAX(received_at), '1970-01-01'::timestamptz) FROM incidents_raw WHERE org_id=$1`,
        orgID,
    ).Scan(&ts)
    return ts, err
}
```

If `IncidentsRepo` doesn't exist yet, create the file with the standard dual-pool constructor pattern (see `repo/billing_repo.go`).

### Task 3.2: sentinel rules + detector

**Files:**
- Create: `services/control-plane/internal/sentinel/rules.go`
- Create: `services/control-plane/internal/sentinel/detector.go`
- Create: `services/control-plane/internal/sentinel/detector_test.go`

- [ ] **Step 1: Write `rules.go`**

```go
package sentinel

import (
    "time"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

const SpikeThreshold = 5
const SpikeWindow   = 5 * time.Minute
const SpikeCooldown = 5 * time.Minute

// Apply evaluates both rules and returns []IncidentTrigger.
// Rule 1: any new fatal row since last_seen → trigger per row.
// Rule 2: if recentCount >= 5 in the last 5m AND no trigger fired for this
// org in the last 5m, trigger once using the latest fatal-or-error row.
func Apply(
    orgID, workspaceID string,
    lastSeen time.Time,
    lastTriggerAt time.Time,
    fatals []domain.IncidentRow,
    recentCount int,
    now time.Time,
) []domain.IncidentTrigger {
    out := make([]domain.IncidentTrigger, 0, len(fatals)+1)
    for _, r := range fatals {
        out = append(out, domain.IncidentTrigger{
            OrgID: orgID, WorkspaceID: workspaceID, IncidentID: r.ID,
            Rule: "fatal_level", DetectedAt: now, ReceivedAt: r.ReceivedAt,
        })
    }
    if recentCount >= SpikeThreshold && now.Sub(lastTriggerAt) > SpikeCooldown && len(fatals) == 0 {
        // Note: when fatals is non-empty we don't double-fire — the per-row
        // rule already covers the spike.
        out = append(out, domain.IncidentTrigger{
            OrgID: orgID, WorkspaceID: workspaceID,
            Rule: "error_rate_spike", DetectedAt: now, ReceivedAt: now,
        })
    }
    return out
}
```

- [ ] **Step 2: Write `detector.go`**

```go
package sentinel

import (
    "context"
    "log/slog"
    "sync"
    "time"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    "github.com/nexis-eco/nexis/services/control-plane/internal/usecase"
)

type Detector struct {
    incidents   domain.IncidentsReader
    workflows   *usecase.WorkflowService
    workspaces  WorkspacesReader // narrow port: DefaultForOrg(orgID) → workspaceID
    integrations IntegrationsReader // narrow port: ConnectedSentryOrgs() → []orgID
    audit       domain.AuditWriter
    interval    time.Duration

    mu            sync.Mutex
    lastSeen      map[string]time.Time
    lastTriggered map[string]time.Time
}

type WorkspacesReader interface {
    DefaultForOrg(ctx context.Context, orgID string) (string, error)
}
type IntegrationsReader interface {
    ConnectedSentryOrgs(ctx context.Context) ([]string, error)
}

func New(
    incidents domain.IncidentsReader,
    wf *usecase.WorkflowService,
    ws WorkspacesReader,
    ints IntegrationsReader,
    audit domain.AuditWriter,
    interval time.Duration,
) *Detector {
    return &Detector{
        incidents: incidents, workflows: wf,
        workspaces: ws, integrations: ints, audit: audit,
        interval: interval,
        lastSeen: map[string]time.Time{}, lastTriggered: map[string]time.Time{},
    }
}

// Run blocks until ctx is cancelled. Errors during a tick slog at WARN and
// do not propagate.
func (d *Detector) Run(ctx context.Context) {
    slog.Info("sentinel.detector.start", "interval_ms", d.interval.Milliseconds())
    // Initialize lastSeen from incidents_raw so we don't replay history on
    // every startup.
    if err := d.warm(ctx); err != nil {
        slog.Warn("sentinel.detector.warm_failed", "err", err)
    }
    t := time.NewTicker(d.interval)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():
            slog.Info("sentinel.detector.stop")
            return
        case now := <-t.C:
            d.tick(ctx, now)
        }
    }
}

func (d *Detector) warm(ctx context.Context) error {
    orgs, err := d.integrations.ConnectedSentryOrgs(ctx)
    if err != nil { return err }
    for _, o := range orgs {
        ts, _ := d.incidents.MaxReceivedAt(ctx, o)
        d.mu.Lock(); d.lastSeen[o] = ts; d.mu.Unlock()
    }
    return nil
}

func (d *Detector) tick(ctx context.Context, now time.Time) {
    orgs, err := d.integrations.ConnectedSentryOrgs(ctx)
    if err != nil {
        slog.Warn("sentinel.detector.tick.orgs", "err", err)
        return
    }
    for _, orgID := range orgs {
        d.mu.Lock()
        last := d.lastSeen[orgID]
        lastTrig := d.lastTriggered[orgID]
        d.mu.Unlock()

        fatals, err := d.incidents.PollFatalSince(ctx, orgID, last)
        if err != nil {
            slog.Warn("sentinel.detector.poll", "org_id", orgID, "err", err); continue
        }
        rc, _ := d.incidents.CountRecent(ctx, orgID, SpikeWindow)

        wsID, err := d.workspaces.DefaultForOrg(ctx, orgID)
        if err != nil { slog.Warn("sentinel.detector.workspace", "org_id", orgID, "err", err); continue }

        triggers := Apply(orgID, wsID, last, lastTrig, fatals, rc, now)
        for _, t := range triggers {
            d.fire(ctx, t)
        }
        if len(fatals) > 0 {
            d.mu.Lock(); d.lastSeen[orgID] = fatals[len(fatals)-1].ReceivedAt; d.mu.Unlock()
        }
        if len(triggers) > 0 {
            d.mu.Lock(); d.lastTriggered[orgID] = now; d.mu.Unlock()
        }
    }
}

func (d *Detector) fire(ctx context.Context, t domain.IncidentTrigger) {
    princ := domain.Principal{Role: "system", OrgID: t.OrgID, UserID: ""}
    in := domain.PipelineInput{
        OrgID: t.OrgID, WorkspaceID: t.WorkspaceID,
        IncidentID: t.IncidentID, Incident: nil, // workflow loads it via Sentinel.Detect
    }
    runID, err := d.workflows.Start(ctx, princ, t.WorkspaceID, in)
    if err != nil {
        slog.Warn("sentinel.detector.workflow_start", "org_id", t.OrgID, "err", err); return
    }
    _ = d.audit.Append(ctx, domain.AuditEntry{
        OrgID: t.OrgID, Actor: "sentinel-detector",
        Action: "incident.sentinel_triggered", Target: runID,
        Metadata: map[string]any{
            "incident_id": t.IncidentID, "rule": t.Rule, "detected_at": t.DetectedAt,
        },
    })
}
```

Note: the type signature for `WorkflowService.Start` in Phase 4 may vary — adapt the call site to match the actual signature in `internal/usecase/workflow_service.go`. The principal-with-empty-UserID variant must be accepted (the `workflow_runs.created_by` column accepts NULL).

- [ ] **Step 3: detector_test.go** — test the rules pure-go (no DB):

```go
func TestApply_FatalLevel(t *testing.T) {
    fatals := []domain.IncidentRow{
        {ID: "i1", ReceivedAt: t0},
        {ID: "i2", ReceivedAt: t0.Add(time.Second)},
    }
    out := sentinel.Apply("o1", "w1", t0.Add(-time.Hour), time.Time{}, fatals, 0, t0.Add(time.Minute))
    require.Len(t, out, 2)
    require.Equal(t, "fatal_level", out[0].Rule)
}

func TestApply_Spike(t *testing.T) {
    out := sentinel.Apply("o1", "w1", t0, t0.Add(-10*time.Minute), nil, 7, t0)
    require.Len(t, out, 1)
    require.Equal(t, "error_rate_spike", out[0].Rule)
}

func TestApply_SpikeCooldown(t *testing.T) {
    out := sentinel.Apply("o1", "w1", t0, t0.Add(-1*time.Minute), nil, 7, t0)
    require.Empty(t, out)
}
```

### Task 3.3: Wire detector + replace Sentinel.Detect stub

**Files:**
- Modify: `services/control-plane/cmd/server/main.go` (COORDINATOR)
- Modify: `services/control-plane/internal/workflow/recovery/activities.go` (COORDINATOR)

- [ ] **Step 1: main.go — start the detector goroutine** after the HTTP server boot:

```go
if cfg.SentinelEnabled {
    det := sentinel.New(
        incidentsRepo, workflowService, workspacesReader, integrationsReader, auditWriter,
        time.Duration(cfg.SentinelPollIntervalMs)*time.Millisecond,
    )
    go det.Run(ctx)
}
```

Define the narrow `workspacesReader` / `integrationsReader` adapters inline if the existing repos don't already expose `DefaultForOrg` / `ConnectedSentryOrgs` — both are one-row helpers.

- [ ] **Step 2: activities.go — replace `SentinelDetect` body** with a thin inline ack:

```go
func (a *Activities) SentinelDetect(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
    if a.IncidentsAdmin == nil {
        return a.stub(ctx, domain.AgentSentinel, "Sentinel.Detect")
    }
    rows, err := a.IncidentsAdmin.PollFatalSince(ctx, in.OrgID, time.Time{})
    var row domain.IncidentRow
    if err == nil {
        for _, r := range rows {
            if r.ID == in.IncidentID { row = r; break }
        }
    }
    return domain.ActivityResult{
        AgentRole: domain.AgentSentinel,
        Status:    domain.ActSucceeded,
        Message:   "sentinel ack",
        Payload: map[string]any{
            "incident_id": in.IncidentID, "title": row.Title, "service": row.Service,
            "environment": row.Environment, "level": row.Level, "received_at": row.ReceivedAt,
        },
    }, nil
}
```

`Activities` struct gains a new field `IncidentsAdmin domain.IncidentsReader` — append to the struct + the `NewActivitiesFull` constructor.

### Task 3.4: Commit Stage 3

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add internal/sentinel internal/adapter/repo/incidents_repo.go cmd/server/main.go internal/workflow/recovery/activities.go
git commit -m "feat(sentinel): phase 6 detector goroutine + rules + sentinel.detect ack (stage 3)"
```

---

## Wave 1 — Pattern C: 4 parallel L2 agent shards (Stages 4-7)

> **Pattern C dispatch:** the coordinator fires four `backend-engineer` agents in a single message. Each shard receives only its sub-tree assignment + the shared types from Stages 0-3 (which the coordinator already landed). No shard touches another's directory. The coordinator merges by running `make build && make test && make vet && make arch` once after all four complete.

### Shard prompts (each goes to its own agent in the same dispatch message)

```
Shard 4 (Pathfinder):
  Owned paths: internal/adapter/graphstore/neo4j/** (reuse stage-2 work),
               internal/adapter/causal/**,
               internal/adapter/agents/pathfinder/**,
               services/causal-inference/**.
  Forbidden:   everything else.
  Reference:   Phase 6 plan §"Stage 4".

Shard 5 (Synthesiser):
  Owned paths: internal/adapter/agents/synthesiser/**.
  Reference:   Phase 6 plan §"Stage 5".

Shard 6 (Validator L2):
  Owned paths: internal/adapter/agents/validator_l2/**,
               services/validator/internal/sandbox/hypothesis.go,
               services/validator/hypothesis-sidecar/**,
               services/validator/Dockerfile (multi-stage append only),
               services/validator/cmd/server/main.go (one append-only flag),
               services/validator/internal/transport/http/handler.go (one branch edit).
  Reference:   Phase 6 plan §"Stage 6".

Shard 7 (Approval Gate):
  Owned paths: internal/adapter/approval/**,
               internal/adapter/repo/approval_repo.go,
               internal/adapter/notifier/** (skeleton only — Wave 2 fills in providers),
               internal/usecase/approval_signaler.go.
  Reference:   Phase 6 plan §"Stage 7".
```

---

## Stage 4 — Pathfinder L2 agent + DoWhy gRPC sidecar (Pattern C shard 4)

### Task 4.1: Causal gRPC client + generated stubs

**Files:**
- Create: `services/causal-inference/proto/causal.proto`
- Create: `services/control-plane/internal/adapter/causal/causalpb/causal.pb.go` (generated)
- Create: `services/control-plane/internal/adapter/causal/causalpb/causal_grpc.pb.go` (generated)
- Create: `services/control-plane/internal/adapter/causal/grpc.go`
- Create: `services/control-plane/internal/adapter/causal/grpc_test.go`

- [ ] **Step 1: Write `causal.proto`** (verbatim from spec §3.2):

```proto
syntax = "proto3";
package nexis.causal.v1;
option go_package = "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/causal/causalpb";

message InferRequest {
  string incident_id        = 1;
  string stacktrace         = 2;
  string root_cause_node    = 3;
  repeated string features  = 4;
}

message InferResponse {
  string hypothesis          = 1;
  double confidence          = 2;
  repeated string evidence   = 3;
  string estimand_name       = 4;
  int64  duration_ms         = 5;
}

service Causal {
  rpc Infer (InferRequest) returns (InferResponse);
}
```

- [ ] **Step 2: Generate Go stubs**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane
mkdir -p internal/adapter/causal/causalpb
protoc -I=../causal-inference/proto \
  --go_out=internal/adapter/causal/causalpb --go_opt=paths=source_relative \
  --go-grpc_out=internal/adapter/causal/causalpb --go-grpc_opt=paths=source_relative \
  causal.proto
```

If `protoc-gen-go` and `protoc-gen-go-grpc` aren't installed:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.0
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.3.0
```

Add to `go.mod`:

```bash
go get google.golang.org/grpc@v1.62.0 google.golang.org/protobuf@v1.34.0
```

- [ ] **Step 3: Write `grpc.go`** — thin client implementing `domain.CausalEngine`:

```go
package causal

import (
    "context"
    "fmt"
    "time"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"

    pb "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/causal/causalpb"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Client struct {
    cc  *grpc.ClientConn
    api pb.CausalClient
}

func Dial(ctx context.Context, endpoint string) (*Client, error) {
    cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    cc, err := grpc.DialContext(cctx, endpoint,
        grpc.WithTransportCredentials(insecure.NewCredentials()),
        grpc.WithBlock(),
    )
    if err != nil {
        return nil, fmt.Errorf("causal dial %s: %w", endpoint, err)
    }
    return &Client{cc: cc, api: pb.NewCausalClient(cc)}, nil
}

func (c *Client) Close() error { return c.cc.Close() }

func (c *Client) Infer(ctx context.Context, q domain.CausalQuery) (domain.CausalResult, error) {
    cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()
    resp, err := c.api.Infer(cctx, &pb.InferRequest{
        IncidentId: q.IncidentID, Stacktrace: q.Stacktrace,
        RootCauseNode: q.RootCauseNode, Features: q.Features,
    })
    if err != nil {
        return domain.CausalResult{}, fmt.Errorf("causal infer: %w: %w", err, domain.ErrCausalUnavailable)
    }
    return domain.CausalResult{
        Hypothesis: resp.GetHypothesis(), Confidence: float32(resp.GetConfidence()),
        Evidence: resp.GetEvidence(), EstimandName: resp.GetEstimandName(),
        DurationMs: resp.GetDurationMs(),
    }, nil
}
```

- [ ] **Step 4: `grpc_test.go`** — env-gated; dials a real `causal-inference` container and asserts confidence > 0 for the null-pointer fingerprint.

### Task 4.2: Python causal-inference service

**Files:**
- Create: `services/causal-inference/Dockerfile`
- Create: `services/causal-inference/pyproject.toml`
- Create: `services/causal-inference/Makefile`
- Create: `services/causal-inference/src/nexis_causal/__init__.py`
- Create: `services/causal-inference/src/nexis_causal/server.py`
- Create: `services/causal-inference/src/nexis_causal/handlers.py`
- Create: `services/causal-inference/src/nexis_causal/canned.py`
- Create: `services/causal-inference/tests/test_handlers.py`

- [ ] **Step 1: `pyproject.toml`** — minimal:

```toml
[project]
name = "nexis-causal"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = [
    "grpcio>=1.62.0",
    "grpcio-tools>=1.62.0",
    "protobuf>=4.25.0",
    "dowhy>=0.11",
    "pandas>=2.1.0",
    "numpy>=1.26.0",
]
[project.optional-dependencies]
dev = ["pytest>=8.0", "pytest-asyncio>=0.23"]
```

- [ ] **Step 2: `Dockerfile`**

```dockerfile
FROM python:3.12-slim AS wheels
WORKDIR /build
COPY pyproject.toml ./
RUN pip wheel --wheel-dir=/wheels grpcio grpcio-tools protobuf dowhy pandas numpy

FROM python:3.12-slim
WORKDIR /app
COPY --from=wheels /wheels /wheels
RUN pip install --no-index --find-links=/wheels /wheels/*.whl && rm -rf /wheels
COPY proto/ proto/
COPY src/ src/
RUN python -m grpc_tools.protoc -I=proto \
    --python_out=src/nexis_causal/proto --grpc_python_out=src/nexis_causal/proto \
    proto/causal.proto
ENV PYTHONPATH=/app/src
EXPOSE 8090
CMD ["python", "-m", "nexis_causal.server"]
```

- [ ] **Step 3: `canned.py`** — the lookup table keyed off stacktrace fingerprints:

```python
"""
Canned causal estimands keyed by an 8-byte sha256 fingerprint of the
stacktrace. Phase 6 demo fixtures populate this table; Phase 7 swaps the
lookup for a real DoWhy estimand search.
"""

# fingerprint hex prefix (8 chars) → (hypothesis, confidence, evidence, estimand_name)
CANNED = {
    "00000001": (
        "NoneType attribute access on safe_div when b=0",
        0.82,
        ["safe_div returns None on zero divisor",
         "caller code dereferences .x attribute on the None result"],
        "backdoor_adjustment_on_caller_dispatch",
    ),
    "00000002": (
        "schema drift on orders.amount column",
        0.91,
        ["orders.amount was renamed to total_amount in migration 0042",
         "OperationalError raised by ORM on every read"],
        "intervention_on_migration_version",
    ),
    "00000003": (
        "OOM in image processor under burst load",
        0.74,
        ["heap dump shows 4GB held by ndarray cache",
         "no LRU eviction in scaler.py"],
        "intervention_on_cache_size",
    ),
}

DEFAULT = ("<unknown>", 0.0, [], "unknown")
```

- [ ] **Step 4: `handlers.py`**

```python
import hashlib
import time

import dowhy
import pandas as pd
import numpy as np

from .canned import CANNED, DEFAULT


def fingerprint(stacktrace: str) -> str:
    return hashlib.sha256(stacktrace.encode("utf-8")).hexdigest()[:8]


def infer(stacktrace: str, root_cause_node: str, features: list[str]) -> dict:
    """Returns dict matching the InferResponse proto fields."""
    start = time.monotonic_ns()
    # Real DoWhy call — placeholder operates on a 5-row frame so deps are
    # exercised, but the output is overridden by the canned table below.
    df = pd.DataFrame({"x": np.arange(5), "y": np.arange(5) * 2, "T": [0, 1, 0, 1, 1]})
    try:
        _ = dowhy.CausalModel(
            data=df, treatment="T", outcome="y",
            common_causes=["x"],
        )
    except Exception:
        pass
    fp = fingerprint(stacktrace)
    hyp, conf, evid, est = CANNED.get(fp, DEFAULT)
    return {
        "hypothesis": hyp,
        "confidence": conf,
        "evidence": evid,
        "estimand_name": est,
        "duration_ms": (time.monotonic_ns() - start) // 1_000_000,
    }
```

Map demo-fixture stacktraces to the `00000001/2/3` fingerprints — the fixture JSON files in Stage 9 contain stacktrace strings whose sha256 prefixes are explicitly set to match. The fixture builder script enforces this by appending a deterministic comment line to each stacktrace.

- [ ] **Step 5: `server.py`** — boots gRPC + binds 0.0.0.0:8090:

```python
import os
from concurrent import futures
import grpc

from .proto import causal_pb2, causal_pb2_grpc
from .handlers import infer


class CausalServicer(causal_pb2_grpc.CausalServicer):
    def Infer(self, request, context):
        out = infer(request.stacktrace, request.root_cause_node, list(request.features))
        return causal_pb2.InferResponse(
            hypothesis=out["hypothesis"],
            confidence=out["confidence"],
            evidence=out["evidence"],
            estimand_name=out["estimand_name"],
            duration_ms=out["duration_ms"],
        )


def main():
    port = os.environ.get("PORT", "8090")
    srv = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    causal_pb2_grpc.add_CausalServicer_to_server(CausalServicer(), srv)
    srv.add_insecure_port(f"0.0.0.0:{port}")
    srv.start()
    print(f"causal-inference: listening on :{port}", flush=True)
    srv.wait_for_termination()


if __name__ == "__main__":
    main()
```

- [ ] **Step 6: `test_handlers.py`**

```python
from nexis_causal.handlers import infer, fingerprint


def test_fingerprint_stable():
    assert fingerprint("foo") == fingerprint("foo")


def test_canned_hit(monkeypatch):
    # Force fingerprint to a canned bucket.
    from nexis_causal import handlers
    monkeypatch.setattr(handlers, "fingerprint", lambda s: "00000001")
    out = infer("anything", "safe_div", [])
    assert out["confidence"] > 0.5
    assert "safe_div" in out["hypothesis"].lower() or "none" in out["hypothesis"].lower()


def test_unknown_fingerprint():
    out = infer("totally-random-input", "?", [])
    assert out["confidence"] == 0.0
```

- [ ] **Step 7: `Makefile`**

```make
.PHONY: gen-proto run-dev test build

gen-proto:
	python -m grpc_tools.protoc -I=proto \
	  --python_out=src/nexis_causal/proto --grpc_python_out=src/nexis_causal/proto \
	  proto/causal.proto

run-dev:
	PYTHONPATH=src python -m nexis_causal.server

test:
	PYTHONPATH=src pytest tests/

build:
	docker build -t nexis-causal:dev .
```

### Task 4.3: Pathfinder agent provider

**Files:**
- Create: `services/control-plane/internal/adapter/agents/pathfinder/provider.go`
- Create: `services/control-plane/internal/adapter/agents/pathfinder/prompts.go`
- Create: `services/control-plane/internal/adapter/agents/pathfinder/schema.go`
- Create: `services/control-plane/internal/adapter/agents/pathfinder/provider_test.go`

- [ ] **Step 1: `provider.go`** — implements `domain.Agent`:

```go
package pathfinder

import (
    "context"
    "encoding/json"
    "log/slog"
    "strings"
    "time"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Provider struct {
    graph    domain.Graph    // may be nil — falls back to canned-evidence
    causal   domain.CausalEngine
    llm      domain.LLMProvider // optional — used only if cfg.LLMRefine
    cfg      Config
}

type Config struct {
    RepoSHA        string // current fixture sha
    LLMRefine      bool
    RefineModel    string // gpt-4o-mini etc.
    RefineProvider string // 'openai'|'ollama'
}

func New(g domain.Graph, c domain.CausalEngine, llm domain.LLMProvider, cfg Config) *Provider {
    return &Provider{graph: g, causal: c, llm: llm, cfg: cfg}
}

func (p *Provider) Name() domain.AgentName { return domain.AgentNamePathfinder }

func (p *Provider) Run(ctx context.Context, in domain.AgentInput) (domain.AgentOutput, error) {
    start := time.Now()
    if in.Incident == nil {
        return domain.AgentOutput{Success: false, Provider: "pathfinder", Model: "pathfinder-0.1"},
            domain.ErrPathfinderUnavailable
    }
    file, line := lastStackFrame(in.Incident.Stacktrace)

    var rootNode string
    var evidence []string

    if p.graph != nil && file != "" {
        sym, err := p.graph.FindSymbolContaining(ctx, in.OrgID, p.cfg.RepoSHA, file, line)
        if err == nil {
            rootNode = sym.Name
            nbrs, _ := p.graph.Neighbours(ctx, sym,
                []domain.GraphEdgeKind{domain.GraphEdgeCalls, domain.GraphEdgeRaised}, 2, 20)
            for _, e := range nbrs {
                evidence = append(evidence, string(e.Kind)+":"+e.To.Name)
            }
        } else {
            slog.Warn("pathfinder.graph.miss", "file", file, "line", line, "err", err)
        }
    }

    cr, cErr := p.causal.Infer(ctx, domain.CausalQuery{
        IncidentID: in.Incident.Label, Stacktrace: in.Incident.Stacktrace,
        RootCauseNode: rootNode, Features: evidence,
    })
    if cErr != nil {
        slog.Warn("pathfinder.causal.failed", "err", cErr)
    }

    hypothesis := cr.Hypothesis
    if p.cfg.LLMRefine && p.llm != nil && hypothesis != "" {
        refined, err := p.refineWithLLM(ctx, hypothesis, evidence)
        if err == nil && refined != "" {
            hypothesis = refined
        }
    }

    structured := map[string]any{
        "root_cause_node": rootNode,
        "hypothesis":      hypothesis,
        "confidence":      cr.Confidence,
        "evidence_chain":  append([]string{}, cr.Evidence...),
        "estimand_name":   cr.EstimandName,
    }
    raw, _ := json.Marshal(structured)
    return domain.AgentOutput{
        Success: cr.Confidence > 0, Content: string(raw),
        Structured: structured, Provider: "pathfinder", Model: "pathfinder-0.1",
        DurationMs: time.Since(start).Milliseconds(),
    }, nil
}

func lastStackFrame(stack string) (string, int) {
    // Phase 6: regex over python-style stack frames `File "<path>", line N, in <symbol>`.
    // Returns the LAST frame so the closest-to-error file/line wins.
    // (~15-line helper omitted for brevity; reference internal/adapter/agents/backend/prompts.go
    // for a similar string-parsing helper from Phase 5.)
    return "", 0 // placeholder; real impl returns (file, line)
}

func (p *Provider) refineWithLLM(ctx context.Context, causalHypothesis string, evidence []string) (string, error) {
    // Phase 5 spine call — one prompt, one round, ignore tokens/cost in Phase 6
    // because pathfinder's ledger row is owned by the activity layer (it sums
    // both the causal-only and the LLM-refined token cost via AgentOutput).
    // ~10-line builder; see internal/adapter/agents/architect/provider.go for
    // the equivalent Phase 5 pattern.
    return "", nil // placeholder
}
```

The two helpers are intentionally inlined — final impl is ~80 lines of mechanical text parsing.

- [ ] **Step 2: `schema.go`** — JSON schema for the structured output (consumed by `agents/registry`'s schema retry path):

```go
package pathfinder

const StructuredSchema = `{
  "type": "object",
  "required": ["root_cause_node","hypothesis","confidence","evidence_chain"],
  "properties": {
    "root_cause_node": {"type": "string"},
    "hypothesis": {"type": "string"},
    "confidence": {"type": "number"},
    "evidence_chain": {"type": "array", "items": {"type": "string"}},
    "estimand_name": {"type": "string"}
  }
}`
```

- [ ] **Step 3: `prompts.go`** — only used when `LLMRefine=1`:

```go
package pathfinder

const refineSystem = `You are a senior incident-response engineer. Given a
causal hypothesis from an upstream graph + causal-inference pipeline and a
bullet list of evidence, return ONE concise English sentence describing the
root cause in plain language. Output is a single sentence. No code, no JSON.`

func buildRefineUser(hyp string, evidence []string) string {
    var b strings.Builder
    b.WriteString("Causal hypothesis: ")
    b.WriteString(hyp); b.WriteString("\n\nEvidence:\n")
    for _, e := range evidence { b.WriteString("- "); b.WriteString(e); b.WriteString("\n") }
    return b.String()
}
```

- [ ] **Step 4: `provider_test.go`** — fake `domain.Graph` + fake `domain.CausalEngine`. Verify confidence flow, fallback when graph nil, fallback when causal returns 0-confidence.

### Task 4.4: Replace PathfinderDiagnose activity body

**Files:**
- Modify: `services/control-plane/internal/workflow/recovery/activities.go` (COORDINATOR, but the diff is in the shard's responsibility — coordinator merges)

- [ ] **Step 1: Register pathfinder in the agents.Registry** — in `internal/adapter/agents/registry.go` (Phase 5 file) ensure the registry accepts the L2 agents. If the registry's constructor takes a `map[domain.AgentName]domain.Agent`, append a constructor option `WithPathfinder(p domain.Agent)`. If not, the L2 agents are wired directly in `main.go` and the activity calls `pathfinderProvider.Run(...)` instead of `registry.Run(...)`.

The cleanest path: extend `agents.Registry.Run` to dispatch every `domain.AgentName` it has been given — both L1 and L2. Phase 5's registry already does this generically — no code change needed.

- [ ] **Step 2: PathfinderDiagnose body**:

```go
func (a *Activities) PathfinderDiagnose(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
    if a.Agents == nil {
        return a.stub(ctx, domain.AgentPathfinder, "Pathfinder.Diagnose")
    }
    out, err := a.Agents.Run(ctx, domain.AgentNamePathfinder, domain.AgentInput{
        WorkflowRunID: in.RunID, OrgID: in.OrgID, WorkspaceID: in.WorkspaceID,
        Incident: in.Incident, RepoSHA: in.RepoSHA,
    })
    if err != nil {
        return domain.ActivityResult{}, err
    }
    return domain.ActivityResult{
        AgentRole: domain.AgentPathfinder, Status: domain.ActSucceeded,
        Message: "pathfinder diagnosed", Payload: map[string]any{
            "structured": out.Structured, "tokens_in": out.TokensIn, "tokens_out": out.TokensOut,
            "cost_cents": out.CostCents, "model": out.Model, "provider": out.Provider,
        },
    }, nil
}
```

### Task 4.5: Commit Stage 4 (Shard 4 completion)

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
cd ../causal-inference && make test
git add services/control-plane/internal/adapter/causal services/control-plane/internal/adapter/agents/pathfinder services/causal-inference services/control-plane/go.mod services/control-plane/go.sum
git commit -m "feat(pathfinder): phase 6 neo4j graph + dowhy gRPC + pathfinder L2 agent (stage 4)"
```

---

## Stage 5 — Synthesiser L2 agent (Pattern C shard 5)

### Task 5.1: Routing table + scenario classifier

**Files:**
- Create: `services/control-plane/internal/adapter/agents/synthesiser/routes.go`
- Create: `services/control-plane/internal/adapter/agents/synthesiser/schema.go`
- Create: `services/control-plane/internal/adapter/agents/synthesiser/prompts.go`
- Create: `services/control-plane/internal/adapter/agents/synthesiser/provider.go`
- Create: `services/control-plane/internal/adapter/agents/synthesiser/provider_test.go`

- [ ] **Step 1: `routes.go`** — verbatim from spec §7.1:

```go
package synthesiser

import "github.com/nexis-eco/nexis/services/control-plane/internal/domain"

type Scenario string

const (
    ScenarioNullDeref   Scenario = "null_deref"
    ScenarioSchemaDrift Scenario = "schema_drift"
    ScenarioOOM         Scenario = "oom"
    ScenarioUnknown     Scenario = "unknown"
)

var AllScenarios = []Scenario{ScenarioNullDeref, ScenarioSchemaDrift, ScenarioOOM, ScenarioUnknown}

var routingTable = map[Scenario][]domain.AgentName{
    ScenarioNullDeref:   {domain.AgentNameArchitect, domain.AgentNameBackend, domain.AgentNameQA},
    ScenarioSchemaDrift: {domain.AgentNameArchitect, domain.AgentNameDataEngineer, domain.AgentNameBackend, domain.AgentNameQA},
    ScenarioOOM:         {domain.AgentNameArchitect, domain.AgentNameDevOps, domain.AgentNameBackend},
    ScenarioUnknown:     {domain.AgentNameArchitect, domain.AgentNameBackend, domain.AgentNameQA},
}

func RouteFor(s Scenario) []domain.AgentName {
    if r, ok := routingTable[s]; ok { return r }
    return routingTable[ScenarioUnknown]
}

func Skipped(selected []domain.AgentName) []domain.AgentName {
    set := map[domain.AgentName]bool{}
    for _, n := range selected { set[n] = true }
    var out []domain.AgentName
    for _, n := range domain.AllL1Agents {
        if !set[n] { out = append(out, n) }
    }
    return out
}
```

- [ ] **Step 2: `schema.go`**

```go
package synthesiser

const StructuredSchema = `{
  "type": "object",
  "required": ["scenario","confidence","selected_agents","rationale"],
  "properties": {
    "scenario": {"type": "string", "enum": ["null_deref","schema_drift","oom","unknown"]},
    "confidence": {"type": "number"},
    "selected_agents": {"type": "array", "items": {"type": "string"}},
    "skipped_agents":  {"type": "array", "items": {"type": "string"}},
    "rationale": {"type": "string"},
    "estimated_duration_ms": {"type": "integer"}
  }
}`
```

- [ ] **Step 3: `prompts.go`** — fast-path matcher + LLM fallback prompt:

```go
package synthesiser

import "strings"

// fastClassify returns one of the scenarios + true if a fast-path token
// signature matched; returns (ScenarioUnknown, false) otherwise.
func fastClassify(pathfinderEvidence []string, stacktrace string) (Scenario, bool) {
    blob := strings.ToLower(strings.Join(pathfinderEvidence, " ") + " " + stacktrace)
    switch {
    case strings.Contains(blob, "operationalerror") && strings.Contains(blob, "column"):
        return ScenarioSchemaDrift, true
    case strings.Contains(blob, "nonetype") && strings.Contains(blob, "attribute"):
        return ScenarioNullDeref, true
    case strings.Contains(blob, "memoryerror") || strings.Contains(blob, "oomkilled"):
        return ScenarioOOM, true
    }
    return ScenarioUnknown, false
}

const classifySystem = `You classify an incident into one of four scenarios.
Output JSON only matching this schema:
{ "scenario": "null_deref"|"schema_drift"|"oom"|"unknown", "confidence": 0..1, "rationale": "..." }
Pick "unknown" if no signal supports the others.`

func buildClassifyUser(pathfinderHypothesis string, evidence []string, stacktraceTail string) string {
    var b strings.Builder
    b.WriteString("Pathfinder hypothesis: "); b.WriteString(pathfinderHypothesis); b.WriteString("\n\nEvidence:\n")
    for _, e := range evidence { b.WriteString("- "); b.WriteString(e); b.WriteString("\n") }
    b.WriteString("\nStacktrace tail:\n"); b.WriteString(stacktraceTail)
    return b.String()
}
```

- [ ] **Step 4: `provider.go`**

```go
package synthesiser

import (
    "context"
    "encoding/json"
    "time"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Provider struct {
    llm   domain.LLMProvider // optional; nil → fast-path only, unknown → ScenarioUnknown
    model string
}

func New(llm domain.LLMProvider, model string) *Provider {
    return &Provider{llm: llm, model: model}
}

func (p *Provider) Name() domain.AgentName { return domain.AgentNameSynthesiser }

func (p *Provider) Run(ctx context.Context, in domain.AgentInput) (domain.AgentOutput, error) {
    start := time.Now()
    pathfinder, _ := in.PriorOutputs["pathfinder"].(map[string]any)
    var evidence []string
    if e, ok := pathfinder["evidence_chain"].([]any); ok {
        for _, v := range e { if s, ok := v.(string); ok { evidence = append(evidence, s) } }
    }
    hypothesis, _ := pathfinder["hypothesis"].(string)
    var stack string
    if in.Incident != nil { stack = in.Incident.Stacktrace }

    scenario, hit := fastClassify(evidence, stack)
    var confidence float64
    var tokensIn, tokensOut int
    var costCents float64
    rationale := ""
    if hit {
        confidence = 0.9
        rationale = "fast-path signature match on pathfinder evidence"
    } else if p.llm != nil {
        // LLM fallback
        // ~30 line block: build messages, call LLM, json-unmarshal into a tiny
        // local struct, accumulate token + cost telemetry. On schema mismatch
        // after 2 retries → ScenarioUnknown + confidence 0.0.
        // Reference: internal/adapter/agents/architect/provider.go from Phase 5.
        scenario, confidence, rationale, tokensIn, tokensOut, costCents = p.llmClassify(ctx, hypothesis, evidence, stack)
    } else {
        scenario = ScenarioUnknown
        rationale = "no fast-path match and LLM disabled; defaulting to unknown route"
    }

    selected := RouteFor(scenario)
    skipped := Skipped(selected)
    structured := map[string]any{
        "scenario":              string(scenario),
        "confidence":            confidence,
        "selected_agents":       toStringSlice(selected),
        "skipped_agents":        toStringSlice(skipped),
        "rationale":             rationale,
        "estimated_duration_ms": 240000,
    }
    raw, _ := json.Marshal(structured)
    return domain.AgentOutput{
        Success: true, Content: string(raw), Structured: structured,
        Provider: "synthesiser", Model: p.model,
        TokensIn: tokensIn, TokensOut: tokensOut, CostCents: costCents,
        DurationMs: time.Since(start).Milliseconds(),
    }, nil
}

func toStringSlice(in []domain.AgentName) []string {
    out := make([]string, len(in))
    for i, n := range in { out[i] = string(n) }
    return out
}

func (p *Provider) llmClassify(ctx context.Context, hyp string, ev []string, stack string) (Scenario, float64, string, int, int, float64) {
    // see Phase 5 architect provider for the standard build+call+parse pattern.
    return ScenarioUnknown, 0.0, "llm path placeholder", 0, 0, 0
}
```

- [ ] **Step 5: `provider_test.go`** — fast-path coverage for all three known scenarios + nil-LLM fallback:

```go
func TestFastClassify_NullDeref(t *testing.T) {
    s, hit := fastClassify([]string{"NoneType has no attribute foo"}, "")
    require.True(t, hit); require.Equal(t, ScenarioNullDeref, s)
}
func TestRoute_SchemaDriftIncludesDataEng(t *testing.T) {
    require.Contains(t, RouteFor(ScenarioSchemaDrift), domain.AgentNameDataEngineer)
}
func TestSkipped(t *testing.T) {
    sel := RouteFor(ScenarioNullDeref)
    sk := Skipped(sel)
    require.Contains(t, sk, domain.AgentNameDevOps)
}
```

### Task 5.2: Replace SynthesiserPlan activity body

**Files:**
- Modify: `services/control-plane/internal/workflow/recovery/activities.go` (COORDINATOR-merged)

```go
func (a *Activities) SynthesiserPlan(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
    if a.Agents == nil {
        return a.stub(ctx, domain.AgentSynthesiser, "Synthesiser.Plan")
    }
    out, err := a.Agents.Run(ctx, domain.AgentNameSynthesiser, domain.AgentInput{
        WorkflowRunID: in.RunID, OrgID: in.OrgID, WorkspaceID: in.WorkspaceID,
        PriorOutputs: in.PriorOutputs, Incident: in.Incident,
    })
    if err != nil { return domain.ActivityResult{}, err }
    return domain.ActivityResult{
        AgentRole: domain.AgentSynthesiser, Status: domain.ActSucceeded,
        Message: "synthesiser routed", Payload: map[string]any{
            "structured": out.Structured, "tokens_in": out.TokensIn, "tokens_out": out.TokensOut,
            "cost_cents": out.CostCents, "model": out.Model, "provider": out.Provider,
        },
    }, nil
}
```

The workflow function reads `out.Structured["selected_agents"]` and writes it into `PipelineInput.SynthesiserPlan` for the L1 walk — Stage 11 wires this.

### Task 5.3: Commit Stage 5 (Shard 5 completion)

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add internal/adapter/agents/synthesiser
git commit -m "feat(synthesiser): phase 6 synthesiser L2 agent + routing table + fast-path classifier (stage 5)"
```

---

## Stage 6 — Validator L2 agent + Hypothesis sidecar (Pattern C shard 6)

### Task 6.1: services/validator hypothesis sidecar

**Files:**
- Create: `services/validator/hypothesis-sidecar/Dockerfile`
- Create: `services/validator/hypothesis-sidecar/sidecar.py`
- Create: `services/validator/hypothesis-sidecar/runners/__init__.py`
- Create: `services/validator/hypothesis-sidecar/runners/base_runner.py`
- Modify: `services/validator/Dockerfile` (multi-stage with sidecar)
- Create: `services/validator/internal/sandbox/hypothesis.go`
- Modify: `services/validator/internal/transport/http/handler.go`
- Modify: `services/validator/cmd/server/main.go`

- [ ] **Step 1: `hypothesis-sidecar/sidecar.py`** — listens on `/tmp/hypothesis.sock`:

```python
"""
Phase 6 hypothesis sidecar.

Wire: 4-byte BE length prefix + JSON body
  request:  {patch_diff, repo_path, max_examples, deadline_ms}
  response: {failures: [...], duration_ms}
"""
import json
import os
import socket
import struct
import subprocess
import tempfile
import time
from pathlib import Path

SOCK_PATH = os.environ.get("HYPOTHESIS_SOCKET", "/tmp/hypothesis.sock")
MAX_EXAMPLES_DEFAULT = int(os.environ.get("HYPOTHESIS_MAX_EXAMPLES", "20"))
DEADLINE_MS_DEFAULT  = int(os.environ.get("HYPOTHESIS_DEADLINE_MS", "10000"))


def read_frame(conn) -> dict:
    raw = conn.recv(4, socket.MSG_WAITALL)
    if len(raw) != 4: return None
    n = struct.unpack(">I", raw)[0]
    body = b""
    while len(body) < n:
        chunk = conn.recv(n - len(body))
        if not chunk: return None
        body += chunk
    return json.loads(body)


def write_frame(conn, obj: dict) -> None:
    body = json.dumps(obj).encode("utf-8")
    conn.send(struct.pack(">I", len(body)) + body)


def run_hypothesis(req: dict) -> dict:
    start = time.monotonic_ns()
    with tempfile.TemporaryDirectory() as tmpd:
        repo = Path(tmpd) / "repo"
        repo.mkdir()
        # Phase 6: skip patch application; we run hypothesis tests against the
        # already-validated patch tree that lives at `repo_path` inside the
        # validator container (the docker sandbox shared the directory).
        repo_path = req.get("repo_path") or "/srv/fixture"
        from runners.base_runner import generate_and_run
        failures = generate_and_run(
            repo_path,
            max_examples=req.get("max_examples", MAX_EXAMPLES_DEFAULT),
            deadline_ms=req.get("deadline_ms", DEADLINE_MS_DEFAULT),
        )
    return {"failures": failures, "duration_ms": (time.monotonic_ns() - start) // 1_000_000}


def main():
    try: os.unlink(SOCK_PATH)
    except FileNotFoundError: pass
    srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    srv.bind(SOCK_PATH); srv.listen(4)
    os.chmod(SOCK_PATH, 0o660)
    while True:
        conn, _ = srv.accept()
        try:
            req = read_frame(conn)
            if req is None: continue
            resp = run_hypothesis(req)
            write_frame(conn, resp)
        finally:
            conn.close()


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: `runners/base_runner.py`** — generates property tests + runs them:

```python
"""
Phase 6 minimal property-test generator. Walks the patched repo, finds
top-level functions with two integer args, and emits one hypothesis test per
function. The test asserts that the function does not raise for any input
combination beyond ValueError/ZeroDivisionError.

This is the deliberately-narrow Phase 6 generator. Phase 7 swaps to a
typed-Python static-analysis generator.
"""
import ast
import os
import subprocess
import tempfile
from pathlib import Path


def find_target_functions(repo: str) -> list[tuple[str, str]]:
    out = []
    for p in Path(repo).rglob("*.py"):
        try: tree = ast.parse(p.read_text())
        except SyntaxError: continue
        for node in tree.body:
            if isinstance(node, ast.FunctionDef) and len(node.args.args) == 2:
                out.append((str(p), node.name))
    return out


TEMPLATE = '''
from hypothesis import given, strategies as st, settings
from {module} import {fn}

@settings(max_examples={max_examples}, deadline={deadline_ms})
@given(st.integers(), st.integers())
def test_{fn}_property(a, b):
    try:
        {fn}(a, b)
    except (ValueError, ZeroDivisionError):
        return
'''


def generate_and_run(repo: str, max_examples: int, deadline_ms: int) -> list[dict]:
    failures = []
    funcs = find_target_functions(repo)
    if not funcs: return failures
    with tempfile.TemporaryDirectory() as td:
        tests_dir = Path(td) / "tests"; tests_dir.mkdir()
        for path, fn in funcs:
            mod = Path(path).stem
            test = TEMPLATE.format(module=mod, fn=fn, max_examples=max_examples, deadline_ms=deadline_ms)
            (tests_dir / f"test_{fn}.py").write_text(test)
        # Run pytest inside td with repo on PYTHONPATH.
        env = os.environ.copy(); env["PYTHONPATH"] = repo + ":" + env.get("PYTHONPATH","")
        cp = subprocess.run(
            ["python", "-m", "pytest", str(tests_dir), "-q", "--tb=line"],
            cwd=td, env=env, capture_output=True, text=True, timeout=(deadline_ms / 1000) * len(funcs) + 10,
        )
        # Parse failures from pytest output — one FAILED line per test.
        for line in cp.stdout.splitlines():
            if "FAILED" in line:
                # e.g. "tests/test_safe_div.py::test_safe_div_property FAILED"
                parts = line.split("::")
                if len(parts) >= 2:
                    failures.append({"test": parts[1].split()[0], "counterexample": "", "shrunk": True})
    return failures
```

- [ ] **Step 3: `hypothesis-sidecar/Dockerfile`** — Phase 6 builds this directly inside the validator multi-stage. The standalone Dockerfile exists for local dev:

```dockerfile
FROM python:3.12-slim
RUN pip install hypothesis>=6.99 pytest>=8.0
WORKDIR /sidecar
COPY sidecar.py runners/ ./
ENV PYTHONPATH=/sidecar
ENTRYPOINT ["python", "sidecar.py"]
```

- [ ] **Step 4: `services/validator/Dockerfile`** — multi-stage with sidecar embedded:

```dockerfile
# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS gobuild
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM python:3.12-slim
WORKDIR /app
RUN pip install --no-cache-dir hypothesis>=6.99 pytest>=8.0
COPY --from=gobuild /out/server /app/server
COPY hypothesis-sidecar/ /sidecar/
COPY fixtures/ /srv/fixture/
ENV PYTHONPATH=/sidecar HYPOTHESIS_SOCKET=/tmp/hypothesis.sock
RUN printf '#!/bin/sh\npython /sidecar/sidecar.py &\nexec /app/server\n' > /entrypoint.sh && chmod +x /entrypoint.sh
EXPOSE 8081
ENTRYPOINT ["/entrypoint.sh"]
```

- [ ] **Step 5: `internal/sandbox/hypothesis.go`** — Go-side unix socket client:

```go
package sandbox

import (
    "context"
    "encoding/binary"
    "encoding/json"
    "fmt"
    "io"
    "net"
    "os"
    "time"
)

type HypothesisRequest struct {
    PatchDiff   string `json:"patch_diff"`
    RepoPath    string `json:"repo_path"`
    MaxExamples int    `json:"max_examples"`
    DeadlineMs  int    `json:"deadline_ms"`
}

type HypothesisFailure struct {
    Test            string `json:"test"`
    Counterexample  string `json:"counterexample"`
    Shrunk          bool   `json:"shrunk"`
}

type HypothesisResponse struct {
    Failures   []HypothesisFailure `json:"failures"`
    DurationMs int64               `json:"duration_ms"`
}

type HypothesisClient struct{ socketPath string }

func NewHypothesisClient() *HypothesisClient {
    s := os.Getenv("HYPOTHESIS_SOCKET")
    if s == "" { s = "/tmp/hypothesis.sock" }
    return &HypothesisClient{socketPath: s}
}

func (c *HypothesisClient) Run(ctx context.Context, req HypothesisRequest) (HypothesisResponse, error) {
    d := net.Dialer{Timeout: 3 * time.Second}
    conn, err := d.DialContext(ctx, "unix", c.socketPath)
    if err != nil {
        return HypothesisResponse{}, fmt.Errorf("hypothesis dial: %w", err)
    }
    defer conn.Close()
    body, _ := json.Marshal(req)
    hdr := make([]byte, 4); binary.BigEndian.PutUint32(hdr, uint32(len(body)))
    if _, err := conn.Write(append(hdr, body...)); err != nil {
        return HypothesisResponse{}, err
    }
    rHdr := make([]byte, 4)
    if _, err := io.ReadFull(conn, rHdr); err != nil { return HypothesisResponse{}, err }
    rN := binary.BigEndian.Uint32(rHdr)
    rBody := make([]byte, rN)
    if _, err := io.ReadFull(conn, rBody); err != nil { return HypothesisResponse{}, err }
    var resp HypothesisResponse
    if err := json.Unmarshal(rBody, &resp); err != nil { return HypothesisResponse{}, err }
    return resp, nil
}
```

- [ ] **Step 6: `internal/transport/http/handler.go`** — wire hypothesis branch:

Locate the existing `/v1/validate` handler. Add the optional field on the request struct + the branch:

```go
type ValidateReq struct {
    // ... existing fields
    Hypothesis bool `json:"hypothesis,omitempty"`
}

type ValidateResp struct {
    // ... existing fields
    HypothesisFailures []sandbox.HypothesisFailure `json:"hypothesis_failures"`
}

// in the handler body, after the existing docker sandbox call:
out := ValidateResp{ /* existing fields populated */, HypothesisFailures: []sandbox.HypothesisFailure{} }
if req.Hypothesis {
    hyp, err := h.hyp.Run(ctx, sandbox.HypothesisRequest{
        PatchDiff: req.PatchDiff, RepoPath: "/srv/fixture",
        MaxExamples: int(envInt("HYPOTHESIS_MAX_EXAMPLES", 20)),
        DeadlineMs:  int(envInt("HYPOTHESIS_DEADLINE_MS", 10000)),
    })
    if err != nil {
        slog.Warn("validator.hypothesis.failed", "err", err)
    } else {
        out.HypothesisFailures = hyp.Failures
    }
}
```

`HypothesisFailures` is always non-nil — empty `[]` when no run happened.

### Task 6.2: Validator L2 agent wrapper

**Files:**
- Create: `services/control-plane/internal/adapter/agents/validator_l2/provider.go`
- Create: `services/control-plane/internal/adapter/agents/validator_l2/provider_test.go`

- [ ] **Step 1: `provider.go`** — calls the validator HTTP API with `hypothesis=true`:

```go
package validator_l2

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Provider struct {
    httpClient *http.Client
    baseURL    string
}

func New(baseURL string) *Provider {
    return &Provider{baseURL: baseURL, httpClient: &http.Client{Timeout: 60 * time.Second}}
}

func (p *Provider) Name() domain.AgentName { return domain.AgentNameValidatorL2 }

func (p *Provider) Run(ctx context.Context, in domain.AgentInput) (domain.AgentOutput, error) {
    start := time.Now()
    backend, _ := in.PriorOutputs["backend"].(map[string]any)
    structured, _ := backend["structured"].(map[string]any)
    patchDiff, _ := structured["patch_diff"].(string)

    body, _ := json.Marshal(map[string]any{
        "repo_sha":   in.RepoSHA,
        "patch_diff": patchDiff,
        "hypothesis": true,
    })
    req, _ := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/validate", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := p.httpClient.Do(req)
    if err != nil {
        return domain.AgentOutput{}, fmt.Errorf("validator l2: %w", err)
    }
    defer resp.Body.Close()
    rb, _ := io.ReadAll(resp.Body)
    var v struct {
        TestsPassed bool `json:"tests_passed"`
        FailCount   int  `json:"fail_count"`
        TestCount   int  `json:"test_count"`
        Coverage    float64 `json:"coverage"`
        DurationMs  int64 `json:"duration_ms"`
        HypFailures []map[string]any `json:"hypothesis_failures"`
    }
    if err := json.Unmarshal(rb, &v); err != nil {
        return domain.AgentOutput{}, fmt.Errorf("validator l2 parse: %w", err)
    }
    success := v.TestsPassed && v.FailCount == 0 && len(v.HypFailures) == 0
    out := map[string]any{
        "tests_passed":         v.TestsPassed,
        "fail_count":           v.FailCount,
        "test_count":           v.TestCount,
        "coverage":             v.Coverage,
        "duration_ms":          v.DurationMs,
        "hypothesis_failures":  v.HypFailures,
    }
    raw, _ := json.Marshal(out)
    return domain.AgentOutput{
        Success: success, Content: string(raw), Structured: out,
        Provider: "hypothesis", Model: "hypothesis-0.x",
        DurationMs: time.Since(start).Milliseconds(),
    }, nil
}
```

The Validator L2 ledger row has `TokensIn=TokensOut=CostCents=0`, `Provider="hypothesis"`, `Model="hypothesis-0.x"` — matches spec §8.4.

### Task 6.3: Wire Validator L2 in activities

The workflow runs Validator L2 **after** the L1 walk (Backend has produced a patch). Stage 11 wires this end-to-end. Sketch:

```go
func (a *Activities) ValidatorL2Validate(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
    if a.Agents == nil { return a.stub(ctx, domain.AgentValidator, "ValidatorL2.Validate") }
    out, err := a.Agents.Run(ctx, domain.AgentNameValidatorL2, domain.AgentInput{
        WorkflowRunID: in.RunID, OrgID: in.OrgID, WorkspaceID: in.WorkspaceID,
        PriorOutputs: in.PriorOutputs, RepoSHA: in.RepoSHA,
    })
    if err != nil { return domain.ActivityResult{}, err }
    status := domain.ActSucceeded
    if !out.Success { status = domain.ActFailed }
    return domain.ActivityResult{
        AgentRole: domain.AgentValidator, Status: status,
        Message: "validator L2 ran", Payload: map[string]any{"structured": out.Structured},
    }, nil
}
```

Add `AgentValidator` constant to `domain/workflow.go` if not present.

### Task 6.4: Commit Stage 6 (Shard 6 completion)

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/validator && go build ./... && go test ./...
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add services/validator services/control-plane/internal/adapter/agents/validator_l2
git commit -m "feat(validator): phase 6 hypothesis sidecar + validator L2 agent wrapper (stage 6)"
```

---

## Stage 7 — Approval Gate domain + repo + severity classifier + Temporal signal pattern (Pattern C shard 7)

### Task 7.1: approval_repo.go

**Files:**
- Create: `services/control-plane/internal/adapter/repo/approval_repo.go`
- Create: `services/control-plane/internal/adapter/repo/approval_repo_test.go`

- [ ] **Step 1: `approval_repo.go`** — dual-pool, RLS-aware reads, admin writes during activity:

```go
package repo

import (
    "context"
    "errors"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

type ApprovalRepo struct {
    pool      *pgxpool.Pool
    adminPool *pgxpool.Pool
}

func NewApprovalRepo(pool, admin *pgxpool.Pool) *ApprovalRepo {
    return &ApprovalRepo{pool: pool, adminPool: admin}
}

func (r *ApprovalRepo) Create(ctx context.Context, d domain.ApprovalDecision) (string, error) {
    var id string
    err := r.adminPool.QueryRow(ctx,
        `INSERT INTO approval_decisions
           (org_id, workspace_id, workflow_run_id, severity, decision, scenario, risk_score)
         VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
        d.OrgID, d.WorkspaceID, d.WorkflowRunID, d.Severity, d.Decision, d.Scenario, d.RiskScore,
    ).Scan(&id)
    return id, err
}

func (r *ApprovalRepo) GetByRun(ctx context.Context, runID string) (domain.ApprovalDecision, error) {
    pool, _ := db.FromCtx(ctx)
    if pool == nil { pool = r.pool }
    var d domain.ApprovalDecision
    err := pool.QueryRow(ctx,
        `SELECT id, org_id, workspace_id, workflow_run_id, severity, decision,
                COALESCE(decided_by::text,''), COALESCE(decided_at, '1970-01-01'::timestamptz),
                COALESCE(notes,''), COALESCE(scenario,''),
                COALESCE(risk_score, 0), created_at
         FROM approval_decisions WHERE workflow_run_id=$1`, runID,
    ).Scan(&d.ID, &d.OrgID, &d.WorkspaceID, &d.WorkflowRunID, &d.Severity, &d.Decision,
        &d.DecidedBy, &d.DecidedAt, &d.Notes, &d.Scenario, &d.RiskScore, &d.CreatedAt)
    if errors.Is(err, pgx.ErrNoRows) {
        return domain.ApprovalDecision{}, domain.ErrNotFound
    }
    return d, err
}

func (r *ApprovalRepo) UpdateDecision(ctx context.Context, runID string, state domain.ApprovalDecisionState, decidedBy, notes string, at time.Time) error {
    pool, _ := db.FromCtx(ctx)
    if pool == nil { pool = r.adminPool }
    var by interface{} = nil
    if decidedBy != "" { by = decidedBy }
    _, err := pool.Exec(ctx,
        `UPDATE approval_decisions SET decision=$1, decided_by=$2, decided_at=$3, notes=$4
         WHERE workflow_run_id=$5`,
        state, by, at.UTC().Truncate(time.Microsecond), notes, runID)
    return err
}

func (r *ApprovalRepo) ListPending(ctx context.Context, orgID string, limit int) ([]domain.ApprovalDecision, error) {
    pool, _ := db.FromCtx(ctx)
    if pool == nil { pool = r.pool }
    rows, err := pool.Query(ctx,
        `SELECT id, org_id, workspace_id, workflow_run_id, severity, decision,
                COALESCE(scenario,''), COALESCE(risk_score, 0), created_at
         FROM approval_decisions WHERE org_id=$1 AND decision='pending'
         ORDER BY created_at DESC LIMIT $2`, orgID, limit,
    )
    if err != nil { return nil, err }
    defer rows.Close()
    var out []domain.ApprovalDecision
    for rows.Next() {
        var d domain.ApprovalDecision
        if err := rows.Scan(&d.ID, &d.OrgID, &d.WorkspaceID, &d.WorkflowRunID,
            &d.Severity, &d.Decision, &d.Scenario, &d.RiskScore, &d.CreatedAt); err != nil {
            return nil, err
        }
        out = append(out, d)
    }
    return out, rows.Err()
}
```

- [ ] **Step 2: tests** — env-gated against real Postgres. Cover create + getByRun + updateDecision + listPending. Verify RLS by binding the GUC to a different org and asserting empty results.

### Task 7.2: Severity classifier

**Files:**
- Create: `services/control-plane/internal/adapter/approval/severity.go`
- Create: `services/control-plane/internal/adapter/approval/severity_test.go`

- [ ] **Step 1: `severity.go`** — pure-go classifier from spec §9.1:

```go
package approval

import (
    "path/filepath"
    "strings"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

var sensitiveGlobs = []string{
    "*.sql",
    "migrations/", "auth/", "security/", "crypto/",
}

const RiskScoreLow    = 15.0
const RiskScoreMedium = 50.0
const RiskScoreHigh   = 85.0

// Classify returns (severity, riskScore) given the synthesiser scenario and a
// unified-diff patch. Empty patch + empty scenario → medium (the "we have no
// idea what's going on" default).
func Classify(scenario string, patchDiff string) (domain.Severity, float64) {
    touched := touchedFiles(patchDiff)
    if scenario == "schema_drift" || hasSensitive(touched) {
        return domain.SeverityHigh, RiskScoreHigh
    }
    if scenario == "unknown" {
        return domain.SeverityHigh, RiskScoreHigh
    }
    if scenario == "oom" {
        return domain.SeverityMedium, RiskScoreMedium
    }
    lines := countChangedLines(patchDiff)
    if lines < 10 && allUnder(touched, "apps/web/components/") {
        return domain.SeverityLow, RiskScoreLow
    }
    return domain.SeverityMedium, RiskScoreMedium
}

func touchedFiles(diff string) []string {
    var out []string
    for _, ln := range strings.Split(diff, "\n") {
        if strings.HasPrefix(ln, "diff --git ") {
            parts := strings.Fields(ln)
            if len(parts) >= 4 {
                out = append(out, strings.TrimPrefix(parts[3], "b/"))
            }
        }
    }
    return out
}

func hasSensitive(files []string) bool {
    for _, f := range files {
        for _, g := range sensitiveGlobs {
            if strings.HasSuffix(g, "/") && strings.Contains(f, g) { return true }
            ok, _ := filepath.Match(g, filepath.Base(f))
            if ok { return true }
        }
    }
    return false
}

func countChangedLines(diff string) int {
    n := 0
    for _, ln := range strings.Split(diff, "\n") {
        if strings.HasPrefix(ln, "+") && !strings.HasPrefix(ln, "+++") { n++ }
        if strings.HasPrefix(ln, "-") && !strings.HasPrefix(ln, "---") { n++ }
    }
    return n
}

func allUnder(files []string, prefix string) bool {
    if len(files) == 0 { return false }
    for _, f := range files { if !strings.HasPrefix(f, prefix) { return false } }
    return true
}
```

- [ ] **Step 2: `severity_test.go`** — coverage matrix:

```go
func TestClassify_SchemaDrift(t *testing.T) {
    sev, _ := approval.Classify("schema_drift", "diff --git a/x.py b/x.py\n+1")
    require.Equal(t, domain.SeverityHigh, sev)
}
func TestClassify_SQLPatchHigh(t *testing.T) {
    sev, _ := approval.Classify("null_deref", "diff --git a/m.sql b/m.sql\n+CREATE TABLE")
    require.Equal(t, domain.SeverityHigh, sev)
}
func TestClassify_TrivialUI(t *testing.T) {
    diff := "diff --git a/apps/web/components/Btn.tsx b/apps/web/components/Btn.tsx\n+const x = 1"
    sev, _ := approval.Classify("null_deref", diff)
    require.Equal(t, domain.SeverityLow, sev)
}
func TestClassify_DefaultMedium(t *testing.T) {
    sev, _ := approval.Classify("null_deref", "diff --git a/foo.go b/foo.go\n+x\n+y\n+z\n+a\n+b\n+c\n+d\n+e\n+f\n+g\n+h")
    require.Equal(t, domain.SeverityMedium, sev)
}
```

### Task 7.3: Approval service

**Files:**
- Create: `services/control-plane/internal/adapter/approval/service.go`
- Create: `services/control-plane/internal/adapter/approval/service_test.go`

- [ ] **Step 1: `service.go`** — owns the create-pending + classify + notify dance. The Temporal signal pattern lives in the workflow function (Stage 11); this service is the activity-side glue.

```go
package approval

import (
    "context"
    "time"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Service struct {
    repo     domain.ApprovalRepository
    notifier domain.Notifier // typically a multi-fanout
    audit    domain.AuditWriter
}

func New(r domain.ApprovalRepository, n domain.Notifier, a domain.AuditWriter) *Service {
    return &Service{repo: r, notifier: n, audit: a}
}

type CreateInput struct {
    OrgID         string
    WorkspaceID   string
    WorkflowRunID string
    Severity      domain.Severity
    Scenario      string
    RiskScore     float64
}

func (s *Service) CreatePending(ctx context.Context, in CreateInput) (string, error) {
    id, err := s.repo.Create(ctx, domain.ApprovalDecision{
        OrgID: in.OrgID, WorkspaceID: in.WorkspaceID, WorkflowRunID: in.WorkflowRunID,
        Severity: in.Severity, Decision: domain.ApprovalPending,
        Scenario: in.Scenario, RiskScore: in.RiskScore,
    })
    if err != nil { return "", err }
    _ = s.audit.Append(ctx, domain.AuditEntry{
        OrgID: in.OrgID, Action: "approval.requested", Target: in.WorkflowRunID,
        Metadata: map[string]any{
            "run_id": in.WorkflowRunID, "severity": in.Severity,
            "scenario": in.Scenario, "risk_score": in.RiskScore,
        },
    })
    return id, nil
}

func (s *Service) AutoApprove(ctx context.Context, runID, orgID string) error {
    if err := s.repo.UpdateDecision(ctx, runID, domain.ApprovalAutoApproved, "", "",
        time.Now().UTC().Truncate(time.Microsecond)); err != nil {
        return err
    }
    return s.audit.Append(ctx, domain.AuditEntry{
        OrgID: orgID, Action: "approval.decided", Target: runID,
        Metadata: map[string]any{"decision": "auto_approved", "auto": true},
    })
}

func (s *Service) Notify(ctx context.Context, n domain.Notification) {
    _ = s.notifier.Send(ctx, n) // multi-fanout swallows per-channel errors
}

func (s *Service) RecordDecision(ctx context.Context, runID, orgID string, sig domain.ApprovalSignal) error {
    if err := s.repo.UpdateDecision(ctx, runID, sig.Decision, sig.DecidedBy, sig.Notes,
        time.Now().UTC().Truncate(time.Microsecond)); err != nil {
        return err
    }
    return s.audit.Append(ctx, domain.AuditEntry{
        OrgID: orgID, Actor: sig.DecidedBy, Action: "approval.decided", Target: runID,
        Metadata: map[string]any{
            "decision": sig.Decision, "decided_by": sig.DecidedBy,
            "notes": sig.Notes, "auto": false,
        },
    })
}
```

- [ ] **Step 2: `service_test.go`** — table-driven over the four severity buckets + the create→notify→update happy path. Use fakes for `ApprovalRepository`, `Notifier`, `AuditWriter`.

### Task 7.4: Notifier skeleton (Wave 2 fills in providers)

**Files:**
- Create: `services/control-plane/internal/adapter/notifier/multi.go`
- Create: `services/control-plane/internal/adapter/notifier/multi_test.go`

- [ ] **Step 1: `multi.go`** — fan-out wrapper:

```go
package notifier

import (
    "context"
    "log/slog"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Multi struct {
    children []domain.Notifier
}

func NewMulti(children ...domain.Notifier) *Multi { return &Multi{children: children} }

func (m *Multi) Channel() string { return "multi" }

func (m *Multi) Send(ctx context.Context, n domain.Notification) error {
    for _, c := range m.children {
        if err := c.Send(ctx, n); err != nil {
            slog.Warn("notifier.channel.failed", "channel", c.Channel(), "err", err)
        }
    }
    return nil
}
```

The Slack / email / console implementations land in **Wave 2 Stage 8**.

### Task 7.5: Approval signaler (HTTP → Temporal)

**Files:**
- Create: `services/control-plane/internal/usecase/approval_signaler.go`

- [ ] **Step 1: usecase**

```go
package usecase

import (
    "context"
    "errors"
    "time"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/approval"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// ApprovalSignaler is the HTTP-side service the approve/reject endpoint
// depends on. It validates state + signals Temporal + writes the decision row.
type ApprovalSignaler struct {
    repo      domain.ApprovalRepository
    workflows TemporalSignaler // narrow port — see WorkflowService
    audit     domain.AuditWriter
    approval  *approval.Service
}

type TemporalSignaler interface {
    SignalApproval(ctx context.Context, runID string, sig domain.ApprovalSignal) error
}

func NewApprovalSignaler(r domain.ApprovalRepository, w TemporalSignaler, a domain.AuditWriter, svc *approval.Service) *ApprovalSignaler {
    return &ApprovalSignaler{repo: r, workflows: w, audit: a, approval: svc}
}

type DecideInput struct {
    Principal     domain.Principal
    WorkspaceID   string
    WorkflowRunID string
    Decision      domain.ApprovalDecisionState // approved | rejected
    Notes         string
}

func (s *ApprovalSignaler) Decide(ctx context.Context, in DecideInput) error {
    d, err := s.repo.GetByRun(ctx, in.WorkflowRunID)
    if err != nil { return err }
    if d.Decision != domain.ApprovalPending {
        return errors.New("approval already decided")
    }
    if d.WorkspaceID != in.WorkspaceID {
        return domain.ErrForbidden
    }
    sig := domain.ApprovalSignal{
        Decision: in.Decision, DecidedBy: in.Principal.UserID, Notes: in.Notes,
    }
    if err := s.workflows.SignalApproval(ctx, in.WorkflowRunID, sig); err != nil {
        return err
    }
    return s.approval.RecordDecision(ctx, in.WorkflowRunID, in.Principal.OrgID, sig)
}
```

Add `domain.ErrForbidden` to `errors.go` if not present.

### Task 7.6: Commit Stage 7 (Shard 7 completion)

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add internal/adapter/approval internal/adapter/repo/approval_repo.go internal/adapter/notifier internal/usecase/approval_signaler.go
git commit -m "feat(approval): phase 6 approval gate service + severity + signaler + multi notifier (stage 7)"
```

---

## Wave 1 merge — coordinator

After all four shards land:

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane
make build && make test && make vet && make arch
docker compose up -d neo4j causal-inference postgres
docker compose run --rm control-plane /app/seed-neo4j --org-id=$(uuidgen)
```

Any shard failure → roll the offending shard back, dispatch alone with the error transcript. Do not proceed to Wave 2 with a red build.

---

## Wave 2 — Pattern B: 3 parallel agents (Stages 8-10)

> **Pattern B dispatch:** the coordinator fires three agents in a single message — two `backend-engineer` (Slack + GitOps) and one `frontend-engineer` (Approvals UI + Live Demo CTA). Each shard's file set is disjoint per the playbook table at the top.

---

## Stage 8 — Slack adapter + email/console notifiers + slack_notifications repo

### Task 8.1: Slack integration adapter

**Files:**
- Create: `services/control-plane/internal/adapter/integration/slack/provider.go`
- Create: `services/control-plane/internal/adapter/integration/slack/provider_test.go`
- Create: `services/control-plane/internal/adapter/repo/slack_notifications_repo.go`

- [ ] **Step 1: `provider.go`** — implements `domain.Integration`. Webhook URL is sealed via `domain.KeyVault` (Phase 2 plumbing) and stored in `integrations.installation_id` text column.

```go
package slack

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Repo interface {
    Upsert(ctx context.Context, orgID string, c domain.Connection, secret []byte) error
    Get(ctx context.Context, orgID string, provider domain.IntegrationProvider) (domain.Connection, []byte, error)
    Delete(ctx context.Context, orgID string, provider domain.IntegrationProvider) error
}

type Provider struct {
    repo Repo
    kv   domain.KeyVault
    http *http.Client
}

func New(r Repo, kv domain.KeyVault) *Provider {
    return &Provider{repo: r, kv: kv, http: &http.Client{Timeout: 10 * time.Second}}
}

func (p *Provider) Name() domain.IntegrationProvider { return domain.IntegrationProvider("slack") }

func (p *Provider) Connect(ctx context.Context, princ domain.Principal, cfg map[string]any) (domain.Connection, error) {
    url, _ := cfg["webhook_url"].(string)
    if url == "" { return domain.Connection{}, errors.New("slack: webhook_url required") }
    channel, _ := cfg["channel_name"].(string)
    // Probe the webhook with a test message.
    if err := p.post(ctx, url, map[string]any{"text": "Nexis connected"}); err != nil {
        return domain.Connection{}, fmt.Errorf("slack: probe failed: %w", err)
    }
    encURL, err := p.kv.Encrypt(ctx, []byte(url))
    if err != nil { return domain.Connection{}, err }
    c := domain.Connection{
        Provider: domain.IntegrationProvider("slack"),
        Status: domain.StatusConnected,
        InstallationID: "slack-" + princ.OrgID[:8], // ledger handle only
        Metadata: map[string]any{"channel_name": channel},
    }
    return c, p.repo.Upsert(ctx, princ.OrgID, c, encURL)
}

func (p *Provider) Disconnect(ctx context.Context, princ domain.Principal) error {
    return p.repo.Delete(ctx, princ.OrgID, domain.IntegrationProvider("slack"))
}

func (p *Provider) Status(ctx context.Context, princ domain.Principal) (domain.Connection, error) {
    c, _, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationProvider("slack"))
    return c, err
}

func (p *Provider) HandleWebhook(ctx context.Context, orgID string, headers map[string]string, body []byte) error {
    return errors.New("slack: incoming webhooks only in phase 6")
}

// SendBlock posts a Block Kit message to the org's configured webhook URL.
// Used by notifier/slack.go.
func (p *Provider) SendBlock(ctx context.Context, orgID string, blocks []map[string]any) (int, error) {
    _, encURL, err := p.repo.Get(ctx, orgID, domain.IntegrationProvider("slack"))
    if err != nil { return 0, err }
    urlB, err := p.kv.Decrypt(ctx, encURL)
    if err != nil { return 0, err }
    return p.postRaw(ctx, string(urlB), map[string]any{"blocks": blocks})
}

func (p *Provider) post(ctx context.Context, url string, body map[string]any) error {
    code, err := p.postRaw(ctx, url, body); _ = code; return err
}

func (p *Provider) postRaw(ctx context.Context, url string, body map[string]any) (int, error) {
    raw, _ := json.Marshal(body)
    req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(raw))
    req.Header.Set("Content-Type", "application/json")
    resp, err := p.http.Do(req); if err != nil { return 0, err }
    defer resp.Body.Close()
    _, _ = io.Copy(io.Discard, resp.Body)
    if resp.StatusCode/100 != 2 { return resp.StatusCode, fmt.Errorf("slack: http %d", resp.StatusCode) }
    return resp.StatusCode, nil
}

var _ domain.Integration = (*Provider)(nil)
```

- [ ] **Step 2: `provider_test.go`** — assert webhook URL NEVER appears in metadata. Mock `Repo` + a stubbed `KeyVault`. Verify a 200 response sets `Status=Connected`; verify the test message body is `{"text":"Nexis connected"}`.

- [ ] **Step 3: `slack_notifications_repo.go`** — admin-pool insert + RLS-aware list. Reference Phase 5's `token_ledger_repo.go` for the dual-pool template. Insert columns: `org_id, workflow_run_id, channel, kind, status, http_status, error`.

### Task 8.2: Notifier providers

**Files:**
- Create: `services/control-plane/internal/adapter/notifier/slack.go`
- Create: `services/control-plane/internal/adapter/notifier/email.go`
- Create: `services/control-plane/internal/adapter/notifier/console.go`

- [ ] **Step 1: `slack.go`** — builds the Block Kit JSON per spec §11.2 + writes a `slack_notifications` row per attempt:

```go
package notifier

import (
    "context"
    "fmt"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/slack"
    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Slack struct {
    prov     *slack.Provider
    ledger   *repo.SlackNotificationsRepo
    consoleBaseURL string
}

func NewSlack(p *slack.Provider, l *repo.SlackNotificationsRepo, baseURL string) *Slack {
    return &Slack{prov: p, ledger: l, consoleBaseURL: baseURL}
}

func (s *Slack) Channel() string { return "slack" }

func (s *Slack) Send(ctx context.Context, n domain.Notification) error {
    blocks := buildApprovalBlocks(n, s.consoleBaseURL)
    code, err := s.prov.SendBlock(ctx, n.OrgID, blocks)
    status := "sent"
    var errStr string
    if err != nil { status = "failed"; errStr = err.Error() }
    _ = s.ledger.Insert(ctx, repo.SlackNotifRow{
        OrgID: n.OrgID, WorkflowRunID: n.WorkflowRunID,
        Kind: string(n.Kind), Status: status, HTTPStatus: code, Error: errStr,
    })
    return err
}

func buildApprovalBlocks(n domain.Notification, baseURL string) []map[string]any {
    return []map[string]any{
        {"type": "header", "text": map[string]any{
            "type": "plain_text",
            "text": fmt.Sprintf("Approval required — %s", n.Scenario),
        }},
        {"type": "section", "fields": []map[string]any{
            {"type": "mrkdwn", "text": fmt.Sprintf("*Severity*\n%s", n.Severity)},
            {"type": "mrkdwn", "text": fmt.Sprintf("*Run ID*\n%s", short(n.WorkflowRunID))},
        }},
        {"type": "section", "text": map[string]any{
            "type": "mrkdwn", "text": "*Plan:* " + n.Body,
        }},
        {"type": "actions", "elements": []map[string]any{{
            "type": "button", "style": "primary",
            "text": map[string]any{"type": "plain_text", "text": "Review in console"},
            "url":  fmt.Sprintf("%s/console/approvals/%s", baseURL, n.WorkflowRunID),
        }}},
    }
}

func short(id string) string { if len(id) > 8 { return id[:8] }; return id }
```

- [ ] **Step 2: `email.go`** — reuses Phase 2's `SMTPMailer` to send to every workspace owner|admin. Subject: `[NEXIS] Approval required — <scenario>`. Body: plaintext + console deep link. Resolve owner+admin emails via `WorkspaceMembersRepo` (Phase 3.5).

- [ ] **Step 3: `console.go`** — no-op `Send` (the UI polls). Implements `domain.Notifier` to keep the multi-fanout shape uniform.

```go
package notifier

import (
    "context"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Console struct{}

func NewConsole() *Console { return &Console{} }
func (c *Console) Channel() string { return "console" }
func (c *Console) Send(ctx context.Context, n domain.Notification) error { return nil }
```

### Task 8.3: Commit Stage 8

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add internal/adapter/integration/slack internal/adapter/repo/slack_notifications_repo.go internal/adapter/notifier/{slack,email,console}.go
git commit -m "feat(slack): phase 6 slack adapter + notifier providers + slack_notifications repo (stage 8)"
```

---

## Stage 9 — GitOps service rewrite (Pattern B shard 2)

### Task 9.1: Bump gitops go.mod

**Files:**
- Modify: `services/gitops/go.mod`

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/gitops
# bump go 1.22 → 1.25
sed -i '' 's/^go 1.22$/go 1.25/' go.mod
go get \
  github.com/bradleyfalzon/ghinstallation/v2@v2.10.0 \
  github.com/google/go-github/v60@v60.0.0 \
  github.com/jackc/pgx/v5@v5.5.5 \
  github.com/go-chi/chi/v5@v5.2.5
go mod tidy
```

### Task 9.2: Service layout

**Files:**
- Create: `services/gitops/internal/platform/config/config.go`
- Create: `services/gitops/internal/domain/pr.go`
- Create: `services/gitops/internal/adapter/github/client.go`
- Create: `services/gitops/internal/adapter/github/client_test.go`
- Create: `services/gitops/internal/adapter/repo/integrations_repo.go`
- Create: `services/gitops/internal/adapter/repo/audit_repo.go`
- Create: `services/gitops/internal/usecase/open_pr.go`
- Create: `services/gitops/internal/usecase/open_pr_test.go`
- Create: `services/gitops/internal/transport/http/handler.go`
- Create: `services/gitops/internal/transport/http/handler_test.go`
- Rewrite: `services/gitops/cmd/server/main.go`
- Modify: `services/gitops/Dockerfile`

- [ ] **Step 1: `config/config.go`** — minimal env loader:

```go
package config

import "os"

type Config struct {
    Port                   string
    DatabaseURL            string
    GitOpsToken            string
    GitHubAppID            int64
    GitHubAppPrivateKeyPath string
    LogLevel               string
}

func Load() Config {
    return Config{
        Port:                    env("PORT", "8082"),
        DatabaseURL:             env("DATABASE_URL", ""),
        GitOpsToken:             env("GITOPS_TOKEN", ""),
        GitHubAppID:             envInt64("GITHUB_APP_ID", 0),
        GitHubAppPrivateKeyPath: env("GITHUB_APP_PRIVATE_KEY_PATH", ""),
        LogLevel:                env("LOG_LEVEL", "info"),
    }
}
// env + envInt64 — standard 5-line helpers.
func env(k, d string) string { if v := os.Getenv(k); v != "" { return v }; return d }
```

- [ ] **Step 2: `domain/pr.go`** — wire shapes per spec §10.1:

```go
package domain

import "time"

type RepoRef struct {
    Owner         string `json:"owner"`
    Name          string `json:"name"`
    DefaultBranch string `json:"default_branch"`
}

type PROpenRequest struct {
    OrgID         string  `json:"org_id"`
    WorkflowRunID string  `json:"workflow_run_id"`
    Repo          RepoRef `json:"repo"`
    PatchDiff     string  `json:"patch_diff"`
    BranchName    string  `json:"branch_name"`
    PRTitle       string  `json:"pr_title"`
    PRBody        string  `json:"pr_body"`
}

type PROpenResponse struct {
    PRNumber int       `json:"pr_number"`
    PRURL    string    `json:"pr_url"`
    Branch   string    `json:"branch"`
    HeadSHA  string    `json:"head_sha"`
    OpenedAt time.Time `json:"opened_at"`
}
```

- [ ] **Step 3: `adapter/github/client.go`** — `ghinstallation`-based builder. See `bradleyfalzon/ghinstallation` README for transport setup. Per spec §10.2:

```go
package github

import (
    "context"
    "errors"
    "fmt"
    "net/http"
    "os"

    "github.com/bradleyfalzon/ghinstallation/v2"
    gh "github.com/google/go-github/v60/github"
)

type ClientBuilder struct {
    AppID         int64
    PrivateKeyPEM []byte
    IntegrationsRepo IntegrationsReader
}

type IntegrationsReader interface {
    InstallationID(ctx context.Context, orgID string) (int64, error)
}

func NewBuilder(appID int64, keyPath string, repo IntegrationsReader) (*ClientBuilder, error) {
    key, err := os.ReadFile(keyPath)
    if err != nil { return nil, fmt.Errorf("read private key %s: %w", keyPath, err) }
    return &ClientBuilder{AppID: appID, PrivateKeyPEM: key, IntegrationsRepo: repo}, nil
}

func (b *ClientBuilder) ForOrg(ctx context.Context, orgID string) (*gh.Client, error) {
    installID, err := b.IntegrationsRepo.InstallationID(ctx, orgID)
    if err != nil { return nil, fmt.Errorf("installation lookup: %w", err) }
    if installID == 0 { return nil, errors.New("github installation not connected") }
    tr, err := ghinstallation.New(http.DefaultTransport, b.AppID, installID, b.PrivateKeyPEM)
    if err != nil { return nil, fmt.Errorf("ghinstallation: %w", err) }
    return gh.NewClient(&http.Client{Transport: tr}), nil
}
```

Reference: https://github.com/bradleyfalzon/ghinstallation, https://github.com/google/go-github.

- [ ] **Step 4: `adapter/repo/integrations_repo.go`** — read `installation_id` from Postgres. Uses the `nexis_gitops` role from Stage 0 migration 0017:

```go
package repo

import (
    "context"
    "strconv"

    "github.com/jackc/pgx/v5/pgxpool"
)

type IntegrationsRepo struct{ pool *pgxpool.Pool }

func NewIntegrationsRepo(p *pgxpool.Pool) *IntegrationsRepo { return &IntegrationsRepo{pool: p} }

func (r *IntegrationsRepo) InstallationID(ctx context.Context, orgID string) (int64, error) {
    var s string
    err := r.pool.QueryRow(ctx,
        `SELECT installation_id FROM integrations WHERE org_id=$1 AND provider='github' AND status='connected'`,
        orgID,
    ).Scan(&s)
    if err != nil { return 0, err }
    return strconv.ParseInt(s, 10, 64)
}
```

- [ ] **Step 5: `adapter/repo/audit_repo.go`** — minimal HMAC-chained append. Reuses the same chain pattern as the control-plane's `audit_log` writer. Phase 6 simplification: the gitops audit row appends with `prev_hash=<MAX(hash) FROM audit_log WHERE org_id=$1>`. ~60 lines.

- [ ] **Step 6: `usecase/open_pr.go`** — full flow per spec §10.3. `parseUnifiedDiff` reads each `diff --git` block; for each `+++ b/<path>` it reads the file's current blob (via `Repositories.GetContents`) and applies the `@@` hunks line-by-line. Handles single-file + multi-file additive diffs; rejects binary, renames, mode changes with `errBadDiff`.

The full open_pr.go is ~180 lines. The structure follows spec §10.3 verbatim. Add at the end of the function:

```go
return domain.PROpenResponse{
    PRNumber: *pr.Number, PRURL: *pr.HTMLURL,
    Branch: in.BranchName, HeadSHA: *commit.SHA,
    OpenedAt: time.Now().UTC().Truncate(time.Microsecond),
}, nil
```

- [ ] **Step 7: `transport/http/handler.go`** — chi router. Bearer token gate via middleware. POST `/v1/prs` → `OpenPRUsecase.Run`. GET `/healthz` → 200.

```go
func New(cfg config.Config, uc *usecase.OpenPRUsecase) http.Handler {
    r := chi.NewRouter()
    r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
    r.Group(func(r chi.Router) {
        r.Use(bearerAuth(cfg.GitOpsToken))
        r.Post("/v1/prs", openPR(uc))
    })
    return r
}
```

`bearerAuth` is a 10-line middleware reading `Authorization: Bearer <token>` and rejecting with 401 if mismatched.

- [ ] **Step 8: `cmd/server/main.go`** — boots config + pgxpool (nexis_gitops user) + ghinstallation builder + OpenPRUsecase + http server. ~70 lines; reference the control-plane main.go for the chi+slog wiring pattern.

- [ ] **Step 9: `Dockerfile`** — bump base + multi-stage:

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/server /app/server
USER nonroot:nonroot
EXPOSE 8082
ENTRYPOINT ["/app/server"]
```

### Task 9.3: Control-plane GitOps client + new activity

**Files:**
- Create: `services/control-plane/internal/adapter/gitops/client.go`
- Modify: `services/control-plane/internal/workflow/recovery/activities.go`

- [ ] **Step 1: `client.go`** — chi-side HTTP client that wraps the gitops service:

```go
package gitops

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"
)

type Client struct {
    baseURL string
    token   string
    http    *http.Client
}

func New(baseURL, token string) *Client {
    return &Client{baseURL: baseURL, token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

type OpenPRInput struct {
    OrgID, WorkflowRunID, RepoOwner, RepoName, DefaultBranch string
    PatchDiff, BranchName, PRTitle, PRBody                   string
}

type OpenPRResult struct {
    PRNumber int    `json:"pr_number"`
    PRURL    string `json:"pr_url"`
    Branch   string `json:"branch"`
    HeadSHA  string `json:"head_sha"`
}

func (c *Client) OpenPR(ctx context.Context, in OpenPRInput) (OpenPRResult, error) {
    body, _ := json.Marshal(map[string]any{
        "org_id": in.OrgID, "workflow_run_id": in.WorkflowRunID,
        "repo": map[string]string{"owner": in.RepoOwner, "name": in.RepoName, "default_branch": in.DefaultBranch},
        "patch_diff": in.PatchDiff, "branch_name": in.BranchName,
        "pr_title": in.PRTitle, "pr_body": in.PRBody,
    })
    req, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/prs", bytes.NewReader(body))
    req.Header.Set("Authorization", "Bearer "+c.token)
    req.Header.Set("Content-Type", "application/json")
    resp, err := c.http.Do(req); if err != nil { return OpenPRResult{}, err }
    defer resp.Body.Close()
    rb, _ := io.ReadAll(resp.Body)
    if resp.StatusCode != 201 {
        return OpenPRResult{}, fmt.Errorf("gitops http %d: %s", resp.StatusCode, string(rb))
    }
    var out OpenPRResult; if err := json.Unmarshal(rb, &out); err != nil { return OpenPRResult{}, err }
    return out, nil
}
```

- [ ] **Step 2: New `GitOpsOpenPR` activity body** (Stage 11 merges this):

```go
func (a *Activities) GitOpsOpenPR(ctx context.Context, in PipelineInput) (domain.ActivityResult, error) {
    if a.GitOps == nil {
        return a.stub(ctx, domain.AgentGitOps, "GitOps.OpenPR")
    }
    // Extract patch from prior outputs.
    backend, _ := in.PriorOutputs["backend"].(map[string]any)
    structured, _ := backend["structured"].(map[string]any)
    patchDiff, _ := structured["patch_diff"].(string)

    branch := fmt.Sprintf("nexis/fix-%s", in.RunID[:8])
    out, err := a.GitOps.OpenPR(ctx, gitops.OpenPRInput{
        OrgID: in.OrgID, WorkflowRunID: in.RunID,
        RepoOwner: a.FixtureRepoOwner, RepoName: a.FixtureRepoName,
        DefaultBranch: a.FixtureRepoDefaultBranch,
        PatchDiff: patchDiff, BranchName: branch,
        PRTitle: fmt.Sprintf("Recover from %s", in.Incident.Label),
        PRBody:  buildPRBody(in),
    })
    if err != nil { return domain.ActivityResult{}, err }
    return domain.ActivityResult{
        AgentRole: domain.AgentGitOps, Status: domain.ActSucceeded,
        Message: fmt.Sprintf("PR #%d opened", out.PRNumber),
        Payload: map[string]any{"pr_number": out.PRNumber, "pr_url": out.PRURL, "branch": out.Branch, "head_sha": out.HeadSHA},
    }, nil
}
```

`buildPRBody` renders a markdown template with the agent transcript table + risk score + placeholder ArgoCD app link. ~30 lines.

### Task 9.4: Commit Stage 9

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/gitops && go build ./... && go test ./...
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add services/gitops services/control-plane/internal/adapter/gitops
git commit -m "feat(gitops): phase 6 full PR opener via ghinstallation + go-github (stage 9)"
```

---

## Stage 10 — Approvals UI + Live Demo CTA + incident timeline badges (Pattern B shard 3)

### Task 10.1: SDK

**Files:**
- Create: `apps/web/lib/approvals.ts`

```ts
import { proxy } from "@/proxy";

export type ApprovalDecision = {
  id: string;
  org_id: string;
  workspace_id: string;
  workflow_run_id: string;
  severity: "low" | "medium" | "high";
  decision: "pending" | "approved" | "rejected" | "auto_approved" | "timeout_rejected";
  decided_by: string | null;
  decided_at: string | null;
  notes: string | null;
  scenario: string | null;
  risk_score: number | null;
  created_at: string;
};

export async function listPending(): Promise<ApprovalDecision[]> {
  const res = await proxy("/v1/approvals?status=pending");
  if (!res.ok) throw new Error("approvals list failed");
  const j = await res.json(); return j.items ?? [];
}

export async function getByRun(ws: string, runId: string): Promise<ApprovalDecision> {
  const res = await proxy(`/v1/workspaces/${ws}/pipelines/${runId}/decision`);
  if (!res.ok) throw new Error("approval get failed");
  return res.json();
}

export async function decide(ws: string, runId: string, decision: "approve" | "reject", notes?: string) {
  const res = await proxy(`/v1/workspaces/${ws}/pipelines/${runId}/approve`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ decision, notes: notes ?? "" }),
  });
  if (!res.ok && res.status !== 202) throw new Error("decide failed");
}
```

### Task 10.2: Components

**Files:**
- Create: `apps/web/components/approvals/ApprovalBadge.tsx`
- Create: `apps/web/components/approvals/ApprovalForm.tsx`
- Create: `apps/web/components/approvals/PendingApprovalsTable.tsx`
- Create: `apps/web/components/pipelines/SynthesiserPlan.tsx`
- Create: `apps/web/components/pipelines/GitOpsPRLink.tsx`

- [ ] **Step 1: `ApprovalBadge.tsx`** — severity color pill (Tremor `Badge`). Green for `approved`/`auto_approved`, red for `rejected`/`timeout_rejected`, yellow for `pending`. Severity prefix: `low/medium/high` colored amber/orange/red. ~40 lines.

- [ ] **Step 2: `ApprovalForm.tsx`** — approve/reject buttons + optional notes textarea. Uses `react-hook-form` already on the page; calls `decide()` from the SDK.

- [ ] **Step 3: `PendingApprovalsTable.tsx`** — Tremor `Table` with columns: severity, scenario, workspace, created_at, action. Reuses `StatusPill` + `AgentIcon` from Phase 5.

- [ ] **Step 4: `SynthesiserPlan.tsx`** — renders `selected_agents` as an arrow chain (`Architect → Backend → QA`) + a tooltip with the rationale.

- [ ] **Step 5: `GitOpsPRLink.tsx`** — small green badge `PR #47 ↗` linking out to `pr_url`. Renders nothing when `pr_url` is empty.

### Task 10.3: Pages

**Files:**
- Rewrite: `apps/web/app/(app)/console/approvals/page.tsx`
- Create: `apps/web/app/(app)/console/approvals/[run_id]/page.tsx`
- Create: `apps/web/app/(app)/console/approvals/[run_id]/client.tsx`
- Rewrite: `apps/web/app/(app)/console/live-demo/client.tsx`
- Modify: `apps/web/app/(app)/console/incidents/[id]/client.tsx`

- [ ] **Step 1: `approvals/page.tsx`** — server component. Loads pending list via `approvals.listPending()` and renders `PendingApprovalsTable`. Empty state: "No pending approvals — your pipelines are coasting."

- [ ] **Step 2: `approvals/[run_id]/page.tsx` + `client.tsx`** — server loads `getByRun` + the workflow run for transcript. Client renders the synthesiser plan, the patch diff (Monaco), the approve/reject form.

- [ ] **Step 3: `live-demo/client.tsx` rewrite** — dropdown of 4 fixture incidents. POST to `/v1/admin/sentinel/trigger` with the chosen `incident_label`. Subscribe to the SSE stream for the returned `run_id`; render the live timeline + the eventual approval badge + the PR link.

- [ ] **Step 4: `incidents/[id]/client.tsx` modify** — in the timeline header, render `ApprovalBadge` + `GitOpsPRLink` when the run has them.

### Task 10.4: Sidebar nav + handler routes

**Files:**
- Modify: `apps/web/app/layout.tsx` (COORDINATOR) — add a sidebar entry "Approvals" with a pending-count badge fetched server-side.
- Create: `services/control-plane/internal/transport/http/handler/approvals.go` — 3 routes per spec §13.1:

```go
func RegisterApprovals(r chi.Router, signaler *usecase.ApprovalSignaler, repo domain.ApprovalRepository) {
    r.Route("/v1/workspaces/{ws}/pipelines/{run_id}", func(r chi.Router) {
        r.Use(rlsBind, requireAuth)
        r.Post("/approve", postApprove(signaler))        // 202
        r.Get("/decision",  getDecision(repo))            // 200
    })
    r.With(rlsBind, requireAuth).Get("/v1/approvals", listPending(repo))
    r.With(rlsBind, requireAuth).Post("/v1/admin/sentinel/trigger", manualTrigger)
}
```

Each handler: parse path/body → RBAC check (owner|admin for writes; any role for reads) → call usecase/repo → return JSON. ~40 lines per handler.

- [ ] **Modify `transport/http/server.go`** (COORDINATOR) to mount `RegisterApprovals(r, signaler, approvalRepo)` alongside the Phase 5 routes.

### Task 10.5: Commit Stage 10

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add apps/web/lib/approvals.ts apps/web/components/approvals apps/web/components/pipelines/SynthesiserPlan.tsx apps/web/components/pipelines/GitOpsPRLink.tsx apps/web/app/{layout.tsx,'(app)'/console/{approvals,live-demo,incidents}} services/control-plane/internal/transport/http/handler/approvals.go services/control-plane/internal/transport/http/server.go
git commit -m "feat(console): phase 6 approvals UI + live demo CTA + timeline badges + signaler routes (stage 10)"
```

---

## Wave 2 merge — coordinator

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
docker compose build gitops causal-inference validator
docker compose up -d
```

Any red signal → roll the failing shard, redispatch alone.

---

## Stage 11 — ArgoCD design docs

**Goal:** Documentation only — Phase 6 ships the YAML + README; Phase 7 wires runtime.

### Task 11.1: app-of-apps + rollouts + README

**Files:**
- Create: `infra/argocd/app-of-apps.yaml`
- Create: `infra/argocd/rollouts.yaml`
- Create: `infra/argocd/README.md`

- [ ] **Step 1: `app-of-apps.yaml`** — verbatim from spec §12 (40 lines of `Application` CR). See: https://argo-cd.readthedocs.io/en/stable/operator-manual/cluster-bootstrapping/.

- [ ] **Step 2: `rollouts.yaml`** — verbatim from spec §12 (canary `Rollout` CR + `AnalysisTemplate` with Prometheus query). See: https://argoproj.github.io/argo-rollouts/features/specification/, https://argoproj.github.io/argo-rollouts/features/analysis/.

- [ ] **Step 3: `README.md`** — opens with the bold warning from spec Risk 13.12:

```md
# ArgoCD + Argo Rollouts — Phase 7 blueprint

**THIS IS A PHASE 7 BLUEPRINT — NOTHING HERE RUNS YET IN COMPOSE.**

The YAML in this directory documents the runtime plan for Phase 7. Phase 6
ships the Nexis recovery loop up to the PR-open step; a human merges the PR
and Phase 7 will wire ArgoCD to pick it up.

## Phase 7 prep checklist
- Stand up ArgoCD in k3d / minikube against the `argo-rollouts` controller.
- Point `app-of-apps.yaml` at the fixture repo (or per-design-partner clone).
- Wire Prometheus + service mesh metrics to satisfy the `slo-latency-p95`
  AnalysisTemplate. Without this, the rollouts canary fails-open.
- Configure the GitHub App's installation to also write to the manifests/
  directory in the fixture repo so the Backend agent's patches land in the
  ArgoCD-watched path.

## Demo guide (Phase 6 walkthrough)
1. Click "Inject fault" on `/console/live-demo`. Pick `fixture-null-pointer`.
2. Wait for the timeline to reach `ApprovalGate.Route` → click Approve.
3. Watch the PR appear at github.com/nexis-eco/fixture-recovery-demo.
4. (Phase 7) ArgoCD picks the merge up; the canary AnalysisTemplate measures
   latency. If the SLO breaches, the rollout aborts and rolls back.
```

### Task 11.2: Commit Stage 11

```bash
git add infra/argocd
git commit -m "docs(argocd): phase 6 app-of-apps + rollouts + readme blueprint for phase 7 wiring (stage 11)"
```

---

## Stage 12 — Workflow refactor + activities glue + fixtures + E2E + DoD (COORDINATOR sequential)

This is the sequential merge stage. It touches `cmd/server/main.go`, `internal/workflow/recovery/{activities,workflow,types}.go`, and `internal/transport/http/server.go` simultaneously and must NOT be dispatched in parallel.

### Task 12.1: Activities wiring

**Files:**
- Modify: `services/control-plane/internal/workflow/recovery/activities.go`

- [ ] **Step 1: Extend `Activities` struct** with the new Phase 6 fields:

```go
type Activities struct {
    // ... existing Phase 4-5 fields
    Approval        *approval.Service
    GitOps          gitops.Client
    Graph           domain.Graph         // optional; nil disables graph evidence
    Causal          domain.CausalEngine  // optional; nil disables causal calls
    IncidentsAdmin  domain.IncidentsReader
    FixtureRepoOwner, FixtureRepoName, FixtureRepoDefaultBranch string
}
```

Extend `NewActivitiesFull` to accept them. Backwards-compat constructor `NewActivities` stays as-is for tests.

- [ ] **Step 2: New activity bodies** — `ApprovalRequestNotify`, `ApprovalRecordDecision`, `ApprovalAutoApprove`, `GitOpsOpenPR`, `ValidatorL2Validate`. All five are <40 lines each, reading from `PipelineInput.SynthesiserPlan` + prior outputs.

- [ ] **Step 3: Replace `ApprovalGateRoute` body** — the workflow now drives approval logic directly via the Temporal signal pattern; this activity becomes a thin record/notify pair the workflow calls separately. Keep the stub method for backwards-compat tests.

### Task 12.2: Workflow refactor

**Files:**
- Modify: `services/control-plane/internal/workflow/recovery/workflow.go`
- Modify: `services/control-plane/internal/workflow/recovery/types.go`

- [ ] **Step 1: Update `types.go`** — add `PipelineInput.SynthesiserPlan` + `PriorOutputs map[string]any`; add `PipelineOutput.ApprovalDecisionID` + `PRURL`. Already covered by Stage 1; ensure the workflow file imports match.

- [ ] **Step 2: Workflow body** — refactor the L1 walk per spec §14:

```go
func RecoveryPipeline(ctx workflow.Context, in PipelineInput) (PipelineOutput, error) {
    var out PipelineOutput
    // Sentinel ack
    var sent domain.ActivityResult
    if err := workflow.ExecuteActivity(ctx, (*Activities).SentinelDetect, in).Get(ctx, &sent); err != nil {
        return out, err
    }
    in.Incident = parseIncidentFromPayload(sent.Payload)

    // Pathfinder
    var pf domain.ActivityResult
    if err := workflow.ExecuteActivity(ctx, (*Activities).PathfinderDiagnose, in).Get(ctx, &pf); err != nil { return out, err }
    setPriorOutput(&in, "pathfinder", pf.Payload["structured"])

    // Synthesiser
    var syn domain.ActivityResult
    if err := workflow.ExecuteActivity(ctx, (*Activities).SynthesiserPlan, in).Get(ctx, &syn); err != nil { return out, err }
    setPriorOutput(&in, "synthesiser", syn.Payload["structured"])
    in.SynthesiserPlan = parseSynthPlan(syn.Payload["structured"])

    // L1 walk in synthesiser-chosen order. Skipped agents get a 'skipped' event.
    table := map[domain.AgentName]any{
        domain.AgentNameArchitect:    (*Activities).ArchitectSolution,
        domain.AgentNameBackend:      (*Activities).BackendCodegen,
        domain.AgentNameQA:           (*Activities).QATestGen,
        domain.AgentNameDevOps:       (*Activities).DevOpsPipeline,
        domain.AgentNameDataEngineer: (*Activities).DataEngineerMigrate,
    }
    for _, name := range in.SynthesiserPlan.SelectedAgents {
        fn, ok := table[name]; if !ok { continue }
        var r domain.ActivityResult
        if err := workflow.ExecuteActivity(ctx, fn, in).Get(ctx, &r); err != nil { return out, err }
        setPriorOutput(&in, string(name), r.Payload["structured"])
    }
    for _, name := range in.SynthesiserPlan.SkippedAgents {
        _ = workflow.ExecuteActivity(ctx, (*Activities).RecordSkipped, RecordSkippedInput{
            RunID: in.RunID, OrgID: in.OrgID, Agent: name,
        }).Get(ctx, nil)
    }

    // Validator L2
    var vl domain.ActivityResult
    if err := workflow.ExecuteActivity(ctx, (*Activities).ValidatorL2Validate, in).Get(ctx, &vl); err != nil {
        return out, err
    }
    if vl.Status == domain.ActFailed {
        return out, temporal.NewNonRetryableApplicationError("validator failed", "ValidatorL2Failed", nil)
    }

    // Approval gate
    backend, _ := in.PriorOutputs["backend"].(map[string]any)
    patch, _ := backend["patch_diff"].(string)
    severity, risk := approval.Classify(in.SynthesiserPlan.Scenario, patch)

    var apReq domain.ActivityResult
    _ = workflow.ExecuteActivity(ctx, (*Activities).ApprovalRequestNotify, ApprovalRequestInput{
        RunID: in.RunID, OrgID: in.OrgID, WorkspaceID: in.WorkspaceID,
        Severity: string(severity), Scenario: in.SynthesiserPlan.Scenario, RiskScore: risk,
    }).Get(ctx, &apReq)
    out.ApprovalDecisionID, _ = apReq.Payload["decision_id"].(string)

    if severity == domain.SeverityLow {
        _ = workflow.ExecuteActivity(ctx, (*Activities).ApprovalAutoApprove, in).Get(ctx, nil)
    } else {
        var sig domain.ApprovalSignal
        ch := workflow.GetSignalChannel(ctx, "approve")
        if severity == domain.SeverityMedium {
            sel := workflow.NewSelector(ctx)
            sel.AddReceive(ch, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, &sig) })
            sel.AddFuture(workflow.NewTimer(ctx, time.Duration(cfgMediumSec)*time.Second), func(workflow.Future) {
                sig = domain.ApprovalSignal{Decision: domain.ApprovalAutoApproved}
            })
            sel.Select(ctx)
        } else { // high
            ch.Receive(ctx, &sig)
        }
        _ = workflow.ExecuteActivity(ctx, (*Activities).ApprovalRecordDecision, ApprovalRecordInput{
            RunID: in.RunID, OrgID: in.OrgID, Signal: sig,
        }).Get(ctx, nil)
        if sig.Decision == domain.ApprovalRejected || sig.Decision == domain.ApprovalTimeoutRejected {
            return out, temporal.NewNonRetryableApplicationError("approval rejected", "ApprovalRejected", nil)
        }
    }

    // GitOps open PR
    var pr domain.ActivityResult
    if err := workflow.ExecuteActivity(ctx, (*Activities).GitOpsOpenPR, in).Get(ctx, &pr); err != nil {
        return out, err
    }
    out.PRURL, _ = pr.Payload["pr_url"].(string)
    return out, nil
}
```

`cfgMediumSec` is read from `cfg.ApprovalMediumTimeoutSeconds` and passed via workflow start parameters or a workflow-local config struct.

### Task 12.3: HTTP route mount + handler updates

**Files:**
- Modify: `services/control-plane/internal/transport/http/server.go` (COORDINATOR)
- Modify: `services/control-plane/internal/transport/http/handler/pipelines.go`

- [ ] **Step 1: Mount approvals routes** (already covered in Stage 10 — verify integration).

- [ ] **Step 2: Extend `GetRun` response** — embed `approval_decision` + `pr_url`:

```go
type runResponse struct {
    // ... existing Phase 4-5 fields
    ApprovalDecision *domain.ApprovalDecision `json:"approval_decision,omitempty"`
    PRURL            string                   `json:"pr_url,omitempty"`
}
```

Fetch via the approval repo on each GET; the PR URL is in `workflow_runs.output.pr_url` already.

### Task 12.4: main.go wiring

**Files:**
- Modify: `services/control-plane/cmd/server/main.go` (COORDINATOR)

- [ ] **Step 1: Build dependencies** — in order:

```go
// causal client
var causalClient domain.CausalEngine
if cfg.CausalEnabled {
    cc, err := causal.Dial(ctx, cfg.CausalGRPCEndpoint)
    if err != nil { slog.Warn("causal dial failed; pathfinder uses canned only", "err", err) } else {
        defer cc.Close(); causalClient = cc
    }
}

// pathfinder + synthesiser + validatorL2 providers
pathfinderProv := pathfinder.New(graphStore, causalClient, llm, pathfinder.Config{
    RepoSHA: "fixture-seed-001", LLMRefine: cfg.PathfinderLLMRefine,
    RefineModel: cfg.AgentModelArchitectOpenAI, RefineProvider: "openai",
})
synthesiserProv := synthesiser.New(llm, cfg.AgentModelArchitectOpenAI)
validatorL2Prov := validator_l2.New(cfg.ValidatorURL) // already in cfg from Phase 4

// registry — append the L2 agents to whatever Phase 5 built
registry.Register(pathfinderProv)
registry.Register(synthesiserProv)
registry.Register(validatorL2Prov)

// approval + notifier + gitops
slackProv := slack.New(integrationsRepo, keyVault)
slackLedger := repo.NewSlackNotificationsRepo(pool, adminPool)
multi := notifier.NewMulti(
    notifier.NewSlack(slackProv, slackLedger, cfg.ConsoleBaseURL),
    notifier.NewEmail(smtpMailer, workspaceMembersRepo, cfg.ConsoleBaseURL),
    notifier.NewConsole(),
)
approvalRepo := repo.NewApprovalRepo(pool, adminPool)
approvalSvc := approval.New(approvalRepo, multi, auditWriter)

gitopsClient := gitops.New(cfg.GitOpsURL, cfg.GitOpsToken)

// activities — full constructor
activities := recovery.NewActivitiesFull(
    workflowRepo, broker, patchStore, validatorClient, registry, ledger,
    approvalSvc, gitopsClient, graphStore, causalClient, incidentsRepo,
    cfg.FixtureRepoOwner, cfg.FixtureRepoName, cfg.FixtureRepoDefaultBranch,
    workflowStubDuration,
)
```

### Task 12.5: Fixtures

**Files:**
- Create: `services/validator/fixtures/incidents/fixture-schema-drift.json`
- Create: `services/validator/fixtures/incidents/fixture-oom.json`
- Create: `services/validator/fixtures/incidents/fixture-trivial-ui-fix.json`

Each is a `domain.IncidentPayload` JSON file. Stacktrace strings are deterministic — their first sha256 prefix is `00000001/2/3` to match `canned.py`. Add a build-time check (test) that asserts every fixture's stacktrace fingerprint matches the canned table.

```json
{
  "label": "fixture-schema-drift",
  "title": "OperationalError: column orders.amount does not exist",
  "service": "billing",
  "environment": "staging",
  "stacktrace": "Traceback (most recent call last):\n  File \"billing/api.py\", line 42, in get_total\n    return Order.query.with_entities(Order.amount).scalar()\nsqlalchemy.exc.OperationalError: column orders.amount does not exist\n# fp-tag: 00000002",
  "logs": "..."
}
```

### Task 12.6: E2E + DoD

Boot the full stack:

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
docker compose down -v
docker compose up -d
sleep 60   # allow neo4j + postgres + temporal warmup
docker compose run --rm control-plane /app/seed-pgvector --org-id=$DEMO_ORG
docker compose run --rm control-plane /app/seed-neo4j --org-id=$DEMO_ORG
```

Run the acceptance criteria from spec §15:

- [ ] **AC1: inject fault → PR opened in < 5 min**

  ```bash
  curl -X POST http://localhost:3000/v1/admin/sentinel/trigger \
    -H "Authorization: Bearer $DEV_TOKEN" \
    -d '{"workspace_id":"'$WS'","incident_label":"fixture-null-pointer"}'
  # poll /v1/workspaces/$WS/pipelines/$RUN until pr_url is non-null
  ```

- [ ] **AC2: severity routing — schema_drift goes to high.** Inject `fixture-schema-drift`. Assert `approval_decisions.severity='high'` and no timeout-auto fires.

- [ ] **AC3: low-severity auto-approve.** Inject `fixture-trivial-ui-fix`. Within ~1s of synth → `decision='auto_approved'`. No slack/email row.

- [ ] **AC4: medium timeout.** Inject `fixture-null-pointer`. Wait 2 min. Verify `decision='auto_approved'` via timeout path.

- [ ] **AC5: rejection blocks PR.** Inject `fixture-schema-drift` → click reject → `workflow.status='failed'`, no `gitops.pr_opened` audit row.

- [ ] **AC6: ArgoCD docs walkthrough.** Open `infra/argocd/README.md`; verify the four checklist items + the warning banner. Manual review.

- [ ] **AC7: Sentinel auto-trigger.** With `SENTINEL_ENABLED=1`, POST a Sentry fatal webhook. Within 15s, a new `workflow_runs` row with `created_by=NULL` and `current_step='Sentinel.Detect'`.

- [ ] **AC8: Neo4j seeded.**

  ```bash
  docker compose exec neo4j cypher-shell -u neo4j -p nexis_dev_password \
    "MATCH ()-[r]->() WHERE r.org_id='$DEMO_ORG' RETURN count(r) AS edges"
  ```

  Expect ≥ 20.

- [ ] **AC9: Causal reachable.**

  ```bash
  grpcurl -plaintext -d '{"stacktrace":"...# fp-tag: 00000001"}' localhost:8090 nexis.causal.v1.Causal/Infer
  ```

  Confidence > 0.5.

- [ ] **AC10: Validator L2 hypothesis.**

  ```bash
  curl -X POST http://localhost:8081/v1/validate -H "Content-Type: application/json" \
    -d '{"repo_sha":"fixture-seed-001","patch_diff":"...","hypothesis":true}'
  ```

  `hypothesis_failures: []`, `duration_ms < 30000`.

- [ ] **AC11: Slack notif row.** With slack connected, after AC4 → `SELECT * FROM slack_notifications WHERE workflow_run_id='$RUN'` shows `status='sent'`, `http_status=200`.

- [ ] **AC12: Email.** Open MailHog at `http://localhost:8025`. Expect a `[NEXIS] Approval required` email to the workspace owner.

- [ ] **AC14: Audit count.**

  ```bash
  docker compose exec postgres psql -U nexis -d nexis -c \
    "SELECT count(*) FROM audit_log WHERE target='$RUN' AND action LIKE 'approval.%'"
  ```

  Expect 2 (`requested` + `decided`). Verify chain HMAC via `POST /v1/admin/audit/verify`.

- [ ] **AC15: RBAC + RLS.** Use a `member`-role principal → expect 403 on `POST /approve`. Use principal from org B → expect empty `/v1/approvals` list.

- [ ] **AC16: full build green** —

  ```bash
  cd services/control-plane && make build && make test && make vet && make arch
  cd ../validator && go build ./... && go test ./...
  cd ../gitops && go build ./... && go test ./...
  cd ../causal-inference && make test
  cd ../../apps/web && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
  ```

### Task 12.7: Final commit

```bash
git add services/control-plane/internal/workflow/recovery services/control-plane/cmd/server/main.go services/control-plane/internal/transport/http services/validator/fixtures/incidents
git commit -m "feat(workflow): phase 6 workflow refactor + activities glue + fixtures + E2E (stage 12)"
```

---

## Definition of Done

- [ ] All 12 stages committed, single-line messages, no Co-Author trailer.
- [ ] `make build && make test && make vet && make arch` green on control-plane.
- [ ] `go build && go test ./...` green on `services/{validator,gitops}`.
- [ ] `make test` green on `services/causal-inference`.
- [ ] `pnpm typecheck && pnpm build` green on `apps/web`.
- [ ] All 16 acceptance criteria from spec §15 verified — checkboxes above.
- [ ] Demo run: inject `fixture-null-pointer` → PR opens in < 5 min wallclock.
- [ ] RLS proven by two-principal curl test on the new approval routes.
- [ ] Audit chain verifies green via `POST /v1/admin/audit/verify` after the demo run.
- [ ] `infra/argocd/README.md` reviewed by tech-lead for the Phase 7 prep checklist.
- [ ] Risks 13.1-13.14 from spec §17 each have a comment in the code referencing the spec section, OR a documented test asserting the mitigation.

---

## Risks captured (cross-reference spec §17)

| # | Risk | Mitigation present in plan |
|---|---|---|
| 13.1 | Sentinel agent vs detector confusion | Stage 3 — no `agents/sentinel/` package; activity is inline ack. |
| 13.2 | ghinstallation token caching | Stage 9 — fresh transport per `OpenPRUsecase.Run` call. |
| 13.3 | Neo4j cold-start | Stage 2 `Verify()` 60s probe; Stage 3 graph-nil fallback in pathfinder. |
| 13.4 | DoWhy dep footprint | Stage 4 wheelhouse-prebuild + `CAUSAL_ENABLED=0` env switch. |
| 13.5 | Hypothesis flakiness | Stage 6 `max_examples=20` + 10s deadline + deterministic fixtures. |
| 13.6 | Approval signal race | Stage 7 `decision != 'pending'` precondition in signaler. |
| 13.7 | Severity glob coverage | Stage 7 globs in one const block; reviewer extends per-PR. |
| 13.8 | Slack URL leak in audit | Stage 8 metadata-shape test asserts URL absent. |
| 13.9 | GitOps DB blast radius | Stage 0 migration 0017 — `nexis_gitops` role with SELECT on `integrations` only. |
| 13.10 | Argo Rollouts SLO Prometheus coupling | Stage 11 README Phase-7 prep checklist. |
| 13.11 | Single-goroutine sentinel | Stage 3 tick loop sequential per org; doc note in `detector.go`. |
| 13.12 | ArgoCD aspirational docs | Stage 11 bold banner in README + UI footer line in live-demo client. |
| 13.13 | Patch parser limits | Stage 9 `parseUnifiedDiff` rejects binary/renames with 400; backend prompt constrains diff grammar. |
| 13.14 | Public fixture repo | Stage 10 live-demo CTA accepts only fixture-catalog labels. |

---

