-- Reverting drops any webhook intake connections first: the narrowed CHECK
-- would otherwise fail against existing rows.
DELETE FROM integrations WHERE provider = 'webhook';

ALTER TABLE integrations
  DROP CONSTRAINT IF EXISTS integrations_provider_check;
ALTER TABLE integrations
  ADD  CONSTRAINT integrations_provider_check
  CHECK (provider IN ('github','sentry','argocd','slack','datadog','pagerduty'));
