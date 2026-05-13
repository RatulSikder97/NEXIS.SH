# Phase 5 — Agents L1 + LLM Spine — Design

**Date:** 2026-05-13
**Phase:** 5 (Weeks 13–15 per `docs/PROJECT_PLAN.md`).
**Dependencies:** Phase 4 (Pipeline Substrate — completes alongside this work under Pattern A pipeline-parallelism).
**External services:** all local — OpenAI HTTPS API (dual-provider, opt-in via `LLM_PROVIDER=openai`) + local Ollama on the host (`LLM_PROVIDER=ollama`) + the same Postgres / MinIO / Temporal we already run.

---

## 1. Goals

1. **Dual-mode LLM spine.** Extend (do not rewrite) the existing `internal/adapter/llm/{openai,ollama,factory}.go` adapters from Phase 1 so every L1 agent can call the same `domain.LLMProvider` port, switch `LLM_PROVIDER` with zero code changes, and request either the **synthesis** model (`gpt-4o` / `llama3.1:8b`) or the **cheap** model (`gpt-4o-mini` / `qwen2.5-coder:14b`) per call. The OpenAI adapter additionally opts into **prompt caching** via `cache_control` on the system block and surfaces `cached_tokens` to the ledger.
2. **5 L1 agent activities — real implementations.** Replace the Phase 4 stub bodies of `ArchitectSolution`, `BackendCodegen`, `QATestGen`, `DevOpsPipeline`, and `DataEngineerMigrate` with real LLM-driven providers behind a single `domain.Agent` port. Each agent:
   * Builds a typed prompt + system block from the prior step's structured output + top-K pgvector retrieval over the fixture repo.
   * Issues exactly one `LLMProvider.Complete` call (JSON-mode for L1 plan/test/devops/data; raw mode for backend codegen which post-processes to a unified diff).
   * Validates the response against an agent-specific JSON schema (retry up to 2× on schema mismatch — the existing `Phase 5 §4` retry pattern).
   * Returns `domain.AgentOutput` whose `Structured` field is the schema-conformant JSON, `Content` the raw model output, plus token + cost telemetry.
3. **Per-tenant token-budget guardrails.** Every LLM call goes through a new `domain.TokenLedger` port. Pre-flight `CheckBudget` returns `domain.ErrBudgetExceeded` when the org has already burned its monthly allotment; the activity wraps that error as a non-retryable `BudgetError` so the workflow returns `cancelled`, not `failed`. Post-flight `Record` writes one `token_ledger` row per call and updates `token_budgets.used_tokens_{in,out}`.
4. **pgvector retrieval store seeded with the fixture codebase.** New `code_embeddings` table indexes the existing `services/validator/fixtures/` Python repo by ~50-LOC chunks, embeddings via OpenAI `text-embedding-3-small` (1536 dims). Each L1 prompt builder pulls top-K (K=5) nearest chunks. Seeding runs out-of-band via `cmd/seed-pgvector`.
5. **Eval harness.** New `cmd/eval` CLI replays the same synthetic incident through `LLM_PROVIDER=openai` then `LLM_PROVIDER=ollama`, persists transcripts + costs + outputs to `eval_runs` + `eval_transcripts`, and the console renders a matrix view at `/console/eval` with per-cell drill-down to the side-by-side transcripts.
6. **Audit + observability parity.** Every LLM call appends an `audit_log` row (`action=agent.invoked`) with `{agent, model, tokens_in, tokens_out, cached_tokens, cost_cents, duration_ms}` and emits a structured slog record. Errors carry an `agent_error_type` field for Grafana grouping.

## 2. Non-goals

* **L2 agents.** Sentinel / Pathfinder / Synthesiser / ApprovalGate stay as Phase 4 stubs — Phase 6 fills them in. The DAG ordering does not change.
* **GitHub PR opening / GitOps writes.** `DevOpsPipeline` emits YAML inside `AgentOutput.Structured` only; no PR is opened, no file is committed. Phase 6 wires the GitOps service.
* **Sentry → workflow auto-trigger.** Phase 5 still triggers workflows manually via the Live Demo CTA + `POST /v1/workspaces/{ws}/pipelines/demo`. The Sentinel streaming subscription lands in Phase 6.
* **Hypothesis property-based tests.** `QATestGen` produces vanilla `pytest` cases. Phase 6 swaps Hypothesis in.
* **Real Stripe usage line items.** Token + cost numbers persist to `token_ledger`. Stitching `token_ledger` into `usage_records` waits for Phase 7 when Stripe metered billing lands.
* **Real Modal / S3 / KMS swaps.** All artifacts continue to land in MinIO via the Phase 4 envelope-encrypted `domain.PatchStore`. Phase 7 swaps the backends.
* **Cross-org embedding sharing.** `code_embeddings.org_id` is the only retrieval key — even though the Phase 5 fixture repo is shared, every org's embeddings are inserted under that org's id. Phase 6/7 introduce a `repo_global` flag if we ever want public OSS embeddings.
* **A second GUC for workspace-scoped RLS.** Phase 5's three new tables are org-scoped only, like every other RLS table since Phase 2.
* **Streaming completion responses.** All providers run in non-stream mode for Phase 5. Streaming tokens to the SSE timeline lands in Phase 6 once the L2 Synthesiser orchestrates multi-turn calls.

## 3. Architecture

Same port/adapter pattern as Phases 1–4. Phase 5 introduces one new component — `agents` — at the same level as `workflow`: `adapter` and `workflow` may depend on it; nothing else may. The `internal/workflow/recovery/activities.go` thin layer keeps its existing dependency surface and gains a single new field (`Agents *agents.Registry`).

### 3.1 control-plane Go layout

