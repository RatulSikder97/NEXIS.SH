// Recovery pipeline surface — active recovery runs across the org.
//
// Server side: fan out across workspaces and pull the latest pipeline runs.
// We filter on workflow_type "RecoveryPipeline" client-side so the page
// stays specific to incident-recovery work even when other workflow types
// run in the same workspaces.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import {
  RecoveryClient,
  type RecoveryRunSeed,
} from "./client";
import type { Workspace } from "@/lib/workspaces";
import type { WorkflowRun } from "@/lib/pipelines";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function RecoveryPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  const wsRes = await fetch(`${API}/v1/workspaces`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  const workspaces: Workspace[] = wsRes.ok ? await wsRes.json() : [];

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
        return rows
          .filter((r) => r.workflow_type === "RecoveryPipeline")
          .map(
            (row): RecoveryRunSeed => ({
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
  const initial = settled.flat().sort(
    (a, b) => Date.parse(b.run.started_at) - Date.parse(a.run.started_at),
  );

  return (
    <RecoveryClient
      initial={initial}
      workspaces={workspaces.map((w) => ({ id: w.id, name: w.name }))}
    />
  );
}
