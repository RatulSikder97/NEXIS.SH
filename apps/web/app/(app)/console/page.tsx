// Phase 3 Stage 6 — Console Home (Task 6.2).
//
// Server component. Fetches /v1/me and /v1/audit?limit=10 in parallel from
// the control-plane using the request's nexis_session cookie. If the
// cookie is missing, redirect to /sign-in (proxy.ts already enforces this
// for /console/* — we double-check here so the server fetch never runs
// without a session). On a 401/500 from /v1/me we also bounce to /sign-in.
// A failure on the audit fetch degrades gracefully to an empty feed.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { KPICard } from "@/components/console/KPICard";
import { WorkspaceCard } from "@/components/workspaces/WorkspaceCard";
import type { MeResp } from "@/lib/auth";
import type { Workspace } from "@/lib/workspaces";

// Container-network URL for the control-plane (API_URL_INTERNAL) takes
// precedence over the browser-facing NEXT_PUBLIC_API_URL. In pure-localhost
// dev they collapse to the same value.
const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

// AuditRow mirrors the JSON the /v1/audit handler returns (snake_case
// per the Go struct tags). Metadata is opaque JSON.
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

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
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

  // Resolve "current workspace" the same way the console layout does:
  // cookie first, then first ready, then first row. Layout already
  // guarantees workspaces.length > 0 before this page renders, so we
  // don't need to handle the empty case.
  const ws: Workspace[] = wsRes.ok ? ((await wsRes.json()) as Workspace[]) : [];
  const currentCookie = c.get("nexis_workspace");
  const currentWs =
    ws.find((w) => w.id === currentCookie?.value) ??
    ws.find((w) => w.status === "ready") ??
    ws[0];

  const firstName = me.user.email.split("@")[0];
  const greeting = greetingForHour(new Date().getHours());

  return (
    <div className="space-y-8">
      <div>
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Home
        </p>
        <h1 className="mt-1 text-3xl font-semibold text-[var(--color-foreground)]">
          {greeting}, {firstName}
        </h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          {me.org.name} · <span className="font-mono">{me.org.slug}</span> ·{" "}
          <span className="uppercase">{me.role}</span>
        </p>
      </div>

      <section aria-labelledby="overview-heading" className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <h2 id="overview-heading" className="sr-only">
          Tenant overview
        </h2>
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Organization
          </p>
          <h2 className="mt-1 truncate text-lg font-semibold text-[var(--color-foreground)]">
            {me.org.name}
          </h2>
          <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
            <span className="font-mono">{me.org.slug}</span> ·{" "}
            <span className="uppercase">{me.role}</span>
          </p>
          <p className="mt-2 text-xs text-[var(--color-muted-foreground)]">
            {me.user.email}
          </p>
        </div>
        {currentWs && (
          <WorkspaceCard workspace={currentWs} isOwner={me.role === "owner"} />
        )}
      </section>

      <section aria-labelledby="kpis-heading">
        <h2 id="kpis-heading" className="sr-only">
          Key metrics
        </h2>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <KPICard label="Open Incidents" value="—" />
          <KPICard label="Pending Approvals" value="—" />
          <KPICard label="Agents Online" value="—" />
          <KPICard label="MTTR (7d)" value="—" />
        </div>
      </section>

      <section aria-labelledby="activity-heading" className="space-y-3">
        <div className="flex items-baseline justify-between">
          <h2
            id="activity-heading"
            className="text-lg font-semibold text-[var(--color-foreground)]"
          >
            Recent activity
          </h2>
          <span className="text-xs text-[var(--color-muted-foreground)]">
            {audit.total.toLocaleString()} total events
          </span>
        </div>
        <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
          {audit.rows.length === 0 ? (
            <p className="px-4 py-8 text-center text-sm text-[var(--color-muted-foreground)]">
              No activity yet. As your team uses NEXIS, audit events will appear here.
            </p>
          ) : (
            <table className="w-full text-sm">
              <thead className="bg-[var(--color-muted)]/50 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
                <tr>
                  <th className="px-4 py-2 font-medium">When</th>
                  <th className="px-4 py-2 font-medium">Actor</th>
                  <th className="px-4 py-2 font-medium">Action</th>
                  <th className="px-4 py-2 font-medium">Target</th>
                </tr>
              </thead>
              <tbody>
                {audit.rows.map((row) => (
                  <tr
                    key={row.id}
                    className="border-t border-[var(--color-border)] text-[var(--color-foreground)]"
                  >
                    <td className="whitespace-nowrap px-4 py-2 text-[var(--color-muted-foreground)]">
                      {formatTime(row.created_at)}
                    </td>
                    <td className="px-4 py-2 font-mono text-xs">{row.actor.slice(0, 8)}</td>
                    <td className="px-4 py-2">{row.action}</td>
                    <td className="px-4 py-2 font-mono text-xs text-[var(--color-muted-foreground)]">
                      {row.target}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>
    </div>
  );
}
