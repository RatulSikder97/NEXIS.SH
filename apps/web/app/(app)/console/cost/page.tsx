// Cost tracker — MTD spend + per-agent + daily stacked bar.
//
// Server side: iterate every workspace and pull /v1/workspaces/{ws}/eval (which
// is the closest stable cost-bearing endpoint we have today) — Phase 5 eval
// run summaries carry `openai_cost_cents_exact` + `ollama_cost_cents_exact`.
// Once /v1/orgs/{org_id}/cost lands we swap the data source.
//
// We also merge in per-agent token + cost totals from /v1/workspaces/{ws}/agents
// so we can attribute spend to L1 vs L2 vs Router rather than provider-only.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { CostClient, type CostAgent, type CostEvalRun } from "./client";
import { agents } from "@/lib/agents";
import type { Workspace } from "@/lib/workspaces";
import type { EvalRunSummary } from "@/lib/eval";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function CostPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  const wsRes = await fetch(`${API}/v1/workspaces`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  const workspaces: Workspace[] = wsRes.ok ? await wsRes.json() : [];

  // Eval runs as a stand-in for "cost bearing runs" until the org-cost
  // endpoint lands.
  const evalSettled = await Promise.all(
    workspaces.map(async (w) => {
      try {
        const r = await fetch(`${API}/v1/workspaces/${w.id}/eval`, {
          headers: { cookie: cookieHeader },
          cache: "no-store",
        });
        if (!r.ok) return [] as EvalRunSummary[];
        return (await r.json()) as EvalRunSummary[];
      } catch {
        return [] as EvalRunSummary[];
      }
    }),
  );
  const evalRuns: CostEvalRun[] = evalSettled.flatMap((rows, i) =>
    rows.map((r) => ({ ...r, workspace_id: workspaces[i].id })),
  );

  // Agents rollup per workspace, merged.
  const fleetSettled = await Promise.all(
    workspaces.map((w) => agents.list(w.id, cookieHeader)),
  );
  const fleetMap = new Map<string, CostAgent>();
  for (const fleet of fleetSettled) {
    for (const a of fleet) {
      const ex = fleetMap.get(a.name);
      if (!ex) {
        fleetMap.set(a.name, {
          name: a.name,
          label: a.label,
          layer: a.layer,
          recent_runs: a.recent_runs,
          tokens_in: a.total_tokens_in,
          tokens_out: a.total_tokens_out,
          cost_cents_exact: a.total_cost_cents_exact,
        });
        continue;
      }
      ex.recent_runs += a.recent_runs;
      ex.tokens_in += a.total_tokens_in;
      ex.tokens_out += a.total_tokens_out;
      ex.cost_cents_exact += a.total_cost_cents_exact;
    }
  }

  return (
    <CostClient
      evalRuns={evalRuns}
      agents={Array.from(fleetMap.values())}
    />
  );
}
