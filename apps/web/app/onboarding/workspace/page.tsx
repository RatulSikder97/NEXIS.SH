// Phase 3.5 Stage 6 — Onboarding wizard entry point.
//
// Server component. Three jobs:
//   1. Re-verify the session cookie via /v1/me — proxy.ts already gated
//      cookie presence; here we catch revoked / forged tokens.
//   2. Idempotent gate: if the user already has a workspace, send them
//      back to /console. We never want a returning user stuck in onboarding.
//   3. Hand the regions list to the client so the picker can render
//      immediately without a client-side fetch waterfall.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { OnboardingClient } from "./client";
import type { Region } from "@/lib/workspaces";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function OnboardingWorkspacePage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");

  const cookieHeader = `nexis_session=${session.value}`;

  // Verify session and read the has_workspace flag in a single round trip.
  const meR = await fetch(`${API}/v1/me`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  if (!meR.ok) redirect("/sign-in");
  const me = (await meR.json()) as { has_workspace?: boolean };

  // Idempotent: already has a workspace → straight to console.
  if (me.has_workspace) redirect("/console");

  const regionsR = await fetch(`${API}/v1/workspaces/regions`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  const regions: Region[] = regionsR.ok ? await regionsR.json() : [];

  return <OnboardingClient regions={regions} />;
}
