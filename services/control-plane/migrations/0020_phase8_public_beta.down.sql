-- Phase 8 — rollback in reverse order so dependent objects drop cleanly.

DROP TRIGGER  IF EXISTS pipeline_runs_increment_recoveries ON workflow_runs;
DROP FUNCTION IF EXISTS tg_pipeline_runs_increment_recoveries();

ALTER TABLE organizations DROP COLUMN IF EXISTS successful_recoveries_count;

DROP POLICY IF EXISTS tenant_isolation ON nasa_tlx_responses;
ALTER TABLE nasa_tlx_responses DISABLE ROW LEVEL SECURITY;

DROP INDEX IF EXISTS nasa_tlx_responses_org_idx;
DROP TABLE IF EXISTS nasa_tlx_responses;

DROP INDEX IF EXISTS invite_codes_expires_at_idx;
DROP INDEX IF EXISTS invite_codes_created_by_idx;
DROP TABLE IF EXISTS invite_codes;
