-- 0022_incidents_raw_source_widen.down.sql — revert to the original
-- ('sentry','github','otel') set. NOTE: rows with source IN ('datadog',
-- 'pagerduty') will block the constraint re-add, so downgrade only after the
-- operator confirms no such rows exist (or DELETEs them).
ALTER TABLE incidents_raw
  DROP CONSTRAINT IF EXISTS incidents_raw_source_check;

ALTER TABLE incidents_raw
  ADD CONSTRAINT incidents_raw_source_check
  CHECK (source IN ('sentry','github','otel'));