```
internal/
  domain/
    agent.go               # NEW — Agent port + AgentInput/AgentOutput + ErrAgentSchemaMismatch
    token_ledger.go        # NEW — TokenLedger port + Budget + ErrBudgetExceeded
    retrieval.go           # NEW — RetrievalStore port + Chunk + Embedding
    errors.go              # MODIFY — add ErrBudgetExceeded, ErrAgentSchemaMismatch
    ports.go               # MODIFY — extend CompletionRequest with CacheSystem bool + Modality enum; CompletionResponse gains CachedTokens + DurationMs + CostCents

  adapter/
    llm/
      openai.go            # MODIFY — accept CacheSystem; pass `cache_control: {type:"ephemeral"}` on system block; surface usage.prompt_tokens_details.cached_tokens; multi-model routing already supported via req.Model
      openai_test.go       # MODIFY — mock cached + non-cached responses; assert CachedTokens propagation
      ollama.go            # MODIFY — extend with Embed(text) for `nomic-embed-text`; ignore CacheSystem; cost = 0
      ollama_test.go       # MODIFY — assert CacheSystem is a no-op
      factory.go           # MODIFY — return concrete provider that also satisfies a new EmbeddingProvider port (OpenAI uses text-embedding-3-small; Ollama uses nomic-embed-text; falls back to OpenAI when nomic-embed-text absent)
    repo/
      token_ledger_repo.go # NEW — token_budgets + token_ledger CRUD; dual-pool (admin for cron + activity, app for HTTP reads)
      retrieval_repo.go    # NEW — code_embeddings CRUD; admin-pool for seed; app-pool for retrieval inside activity (kept dual to allow Phase 6 multi-org reads from system jobs)
      eval_repo.go         # NEW — eval_runs + eval_transcripts CRUD
    agents/                # NEW — one Provider per agent + a Registry + a shared LLM wrapper
      registry.go          # NEW — Registry.Get(name) Agent; lazy init
      llm.go               # NEW — wrap a domain.LLMProvider + TokenLedger + AuditWriter + slog; one Invoke() helper that every agent calls
      retrieval.go         # NEW — top-K wrapper around domain.RetrievalStore + a prompt-stringification helper
      architect/
        provider.go        # NEW — implements domain.Agent for "architect"
        prompts.go         # NEW — Go const SystemPrompt + UserTemplate
        schema.go          # NEW — JSON schema document (Go string const) + validator using gojsonschema
        provider_test.go   # NEW — table-driven: golden prompt build, mocked LLM, schema-mismatch retry
      backend/
        provider.go        # NEW — implements domain.Agent for "backend"; post-processes raw model output to a unified diff via patch_extract.go
        prompts.go
        patch_extract.go   # NEW — yank ```diff ... ``` blocks, fall back to "+/-" line scan, return canonical unified diff or ErrAgentSchemaMismatch
        provider_test.go
      qa/
        provider.go        # NEW — implements domain.Agent for "qa"; outputs map[string]string{filename → pytest source}
        prompts.go
        schema.go
        provider_test.go
      devops/
        provider.go        # NEW — implements domain.Agent for "devops"; outputs ArgoCD app yaml + GH Actions yaml as strings
        prompts.go
        schema.go
        provider_test.go
      data_engineer/
        provider.go        # NEW — implements domain.Agent for "data_engineer"; outputs forward + reverse SQL migrations
        prompts.go
        schema.go
        provider_test.go
    retrieval/
      pgvector.go          # NEW — RetrievalStore impl on top of pgvector-go + a *pgxpool.Pool
      pgvector_test.go     # NEW — testcontainers-bound; skipped without DATABASE_URL_TEST
      chunker.go           # NEW — file → []Chunk: walk a directory, read each file, slice by ~50 LOC with 5-LOC overlap, skip lockfiles + binary
      chunker_test.go
    audit/
      llm_audit.go         # NEW — thin wrapper that builds the metadata blob agent.invoked rows carry; uses existing domain.AuditWriter

  usecase/
    pipeline_demo.go       # MODIFY — extend the demo PipelineInput so the synthetic incident carries a real "incident" payload (title, service, stacktrace) so the L1 agents have something to read
    eval_runner.go         # NEW — orchestrates a single eval invocation: spin a workflow input → run RecoveryPipeline twice (once per provider) via local activity invocation → diff outputs → persist transcripts

  workflow/
    recovery/
      activities.go        # MODIFY — Activities gains Agents *agents.Registry; the 5 L1 methods now call a.Agents.Get(<name>).Run(ctx, ...) instead of stub(); Phase 4 stub paths kept when Agents == nil so the test suite remains green without LLM mocks
      activities_test.go   # MODIFY — add mock-agent path
      types.go             # MODIFY — PipelineInput gains optional Incident *IncidentPayload (org-scoped; fed by the demo usecase)
      workflow.go          # MODIFY — no changes to ordering; just bumps llmActivityOpts.MaximumAttempts to 2 (LLM activities should not retry storm)

  transport/http/
    handler/
      eval.go              # NEW — POST /v1/eval/incidents (admin-only) → start a run; GET /v1/eval/runs → list; GET /v1/eval/runs/{id} → full matrix + per-cell transcripts
      pipelines.go         # MODIFY — when GetRun is called, also embed the per-activity token/cost rollup from token_ledger so the timeline footer can render "tokens 12,300 cost $0.18"

  platform/
    config/
      config.go            # MODIFY — add OpenAIEmbedModel, OllamaEmbedModel, OpenAIPromptCache, TokenBudgetTokensIn, TokenBudgetTokensOut, TokenBudgetPeriodDays, EvalEnabled
    pgvector/
      pgvector.go          # NEW — tiny helper around github.com/pgvector/pgvector-go (Vector type, encode/decode helpers); kept here so the adapter layer doesn't import pgvector directly outside retrieval

cmd/
  server/main.go           # MODIFY (COORDINATOR) — wire EmbeddingProvider; instantiate TokenLedgerRepo + RetrievalStore + agents.Registry; pass into NewActivitiesFull
  seed-pgvector/main.go    # NEW — walks services/validator/fixtures/, embeds, inserts to code_embeddings under a config-provided ORG_ID
  eval/main.go             # NEW — CLI entry: runs an incident through both providers, prints summary, writes eval_runs row
```

### 3.2 Web (Next.js 16) layout

```
apps/web/
  app/
    (app)/
      console/
        eval/
          page.tsx                 # NEW — server fetches /v1/eval/runs; renders the matrix
          [id]/
            page.tsx               # NEW — per-eval-run matrix + transcript panel
            client.tsx             # NEW — side-by-side diff viewer; uses Monaco for the patch column
        incidents/[id]/
          client.tsx               # MODIFY — render tokens + cost in the timeline header
  components/
    eval/
      EvalMatrix.tsx               # NEW — table: rows = activities; columns = providers; cells = pass/fail/cost
      TranscriptPane.tsx           # NEW — collapsible system + user + assistant blocks with token counts
      DiffSplit.tsx                # NEW — Monaco editor split view for the two backend patches
    pipelines/
      TokensPill.tsx               # NEW — small pill rendered next to an activity row showing "12.3K → 4.5K · $0.18"
  lib/
    eval.ts                        # NEW — SDK: listRuns, getRun, triggerRun
```

