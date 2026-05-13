-- 0024_projects.down.sql

ALTER TABLE webhook_deliveries DROP COLUMN IF EXISTS project_id;
ALTER TABLE approvals          DROP COLUMN IF EXISTS project_id;
ALTER TABLE incidents_raw      DROP COLUMN IF EXISTS project_id;
ALTER TABLE workflow_runs      DROP COLUMN IF EXISTS project_id;

DROP TRIGGER IF EXISTS trg_projects_updated_at ON projects;
DROP FUNCTION IF EXISTS projects_set_updated_at();
DROP TABLE IF EXISTS projects CASCADE;
