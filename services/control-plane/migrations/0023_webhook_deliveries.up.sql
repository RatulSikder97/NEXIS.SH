-- 0023_webhook_deliveries.up.sql — log every inbound provider webhook delivery.
--
-- The existing webhook handlers (handler/webhooks.go::Webhook and
-- ::WebhookByQuery) verify HMAC and dispatch to the adapter today but leave
-- no audit-grade trail behind. This table fills that gap: every delivery
-- (verified, rejected, processed, failed) is appended here so the
-- /v1/integrations/webhooks endpoint can power the operator's "webhook
-- activity" view + give engineers a replay log.
--
-- Sizing notes:
--
--   - payload is jsonb, intentionally not capped at the SQL layer. The
--     handler truncates oversized payloads to ~32KB at insert time so the
--     row stays manageable. headers is small (a few hundred bytes per row)
--     and is kept verbatim so HMAC failures can be diagnosed.
--
--   - status is constrained to the four canonical outcomes so dashboards
--     can rely on the column for counts/groupings without normalising at
--     read time.
--
--   - The (org_id, ts DESC) index supports the "most recent N deliveries
--     for an org" query pattern the operator endpoint uses.
--
-- RLS:
--
--   tenant_isolation matches the rest of the surface — every row is filtered
--   by app.current_org_id under the per-request RLS tx. The webhook handler
--   pins app.current_org_id from the URL/query before INSERT, so writes
--   succeed under the policy without a request principal.
--
-- Grants:
--
--   nexis_app gets SELECT + INSERT only. Deliveries are immutable history;
--   nothing in the app updates or deletes rows. Retention is left to a
--   future cron once volume warrants it (Phase 9+).

CREATE TABLE webhook_deliveries (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  provider      text NOT NULL,
  event_type    text NOT NULL,
  status        text NOT NULL CHECK (status IN ('verified','rejected','processed','failed')),
  latency_ms    int NOT NULL DEFAULT 0,
  payload_size  int NOT NULL DEFAULT 0,
  source_ip     inet,
  headers       jsonb,
  payload       jsonb,
  error         text,
  ts            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_webhook_deliveries_org_ts ON webhook_deliveries (org_id, ts DESC);

ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
CREATE POLICY webhook_deliveries_tenant ON webhook_deliveries
  USING (org_id = NULLIF(current_setting('app.current_org_id', true), '')::uuid);

GRANT SELECT, INSERT ON webhook_deliveries TO nexis_app;
