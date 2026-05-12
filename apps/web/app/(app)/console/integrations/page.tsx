// Phase 3 Stage 7 — Integrations surface.
//
// Server component. Loads /v1/integrations + /v1/me in parallel using the
// request's nexis_session cookie. If the session is missing or rejected we
// bounce to /sign-in. The list response is opaque to this page; rendering
// + form orchestration happens in the IntegrationsClient client component.
//
// API_URL_INTERNAL takes precedence for the server-side fetch so docker-
// compose deployments can talk to the control-plane over the internal
// network. NEXT_PUBLIC_API_URL is the same value in pure-localhost dev and
// is the URL we surface to the browser (for the Sentry webhook URL).

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { IntegrationsClient } from "./client";
import type { Integration } from "@/lib/integrations";
import type { MeResp } from "@/lib/auth";

const INTERNAL_API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";
const PUBLIC_API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export default async function IntegrationsPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");

  const cookieHeader = `nexis_session=${session.value}`;
  const [intR, meR] = await Promise.all([
    fetch(`${INTERNAL_API}/v1/integrations`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
    fetch(`${INTERNAL_API}/v1/me`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
  ]);
  if (!meR.ok) redirect("/sign-in");
  const me = (await meR.json()) as MeResp;
  const initial: Integration[] = intR.ok
    ? ((await intR.json()) as Integration[])
    : [];

  return (
    <IntegrationsClient initial={initial} orgId={me.org.id} apiUrl={PUBLIC_API} />
  );
}
