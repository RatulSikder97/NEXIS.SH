// Phase 4 Stage 7 — Pipeline run detail page.
//
// Server component shell. Reads the run id from the route param (Next 16 hands
// params as a Promise — we await before reading), resolves the current
// workspace the same way the list page does, and fetches the run snapshot
// + initial activity events for the seed render.
//
// On 404 from the control-plane we fall back to rendering with empty seed
// state — the client SSE will still attempt to connect and surface a clearer
// error inline if the run truly doesn't exist or belongs to another tenant.

import { cookies } from "next/headers";
import { notFound, redirect } from "next/navigation";

import { TimelineClient } from "./client";
import type { Workspace } from "@/lib/workspaces";
import type { PipelineDetail } from "@/lib/pipelines";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function PipelineRunPage({
  params,
}: {
  // Next 16 hands route params as a Promise; await before reading .id.
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
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

  if (!current) notFound();

  const r = await fetch(`${API}/v1/workspaces/${current.id}/pipelines/${id}`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  if (r.status === 404) notFound();

  let initial: PipelineDetail | null = null;
  if (r.ok) {
    initial = (await r.json()) as PipelineDetail;
  }

  return (
    <TimelineClient
      workspaceId={current.id}
      runId={id}
      initial={initial}
    />
  );
}
