ALTER TABLE users ADD COLUMN preferences jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE integrations (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id            uuid NOT NULL REFERENCES organizations(id),
  provider          text NOT NULL CHECK (provider IN ('github','sentry','argocd')),
  status            text NOT NULL CHECK (status IN ('connected','pending','error','disconnected')),
  installation_id   text,
  secret_ciphertext bytea,
  metadata          jsonb,
  last_error        text,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, provider)
);

CREATE TABLE incidents_raw (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id),
  source          text NOT NULL CHECK (source IN ('sentry','github','otel')),
  source_event_id text,
  title           text,
  level           text,
  service         text,
  environment     text,
  raw_payload     jsonb,
  received_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, source, source_event_id)
);
CREATE INDEX incidents_raw_org_received_idx ON incidents_raw (org_id, received_at);

CREATE TABLE org_invites (
  token_hash       bytea PRIMARY KEY,
  org_id           uuid NOT NULL REFERENCES organizations(id),
  email            text NOT NULL,
  role             text NOT NULL CHECK (role IN ('admin','member')),
  inviter_user_id  uuid NOT NULL REFERENCES users(id),
  expires_at       timestamptz NOT NULL,
  claimed_at       timestamptz
);
CREATE INDEX org_invites_org_email_idx ON org_invites (org_id, email);
