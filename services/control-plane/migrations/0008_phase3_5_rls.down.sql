-- 0008_phase3_5_rls.down.sql

DROP POLICY IF EXISTS tenant_isolation ON usage_records;
DROP POLICY IF EXISTS tenant_isolation ON invoices;
DROP POLICY IF EXISTS tenant_isolation ON payment_methods;
DROP POLICY IF EXISTS tenant_isolation ON workspaces;

ALTER TABLE usage_records   DISABLE ROW LEVEL SECURITY;
ALTER TABLE invoices        DISABLE ROW LEVEL SECURITY;
ALTER TABLE payment_methods DISABLE ROW LEVEL SECURITY;
ALTER TABLE workspaces      DISABLE ROW LEVEL SECURITY;

REVOKE SELECT, INSERT, UPDATE, DELETE ON usage_records   FROM nexis_app;
REVOKE SELECT, INSERT, UPDATE, DELETE ON invoices        FROM nexis_app;
REVOKE SELECT, INSERT, UPDATE, DELETE ON payment_methods FROM nexis_app;
REVOKE SELECT, INSERT, UPDATE, DELETE ON workspaces      FROM nexis_app;
