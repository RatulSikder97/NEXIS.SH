import { sql } from "drizzle-orm";
import {
  pgTable,
  uuid,
  text,
  timestamp,
  jsonb,
  index,
  boolean,
  primaryKey,
  customType,
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
  orgId:     uuid("org_id").notNull().references(() => organizations.id),
  userId:    uuid("user_id").notNull().references(() => users.id),
  role:      text("role", { enum: ["owner", "admin", "member"] }).notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
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
