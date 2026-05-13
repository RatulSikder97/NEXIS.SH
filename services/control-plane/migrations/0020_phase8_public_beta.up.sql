-- Phase 8 — public-beta surface: system-wide invite codes, NASA-TLX responses,
-- and the per-org successful-recoveries counter the hero stats banner reads.
--
-- Three concerns ship together because they all back the same /v1 surface:
--
--   1. invite_codes — gated signup. System-wide (no RLS) because codes are
--      handed out outside any tenant scope before signup succeeds. Owner role
--      on any org can mint; an env flag (SIGNUP_REQUIRES_INVITE=1) makes
--      consumption mandatory.
--
--   2. nasa_tlx_responses — workload survey rows post-recovery. RLS-scoped on
--      org_id so the in-app survey form can't leak between tenants. Mirrors
--      the 0..20 scale of the standard six NASA-TLX subscales.
--
--   3. organizations.successful_recoveries_count — the hero stat shown on the
--      public landing. Maintained by an AFTER UPDATE trigger on workflow_runs
--      that increments when a run transitions to 'succeeded'. Stored on the
--      org row rather than computed at read time so the public landing page
--      can read without hitting Temporal data.

CREATE TABLE invite_codes (
  code         text PRIMARY KEY,
  max_uses     int  NOT NULL DEFAULT 1 CHECK (max_uses >= 1),
  used_count   int  NOT NULL DEFAULT 0 CHECK (used_count >= 0),
  expires_at   timestamptz,
  created_by   uuid REFERENCES users(id),
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX invite_codes_created_by_idx ON invite_codes (created_by);
CREATE INDEX invite_codes_expires_at_idx ON invite_codes (expires_at);

CREATE TABLE nasa_tlx_responses (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id          uuid REFERENCES users(id),
  org_id           uuid REFERENCES organizations(id),
  recovery_run_id  uuid,
  mental_demand    int CHECK (mental_demand    BETWEEN 0 AND 20),
  physical_demand  int CHECK (physical_demand  BETWEEN 0 AND 20),
  temporal_demand  int CHECK (temporal_demand  BETWEEN 0 AND 20),
  performance      int CHECK (performance      BETWEEN 0 AND 20),
  effort           int CHECK (effort           BETWEEN 0 AND 20),
  frustration      int CHECK (frustration      BETWEEN 0 AND 20),
  notes            text,
  created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX nasa_tlx_responses_org_idx ON nasa_tlx_responses (org_id, created_at);

ALTER TABLE organizations
  ADD COLUMN successful_recoveries_count int NOT NULL DEFAULT 0;

-- Trigger: bump organizations.successful_recoveries_count on each terminal
-- 'succeeded' transition. WHEN clause makes the trigger a no-op for any
-- update that doesn't flip into 'succeeded' from a non-'succeeded' value, so
-- idempotent UPDATE statements (e.g. re-stamping status='succeeded' on a row
-- that already had it) don't double-count.
CREATE OR REPLACE FUNCTION tg_pipeline_runs_increment_recoveries() RETURNS trigger AS $$
BEGIN
  UPDATE organizations
     SET successful_recoveries_count = successful_recoveries_count + 1
   WHERE id = NEW.org_id;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER pipeline_runs_increment_recoveries
AFTER UPDATE ON workflow_runs
FOR EACH ROW
WHEN (NEW.status = 'succeeded' AND OLD.status IS DISTINCT FROM 'succeeded')
EXECUTE FUNCTION tg_pipeline_runs_increment_recoveries();

-- RLS on nasa_tlx_responses — same NULLIF current_setting pattern used in 0004
-- so admin-pool reads (when app.current_org_id is unset) bypass the policy.
ALTER TABLE nasa_tlx_responses ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nasa_tlx_responses
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE, DELETE ON nasa_tlx_responses TO nexis_app;

-- invite_codes is intentionally NOT RLS-protected — it's a system-wide table.
-- nexis_app still needs SELECT (for the redemption path during signup, which
-- runs without an RLS principal anyway) and INSERT/UPDATE (for owner-issued
-- mint + admin revoke).
GRANT SELECT, INSERT, UPDATE, DELETE ON invite_codes TO nexis_app;
