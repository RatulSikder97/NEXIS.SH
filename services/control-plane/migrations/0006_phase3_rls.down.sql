DROP POLICY IF EXISTS tenant_isolation ON integrations;
DROP POLICY IF EXISTS tenant_isolation ON incidents_raw;
DROP POLICY IF EXISTS tenant_isolation ON org_invites;
ALTER TABLE integrations  DISABLE ROW LEVEL SECURITY;
ALTER TABLE incidents_raw DISABLE ROW LEVEL SECURITY;
ALTER TABLE org_invites   DISABLE ROW LEVEL SECURITY;
