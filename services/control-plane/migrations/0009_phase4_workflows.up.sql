-- Phase 4 — workflow_runs + activity_events tables.
--
-- workflow_runs is one row per Temporal-execution-of-a-workflow. The id is
-- generated client-side and reused as the Temporal WorkflowID so log/audit
-- correlation is trivial. temporal_run_id is Temporal's per-attempt id.
--
-- activity_events is the per-step timeline driving the Console SSE stream.
-- seq is monotonic per run; SSE consumers dedupe on (workflow_run_id, seq).

CREATE TABLE workflow_runs (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id            uuid NOT NULL REFERENCES organizations(id),
  workspace_id      uuid NOT NULL REFERENCES workspaces(id),
  workflow_type     text NOT NULL,
  temporal_run_id   text NOT NULL,
  temporal_wf_id    text NOT NULL,
  status            text NOT NULL CHECK (status IN ('queued','running','succeeded','failed','timed_out','cancelled')),
  current_step      text,
  input             jsonb,
  output            jsonb,
  error             text,
  started_at        timestamptz NOT NULL DEFAULT now(),
  completed_at      timestamptz,
  duration_ms       bigint,
  created_by        uuid REFERENCES users(id),
  UNIQUE (org_id, temporal_run_id)
);
CREATE INDEX workflow_runs_org_ws_started_idx ON workflow_runs (org_id, workspace_id, started_at DESC);

CREATE TABLE activity_events (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id            uuid NOT NULL REFERENCES organizations(id),
  workflow_run_id   uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
  seq               int  NOT NULL,
  agent_role        text NOT NULL,
  activity_name     text NOT NULL,
  status            text NOT NULL CHECK (status IN ('started','succeeded','failed','retrying','timed_out')),
  attempt           int  NOT NULL DEFAULT 1,
  message           text,
  payload           jsonb,
  ts                timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workflow_run_id, seq)
);
