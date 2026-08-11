-- 0031_qa_test_suites.up.sql — QA continuous test loop (FYP: "QA Agent ...
-- runs continuously — not just on demand").
--
-- qa_test_suites persists the pytest files the QA agent generates during a
-- recovery run so a background cron (usecase.QALoop) can re-run them against
-- the validator sandbox long after the workflow that created them completed.
-- One row per workflow run (UNIQUE workflow_run_id) — regeneration inside a
-- retried run upserts instead of duplicating.
--
-- last_status is the loop's memory: 'passed' → 'failed' is a REGRESSION
-- (previously-green tests broke against the current code) and fires an
-- incident + audit entry; 'unknown' → 'failed' is just a baseline that never
-- passed and only updates the row.

CREATE TABLE qa_test_suites (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id           uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  workspace_id     uuid REFERENCES workspaces(id) ON DELETE SET NULL,
  project_id       uuid REFERENCES projects(id) ON DELETE SET NULL,
  workflow_run_id  uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
  repo_sha         text NOT NULL DEFAULT '',
  -- filename → pytest source, exactly the QA agent's structured "tests" map.
  tests            jsonb NOT NULL,
  covers_files     jsonb NOT NULL DEFAULT '[]'::jsonb,
  status           text NOT NULL DEFAULT 'active'  CHECK (status IN ('active', 'disabled')),
  last_status      text NOT NULL DEFAULT 'unknown' CHECK (last_status IN ('unknown', 'passed', 'failed')),
  last_fail_count  int  NOT NULL DEFAULT 0,
  last_run_at      timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workflow_run_id)
);

-- The cron lists active suites oldest-run-first so every suite eventually
-- gets a turn even when MaxSuitesPerTick caps the batch.
CREATE INDEX qa_test_suites_active_idx
  ON qa_test_suites (status, last_run_at ASC NULLS FIRST);

-- Tenant isolation — canonical 0025-style policy (ENABLE + FORCE + WITH CHECK).
ALTER TABLE qa_test_suites ENABLE ROW LEVEL SECURITY;
ALTER TABLE qa_test_suites FORCE  ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON qa_test_suites
  USING      (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid)
  WITH CHECK (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON qa_test_suites TO nexis_app;

-- Widen incidents_raw.source so the QA loop can report a regression through
-- the same IncidentSink path the webhook adapters + schema-drift checker use.
-- 'qa_loop' joins the polled set in IncidentsRepo.PollFatalSince so a
-- regression triggers a recovery pipeline exactly like a Sentry fatal.
ALTER TABLE incidents_raw
  DROP CONSTRAINT IF EXISTS incidents_raw_source_check;

ALTER TABLE incidents_raw
  ADD CONSTRAINT incidents_raw_source_check
  CHECK (source IN ('sentry','github','otel','datadog','pagerduty','schema_drift','qa_loop'));
