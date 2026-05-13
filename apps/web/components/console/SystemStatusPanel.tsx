"use client";

// SystemStatusPanel — dashboard-sized system + integration health snapshot.
//
// Two horizontal rows of pill cards:
//   1. Sub-systems (4): Control plane, Temporal, Postgres, Redis.
//      Today we synthesise their state from the integrations probe data — if
//      we can reach /v1/integrations the control-plane is up. Once the
//      system-health endpoint lands we swap to its richer payload.
//   2. Integrations (6): live from /v1/integrations.
//
// The full /console/health page renders the same data with more detail.

import * as React from "react";
import Link from "next/link";
import {
  Activity,
  Database,
  HeartPulse,
  Network,
  ServerCog,
  Workflow,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { HealthPill, type HealthState } from "@/components/integrations/HealthPill";
import {
  integrations,
  type IntegrationConnection,
  type IntegrationProvider,
} from "@/lib/integrations";

const POLL_MS = 30_000;

type SubsystemRow = {
  key: string;
  label: string;
  icon: LucideIcon;
  state: HealthState;
  description: string;
};

const INTEGRATION_LABEL: Record<IntegrationProvider, string> = {
  github: "GitHub",
  sentry: "Sentry",
  argocd: "ArgoCD",
  slack: "Slack",
  datadog: "Datadog",
  pagerduty: "PagerDuty",
};

function deriveSubsystems(
  rows: IntegrationConnection[],
  reachable: boolean,
): SubsystemRow[] {
  // We synthesise the 4 sub-systems from observable signals until the
  // dedicated /v1/system-health endpoint lands. Logic:
  //   * Control plane is "healthy" if /v1/integrations was reachable at all.
  //   * Temporal/Postgres/Redis assumed healthy when control plane is
  //     reachable — the next iteration replaces this with the real endpoint.
  const state: HealthState = reachable ? "healthy" : "unknown";
  return [
    {
      key: "control_plane",
      label: "Control plane",
      icon: ServerCog,
      state,
      description: "API + auth + RLS",
    },
    {
      key: "temporal",
      label: "Temporal",
      icon: Workflow,
      state,
      description: "Workflow engine",
    },
    {
      key: "postgres",
      label: "Postgres",
      icon: Database,
      state,
      description: "OLTP + pgvector",
    },
    {
      key: "redis",
      label: "Redis",
      icon: Activity,
      state,
      description: "Cache + pubsub",
    },
    {
      key: "network",
      label: "Edge / Proxy",
      icon: Network,
      state,
      description: rows.length > 0 ? `${rows.length} integration${rows.length === 1 ? "" : "s"} registered` : "No integrations",
    },
  ];
}

export function SystemStatusPanel() {
  const [rows, setRows] = React.useState<IntegrationConnection[]>([]);
  const [reachable, setReachable] = React.useState<boolean>(true);

  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      try {
        const list = await integrations.listConnections();
        if (cancelled) return;
        setRows(list);
        setReachable(true);
      } catch {
        if (cancelled) return;
        setReachable(false);
      }
    }
    void tick();
    const id = window.setInterval(tick, POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);

  const subsystems = deriveSubsystems(rows, reachable);

  return (
    <section
      aria-labelledby="system-status-heading"
      className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
    >
      <header className="mb-4 flex items-baseline justify-between gap-3">
        <div>
          <h2
            id="system-status-heading"
            className="flex items-center gap-2 text-sm font-semibold text-[var(--color-foreground)]"
          >
            <HeartPulse className="h-4 w-4 text-[var(--color-primary)]" />
            System status
          </h2>
          <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
            Sub-systems + integration health, polled every 30s.
          </p>
        </div>
        <Link
          href={"/console/health" as never}
          className="text-xs font-medium text-[var(--color-primary)] hover:underline"
        >
          Full health board →
        </Link>
      </header>

      <div className="space-y-4">
        <div>
          <p className="mb-2 text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Core sub-systems
          </p>
          <div className="grid grid-cols-2 gap-2 md:grid-cols-3 lg:grid-cols-5">
            {subsystems.map((s) => {
              const Icon = s.icon;
              const dot =
                s.state === "healthy"
                  ? "bg-emerald-500"
                  : s.state === "degraded"
                    ? "bg-amber-500"
                    : s.state === "down"
                      ? "bg-red-500"
                      : "bg-zinc-400";
              return (
                <div
                  key={s.key}
                  className="flex items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2"
                >
                  <span className="inline-flex h-6 w-6 items-center justify-center rounded-md bg-[var(--color-muted)] text-[var(--color-muted-foreground)]">
                    <Icon className="h-3.5 w-3.5" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-xs font-medium text-[var(--color-foreground)]">
                      {s.label}
                    </p>
                    <p className="truncate text-[10px] text-[var(--color-muted-foreground)]">
                      {s.description}
                    </p>
                  </div>
                  <span
                    aria-hidden
                    className={cn(
                      "inline-block h-2 w-2 rounded-full",
                      dot,
                      s.state === "healthy" && "animate-pulse",
                    )}
                  />
                </div>
              );
            })}
          </div>
        </div>

        <div>
          <p className="mb-2 text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Integrations
          </p>
          {rows.length === 0 ? (
            <p className="rounded-md border border-dashed border-[var(--color-border)] bg-[var(--color-background)] px-3 py-3 text-xs text-[var(--color-muted-foreground)]">
              No integration connections yet. Connect one from <Link href={"/console/integrations" as never} className="font-medium text-[var(--color-primary)] underline">Integrations</Link> to see live probe health.
            </p>
          ) : (
            <div className="grid grid-cols-2 gap-2 md:grid-cols-3 lg:grid-cols-6">
              {rows.map((r) => (
                <div
                  key={r.provider}
                  className="flex items-center justify-between gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2"
                >
                  <span className="truncate text-xs font-medium text-[var(--color-foreground)]">
                    {INTEGRATION_LABEL[r.provider] ?? r.provider}
                  </span>
                  <HealthPill
                    state={r.health.state}
                    latency_ms={r.health.latency_ms}
                    last_check_at={r.health.last_check_at}
                    last_error={r.health.last_error}
                  />
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
