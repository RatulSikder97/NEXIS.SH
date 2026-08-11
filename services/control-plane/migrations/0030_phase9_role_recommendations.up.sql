-- 0030_phase9_role_recommendations.up.sql — Intelligent role recommendation
-- (FYP: "Role Management & Access Control (Intelligent)").
--
-- The recommender (internal/usecase/rolerecommend) analyses audit_log activity
-- + session recency per org member and persists explainable recommendations
-- here. Admins accept (which mutates org_members.role) or dismiss from the
-- Members & Roles settings page. Every row carries a plain-language rationale
-- so the WHY is auditable alongside the decision itself.

CREATE TABLE role_recommendations (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id           uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  -- Named existing_role, not current_role: CURRENT_ROLE is a reserved SQL
  -- keyword (same family as CURRENT_USER) and breaks the parser unquoted.
  existing_role    text NOT NULL CHECK (existing_role IN ('owner', 'admin', 'member')),
  recommended_role text NOT NULL CHECK (recommended_role IN ('owner', 'admin', 'member')),
  -- machine id of the heuristic that fired (promote_active_member, ...);
  -- used for the dismissal cool-down so a dismissed rule is not re-raised
  -- for the same user on the next analysis run.
  rule             text NOT NULL,
  rationale        text NOT NULL,
  status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'dismissed')),
  created_at       timestamptz NOT NULL DEFAULT now(),
  decided_at       timestamptz,
  decided_by       uuid REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX idx_role_recommendations_org_status
  ON role_recommendations (org_id, status, created_at DESC);

-- At most one live recommendation per member per org — repeated analysis runs
-- upsert against this via ON CONFLICT ... DO NOTHING.
CREATE UNIQUE INDEX idx_role_recommendations_one_pending
  ON role_recommendations (org_id, user_id) WHERE status = 'pending';

-- Tenant isolation — canonical 0025-style policy (ENABLE + FORCE + WITH CHECK).
ALTER TABLE role_recommendations ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_recommendations FORCE  ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON role_recommendations
  USING      (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid)
  WITH CHECK (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON role_recommendations TO nexis_app;
