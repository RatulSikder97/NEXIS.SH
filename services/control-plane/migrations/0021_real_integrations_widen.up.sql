-- 0021_real_integrations_widen.up.sql — widen integrations.provider to include
-- datadog + pagerduty. Replaces migration 0016's CHECK.
ALTER TABLE integrations
  DROP CONSTRAINT IF EXISTS integrations_provider_check;

ALTER TABLE integrations
  ADD CONSTRAINT integrations_provider_check
  CHECK (provider IN ('github','sentry','argocd','slack','datadog','pagerduty'));
