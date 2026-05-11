import { drizzle } from "drizzle-orm/postgres-js";
import type { PostgresJsDatabase } from "drizzle-orm/postgres-js";
import postgres from "postgres";
import * as schema from "@nexis/db";

type DB = PostgresJsDatabase<typeof schema>;

let cached: DB | null = null;

function getDb(): DB {
  if (cached) return cached;
  const url = process.env.DATABASE_URL;
  if (!url) throw new Error("DATABASE_URL is not set");
  const client = postgres(url, { prepare: false });
  cached = drizzle(client, { schema });
  return cached;
}

export const db: DB = new Proxy({} as DB, {
  get: (_t, prop) => Reflect.get(getDb() as object, prop),
}) as DB;

export { schema };
