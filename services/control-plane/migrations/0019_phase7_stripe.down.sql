-- Phase 7 — reverse 0019: drop audit anchors + Stripe idempotency + entitlements.

DROP INDEX IF EXISTS audit_anchors_period_idx;
DROP TABLE IF EXISTS audit_anchors;

DROP TABLE IF EXISTS stripe_events_processed;

DROP POLICY IF EXISTS tenant_isolation ON entitlements;
ALTER TABLE IF EXISTS entitlements DISABLE ROW LEVEL SECURITY;
DROP INDEX IF EXISTS entitlements_stripe_sub_idx;
DROP TABLE IF EXISTS entitlements;
