-- 0022_incidents_raw_source_widen.up.sql — widen incidents_raw.source CHECK so
-- the datadog + pagerduty adapters can persist rows via IncidentSink.Insert.
-- The original constraint from 0005 limited source to ('sentry','github','otel');
-- multi-source Sentinel needs ('sentry','github','otel','datadog','pagerduty').
--
-- Idempotent against the original constraint name (incidents_raw_source_check)
-- AND any previous re-add via this migration.
ALTER TABLE incidents_raw
  DROP CONSTRAINT IF EXISTS incidents_raw_source_check;

ALTER TABLE incidents_raw
  ADD CONSTRAINT incidents_raw_source_check
  CHECK (source IN ('sentry','github','otel','datadog','pagerduty'));
