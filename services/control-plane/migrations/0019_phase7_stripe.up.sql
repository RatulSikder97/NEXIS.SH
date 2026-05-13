-- Phase 7 — Stripe billing columns + idempotency table + audit anchors.
--
-- entitlements rows are tenant-isolated (RLS inherited from Phase 3.5).
-- The new Stripe columns inherit that policy automatically.
--
-- stripe_events_processed is a system table — admin-only writes, no RLS
-- because Stripe webhooks land before any per-tenant context exists.

-- ---------------------------------------------------------------------------
-- Phase 3.5 shipped a `payment_methods` table; Phase 7 introduces a new
-- `entitlements` ledger that tracks the per-org subscription state Stripe
-- owns. We create it from scratch (Phase 3.5 did not).
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS entitlements (
  org_id                  uuid PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
  plan                    text NOT NULL DEFAULT 'free',
  status                  text NOT NULL DEFAULT 'trialing',
  stripe_subscription_id  text,
  stripe_price_id         text,
  stripe_customer_id      text,
  current_period_end      timestamptz,
  trial_end               timestamptz,
  cancel_at_period_end    boolean NOT NULL DEFAULT false,
  updated_at              timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE entitlements DROP CONSTRAINT IF EXISTS entitlements_status_check;
ALTER TABLE entitlements ADD CONSTRAINT entitlements_status_check
  CHECK (status IN ('trialing','active','past_due','canceled','incomplete'));

CREATE INDEX IF NOT EXISTS entitlements_stripe_sub_idx
  ON entitlements (stripe_subscription_id);

-- ---------------------------------------------------------------------------
-- Stripe event idempotency. Webhook handler INSERTs (id, type) ON CONFLICT
-- DO NOTHING; rows-affected = 0 means duplicate event id => short-circuit
-- success without re-applying side effects.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS stripe_events_processed (
  id           text PRIMARY KEY,
  type         text NOT NULL,
  received_at  timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- RLS — entitlements rows are tenant-owned. tenant_isolation policy uses the
-- canonical NULLIF(current_setting(...), '')::uuid pattern so admin pool
-- (where app.current_org_id is unset) bypasses cleanly while the application
-- pool inside an RLS-pinned tx sees only its own org.
-- ---------------------------------------------------------------------------

ALTER TABLE entitlements ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON entitlements;
CREATE POLICY tenant_isolation ON entitlements
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid)
  WITH CHECK (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

-- stripe_events_processed is a system table — admin pool only.
-- No RLS, no GRANT to nexis_app.
GRANT SELECT, INSERT ON entitlements           TO nexis_app;
GRANT SELECT, UPDATE ON entitlements           TO nexis_app;

-- ---------------------------------------------------------------------------
-- Audit anchors — daily Merkle-rooted snapshots of audit_log for SOC2-lite.
-- Admin-pool only; no RLS.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS audit_anchors (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  period_start  timestamptz NOT NULL,
  period_end    timestamptz NOT NULL,
  row_count     bigint      NOT NULL,
  merkle_root   text        NOT NULL,
  prev_root     text,
  s3_object_key text        NOT NULL,
  s3_version_id text        NOT NULL,
  anchored_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (period_end)
);

CREATE INDEX IF NOT EXISTS audit_anchors_period_idx
  ON audit_anchors (period_end DESC);
