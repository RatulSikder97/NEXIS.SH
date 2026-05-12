// Phase 3 Stage 9 — Audit surface.
//
// Server component shell. Fetches the first page of /v1/audit (limit=50) so
// the table renders on first paint. Subsequent filtering + pagination are
// driven entirely client-side via audit.list(...).
//
// Member-role users can't list audit; we degrade to an "Insufficient access"
// banner if the API returns 403. Other failures show an empty table.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { AuditClient } from "./client";
import type { AuditListResp } from "@/lib/audit";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

const DEFAULT_LIMIT = 50;

export default async function AuditPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const r = await fetch(`${API}/v1/audit?limit=${DEFAULT_LIMIT}`, {
    headers: { cookie: `nexis_session=${session.value}` },
    cache: "no-store",
  });
  let initial: AuditListResp = { rows: [], total: 0 };
  let forbidden = false;
  if (r.status === 401) redirect("/sign-in");
  if (r.status === 403) forbidden = true;
  if (r.ok) initial = (await r.json()) as AuditListResp;

  return (
    <AuditClient initial={initial} forbidden={forbidden} pageSize={DEFAULT_LIMIT} />
  );
}
