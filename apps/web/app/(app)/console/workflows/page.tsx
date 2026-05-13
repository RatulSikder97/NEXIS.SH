// Workflows surface — pan-workspace recent workflow runs.
//
// Server component shell. We list every workspace the operator can see and
// fan-out a /v1/workspaces/{ws}/pipelines call to seed the client. The client
// reduces the union by `started_at desc` and polls every 5s while any row
// is in motion. Each row expands inline to /console/incidents/{id}-style
// drill-down (we link out — the row body shows a quick OperationalSegments
// preview built from the synchronous list response).

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { WorkflowsClient, type WorkflowRowSeed } from "./client";
import type { Workspace } from "@/lib/workspaces";
import type { WorkflowRun } from "@/lib/pipelines";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function WorkflowsPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  // List workspaces server-side. The layout already guarantees ≥ 1 row.
  const wsRes = await fetch(`${API}/v1/workspaces`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  const workspaces: Workspace[] = wsRes.ok ? await wsRes.json() : [];

  // Fan out: list pipelines per workspace. Failures degrade to empty.
  const settled = await Promise.all(
    workspaces.map(async (w) => {
      try {
        const r = await fetch(
          `${API}/v1/workspaces/${w.id}/pipelines?limit=25`,
          {
            headers: { cookie: cookieHeader },
            cache: "no-store",
          },
        );
        if (!r.ok) return [];
        const rows = (await r.json()) as WorkflowRun[];
        return rows.map(
          (row): WorkflowRowSeed => ({
            run: row,
            workspace_id: w.id,
            workspace_name: w.name,
          }),
        );
      } catch {
        return [];
      }
    }),
  );
  const initial = settled.flat().sort((a, b) => {
    const ta = Date.parse(a.run.started_at);
    const tb = Date.parse(b.run.started_at);
    return tb - ta;
  });

  return (
    <WorkflowsClient
      initial={initial}
      workspaces={workspaces.map((w) => ({ id: w.id, name: w.name }))}
    />
  );
}
