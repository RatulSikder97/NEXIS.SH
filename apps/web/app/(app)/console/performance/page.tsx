// Performance surface — org-wide latency + agent rollup.
//
// Server-side: pull the agents list for every workspace the operator can
// see and merge into one org-wide view. Each agent row already carries
// p50/p95/recent_runs, so we can compose the KPI strip + stacked bar chart
// from existing data without a new endpoint.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { PerformanceClient, type PerformanceAgent } from "./client";
import { agents, type AgentInfo } from "@/lib/agents";
import type { Workspace } from "@/lib/workspaces";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function PerformancePage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  const wsRes = await fetch(`${API}/v1/workspaces`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  const workspaces: Workspace[] = wsRes.ok ? await wsRes.json() : [];

  // Fan out: list agents per workspace, merge by agent.name.
  const settled = await Promise.all(
    workspaces.map((w) => agents.list(w.id, cookieHeader)),
  );
  const merged = new Map<string, AgentInfo>();
  for (const fleet of settled) {
    for (const a of fleet) {
      const existing = merged.get(a.name);
      if (!existing) {
        merged.set(a.name, { ...a });
        continue;
      }
      // Sum tokens + runs, take max-of last_seen, recompute p50/p95 as
      // weighted average when possible (fall back to the larger value).
      existing.recent_runs += a.recent_runs;
      existing.total_tokens_in += a.total_tokens_in;
      existing.total_tokens_out += a.total_tokens_out;
      existing.total_cost_cents_exact += a.total_cost_cents_exact;
      existing.p50_duration_ms = Math.max(
        existing.p50_duration_ms,
        a.p50_duration_ms,
      );
      existing.p95_duration_ms = Math.max(
        existing.p95_duration_ms,
        a.p95_duration_ms,
      );
      if (a.last_seen_at && (!existing.last_seen_at || a.last_seen_at > existing.last_seen_at)) {
        existing.last_seen_at = a.last_seen_at;
      }
    }
  }

  const out: PerformanceAgent[] = Array.from(merged.values()).map((a) => ({
    name: a.name,
    label: a.label,
    layer: a.layer,
    p50_duration_ms: a.p50_duration_ms,
    p95_duration_ms: a.p95_duration_ms,
    recent_runs: a.recent_runs,
    tokens_in: a.total_tokens_in,
    tokens_out: a.total_tokens_out,
    cost_cents_exact: a.total_cost_cents_exact,
    last_seen_at: a.last_seen_at,
    status: a.status,
  }));

  return <PerformanceClient agents={out} />;
}
