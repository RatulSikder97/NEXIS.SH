// Phase 5 Stage 8 — Eval list surface.
//
// Server component shell. Mirrors the resolve-current-workspace pattern
// from the incidents list page so the eval surface is scoped to whichever
// workspace the Topbar shows:
//   1. Read the nexis_session cookie (layout-level auth gate is upstream).
//   2. List workspaces, resolve the current one from nexis_workspace cookie
//      (fall back to first ready, then first row).
//   3. Fetch /v1/workspaces/{ws}/eval to seed the client.
//
// Failures degrade silently to an empty list — EvalClient handles the
// polling refetch so a 5xx blip recovers without a page reload.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { EvalClient } from "./client";
import type { Workspace } from "@/lib/workspaces";
import type { EvalRunSummary } from "@/lib/eval";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function EvalPage() {
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

  let initial: EvalRunSummary[] = [];
  if (current) {
    const r = await fetch(`${API}/v1/workspaces/${current.id}/eval`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    });
    if (r.ok) initial = (await r.json()) as EvalRunSummary[];
  }

  return (
    <EvalClient workspaceId={current?.id ?? ""} initial={initial} />
  );
}
