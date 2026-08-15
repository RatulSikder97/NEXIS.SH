DROP POLICY IF EXISTS gitops_audit_select ON audit_log;

REVOKE SELECT (row_hash, org_id, created_at) ON audit_log FROM nexis_gitops;
