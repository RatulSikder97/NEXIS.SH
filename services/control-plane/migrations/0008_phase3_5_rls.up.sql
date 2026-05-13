-- 0008_phase3_5_rls.up.sql — RLS policies + GRANTs for the four Phase 3.5
-- tables. Mirrors the NULLIF pattern established in 0004/0006 so an empty
-- app.current_org_id GUC produces a NULL uuid (no rows visible) instead of
-- an InvalidTextRepresentation error.

ALTER TABLE workspaces      ENABLE ROW LEVEL SECURITY;
ALTER TABLE payment_methods ENABLE ROW LEVEL SECURITY;
ALTER TABLE invoices        ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage_records   ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON workspaces
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON payment_methods
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON invoices
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON usage_records
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON workspaces      TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON payment_methods TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON invoices        TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON usage_records   TO nexis_app;
