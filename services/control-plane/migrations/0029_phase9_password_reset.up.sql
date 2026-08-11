-- Phase 9 — password reset + session management.
--
-- 1. password_reset_tokens: single-use, hashed-at-rest reset tokens. Mirrors
--    magic_tokens (bytea SHA-256 PK, expiry, consume-once stamp) but lives in
--    its own table so the reset flow can evolve independently (rate limits,
--    audit joins) without overloading magic_tokens.purpose semantics.
CREATE TABLE IF NOT EXISTS password_reset_tokens (
  token_hash  bytea PRIMARY KEY,
  user_id     uuid NOT NULL REFERENCES users(id),
  expires_at  timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS password_reset_tokens_user_id_idx
  ON password_reset_tokens (user_id);

-- 2. sessions: device metadata for the session-management UI. All nullable —
--    existing rows predate collection, and non-browser paths (magic link,
--    invite claim, OAuth) may not carry a user agent.
ALTER TABLE sessions
  ADD COLUMN IF NOT EXISTS last_seen_at timestamptz,
  ADD COLUMN IF NOT EXISTS user_agent   text,
  ADD COLUMN IF NOT EXISTS ip           text;
