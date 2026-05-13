# Phase 5 — Agents L1 + LLM Spine — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking. Stage 5 is the canonical **Pattern C** instance — five backend-engineers in a single dispatch message, one shard per L1 agent.

**Goal:** Replace the 5 L1 stub activities (Architect / Backend / QA / DevOps / DataEngineer) shipped by Phase 4 with real LLM-driven agent providers behind a single `domain.Agent` port, extend the dual-mode LLM spine with prompt caching + dual-model routing + cost telemetry, gate every call on per-tenant token budgets, seed pgvector with the fixture codebase for retrieval, and ship the eval harness that replays an incident across OpenAI + Ollama with a console-side matrix view.

**Architecture:** Port/adapter pattern continues. New ports go in `internal/domain/`: `agent.go`, `token_ledger.go`, `retrieval.go`. New adapter subtree `internal/adapter/agents/<name>/` — one Provider per agent + a Registry. Existing `internal/adapter/llm/{openai,ollama,factory}.go` are extended in place — never rewritten — to support prompt caching, dual-model routing, and embeddings. `internal/workflow/recovery/activities.go` keeps its dependency surface and gains one new field (`Agents *agents.Registry`); the 5 stub bodies become 5 calls into the registry. pgvector lives behind `internal/adapter/retrieval/pgvector.go`. The eval harness uses a per-run-scoped registry pair to avoid cross-provider contamination.

**Tech Stack:** Go 1.25, `github.com/pgvector/pgvector-go` v0.2+, OpenAI Chat Completions API + `cache_control`-extended Messages API, Ollama `/api/chat` + `/api/embeddings`, Postgres 16 + `vector` extension, Temporal SDK (unchanged from Phase 4), Next.js 16.2.2 + React 19 + Monaco (already used in `/console/integrations`), shadcn/ui + Tremor.

**Spec:** `docs/superpowers/specs/2026-05-13-phase-5-agents-l1-llm-spine.md`.

---

## Salvage / Reuse from Phases 1–4

- `internal/adapter/llm/{openai,ollama,factory}.go` — **extend; never rewrite.** Phase 5 adds `CacheSystem`, `CachedTokens`, `Embed()`, and cost calculation. The existing `Complete()` body and types stay.
- `internal/domain/{ports.go,errors.go,audit.go,workflow.go,workspace.go,patchstore.go}` — extend. New ports get their own files (`agent.go`, `token_ledger.go`, `retrieval.go`).
- `internal/workflow/recovery/{activities.go,workflow.go,types.go}` — keep ordering + retry policies; only swap the 5 L1 method bodies and extend `Activities` with `Agents *agents.Registry` + `Ledger domain.TokenLedger` fields.
- `internal/adapter/repo/billing_repo.go` — copy the **dual-pool pattern** (`pool` + `adminPool`, `db.FromCtx` for RLS-aware reads, bare `adminPool` for cron paths) into the three new repos.
- `internal/adapter/repo/workflow_repo.go` — copy the **admin-pool insert pattern** for the system-job path the agents take (the LLM activity does not run inside an RLS request tx).
- `internal/platform/sse/broker.go` — unchanged; not reused by Phase 5 except via Phase 4 timeline events.
- `internal/platform/cron/cron.go` — extended with one new job `token_budgets_rotate`.
- `internal/adapter/audit/*` — reused as-is via `domain.AuditWriter`; new metadata blob is just a `map[string]any`.
- `apps/web/lib/pipelines.ts` SDK pattern — copied to `apps/web/lib/eval.ts`.
- `apps/web/components/pipelines/StatusPill.tsx` + `AgentIcon.tsx` — reused on the eval matrix.
- `apps/web/components/integrations/*` Monaco wrapper — reused (Phase 3 already pulled in Monaco) for the eval diff viewer.
- `services/validator/fixtures/` — the existing 12-test Python repo is the retrieval corpus. New: `services/validator/fixtures/incidents/<label>.json` files seed the demo incidents.
- `docker-compose.yml` — extend; do not rewrite. Postgres needs `vector` extension installed in its image — we extend the existing `postgres` service definition with the `pgvector/pgvector:pg16` image (binary-compatible with `postgres:16` for our schemas).

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

## Stage 0 — Schema + RLS + pgvector extension + config + env

### Task 0.1: Drizzle schema additions

**Files:**
- Modify: `packages/db/schema.ts`

- [ ] **Step 1: Append after the Phase 4 `activityEvents` block:**

```ts
// ---------------------------------------------------------------------------
// Phase 5 — Agents L1 + LLM spine (token ledger, pgvector retrieval, eval).
// ---------------------------------------------------------------------------

export const tokenBudgets = pgTable("token_budgets", {
  id:                uuid("id").primaryKey().defaultRandom(),
  orgId:             uuid("org_id").notNull().references(() => organizations.id),
  periodStart:       timestamp("period_start", { withTimezone: true }).notNull(),
  periodEnd:         timestamp("period_end",   { withTimezone: true }).notNull(),
  allowedTokensIn:   bigint("allowed_tokens_in",  { mode: "number" }).notNull(),
  allowedTokensOut:  bigint("allowed_tokens_out", { mode: "number" }).notNull(),
  usedTokensIn:      bigint("used_tokens_in",     { mode: "number" }).notNull().default(0),
  usedTokensOut:     bigint("used_tokens_out",    { mode: "number" }).notNull().default(0),
  createdAt:         timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  updatedAt:         timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqOrgPeriod: uniqueIndex("token_budgets_org_period_uniq").on(t.orgId, t.periodStart),
}));

export const tokenLedger = pgTable("token_ledger", {
  id:                uuid("id").primaryKey().defaultRandom(),
  orgId:             uuid("org_id").notNull().references(() => organizations.id),
  workflowRunId:     uuid("workflow_run_id").references(() => workflowRuns.id, { onDelete: "set null" }),
  agent:             text("agent").notNull(),
  model:             text("model").notNull(),
  provider:          text("provider").notNull(),
  tokensIn:          integer("tokens_in").notNull(),
  tokensOut:         integer("tokens_out").notNull(),
  cachedTokens:      integer("cached_tokens").notNull().default(0),
  costCents:         numeric("cost_cents", { precision: 20, scale: 6 }).notNull().default("0"),
  durationMs:        integer("duration_ms").notNull(),
  status:            text("status").notNull(),
  recordedAt:        timestamp("recorded_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  orgRecordedIdx: index("token_ledger_org_recorded_idx").on(t.orgId, t.recordedAt),
  runIdx:         index("token_ledger_run_idx").on(t.workflowRunId),
}));

// pgvector column is declared via raw SQL in the migration; Drizzle Studio
// shows it as `unknown`. The Go side reads/writes via pgvector-go.
export const codeEmbeddings = pgTable("code_embeddings", {
  id:           uuid("id").primaryKey().defaultRandom(),
  orgId:        uuid("org_id").notNull().references(() => organizations.id),
  repoSha:      text("repo_sha").notNull(),
  filePath:     text("file_path").notNull(),
  chunkStart:   integer("chunk_start").notNull(),
  chunkEnd:     integer("chunk_end").notNull(),
  content:      text("content").notNull(),
  // embedding vector(1536) — added via custom() / raw SQL in 0011.up.sql
  createdAt:    timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  orgRepoIdx: index("code_embeddings_org_repo_idx").on(t.orgId, t.repoSha),
}));

const evalRunStatus = ["queued", "running", "completed", "failed"] as const;

export const evalRuns = pgTable("eval_runs", {
  id:                 uuid("id").primaryKey().defaultRandom(),
  orgId:              uuid("org_id").notNull().references(() => organizations.id),
  triggeredBy:        uuid("triggered_by").references(() => users.id),
  incidentLabel:      text("incident_label").notNull(),
  status:             text("status", { enum: evalRunStatus }).notNull(),
  providers:          text("providers").array().notNull(),
  openaiRunId:        uuid("openai_run_id").references(() => workflowRuns.id),
  ollamaRunId:        uuid("ollama_run_id").references(() => workflowRuns.id),
  openaiCostCents:    numeric("openai_cost_cents", { precision: 20, scale: 6 }),
  ollamaCostCents:    numeric("ollama_cost_cents", { precision: 20, scale: 6 }),
  openaiTokensIn:     bigint("openai_tokens_in",  { mode: "number" }),
  openaiTokensOut:    bigint("openai_tokens_out", { mode: "number" }),
  ollamaTokensIn:     bigint("ollama_tokens_in",  { mode: "number" }),
  ollamaTokensOut:    bigint("ollama_tokens_out", { mode: "number" }),
  startedAt:          timestamp("started_at",   { withTimezone: true }).notNull().defaultNow(),
  completedAt:        timestamp("completed_at", { withTimezone: true }),
  error:              text("error"),
}, t => ({
  orgStartedIdx: index("eval_runs_org_started_idx").on(t.orgId, t.startedAt),
}));

export const evalTranscripts = pgTable("eval_transcripts", {
  id:                uuid("id").primaryKey().defaultRandom(),
  orgId:             uuid("org_id").notNull().references(() => organizations.id),
  evalRunId:         uuid("eval_run_id").notNull().references(() => evalRuns.id, { onDelete: "cascade" }),
  provider:          text("provider").notNull(),
  agent:             text("agent").notNull(),
  model:             text("model").notNull(),
  systemPrompt:      text("system_prompt").notNull(),
  userPrompt:        text("user_prompt").notNull(),
  assistantOutput:   text("assistant_output").notNull(),
  tokensIn:          integer("tokens_in").notNull(),
  tokensOut:         integer("tokens_out").notNull(),
  cachedTokens:      integer("cached_tokens").notNull().default(0),
  costCents:         numeric("cost_cents", { precision: 20, scale: 6 }).notNull().default("0"),
  durationMs:        integer("duration_ms").notNull(),
  schemaValid:       boolean("schema_valid").notNull(),
  recordedAt:        timestamp("recorded_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqEvalProviderAgent: uniqueIndex("eval_transcripts_eval_provider_agent_uniq").on(t.evalRunId, t.provider, t.agent),
}));
```

Imports already cover all referenced helpers from Phase 4.

- [ ] **Step 2: Generate**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm exec drizzle-kit generate --name=phase5_agents
```

Expected output: a single new migration file at `packages/db/migrations/0005_*_phase5_agents.sql` with five `CREATE TABLE` statements. The pgvector column is not emitted by drizzle — we add it in the control-plane SQL migration below.

### Task 0.2: Control-plane mirror migrations

**Files:**
- Create: `services/control-plane/migrations/0011_phase5_agents.up.sql`
- Create: `services/control-plane/migrations/0011_phase5_agents.down.sql`
- Create: `services/control-plane/migrations/0012_phase5_pgvector.up.sql`
- Create: `services/control-plane/migrations/0012_phase5_pgvector.down.sql`
- Create: `services/control-plane/migrations/0013_phase5_rls.up.sql`
- Create: `services/control-plane/migrations/0013_phase5_rls.down.sql`

- [ ] **Step 1: Up — token_budgets + token_ledger + eval_runs + eval_transcripts**

`0011_phase5_agents.up.sql`:

```sql
CREATE TABLE token_budgets (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              uuid NOT NULL REFERENCES organizations(id),
  period_start        timestamptz NOT NULL,
  period_end          timestamptz NOT NULL,
  allowed_tokens_in   bigint NOT NULL,
  allowed_tokens_out  bigint NOT NULL,
  used_tokens_in      bigint NOT NULL DEFAULT 0,
  used_tokens_out     bigint NOT NULL DEFAULT 0,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, period_start)
);

