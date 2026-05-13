// Phase 3.5 Stage 8 — Settings/Workspaces.
//
// Server component that pulls the workspaces list + regions in parallel
// (regions are tiny + fixed, but fetching here lets the client render the
// create-workspace dialog without an extra round trip). Also reads /v1/me
// so we can pass the caller's role down for the Suspend-button gating.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { WorkspacesSettingsClient } from "./client";
import type { MeResp } from "@/lib/auth";
import type { Region, Workspace } from "@/lib/workspaces";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function WorkspacesSettingsPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");

  const cookieHeader = `nexis_session=${session.value}`;

  const [meR, wsR, regR] = await Promise.all([
    fetch(`${API}/v1/me`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
    fetch(`${API}/v1/workspaces`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
    fetch(`${API}/v1/workspaces/regions`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
  ]);

  if (!meR.ok) redirect("/sign-in");
  const me = (await meR.json()) as MeResp;
  const initial: Workspace[] = wsR.ok ? await wsR.json() : [];
  const regions: Region[] = regR.ok ? await regR.json() : [];

  return (
    <WorkspacesSettingsClient
      initial={initial}
      regions={regions}
      role={me.role}
    />
  );
}