## 4. Database

### 4.1 New tables

```
token_budgets
  id                    uuid PK
  org_id                uuid NOT NULL REFERENCES organizations(id)
  period_start          timestamptz NOT NULL
  period_end            timestamptz NOT NULL
  allowed_tokens_in     bigint NOT NULL
  allowed_tokens_out    bigint NOT NULL
  used_tokens_in        bigint NOT NULL DEFAULT 0
  used_tokens_out       bigint NOT NULL DEFAULT 0
  created_at            timestamptz NOT NULL DEFAULT now()
  updated_at            timestamptz NOT NULL DEFAULT now()
  UNIQUE (org_id, period_start)

token_ledger
  id                    uuid PK
  org_id                uuid NOT NULL REFERENCES organizations(id)
  workflow_run_id       uuid REFERENCES workflow_runs(id) ON DELETE SET NULL
  agent                 text NOT NULL                   -- 'architect'|'backend'|...
  model                 text NOT NULL                   -- 'gpt-4o' | 'llama3.1:8b' | ...
  provider              text NOT NULL                   -- 'openai' | 'ollama'
  tokens_in             int  NOT NULL
  tokens_out            int  NOT NULL
  cached_tokens         int  NOT NULL DEFAULT 0
  cost_cents            numeric(20, 6) NOT NULL DEFAULT 0
  duration_ms           int  NOT NULL
  status                text NOT NULL                   -- 'succeeded'|'schema_mismatch'|'budget_exceeded'|'provider_error'
  recorded_at           timestamptz NOT NULL DEFAULT now()

code_embeddings
  id                    uuid PK
  org_id                uuid NOT NULL REFERENCES organizations(id)
  repo_sha              text NOT NULL                   -- fixture identifier; future: real repo sha
  file_path             text NOT NULL
  chunk_start           int NOT NULL                    -- starting line (1-indexed)
  chunk_end             int NOT NULL
  content               text NOT NULL                   -- raw chunk text
  embedding             vector(1536) NOT NULL           -- pgvector, OpenAI text-embedding-3-small dims
  created_at            timestamptz NOT NULL DEFAULT now()

eval_runs
  id                    uuid PK
  org_id                uuid NOT NULL REFERENCES organizations(id)
  triggered_by          uuid REFERENCES users(id)
  incident_label        text NOT NULL                   -- 'demo-null-pointer-fixture' etc.
  status                text NOT NULL                   -- 'queued'|'running'|'completed'|'failed'
  providers             text[] NOT NULL                 -- ['openai','ollama']
  openai_run_id         uuid REFERENCES workflow_runs(id)
  ollama_run_id         uuid REFERENCES workflow_runs(id)
  openai_cost_cents     numeric(20, 6)
  ollama_cost_cents     numeric(20, 6)
  openai_tokens_in      bigint
  openai_tokens_out     bigint
  ollama_tokens_in      bigint
  ollama_tokens_out     bigint
  started_at            timestamptz NOT NULL DEFAULT now()
  completed_at          timestamptz
  error                 text

eval_transcripts
  id                    uuid PK
  org_id                uuid NOT NULL REFERENCES organizations(id)
  eval_run_id           uuid NOT NULL REFERENCES eval_runs(id) ON DELETE CASCADE
  provider              text NOT NULL                   -- 'openai'|'ollama'
  agent                 text NOT NULL                   -- 'architect'|'backend'|'qa'|'devops'|'data_engineer'
  model                 text NOT NULL
  system_prompt         text NOT NULL
  user_prompt           text NOT NULL
  assistant_output      text NOT NULL
  tokens_in             int NOT NULL
  tokens_out            int NOT NULL
  cached_tokens         int NOT NULL DEFAULT 0
  cost_cents            numeric(20, 6) NOT NULL DEFAULT 0
  duration_ms           int NOT NULL
  schema_valid          boolean NOT NULL
  recorded_at           timestamptz NOT NULL DEFAULT now()
  UNIQUE (eval_run_id, provider, agent)
```

All five tables RLS-protected on `org_id` with the standard `tenant_isolation` policy using `NULLIF(current_setting('app.current_org_id', true), '')::uuid`. `GRANT SELECT, INSERT, UPDATE, DELETE` to `nexis_app` on all five.

Extension dependency: `CREATE EXTENSION IF NOT EXISTS vector` runs in the same migration that creates `code_embeddings`.

Indexes:
- `token_ledger (org_id, recorded_at DESC)` — billing / eval queries.
- `token_ledger (workflow_run_id)` — timeline footer rollup.
- `code_embeddings (org_id, repo_sha)` — narrows the retrieval scan.
- `code_embeddings USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)` — the actual ANN index. Built after seeding to populate the centroids correctly; the migration omits the index and the seed script runs `CREATE INDEX` once after insertion.
- `eval_runs (org_id, started_at DESC)` — list view.

### 4.2 Modifications

None. No new columns on existing tables. `audit_log` already accepts arbitrary metadata so `agent.invoked` rows fit without schema change.

## 5. Agent port design

