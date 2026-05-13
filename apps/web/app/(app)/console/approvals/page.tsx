// Phase 6 Stage 9 — Approvals (pending decisions) list page.
//
// Server component shell. Mirrors the resolve-current-workspace pattern from
// the incidents page so the surface scopes to the workspace the Topbar badge
// is pointing at:
//   1. Read the nexis_session cookie (the layout already gated on /v1/me).
//   2. List workspaces, resolve the current one from nexis_workspace cookie.
//   3. Fetch /v1/workspaces/{ws}/approvals/pending server-side to seed the
//      client component. Failures degrade silently to an empty list; the
//      client polls every 5s and recovers from transient errors.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { ApprovalsClient } from "./client";
import type { Workspace } from "@/lib/workspaces";
import type { PendingApproval } from "@/lib/approvals";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function ApprovalsPage() {
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

  let initial: PendingApproval[] = [];
  if (current) {
    const r = await fetch(
      `${API}/v1/workspaces/${current.id}/approvals/pending`,
      {
        headers: { cookie: cookieHeader },
        cache: "no-store",
      },
    );
    if (r.ok) initial = (await r.json()) as PendingApproval[];
  }

  return (
    <ApprovalsClient
      workspaceId={current?.id ?? ""}
      initial={initial}
    />
  );
}
