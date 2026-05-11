-- Revert Phase 2 auth + tenancy schema additions.

DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS magic_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS org_members;

ALTER TABLE audit_log
  DROP COLUMN IF EXISTS row_hash,
  DROP COLUMN IF EXISTS prev_hash;

ALTER TABLE users
  DROP COLUMN IF EXISTS email_verified_at,
  DROP COLUMN IF EXISTS mfa_enabled,
  DROP COLUMN IF EXISTS mfa_secret,
  DROP COLUMN IF EXISTS password_hash;

ALTER TABLE organizations
  DROP CONSTRAINT IF EXISTS organizations_slug_unique;
ALTER TABLE organizations
  DROP COLUMN IF EXISTS owner_user_id,
  DROP COLUMN IF EXISTS slug;
