-- Revert Phase 2 RLS policies + disable RLS.

DROP POLICY IF EXISTS tenant_isolation ON org_members;
DROP POLICY IF EXISTS tenant_isolation ON sessions;
DROP POLICY IF EXISTS user_isolation ON magic_tokens;
DROP POLICY IF EXISTS tenant_isolation ON api_keys;
DROP POLICY IF EXISTS tenant_isolation ON audit_log;

ALTER TABLE org_members  DISABLE ROW LEVEL SECURITY;
ALTER TABLE sessions     DISABLE ROW LEVEL SECURITY;
ALTER TABLE magic_tokens DISABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys     DISABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log    DISABLE ROW LEVEL SECURITY;
