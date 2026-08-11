-- 0033_deployments.up.sql — Preview deployments (deploy-engine integration).
--
-- Each row records one POST /v1/deploy round-trip against the
-- services/deploy-engine sidecar: control-plane mints the deployment id,
-- calls the engine, and persists whatever came back (running container URL
-- on success; build/container logs + error on failure). control-plane is the
-- system of record — the engine only keeps in-memory state per instance.
--
-- id has no server default on purpose in spirit (control-plane mints it for
-- container/image naming before the engine call), but gen_random_uuid() is
-- kept as a safety net for ad-hoc inserts.

CREATE TABLE deployments (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id        uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  org_id            uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  status            text NOT NULL CHECK (status IN ('building','running','failed','stopped')),
  url               text,
  port              int,
  image_tag         text,
  dockerfile_source text,
  detected_stack    text,
  build_log         text NOT NULL DEFAULT '',
  container_log     text NOT NULL DEFAULT '',
  error             text NOT NULL DEFAULT '',
  started_at        timestamptz,
  finished_at       timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_deployments_project_created ON deployments (project_id, created_at DESC);
CREATE INDEX idx_deployments_org_created     ON deployments (org_id, created_at DESC);

-- updated_at trigger — same shape as trg_projects_updated_at (0024).
CREATE OR REPLACE FUNCTION deployments_set_updated_at() RETURNS trigger AS $$
BEGIN NEW.updated_at = now(); RETURN NEW; END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_deployments_updated_at BEFORE UPDATE ON deployments
  FOR EACH ROW EXECUTE FUNCTION deployments_set_updated_at();

-- Tenant isolation — canonical 0025-style policy (ENABLE + FORCE + WITH CHECK).
ALTER TABLE deployments ENABLE ROW LEVEL SECURITY;
ALTER TABLE deployments FORCE  ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON deployments
  USING      (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid)
  WITH CHECK (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON deployments TO nexis_app;
