ALTER TABLE integrations  ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents_raw ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_invites   ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON integrations
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON incidents_raw
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON org_invites
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON integrations  TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON incidents_raw TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON org_invites   TO nexis_app;
