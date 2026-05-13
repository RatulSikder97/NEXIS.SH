DROP POLICY IF EXISTS tenant_isolation ON slack_notifications;
DROP POLICY IF EXISTS tenant_isolation ON approval_decisions;
ALTER TABLE slack_notifications  DISABLE ROW LEVEL SECURITY;
ALTER TABLE approval_decisions   DISABLE ROW LEVEL SECURITY;
