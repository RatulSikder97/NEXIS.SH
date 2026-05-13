// Phase 4 Stage 7 — Incidents (pipeline runs) list page.
//
// Server component shell. We mirror the resolve-current-workspace pattern
// from app/(app)/console/page.tsx exactly so the list always queries the
// workspace the Topbar badge is pointing at:
//   1. Read the nexis_session cookie (the layout already gated on /v1/me,
//      but the proxy plus server fetch are best-effort layered).
//   2. List workspaces, resolve the current one from nexis_workspace cookie
//      (fall back to first ready, then first row).
//   3. Fetch /v1/workspaces/{ws}/pipelines server-side to seed the client.
//
// Failures degrade silently to an empty list — the client component handles
// the polling refetch so transient errors recover without a page reload.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { IncidentsClient } from "./client";
import type { Workspace } from "@/lib/workspaces";
import type { WorkflowRun } from "@/lib/pipelines";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function IncidentsPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");

  const cookieHeader = `nexis_session=${session.value}`;

  // Workspace resolution mirrors the console layout's logic so the list
  // matches what the Topbar shows. The layout already redirects to
  // /onboarding/workspace when the user has zero workspaces, so by the
  // time we get here `workspaces.length > 0` is effectively invariant —
  // but we still guard for safety.
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

  let initial: WorkflowRun[] = [];
  if (current) {
    const r = await fetch(
      `${API}/v1/workspaces/${current.id}/pipelines?limit=50`,
      {
        headers: { cookie: cookieHeader },
        cache: "no-store",
      },
    );
    if (r.ok) initial = (await r.json()) as WorkflowRun[];
  }

  return (
    <IncidentsClient
      workspaceId={current?.id ?? ""}
      initial={initial}
    />
  );
}
