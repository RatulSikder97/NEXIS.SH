// Phase 3.5 — Projects list page.
//
// Server component shell. Mirrors the resolve-current-workspace pattern from
// app/(app)/console/incidents/page.tsx exactly so the list always reflects the
// workspace the Topbar badge is pointing at:
//
//   1. Read the nexis_session cookie (layout already gated, but layered).
//   2. List workspaces, resolve the current one from nexis_workspace cookie
//      (fall back to first ready, then first row).
//   3. Fetch /v1/workspaces/{ws}/projects server-side to seed the client.
//
// Failures from the projects endpoint (BE-A ships in parallel) degrade to an
// empty list — the client renders an EmptyState with the "Connect project"
// CTA so the page is never broken while the backend catches up.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { ProjectsListClient } from "./client";
import type { Workspace } from "@/lib/workspaces";
import { projects, type Project } from "@/lib/projects";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function ProjectsPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");

  const cookieHeader = `nexis_session=${session.value}`;

  // Workspace resolution mirrors the layout's logic. The layout already
  // redirects users with zero workspaces to /onboarding/workspace, so by
  // the time we get here `workspaces.length > 0` is effectively invariant.
  const wsRes = await fetch(`${API}/v1/workspaces`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  const workspaces: Workspace[] = wsRes.ok ? await wsRes.json() : [];
  if (workspaces.length === 0) redirect("/onboarding/workspace");

  const currentCookie = c.get("nexis_workspace");
  const current =
    workspaces.find((w) => w.id === currentCookie?.value) ??
    workspaces.find((w) => w.status === "ready") ??
    workspaces[0];

  let initial: Project[] = [];
  if (current) {
    // The SDK fails soft to [] on non-2xx, but we still pass the cookie
    // explicitly because we're in a server context.
    initial = await projects.list(current.id, { cookie: cookieHeader });
  }

  return (
    <ProjectsListClient
      workspaceId={current?.id ?? ""}
      initial={initial}
    />
  );
}
