DROP POLICY IF EXISTS gitops_audit_insert ON audit_log;
DROP POLICY IF EXISTS gitops_read_all     ON integrations;
ALTER TABLE integrations NO FORCE ROW LEVEL SECURITY;
REVOKE INSERT ON audit_log    FROM nexis_gitops;
REVOKE SELECT ON integrations FROM nexis_gitops;
REVOKE USAGE  ON SCHEMA public FROM nexis_gitops;
DROP ROLE IF EXISTS nexis_gitops;
