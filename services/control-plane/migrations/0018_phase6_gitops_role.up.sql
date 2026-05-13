-- Phase 6 — narrow nexis_gitops role for the gitops service.
--
-- The gitops service mints its own GitHub App installation tokens, so it only
-- needs (a) SELECT on integrations to read the installation_id keyed by
-- (org_id, provider='github'), and (b) INSERT on audit_log to record
-- gitops.pr_opened rows. RLS would block (a) without a per-call GUC bind,
-- so we register two FORCE-RLS policies that allow nexis_gitops to read
-- + insert without an app.current_org_id setting.

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nexis_gitops') THEN
    CREATE ROLE nexis_gitops LOGIN PASSWORD 'nexis_dev_password';
  END IF;
END
$$;
GRANT USAGE  ON SCHEMA public TO nexis_gitops;
GRANT SELECT ON integrations TO nexis_gitops;
GRANT INSERT ON audit_log    TO nexis_gitops;

ALTER TABLE integrations FORCE ROW LEVEL SECURITY;
CREATE POLICY gitops_read_all ON integrations
  FOR SELECT TO nexis_gitops USING (true);
CREATE POLICY gitops_audit_insert ON audit_log
  FOR INSERT TO nexis_gitops WITH CHECK (true);