```go
// internal/domain/agent.go

// AgentName names the 5 L1 agents (Phase 5) and the 4 L2 agents (Phase 6).
// Phase 5 ships only the L1 set; Phase 6 fills in the rest.
type AgentName string

const (
    AgentNameArchitect     AgentName = "architect"
    AgentNameBackend       AgentName = "backend"
    AgentNameQA            AgentName = "qa"
    AgentNameDevOps        AgentName = "devops"
    AgentNameDataEngineer  AgentName = "data_engineer"
)

// AgentInput is what every agent reads. PriorOutputs is the {agent_name → Structured}
// map from earlier steps in the DAG (Architect.Structured is keyed at "architect", etc.).
// PromptContext is the workflow-run-scoped narrative the L2 Synthesiser produced;
// Phase 5's demo path seeds it with a fixture incident description.
type AgentInput struct {
    WorkflowRunID string                 `json:"workflow_run_id"`
    OrgID         string                 `json:"org_id"`
    WorkspaceID   string                 `json:"workspace_id"`
    PriorOutputs  map[string]any         `json:"prior_outputs,omitempty"`
    PromptContext string                 `json:"prompt_context"`
    Incident      *IncidentPayload       `json:"incident,omitempty"`
}

// AgentOutput is what every agent returns. Structured is the JSON-validated
// payload Phase 6 agents consume; Content is the raw model text (useful for
// transcripts + diffing across providers).
type AgentOutput struct {
    Success     bool                   `json:"success"`
    Content     string                 `json:"content"`
    Structured  map[string]any         `json:"structured"`
    TokensIn    int                    `json:"tokens_in"`
    TokensOut   int                    `json:"tokens_out"`
    CachedTokens int                   `json:"cached_tokens,omitempty"`
    CostCents   float64                `json:"cost_cents"`
    DurationMs  int64                  `json:"duration_ms"`
    Model       string                 `json:"model"`
    Provider    string                 `json:"provider"`
}

type Agent interface {
    Name() AgentName
    Run(ctx context.Context, in AgentInput) (AgentOutput, error)
}

// IncidentPayload — minimal shape consumed by the L1 agents in Phase 5. The
// demo usecase fills this from a fixture file at services/validator/fixtures/incidents/.
type IncidentPayload struct {
    Title       string `json:"title"`
    Service     string `json:"service"`
    Environment string `json:"environment"`
    Stacktrace  string `json:"stacktrace,omitempty"`
    Logs        string `json:"logs,omitempty"`
}
```

The `Agent` interface is satisfied by every package under `internal/adapter/agents/<name>/`. Phase 6's L2 agents (sentinel, pathfinder, synthesiser, approval_gate) will implement the same port. The Registry is the single point of dispatch:

```go
// internal/adapter/agents/registry.go
type Registry struct{ byName map[domain.AgentName]domain.Agent }
func (r *Registry) Get(n domain.AgentName) (domain.Agent, error)
```

## 6. LLM adapter extensions

### 6.1 Request / response surface changes

The `domain.CompletionRequest` + `domain.CompletionResponse` carry two new fields plus a new request mode. Existing callers (Phase 1 smoke tests, Phase 3 prompt seeds) are unaffected — defaults keep the prior behavior.

```go
type CompletionRequest struct {
    Model        string  `json:"model"`
    System       string  `json:"system,omitempty"`
    Prompt       string  `json:"prompt"`
    MaxTokens    int     `json:"max_tokens,omitempty"`
    Temperature  float32 `json:"temperature,omitempty"`
    JSONResponse bool    `json:"json_response,omitempty"`
    CacheSystem  bool    `json:"cache_system,omitempty"` // NEW — turn on prompt caching (OpenAI only; Ollama ignores)
    Modality     string  `json:"modality,omitempty"`     // NEW — 'chat' (default) | 'embed'
}

type CompletionResponse struct {
    Content       string  `json:"content"`
    InputTokens   int     `json:"input_tokens"`
    OutputTokens  int     `json:"output_tokens"`
    CachedTokens  int     `json:"cached_tokens,omitempty"`  // NEW
    Model         string  `json:"model"`
    DurationMs    int64   `json:"duration_ms,omitempty"`    // NEW
    CostCents     float64 `json:"cost_cents,omitempty"`     // NEW — populated by the adapter
}
```

### 6.2 OpenAI prompt caching

`internal/adapter/llm/openai.go`:

* When `req.CacheSystem == true` and `req.System != ""`, the request body's system message becomes an array-of-blocks shape:
  ```json
  { "role": "system", "content": [ { "type": "text", "text": "<system>", "cache_control": { "type": "ephemeral" } } ] }
  ```
  rather than the plain `{ role, content: string }` form. OpenAI returns the cache hit ratio inside `usage.prompt_tokens_details.cached_tokens`.
* After decode, the adapter sets `CompletionResponse.CachedTokens = parsed.Usage.PromptTokensDetails.CachedTokens`.
* Cost is computed inside the adapter:
  * `gpt-4o`:        $2.50 / 1M tokens in, $10.00 / 1M tokens out, cached input $1.25 / 1M.
  * `gpt-4o-mini`:   $0.15 / 1M tokens in, $0.60 / 1M tokens out, cached input $0.075 / 1M.
  * `text-embedding-3-small`: $0.02 / 1M tokens.
  Rates live as Go constants in `openai.go` so a price change is one line.

### 6.3 Dual-model routing

Already supported by `req.Model`. Phase 5 wires it like this:

| Agent | Model on OpenAI | Model on Ollama | `CacheSystem` |
|---|---|---|---|
| Architect | `gpt-4o-mini` (cheap, JSON-mode) | `llama3.1:8b` | true |
| Backend | `gpt-4o` (synthesis) | `qwen2.5-coder:14b` if present, else `llama3.1:8b` | true |
| QA | `gpt-4o-mini` | `llama3.1:8b` | true |
| DevOps | `gpt-4o-mini` | `llama3.1:8b` | true |
| DataEngineer | `gpt-4o-mini` | `llama3.1:8b` | true |

The agent `provider.go` calls `Complete` with `req.Model = a.cfg.ModelFor(name, provider)`. `ModelFor` reads the config that the factory baked in.

Each agent's `MaxTokens` budget:
* Architect: 1500
* Backend: 4000
* QA: 3500
* DevOps: 1500
* DataEngineer: 1500

### 6.4 Embedding provider port

```go
// internal/domain/retrieval.go
type EmbeddingProvider interface {
    Embed(ctx context.Context, model string, texts []string) ([][]float32, error)
    EmbeddingDims() int
}
```

The factory returns an `EmbeddingProvider` next to the `LLMProvider`:
* `openai`: real `POST /v1/embeddings` to `text-embedding-3-small`, 1536 dims, $0.02 / 1M cost.
* `ollama`: tries `POST /api/embeddings` to `nomic-embed-text`; if the model is absent (404 from `/api/show`) the factory falls back to the **OpenAI** embedding adapter — embeddings are required for retrieval and `nomic-embed-text` is only 137MB so we instruct the user to pull it in `README.md`. The fallback is logged at startup so the dev knows.

