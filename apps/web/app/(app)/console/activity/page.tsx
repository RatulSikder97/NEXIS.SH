// Activity stream — org-wide live audit + activity feed.
//
// Server-side seed: latest 50 audit rows for the org. The client island
// owns the SSE attempt (GET /v1/orgs/{org}/activity-stream) with a fallback
// to a 2s poll on /v1/audit. Both endpoints may not exist yet — we just
// surface "stream unavailable" gracefully if the SSE endpoint 404s.
//
// Filters: workspace, agent role, event kind, severity. The page renders a
// filter chip strip that drives client-side filtering only — the server
// payload is the full org stream.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { ActivityClient, type ActivityRow } from "./client";
import type { MeResp } from "@/lib/auth";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

type AuditResp = {
  rows: ActivityRow[];
  total: number;
};

export default async function ActivityPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  const [meRes, auditRes] = await Promise.all([
    fetch(`${API}/v1/me`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
    fetch(`${API}/v1/audit?limit=50`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
  ]);
  if (!meRes.ok) redirect("/sign-in");
  const me = (await meRes.json()) as MeResp;
  const audit: AuditResp = auditRes.ok
    ? ((await auditRes.json()) as AuditResp)
    : { rows: [], total: 0 };

  return <ActivityClient initial={audit.rows} orgId={me.org.id} />;
}
