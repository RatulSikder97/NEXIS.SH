-- Phase 6 — RLS on the two new Phase 6 tables. tenant_isolation uses the
-- same NULLIF(current_setting('app.current_org_id', true), '')::uuid pattern
-- as Phase 4/5 so system jobs (admin pool) can bypass when the GUC is unset.

ALTER TABLE approval_decisions   ENABLE ROW LEVEL SECURITY;
ALTER TABLE slack_notifications  ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON approval_decisions
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON slack_notifications
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON approval_decisions  TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON slack_notifications TO nexis_app;
