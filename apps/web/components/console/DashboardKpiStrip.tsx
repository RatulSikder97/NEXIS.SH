"use client";

// DashboardKpiStrip — 4-card KPI rail rendered on the console home page.
// Cards: Open incidents, Pending approvals, Active workflows, MTD spend.
//
// Data sources:
//   * pipelines.list(ws) — running/queued count drives Open incidents and
//     Active workflows (same signal, distinct framing for the operator).
//   * approvals.pending(ws) — Pending approvals count.
//   * MTD spend — sums `cost_cents_exact` across the latest 50 pipeline runs
//     started within the current month. Pipeline rows don't carry per-run
//     cost directly today; once `/v1/orgs/{org_id}/cost` lands we swap in
//     the real aggregator. Today we render "—" if no data.
//
// All KPIs poll at 30s and degrade silently when the workspace cookie is
// absent or the endpoint returns non-2xx.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import {
  AlertTriangle,
  CheckSquare,
  DollarSign,
  Workflow,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { pipelines, type WorkflowRun } from "@/lib/pipelines";
import { approvals } from "@/lib/approvals";

const COOKIE_WORKSPACE = "nexis_workspace";
const POLL_MS = 30_000;

function readWorkspaceCookie(): string | null {
  if (typeof document === "undefined") return null;
  const m = document.cookie.match(
    new RegExp(
      "(?:^|; )" +
        COOKIE_WORKSPACE.replace(/[.$?*|{}()[\]\\/+^]/g, "\\$&") +
        "=([^;]*)",
    ),
  );
  return m ? decodeURIComponent(m[1]) : null;
}

type Stats = {
  open_incidents: number;
  pending_approvals: number;
  active_workflows: number;
  // MTD spend in dollars. -1 sentinel = "no data".
  mtd_spend_usd: number;
};

const ZERO: Stats = {
  open_incidents: 0,
  pending_approvals: 0,
  active_workflows: 0,
  mtd_spend_usd: -1,
};

function computeStats(runs: WorkflowRun[], pendingCount: number): Stats {
  let active = 0;
  let openIncidents = 0;
  const now = new Date();
  const monthStart = new Date(now.getFullYear(), now.getMonth(), 1).getTime();
  // We don't currently surface per-run cost on the list, so MTD stays "no
  // data" (-1) until the cost endpoint lands. We still iterate so we can
  // compute the active counts without a second pass.
  for (const r of runs) {
    if (r.status === "running" || r.status === "queued") {
      active++;
      openIncidents++;
    }
    const t = Date.parse(r.started_at);
    if (Number.isFinite(t) && t < monthStart) continue;
  }
  return {
    open_incidents: openIncidents,
    pending_approvals: pendingCount,
    active_workflows: active,
    mtd_spend_usd: -1,
  };
}

function KpiCard({
  label,
  value,
  href,
  icon: Icon,
  tone,
  hint,
}: {
  label: string;
  value: string;
  href: string;
  icon: LucideIcon;
  tone: "default" | "warn" | "danger" | "info";
  hint?: string;
}) {
  const toneClass: Record<typeof tone, string> = {
    default: "text-[var(--color-foreground)]",
    warn: "text-amber-600 dark:text-amber-400",
    danger: "text-red-600 dark:text-red-400",
    info: "text-blue-600 dark:text-blue-400",
  };
  return (
    <Link
      href={href as Route}
      className="group block rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4 transition-colors hover:border-[var(--color-primary)]/40 hover:bg-[var(--color-muted)]/30"
    >
      <div className="flex items-center justify-between">
        <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
          {label}
        </p>
        <Icon
          className={cn(
            "h-4 w-4 text-[var(--color-muted-foreground)] group-hover:text-[var(--color-primary)]",
          )}
        />
      </div>
      <p className={cn("mt-2 text-3xl font-semibold", toneClass[tone])}>
        {value}
      </p>
      {hint && (
        <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
          {hint}
        </p>
      )}
    </Link>
  );
}

export function DashboardKpiStrip() {
  const [stats, setStats] = React.useState<Stats>(ZERO);

  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      const wsId = readWorkspaceCookie();
      if (!wsId) {
        if (!cancelled) setStats(ZERO);
        return;
      }
      try {
        const [runs, pending] = await Promise.all([
          pipelines.list(wsId, 50).catch(() => [] as WorkflowRun[]),
          approvals.pending(wsId).catch(() => []),
        ]);
        if (cancelled) return;
        setStats(computeStats(runs, pending.length));
      } catch {
        // ignore — next tick retries
      }
    }
    void tick();
    const id = window.setInterval(tick, POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);

  return (
    <section aria-labelledby="kpis-heading">
      <h2 id="kpis-heading" className="sr-only">
        Operational KPIs
      </h2>
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <KpiCard
          label="Open incidents"
          value={String(stats.open_incidents)}
          href="/console/incidents"
          icon={AlertTriangle}
          tone={stats.open_incidents > 0 ? "danger" : "default"}
          hint={stats.open_incidents > 0 ? "Active runs" : "All clear"}
        />
        <KpiCard
          label="Pending approvals"
          value={String(stats.pending_approvals)}
          href="/console/approvals"
          icon={CheckSquare}
          tone={stats.pending_approvals > 0 ? "warn" : "default"}
          hint={
            stats.pending_approvals > 0 ? "Operator review needed" : "Nothing waiting"
          }
        />
        <KpiCard
          label="Active workflows"
          value={String(stats.active_workflows)}
          href="/console/workflows"
          icon={Workflow}
          tone={stats.active_workflows > 0 ? "info" : "default"}
          hint={stats.active_workflows > 0 ? "Running" : "Idle"}
        />
        <KpiCard
          label="MTD spend"
          value={stats.mtd_spend_usd >= 0 ? `$${stats.mtd_spend_usd.toFixed(2)}` : "—"}
          href="/console/cost"
          icon={DollarSign}
          tone="default"
          hint={stats.mtd_spend_usd < 0 ? "Awaiting data" : "Month-to-date"}
        />
      </div>
    </section>
  );
}