Both implementations live in their existing files (`openai.go`, `ollama.go`); a single new function `Embed(ctx, model, []texts) ([][]float32, error)` is added to each. The factory composes:

```go
type Providers struct {
    LLM       domain.LLMProvider
    Embedding domain.EmbeddingProvider
}
func NewFromConfig(cfg config.Config) (Providers, error)
```

## 7. TokenLedger design

```go
// internal/domain/token_ledger.go
type TokenBudget struct {
    OrgID            string
    PeriodStart      time.Time
    PeriodEnd        time.Time
    AllowedTokensIn  int64
    AllowedTokensOut int64
    UsedTokensIn     int64
    UsedTokensOut    int64
}

type TokenLedgerEntry struct {
    OrgID         string
    WorkflowRunID string
    Agent         AgentName
    Model         string
    Provider      string
    TokensIn      int
    TokensOut     int
    CachedTokens  int
    CostCents     float64
    DurationMs    int
    Status        string  // 'succeeded'|'schema_mismatch'|'budget_exceeded'|'provider_error'
}

type TokenLedger interface {
    // CheckBudget returns ErrBudgetExceeded when the current period's
    // used_tokens_{in,out} + expectedTokensIn would breach the allowance.
    // expectedTokensIn is a rough upper bound — the agent passes
    // (max_tokens_in_estimate + max_tokens_out_estimate) to keep the check cheap.
    CheckBudget(ctx context.Context, orgID string, expectedTokens int) error

    // Record persists one row + updates token_budgets atomically inside a tx.
    Record(ctx context.Context, e TokenLedgerEntry) error

    // GetBudget returns the current period's budget (creating one with default
    // allotments if missing). Caller is expected to be inside an RLS tx.
    GetBudget(ctx context.Context, orgID string) (TokenBudget, error)
}
```

**Default allotment**: 1,000,000 tokens-in + 200,000 tokens-out per 30-day rolling period. Env-tunable via `TOKEN_BUDGET_TOKENS_IN`, `TOKEN_BUDGET_TOKENS_OUT`, `TOKEN_BUDGET_PERIOD_DAYS`. The very first call for an org bootstraps a row using `INSERT … ON CONFLICT DO NOTHING`.

**Cron**: the existing `internal/platform/cron` tick runs a `token_budgets_rotate` job daily at 00:10 local — closes periods whose `period_end < now()` and opens a fresh row keyed off `now() + interval N days`. Idempotent on `(org_id, period_start)`.

**Pre-flight check**:
```go
err := ledger.CheckBudget(ctx, orgID, expectedTokens)
if errors.Is(err, domain.ErrBudgetExceeded) {
    return AgentOutput{}, temporal.NewNonRetryableApplicationError(
        "budget exceeded", "BudgetError", err,
    )
}
```
The workflow inspects the error type and updates `workflow_runs.status = 'cancelled'`. The SSE handler emits a final terminal event with `status: 'cancelled'` and `message: 'token budget exceeded for org'`.

**Post-flight record**: a single transaction does both the ledger insert and the `used_tokens_*` increment so retries don't double-count. Ollama runs record `cost_cents = 0` but still bump usage so the budget caps the local-mode dev/demo too.

**Audit**: every `Record` call also writes one `audit_log` row with `action = agent.invoked` and metadata `{agent, model, provider, tokens_in, tokens_out, cached_tokens, cost_cents, status}`. The audit chain HMAC is fed by `AuditSecret` exactly as in Phases 2+.

## 8. Retrieval store design

### 8.1 Chunking

`internal/adapter/retrieval/chunker.go`:

* Walk the input directory recursively.
* Skip files matching any of: `.git/**`, `__pycache__/**`, lockfiles (`*.lock`, `pnpm-lock.yaml`, `package-lock.json`, `poetry.lock`), binaries detected by sniffing the first 512 bytes for null bytes, files over 256 KB.
* Split each remaining file into chunks of **50 lines** with **5 lines of overlap** between chunks (so a function on the boundary is in both). For files under 50 lines, one chunk = the whole file.
* Each chunk gets:
  * `file_path` — repo-relative POSIX path.
  * `chunk_start`, `chunk_end` — 1-indexed line numbers.
  * `content` — exact slice (preserving leading whitespace).

### 8.2 Seed flow

`cmd/seed-pgvector/main.go`:

1. Load config; resolve `ORG_ID` from a required `--org-id` flag (or `SEED_ORG_ID` env).
2. Resolve `repo_sha`: for Phase 5 the fixture has no real sha so we use `git rev-parse HEAD` of the monorepo via `os/exec`, prefixed with `fixture-`.
3. Walk `services/validator/fixtures/` → chunks via the chunker.
4. Batch the chunks into groups of 64 → call `EmbeddingProvider.Embed(ctx, EmbedModel, batch.Contents())`.
5. Insert into `code_embeddings` via the admin pool (RLS bypassed; the script provides `org_id` explicitly per row).
6. After every chunk is in, `CREATE INDEX IF NOT EXISTS code_embeddings_embedding_ivfflat ON code_embeddings USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);`. ivfflat must be built after data exists, otherwise centroids are useless.
7. Print `seeded N chunks for org=<id> repo_sha=<sha>`.

The seed script lives in `cmd/seed-pgvector/` so it can be invoked from Compose:
```
docker compose run --rm control-plane /app/seed-pgvector --org-id=$ORG_ID
```

### 8.3 Retrieval flow

Inside each agent's prompt builder:

1. Build a query string — for Architect/Backend/QA/DataEngineer it concatenates `Incident.Title + Incident.Stacktrace + PriorOutputs["architect"]["files"]` (when present). For DevOps the query is `Incident.Title + Incident.Environment`.
2. Embed the query (one embedding call per agent).
3. Call `RetrievalStore.TopK(ctx, orgID, repoSHA, queryEmbedding, K=5)`.
4. Format the chunks as a markdown block prepended to the user prompt:
   ```
   ## Relevant code (top 5 chunks by cosine similarity)
   ### `src/foo.py` L12–L60
   ```python
   <content>
   ```
   ### `src/bar.py` L20–L70
   ...
   ```

`RetrievalStore` port:

