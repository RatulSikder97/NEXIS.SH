-- Revert the NULLIF-wrapped RLS policies back to the literal cast form from
-- 0003_rls.up.sql. Note: this reintroduces the empty-string cast hazard;
-- only useful for `migrate down` paths.

DROP POLICY IF EXISTS tenant_isolation ON org_members;
CREATE POLICY tenant_isolation ON org_members
  USING (org_id = current_setting('app.current_org_id', true)::uuid);

DROP POLICY IF EXISTS tenant_isolation ON sessions;
CREATE POLICY tenant_isolation ON sessions
  USING (org_id = current_setting('app.current_org_id', true)::uuid);

DROP POLICY IF EXISTS user_isolation ON magic_tokens;
CREATE POLICY user_isolation ON magic_tokens
  USING (user_id IN (
    SELECT user_id FROM org_members
    WHERE org_id = current_setting('app.current_org_id', true)::uuid
  ));

DROP POLICY IF EXISTS tenant_isolation ON api_keys;
CREATE POLICY tenant_isolation ON api_keys
  USING (org_id = current_setting('app.current_org_id', true)::uuid);

DROP POLICY IF EXISTS tenant_isolation ON audit_log;
CREATE POLICY tenant_isolation ON audit_log
  USING (org_id = current_setting('app.current_org_id', true)::uuid);
