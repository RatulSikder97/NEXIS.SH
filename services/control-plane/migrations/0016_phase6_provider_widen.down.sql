DELETE FROM integrations WHERE provider = 'slack';
ALTER TABLE integrations
  DROP CONSTRAINT IF EXISTS integrations_provider_check;
ALTER TABLE integrations
  ADD  CONSTRAINT integrations_provider_check
  CHECK (provider IN ('github','sentry','argocd'));