```go
type RetrievalStore interface {
    Insert(ctx context.Context, batch []Chunk) error
    TopK(ctx context.Context, orgID, repoSHA string, query []float32, k int) ([]Chunk, error)
}

type Chunk struct {
    OrgID, RepoSHA, FilePath string
    ChunkStart, ChunkEnd     int
    Content                  string
    Embedding                []float32
    Similarity               float32 // populated by TopK
}
```

Underneath, `pgvector.go` uses `pgvector.NewVector(float32slice)` to bind a Go slice to the `vector` column and parameterizes the query:

```sql
SELECT id, file_path, chunk_start, chunk_end, content,
       1 - (embedding <=> $1::vector) AS similarity
FROM code_embeddings
WHERE org_id = $2 AND repo_sha = $3
ORDER BY embedding <=> $1::vector
LIMIT $4
```

The cosine distance operator is `<=>`; similarity is `1 - distance`. The `<=>` operator uses the ivfflat index when present.

## 9. Eval harness design

### 9.1 Trigger surface

```
POST /v1/eval/incidents          { "label": "demo-null-pointer" }
GET  /v1/eval/runs               → 200 [EvalRun]
GET  /v1/eval/runs/{id}          → 200 EvalRunDetail
```

The `POST` endpoint:
1. Validates the label exists in the fixture catalog (`services/validator/fixtures/incidents/<label>.json`).
2. Inserts an `eval_runs` row with `status='queued'`.
3. Kicks off a background goroutine in `usecase/eval_runner.go` that:
   * Builds an `IncidentPayload` from the fixture JSON.
   * Runs the full `RecoveryPipeline` workflow **twice in parallel**, once with provider override = `openai`, once with `ollama`. Provider override propagates via a per-call `agents.Registry` instance — each run constructs its own registry with a single-provider factory.
   * On completion, looks up `token_ledger` rows for each run and rolls them up; writes `eval_transcripts` rows from the per-agent `AgentOutput`.
   * Updates `eval_runs.status` to `completed` (or `failed` with `error`).

The provider override means we instantiate `llm.NewFromConfigWithOverride(cfg, "openai")` and `…("ollama")` — two registries, two workflows, one DB row tying them. The temp registries are scoped to the eval run only.

### 9.2 Matrix view

`apps/web/app/(app)/console/eval/page.tsx` server-renders a table:

| Agent | OpenAI cost | OpenAI tokens | OpenAI schema valid? | Ollama cost | Ollama tokens | Ollama schema valid? |
|---|---|---|---|---|---|---|
| Architect | $0.0021 | 12k/2k | ✓ | $0.0000 | 18k/3k | ✓ |
| Backend  | $0.18  | 35k/9k | ✓ | $0.0000 | 41k/12k | ✓ |
| QA       | $0.0040 | 18k/4k | ✓ | $0.0000 | 20k/5k | ✓ |
| …        |        |        |  |        |        |  |

Cells link to a `TranscriptPane` showing system + user + assistant blocks with token counts. For the Backend row a `DiffSplit` opens that mounts Monaco editors side by side with the two patches.

### 9.3 Replay safety

The eval runner does not signal Temporal — it instantiates a **local activity invocation** path (Phase 5 introduces a tiny `agents.Invoker` interface) that bypasses the worker queue entirely so the eval runs aren't visible in normal pipeline lists and don't contend for the `nexis-recovery` task queue with prod-shaped runs. The `eval_runs.openai_run_id` + `ollama_run_id` are synthetic — they reference fresh `workflow_runs` rows the runner inserts with `workflow_type = 'EvalReplay'` so the audit trail is preserved without polluting the timeline.

## 10. HTTP surface

### 10.1 New routes

```
POST /v1/eval/incidents                                  → 202 EvalRun           [auth, RLS, owner|admin]
GET  /v1/eval/runs                                       → 200 [EvalRun]         [auth, RLS, any role]
GET  /v1/eval/runs/{id}                                  → 200 EvalRunDetail     [auth, RLS, any role]
GET  /v1/admin/token-budget                              → 200 TokenBudget       [auth, RLS, owner]
POST /v1/admin/token-budget                              → 200 TokenBudget       [auth, RLS, owner] — owner can up/down their own budget
```

`POST /v1/eval/incidents` rejects requests when `EVAL_ENABLED=false` (default `true` in dev, `false` in prod via Phase 7 env defaults).

### 10.2 Modified routes

```
GET /v1/workspaces/{ws}/pipelines/{run_id}               # now includes
                                                         #   token_rollup: { tokens_in, tokens_out, cost_cents }
                                                         #   per_agent:    [{agent, tokens_in, tokens_out, cost_cents}]
```

The workspace-ownership + RLS pattern from Phase 4 stays unchanged.

### 10.3 RBAC

| Endpoint | owner | admin | member |
|---|---|---|---|
| `POST /v1/eval/incidents` | ✓ | ✓ | ✗ |
| `GET  /v1/eval/runs*` | ✓ | ✓ | ✓ |
| `GET  /v1/admin/token-budget` | ✓ | ✗ | ✗ |
| `POST /v1/admin/token-budget` | ✓ | ✗ | ✗ |

All writes audit-log via the existing `domain.AuditWriter`.

## 11. Acceptance criteria

