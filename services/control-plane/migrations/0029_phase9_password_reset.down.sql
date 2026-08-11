-- Phase 9 rollback — password reset + session management.
ALTER TABLE sessions
  DROP COLUMN IF EXISTS ip,
  DROP COLUMN IF EXISTS user_agent,
  DROP COLUMN IF EXISTS last_seen_at;

DROP INDEX IF EXISTS password_reset_tokens_user_id_idx;
DROP TABLE IF EXISTS password_reset_tokens;
