-- Phase 5 — token budgets + ledger + eval runs/transcripts (no pgvector here).
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
