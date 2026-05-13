-- Phase 6 — approval_decisions + slack_notifications ledger tables.
--
-- approval_decisions holds one row per workflow run that hit the ApprovalGate
-- step. The (workflow_run_id) UNIQUE makes the gate idempotent: signal
-- replays + retries do not create duplicate decisions. severity drives the
-- timeout race in the workflow (low=auto-approved, medium=2-min timer race,
-- high=block until signal).
--
-- slack_notifications is the ledger row the SlackNotifier writes after every
-- webhook POST. Useful for ops to confirm a delivery actually fired and to
-- correlate a stalled approval with a missing Slack ping.

CREATE TABLE approval_decisions (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id),
  workspace_id    uuid NOT NULL REFERENCES workspaces(id),
  workflow_run_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
  severity        text NOT NULL CHECK (severity IN ('low','medium','high')),
  decision        text NOT NULL CHECK (decision IN ('pending','approved','rejected','auto_approved','timeout_rejected')),
  decided_by      uuid REFERENCES users(id),
  decided_at      timestamptz,
  notes           text,
  scenario        text,
  risk_score      numeric(5, 2),
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workflow_run_id)
);
CREATE INDEX approval_decisions_pending_idx ON approval_decisions (org_id, workspace_id, decision, created_at DESC);

CREATE TABLE slack_notifications (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id),
  workflow_run_id uuid REFERENCES workflow_runs(id) ON DELETE SET NULL,
  channel         text,
  kind            text NOT NULL,
  status          text NOT NULL CHECK (status IN ('queued','sent','failed')),
  http_status     int,
  attempted_at    timestamptz NOT NULL DEFAULT now(),
  error           text
);
CREATE INDEX slack_notifications_org_run_idx ON slack_notifications (org_id, workflow_run_id);
