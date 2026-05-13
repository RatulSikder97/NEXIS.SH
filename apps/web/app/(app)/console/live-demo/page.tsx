// Phase 4 Stage 7 — Live Demo page.
//
// Server component shell. Resolves the current workspace from the
// nexis_workspace cookie (same pattern as the incidents pages), then hands
// it off to the client component which owns the CTA + navigation.
//
// Phase 4 ships the trigger only; the underlying RecoveryPipeline is the
// real Temporal workflow registered by the control-plane.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { LiveDemoClient } from "./client";
import type { Workspace } from "@/lib/workspaces";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function LiveDemoPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  const wsRes = await fetch(`${API}/v1/workspaces`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  const workspaces: Workspace[] = wsRes.ok ? await wsRes.json() : [];
  const currentCookie = c.get("nexis_workspace");
  const current =
    workspaces.find((w) => w.id === currentCookie?.value) ??
    workspaces.find((w) => w.status === "ready") ??
    workspaces[0];

  return <LiveDemoClient workspaceId={current?.id ?? ""} />;
}
