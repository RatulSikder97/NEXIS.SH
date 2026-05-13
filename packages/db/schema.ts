import { sql } from "drizzle-orm";
import {
  pgTable,
  uuid,
  text,
  timestamp,
  jsonb,
  index,
  uniqueIndex,
  boolean,
  primaryKey,
  customType,
  bigint,
  integer,
  numeric,
} from "drizzle-orm/pg-core";

const bytea = customType<{ data: Buffer }>({ dataType: () => "bytea" });

export const organizations = pgTable("organizations", {
  id:           uuid("id").primaryKey().defaultRandom(),
  name:         text("name").notNull(),
  slug:         text("slug").notNull().unique(),
  ownerUserId:  uuid("owner_user_id"),
  createdAt:    timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const users = pgTable("users", {
  id:               uuid("id").primaryKey().defaultRandom(),
  email:            text("email").notNull().unique(),
  passwordHash:     text("password_hash"),
  mfaSecret:        text("mfa_secret"),
  mfaEnabled:       boolean("mfa_enabled").notNull().default(false),
  emailVerifiedAt:  timestamp("email_verified_at", { withTimezone: true }),
  preferences:      jsonb("preferences").notNull().default(sql`'{}'::jsonb`),
  createdAt:        timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const auditLog = pgTable("audit_log", {
  id:        uuid("id").primaryKey().defaultRandom(),
  orgId:     uuid("org_id"),
  actor:     text("actor"),
  action:    text("action").notNull(),
  target:    text("target"),
  metadata:  jsonb("metadata"),
  prevHash:  bytea("prev_hash"),
  rowHash:   bytea("row_hash").notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  orgCreatedIdx: index("audit_log_org_created_idx").on(t.orgId, t.createdAt),
}));

export const waitlist = pgTable("waitlist", {
  id:        uuid("id").primaryKey().defaultRandom(),
  email:     text("email").notNull().unique(),
  source:    text("source"),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const orgMembers = pgTable("org_members", {
  orgId:            uuid("org_id").notNull().references(() => organizations.id),
  userId:           uuid("user_id").notNull().references(() => users.id),
  role:             text("role", { enum: ["owner", "admin", "member"] }).notNull(),
  lastWorkspaceId:  uuid("last_workspace_id"),
  createdAt:        timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  pk: primaryKey({ columns: [t.orgId, t.userId] }),
}));

export const sessions = pgTable("sessions", {
  id:        uuid("id").primaryKey().defaultRandom(),
  userId:    uuid("user_id").notNull().references(() => users.id),
  orgId:     uuid("org_id").notNull().references(() => organizations.id),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  expiresAt: timestamp("expires_at", { withTimezone: true }).notNull(),
  revokedAt: timestamp("revoked_at", { withTimezone: true }),
});

export const magicTokens = pgTable("magic_tokens", {
  tokenHash: bytea("token_hash").primaryKey(),
  userId:    uuid("user_id").notNull().references(() => users.id),
  purpose:   text("purpose", { enum: ["login", "verify_email", "reset_password"] }).notNull(),
  expiresAt: timestamp("expires_at", { withTimezone: true }).notNull(),
  usedAt:    timestamp("used_at", { withTimezone: true }),
});

export const apiKeys = pgTable("api_keys", {
  id:          uuid("id").primaryKey().defaultRandom(),
  orgId:       uuid("org_id").notNull().references(() => organizations.id),
  userId:      uuid("user_id").notNull().references(() => users.id),
  prefix:      text("prefix").notNull(),
  hash:        bytea("hash").notNull(),
  scopes:      text("scopes").array().notNull().default(sql`'{}'`),
  name:        text("name").notNull(),
  createdAt:   timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  lastUsedAt:  timestamp("last_used_at", { withTimezone: true }),
  revokedAt:   timestamp("revoked_at", { withTimezone: true }),
}, t => ({
  prefixIdx: index("api_keys_prefix_idx").on(t.prefix),
}));

export const integrations = pgTable("integrations", {
  id:                uuid("id").primaryKey().defaultRandom(),
  orgId:             uuid("org_id").notNull().references(() => organizations.id),
  provider:          text("provider", { enum: ["github", "sentry", "argocd"] }).notNull(),
  status:            text("status", { enum: ["connected", "pending", "error", "disconnected"] }).notNull(),
  installationId:    text("installation_id"),
  secretCiphertext:  bytea("secret_ciphertext"),
  metadata:          jsonb("metadata"),
  lastError:         text("last_error"),
  createdAt:         timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  updatedAt:         timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqOrgProvider: uniqueIndex("integrations_org_provider_uniq").on(t.orgId, t.provider),
}));

export const incidentsRaw = pgTable("incidents_raw", {
  id:           uuid("id").primaryKey().defaultRandom(),
  orgId:        uuid("org_id").notNull().references(() => organizations.id),
  source:       text("source", { enum: ["sentry", "github", "otel"] }).notNull(),
  sourceEventId: text("source_event_id"),
  title:        text("title"),
  level:        text("level"),
  service:      text("service"),
  environment:  text("environment"),
  rawPayload:   jsonb("raw_payload"),
  receivedAt:   timestamp("received_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqSource: uniqueIndex("incidents_raw_source_uniq").on(t.orgId, t.source, t.sourceEventId),
  orgReceivedIdx: index("incidents_raw_org_received_idx").on(t.orgId, t.receivedAt),
}));

export const orgInvites = pgTable("org_invites", {
  tokenHash:     bytea("token_hash").primaryKey(),
  orgId:         uuid("org_id").notNull().references(() => organizations.id),
  email:         text("email").notNull(),
  role:          text("role", { enum: ["admin", "member"] }).notNull(),
  inviterUserId: uuid("inviter_user_id").notNull().references(() => users.id),
  expiresAt:     timestamp("expires_at", { withTimezone: true }).notNull(),
  claimedAt:     timestamp("claimed_at", { withTimezone: true }),
}, t => ({
  orgEmailIdx: index("org_invites_org_email_idx").on(t.orgId, t.email),
}));

export const workspaces = pgTable("workspaces", {
  id:                uuid("id").primaryKey().defaultRandom(),
  orgId:             uuid("org_id").notNull().references(() => organizations.id),
  name:              text("name").notNull(),
  slug:              text("slug").notNull(),
  region:            text("region").notNull(),
  status:            text("status", { enum: ["provisioning", "ready", "error", "suspended"] }).notNull(),
  statusMessage:     text("status_message"),
  provisioningStep:  text("provisioning_step"),
  createdAt:         timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  readyAt:           timestamp("ready_at", { withTimezone: true }),
  updatedAt:         timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqOrgSlug: uniqueIndex("workspaces_org_slug_uniq").on(t.orgId, t.slug),
}));

export const paymentMethods = pgTable("payment_methods", {
  id:                       uuid("id").primaryKey().defaultRandom(),
  orgId:                    uuid("org_id").notNull().references(() => organizations.id),
  provider:                 text("provider").notNull(),
  externalCustomerId:       text("external_customer_id"),
  externalPaymentMethodId:  text("external_payment_method_id"),
  brand:                    text("brand"),
  last4:                    text("last4"),
  expMonth:                 integer("exp_month"),
  expYear:                  integer("exp_year"),
  billingEmail:             text("billing_email"),
  createdAt:                timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqOrg: uniqueIndex("payment_methods_org_uniq").on(t.orgId),
}));

export const invoices = pgTable("invoices", {
  id:           uuid("id").primaryKey().defaultRandom(),
  orgId:        uuid("org_id").notNull().references(() => organizations.id),
  periodStart:  timestamp("period_start", { withTimezone: true }).notNull(),
  periodEnd:    timestamp("period_end", { withTimezone: true }).notNull(),
  totalCents:   bigint("total_cents", { mode: "number" }).notNull().default(0),
  status:       text("status", { enum: ["open", "paid", "void"] }).notNull(),
  createdAt:    timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqOrgPeriod: uniqueIndex("invoices_org_period_uniq").on(t.orgId, t.periodStart),
}));

