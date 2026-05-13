// Console Home — operational dashboard.
//
// Server component shell. We resolve the session + current workspace, then
// fetch four things in parallel:
//   * /v1/me                                        (auth)
//   * /v1/audit?limit=10                            (recent activity feed)
//   * /v1/workspaces                                (workspace list)
//   * /v1/workspaces/{ws}/pipelines?limit=200       (chart series)
//   * /v1/workspaces/{ws}/agents                    (token-usage stacked bars)
//
// The chart island consumes the seed and continues polling client-side on
// a 60s cadence so the dashboard stays live.
//
// Layout (top → bottom):
//   1. Greeting header
//   2. DashboardCharts island
//      - Row 1: 4 KpiCards w/ sparkline + delta
//      - Row 2: Incidents over time + severity donut
//      - Row 3: Recovery outcomes + token usage stacked bars
//      - Row 4: Activity heatmap
//   3. SystemStatusPanel
//   4. ProjectsHealthGrid
//   5. RecentActivityFeed + Organisation/Workspace cards
//   6. QuickActionsRow

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { DashboardCharts } from "@/components/console/DashboardCharts";
import { ProjectsHealthGrid } from "@/components/console/ProjectsHealthGrid";
import { QuickActionsRow } from "@/components/console/QuickActionsRow";
import { RecentActivityFeed } from "@/components/console/RecentActivityFeed";
import { SystemStatusPanel } from "@/components/console/SystemStatusPanel";
import { WorkspaceCard } from "@/components/workspaces/WorkspaceCard";
import type { AgentInfo } from "@/lib/agents";
import type { MeResp } from "@/lib/auth";
import type { WorkflowRun } from "@/lib/pipelines";
import type { Workspace } from "@/lib/workspaces";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

type AuditRow = {
  id: string;
  org_id: string;
  actor: string;
  action: string;
  target: string;
  metadata?: Record<string, unknown> | null;
  created_at: string;
};

type AuditResp = {
  rows: AuditRow[];
  total: number;
};

function greetingForHour(hour: number): string {
  if (hour < 12) return "Good morning";
  if (hour < 18) return "Good afternoon";
  return "Good evening";
}

export default async function ConsoleHomePage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");

  const cookieHeader = `nexis_session=${session.value}`;

  const [meRes, auditRes, wsRes] = await Promise.all([
    fetch(`${API}/v1/me`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
    fetch(`${API}/v1/audit?limit=10`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
    fetch(`${API}/v1/workspaces`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    }),
  ]);

  if (!meRes.ok) redirect("/sign-in");
  const me = (await meRes.json()) as MeResp;

  let audit: AuditResp = { rows: [], total: 0 };
  if (auditRes.ok) {
    audit = (await auditRes.json()) as AuditResp;
  }

  const ws: Workspace[] = wsRes.ok ? ((await wsRes.json()) as Workspace[]) : [];
  const currentCookie = c.get("nexis_workspace");
  const currentWs =
    ws.find((w) => w.id === currentCookie?.value) ??
    ws.find((w) => w.status === "ready") ??
    ws[0];

  // Fetch chart seed data only when we have a workspace. Both endpoints
  // fail-soft to empty arrays — the chart island renders the empty
  // states without throwing.
  let runs: WorkflowRun[] = [];
  let agents: AgentInfo[] = [];
  if (currentWs) {
    const [runsRes, agentsRes] = await Promise.all([
      fetch(`${API}/v1/workspaces/${currentWs.id}/pipelines?limit=200`, {
        headers: { cookie: cookieHeader },
        cache: "no-store",
      }).catch(() => null),
      fetch(`${API}/v1/workspaces/${currentWs.id}/agents`, {
        headers: { cookie: cookieHeader },
        cache: "no-store",
      }).catch(() => null),
    ]);
    if (runsRes && runsRes.ok) {
      runs = (await runsRes.json()) as WorkflowRun[];
    }
    if (agentsRes && agentsRes.ok) {
      agents = (await agentsRes.json()) as AgentInfo[];
    }
  }

  const firstName = me.user.email.split("@")[0];
  const greeting = greetingForHour(new Date().getHours());

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-1">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Operations
        </p>
        <h1 className="text-2xl font-semibold text-[var(--color-foreground)]">
          {greeting}, {firstName}
        </h1>
        <p className="text-sm text-[var(--color-muted-foreground)]">
          {me.org.name} · <span className="font-mono">{me.org.slug}</span> ·{" "}
          <span className="uppercase">{me.role}</span>
        </p>
      </div>

      <DashboardCharts
        workspaceId={currentWs?.id ?? ""}
        initialRuns={runs}
        initialAgents={agents}
      />

      <SystemStatusPanel />

      <ProjectsHealthGrid />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <RecentActivityFeed initial={audit.rows} />
        </div>
        <div className="space-y-4">
          <section
            aria-labelledby="org-card-heading"
            className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
          >
            <h2
              id="org-card-heading"
              className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]"
            >
              Organisation
            </h2>
            <p className="mt-1 truncate text-base font-semibold text-[var(--color-foreground)]">
              {me.org.name}
            </p>
            <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
              <span className="font-mono">{me.org.slug}</span> ·{" "}
              <span className="uppercase">{me.role}</span>
            </p>
            <p className="mt-2 truncate text-xs text-[var(--color-muted-foreground)]">
              {me.user.email}
            </p>
          </section>
          {currentWs && (
            <WorkspaceCard workspace={currentWs} isOwner={me.role === "owner"} />
          )}
        </div>
      </div>

      <QuickActionsRow />
    </div>
  );
}