CREATE TABLE token_ledger (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              uuid NOT NULL REFERENCES organizations(id),
  workflow_run_id     uuid REFERENCES workflow_runs(id) ON DELETE SET NULL,
  agent               text NOT NULL,
  model               text NOT NULL,
  provider            text NOT NULL,
  tokens_in           int  NOT NULL,
  tokens_out          int  NOT NULL,
  cached_tokens       int  NOT NULL DEFAULT 0,
  cost_cents          numeric(20, 6) NOT NULL DEFAULT 0,
  duration_ms         int  NOT NULL,
  status              text NOT NULL CHECK (status IN ('succeeded','schema_mismatch','budget_exceeded','provider_error')),
  recorded_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX token_ledger_org_recorded_idx ON token_ledger (org_id, recorded_at DESC);
CREATE INDEX token_ledger_run_idx          ON token_ledger (workflow_run_id);

CREATE TABLE eval_runs (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              uuid NOT NULL REFERENCES organizations(id),
  triggered_by        uuid REFERENCES users(id),
  incident_label      text NOT NULL,
  status              text NOT NULL CHECK (status IN ('queued','running','completed','failed')),
  providers           text[] NOT NULL,
  openai_run_id       uuid REFERENCES workflow_runs(id),
  ollama_run_id       uuid REFERENCES workflow_runs(id),
  openai_cost_cents   numeric(20, 6),
  ollama_cost_cents   numeric(20, 6),
  openai_tokens_in    bigint,
  openai_tokens_out   bigint,
  ollama_tokens_in    bigint,
  ollama_tokens_out   bigint,
  started_at          timestamptz NOT NULL DEFAULT now(),
  completed_at        timestamptz,
  error               text
);
CREATE INDEX eval_runs_org_started_idx ON eval_runs (org_id, started_at DESC);

CREATE TABLE eval_transcripts (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              uuid NOT NULL REFERENCES organizations(id),
  eval_run_id         uuid NOT NULL REFERENCES eval_runs(id) ON DELETE CASCADE,
  provider            text NOT NULL,
  agent               text NOT NULL,
  model               text NOT NULL,
  system_prompt       text NOT NULL,
  user_prompt         text NOT NULL,
  assistant_output    text NOT NULL,
  tokens_in           int NOT NULL,
  tokens_out          int NOT NULL,
  cached_tokens       int NOT NULL DEFAULT 0,
  cost_cents          numeric(20, 6) NOT NULL DEFAULT 0,
  duration_ms         int NOT NULL,
  schema_valid        boolean NOT NULL,
  recorded_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (eval_run_id, provider, agent)
);
```

`0011_phase5_agents.down.sql`:

```sql
DROP TABLE IF EXISTS eval_transcripts;
DROP TABLE IF EXISTS eval_runs;
DROP TABLE IF EXISTS token_ledger;
DROP TABLE IF EXISTS token_budgets;
```

- [ ] **Step 2: pgvector extension + code_embeddings**

`0012_phase5_pgvector.up.sql`:

```sql
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE code_embeddings (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL REFERENCES organizations(id),
  repo_sha      text NOT NULL,
  file_path     text NOT NULL,
  chunk_start   int  NOT NULL,
  chunk_end     int  NOT NULL,
  content       text NOT NULL,
  embedding     vector(1536) NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX code_embeddings_org_repo_idx ON code_embeddings (org_id, repo_sha);

-- ivfflat index is created post-seed (centroid initialization requires data).
-- See cmd/seed-pgvector/main.go.
```

`0012_phase5_pgvector.down.sql`:

```sql
DROP TABLE IF EXISTS code_embeddings;
-- Leave the `vector` extension installed; dropping it would break any other
-- consumer in the future. Pure ALTER is a no-op for downgrades.
```

- [ ] **Step 3: RLS + grants**

`0013_phase5_rls.up.sql`:

```sql
ALTER TABLE token_budgets     ENABLE ROW LEVEL SECURITY;
ALTER TABLE token_ledger      ENABLE ROW LEVEL SECURITY;
ALTER TABLE code_embeddings   ENABLE ROW LEVEL SECURITY;
ALTER TABLE eval_runs         ENABLE ROW LEVEL SECURITY;
ALTER TABLE eval_transcripts  ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON token_budgets
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON token_ledger
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON code_embeddings
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON eval_runs
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON eval_transcripts
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON token_budgets    TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON token_ledger     TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON code_embeddings  TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON eval_runs        TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON eval_transcripts TO nexis_app;
```

`0013_phase5_rls.down.sql`:

```sql
DROP POLICY IF EXISTS tenant_isolation ON eval_transcripts;
DROP POLICY IF EXISTS tenant_isolation ON eval_runs;
DROP POLICY IF EXISTS tenant_isolation ON code_embeddings;
DROP POLICY IF EXISTS tenant_isolation ON token_ledger;
DROP POLICY IF EXISTS tenant_isolation ON token_budgets;
ALTER TABLE eval_transcripts DISABLE ROW LEVEL SECURITY;
ALTER TABLE eval_runs        DISABLE ROW LEVEL SECURITY;
ALTER TABLE code_embeddings  DISABLE ROW LEVEL SECURITY;
ALTER TABLE token_ledger     DISABLE ROW LEVEL SECURITY;
ALTER TABLE token_budgets    DISABLE ROW LEVEL SECURITY;
```

- [ ] **Step 4: Apply migrations against the dev DB**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make migrate-up
# Expect output: "0011 ... applied", "0012 ... applied", "0013 ... applied"
docker compose exec -T postgres psql -U nexis -d nexis -c "\dt"  | grep -E "token_|code_embeddings|eval_"
docker compose exec -T postgres psql -U nexis -d nexis -c "\dx vector"  # confirms extension present
```

### Task 0.3: Config + env

**Files:**
- Modify: `services/control-plane/internal/platform/config/config.go` (COORDINATOR)

- [ ] **Step 1: Append Phase 5 fields to `Config`**

After the Phase 4 `WorkflowStubDurationMs` field, add:

```go
// Phase 5 — agents L1 + LLM spine.
TokenBudgetTokensIn    int64
TokenBudgetTokensOut   int64
TokenBudgetPeriodDays  int
OpenAIEmbedModel       string
OllamaEmbedModel       string
OpenAIPromptCache      bool
EvalEnabled            bool
AgentSchemaRetryMax    int
AgentModelArchitectOpenAI    string
AgentModelBackendOpenAI      string
AgentModelQAOpenAI           string
AgentModelDevOpsOpenAI       string
AgentModelDataEngOpenAI      string
AgentModelArchitectOllama    string
AgentModelBackendOllama      string
AgentModelQAOllama           string
AgentModelDevOpsOllama       string
AgentModelDataEngOllama      string
```

- [ ] **Step 2: Append to `Load()`** (snake-case env reads):

```go
TokenBudgetTokensIn:   int64(envInt("TOKEN_BUDGET_TOKENS_IN",  1_000_000)),
TokenBudgetTokensOut:  int64(envInt("TOKEN_BUDGET_TOKENS_OUT",   200_000)),
TokenBudgetPeriodDays: envInt("TOKEN_BUDGET_PERIOD_DAYS", 30),
OpenAIEmbedModel:      env("OPENAI_EMBED_MODEL", "text-embedding-3-small"),
OllamaEmbedModel:      env("OLLAMA_EMBED_MODEL", "nomic-embed-text"),
OpenAIPromptCache:     parseBool(env("OPENAI_PROMPT_CACHE", "1")),
EvalEnabled:           parseBool(env("EVAL_ENABLED", "1")),
AgentSchemaRetryMax:   envInt("AGENT_SCHEMA_RETRY_MAX", 2),

AgentModelArchitectOpenAI:  env("AGENT_MODEL_ARCHITECT_OPENAI", "gpt-4o-mini"),
AgentModelBackendOpenAI:    env("AGENT_MODEL_BACKEND_OPENAI",   "gpt-4o"),
AgentModelQAOpenAI:         env("AGENT_MODEL_QA_OPENAI",        "gpt-4o-mini"),
AgentModelDevOpsOpenAI:     env("AGENT_MODEL_DEVOPS_OPENAI",    "gpt-4o-mini"),
AgentModelDataEngOpenAI:    env("AGENT_MODEL_DATAENG_OPENAI",   "gpt-4o-mini"),

AgentModelArchitectOllama:  env("AGENT_MODEL_ARCHITECT_OLLAMA", "llama3.1:8b"),
AgentModelBackendOllama:    env("AGENT_MODEL_BACKEND_OLLAMA",   "qwen2.5-coder:14b"),
AgentModelQAOllama:         env("AGENT_MODEL_QA_OLLAMA",        "llama3.1:8b"),
AgentModelDevOpsOllama:     env("AGENT_MODEL_DEVOPS_OLLAMA",    "llama3.1:8b"),
AgentModelDataEngOllama:    env("AGENT_MODEL_DATAENG_OLLAMA",   "llama3.1:8b"),
```

### Task 0.4: docker-compose Postgres → pgvector image

**Files:**
- Modify: `docker-compose.yml` (COORDINATOR)

- [ ] **Step 1: Swap the `postgres` service image to `pgvector/pgvector:pg16`**

Locate the existing `postgres:` service. The current line `image: postgres:16` becomes:

```yaml
  postgres:
    image: pgvector/pgvector:pg16
```

Everything else (volumes, healthcheck, ports, env) stays identical — the pgvector image is the official Postgres 16 image with `vector` precompiled. No data migration is needed for a fresh dev volume; if a contributor has an existing volume, the migration in Task 0.2 step 2 runs `CREATE EXTENSION` so the row layout works on either image.

- [ ] **Step 2: Add `seed-pgvector` + `eval` to the control-plane build target**

The control-plane Dockerfile currently builds `cmd/server` only. Add a second `RUN go build -o /app/seed-pgvector ./cmd/seed-pgvector` and `RUN go build -o /app/eval ./cmd/eval` after the existing `go build -o /app/server` step. These binaries are not started by the entrypoint; they're invoked via `docker compose run --rm control-plane /app/<binary>`.

- [ ] **Step 3: Rebuild + verify**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
docker compose down -v   # only on clean dev — wipes the volume
docker compose up -d postgres
docker compose exec postgres psql -U nexis -d nexis -c "SELECT extname FROM pg_extension WHERE extname='vector';"
# expect: vector
```

### Task 0.5: .arch.yaml — declare `agents` component

**Files:**
- Modify: `services/control-plane/.arch.yaml` (COORDINATOR)

- [ ] **Step 1: Append to `components:`**

```yaml
  agents:
    in: internal/adapter/agents/**
```

- [ ] **Step 2: Append to `deps:`**

Phase 5 adds `agents` as a peer of `adapter`. The dependency rules:
- `agents` may depend on `domain`, `platform`, `adapter` (so `agents/<name>` can use the LLM adapter + retrieval adapter directly).
- `workflow` may depend on `agents` (so activities call into the registry).
- `adapter` may depend on `agents` (so the registry can live in the adapter tree without breaking the existing adapter→adapter rule).

Update the deps block:

```yaml
  agents:
    mayDependOn: [domain, platform, adapter]
  workflow:
    mayDependOn: [usecase, domain, adapter, platform, agents]
  adapter:
    mayDependOn: [domain, platform, adapter, workflow, agents]
```

The other rules stay unchanged.

### Task 0.6: Commit Stage 0

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm typecheck
git add services/control-plane/migrations packages/db services/control-plane/internal/platform/config docker-compose.yml services/control-plane/.arch.yaml services/control-plane/Dockerfile
git commit -m "feat(db): phase 5 token ledger + pgvector + eval schema + config (stage 0)"
```

---

## Stage 1 — Domain ports

### Task 1.1: Agent port

**Files:**
- Create: `services/control-plane/internal/domain/agent.go`

- [ ] **Step 1: Write the file**

```go
package domain

import "context"

// AgentName names the 5 L1 agents (Phase 5) and the 4 L2 agents (Phase 6).
// Phase 5 ships only the L1 set; Phase 6 fills in the rest. Keep the wire
// value snake_case to match AgentRole — the two enums share string values so
// activity_events.agent_role + token_ledger.agent join naturally on text.
type AgentName string

const (
    AgentNameArchitect    AgentName = "architect"
    AgentNameBackend      AgentName = "backend"
    AgentNameQA           AgentName = "qa"
    AgentNameDevOps       AgentName = "devops"
    AgentNameDataEngineer AgentName = "data_engineer"
)

// AllL1Agents lets the eval harness + factory iterate without hard-coding.
var AllL1Agents = []AgentName{
    AgentNameArchitect, AgentNameBackend, AgentNameQA,
    AgentNameDevOps, AgentNameDataEngineer,
}

// IncidentPayload is the minimal shape consumed by L1 agents in Phase 5. The
// demo usecase fills this from services/validator/fixtures/incidents/<label>.json.
type IncidentPayload struct {
    Label       string `json:"label"`
    Title       string `json:"title"`
    Service     string `json:"service"`
    Environment string `json:"environment"`
    Stacktrace  string `json:"stacktrace,omitempty"`
    Logs        string `json:"logs,omitempty"`
}

// AgentInput is what every agent reads. PriorOutputs is the {agent_name → Structured}
// map from earlier steps in the DAG (Architect.Structured keyed at "architect", etc.).
type AgentInput struct {
    WorkflowRunID string                 `json:"workflow_run_id"`
    OrgID         string                 `json:"org_id"`
    WorkspaceID   string                 `json:"workspace_id"`
    PriorOutputs  map[string]any         `json:"prior_outputs,omitempty"`
    PromptContext string                 `json:"prompt_context"`
    Incident      *IncidentPayload       `json:"incident,omitempty"`
    RepoSHA       string                 `json:"repo_sha,omitempty"` // retrieval scope
}

// AgentOutput is what every agent returns. Structured is the schema-validated
// JSON payload; Content is the raw model text for transcripts + diffing.
type AgentOutput struct {
    Success      bool           `json:"success"`
    Content      string         `json:"content"`
    Structured   map[string]any `json:"structured"`
    TokensIn     int            `json:"tokens_in"`
    TokensOut    int            `json:"tokens_out"`
    CachedTokens int            `json:"cached_tokens,omitempty"`
    CostCents    float64        `json:"cost_cents"`
    DurationMs   int64          `json:"duration_ms"`
    Model        string         `json:"model"`
    Provider     string         `json:"provider"`
    SchemaRetries int           `json:"schema_retries"`
    SystemPrompt string         `json:"system_prompt,omitempty"`
    UserPrompt   string         `json:"user_prompt,omitempty"`
}

// Agent is the single port every L1/L2 agent implements.
type Agent interface {
    Name() AgentName
    Run(ctx context.Context, in AgentInput) (AgentOutput, error)
}
```

### Task 1.2: TokenLedger port

**Files:**
- Create: `services/control-plane/internal/domain/token_ledger.go`

- [ ] **Step 1: Write the file**

```go
package domain

import (
    "context"
    "time"
)

// TokenBudget is the per-org per-period allotment. Period is rolling: the
// cron rotation job opens a new period when the previous one ends.
type TokenBudget struct {
    OrgID             string
    PeriodStart       time.Time
    PeriodEnd         time.Time
    AllowedTokensIn   int64
    AllowedTokensOut  int64
    UsedTokensIn      int64
    UsedTokensOut     int64
}

// TokenLedgerEntry is one row in token_ledger. Status is one of:
//   succeeded | schema_mismatch | budget_exceeded | provider_error
type TokenLedgerEntry struct {
    OrgID          string
    WorkflowRunID  string  // empty string when not workflow-bound (e.g. seed)
    Agent          AgentName
    Model          string
    Provider       string
    TokensIn       int
    TokensOut      int
    CachedTokens   int
    CostCents      float64
    DurationMs     int
    Status         string
}

// TokenLedger is the port the agents layer + cron depend on. Implementations
// live in internal/adapter/repo/token_ledger_repo.go (Postgres-backed).
type TokenLedger interface {
    // CheckBudget returns ErrBudgetExceeded if the current period's
    // used_tokens_in + expectedTokensIn would breach allowed_tokens_in.
    // expectedTokensIn is a rough upper bound the agent passes; the
    // implementation is intentionally permissive (only checks input tokens,
    // because output tokens are unknown until completion).
    CheckBudget(ctx context.Context, orgID string, expectedTokensIn int) error

    // Record persists one row + updates token_budgets atomically inside a tx.
    Record(ctx context.Context, e TokenLedgerEntry) error

    // GetBudget returns the current period's budget, creating one with the
    // configured defaults if missing. Caller is expected to be inside an RLS
    // tx (request-scoped) or to use an admin-pool variant for cron paths.
    GetBudget(ctx context.Context, orgID string) (TokenBudget, error)

    // RotatePeriods is called by the daily cron job — closes finished periods
    // and opens fresh ones for every org with a previously open period.
    RotatePeriods(ctx context.Context, now time.Time) error
}
```

### Task 1.3: RetrievalStore + EmbeddingProvider ports

**Files:**
- Create: `services/control-plane/internal/domain/retrieval.go`

- [ ] **Step 1: Write the file**

```go
package domain

import "context"

// Chunk is the unit of retrieval. The pgvector index is on embedding; the
// repo writes chunk_start + chunk_end + content alongside.
type Chunk struct {
    OrgID       string
    RepoSHA     string
    FilePath    string
    ChunkStart  int     // 1-indexed
    ChunkEnd    int
    Content     string
    Embedding   []float32 // 1536 dims for text-embedding-3-small
    Similarity  float32   // populated by TopK; 1.0 = perfect match
}

// RetrievalStore is the port the agents layer (+ seed CLI) depend on.
// Implementations live in internal/adapter/retrieval/pgvector.go.
type RetrievalStore interface {
    Insert(ctx context.Context, batch []Chunk) error
    TopK(ctx context.Context, orgID, repoSHA string, query []float32, k int) ([]Chunk, error)
}

// EmbeddingProvider is implemented by both the OpenAI and Ollama LLM
// adapters. The factory composes one alongside the LLMProvider; the agents
// layer + seed CLI consume it.
type EmbeddingProvider interface {
    Name() string
    Embed(ctx context.Context, model string, texts []string) ([][]float32, error)
    EmbeddingDims() int
}
```

### Task 1.4: Extend `errors.go` + `ports.go`

**Files:**
- Modify: `services/control-plane/internal/domain/errors.go`
- Modify: `services/control-plane/internal/domain/ports.go`

- [ ] **Step 1: errors.go — append**

```go
ErrBudgetExceeded     = errors.New("token budget exceeded")
ErrAgentSchemaMismatch = errors.New("agent output failed schema validation")
ErrEmbeddingFailed    = errors.New("embedding provider failed")
```

- [ ] **Step 2: ports.go — extend `CompletionRequest` + `CompletionResponse`**

Replace the existing struct definitions with:

```go
type CompletionRequest struct {
    Model        string  `json:"model"`
    System       string  `json:"system,omitempty"`
    Prompt       string  `json:"prompt"`
    MaxTokens    int     `json:"max_tokens,omitempty"`
    Temperature  float32 `json:"temperature,omitempty"`
    JSONResponse bool    `json:"json_response,omitempty"`
    CacheSystem  bool    `json:"cache_system,omitempty"`
    Modality     string  `json:"modality,omitempty"` // 'chat' (default) | 'embed'
}

type CompletionResponse struct {
    Content       string  `json:"content"`
    InputTokens   int     `json:"input_tokens"`
    OutputTokens  int     `json:"output_tokens"`
    CachedTokens  int     `json:"cached_tokens,omitempty"`
    Model         string  `json:"model"`
    DurationMs    int64   `json:"duration_ms,omitempty"`
    CostCents     float64 `json:"cost_cents,omitempty"`
}
```

### Task 1.5: Commit Stage 1

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make vet && make arch
git add internal/domain
git commit -m "feat(domain): phase 5 agent + token ledger + retrieval ports (stage 1)"
```

---

## Stage 2 — TokenLedger implementation + cron rotation

### Task 2.1: Repo

**Files:**
- Create: `services/control-plane/internal/adapter/repo/token_ledger_repo.go`
- Create: `services/control-plane/internal/adapter/repo/token_ledger_repo_test.go`

- [ ] **Step 1: Write the repo**

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

// TokenLedgerRepo dual-pools the way BillingRepo does: app pool for
// RLS-scoped HTTP reads, admin pool for activity-side writes that run
// outside any request tx.
type TokenLedgerRepo struct {
    pool      *pgxpool.Pool
    adminPool *pgxpool.Pool
    cfg       TokenLedgerConfig
}

type TokenLedgerConfig struct {
    AllowedTokensIn  int64
    AllowedTokensOut int64
    PeriodDays       int
}

func NewTokenLedgerRepo(pool, adminPool *pgxpool.Pool, cfg TokenLedgerConfig) *TokenLedgerRepo {
    if adminPool == nil {
        adminPool = pool
    }
    if cfg.PeriodDays <= 0 {
        cfg.PeriodDays = 30
    }
    return &TokenLedgerRepo{pool: pool, adminPool: adminPool, cfg: cfg}
}

func (r *TokenLedgerRepo) CheckBudget(ctx context.Context, orgID string, expectedTokensIn int) error {
    b, err := r.GetBudget(ctx, orgID)
    if err != nil {
        return err
    }
    if b.UsedTokensIn+int64(expectedTokensIn) > b.AllowedTokensIn {
        return domain.ErrBudgetExceeded
    }
    if b.UsedTokensOut >= b.AllowedTokensOut {
        return domain.ErrBudgetExceeded
    }
    return nil
}

// GetBudget reads or lazily creates the current period's row. Uses the admin
// pool so it works from both HTTP handlers (which RLS-set first via ctx) and
// activities (which have no ctx-bound tx).
func (r *TokenLedgerRepo) GetBudget(ctx context.Context, orgID string) (domain.TokenBudget, error) {
    var b domain.TokenBudget
    now := time.Now().UTC().Truncate(time.Microsecond)
    periodStart := now.Truncate(24 * time.Hour) // midnight UTC of today
    periodEnd := periodStart.AddDate(0, 0, r.cfg.PeriodDays)

    // INSERT … ON CONFLICT DO NOTHING then SELECT — two roundtrips but
    // race-safe across concurrent first-time calls.
    _, err := r.adminPool.Exec(ctx, `
        INSERT INTO token_budgets
          (org_id, period_start, period_end, allowed_tokens_in, allowed_tokens_out)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (org_id, period_start) DO NOTHING`,
        orgID, periodStart, periodEnd, r.cfg.AllowedTokensIn, r.cfg.AllowedTokensOut)
    if err != nil {
        return b, err
    }

    err = r.adminPool.QueryRow(ctx, `
        SELECT org_id, period_start, period_end,
               allowed_tokens_in, allowed_tokens_out,
               used_tokens_in,    used_tokens_out
        FROM token_budgets
        WHERE org_id=$1 AND period_start <= $2 AND period_end > $2
        ORDER BY period_start DESC
        LIMIT 1`, orgID, now).Scan(
        &b.OrgID, &b.PeriodStart, &b.PeriodEnd,
        &b.AllowedTokensIn, &b.AllowedTokensOut,
        &b.UsedTokensIn, &b.UsedTokensOut,
    )
    if errors.Is(err, pgx.ErrNoRows) {
        return b, domain.ErrNotFound
    }
    return b, err
}

func (r *TokenLedgerRepo) Record(ctx context.Context, e domain.TokenLedgerEntry) error {
    tx, err := r.adminPool.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx) //nolint:errcheck

    var runID any
    if e.WorkflowRunID != "" {
        runID = e.WorkflowRunID
    }
    _, err = tx.Exec(ctx, `
        INSERT INTO token_ledger
          (org_id, workflow_run_id, agent, model, provider,
           tokens_in, tokens_out, cached_tokens, cost_cents,
           duration_ms, status, recorded_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
        e.OrgID, runID, string(e.Agent), e.Model, e.Provider,
        e.TokensIn, e.TokensOut, e.CachedTokens, e.CostCents,
        e.DurationMs, e.Status, time.Now().UTC().Truncate(time.Microsecond))
    if err != nil {
        return err
    }

    // Only bump used counters for non-error rows so a failed schema retry
    // doesn't double-charge the org for the schema retry's tokens. Actually
    // — we DO want to bump for any call that hit the provider, so retries
    // are reflected. Only budget_exceeded skips because it never reached the
    // provider.
    if e.Status != "budget_exceeded" {
        _, err = tx.Exec(ctx, `
            UPDATE token_budgets
            SET used_tokens_in  = used_tokens_in  + $2,
                used_tokens_out = used_tokens_out + $3,
                updated_at      = now()
            WHERE org_id=$1
              AND period_start <= now()
              AND period_end   > now()`,
            e.OrgID, e.TokensIn, e.TokensOut)
        if err != nil {
            return err
        }
    }

    return tx.Commit(ctx)
}

func (r *TokenLedgerRepo) RotatePeriods(ctx context.Context, now time.Time) error {
    // For every org whose latest period ended before `now`, open a fresh
    // period starting at the previous period_end. Idempotent via UNIQUE
    // (org_id, period_start).
    _, err := r.adminPool.Exec(ctx, `
        INSERT INTO token_budgets
          (org_id, period_start, period_end, allowed_tokens_in, allowed_tokens_out)
        SELECT DISTINCT ON (org_id)
          org_id,
          period_end                              AS period_start,
          period_end + ($1 || ' days')::interval  AS period_end,
          $2 AS allowed_tokens_in,
          $3 AS allowed_tokens_out
        FROM token_budgets
        WHERE period_end <= $4
        ORDER BY org_id, period_end DESC
        ON CONFLICT (org_id, period_start) DO NOTHING`,
        r.cfg.PeriodDays, r.cfg.AllowedTokensIn, r.cfg.AllowedTokensOut, now)
    return err
}

// ListLedgerByRun returns the per-agent rollup for one workflow run. Used by
// the modified GET /v1/workspaces/{ws}/pipelines/{run_id} handler. App pool
// + db.FromCtx — RLS-scoped.
func (r *TokenLedgerRepo) ListLedgerByRun(ctx context.Context, runID string) ([]domain.TokenLedgerEntry, error) {
    q := db.FromCtx(ctx, r.pool)
    rows, err := q.Query(ctx, `
        SELECT org_id, COALESCE(workflow_run_id::text,''), agent, model, provider,
               tokens_in, tokens_out, cached_tokens, cost_cents::float8,
               duration_ms, status
        FROM token_ledger
        WHERE workflow_run_id=$1
        ORDER BY recorded_at`, runID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    out := []domain.TokenLedgerEntry{}
    for rows.Next() {
        var e domain.TokenLedgerEntry
        var agent string
        if err := rows.Scan(&e.OrgID, &e.WorkflowRunID, &agent, &e.Model, &e.Provider,
            &e.TokensIn, &e.TokensOut, &e.CachedTokens, &e.CostCents,
            &e.DurationMs, &e.Status); err != nil {
            return nil, err
        }
        e.Agent = domain.AgentName(agent)
        out = append(out, e)
    }
    return out, rows.Err()
}
```

- [ ] **Step 2: Test (skip without DATABASE_URL_TEST)**

```go
//go:build integration

package repo

import (
    "context"
    "os"
    "testing"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func TestTokenLedger_BudgetEnforcement(t *testing.T) {
    url := os.Getenv("DATABASE_URL_TEST")
    if url == "" { t.Skip("DATABASE_URL_TEST unset") }
    ctx := context.Background()
    pool, err := pgxpool.New(ctx, url)
    if err != nil { t.Fatal(err) }
    defer pool.Close()

    r := NewTokenLedgerRepo(pool, pool, TokenLedgerConfig{
        AllowedTokensIn: 100, AllowedTokensOut: 100, PeriodDays: 30,
    })
    org := uuidish(t, pool, "Budget Co")

    if err := r.CheckBudget(ctx, org, 50); err != nil {
        t.Fatalf("expected ok, got %v", err)
    }
    _ = r.Record(ctx, domain.TokenLedgerEntry{
        OrgID: org, Agent: "architect", Model: "x", Provider: "openai",
        TokensIn: 80, TokensOut: 1, Status: "succeeded",
    })
    if err := r.CheckBudget(ctx, org, 50); err != domain.ErrBudgetExceeded {
        t.Fatalf("expected budget exceeded, got %v", err)
    }
}
```

`uuidish` is a small helper that inserts an `organizations` row and returns the id; the implementation mirrors the existing test helpers in `services/control-plane/tests/`.

### Task 2.2: Cron job

**Files:**
- Modify: `services/control-plane/cmd/server/main.go` (COORDINATOR)

- [ ] **Step 1: Register the rotation job**

In `main.go` where the existing cron jobs register (billing usage + invoice cron), append:

```go
cronRunner.Register("token_budgets_rotate", 24*time.Hour, func(ctx context.Context) error {
    return tokenLedgerRepo.RotatePeriods(ctx, time.Now().UTC())
})
```

The exact registration call must match the existing surface (likely `cronRunner.AddJob` or similar — match Phase 3.5's `usage_tick` registration shape).

### Task 2.3: Commit Stage 2

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make vet && make arch && make test
git add internal/adapter/repo/token_ledger_repo.go internal/adapter/repo/token_ledger_repo_test.go cmd/server/main.go
git commit -m "feat(repo): token ledger + budget enforcement + daily rotation (stage 2)"
```

---

## Stage 3 — RetrievalStore + chunker + seed CLI + provider Embed()

### Task 3.1: pgvector helper

**Files:**
- Create: `services/control-plane/internal/platform/pgvector/pgvector.go`

- [ ] **Step 1: Write the file**

```go
// Package pgvector is a thin wrapper over github.com/pgvector/pgvector-go that
// keeps the adapter layer free of direct vector-encoding noise. We use the
// Vector type for pgx parameter binding and column scanning; everything else
// stays pgx.
package pgvector

import (
    pgv "github.com/pgvector/pgvector-go"
)

// FromSlice converts a []float32 into a pgvector.Vector for INSERT/UPDATE.
func FromSlice(v []float32) pgv.Vector { return pgv.NewVector(v) }

// ToSlice converts a pgvector.Vector back to []float32 after SELECT.
func ToSlice(v pgv.Vector) []float32 { return v.Slice() }
```

- [ ] **Step 2: Add the dependency**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane
go get github.com/pgvector/pgvector-go@v0.2.2
go mod tidy
```

### Task 3.2: Chunker

**Files:**
- Create: `services/control-plane/internal/adapter/retrieval/chunker.go`
- Create: `services/control-plane/internal/adapter/retrieval/chunker_test.go`

- [ ] **Step 1: Write the chunker**

```go
package retrieval

import (
    "bytes"
    "io/fs"
    "os"
    "path/filepath"
    "strings"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

const (
    chunkLines   = 50
    chunkOverlap = 5
    maxFileBytes = 256 * 1024
)

var skipDirs = map[string]bool{
    ".git": true, "__pycache__": true, "node_modules": true,
    ".venv": true, "dist": true, "build": true, "target": true,
}

var lockfiles = map[string]bool{
    "pnpm-lock.yaml": true, "package-lock.json": true,
    "poetry.lock": true, "Cargo.lock": true, "go.sum": true,
}

// Walk yields one Chunk per non-overlapping window of `chunkLines` with
// `chunkOverlap` lines repeated between consecutive chunks. Binary files,
// lockfiles, hidden + skip-listed dirs, and files larger than maxFileBytes
// are excluded.
func Walk(root string, orgID, repoSHA string) ([]domain.Chunk, error) {
    var out []domain.Chunk
    err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
        if err != nil { return err }
        if d.IsDir() {
            if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
                return fs.SkipDir
            }
            return nil
        }
        if lockfiles[d.Name()] { return nil }

        info, err := d.Info()
        if err != nil { return err }
        if info.Size() > maxFileBytes { return nil }

        body, err := os.ReadFile(path)
        if err != nil { return err }
        if bytes.IndexByte(body[:min(len(body), 512)], 0) >= 0 { return nil } // binary sniff

        relPath, _ := filepath.Rel(root, path)
        relPath = filepath.ToSlash(relPath)
        lines := strings.Split(string(body), "\n")

        for start := 0; start < len(lines); start += chunkLines - chunkOverlap {
            end := start + chunkLines
            if end > len(lines) { end = len(lines) }
            content := strings.Join(lines[start:end], "\n")
            if strings.TrimSpace(content) == "" {
                if end == len(lines) { break }
                continue
            }
            out = append(out, domain.Chunk{
                OrgID:      orgID,
                RepoSHA:    repoSHA,
                FilePath:   relPath,
                ChunkStart: start + 1,
                ChunkEnd:   end,
                Content:    content,
            })
            if end == len(lines) { break }
        }
        return nil
    })
    return out, err
}

func min(a, b int) int { if a < b { return a } ; return b }
```

- [ ] **Step 2: Test against a tiny fixture**

```go
package retrieval

import (
    "os"
    "path/filepath"
    "testing"
)

func TestWalk_SmallFile(t *testing.T) {
    dir := t.TempDir()
    body := ""
    for i := 0; i < 12; i++ { body += "line " + string(rune('A'+i)) + "\n" }
    _ = os.WriteFile(filepath.Join(dir, "small.py"), []byte(body), 0o644)
    _ = os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
    _ = os.WriteFile(filepath.Join(dir, ".git", "skip.py"), []byte("x"), 0o644)

    chunks, err := Walk(dir, "org", "sha")
    if err != nil { t.Fatal(err) }
    if len(chunks) != 1 { t.Fatalf("want 1 chunk, got %d", len(chunks)) }
    if chunks[0].ChunkStart != 1 || chunks[0].ChunkEnd < 12 {
        t.Fatalf("unexpected bounds: %+v", chunks[0])
    }
}
```

### Task 3.3: pgvector store

**Files:**
- Create: `services/control-plane/internal/adapter/retrieval/pgvector.go`
- Create: `services/control-plane/internal/adapter/retrieval/pgvector_test.go`

- [ ] **Step 1: Write the store**

```go
package retrieval

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    pgv "github.com/nexis-eco/nexis/services/control-plane/internal/platform/pgvector"
)

// Store implements domain.RetrievalStore on top of pgvector. The seed
// script uses the admin pool (no RLS principal); request-bound TopK uses
// the same admin pool (the LLM activity is system-owned and passes orgID
// explicitly), but org_id is always filtered in the WHERE clause.
type Store struct {
    pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Insert(ctx context.Context, batch []domain.Chunk) error {
    if len(batch) == 0 { return nil }
    tx, err := s.pool.Begin(ctx)
    if err != nil { return err }
    defer tx.Rollback(ctx) //nolint:errcheck
    for _, c := range batch {
        _, err := tx.Exec(ctx, `
            INSERT INTO code_embeddings
              (org_id, repo_sha, file_path, chunk_start, chunk_end, content, embedding)
            VALUES ($1,$2,$3,$4,$5,$6,$7)`,
            c.OrgID, c.RepoSHA, c.FilePath, c.ChunkStart, c.ChunkEnd, c.Content,
            pgv.FromSlice(c.Embedding))
        if err != nil { return fmt.Errorf("insert chunk %s L%d: %w", c.FilePath, c.ChunkStart, err) }
    }
    return tx.Commit(ctx)
}

func (s *Store) TopK(ctx context.Context, orgID, repoSHA string, query []float32, k int) ([]domain.Chunk, error) {
    rows, err := s.pool.Query(ctx, `
        SELECT id, file_path, chunk_start, chunk_end, content,
               1 - (embedding <=> $1::vector) AS similarity
        FROM code_embeddings
        WHERE org_id=$2 AND repo_sha=$3
        ORDER BY embedding <=> $1::vector
        LIMIT $4`,
        pgv.FromSlice(query), orgID, repoSHA, k)
    if err != nil { return nil, err }
    defer rows.Close()
    out := []domain.Chunk{}
    for rows.Next() {
        var id string
        var c domain.Chunk
        c.OrgID, c.RepoSHA = orgID, repoSHA
        if err := rows.Scan(&id, &c.FilePath, &c.ChunkStart, &c.ChunkEnd, &c.Content, &c.Similarity); err != nil {
            return nil, err
        }
        out = append(out, c)
    }
    return out, rows.Err()
}
```

### Task 3.4: Extend LLM adapters with Embed()

**Files:**
- Modify: `services/control-plane/internal/adapter/llm/openai.go`
- Modify: `services/control-plane/internal/adapter/llm/ollama.go`
- Modify: `services/control-plane/internal/adapter/llm/factory.go`

- [ ] **Step 1: OpenAI — append `Embed` + `EmbeddingDims`**

In `openai.go`, after `Complete()`:

```go
// embeddingDims is the dim count for text-embedding-3-small. If we ever
// support 3-large (3072 dims) it gets a config-driven branch.
const embeddingDims = 1536

func (p *OpenAIProvider) EmbeddingDims() int { return embeddingDims }

type openaiEmbedReq struct {
    Model string   `json:"model"`
    Input []string `json:"input"`
}
type openaiEmbedResp struct {
    Data []struct {
        Embedding []float32 `json:"embedding"`
    } `json:"data"`
    Usage struct {
        PromptTokens int `json:"prompt_tokens"`
    } `json:"usage"`
}

func (p *OpenAIProvider) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
    if model == "" { model = "text-embedding-3-small" }
    body, _ := json.Marshal(openaiEmbedReq{Model: model, Input: texts})
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/v1/embeddings", bytes.NewReader(body))
    if err != nil { return nil, fmt.Errorf("openai embed build: %w", err) }
    req.Header.Set("Content-Type", "application/json")
    if p.cfg.APIKey != "" { req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey) }
    resp, err := p.http.Do(req)
    if err != nil { return nil, fmt.Errorf("%w: openai embed http: %v", domain.ErrEmbeddingFailed, err) }
    defer resp.Body.Close()
    raw, _ := io.ReadAll(resp.Body)
    if resp.StatusCode >= 400 {
        return nil, fmt.Errorf("%w: openai embed status %d: %s", domain.ErrEmbeddingFailed, resp.StatusCode, string(raw))
    }
    var out openaiEmbedResp
    if err := json.Unmarshal(raw, &out); err != nil {
        return nil, fmt.Errorf("%w: openai embed decode: %v", domain.ErrEmbeddingFailed, err)
    }
    vecs := make([][]float32, len(out.Data))
    for i := range out.Data { vecs[i] = out.Data[i].Embedding }
    return vecs, nil
}
```

- [ ] **Step 2: Ollama — append `Embed` + `EmbeddingDims`**

In `ollama.go`:

```go
// nomic-embed-text dims; if a different model is wired the factory probes
// /api/show and overrides via SetEmbeddingDims.
func (p *OllamaProvider) EmbeddingDims() int {
    if p.embedDims > 0 { return p.embedDims }
    return 768
}

func (p *OllamaProvider) SetEmbeddingDims(d int) { p.embedDims = d }

type ollamaEmbedReq struct {
    Model string `json:"model"`
    Input string `json:"input"` // ollama only accepts one input at a time on /api/embeddings — we batch in Go
}
type ollamaEmbedResp struct {
    Embedding []float32 `json:"embedding"`
}

func (p *OllamaProvider) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
    if model == "" { model = "nomic-embed-text" }
    out := make([][]float32, len(texts))
    for i, t := range texts {
        body, _ := json.Marshal(ollamaEmbedReq{Model: model, Input: t})
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/api/embeddings", bytes.NewReader(body))
        if err != nil { return nil, err }
        req.Header.Set("Content-Type", "application/json")
        resp, err := p.http.Do(req)
        if err != nil { return nil, fmt.Errorf("%w: ollama embed: %v", domain.ErrEmbeddingFailed, err) }
        raw, _ := io.ReadAll(resp.Body)
        _ = resp.Body.Close()
        if resp.StatusCode >= 400 {
            return nil, fmt.Errorf("%w: ollama embed status %d: %s", domain.ErrEmbeddingFailed, resp.StatusCode, string(raw))
        }
        var parsed ollamaEmbedResp
        if err := json.Unmarshal(raw, &parsed); err != nil {
            return nil, fmt.Errorf("%w: ollama embed decode: %v", domain.ErrEmbeddingFailed, err)
        }
        out[i] = parsed.Embedding
    }
    return out, nil
}
```

Add `embedDims int` to `OllamaProvider` struct.

- [ ] **Step 3: Factory composition**

Replace `factory.go` with:

```go
package llm

import (
    "context"
    "fmt"
    "log/slog"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// Providers bundles the LLM + Embedding port so callers (agents, seed CLI,
// eval CLI) get both from one factory call.
type Providers struct {
    LLM       domain.LLMProvider
    Embedding domain.EmbeddingProvider
}

func NewFromConfig(cfg config.Config) (domain.LLMProvider, error) {
    p, err := NewProviders(cfg, slog.Default())
    if err != nil { return nil, err }
    return p.LLM, nil
}

// NewProviders is the Phase 5 entrypoint. When LLM_PROVIDER=ollama and the
// configured ollama embed model is missing, falls back to OpenAI for
// embeddings only — the LLM path stays on ollama. Logs the fallback.
func NewProviders(cfg config.Config, logger *slog.Logger) (Providers, error) {
    switch cfg.LLMProvider {
    case "openai":
        op := NewOpenAIProvider(OpenAIConfig{
            APIKey:  cfg.OpenAIAPIKey,
            Default: cfg.OpenAIModelCheap,
        })
        return Providers{LLM: op, Embedding: op}, nil
    case "ollama":
        ol := NewOllamaProvider(OllamaConfig{
            BaseURL: cfg.OllamaBaseURL,
            Default: cfg.OllamaModelGen,
        })
        // Probe ollama for the embed model. If absent → fall back to OpenAI
        // embeddings (the agents layer doesn't care which provider owns
        // embeddings, only that EmbeddingDims() is consistent).
        if probeOllamaModel(context.Background(), cfg.OllamaBaseURL, cfg.OllamaEmbedModel) {
            return Providers{LLM: ol, Embedding: ol}, nil
        }
        logger.Warn("ollama embed model missing — falling back to OpenAI for embeddings",
            "missing_model", cfg.OllamaEmbedModel, "fallback_model", cfg.OpenAIEmbedModel)
        op := NewOpenAIProvider(OpenAIConfig{APIKey: cfg.OpenAIAPIKey})
        return Providers{LLM: ol, Embedding: op}, nil
    case "openai_eval", "ollama_eval":
        // Phase 5 eval-override path: instantiates a provider regardless of
        // global LLM_PROVIDER. Used only by usecase/eval_runner.go.
        return NewProviders(forceProvider(cfg, cfg.LLMProvider[:len(cfg.LLMProvider)-len("_eval")]), logger)
    default:
        return Providers{}, fmt.Errorf("unknown LLM_PROVIDER %q (want openai|ollama)", cfg.LLMProvider)
    }
}

func forceProvider(cfg config.Config, p string) config.Config {
    cfg.LLMProvider = p
    return cfg
}

// NewProvidersFor is the canonical eval-override entry. p in {"openai","ollama"}.
func NewProvidersFor(cfg config.Config, p string, logger *slog.Logger) (Providers, error) {
    return NewProviders(forceProvider(cfg, p), logger)
}
```

`probeOllamaModel` is a 5-second `POST /api/show {"model": "..."}` returning true on 200, false on 404.

### Task 3.5: Seed CLI

**Files:**
- Create: `services/control-plane/cmd/seed-pgvector/main.go`

- [ ] **Step 1: Write the CLI**

```go
// Package main seeds code_embeddings from services/validator/fixtures/. Run
// via: docker compose run --rm control-plane /app/seed-pgvector --org-id=<id>
// Re-running is safe: the script does INSERT … (no ON CONFLICT) so duplicate
// org/repo_sha pairs will multiply rows. Wipe with:
//   DELETE FROM code_embeddings WHERE org_id=$1 AND repo_sha=$2
package main

import (
    "context"
    "flag"
    "fmt"
    "log/slog"
    "os"
    "os/exec"
    "strings"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/llm"
    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/retrieval"
    "github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

const fixtureRoot = "services/validator/fixtures"

func main() {
    var orgID, fixtureOverride string
    flag.StringVar(&orgID, "org-id", os.Getenv("SEED_ORG_ID"), "organization id to seed under")
    flag.StringVar(&fixtureOverride, "fixture-dir", fixtureRoot, "directory to walk")
    flag.Parse()
    if orgID == "" {
        fmt.Fprintln(os.Stderr, "--org-id required (or SEED_ORG_ID env)")
        os.Exit(2)
    }

    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    cfg := config.Load()

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    pool, err := pgxpool.New(ctx, cfg.DatabaseURL) // admin URL — bypasses RLS for seed
    if err != nil { logger.Error("pool", "err", err); os.Exit(1) }
    defer pool.Close()

    providers, err := llm.NewProviders(cfg, logger)
    if err != nil { logger.Error("providers", "err", err); os.Exit(1) }

    sha := repoSHA()
    repoSHA := "fixture-" + sha
    chunks, err := retrieval.Walk(fixtureOverride, orgID, repoSHA)
    if err != nil { logger.Error("walk", "err", err); os.Exit(1) }
    logger.Info("walk complete", "chunks", len(chunks), "repo_sha", repoSHA)

    // Batch embed 64 at a time
    store := retrieval.New(pool)
    for i := 0; i < len(chunks); i += 64 {
        end := i + 64
        if end > len(chunks) { end = len(chunks) }
        batch := chunks[i:end]
        texts := make([]string, len(batch))
        for j, c := range batch { texts[j] = c.Content }
        vecs, err := providers.Embedding.Embed(ctx, cfg.OpenAIEmbedModel, texts)
        if err != nil { logger.Error("embed", "err", err, "batch_start", i); os.Exit(1) }
        for j := range batch { batch[j].Embedding = vecs[j] }
        if err := store.Insert(ctx, batch); err != nil {
            logger.Error("insert", "err", err, "batch_start", i); os.Exit(1)
        }
    }

    // Build the ivfflat index (idempotent CREATE INDEX IF NOT EXISTS)
    _, err = pool.Exec(ctx, `
        CREATE INDEX IF NOT EXISTS code_embeddings_embedding_ivfflat
        ON code_embeddings USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)`)
    if err != nil { logger.Error("index", "err", err); os.Exit(1) }

    fmt.Printf("seeded %d chunks for org=%s repo_sha=%s\n", len(chunks), orgID, repoSHA)
}

func repoSHA() string {
    out, err := exec.Command("git", "rev-parse", "HEAD").Output()
    if err != nil { return "unknown" }
    return strings.TrimSpace(string(out))
}
```

### Task 3.6: Verify Stage 3

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
docker compose up -d --build control-plane
# Boot a workspace (Phase 3.5 path) to get an org id, then:
ORG=$(docker compose exec -T postgres psql -U nexis -d nexis -tA -c "SELECT id FROM organizations LIMIT 1;")
docker compose run --rm control-plane /app/seed-pgvector --org-id=$ORG
# Expect: "seeded N chunks for org=$ORG repo_sha=fixture-..."
docker compose exec -T postgres psql -U nexis -d nexis -c \
  "SELECT count(*), repo_sha FROM code_embeddings WHERE org_id='$ORG' GROUP BY repo_sha;"
# Expect at least 1 row, count >= 10
docker compose exec -T postgres psql -U nexis -d nexis -c "\d code_embeddings" | grep ivfflat
# Expect: code_embeddings_embedding_ivfflat present
```

### Task 3.7: Commit Stage 3

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add internal/adapter/retrieval internal/platform/pgvector internal/adapter/llm cmd/seed-pgvector go.mod go.sum
git commit -m "feat(retrieval): pgvector store + chunker + embed extension + seed CLI (stage 3)"
```

---

## Stage 4 — LLM adapter extensions: prompt caching + dual-model + cost

### Task 4.1: OpenAI prompt caching + cost calc

**Files:**
- Modify: `services/control-plane/internal/adapter/llm/openai.go`
- Modify: `services/control-plane/internal/adapter/llm/openai_test.go`

- [ ] **Step 1: Switch `Complete()` to support `CacheSystem`**

Replace the message-build block:

```go
messages := []map[string]string{}
if req.System != "" {
    messages = append(messages, map[string]string{"role": "system", "content": req.System})
}
messages = append(messages, map[string]string{"role": "user", "content": req.Prompt})
```

with:

```go
type contentBlock struct {
    Type         string `json:"type"`
    Text         string `json:"text"`
    CacheControl *struct {
        Type string `json:"type"`
    } `json:"cache_control,omitempty"`
}
messagesGeneric := []map[string]any{}
if req.System != "" {
    if req.CacheSystem {
        block := contentBlock{Type: "text", Text: req.System}
        block.CacheControl = &struct{ Type string `json:"type"` }{Type: "ephemeral"}
        messagesGeneric = append(messagesGeneric, map[string]any{
            "role":    "system",
            "content": []any{block},
        })
    } else {
        messagesGeneric = append(messagesGeneric, map[string]any{
            "role": "system", "content": req.System,
        })
    }
}
messagesGeneric = append(messagesGeneric, map[string]any{
    "role": "user", "content": req.Prompt,
})
```

Replace `body["messages"] = messages` with `body["messages"] = messagesGeneric`.

- [ ] **Step 2: Parse cached_tokens + populate cost**

Extend the response decoder:

```go
var parsed struct {
    Model   string `json:"model"`
    Choices []struct {
        Message struct {
            Content string `json:"content"`
        } `json:"message"`
    } `json:"choices"`
    Usage struct {
        PromptTokens          int `json:"prompt_tokens"`
        CompletionTokens      int `json:"completion_tokens"`
        PromptTokensDetails   struct {
            CachedTokens int `json:"cached_tokens"`
        } `json:"prompt_tokens_details"`
    } `json:"usage"`
}
```

After decode, compute cost:

```go
cached := parsed.Usage.PromptTokensDetails.CachedTokens
nonCachedIn := parsed.Usage.PromptTokens - cached
cost := costCentsForOpenAI(model, nonCachedIn, parsed.Usage.CompletionTokens, cached)
duration := time.Since(start).Milliseconds()

return CompletionResponse{
    Content:      parsed.Choices[0].Message.Content,
    Model:        parsed.Model,
    InputTokens:  parsed.Usage.PromptTokens,
    OutputTokens: parsed.Usage.CompletionTokens,
    CachedTokens: cached,
    CostCents:    cost,
    DurationMs:   duration,
}, nil
```

Append `start := time.Now()` at the top of `Complete()` so `duration` works.

- [ ] **Step 3: Cost table**

Append the cost helper to `openai.go`:

```go
// USD-cents-per-1M-tokens rate table for OpenAI models we use. Updated
// 2026-05 from openai.com/api/pricing. New models gain entries here.
var openaiPricing = map[string]struct {
    InputCentsPer1M       float64
    OutputCentsPer1M      float64
    CachedInputCentsPer1M float64
}{
    "gpt-4o":      {250.0,  1000.0, 125.0},
    "gpt-4o-mini": { 15.0,    60.0,   7.5},

    // Embedding pricing is also surfaced for Embed() callers via embedCostCents.
    "text-embedding-3-small": {2.0, 0.0, 2.0},
}

func costCentsForOpenAI(model string, in, out, cached int) float64 {
    p, ok := openaiPricing[model]
    if !ok { return 0 }
    return float64(in)*p.InputCentsPer1M/1e6 +
           float64(out)*p.OutputCentsPer1M/1e6 +
           float64(cached)*p.CachedInputCentsPer1M/1e6
}

// embedCostCents surfaces embedding cost; called by usage instrumentation.
func embedCostCents(model string, in int) float64 {
    p, ok := openaiPricing[model]
    if !ok { return 0 }
    return float64(in)*p.InputCentsPer1M/1e6
}
```

- [ ] **Step 4: Test — assert cached_tokens propagates**

```go
package llm

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestOpenAI_CachedTokens(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{
            "model":"gpt-4o-mini",
            "choices":[{"message":{"content":"ok"}}],
            "usage":{"prompt_tokens":2500,"completion_tokens":400,"prompt_tokens_details":{"cached_tokens":2000}}
        }`))
    }))
    defer srv.Close()
    p := NewOpenAIProvider(OpenAIConfig{BaseURL: srv.URL, APIKey: "x", Default: "gpt-4o-mini"})
    res, err := p.Complete(context.Background(), CompletionRequest{System: "s", Prompt: "u", CacheSystem: true})
    if err != nil { t.Fatal(err) }
    if res.CachedTokens != 2000 { t.Errorf("cached=%d want 2000", res.CachedTokens) }
    if res.InputTokens != 2500 || res.OutputTokens != 400 { t.Errorf("token counts wrong: %+v", res) }
    if res.CostCents <= 0 { t.Errorf("cost not populated: %+v", res) }
}
```

### Task 4.2: Ollama — accept CacheSystem as no-op + populate duration

**Files:**
- Modify: `services/control-plane/internal/adapter/llm/ollama.go`
- Modify: `services/control-plane/internal/adapter/llm/ollama_test.go`

- [ ] **Step 1: Add `start := time.Now()` + `DurationMs` populate; cost stays 0**

At the top of `Complete()`:

```go
start := time.Now()
```

Just before `return CompletionResponse{...}`, add `DurationMs: time.Since(start).Milliseconds(), CostCents: 0`.

`CacheSystem` is intentionally ignored — Ollama has no equivalent.

- [ ] **Step 2: Test**

```go
func TestOllama_CacheSystemIsNoop(t *testing.T) {
    // Ollama call with CacheSystem=true must succeed identically to false.
    // Use a fake server that asserts the request body has no cache_control.
    ...
}
```

### Task 4.3: Commit Stage 4

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add internal/adapter/llm
git commit -m "feat(llm): openai prompt caching + dual-model cost calc + ollama parity (stage 4)"
```

---

## Stage 5 — 5 L1 agent implementations (Pattern C — 5 parallel shards)

> **Pattern C dispatch:** the controller fires five `backend-engineer` agents in a single message. Each shard receives only its sub-tree assignment + the shared `internal/adapter/agents/{registry,llm,retrieval}.go` interfaces from Task 5.0 (which the coordinator lands first). No shard touches another's directory. The coordinator merges by running `make build && make test && make vet && make arch` once after all five complete.

### Task 5.0: Shared agents infrastructure (coordinator, BEFORE shards)

**Files:**
- Create: `services/control-plane/internal/adapter/agents/registry.go`
- Create: `services/control-plane/internal/adapter/agents/llm.go`
- Create: `services/control-plane/internal/adapter/agents/retrieval.go`
- Create: `services/control-plane/internal/adapter/agents/schema.go`

- [ ] **Step 1: registry.go**

```go
// Package agents hosts one Provider subpackage per L1/L2 agent + a Registry
// that dispatches by name. Phase 5 ships the 5 L1 providers (architect,
// backend, qa, devops, data_engineer); Phase 6 fills in the L2 set.
package agents

import (
    "context"
    "fmt"
    "sync"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Registry maps an AgentName to its concrete domain.Agent implementation.
// Lazy-init friendly: NewRegistry takes a map keyed by name.
type Registry struct {
    mu      sync.RWMutex
    byName  map[domain.AgentName]domain.Agent
}

func NewRegistry(agents map[domain.AgentName]domain.Agent) *Registry {
    cp := make(map[domain.AgentName]domain.Agent, len(agents))
    for k, v := range agents { cp[k] = v }
    return &Registry{byName: cp}
}

func (r *Registry) Get(n domain.AgentName) (domain.Agent, error) {
    r.mu.RLock()
    a, ok := r.byName[n]
    r.mu.RUnlock()
    if !ok { return nil, fmt.Errorf("unknown agent %q", n) }
    return a, nil
}

// Run is a convenience wrapper used by activities.go.
func (r *Registry) Run(ctx context.Context, n domain.AgentName, in domain.AgentInput) (domain.AgentOutput, error) {
    a, err := r.Get(n)
    if err != nil { return domain.AgentOutput{}, err }
    return a.Run(ctx, in)
}
```

- [ ] **Step 2: llm.go — the Invoke helper every agent calls**

```go
package agents

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "log/slog"
    "time"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// LLMClient wraps a domain.LLMProvider + TokenLedger + AuditWriter so every
// agent gets uniform budget enforcement, ledger persistence, and audit row
// emission for free. Each agent constructs one of these via NewLLMClient and
// calls Invoke() once per LLM round.
type LLMClient struct {
    Provider    domain.LLMProvider
    Embedding   domain.EmbeddingProvider
    Ledger      domain.TokenLedger
    Audit       domain.AuditWriter
    Logger      *slog.Logger
    SchemaRetryMax int
}

type InvokeRequest struct {
    Agent          domain.AgentName
    Model          string
    System         string
    User           string
    MaxTokens      int
    JSONResponse   bool
    CacheSystem    bool
    WorkflowRunID  string
    OrgID          string
    Validate       func(content string) (map[string]any, error) // returns ErrAgentSchemaMismatch on bad shape
    EstimatedTokensIn int // for the pre-flight budget check
}

type InvokeResult struct {
    Content       string
    Structured    map[string]any
    TokensIn      int
    TokensOut     int
    CachedTokens  int
    CostCents     float64
    DurationMs    int64
    Model         string
    Provider      string
    SchemaRetries int
}

func (c *LLMClient) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
    if err := c.Ledger.CheckBudget(ctx, req.OrgID, req.EstimatedTokensIn); err != nil {
        _ = c.Ledger.Record(ctx, domain.TokenLedgerEntry{
            OrgID: req.OrgID, WorkflowRunID: req.WorkflowRunID,
            Agent: req.Agent, Model: req.Model, Provider: c.Provider.Name(),
            Status: "budget_exceeded",
        })
        return InvokeResult{}, fmt.Errorf("%w: agent=%s", domain.ErrBudgetExceeded, req.Agent)
    }

    var lastErr error
    var lastResp domain.CompletionResponse
    var structured map[string]any
    for attempt := 0; attempt <= c.SchemaRetryMax; attempt++ {
        start := time.Now()
        resp, err := c.Provider.Complete(ctx, domain.CompletionRequest{
            Model:        req.Model,
            System:       req.System,
            Prompt:       req.User,
            MaxTokens:    req.MaxTokens,
            JSONResponse: req.JSONResponse,
            CacheSystem:  req.CacheSystem,
        })
        duration := time.Since(start).Milliseconds()
        if err != nil {
            _ = c.Ledger.Record(ctx, domain.TokenLedgerEntry{
                OrgID: req.OrgID, WorkflowRunID: req.WorkflowRunID,
                Agent: req.Agent, Model: req.Model, Provider: c.Provider.Name(),
                DurationMs: int(duration), Status: "provider_error",
            })
            return InvokeResult{}, fmt.Errorf("provider call failed: %w", err)
        }
        lastResp = resp

        if req.Validate != nil {
            s, vErr := req.Validate(resp.Content)
            if vErr == nil {
                structured = s
                _ = c.Ledger.Record(ctx, domain.TokenLedgerEntry{
                    OrgID: req.OrgID, WorkflowRunID: req.WorkflowRunID,
                    Agent: req.Agent, Model: resp.Model, Provider: c.Provider.Name(),
                    TokensIn: resp.InputTokens, TokensOut: resp.OutputTokens,
                    CachedTokens: resp.CachedTokens, CostCents: resp.CostCents,
                    DurationMs: int(resp.DurationMs), Status: "succeeded",
                })
                c.auditInvoked(ctx, req, resp, "succeeded", attempt)
                return InvokeResult{
                    Content: resp.Content, Structured: s,
                    TokensIn: resp.InputTokens, TokensOut: resp.OutputTokens,
                    CachedTokens: resp.CachedTokens, CostCents: resp.CostCents,
                    DurationMs: resp.DurationMs, Model: resp.Model, Provider: c.Provider.Name(),
                    SchemaRetries: attempt,
                }, nil
            }
            lastErr = vErr
            _ = c.Ledger.Record(ctx, domain.TokenLedgerEntry{
                OrgID: req.OrgID, WorkflowRunID: req.WorkflowRunID,
                Agent: req.Agent, Model: resp.Model, Provider: c.Provider.Name(),
                TokensIn: resp.InputTokens, TokensOut: resp.OutputTokens,
                CachedTokens: resp.CachedTokens, CostCents: resp.CostCents,
                DurationMs: int(resp.DurationMs), Status: "schema_mismatch",
            })
            c.auditInvoked(ctx, req, resp, "schema_mismatch", attempt)
            c.Logger.Warn("agent schema mismatch", "agent", req.Agent, "attempt", attempt+1, "err", vErr)
            continue
        }
        // No validator → return raw content unchanged.
        return InvokeResult{
            Content: resp.Content, TokensIn: resp.InputTokens, TokensOut: resp.OutputTokens,
            CachedTokens: resp.CachedTokens, CostCents: resp.CostCents,
            DurationMs: resp.DurationMs, Model: resp.Model, Provider: c.Provider.Name(),
        }, nil
    }
    _ = lastResp
    _ = structured
    return InvokeResult{}, fmt.Errorf("%w: agent=%s attempts=%d last=%v",
        domain.ErrAgentSchemaMismatch, req.Agent, c.SchemaRetryMax+1, lastErr)
}

func (c *LLMClient) auditInvoked(ctx context.Context, req InvokeRequest, resp domain.CompletionResponse, status string, retries int) {
    if c.Audit == nil { return }
    _ = c.Audit.Write(ctx, domain.Principal{OrgID: req.OrgID}, "agent.invoked", req.WorkflowRunID, map[string]any{
        "agent":           string(req.Agent),
        "model":           resp.Model,
        "provider":        c.Provider.Name(),
        "tokens_in":       resp.InputTokens,
        "tokens_out":      resp.OutputTokens,
        "cached_tokens":   resp.CachedTokens,
        "cost_cents":      resp.CostCents,
        "duration_ms":     resp.DurationMs,
        "status":          status,
        "schema_retries":  retries,
    })
}

// DecodeJSONOrError is the default validator each agent plugs in. Returns
// ErrAgentSchemaMismatch when the body is not parseable JSON; per-agent
// schema.go provides a stricter validator that calls this first.
func DecodeJSONOrError(content string) (map[string]any, error) {
    var m map[string]any
    if err := json.Unmarshal([]byte(content), &m); err != nil {
        return nil, errors.Join(domain.ErrAgentSchemaMismatch, err)
    }
    return m, nil
}
```

- [ ] **Step 3: retrieval.go — top-K helper that emits a markdown block**

```go
package agents

import (
    "context"
    "fmt"
    "strings"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type RetrievalClient struct {
    Store     domain.RetrievalStore
    Embed     domain.EmbeddingProvider
    EmbedModel string
    K         int
}

func (r *RetrievalClient) ContextFor(ctx context.Context, orgID, repoSHA, query string) (string, []domain.Chunk, error) {
    if r == nil || r.Store == nil { return "", nil, nil }
    vecs, err := r.Embed.Embed(ctx, r.EmbedModel, []string{query})
    if err != nil { return "", nil, err }
    chunks, err := r.Store.TopK(ctx, orgID, repoSHA, vecs[0], r.K)
    if err != nil { return "", nil, err }
    if len(chunks) == 0 { return "", nil, nil }

    var b strings.Builder
    b.WriteString("## Relevant code (top ")
    b.WriteString(fmt.Sprint(len(chunks)))
    b.WriteString(" chunks by cosine similarity)\n\n")
    for _, c := range chunks {
        fmt.Fprintf(&b, "### `%s` L%d–L%d (sim=%.2f)\n```\n%s\n```\n\n",
            c.FilePath, c.ChunkStart, c.ChunkEnd, c.Similarity, c.Content)
    }
    return b.String(), chunks, nil
}
```

- [ ] **Step 4: schema.go — minimal JSON-schema validator**

```go
package agents

import (
    "errors"
    "fmt"

    "github.com/xeipuuv/gojsonschema"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Validate is the helper each agent's schema.go calls. SchemaJSON is the
// schema document as a Go string; payload is the LLM's JSON response body.
// Wraps ErrAgentSchemaMismatch on failure so the LLMClient retry loop
// recognises it.
func Validate(schemaJSON, payloadJSON string) (map[string]any, error) {
    schemaLoader := gojsonschema.NewStringLoader(schemaJSON)
    docLoader := gojsonschema.NewStringLoader(payloadJSON)
    result, err := gojsonschema.Validate(schemaLoader, docLoader)
    if err != nil { return nil, errors.Join(domain.ErrAgentSchemaMismatch, err) }
    if !result.Valid() {
        var first string
        if errs := result.Errors(); len(errs) > 0 { first = errs[0].String() }
        return nil, fmt.Errorf("%w: %s", domain.ErrAgentSchemaMismatch, first)
    }
    return DecodeJSONOrError(payloadJSON)
}
```

Pull the dep:

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane
go get github.com/xeipuuv/gojsonschema@latest
go mod tidy
```

- [ ] **Step 5: Commit the shared infra so shards can branch from it**

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make vet && make arch
git add internal/adapter/agents/{registry,llm,retrieval,schema}.go go.mod go.sum
git commit -m "feat(agents): shared registry + llm wrapper + retrieval helper (stage 5 shared)"
```

### Task 5.A: Architect agent shard

**Dispatch input to backend-engineer:** "Implement Architect L1 agent under `internal/adapter/agents/architect/`. Read the shared infrastructure at `internal/adapter/agents/{registry,llm,retrieval,schema}.go` — do not modify it. Use `domain.AgentNameArchitect`."

**Files:**
- Create: `services/control-plane/internal/adapter/agents/architect/provider.go`
- Create: `services/control-plane/internal/adapter/agents/architect/prompts.go`
- Create: `services/control-plane/internal/adapter/agents/architect/schema.go`
- Create: `services/control-plane/internal/adapter/agents/architect/provider_test.go`

- [ ] **Step 1: prompts.go**

```go
package architect

const SystemPrompt = `You are NEXIS Architect, the planning agent in an autonomous SRE pipeline.

Your job: read the incident + the retrieved code context, then propose a small, minimal-blast-radius engineering plan to fix the root cause. Output strict JSON only — no prose outside the JSON. Conform exactly to the schema below.

{
  "plan_steps":     [ { "id": "p1", "description": "...", "files": ["..."], "rationale": "..." } ],
  "affected_files": [ "..." ],
  "risk_level":     "low" | "medium" | "high"
}

Rules:
- Never invent a file path that wasn't shown in the retrieved chunks.
- Keep plan_steps to 1–5 entries.
- risk_level=high requires a rationale that names a production-facing concern.
`

const UserTemplate = `# Incident
Title: {{.Title}}
Service: {{.Service}} ({{.Environment}})
Stacktrace:
{{.Stacktrace}}

# Logs
{{.Logs}}

{{.RetrievalContext}}

Produce the JSON plan now.
`
```

- [ ] **Step 2: schema.go**

```go
package architect

const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["plan_steps", "affected_files", "risk_level"],
  "properties": {
    "plan_steps": {
      "type": "array", "minItems": 1, "maxItems": 5,
      "items": {
        "type": "object",
        "required": ["id", "description", "files", "rationale"],
        "properties": {
          "id":          {"type": "string"},
          "description": {"type": "string"},
          "files":       {"type": "array", "items": {"type": "string"}},
          "rationale":   {"type": "string"}
        }
      }
    },
    "affected_files": {"type": "array", "items": {"type": "string"}},
    "risk_level":     {"type": "string", "enum": ["low", "medium", "high"]}
  }
}`
```

- [ ] **Step 3: provider.go**

```go
package architect

import (
    "bytes"
    "context"
    "text/template"
    "time"

    "github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Provider struct {
    LLM        *agents.LLMClient
    Retrieval  *agents.RetrievalClient
    Model      string // resolved from cfg.AgentModelArchitectOpenAI / *Ollama by the factory
}

func New(llm *agents.LLMClient, retrieval *agents.RetrievalClient, model string) *Provider {
    return &Provider{LLM: llm, Retrieval: retrieval, Model: model}
}

func (p *Provider) Name() domain.AgentName { return domain.AgentNameArchitect }

func (p *Provider) Run(ctx context.Context, in domain.AgentInput) (domain.AgentOutput, error) {
    start := time.Now()
    inc := in.Incident
    if inc == nil { inc = &domain.IncidentPayload{Title: "Unspecified incident"} }

    query := inc.Title + " " + inc.Stacktrace
    rctx, _, _ := p.Retrieval.ContextFor(ctx, in.OrgID, in.RepoSHA, query)

    var buf bytes.Buffer
    _ = template.Must(template.New("u").Parse(UserTemplate)).Execute(&buf, map[string]any{
        "Title": inc.Title, "Service": inc.Service, "Environment": inc.Environment,
        "Stacktrace": inc.Stacktrace, "Logs": inc.Logs, "RetrievalContext": rctx,
    })

    res, err := p.LLM.Invoke(ctx, agents.InvokeRequest{
        Agent:             domain.AgentNameArchitect,
        Model:             p.Model,
        System:            SystemPrompt,
        User:              buf.String(),
        MaxTokens:         1500,
        JSONResponse:      true,
        CacheSystem:       true,
        WorkflowRunID:     in.WorkflowRunID,
        OrgID:             in.OrgID,
        EstimatedTokensIn: 3000, // SystemPrompt + retrieval chunks + incident body
        Validate: func(content string) (map[string]any, error) {
            return agents.Validate(SchemaJSON, content)
        },
    })
    if err != nil { return domain.AgentOutput{}, err }

    return domain.AgentOutput{
        Success:       true,
        Content:       res.Content,
        Structured:    res.Structured,
        TokensIn:      res.TokensIn,
        TokensOut:     res.TokensOut,
        CachedTokens:  res.CachedTokens,
        CostCents:     res.CostCents,
        DurationMs:    time.Since(start).Milliseconds(),
        Model:         res.Model,
        Provider:      res.Provider,
        SchemaRetries: res.SchemaRetries,
        SystemPrompt:  SystemPrompt,
        UserPrompt:    buf.String(),
    }, nil
}
```

- [ ] **Step 4: provider_test.go** — table-driven test asserts: (1) golden prompt build with a fixture incident, (2) mocked LLM returning valid JSON returns Structured populated, (3) mocked LLM returning malformed JSON retries up to `SchemaRetryMax` then errors with `ErrAgentSchemaMismatch`.

### Task 5.B: Backend agent shard

**Dispatch input to backend-engineer:** "Implement Backend L1 agent under `internal/adapter/agents/backend/`. The agent emits a unified diff; the response is not JSON. Use `patch_extract.go` to yank the diff out of the model's prose (it usually fences in ` ```diff`)."

**Files:**
- Create: `services/control-plane/internal/adapter/agents/backend/provider.go`
- Create: `services/control-plane/internal/adapter/agents/backend/prompts.go`
- Create: `services/control-plane/internal/adapter/agents/backend/patch_extract.go`
- Create: `services/control-plane/internal/adapter/agents/backend/provider_test.go`

- [ ] **Step 1: prompts.go** — `SystemPrompt` instructs the model to output exactly one ` ```diff ... ``` ` block. `UserTemplate` includes the architect plan (`{{.PriorOutputs.architect}}` rendered as JSON) + the retrieved code + the incident.

- [ ] **Step 2: patch_extract.go**

```go
package backend

import (
    "errors"
    "regexp"
    "strings"

    "github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

var diffBlockRe = regexp.MustCompile("(?s)```diff\\s*\\n(.*?)```")

func ExtractDiff(content string) (string, []string, error) {
    m := diffBlockRe.FindStringSubmatch(content)
    var diff string
    switch {
    case m != nil:
        diff = strings.TrimSpace(m[1])
    case strings.Contains(content, "diff --git"):
        idx := strings.Index(content, "diff --git")
        diff = strings.TrimSpace(content[idx:])
    default:
        return "", nil, errors.Join(domain.ErrAgentSchemaMismatch,
            errors.New("no ```diff``` block or 'diff --git' header found"))
    }
    files := extractFiles(diff)
    if len(files) == 0 {
        return diff, nil, errors.Join(domain.ErrAgentSchemaMismatch,
            errors.New("diff contains no file headers"))
    }
    return diff, files, nil
}

var fileHeaderRe = regexp.MustCompile(`(?m)^\+\+\+ b/(.+)$`)

func extractFiles(diff string) []string {
    var out []string
    for _, m := range fileHeaderRe.FindAllStringSubmatch(diff, -1) {
        out = append(out, m[1])
    }
    return out
}
```

- [ ] **Step 3: provider.go** — uses `LLM.Invoke` with `JSONResponse: false`. The `Validate` callback wraps `ExtractDiff` and packages the result as `{"patch_diff": diff, "files_changed": files, "summary": <first 200 chars of content above the diff>}`.

- [ ] **Step 4: provider_test.go** — golden: fixture LLM output with ` ```diff ... ``` ` returns Structured with `patch_diff` and `files_changed`; malformed output returns `ErrAgentSchemaMismatch`.

### Task 5.C: QA agent shard

**Dispatch input to backend-engineer:** "Implement QA L1 agent under `internal/adapter/agents/qa/`. The agent outputs JSON `{tests: {filename: source}, covers_files: [...]}`. Use Validate against `SchemaJSON`."

**Files:**
- Create: `services/control-plane/internal/adapter/agents/qa/provider.go`
- Create: `services/control-plane/internal/adapter/agents/qa/prompts.go`
- Create: `services/control-plane/internal/adapter/agents/qa/schema.go`
- Create: `services/control-plane/internal/adapter/agents/qa/provider_test.go`

- [ ] **Step 1: prompts.go** — System prompt asks for one or more pytest files that target the patch produced by Backend. User template includes the backend patch + architect plan + retrieved code.

- [ ] **Step 2: schema.go**

```go
package qa

const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["tests", "covers_files"],
  "properties": {
    "tests": {
      "type": "object",
      "minProperties": 1,
      "additionalProperties": {"type": "string", "minLength": 10}
    },
    "covers_files": {"type": "array", "items": {"type": "string"}}
  }
}`
```

- [ ] **Step 3: provider.go** — Same shape as Architect but reads `in.PriorOutputs["backend"]` to extract `patch_diff` for the user prompt.

- [ ] **Step 4: provider_test.go**

### Task 5.D: DevOps agent shard

**Dispatch input to backend-engineer:** "Implement DevOps L1 agent under `internal/adapter/agents/devops/`. Output JSON `{argocd_app_yaml, gh_actions_yaml, rollout_strategy}`."

**Files:**
- Create: `services/control-plane/internal/adapter/agents/devops/provider.go`
- Create: `services/control-plane/internal/adapter/agents/devops/prompts.go`
- Create: `services/control-plane/internal/adapter/agents/devops/schema.go`
- Create: `services/control-plane/internal/adapter/agents/devops/provider_test.go`

- [ ] **Step 1: schema.go**

```go
package devops

const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["argocd_app_yaml", "gh_actions_yaml", "rollout_strategy"],
  "properties": {
    "argocd_app_yaml":  {"type": "string", "minLength": 10},
    "gh_actions_yaml":  {"type": "string", "minLength": 10},
    "rollout_strategy": {"type": "string", "enum": ["canary-10-50-100", "blue-green", "rolling", "recreate"]}
  }
}`
```

- [ ] **Step 2: prompts.go** — Mentions ArgoCD + GH Actions explicitly; warns the model to only produce YAML that is valid against the canonical schemas.

- [ ] **Step 3: provider.go** — Same shape as Architect; uses `in.Incident.Service` to pick the app name.

- [ ] **Step 4: provider_test.go**

### Task 5.E: DataEngineer agent shard

**Dispatch input to backend-engineer:** "Implement DataEngineer L1 agent under `internal/adapter/agents/data_engineer/`. Output JSON `{migrations: [{version, name, up_sql, down_sql}], data_backfill: null | {...}}`."

**Files:**
- Create: `services/control-plane/internal/adapter/agents/data_engineer/provider.go`
- Create: `services/control-plane/internal/adapter/agents/data_engineer/prompts.go`
- Create: `services/control-plane/internal/adapter/agents/data_engineer/schema.go`
- Create: `services/control-plane/internal/adapter/agents/data_engineer/provider_test.go`

- [ ] **Step 1: schema.go**

```go
package data_engineer

const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["migrations"],
  "properties": {
    "migrations": {
      "type": "array", "minItems": 0, "maxItems": 5,
      "items": {
        "type": "object",
        "required": ["version", "name", "up_sql", "down_sql"],
        "properties": {
          "version":  {"type": "string", "pattern": "^[0-9]{14}$"},
          "name":     {"type": "string"},
          "up_sql":   {"type": "string"},
          "down_sql": {"type": "string"}
        }
      }
    },
    "data_backfill": {
      "oneOf": [
        {"type": "null"},
        {"type": "object", "required": ["sql", "rationale"]}
      ]
    }
  }
}`
```

- [ ] **Step 2: prompts.go** — Instructs the model that "no migration needed" → return `{"migrations": [], "data_backfill": null}`.

- [ ] **Step 3: provider.go** — Same shape as Architect.

- [ ] **Step 4: provider_test.go**

### Task 5.M (merge): coordinator merge of 5 shards

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane
make build && make test && make vet && make arch
git add internal/adapter/agents/{architect,backend,qa,devops,data_engineer}
git commit -m "feat(agents): 5 L1 agents — Architect/Backend/QA/DevOps/DataEng (stage 5)"
```

---

## Stage 6 — Wire L1 agents into activities.go

### Task 6.1: Extend Activities

**Files:**
- Modify: `services/control-plane/internal/workflow/recovery/activities.go`
- Modify: `services/control-plane/internal/workflow/recovery/types.go`
- Modify: `services/control-plane/internal/workflow/recovery/workflow.go`

- [ ] **Step 1: types.go — extend `PipelineInput`**

```go
type PipelineInput struct {
    OrgID       string                  `json:"org_id"`
    WorkspaceID string                  `json:"workspace_id"`
    RunID       string                  `json:"run_id"`
    IncidentID  string                  `json:"incident_id"`
    TriggeredBy string                  `json:"triggered_by"`
    Incident    *domain.IncidentPayload `json:"incident,omitempty"` // NEW
    RepoSHA     string                  `json:"repo_sha,omitempty"` // NEW — retrieval scope
}
```

- [ ] **Step 2: activities.go — extend Activities struct + constructor**

```go
type Activities struct {
    Repo      *repo.WorkflowRepo
    Broker    *sse.Broker[domain.ActivityEvent]
    Patches   domain.PatchStore
    Validator ValidatorClient
    Agents    *agents.Registry  // NEW
    Ledger    domain.TokenLedger // NEW
    StubSleep time.Duration
}

func NewActivitiesFull(r *repo.WorkflowRepo, b *sse.Broker[domain.ActivityEvent],
    ps domain.PatchStore, v ValidatorClient, ag *agents.Registry, ledger domain.TokenLedger,
    stubSleep time.Duration) *Activities {
    return &Activities{
        Repo: r, Broker: b, Patches: ps, Validator: v,
        Agents: ag, Ledger: ledger, StubSleep: stubSleep,
    }
}
```

- [ ] **Step 3: activities.go — replace the 5 L1 method bodies**

Use this helper at the top of the file:

```go
func (a *Activities) runAgent(ctx context.Context, name domain.AgentName, in PipelineInput, prior map[string]any) (domain.ActivityResult, error) {
    if a.Agents == nil {
        // Test path / Phase 4 compatibility — fall back to stub.
        return a.stub(ctx, agentRoleOf(name), string(name)+".Run")
    }
    out, err := a.Agents.Run(ctx, domain.AgentInput{
        WorkflowRunID: in.RunID,
        OrgID:         in.OrgID,
        WorkspaceID:   in.WorkspaceID,
        PriorOutputs:  prior,
        PromptContext: "",
        Incident:      in.Incident,
        RepoSHA:       in.RepoSHA,
    })
    if err != nil {
        return domain.ActivityResult{}, err
    }
    payload := map[string]any{
        "tokens_in":  out.TokensIn,
        "tokens_out": out.TokensOut,
        "cost_cents": out.CostCents,
        "model":      out.Model,
        "provider":   out.Provider,
        "structured": out.Structured,
    }
    return domain.ActivityResult{
        AgentRole: agentRoleOf(name),
        Status:    domain.ActSucceeded,
        Message:   fmt.Sprintf("agent=%s tokens=%d/%d cost_cents=%.4f",
            name, out.TokensIn, out.TokensOut, out.CostCents),
        Payload: payload,
    }, nil
}

func agentRoleOf(n domain.AgentName) domain.AgentRole {
    switch n {
    case domain.AgentNameArchitect:    return domain.AgentArchitect
    case domain.AgentNameBackend:      return domain.AgentBackend
    case domain.AgentNameQA:           return domain.AgentQA
    case domain.AgentNameDevOps:       return domain.AgentDevOps
    case domain.AgentNameDataEngineer: return domain.AgentDataEngineer
    }
    return ""
}
```

Replace each method body with `return a.runAgent(ctx, domain.AgentName<X>, in, nil)` (the workflow function will be the one threading prior outputs through — see Step 5 below).

Special case: `BackendCodegen` retains the existing MinIO + validator round-trip. It calls `runAgent` first, then if `out.Structured["patch_diff"]` is non-empty, posts that diff through `a.Patches.Put` + `a.Validator.Validate`. Wrap any error from validate into the payload but don't fail the activity (we want the timeline to show that backend produced + validated).

- [ ] **Step 4: workflow.go — pipe prior outputs**

Replace the `runActivity` closure to accumulate a `prior := map[string]any{}` and serialise each `result.Payload["structured"]` into it before invoking the next step. The `ExecuteActivity` call passes the in-memory `prior` snapshot through `in.PriorOutputs`. Concretely:

```go
// before each L1 step, attach prior to the PipelineInput
in.PriorOutputs = prior  // requires adding PriorOutputs field to PipelineInput
```

Append to `types.go`:

```go
type PipelineInput struct {
    ...
    PriorOutputs map[string]any `json:"prior_outputs,omitempty"`
}
```

- [ ] **Step 5: workflow.go — bump retry**

In `llmActivityOpts`, set `MaximumAttempts: 2` (was 3 by inheritance from `stdActivityOpts`). LLM retries can multiply tokens; we cap.

```go
var llmActivityOpts = func() workflow.ActivityOptions {
    o := stdActivityOpts
    o.StartToCloseTimeout = 5 * time.Minute
    if o.RetryPolicy != nil {
        rp := *o.RetryPolicy
        rp.MaximumAttempts = 2
        o.RetryPolicy = &rp
    }
    return o
}()
```

### Task 6.2: Wire in `cmd/server/main.go`

**Files:**
- Modify: `services/control-plane/cmd/server/main.go` (COORDINATOR)

- [ ] **Step 1: Build the registry**

```go
providers, err := llm.NewProviders(cfg, logger)
if err != nil { logger.Error("llm providers", "err", err); os.Exit(1) }

tokenLedgerRepo := repo.NewTokenLedgerRepo(appPool, adminPool, repo.TokenLedgerConfig{
    AllowedTokensIn:  cfg.TokenBudgetTokensIn,
    AllowedTokensOut: cfg.TokenBudgetTokensOut,
    PeriodDays:       cfg.TokenBudgetPeriodDays,
})
retrievalStore := retrieval.New(adminPool)
retrievalClient := &agents.RetrievalClient{
    Store: retrievalStore, Embed: providers.Embedding,
    EmbedModel: cfg.OpenAIEmbedModel, K: 5,
}
llmClient := &agents.LLMClient{
    Provider: providers.LLM, Embedding: providers.Embedding,
    Ledger: tokenLedgerRepo, Audit: auditWriter,
    Logger: logger, SchemaRetryMax: cfg.AgentSchemaRetryMax,
}

archModel, beModel, qaModel, devopsModel, dataEngModel := modelsFor(cfg)
registry := agents.NewRegistry(map[domain.AgentName]domain.Agent{
    domain.AgentNameArchitect:    architect.New(llmClient, retrievalClient, archModel),
    domain.AgentNameBackend:      backend.New(llmClient, retrievalClient, beModel),
    domain.AgentNameQA:           qa.New(llmClient, retrievalClient, qaModel),
    domain.AgentNameDevOps:       devops.New(llmClient, retrievalClient, devopsModel),
    domain.AgentNameDataEngineer: data_engineer.New(llmClient, retrievalClient, dataEngModel),
})

activities := recovery.NewActivitiesFull(
    workflowRepo, sseBroker, patchStore, validatorClient,
    registry, tokenLedgerRepo,
    time.Duration(cfg.WorkflowStubDurationMs)*time.Millisecond,
)
```

`modelsFor(cfg)` reads the per-agent env model based on `cfg.LLMProvider`.

### Task 6.3: Demo usecase — wire a real incident

**Files:**
- Modify: `services/control-plane/internal/usecase/pipeline_demo.go`
- Create: `services/validator/fixtures/incidents/demo-null-pointer.json`
- Create: `services/validator/fixtures/incidents/demo-zero-div.json`

- [ ] **Step 1: Fixture incidents**

`demo-null-pointer.json`:

```json
{
  "label": "demo-null-pointer",
  "title": "TypeError: 'NoneType' object is not subscriptable in /predict",
  "service": "nexis-fixture",
  "environment": "production",
  "stacktrace": "Traceback (most recent call last):\n  File \"src/nexis_fixture/api.py\", line 8, in safe_div\n    return a / b\nZeroDivisionError: division by zero",
  "logs": "request_id=abc env=prod ts=2026-05-13T12:00:00Z msg=zero-div invocation"
}
```

- [ ] **Step 2: pipeline_demo.go — load the fixture into `PipelineInput.Incident`**

The existing demo usecase already builds a `PipelineInput`. Read the fixture JSON from disk (path resolved from `services/validator/fixtures/incidents/<label>.json`), populate `Incident: &domain.IncidentPayload{...}`, set `RepoSHA: "fixture-" + git head` (resolved at server start, cached). Pass that incident through.

### Task 6.4: Verify Stage 6

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
docker compose up -d --build control-plane
# Re-seed pgvector with the dev org if not already
ORG=$(docker compose exec -T postgres psql -U nexis -d nexis -tA -c "SELECT id FROM organizations LIMIT 1;")
docker compose run --rm control-plane /app/seed-pgvector --org-id=$ORG
# Trigger the demo with LLM_PROVIDER=openai
docker compose logs control-plane | grep "agent.invoked" | head -10
```

### Task 6.5: Commit Stage 6

```bash
git add services/control-plane/internal/workflow services/control-plane/cmd/server services/control-plane/internal/usecase/pipeline_demo.go services/validator/fixtures/incidents
git commit -m "feat(workflow): wire L1 agents into RecoveryPipeline activities + demo incident (stage 6)"
```

---

## Stage 7 — Eval harness

### Task 7.1: Eval repo

**Files:**
- Create: `services/control-plane/internal/adapter/repo/eval_repo.go`

- [ ] **Step 1: Write CRUD** (mirrors `billing_repo` dual-pool):

```go
type EvalRepo struct { pool, adminPool *pgxpool.Pool }

func NewEvalRepo(pool, admin *pgxpool.Pool) *EvalRepo { ... }

func (r *EvalRepo) Insert(ctx context.Context, e EvalRun) (string, error) {
    // adminPool — eval runs are kicked off from a usecase but the goroutine
    // path runs outside the request tx; we record orgID explicitly.
}
func (r *EvalRepo) Update(ctx context.Context, e EvalRun) error
func (r *EvalRepo) Get(ctx context.Context, runID string) (EvalRun, []EvalTranscript, error) // RLS-bound; uses FromCtx + appPool
func (r *EvalRepo) List(ctx context.Context, orgID string, limit int) ([]EvalRun, error)
func (r *EvalRepo) InsertTranscript(ctx context.Context, t EvalTranscript) error  // adminPool
```

Domain types `EvalRun` + `EvalTranscript` live in a new `internal/domain/eval.go`.

### Task 7.2: Eval domain types

**Files:**
- Create: `services/control-plane/internal/domain/eval.go`

```go
package domain

import "time"

type EvalRun struct {
    ID                string
    OrgID             string
    TriggeredBy       string
    IncidentLabel     string
    Status            string // 'queued'|'running'|'completed'|'failed'
    Providers         []string
    OpenAIRunID       string
    OllamaRunID       string
    OpenAICostCents   float64
    OllamaCostCents   float64
    OpenAITokensIn    int64
    OpenAITokensOut   int64
    OllamaTokensIn    int64
    OllamaTokensOut   int64
    StartedAt         time.Time
    CompletedAt       *time.Time
    Error             string
}

type EvalTranscript struct {
    ID               string
    OrgID            string
    EvalRunID        string
    Provider         string
    Agent            AgentName
    Model            string
    SystemPrompt     string
    UserPrompt       string
    AssistantOutput  string
    TokensIn         int
    TokensOut        int
    CachedTokens     int
    CostCents        float64
    DurationMs       int
    SchemaValid      bool
    RecordedAt       time.Time
}
```

### Task 7.3: Eval runner usecase

**Files:**
- Create: `services/control-plane/internal/usecase/eval_runner.go`

- [ ] **Step 1: Write the runner**

```go
// Package usecase — eval_runner orchestrates a single eval: run the
// RecoveryPipeline DAG twice (one per provider) with provider-overridden
// registries, persist transcripts, and roll up costs.
package usecase

// EvalRunner is constructed in main.go with both provider variants of the
// registry pre-built. Run() inserts an eval_runs row, kicks off two parallel
// goroutines, persists transcripts as the agents emit them via a per-run
// transcript collector, then closes the row.
type EvalRunner struct {
    Cfg          config.Config
    AppPool      *pgxpool.Pool
    AdminPool    *pgxpool.Pool
    EvalRepo     *repo.EvalRepo
    LedgerRepo   *repo.TokenLedgerRepo
    Patches      domain.PatchStore
    Validator    ValidatorClient
    SseBroker    *sse.Broker[domain.ActivityEvent]
    AuditWriter  domain.AuditWriter
    Logger       *slog.Logger
    Workflow     *repo.WorkflowRepo
}

func (e *EvalRunner) Run(ctx context.Context, p domain.Principal, label string) (string, error) {
    incident, err := loadFixtureIncident(label)
    if err != nil { return "", err }
    runID, err := e.EvalRepo.Insert(ctx, domain.EvalRun{
        OrgID: p.OrgID, TriggeredBy: p.UserID, IncidentLabel: label,
        Status: "queued", Providers: []string{"openai", "ollama"},
    })
    if err != nil { return "", err }

    go e.runBackground(context.WithoutCancel(ctx), p, runID, incident)
    return runID, nil
}

func (e *EvalRunner) runBackground(ctx context.Context, p domain.Principal, runID string, incident *domain.IncidentPayload) {
    var wg sync.WaitGroup
    results := map[string]providerResult{}
    var mu sync.Mutex

    for _, prov := range []string{"openai", "ollama"} {
        wg.Add(1)
        go func(prov string) {
            defer wg.Done()
            res := e.invokeOneProvider(ctx, p, runID, prov, incident)
            mu.Lock(); results[prov] = res; mu.Unlock()
        }(prov)
    }
    wg.Wait()

    final := domain.EvalRun{
        ID: runID, OrgID: p.OrgID, Status: "completed",
        OpenAICostCents: results["openai"].costCents,
        OllamaCostCents: results["ollama"].costCents,
        OpenAITokensIn:  results["openai"].tokensIn,
        OpenAITokensOut: results["openai"].tokensOut,
        OllamaTokensIn:  results["ollama"].tokensIn,
        OllamaTokensOut: results["ollama"].tokensOut,
        OpenAIRunID:     results["openai"].runID,
        OllamaRunID:     results["ollama"].runID,
    }
    _ = e.EvalRepo.Update(ctx, final)
}

type providerResult struct {
    runID      string
    costCents  float64
    tokensIn   int64
    tokensOut  int64
}

// invokeOneProvider builds a temp registry for the requested provider and
// invokes the 5 L1 agents sequentially (the eval bypasses the L2 stubs).
// Each agent's transcript is persisted to eval_transcripts.
func (e *EvalRunner) invokeOneProvider(ctx context.Context, p domain.Principal, runID, prov string, inc *domain.IncidentPayload) providerResult {
    providers, _ := llm.NewProvidersFor(e.Cfg, prov, e.Logger)
    ledger := e.LedgerRepo
    audit := e.AuditWriter
    llmClient := &agents.LLMClient{Provider: providers.LLM, Embedding: providers.Embedding,
        Ledger: ledger, Audit: audit, Logger: e.Logger, SchemaRetryMax: e.Cfg.AgentSchemaRetryMax}

    retClient := &agents.RetrievalClient{
        Store: retrieval.New(e.AdminPool), Embed: providers.Embedding,
        EmbedModel: e.Cfg.OpenAIEmbedModel, K: 5,
    }

    // Insert a synthetic workflow_runs row for audit attribution.
    syntheticRun := &domain.WorkflowRun{
        OrgID: p.OrgID, WorkspaceID: "", WorkflowType: "EvalReplay",
        Status: domain.WRRunning, StartedAt: time.Now().UTC().Truncate(time.Microsecond),
    }
    _ = e.Workflow.InsertRun(ctx, syntheticRun)

    prior := map[string]any{}
    var totalIn, totalOut int64
    var totalCost float64
    for _, name := range domain.AllL1Agents {
        ag := buildAgent(name, prov, e.Cfg, llmClient, retClient)
        out, err := ag.Run(ctx, domain.AgentInput{
            WorkflowRunID: syntheticRun.ID, OrgID: p.OrgID,
            PriorOutputs: prior, Incident: inc, RepoSHA: "fixture-" + e.Cfg.FixtureSHA,
        })
        schemaValid := err == nil
        _ = e.EvalRepo.InsertTranscript(ctx, domain.EvalTranscript{
            OrgID: p.OrgID, EvalRunID: runID, Provider: prov, Agent: name,
            Model: out.Model, SystemPrompt: out.SystemPrompt, UserPrompt: out.UserPrompt,
            AssistantOutput: out.Content,
            TokensIn: out.TokensIn, TokensOut: out.TokensOut, CachedTokens: out.CachedTokens,
            CostCents: out.CostCents, DurationMs: int(out.DurationMs),
            SchemaValid: schemaValid,
        })
        if err == nil {
            prior[string(name)] = out.Structured
            totalIn += int64(out.TokensIn); totalOut += int64(out.TokensOut); totalCost += out.CostCents
        }
    }
    return providerResult{runID: syntheticRun.ID, costCents: totalCost, tokensIn: totalIn, tokensOut: totalOut}
}
```

`buildAgent` is a small switch that wires each agent constructor with the per-provider model.

### Task 7.4: HTTP handler

**Files:**
- Create: `services/control-plane/internal/transport/http/handler/eval.go`
- Modify: `services/control-plane/internal/transport/http/server.go` (COORDINATOR)

- [ ] **Step 1: handler/eval.go**

Routes:
- `POST /v1/eval/incidents`: body `{label}`; calls `EvalRunner.Run`; returns `{eval_run_id}` 202.
- `GET  /v1/eval/runs`: calls `EvalRepo.List`; returns array.
- `GET  /v1/eval/runs/{id}`: calls `EvalRepo.Get`; returns `{run, transcripts}`.
- `GET  /v1/admin/token-budget` / `POST /v1/admin/token-budget`: read / write to `token_budgets` (owner-only).

Each handler audit-writes (`eval.create`, `eval.read`, `token_budget.set`).

- [ ] **Step 2: server.go — mount under the protected group with RBAC**

```go
r.Route("/v1/eval", func(r chi.Router) {
    r.Use(middleware.RequireRole(domain.RoleAdmin))
    r.Post("/incidents", evalHandler.Create)
    r.Get("/runs",       evalHandler.List)
    r.Get("/runs/{id}",  evalHandler.Get)
})
r.Route("/v1/admin/token-budget", func(r chi.Router) {
    r.Use(middleware.RequireRole(domain.RoleOwner))
    r.Get("/",  budgetHandler.Get)
    r.Post("/", budgetHandler.Update)
})
```

### Task 7.5: eval CLI

**Files:**
- Create: `services/control-plane/cmd/eval/main.go`

- [ ] **Step 1: Write the CLI**

Flags: `--org-id`, `--label`, `--providers=openai,ollama`. The CLI wires its own pool + repos + `EvalRunner` and calls `Run` synchronously (polls the `eval_runs` row every 5s until `status='completed'` or `failed`). Prints the summary block from the spec's Appendix E.

### Task 7.6: Modify pipelines handler to embed token rollup

**Files:**
- Modify: `services/control-plane/internal/transport/http/handler/pipelines.go`

- [ ] **Step 1: Augment the `GET /v1/workspaces/{ws}/pipelines/{run_id}` response**

```go
ledger, _ := e.LedgerRepo.ListLedgerByRun(ctx, runID)
var inSum, outSum int
var costSum float64
perAgent := []map[string]any{}
for _, l := range ledger {
    inSum += l.TokensIn; outSum += l.TokensOut; costSum += l.CostCents
    perAgent = append(perAgent, map[string]any{
        "agent": string(l.Agent), "tokens_in": l.TokensIn,
        "tokens_out": l.TokensOut, "cost_cents": l.CostCents,
    })
}
resp["token_rollup"] = map[string]any{"tokens_in": inSum, "tokens_out": outSum, "cost_cents": costSum}
resp["per_agent"] = perAgent
```

### Task 7.7: Commit Stage 7

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
git add internal/adapter/repo/eval_repo.go internal/domain/eval.go internal/usecase/eval_runner.go internal/transport/http/handler/eval.go internal/transport/http/handler/pipelines.go internal/transport/http/server.go cmd/eval
git commit -m "feat(eval): eval harness + repo + handlers + CLI + pipeline token rollup (stage 7)"
```

---

## Stage 8 — Console /console/eval surface + tokens pill on timeline

### Task 8.1: Eval SDK

**Files:**
- Create: `apps/web/lib/eval.ts`

```ts
const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type EvalRunStatus = "queued"|"running"|"completed"|"failed";
export type EvalRun = {
  id: string; status: EvalRunStatus;
  incident_label: string;
  providers: string[];
  openai_cost_cents?: number; ollama_cost_cents?: number;
  openai_tokens_in?: number;  openai_tokens_out?: number;
  ollama_tokens_in?: number;  ollama_tokens_out?: number;
  started_at: string; completed_at?: string; error?: string;
};
export type EvalTranscript = {
  id: string; provider: "openai"|"ollama"; agent: string;
  model: string; system_prompt: string; user_prompt: string;
  assistant_output: string;
  tokens_in: number; tokens_out: number; cached_tokens: number;
  cost_cents: number; duration_ms: number; schema_valid: boolean;
};
export const evalApi = {
  list:    async (): Promise<EvalRun[]> => (await fetch(`${API}/v1/eval/runs`,    {credentials:"include"})).json(),
  get:     async (id: string): Promise<{run: EvalRun; transcripts: EvalTranscript[]}> =>
             (await fetch(`${API}/v1/eval/runs/${id}`, {credentials:"include"})).json(),
  trigger: async (label: string): Promise<{eval_run_id: string}> =>
             (await fetch(`${API}/v1/eval/incidents`, {
               method:"POST", credentials:"include",
               headers:{"content-type":"application/json"},
               body: JSON.stringify({label}),
             })).json(),
};
```

### Task 8.2: Matrix view

**Files:**
- Create: `apps/web/app/(app)/console/eval/page.tsx`
- Create: `apps/web/components/eval/EvalMatrix.tsx`

Server component fetches `/v1/eval/runs`. Renders a table:
| Run ID | Incident | Started | OpenAI cost | Ollama cost | Status |

Each row links to `/console/eval/[id]`.

### Task 8.3: Detail page + transcript pane + diff viewer

**Files:**
- Create: `apps/web/app/(app)/console/eval/[id]/page.tsx`
- Create: `apps/web/app/(app)/console/eval/[id]/client.tsx`
- Create: `apps/web/components/eval/TranscriptPane.tsx`
- Create: `apps/web/components/eval/DiffSplit.tsx`

`EvalMatrix` (per-eval): rows = `AGENTS_IN_ORDER` (filtered to L1), columns = providers. Each cell is a `<button>` opening the transcript pane on the right.

`TranscriptPane` shows three blocks: system prompt (collapsed by default; click to expand), user prompt (rendered as markdown), assistant output (raw text; for backend, parsed diff). Footer: tokens in/out + cached + cost + duration.

`DiffSplit` mounts two Monaco editors side-by-side for the backend row only, loading `transcripts.find(t => t.agent==="backend" && t.provider==="openai").assistant_output` and the ollama equivalent. Read-only mode; language="diff".

### Task 8.4: Tokens pill on the per-run timeline

**Files:**
- Modify: `apps/web/app/(app)/console/incidents/[id]/client.tsx`
- Create: `apps/web/components/pipelines/TokensPill.tsx`

When the run detail JSON contains `per_agent`, render a `<TokensPill>` next to each `ActivityTimeline` row showing `12.3K → 4.5K · $0.18`. The pill is hidden for L2 agents (no ledger row).

### Task 8.5: Sidebar nav badge

**Files:**
- Modify: `apps/web/components/console/Sidebar.tsx`

Add a new nav entry "Eval" with the `Beaker` icon, pointing to `/console/eval`. Gated to owner/admin (read role from the same source the Sidebar already uses).

### Task 8.6: Commit Stage 8

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
git add apps/web
git commit -m "feat(web): /console/eval matrix + transcript pane + DiffSplit + tokens pill (stage 8)"
```

---

## Stage 9 — E2E + Definition of Done

### Task 9.1: Boot the full stack

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app
docker compose down
docker compose up --build -d
sleep 40
docker compose ps
docker compose logs control-plane | grep "temporal worker registered"
docker compose logs control-plane | grep "agents registered"   # NEW log line in main.go: "agents registered names=architect,backend,qa,devops,data_engineer"
```

### Task 9.2: E2E — OpenAI happy path

```bash
EMAIL="p5+$(date +%s)@example.com"
RESP=$(curl -s -i -X POST http://localhost:8080/v1/auth/signup -H "content-type: application/json" \
  -d "{\"email\":\"$EMAIL\",\"password\":\"correct-horse-battery-staple\",\"org_name\":\"P5 Co\"}")
CK=$(echo "$RESP" | grep -i 'set-cookie:.*nexis_session' | sed 's/.*nexis_session=\([^;]*\).*/\1/' | tr -d '\r')

curl -s -X POST -H "Cookie: nexis_session=$CK" -H "content-type: application/json" \
  -d '{"name":"prod","region":"us-east-1"}' http://localhost:8080/v1/workspaces | jq
sleep 7
WS=$(curl -s -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/workspaces | jq -r '.[0].id')
ORG=$(curl -s -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/me | jq -r '.org_id')

docker compose run --rm control-plane /app/seed-pgvector --org-id=$ORG

# Trigger demo with LLM_PROVIDER=openai (assumed default).
RUN=$(curl -s -X POST -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/workspaces/$WS/pipelines/demo | jq -r '.id')

# Stream events
timeout 120 curl -s -H "Cookie: nexis_session=$CK" \
  http://localhost:8080/v1/workspaces/$WS/pipelines/$RUN/events | head -50

curl -s -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/workspaces/$WS/pipelines/$RUN | jq '.token_rollup'
# Expect: cost_cents < 40, tokens_in/out non-zero
```

### Task 9.3: Verify token ledger + audit

```bash
docker compose exec -T postgres psql -U nexis -d nexis -c \
  "SELECT agent, model, tokens_in, tokens_out, cost_cents, status FROM token_ledger WHERE workflow_run_id='$RUN' ORDER BY recorded_at;"
# Expect 5 rows (one per L1 agent), status='succeeded'

docker compose exec -T postgres psql -U nexis -d nexis -c \
  "SELECT count(*) FROM audit_log WHERE action='agent.invoked' AND target='$RUN';"
# Expect 5
```

### Task 9.4: Verify Ollama path

```bash
docker compose down
LLM_PROVIDER=ollama docker compose up -d
sleep 40
# Re-trigger the demo
RUN_OLLAMA=$(curl -s -X POST -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/workspaces/$WS/pipelines/demo | jq -r '.id')
# Wait up to 5 min for completion
for i in $(seq 1 60); do
    STATUS=$(curl -s -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/workspaces/$WS/pipelines/$RUN_OLLAMA | jq -r '.run.status')
    [ "$STATUS" != "running" ] && break
    sleep 5
done
[ "$STATUS" = "succeeded" ] || (echo "ollama failed: $STATUS"; exit 1)
docker compose exec -T postgres psql -U nexis -d nexis -c \
  "SELECT agent, provider, cost_cents FROM token_ledger WHERE workflow_run_id='$RUN_OLLAMA';"
# Expect cost_cents = 0 on all rows
```

### Task 9.5: Verify schema-retry path

```bash
# Set OPENAI_MOCK_RESPONSE to a known-malformed payload via env override on a single container
docker compose run --rm -e OPENAI_API_KEY=invalid control-plane /app/eval --org-id=$ORG --label=demo-null-pointer --providers=openai
# Should exit code 2; eval_transcripts for the failed call has schema_valid=false
docker compose exec -T postgres psql -U nexis -d nexis -c \
  "SELECT provider, agent, schema_valid FROM eval_transcripts WHERE eval_run_id IN (SELECT id FROM eval_runs ORDER BY started_at DESC LIMIT 1);"
```

### Task 9.6: Verify budget enforcement

```bash
docker compose exec -T postgres psql -U nexis -d nexis -c \
  "UPDATE token_budgets SET allowed_tokens_in=100 WHERE org_id='$ORG';"
RUN_CAP=$(curl -s -X POST -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/workspaces/$WS/pipelines/demo | jq -r '.id')
sleep 5
curl -s -H "Cookie: nexis_session=$CK" http://localhost:8080/v1/workspaces/$WS/pipelines/$RUN_CAP | jq '.run.status, .run.error'
# Expect status='cancelled', error mentions budget
```

### Task 9.7: Verify console UI

```bash
open http://localhost:3000/console/eval
# After triggering /v1/eval/incidents, expect the matrix to render with both providers
open http://localhost:3000/console/incidents/$RUN
# Expect tokens pill next to each L1 row
```

### Task 9.8: Stop-the-world checklists

```bash
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/control-plane && make build && make test && make vet && make arch
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/services/validator    && go build ./... && go test ./...
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web              && /opt/homebrew/bin/pnpm typecheck && /opt/homebrew/bin/pnpm build
```

### Task 9.9: Mark Phase 5 complete

```bash
# Edit docs/PROJECT_PLAN.md — append "— Completed YYYY-MM-DD" to the "Phase 5 — Agents L1 + LLM Spine" heading.
git add docs/PROJECT_PLAN.md
git commit -m "docs: mark Phase 5 complete"
```

---

## Definition of Done

- [ ] `docker compose up --build` brings the full stack (pgvector image + control-plane + temporal + validator + web) to a healthy state.
- [ ] Control-plane logs `agents registered names=architect,backend,qa,devops,data_engineer`.
- [ ] `POST /v1/workspaces/{ws}/pipelines/demo` with `LLM_PROVIDER=openai`: workflow completes in **< 90 s**; sum of `token_ledger.cost_cents` for that run is **< 40** (i.e. < $0.40); all 5 L1 rows `status='succeeded'`.
- [ ] Same demo with `LLM_PROVIDER=ollama` completes in **< 5 minutes** on a 16GB host; all `token_ledger.cost_cents = 0`.
- [ ] After 5 consecutive OpenAI runs, the median `cached_tokens / tokens_in` ratio across Architect+QA+DevOps+DataEngineer is **≥ 60%**.
- [ ] Backend agent's `Structured["patch_diff"]` applied against the fixture via the validator (`POST /v1/validate`) returns `tests_passed=true` on at least one demo incident.
- [ ] QA agent's `Structured["tests"]` files: when concatenated with the Backend patch and run via the validator, **≥ 60%** of test files produce non-erroring pytest runs.
- [ ] Schema retry: a forced malformed response produces one `schema_mismatch` row in `token_ledger`, a retry, then a `succeeded` row; SSE timeline shows `retrying` → `succeeded`.
- [ ] Budget enforcement: with `allowed_tokens_in=100`, a demo trigger lands `workflow_runs.status='cancelled'` within ~2s; `audit_log` has a `agent.invoked` row with `status='budget_exceeded'`.
- [ ] `code_embeddings` has ≥ 10 rows after seeding; the `code_embeddings_embedding_ivfflat` index exists.
- [ ] `POST /v1/eval/incidents` returns 202 with an eval id; `GET /v1/eval/runs/{id}` shows `status='completed'` within ~2 min; transcripts populated for both providers.
- [ ] `/console/eval/{id}` renders the 5×2 matrix; clicking Backend opens a side-by-side Monaco diff of the two patches.
- [ ] `/console/incidents/{id}` shows token + cost pills next to the 5 L1 rows.
- [ ] Member-role user gets 403 on `POST /v1/eval/incidents`.
- [ ] Cross-org user gets 404 on any `/v1/eval/runs/{id}` for a foreign run (RLS).
- [ ] `make build / test / vet / arch` green on control-plane; `pnpm typecheck / build` green on web; validator unchanged + still green.
- [ ] `docs/PROJECT_PLAN.md` Phase 5 heading carries a completion date.

---

## Risks captured

- **OpenAI `cache_control` shape drift.** Adapter tests both branches (CacheSystem=true → array content, =false → string content). If OpenAI removes the field, the adapter still works; we lose the cache discount only.
- **Ollama embedding model missing.** Factory probes `/api/show nomic-embed-text`; on 404 falls back to OpenAI embeddings and logs the fallback. The eval matrix renders a yellow caveat dot when the fallback is active.
- **pgvector ivfflat needs data first.** The migration leaves the index off; the seed script creates it after insertion. Stage 9 E2E includes the seed step.
- **Token budget bootstrap race.** `INSERT … ON CONFLICT (org_id, period_start) DO NOTHING` is race-safe; the subsequent SELECT picks up whichever row won.
- **Eval double-billing.** Eval runs do hit `token_ledger`; the `meta.eval=true` audit metadata lets admin reports exclude them from real-usage. Budget gate still applies — guards runaway eval cost.
- **Ollama 14b model RAM fit.** Factory downgrades `qwen2.5-coder:14b` → `llama3.1:8b` if `/api/show` returns 404. Eval matrix shows the actual model loaded in the column header.
- **Prompt cache TTL ~5 min.** Acceptance criterion 1 implies back-to-back runs in dev; gap-runs (10+ min) miss cache. Documented; not a correctness issue.
- **Schema retry × 2 × 5 agents.** Worst case 15 OpenAI calls per run. Cached, still < $0.50. On Ollama, slower; accepted within the 5-min bar.
- **Cross-org embedding leak.** Only `TopK` reads embeddings, and it always filters by `org_id`. Integration test asserts two orgs cannot read each other's chunks.
- **`pgvector-go` version drift vs pgx.** Pinned to a known-compatible version in `go.mod`; bumping pgx is a paired change documented in CONTRIBUTING.
- **DevOps agent's YAML correctness.** Phase 5 only schema-validates that the string is non-empty. Real ArgoCD / GH Actions linting lands in Phase 6 alongside the GitOps service.
- **DataEngineer agent's SQL safety.** We do not execute the migrations in Phase 5. Phase 6 wires sandboxed SQL apply against a throwaway schema.
- **Eval runner duplicates ledger writes.** Each provider's pass writes its own ledger rows. The eval rollup queries by `workflow_run_id` (the synthetic `EvalReplay` run id), so the regular pipeline ledger is untouched.
- **Provider override re-instantiation cost.** Two parallel registries per eval is fine at Phase 5 scale (1 eval at a time). If Phase 6 batches we'll pool.
