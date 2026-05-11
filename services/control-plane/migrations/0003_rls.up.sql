-- Phase 2 row-level security: enable RLS + tenant isolation policies on all
-- tenant-scoped tables. Policies key off the per-transaction GUC
-- 'app.current_org_id' which is set by the auth middleware in app code.
-- organizations + users are intentionally NOT RLS-protected: they have to be
-- readable before a session exists (signup, login, org bootstrap).

ALTER TABLE org_members  ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions     ENABLE ROW LEVEL SECURITY;
ALTER TABLE magic_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys     ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log    ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON org_members
  USING (org_id = current_setting('app.current_org_id', true)::uuid);

CREATE POLICY tenant_isolation ON sessions
  USING (org_id = current_setting('app.current_org_id', true)::uuid);

-- magic_tokens has no org_id (issued pre-tenant) — derive isolation via
-- org_members so a token is visible only inside the issuing org's session.
CREATE POLICY user_isolation ON magic_tokens
  USING (user_id IN (
    SELECT user_id FROM org_members
    WHERE org_id = current_setting('app.current_org_id', true)::uuid
  ));

CREATE POLICY tenant_isolation ON api_keys
  USING (org_id = current_setting('app.current_org_id', true)::uuid);

CREATE POLICY tenant_isolation ON audit_log
  USING (org_id = current_setting('app.current_org_id', true)::uuid);
