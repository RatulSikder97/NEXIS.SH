-- Phase 2 auth + tenancy: extend organizations/users/audit_log + add 4 new tables.
-- Mirrors packages/db/migrations/0001_phase2_auth.sql produced by drizzle-kit.

-- 1. Extend organizations: slug (NOT NULL UNIQUE) + owner_user_id.
--    Use a temporary default so the ALTER works even if rows already exist.
ALTER TABLE organizations
  ADD COLUMN slug          text NOT NULL DEFAULT 'tmp',
  ADD COLUMN owner_user_id uuid;
ALTER TABLE organizations
  ALTER COLUMN slug DROP DEFAULT;
ALTER TABLE organizations
  ADD CONSTRAINT organizations_slug_unique UNIQUE (slug);

-- 2. Extend users with auth-related columns.
ALTER TABLE users
  ADD COLUMN password_hash     text,
  ADD COLUMN mfa_secret        text,
  ADD COLUMN mfa_enabled       boolean NOT NULL DEFAULT false,
  ADD COLUMN email_verified_at timestamptz;

-- 3. Extend audit_log with the hash-chain columns.
--    row_hash uses temp default '\x' so the ALTER works against existing rows.
ALTER TABLE audit_log
  ADD COLUMN prev_hash bytea,
  ADD COLUMN row_hash  bytea NOT NULL DEFAULT '\x'::bytea;
ALTER TABLE audit_log
  ALTER COLUMN row_hash DROP DEFAULT;

-- 4. org_members — composite PK (org_id, user_id).
CREATE TABLE IF NOT EXISTS org_members (
  org_id     uuid NOT NULL REFERENCES organizations(id),
  user_id    uuid NOT NULL REFERENCES users(id),
  role       text NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT org_members_org_id_user_id_pk PRIMARY KEY (org_id, user_id)
);

-- 5. sessions.
CREATE TABLE IF NOT EXISTS sessions (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid NOT NULL REFERENCES users(id),
  org_id     uuid NOT NULL REFERENCES organizations(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz
);

-- 6. magic_tokens — PK on bytea token_hash.
CREATE TABLE IF NOT EXISTS magic_tokens (
  token_hash bytea PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users(id),
  purpose    text NOT NULL CHECK (purpose IN ('login', 'verify_email', 'reset_password')),
  expires_at timestamptz NOT NULL,
  used_at    timestamptz
);

-- 7. api_keys.
CREATE TABLE IF NOT EXISTS api_keys (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL REFERENCES organizations(id),
  user_id      uuid NOT NULL REFERENCES users(id),
  prefix       text NOT NULL,
  hash         bytea NOT NULL,
  scopes       text[] NOT NULL DEFAULT '{}',
  name         text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz,
  revoked_at   timestamptz
);

CREATE INDEX IF NOT EXISTS api_keys_prefix_idx ON api_keys (prefix);
