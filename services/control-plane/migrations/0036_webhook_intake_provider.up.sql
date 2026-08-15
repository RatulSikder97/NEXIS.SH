-- Vendor-neutral incident intake. Sentinel only scans orgs returned by
-- ConnectedIncidentOrgs, which is driven off this table, so a deployment
-- without a Sentry/Datadog/PagerDuty account could never detect anything.
-- 'webhook' is a first-class provider whose Connect needs no external
-- account: it mints a signing secret and accepts signed events at
-- POST /v1/webhooks/webhook/{org_id}.

ALTER TABLE integrations
  DROP CONSTRAINT IF EXISTS integrations_provider_check;
ALTER TABLE integrations
  ADD  CONSTRAINT integrations_provider_check
  CHECK (provider IN ('github','sentry','argocd','slack','datadog','pagerduty','webhook'));