export const usageRecords = pgTable("usage_records", {
  id:              uuid("id").primaryKey().defaultRandom(),
  orgId:           uuid("org_id").notNull().references(() => organizations.id),
  workspaceId:     uuid("workspace_id").notNull().references(() => workspaces.id),
  project:         text("project").notNull().default("default"),
  kind:            text("kind").notNull(),
  quantity:        numeric("quantity", { precision: 20, scale: 6 }).notNull(),
  unitPriceCents:  numeric("unit_price_cents", { precision: 20, scale: 6 }).notNull(),
  amountCents:     numeric("amount_cents", { precision: 20, scale: 6 }).notNull(),
  recordedAt:      timestamp("recorded_at", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  orgRecordedIdx: index("usage_records_org_recorded_idx").on(t.orgId, t.recordedAt),
}));

// ---------------------------------------------------------------------------
// Phase 4 — Pipeline substrate (Temporal workflows + activity events).
// ---------------------------------------------------------------------------

const workflowRunStatus = ["queued", "running", "succeeded", "failed", "timed_out", "cancelled"] as const;
const activityStatus    = ["started", "succeeded", "failed", "retrying", "timed_out"] as const;

export const workflowRuns = pgTable("workflow_runs", {
  id:              uuid("id").primaryKey().defaultRandom(),
  orgId:           uuid("org_id").notNull().references(() => organizations.id),
  workspaceId:     uuid("workspace_id").notNull().references(() => workspaces.id),
  workflowType:    text("workflow_type").notNull(),
  temporalRunId:   text("temporal_run_id").notNull(),
  temporalWfId:    text("temporal_wf_id").notNull(),
  status:          text("status", { enum: workflowRunStatus }).notNull(),
  currentStep:     text("current_step"),
  input:           jsonb("input"),
  output:          jsonb("output"),
  error:           text("error"),
  startedAt:       timestamp("started_at",   { withTimezone: true }).notNull().defaultNow(),
  completedAt:     timestamp("completed_at", { withTimezone: true }),
  durationMs:      bigint("duration_ms", { mode: "number" }),
  createdBy:       uuid("created_by").references(() => users.id),
}, t => ({
  uniqOrgTemporal: uniqueIndex("workflow_runs_org_temporal_uniq").on(t.orgId, t.temporalRunId),
  orgWsStartedIdx: index("workflow_runs_org_ws_started_idx").on(t.orgId, t.workspaceId, t.startedAt),
}));

export const activityEvents = pgTable("activity_events", {
  id:              uuid("id").primaryKey().defaultRandom(),
  orgId:           uuid("org_id").notNull().references(() => organizations.id),
  workflowRunId:   uuid("workflow_run_id").notNull().references(() => workflowRuns.id, { onDelete: "cascade" }),
  seq:             integer("seq").notNull(),
  agentRole:       text("agent_role").notNull(),
  activityName:    text("activity_name").notNull(),
  status:          text("status", { enum: activityStatus }).notNull(),
  attempt:         integer("attempt").notNull().default(1),
  message:         text("message"),
  payload:         jsonb("payload"),
  ts:              timestamp("ts", { withTimezone: true }).notNull().defaultNow(),
}, t => ({
  uniqRunSeq: uniqueIndex("activity_events_run_seq_uniq").on(t.workflowRunId, t.seq),
}));
