-- 0028_phase9_lineage_events.up.sql — OpenLineage RunEvent persistence.
--
-- lineage_events stores one OpenLineage-spec-shaped RunEvent per Data
-- Engineer migration proposal / apply. The full spec JSON lives verbatim in
-- `event` (eventType, eventTime, run{runId}, job{namespace,name}, inputs[],
-- outputs[], producer, schemaURL); the typed columns duplicate the fields the
-- dashboard queries on so lists never have to unpack jsonb.
--
-- event_type is constrained to the OpenLineage RunState enum. run_id is the
-- spec's run.runId — a deterministic UUIDv5 derived from (workflow run, job),
-- distinct from workflow_run_id which FKs our own workflow_runs row so the
-- timeline can join lineage onto the run drill-down (and cascade on delete).
--
-- RLS + grants mirror webhook_deliveries: append-only tenant history —
-- nexis_app gets SELECT + INSERT, no UPDATE/DELETE.

CREATE TABLE lineage_events (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id           uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  workflow_run_id  uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
  event_type       text NOT NULL CHECK (event_type IN ('START','RUNNING','COMPLETE','ABORT','FAIL','OTHER')),
  event_time       timestamptz NOT NULL,
  job_namespace    text NOT NULL,
  job_name         text NOT NULL,
  run_id           uuid NOT NULL,
  event            jsonb NOT NULL,
  created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_lineage_events_org_created ON lineage_events (org_id, created_at DESC);
CREATE INDEX idx_lineage_events_workflow_run ON lineage_events (workflow_run_id);

ALTER TABLE lineage_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY lineage_events_tenant ON lineage_events
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT ON lineage_events TO nexis_app;
