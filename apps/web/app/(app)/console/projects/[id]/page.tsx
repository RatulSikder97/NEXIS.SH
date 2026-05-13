// Phase 3.5 — Project detail page.
//
// Server component. Loads the project + best-effort recent incidents in
// parallel:
//
//   GET /v1/projects/{id}
//   GET /v1/workspaces/{ws}/incidents?project_id={id}   (fall back to [] on 404)
//
// We resolve the workspace from the cookie the same way the list page does
// so the incidents query lines up with what the Topbar is showing. Both
// fetches degrade independently — a 404 on incidents renders the empty
// state inside the Overview tab while the rest of the surface still works.

import { cookies } from "next/headers";
import { notFound, redirect } from "next/navigation";

import { ProjectDetailClient } from "./client";
import { projects, type Project } from "@/lib/projects";
import type { Workspace } from "@/lib/workspaces";
import type { WorkflowRun } from "@/lib/pipelines";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function ProjectDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;

  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  const project = await projects.get(id, { cookie: cookieHeader });

  // Workspace resolution is needed for the incidents fetch even when the
  // project endpoint is offline. We still try to render the detail surface
  // with a friendly fallback when the backend hasn't shipped yet.
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

  // If the project endpoint is fully offline we render a stub Project the
  // client can use for the EmptyState fallback; we don't 404 because the
  // BE-A endpoints ship in parallel and a 404 here would block users from
  // reaching the page at all during the rollout.
  let resolved: Project | null = project;
  if (!resolved && current) {
    // Bare placeholder so the surface still renders with "Endpoint coming
    // soon" panels under each tab. Pretty-much every field is left empty —
    // the client component uses `placeholderId` as the marker to render
    // a fallback banner instead of real content.
    resolved = {
      id,
      org_id: "",
      workspace_id: current.id,
      name: "Project",
      slug: id,
      description: "",
      environment: "dev",
      owner_user_id: "",
      selectors: {},
      recovery_policy: {
        auto_merge_low_severity: false,
        auto_merge_medium_severity: false,
        medium_countdown_seconds: 600,
        kill_switch_enabled: false,
        approver_user_ids: [],
        max_concurrent_recoveries: 1,
        rollback_on_slo_breach: false,
      },
      slo: {},
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };
  }
  if (!resolved) notFound();

  // Recent incidents are sourced from the existing pipelines endpoint with
  // an optional project_id filter. Backends that don't filter on project
  // yet will return all rows — we cap at 10 client-side either way.
  let incidents: WorkflowRun[] = [];
  if (current) {
    try {
      const r = await fetch(
        `${API}/v1/workspaces/${current.id}/incidents?project_id=${encodeURIComponent(id)}&limit=10`,
        {
          headers: { cookie: cookieHeader },
          cache: "no-store",
        },
      );
      if (r.ok) {
        incidents = (await r.json()) as WorkflowRun[];
      } else if (r.status === 404) {
        // Old endpoint name — try /pipelines as a fallback.
        const fb = await fetch(
          `${API}/v1/workspaces/${current.id}/pipelines?project_id=${encodeURIComponent(id)}&limit=10`,
          {
            headers: { cookie: cookieHeader },
            cache: "no-store",
          },
        );
        if (fb.ok) incidents = (await fb.json()) as WorkflowRun[];
      }
    } catch {
      // ignore — empty list is the safe default
    }
  }

  return (
    <ProjectDetailClient
      project={resolved}
      backendMissing={project === null}
      workspaceId={current?.id ?? ""}
      initialIncidents={incidents}
    />
  );
}
