-- Let nexis_gitops read the audit hash-chain tip it has to extend.
--
-- 0018 granted INSERT on audit_log but not SELECT, while AppendPROpened runs
--   SELECT row_hash FROM audit_log WHERE org_id=$1 ORDER BY created_at DESC LIMIT 1
-- first, to chain the new row onto the previous hash. That read failed with
-- "permission denied for table audit_log" (SQLSTATE 42501), so every
-- gitops.pr_opened append errored — after the pull request had already been
-- opened on GitHub. The workflow then reported GitOps.Deploy as failed for a
-- step that had actually succeeded.
--
-- The grant is column-scoped rather than table-wide: the chain read needs
-- exactly row_hash / org_id / created_at, and the audit log's payload stays
-- unreadable to a service whose only job is appending to it. The RLS policy
-- mirrors gitops_read_all on integrations — nexis_gitops has no
-- app.current_org_id GUC bound, so it needs an explicit policy to see rows.

GRANT SELECT (row_hash, org_id, created_at) ON audit_log TO nexis_gitops;

CREATE POLICY gitops_audit_select ON audit_log
  FOR SELECT TO nexis_gitops USING (true);
