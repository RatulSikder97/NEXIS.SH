-- 0007_phase3_5_workspaces.up.sql
--
-- Phase 3.5 — adds workspaces, payment_methods, invoices, usage_records.
-- RLS policies + GRANTs are split into 0008 so the mirror order matches the
-- existing 0005/0006 pattern.

ALTER TABLE org_members ADD COLUMN last_workspace_id uuid;

CREATE TABLE workspaces (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id             uuid NOT NULL REFERENCES organizations(id),
  name               text NOT NULL,
  slug               text NOT NULL,
  region             text NOT NULL,
  status             text NOT NULL CHECK (status IN ('provisioning','ready','error','suspended')),
  status_message     text,
  provisioning_step  text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  ready_at           timestamptz,
  updated_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, slug)
);

CREATE TABLE payment_methods (
  id                          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                      uuid NOT NULL REFERENCES organizations(id),
  provider                    text NOT NULL,
  external_customer_id        text,
  external_payment_method_id  text,
  brand                       text,
  last4                       text,
  exp_month                   int,
  exp_year                    int,
  billing_email               text,
  created_at                  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id)
);

CREATE TABLE invoices (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL REFERENCES organizations(id),
  period_start  timestamptz NOT NULL,
  period_end    timestamptz NOT NULL,
  total_cents   bigint NOT NULL DEFAULT 0,
  status        text NOT NULL CHECK (status IN ('open','paid','void')),
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, period_start)
);

CREATE TABLE usage_records (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id            uuid NOT NULL REFERENCES organizations(id),
  workspace_id      uuid NOT NULL REFERENCES workspaces(id),
  project           text NOT NULL DEFAULT 'default',
  kind              text NOT NULL,
  quantity          numeric(20,6) NOT NULL,
  unit_price_cents  numeric(20,6) NOT NULL,
  amount_cents      numeric(20,6) NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX usage_records_org_recorded_idx ON usage_records (org_id, recorded_at);
