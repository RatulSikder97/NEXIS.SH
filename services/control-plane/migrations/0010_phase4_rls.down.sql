DROP POLICY IF EXISTS tenant_isolation ON activity_events;
DROP POLICY IF EXISTS tenant_isolation ON workflow_runs;
ALTER TABLE activity_events DISABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_runs   DISABLE ROW LEVEL SECURITY;
