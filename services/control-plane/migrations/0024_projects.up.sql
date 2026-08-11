-- 0024_projects.up.sql — Projects: first-class self-healing targets.
-- A project bundles integration mappings, recovery policy, and SLOs for one
-- service/repo the customer wants NEXIS to auto-heal.

CREATE TABLE projects (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  workspace_id    uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name            text NOT NULL,
  slug            text NOT NULL,
  description     text NOT NULL DEFAULT '',
  environment     text NOT NULL CHECK (environment IN ('dev','staging','prod')),
  owner_user_id   uuid REFERENCES users(id) ON DELETE SET NULL,

  -- Integration selector fields (all optional — each project links to as many as customer connects).
  github_repo                    text,
  github_installation_id         bigint,
  github_default_branch          text DEFAULT 'main',
  sentry_organization_slug       text,
  sentry_project_slug            text,
  argocd_server_url              text,
  argocd_app_name                text,
  argocd_project                 text DEFAULT 'default',
  pagerduty_service_id           text,
  pagerduty_escalation_policy_id text,
  datadog_service_tag            text,
  datadog_env_tag                text,
  slack_channel_id               text,

  -- Recovery policy as JSONB for forward-compat without migrations.
  recovery_policy jsonb NOT NULL DEFAULT '{
    "auto_merge_low_severity": false,
    "auto_merge_medium_severity": false,
    "medium_countdown_seconds": 120,
    "kill_switch_enabled": false,
    "approver_user_ids": [],
    "max_concurrent_recoveries": 1,
    "rollback_on_slo_breach": true
  }'::jsonb,

  -- SLO targets (optional — used by Sentinel for severity scoring).
  slo_availability_target numeric(5,4),
  slo_latency_p95_ms      int,
  slo_error_rate_pct      numeric(5,2),

  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  archived_at timestamptz,
  UNIQUE (workspace_id, slug)
);

CREATE INDEX idx_projects_org_workspace ON projects (org_id, workspace_id) WHERE archived_at IS NULL;
CREATE INDEX idx_projects_github_repo   ON projects (github_repo) WHERE github_repo IS NOT NULL;
CREATE INDEX idx_projects_sentry        ON projects (sentry_organization_slug, sentry_project_slug) WHERE sentry_project_slug IS NOT NULL;
CREATE INDEX idx_projects_pagerduty     ON projects (pagerduty_service_id) WHERE pagerduty_service_id IS NOT NULL;
CREATE INDEX idx_projects_datadog       ON projects (datadog_service_tag) WHERE datadog_service_tag IS NOT NULL;

ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
CREATE POLICY projects_tenant ON projects
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON projects TO nexis_app;

-- Add project_id to downstream tables so recovery + incidents are project-scoped.
ALTER TABLE workflow_runs      ADD COLUMN project_id uuid REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE incidents_raw      ADD COLUMN project_id uuid REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE approval_decisions ADD COLUMN project_id uuid REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE webhook_deliveries ADD COLUMN project_id uuid REFERENCES projects(id) ON DELETE SET NULL;
CREATE INDEX idx_workflow_runs_project   ON workflow_runs (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX idx_incidents_raw_project   ON incidents_raw (project_id) WHERE project_id IS NOT NULL;

-- updated_at trigger.
CREATE OR REPLACE FUNCTION projects_set_updated_at() RETURNS trigger AS $$
BEGIN NEW.updated_at = now(); RETURN NEW; END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_projects_updated_at BEFORE UPDATE ON projects
  FOR EACH ROW EXECUTE FUNCTION projects_set_updated_at();
