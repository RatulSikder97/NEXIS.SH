-- Phase 6 — widen integrations.provider CHECK to include 'slack'. Slack
-- joins the (github, sentry, argocd) trio as the 4th external integration.

ALTER TABLE integrations
  DROP CONSTRAINT IF EXISTS integrations_provider_check;
ALTER TABLE integrations
  ADD  CONSTRAINT integrations_provider_check
  CHECK (provider IN ('github','sentry','argocd','slack'));
