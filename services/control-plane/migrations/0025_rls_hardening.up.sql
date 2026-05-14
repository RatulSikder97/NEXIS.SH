-- 0025_rls_hardening.up.sql — close write-side RLS holes (Tech-Lead audit R-2).
--
-- Symptoms in the existing migrations:
--
--   * Phase 2/3/4/5/6 policies are USING-only. A USING clause filters which
--     rows are visible to SELECT/UPDATE/DELETE, but it does NOT constrain new
--     rows produced by INSERT or by an UPDATE that flips org_id. A
--     misbehaving handler that forgets to stamp the tenant on a write can
--     therefore land a row in another tenant — the row exists at the table
--     layer but isn't visible to the source tenant. WITH CHECK closes that.
--
--   * Several tables enable RLS but do not FORCE it, so the table owner
--     (typically the role running migrations) bypasses the policy. nexis_app
--     is not the owner today, so the live read/write surface is filtered —
--     but as soon as any pg_dump/restore round-trip changes ownership or a
--     superuser-borrowed session writes, the policy is silently skipped.
--     FORCE removes that escape hatch.
--
-- This migration is idempotent:
--
--   * `DROP POLICY IF EXISTS … ON …` ahead of every CREATE POLICY so reruns
--     don't error if the same policy already exists at any stage.
--   * Each ALTER TABLE … FORCE ROW LEVEL SECURITY is wrapped in a DO block
--     that checks pg_class.relrowsecurity first and skips when missing tables
--     (e.g. the audit-listed `approvals` table never existed under that name
--     in this schema — the equivalent today is `approval_decisions`).
--   * Tables that do not have an `org_id` column (invite_codes is intentionally
--     system-wide) are skipped per the audit brief.
--
-- The rebound policies all key off:
--
--     NULLIF(current_setting('app.current_org_id', true), '')::uuid
--
-- so admin-pool callers (which do not pin the GUC) continue to bypass via the
-- NULL match — the existing 0004/0008/0010/0013/0017 invariant the runtime
-- relies on. Application-role traffic (nexis_app) sees only its own org.
--
-- Schema invariant: `projects` and `webhook_deliveries` already shipped with
-- the canonical name (`projects_tenant`, `webhook_deliveries_tenant`); we
-- drop and recreate under `tenant_isolation` so the audit surface is uniform.

-- ----------------------------------------------------------------------------
-- 1. Helper: rebind one tenant table's policy.
-- ----------------------------------------------------------------------------
-- Implemented as inline statements (no SQL function) so the migration is one
-- transactional unit. Each table block is guarded by an existence check so a
-- partial schema (missing legacy table) doesn't abort the run.

