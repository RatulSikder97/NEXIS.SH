-- Phase 5 — RLS + grants for the 5 new tables.
ALTER TABLE token_budgets     ENABLE ROW LEVEL SECURITY;
ALTER TABLE token_ledger      ENABLE ROW LEVEL SECURITY;
ALTER TABLE code_embeddings   ENABLE ROW LEVEL SECURITY;
ALTER TABLE eval_runs         ENABLE ROW LEVEL SECURITY;
ALTER TABLE eval_transcripts  ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON token_budgets
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON token_ledger
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON code_embeddings
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON eval_runs
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON eval_transcripts
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON token_budgets    TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON token_ledger     TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON code_embeddings  TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON eval_runs        TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON eval_transcripts TO nexis_app;
