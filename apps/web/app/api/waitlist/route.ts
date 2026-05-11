import { NextResponse } from "next/server";
import { db, schema } from "@/lib/db";

export async function POST(req: Request) {
  let body: { email?: unknown; source?: unknown };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "invalid JSON" }, { status: 400 });
  }

  const email = typeof body.email === "string" ? body.email.trim().toLowerCase() : "";
  if (!email || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    return NextResponse.json({ error: "valid email required" }, { status: 400 });
  }
  const source = typeof body.source === "string" ? body.source.slice(0, 64) : null;

  const rows = await db
    .insert(schema.waitlist)
    .values({ email, source })
    .onConflictDoNothing()
    .returning();

  const row = rows[0] ?? { email };
  return NextResponse.json({ email: row.email }, { status: 201 });
}
