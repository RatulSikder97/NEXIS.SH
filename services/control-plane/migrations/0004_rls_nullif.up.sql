-- Stage 3 fix: harden RLS policies against the empty-string remnant left
-- behind on a pgx-pool connection after a `set_config('app.current_org_id',
-- uuid, true)` tx commits.
--
-- Postgres behaviour we hit:
--
--   - current_setting('app.current_org_id', true) returns NULL while the
--     GUC has never been touched on the connection (good).
--   - After ANY set_config call — even with is_local=true and a subsequent
--     COMMIT/ROLLBACK — the parameter remains "known" on the session, and
--     current_setting(...) returns '' (empty string) instead of NULL.
--   - The original policy text `current_setting(...)::uuid` then fails to
--     cast '' → uuid, raising 22P02 against ANY query on these tables that
--     a recycled pool connection touches without first re-binding the GUC.
--
-- Fix: wrap with NULLIF(..., '')::uuid so empty string collapses to NULL,
-- the comparison `org_id = NULL` becomes NULL (treated as false by USING),
-- and queries without a bound principal silently return zero rows instead
-- of erroring. This is the correct production-grade behaviour — a request
-- that forgot to bind the tenant GUC should NOT be able to read tenant
-- data, regardless of whether the connection had been used previously.

DROP POLICY IF EXISTS tenant_isolation ON org_members;
CREATE POLICY tenant_isolation ON org_members
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON sessions;
CREATE POLICY tenant_isolation ON sessions
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

DROP POLICY IF EXISTS user_isolation ON magic_tokens;
CREATE POLICY user_isolation ON magic_tokens
  USING (user_id IN (
    SELECT user_id FROM org_members
    WHERE org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid
  ));

DROP POLICY IF EXISTS tenant_isolation ON api_keys;
CREATE POLICY tenant_isolation ON api_keys
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON audit_log;
CREATE POLICY tenant_isolation ON audit_log
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
