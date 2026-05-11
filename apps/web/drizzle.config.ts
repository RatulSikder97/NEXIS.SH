import type { Config } from "drizzle-kit";
export default {
  schema: "../../packages/db/schema.ts",
  out: "../../packages/db/migrations",
  dialect: "postgresql",
  dbCredentials: { url: process.env.DATABASE_URL ?? "postgres://nexis:nexis_dev_password@localhost:5432/nexis" },
} satisfies Config;
