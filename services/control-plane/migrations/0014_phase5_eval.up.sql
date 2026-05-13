-- Phase 5 Stage 7 — extend eval_runs + eval_transcripts to match the eval
-- harness contract (per-provider status, per-provider duration, structured
-- input/output JSON, started/finished timestamps, schema_valid retained for
-- legacy fixture-mismatch detection in the CLI exit-code classifier).
--
-- The original 0011 migration already created the tables with the bulk of
-- the columns; this migration is additive so re-running 0011 + 0014 in
-- order on a fresh DB lands the final shape, and applying 0014 on an
-- existing DB (with a partly-populated 0011 table) does not lose data.

ALTER TABLE eval_runs
    ADD COLUMN IF NOT EXISTS openai_status      text,
    ADD COLUMN IF NOT EXISTS ollama_status      text,
    ADD COLUMN IF NOT EXISTS duration_openai_ms bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS duration_ollama_ms bigint NOT NULL DEFAULT 0;

-- 0011 used CHECK on the legacy single status column; the new per-provider
-- columns accept the same domain (queued|running|succeeded|failed|error).
-- We rely on application validation rather than a CHECK constraint so
-- partial state during a run (one provider succeeded, the other still
-- running) is representable without intermediate ALTER TABLE churn.

ALTER TABLE eval_transcripts
    ADD COLUMN IF NOT EXISTS input_json   jsonb,
    ADD COLUMN IF NOT EXISTS output_json  jsonb,
    ADD COLUMN IF NOT EXISTS success      boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS started_at   timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS finished_at  timestamptz;

-- The 0011 schema declared system_prompt/user_prompt/assistant_output as
-- NOT NULL; the runner now persists JSON payloads instead, so relax the
-- NOT NULL constraint on those legacy columns to keep both shapes valid.
ALTER TABLE eval_transcripts
    ALTER COLUMN system_prompt    DROP NOT NULL,
    ALTER COLUMN user_prompt      DROP NOT NULL,
    ALTER COLUMN assistant_output DROP NOT NULL;

CREATE INDEX IF NOT EXISTS eval_transcripts_run_started_idx
    ON eval_transcripts (eval_run_id, started_at);