-- ----------------------------------------------------------------------------
-- workspaces (org_id NOT NULL)
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='workspaces' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE workspaces FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON workspaces';
    EXECUTE 'CREATE POLICY tenant_isolation ON workspaces
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- payment_methods
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='payment_methods' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE payment_methods ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE payment_methods FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON payment_methods';
    EXECUTE 'CREATE POLICY tenant_isolation ON payment_methods
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- invoices
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='invoices' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE invoices ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE invoices FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON invoices';
    EXECUTE 'CREATE POLICY tenant_isolation ON invoices
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- usage_records
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='usage_records' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE usage_records ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE usage_records FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON usage_records';
    EXECUTE 'CREATE POLICY tenant_isolation ON usage_records
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- workflow_runs
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='workflow_runs' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE workflow_runs ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE workflow_runs FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON workflow_runs';
    EXECUTE 'CREATE POLICY tenant_isolation ON workflow_runs
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- activity_events
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='activity_events' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE activity_events ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE activity_events FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON activity_events';
    EXECUTE 'CREATE POLICY tenant_isolation ON activity_events
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- incidents_raw
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='incidents_raw' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE incidents_raw ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE incidents_raw FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON incidents_raw';
    EXECUTE 'CREATE POLICY tenant_isolation ON incidents_raw
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- code_embeddings
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='code_embeddings' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE code_embeddings ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE code_embeddings FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON code_embeddings';
    EXECUTE 'CREATE POLICY tenant_isolation ON code_embeddings
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- token_ledger
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='token_ledger' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE token_ledger ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE token_ledger FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON token_ledger';
    EXECUTE 'CREATE POLICY tenant_isolation ON token_ledger
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- approvals — the audit checklist names this table, but historically the
-- schema shipped the equivalent as `approval_decisions`. Cover both: skip
-- whichever isn't present. The migration is forward-compat if/when a real
-- `approvals` table lands.
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='approvals' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE approvals ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE approvals FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON approvals';
    EXECUTE 'CREATE POLICY tenant_isolation ON approvals
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='approval_decisions' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE approval_decisions ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE approval_decisions FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON approval_decisions';
    EXECUTE 'CREATE POLICY tenant_isolation ON approval_decisions
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- eval_runs
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='eval_runs' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE eval_runs ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE eval_runs FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON eval_runs';
    EXECUTE 'CREATE POLICY tenant_isolation ON eval_runs
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- webhook_deliveries — initial 0023 policy was `webhook_deliveries_tenant`
-- with USING-only. Replace both legacy and canonical names.
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='webhook_deliveries' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE webhook_deliveries FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS webhook_deliveries_tenant ON webhook_deliveries';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation          ON webhook_deliveries';
    EXECUTE 'CREATE POLICY tenant_isolation ON webhook_deliveries
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- projects — initial 0024 policy was `projects_tenant` with USING-only.
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='projects' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE projects ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE projects FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS projects_tenant   ON projects';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation  ON projects';
    EXECUTE 'CREATE POLICY tenant_isolation ON projects
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- nasa_tlx_responses
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='nasa_tlx_responses' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE nasa_tlx_responses ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE nasa_tlx_responses FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON nasa_tlx_responses';
    EXECUTE 'CREATE POLICY tenant_isolation ON nasa_tlx_responses
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- org_members
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='org_members' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE org_members ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE org_members FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON org_members';
    EXECUTE 'CREATE POLICY tenant_isolation ON org_members
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- audit_log — the 0018_phase6_gitops_role migration also added a gitops-only
-- INSERT-WITH-CHECK policy; we keep that side alongside the canonical
-- tenant_isolation. The gitops role uses its own narrow policy and is
-- unaffected by this rebind.
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='audit_log' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE audit_log FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON audit_log';
    EXECUTE 'CREATE POLICY tenant_isolation ON audit_log
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- api_keys
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='api_keys' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE api_keys FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON api_keys';
    EXECUTE 'CREATE POLICY tenant_isolation ON api_keys
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- invite_codes — intentionally skipped: no org_id column (system-wide table).
-- Listed in the audit only as a sanity placeholder; the design is documented
-- in 0020_phase8_public_beta.up.sql and the Phase 8 invite redemption path.
-- ----------------------------------------------------------------------------

-- ----------------------------------------------------------------------------
-- integrations — the 0018_phase6_gitops_role migration already FORCE's RLS
-- on this table and adds a `gitops_read_all` policy for the nexis_gitops
-- service role. Keep that alongside the rebound tenant_isolation policy.
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='integrations' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE integrations ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE integrations FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON integrations';
    EXECUTE 'CREATE POLICY tenant_isolation ON integrations
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;

-- ----------------------------------------------------------------------------
-- Bonus surface (not in the audit list but recovered from the migration tree
-- because they share the same USING-only bug): token_budgets, eval_transcripts,
-- entitlements, slack_notifications. Same idempotent shape.
-- ----------------------------------------------------------------------------
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='token_budgets' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE token_budgets ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE token_budgets FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON token_budgets';
    EXECUTE 'CREATE POLICY tenant_isolation ON token_budgets
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='eval_transcripts' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE eval_transcripts ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE eval_transcripts FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON eval_transcripts';
    EXECUTE 'CREATE POLICY tenant_isolation ON eval_transcripts
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='entitlements' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE entitlements ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE entitlements FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON entitlements';
    EXECUTE 'CREATE POLICY tenant_isolation ON entitlements
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema='public' AND table_name='slack_notifications' AND column_name='org_id') THEN
    EXECUTE 'ALTER TABLE slack_notifications ENABLE ROW LEVEL SECURITY';
    EXECUTE 'ALTER TABLE slack_notifications FORCE  ROW LEVEL SECURITY';
    EXECUTE 'DROP POLICY IF EXISTS tenant_isolation ON slack_notifications';
    EXECUTE 'CREATE POLICY tenant_isolation ON slack_notifications
               USING      (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)
               WITH CHECK (org_id = NULLIF(current_setting(''app.current_org_id'', true), '''')::uuid)';
  END IF;
END $$;
