-- Reverse of 0014_phase5_eval.up.sql.
DROP INDEX IF EXISTS eval_transcripts_run_started_idx;

ALTER TABLE eval_transcripts
    ALTER COLUMN system_prompt    SET NOT NULL,
    ALTER COLUMN user_prompt      SET NOT NULL,
    ALTER COLUMN assistant_output SET NOT NULL;

ALTER TABLE eval_transcripts
    DROP COLUMN IF EXISTS finished_at,
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS success,
    DROP COLUMN IF EXISTS output_json,
    DROP COLUMN IF EXISTS input_json;

ALTER TABLE eval_runs
    DROP COLUMN IF EXISTS duration_ollama_ms,
    DROP COLUMN IF EXISTS duration_openai_ms,
    DROP COLUMN IF EXISTS ollama_status,
    DROP COLUMN IF EXISTS openai_status;
