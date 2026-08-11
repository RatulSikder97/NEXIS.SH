DROP TABLE IF EXISTS qa_test_suites;

ALTER TABLE incidents_raw
  DROP CONSTRAINT IF EXISTS incidents_raw_source_check;

ALTER TABLE incidents_raw
  ADD CONSTRAINT incidents_raw_source_check
  CHECK (source IN ('sentry','github','otel','datadog','pagerduty','schema_drift'));
