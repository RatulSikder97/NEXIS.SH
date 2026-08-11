// Phase 9 — Settings/Sessions.
//
// Server component shell: fetches the caller's active sessions then hands
// off to the SessionsClient client component. Revocation happens client-side
// so the row can show a spinner and refresh in place — same split as the
// sibling api-keys page.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { SessionsClient } from "./client";
import type { SessionInfo } from "@/lib/auth";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function SessionsPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const r = await fetch(`${API}/v1/me/sessions`, {
    headers: { cookie: `nexis_session=${session.value}` },
    cache: "no-store",
  });
  if (r.status === 401) redirect("/sign-in");
  const initial: SessionInfo[] = r.ok
    ? ((await r.json()) as SessionInfo[])
    : [];

  return <SessionsClient initial={initial} />;
}
