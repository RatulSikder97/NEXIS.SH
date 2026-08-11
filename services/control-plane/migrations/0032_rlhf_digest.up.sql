-- 0032_rlhf_digest.up.sql — RLHF feedback pipeline + daily admin digest.
--
-- Part 1 — 'modified' approval decision state. The FYP RLHF loop needs
-- "approve, rejection, and modification history"; 'modified' is the state
-- where an engineer edited the proposed patch before approving it. The
-- original 0015 inline CHECK predates the state, so widen it here (same
-- pattern 0026 used for activity_events.status).
--
-- Part 2 — feedback_examples. One row per terminal human/system decision on
-- a recovery patch: the scenario, the agent's original diff, the decision,
-- and (for 'modified') the engineer's edited diff. exported_at implements
-- the JSONL export cursor: GET /v1/admin/rlhf-export streams rows WHERE
-- exported_at IS NULL and stamps them in the same statement. The _legacy
-- scaffold had a feedback_examples sketch (reward-scored, agent-keyed) that
-- never shipped; this is the fresh design matched to the decision flow the
-- Approval Gate actually implements.
--
-- Part 3 — digest_reports. One row per org per day, produced by the
-- DailyDigest cron: incidents detected, repairs applied/approved/rejected,
-- MTTR, and per-agent activity counts over the trailing 24h. metrics is
-- jsonb so the report shape can grow without churning this schema.

ALTER TABLE approval_decisions
  DROP CONSTRAINT IF EXISTS approval_decisions_decision_check;
ALTER TABLE approval_decisions
  ADD CONSTRAINT approval_decisions_decision_check
  CHECK (decision IN ('pending','approved','rejected','auto_approved','timeout_rejected','modified'));

CREATE TABLE feedback_examples (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  -- text, not uuid: PipelineInput.IncidentID carries 'manual' / 'demo' for
  -- operator-triggered runs alongside real incidents_raw uuids.
  incident_id     text NOT NULL DEFAULT '',
  workflow_run_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
  scenario        text NOT NULL DEFAULT '',
  patch_diff      text NOT NULL DEFAULT '',
  decision        text NOT NULL CHECK (decision IN ('approved','rejected','modified')),
  modified_diff   text,
  decided_by      uuid REFERENCES users(id) ON DELETE SET NULL,
  decided_at      timestamptz NOT NULL DEFAULT now(),
  exported_at     timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now()
);

-- Export scan: unexported rows per org, oldest-first.
CREATE INDEX feedback_examples_unexported_idx
  ON feedback_examples (org_id, created_at ASC)
  WHERE exported_at IS NULL;

ALTER TABLE feedback_examples ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_examples FORCE  ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON feedback_examples
  USING      (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid)
  WITH CHECK (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON feedback_examples TO nexis_app;

CREATE TABLE digest_reports (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  period_start timestamptz NOT NULL,
  period_end   timestamptz NOT NULL,
  metrics      jsonb NOT NULL,
  insights     jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at   timestamptz NOT NULL DEFAULT now()
);

-- At most one digest per org per UTC day — restarts and overlapping ticks
-- upsert against this via ON CONFLICT DO NOTHING.
CREATE UNIQUE INDEX digest_reports_org_day_idx
  ON digest_reports (org_id, ((period_end AT TIME ZONE 'UTC')::date));

ALTER TABLE digest_reports ENABLE ROW LEVEL SECURITY;
ALTER TABLE digest_reports FORCE  ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON digest_reports
  USING      (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid)
  WITH CHECK (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON digest_reports TO nexis_app;
