import { pgTable, uuid, text, timestamp, jsonb, index } from "drizzle-orm/pg-core";

export const organizations = pgTable("organizations", {
  id:        uuid("id").primaryKey().defaultRandom(),
  name:      text("name").notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const users = pgTable("users", {
  id:        uuid("id").primaryKey().defaultRandom(),
  email:     text("email").notNull().unique(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const auditLog = pgTable("audit_log", {
  id:        uuid("id").primaryKey().defaultRandom(),
  orgId:     uuid("org_id"),
  actor:     text("actor"),
  action:    text("action").notNull(),
  target:    text("target"),
  metadata:  jsonb("metadata"),
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
