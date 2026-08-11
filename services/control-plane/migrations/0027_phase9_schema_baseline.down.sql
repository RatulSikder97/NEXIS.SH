-- Revert the schema-baseline feature. Synthetic drift incidents would violate
-- the narrowed source CHECK, so drop them first — they are derived signals the
-- drift checker regenerates on its next tick, not source-of-truth history, so
-- losing them on a rollback is acceptable.

DELETE FROM incidents_raw WHERE source = 'schema_drift';
ALTER TABLE incidents_raw
  DROP CONSTRAINT IF EXISTS incidents_raw_source_check;
ALTER TABLE incidents_raw
  ADD CONSTRAINT incidents_raw_source_check
  CHECK (source IN ('sentry','github','otel','datadog','pagerduty'));

DROP TABLE IF EXISTS schema_baseline_snapshots;
