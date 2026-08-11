-- 0027_phase9_schema_baseline.up.sql — proactive schema drift detection.
--
-- schema_baseline_snapshots stores the last blessed shape of the control
-- plane's own tracked tables: one row per (table, column) with the
-- information_schema data_type at capture time. The Data Engineer drift
-- detector (internal/adapter/agents/data_engineer/drift.go) re-introspects
-- information_schema.columns on a cron tick and diffs the live shape against
-- these rows — any divergence (column added / removed / type changed) lands a
-- level='fatal' incidents_raw row that rides the existing Sentinel
-- detection → RecoveryPipeline path.
--
-- The table is deliberately NOT org-scoped: it describes the control plane's
-- own database schema, which is system-global — there is no tenant dimension
-- to isolate, so no RLS either (mirrors schema_migrations). nexis_app gets
-- SELECT + INSERT + DELETE because CaptureBaseline re-snapshots via
-- DELETE-then-INSERT and must work on either pool (the db.FromCtx pattern).
--
-- The (table_name, column_name) unique index doubles as the natural key the
-- diff joins on.

CREATE TABLE schema_baseline_snapshots (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  table_name   text NOT NULL,
  column_name  text NOT NULL,
  data_type    text NOT NULL,
  captured_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (table_name, column_name)
);

GRANT SELECT, INSERT, DELETE ON schema_baseline_snapshots TO nexis_app;

-- Widen incidents_raw.source so the drift checker can persist its synthetic
-- incident rows through the same IncidentSink path the webhook adapters use.
-- 'schema_drift' joins the polled set in IncidentsRepo.PollFatalSince so the
-- Sentinel detector picks real drift up exactly like a Sentry fatal.
ALTER TABLE incidents_raw
  DROP CONSTRAINT IF EXISTS incidents_raw_source_check;

ALTER TABLE incidents_raw
  ADD CONSTRAINT incidents_raw_source_check
  CHECK (source IN ('sentry','github','otel','datadog','pagerduty','schema_drift'));
