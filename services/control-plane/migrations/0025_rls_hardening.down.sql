-- 0025_rls_hardening.down.sql — revert the WITH CHECK + FORCE hardening to
-- the pre-0025 USING-only / no-FORCE shape.
--
-- Reverting cleanly is non-destructive at the row level: we drop the rebound
-- canonical `tenant_isolation` policies and recreate the USING-only forms
-- that lived in 0003/0004/0006/0008/0010/0013/0017/0019/0020/0023/0024 so an
-- operator who downgrades the binary doesn't lose row visibility filtering.
--
-- We DO NOT remove FORCE on `integrations` — that was already set by
-- 0018_phase6_gitops_role and is unrelated to this migration. Same for any
-- baseline ENABLE — the earlier migrations are the source of truth there.
--
-- Same idempotency rules as the up migration: every block is guarded by an
-- existence check so a partial schema doesn't abort.

-- ----------------------------------------------------------------------------
-- workspaces
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='workspaces' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE workspaces NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON workspaces';
    EXECUTE 'CREATE POLICY tenant_isolation ON workspaces
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- payment_methods
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='payment_methods' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE payment_methods NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON payment_methods';
    EXECUTE 'CREATE POLICY tenant_isolation ON payment_methods
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- invoices
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='invoices' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE invoices NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON invoices';
    EXECUTE 'CREATE POLICY tenant_isolation ON invoices
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- usage_records
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='usage_records' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE usage_records NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON usage_records';
    EXECUTE 'CREATE POLICY tenant_isolation ON usage_records
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- workflow_runs
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='workflow_runs' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE workflow_runs NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON workflow_runs';
    EXECUTE 'CREATE POLICY tenant_isolation ON workflow_runs
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- activity_events
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='activity_events' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE activity_events NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON activity_events';
    EXECUTE 'CREATE POLICY tenant_isolation ON activity_events
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- incidents_raw
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='incidents_raw' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE incidents_raw NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON incidents_raw';
    EXECUTE 'CREATE POLICY tenant_isolation ON incidents_raw
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- code_embeddings
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='code_embeddings' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE code_embeddings NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON code_embeddings';
    EXECUTE 'CREATE POLICY tenant_isolation ON code_embeddings
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- token_ledger
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='token_ledger' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE token_ledger NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON token_ledger';
    EXECUTE 'CREATE POLICY tenant_isolation ON token_ledger
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- approvals / approval_decisions
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='approvals' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE approvals NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON approvals';
    EXECUTE 'CREATE POLICY tenant_isolation ON approvals
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='approval_decisions' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE approval_decisions NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON approval_decisions';
    EXECUTE 'CREATE POLICY tenant_isolation ON approval_decisions
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- eval_runs
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='eval_runs' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE eval_runs NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON eval_runs';
    EXECUTE 'CREATE POLICY tenant_isolation ON eval_runs
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- webhook_deliveries — restore the original `webhook_deliveries_tenant` name.
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='webhook_deliveries' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE webhook_deliveries NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation          ON webhook_deliveries';
    EXECUTE 'DROP POLICY IF EXISTS webhook_deliveries_tenant ON webhook_deliveries';
    EXECUTE 'CREATE POLICY webhook_deliveries_tenant ON webhook_deliveries
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- projects — restore the original `projects_tenant` name.
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='projects' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE projects NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON projects';
    EXECUTE 'DROP POLICY IF EXISTS projects_tenant  ON projects';
    EXECUTE 'CREATE POLICY projects_tenant ON projects
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- nasa_tlx_responses
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='nasa_tlx_responses' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE nasa_tlx_responses NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON nasa_tlx_responses';
    EXECUTE 'CREATE POLICY tenant_isolation ON nasa_tlx_responses
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- org_members
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='org_members' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE org_members NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON org_members';
    EXECUTE 'CREATE POLICY tenant_isolation ON org_members
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- audit_log
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='audit_log' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE audit_log NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON audit_log';
    EXECUTE 'CREATE POLICY tenant_isolation ON audit_log
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- api_keys
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='api_keys' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE api_keys NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON api_keys';
    EXECUTE 'CREATE POLICY tenant_isolation ON api_keys
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- integrations — DO NOT remove FORCE here; 0018_phase6_gitops_role added it
-- for the gitops role and that contract is independent of this migration.
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='integrations' AND column_name='org_id') THEN
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON integrations';
    EXECUTE 'CREATE POLICY tenant_isolation ON integrations
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- Bonus surface (token_budgets, eval_transcripts, entitlements, slack_notifications)
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='token_budgets' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE token_budgets NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON token_budgets';
    EXECUTE 'CREATE POLICY tenant_isolation ON token_budgets
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='eval_transcripts' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE eval_transcripts NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON eval_transcripts';
    EXECUTE 'CREATE POLICY tenant_isolation ON eval_transcripts
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='entitlements' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE entitlements NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON entitlements';
    EXECUTE 'CREATE POLICY tenant_isolation ON entitlements
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='slack_notifications' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE slack_notifications NO FORCE ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON slack_notifications';
    EXECUTE 'CREATE POLICY tenant_isolation ON slack_notifications
               USING (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;
