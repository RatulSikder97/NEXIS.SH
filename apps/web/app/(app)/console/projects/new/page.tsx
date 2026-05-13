// Phase 3.5 — Connect Project wizard shell.
//
// Server component. Loads /v1/me + the integrations list so the wizard
// knows which providers are connected (the GitHub / Sentry / Slack steps
// short-circuit to a "connect this integration first" path when the
// integration isn't wired). We also pull the workspace id so the final
// POST hits the right tenant.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { ProjectConnectWizard } from "@/components/projects/ProjectConnectWizard";
import type { Integration } from "@/lib/integrations";
import type { MeResp } from "@/lib/auth";
import type { Workspace } from "@/lib/workspaces";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function NewProjectPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  const [meR, intR, wsR] = await Promise.all([
    fetch(`${API}/v1/me`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
    fetch(`${API}/v1/integrations`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
    fetch(`${API}/v1/workspaces`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
  ]);

  if (!meR.ok) redirect("/sign-in");
  const me = (await meR.json()) as MeResp;
  const integrations: Integration[] = intR.ok
    ? ((await intR.json()) as Integration[])
    : [];
  const workspaces: Workspace[] = wsR.ok
    ? ((await wsR.json()) as Workspace[])
    : [];

  if (workspaces.length === 0) redirect("/onboarding/workspace");
  const currentCookie = c.get("nexis_workspace");
  const current =
    workspaces.find((w) => w.id === currentCookie?.value) ??
    workspaces.find((w) => w.status === "ready") ??
    workspaces[0];
  if (!current) redirect("/onboarding/workspace");

  return (
    <ProjectConnectWizard
      workspaceId={current.id}
      workspaceName={current.name}
      me={me}
      integrations={integrations}
    />
  );
}