1. **OpenAI happy path (`LLM_PROVIDER=openai`)**: trigger `/v1/workspaces/{ws}/pipelines/demo` → workflow completes in **< 90 s** with all 9 activity rows `succeeded`. `token_ledger` rows for the 5 L1 agents sum to **< $0.40**. Cache-hit rate on Architect+QA+DevOps+DataEng across **5 consecutive runs** is **≥ 60%**.
2. **Ollama happy path (`LLM_PROVIDER=ollama`)**: same flow on a 16GB-RAM host with `qwen2.5-coder:14b` (or `llama3.1:8b` fallback) completes in **< 5 minutes**. Total cost `0.000`.
3. **Quality bar.** Run the OpenAI L1 patch + tests against the fixture sandbox (`POST /v1/validate`). `tests_passed && fail_count == 0` is the target. The acceptance threshold is **≥ 60% of generated test files** (`AgentOutput.Structured["tests"]`) produce non-erroring `pytest` runs against the fixture repo with the OpenAI-produced patch applied.
4. **Schema enforcement.** Manually inject an invalid response by setting `OPENAI_MOCK_RESPONSE=<malformed>`; observe one `schema_mismatch` row in `token_ledger`, a retry, then a `succeeded` row. Workflow surfaces the retry frame on the SSE timeline (`status='retrying'` → `'succeeded'`).
5. **Budget enforcement.** Set `TOKEN_BUDGET_TOKENS_IN=100` for the demo org; trigger the demo workflow; observe `workflow_runs.status='cancelled'` within ~2 seconds, the SSE timeline emits a terminal `Pipeline.Complete` event with `status='cancelled'`, and `audit_log` has a `agent.invoked` row with `status='budget_exceeded'`.
6. **pgvector seeded.** After `docker compose run --rm control-plane /app/seed-pgvector --org-id=<id>`, `SELECT count(*) FROM code_embeddings WHERE org_id=$1` returns ≥ 10 (12 trivial fixture files × ~1 chunk each). The ivfflat index exists: `\d code_embeddings` lists `code_embeddings_embedding_ivfflat`.
7. **Eval matrix.** `POST /v1/eval/incidents` with `{"label":"demo-null-pointer"}` returns 202 within 50 ms. ~2 minutes later `GET /v1/eval/runs/{id}` shows `status='completed'`, the matrix populated with 5 agents × 2 providers, transcripts readable. `/console/eval/{id}` renders the matrix + DiffSplit for the Backend row.
8. **Auditability.** `SELECT count(*) FROM audit_log WHERE action='agent.invoked' AND target=<run_id>` returns at minimum 5 (one per L1 agent) and the chain HMAC verifies green via the existing `audit chain verify` admin endpoint.
9. **Arch lint + lint.** `make build && make test && make vet && make arch` green on control-plane; `pnpm typecheck && pnpm build` green on web; `cd services/validator && go build ./... && go test ./...` green (unchanged from Phase 4).

## 12. Stage breakdown

| Stage | Title | Pattern |
|---|---|---|
| 0 | Schema additions + RLS + pgvector extension + config + env | sequential (coordinator-owned files) |
| 1 | Domain ports — `Agent`, `AgentInput/Output`, `TokenLedger`, `RetrievalStore`, `EmbeddingProvider`, errors | sequential |
| 2 | `TokenLedger` impl + cron rotation + audit instrumentation + budget pre-flight error wiring | sequential |
| 3 | `RetrievalStore` pgvector impl + chunker + `cmd/seed-pgvector` + provider `Embed()` extension | sequential |
| 4 | LLM adapter extensions — prompt caching on OpenAI, dual-model routing, cost calc, `EmbeddingProvider` factory composition | sequential |
| 5 | 5 L1 agent implementations | **Pattern C — 5 parallel shards** (one backend-engineer per agent) |
| 6 | Wire L1 agents into `activities.go` — replace stub bodies; bump `llmActivityOpts.MaximumAttempts` | sequential |
| 7 | Eval harness — `cmd/eval` + `usecase/eval_runner` + `/v1/eval/*` HTTP routes + `eval_runs/eval_transcripts` repos | sequential |
| 8 | Console `/console/eval` surface — matrix view, transcript pane, Monaco diff, tokens pill on timeline | sequential |
| 9 | E2E + DoD + mark phase complete | sequential |

Stage 5 is the canonical **Pattern C** instance — five backend-engineers in one dispatch message, each working under `internal/adapter/agents/<name>/` with a clean dependency boundary (each shard touches only its own subpackage + shared types in `internal/adapter/agents/llm.go`). The coordinator merges by running `make build && make test && make vet && make arch` once after all five finish.

## 13. Risks / open items

* **OpenAI prompt-caching shape can drift.** OpenAI shipped the `cache_control` field as part of the `messages[].content[]` array shape; the cached-tokens field arrived as `usage.prompt_tokens_details.cached_tokens` in late 2024. If OpenAI later removes the API we lose the cost optimization but not correctness — the adapter falls back to plain string content blocks when `CacheSystem=false`. We test both branches; the cached-tokens parser tolerates the field being absent.
* **Ollama embedding fallback.** If a dev doesn't have `nomic-embed-text` pulled, embeddings route to OpenAI even when `LLM_PROVIDER=ollama` — meaning the "Ollama path" still incurs ~$0.0001 per seed run. Mitigation: README + startup log line `embedding fallback active: openai/text-embedding-3-small`; the eval matrix surfaces this as a yellow caveat dot on the row.
* **pgvector ivfflat index requires data first.** Migrations create the table without the index; the seed script creates the index after insertion. If a dev forgets to seed, retrieval falls back to a sequential scan — slow on a real repo, harmless on the 12-file fixture (well under 1ms). The seed-script command is therefore part of the Stage 9 E2E checklist, not a Stage 0 migration.
* **Token budget bootstrap race.** Two concurrent first-time L1 calls both try to INSERT a budget row. `ON CONFLICT (org_id, period_start) DO NOTHING` makes that safe, but a SELECT-after-INSERT inside the same tx must use the conflict path's `RETURNING` shape — `INSERT … RETURNING * ON CONFLICT … DO UPDATE SET updated_at = now()` is the idiom we lock in. Documented in `token_ledger_repo.go`.
* **Provider override re-instantiation cost.** The eval runner builds two parallel registries per run. Each registry creates its own HTTP client + cost tables. For Phase 5 throughput (1 eval run at a time, ~3 minutes) this is fine; if Phase 6 wires multi-incident eval batches we'll pool the registries.
* **Schema-mismatch retry exhaustion.** Two retries means a worst case of 3 LLM calls per agent per run. With 5 agents that's 15 calls. On OpenAI at peak that's still under $0.50 with caching. On Ollama it's slower (~3× the run time) — the 5-minute Ollama acceptance bar accommodates one schema retry per agent. Watched at Stage 5 verification.
* **Cross-org embedding accidental share.** `code_embeddings.org_id` is the only retrieval filter. If a dev seeds two orgs against the same `repo_sha` (e.g. both org A and org B run the seed against the fixture) we end up with two separate embedding sets — fine, just storage. The risk would be a misindexed query forgetting the `org_id` filter; the repo's `TopK` is the only caller and it always passes the filter. Tested explicitly with a two-org integration test in Stage 3.
* **`pgvector` driver version lock.** `github.com/pgvector/pgvector-go` depends on a compatible pgx version. Phase 5 pins to the version that matches our `pgx/v5`. Documented in `go.mod` go.mod replace? No replace needed at the current versions; we just `go get github.com/pgvector/pgvector-go@latest` and pin.
* **Eval runner double-billing.** Each provider's pass writes to `token_ledger`. Org budget shrinks by both runs. Mitigation: the eval runner records under a synthetic `WorkflowRunID` and adds a `meta.eval = true` audit metadata so admin tools can exclude eval tokens from real-usage reports. The budget gate intentionally still applies — runaway eval would still rack up cost.
* **Ollama 16GB-RAM model fit.** `qwen2.5-coder:14b` Q4 is ~10GB on disk + ~12GB resident; a 16GB machine can run it but barely. The factory automatically downgrades to `llama3.1:8b` if `/api/show qwen2.5-coder:14b` returns 404. The Ollama acceptance bar runs against whichever was actually loaded — the eval matrix renders the model name in the column header so the reviewer knows.
* **Prompt size + cache TTL.** OpenAI's ephemeral cache TTL is ~5 minutes. Pipeline runs are ~90s on OpenAI so back-to-back runs land within TTL; gap-test runs (one run, wait 10 min, run again) will miss cache. Acceptance criterion 1 says "after 5 runs" — implicit assumption is the 5 runs happen back-to-back in dev, which is how the Stage 9 E2E exercises it.

