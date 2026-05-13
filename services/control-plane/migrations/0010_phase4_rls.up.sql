-- Phase 4 — tenant isolation policies for workflow_runs + activity_events.
--
-- Same NULLIF-current_setting pattern as the rest of the codebase (introduced
-- in migration 0004). nexis_app's GRANTs are explicit so the role can write
-- both tables under the per-request tx.

ALTER TABLE workflow_runs    ENABLE ROW LEVEL SECURITY;
ALTER TABLE activity_events  ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON workflow_runs
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON activity_events
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON workflow_runs   TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON activity_events TO nexis_app;
