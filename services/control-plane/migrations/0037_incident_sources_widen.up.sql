-- Widen incidents_raw.source to cover every source the code actually emits.
--
-- Two were missing and both failed at INSERT time with a CHECK violation:
--
--   'webhook'       — the vendor-neutral intake added in 0036. Without it the
--                     only incident source that needs no vendor account could
--                     never land a row.
--   'deploy_engine' — handler/deployments.go files an incident when a preview
--                     build or health check fails ("broken builds self-heal
--                     too"). That path had never been exercised against a
--                     connected project, so the violation went unnoticed.
--
-- Keep this list in sync with the `Source:` literals in the Go tree:
--   rg 'Source:\s*"' services/control-plane/internal --type go
ALTER TABLE incidents_raw
  DROP CONSTRAINT IF EXISTS incidents_raw_source_check;

ALTER TABLE incidents_raw
  ADD CONSTRAINT incidents_raw_source_check
  CHECK (source IN ('sentry','github','otel','datadog','pagerduty',
                    'schema_drift','qa_loop','webhook','deploy_engine'));
