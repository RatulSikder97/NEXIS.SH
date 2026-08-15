-- Rows from the newly-allowed sources must go before the CHECK narrows, or
-- the constraint fails to validate.
DELETE FROM incidents_raw WHERE source IN ('webhook','deploy_engine');

ALTER TABLE incidents_raw
  DROP CONSTRAINT IF EXISTS incidents_raw_source_check;

ALTER TABLE incidents_raw
  ADD CONSTRAINT incidents_raw_source_check
  CHECK (source IN ('sentry','github','otel','datadog','pagerduty','schema_drift','qa_loop'));