---

## Appendix A: Env vars added

```
# control-plane
TOKEN_BUDGET_TOKENS_IN=1000000              # per org per period
TOKEN_BUDGET_TOKENS_OUT=200000              # per org per period
TOKEN_BUDGET_PERIOD_DAYS=30                 # rolling period length
OPENAI_EMBED_MODEL=text-embedding-3-small   # 1536 dims
OLLAMA_EMBED_MODEL=nomic-embed-text         # 768 dims; falls back to OpenAI when absent
OPENAI_PROMPT_CACHE=1                       # 0 disables cache_control on system blocks
EVAL_ENABLED=1                              # gate for /v1/eval/* (0 in prod)
AGENT_SCHEMA_RETRY_MAX=2                    # per-agent schema-mismatch retry cap
AGENT_MODEL_ARCHITECT_OPENAI=gpt-4o-mini
AGENT_MODEL_BACKEND_OPENAI=gpt-4o
AGENT_MODEL_QA_OPENAI=gpt-4o-mini
AGENT_MODEL_DEVOPS_OPENAI=gpt-4o-mini
AGENT_MODEL_DATAENG_OPENAI=gpt-4o-mini
AGENT_MODEL_ARCHITECT_OLLAMA=llama3.1:8b
AGENT_MODEL_BACKEND_OLLAMA=qwen2.5-coder:14b
AGENT_MODEL_QA_OLLAMA=llama3.1:8b
AGENT_MODEL_DEVOPS_OLLAMA=llama3.1:8b
AGENT_MODEL_DATAENG_OLLAMA=llama3.1:8b
SEED_ORG_ID=                                # consumed only by cmd/seed-pgvector; not read in server bootstrap
```

## Appendix B: HTTP route map (full)

```
POST /v1/eval/incidents                                  [auth, RLS, owner|admin, EVAL_ENABLED=1]
GET  /v1/eval/runs                                       [auth, RLS, any role]
GET  /v1/eval/runs/{id}                                  [auth, RLS, any role]
GET  /v1/admin/token-budget                              [auth, RLS, owner]
POST /v1/admin/token-budget                              [auth, RLS, owner]
GET  /v1/workspaces/{ws}/pipelines/{run_id}              [auth, RLS, any role] (modified — embeds token_rollup)
```

All Phase 4 routes survive unchanged. Workspace-ownership verification continues to gate every workspace-scoped route.

## Appendix C: `AgentOutput.Structured` shapes (per L1 agent)

```json
// architect
{
  "plan_steps": [
    { "id": "p1", "description": "Guard the `safe_div` call with an explicit zero check", "files": ["src/nexis_fixture/api.py"], "rationale": "matches the stack trace" }
  ],
  "affected_files": ["src/nexis_fixture/api.py"],
  "risk_level": "low"
}

// backend
{
  "patch_diff": "diff --git a/src/nexis_fixture/api.py …",
  "files_changed": ["src/nexis_fixture/api.py"],
  "summary": "added a ValueError when b == 0 before division"
}

// qa
{
  "tests": {
    "tests/test_safe_div_regression.py": "import pytest\nfrom nexis_fixture.api import safe_div\n…"
  },
  "covers_files": ["src/nexis_fixture/api.py"]
}

// devops
{
  "argocd_app_yaml": "apiVersion: argoproj.io/v1alpha1\nkind: Application\n…",
  "gh_actions_yaml": "name: ci\non: [pull_request]\n…",
  "rollout_strategy": "canary-10-50-100"
}

// data_engineer
{
  "migrations": [
    {
      "version": "20260513120000",
      "name": "add_safe_div_audit_column",
      "up_sql": "ALTER TABLE …",
      "down_sql": "ALTER TABLE …"
    }
  ],
  "data_backfill": null
}
```

## Appendix D: Audit metadata shape for `agent.invoked`

```json
{
  "agent": "backend",
  "model": "gpt-4o",
  "provider": "openai",
  "tokens_in": 12453,
  "tokens_out": 4231,
  "cached_tokens": 9100,
  "cost_cents": 18.32,
  "duration_ms": 4120,
  "status": "succeeded",
  "workflow_run_id": "8c4f...",
  "schema_retries": 0
}
```

## Appendix E: Eval CLI usage

```
# Run a single eval against the fixture catalog
docker compose run --rm control-plane /app/eval \
    --org-id=$ORG_ID \
    --label=demo-null-pointer \
    --providers=openai,ollama

# Output:
#   eval_run_id=ab12... started_at=…
#   openai:  duration=82s   cost=$0.36  schema_valid=5/5
#   ollama:  duration=246s  cost=$0.00  schema_valid=4/5  (qa: schema_mismatch retried 1x)
#   diff summary: backend patch differs (lines_changed: openai=4, ollama=6); architect plan_steps agree
#   view at: http://localhost:3000/console/eval/ab12...
```

Exit codes: `0` clean, `2` schema-mismatch on any provider, `3` budget exceeded, `4` infrastructure failure.
