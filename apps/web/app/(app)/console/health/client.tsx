"use client";

// HealthClient — sub-systems status grid + integration health board.
//
// Renders the system-health endpoint output if available, otherwise shows
// an EmptyState card for that half. Integration board uses HealthPill +
// last-check + last-error. Polls everything at 30s.

import * as React from "react";
import {
  AlertOctagon,
  Database,
  HardDrive,
  HeartPulse,
  Network,
  ServerCog,
  Workflow,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { EmptyState } from "@/components/empty-state/EmptyState";
import {
  OperationalSegments,
  type OperationalSegment,
  type SegmentStatus,
} from "@/components/console/OperationalSegments";
import { HealthPill, type HealthState } from "@/components/integrations/HealthPill";
import {
  integrations,
  type IntegrationConnection,
  type IntegrationProvider,
} from "@/lib/integrations";
import { formatRelative } from "@/lib/agents-format";

export type SubsystemRow = {
  key: string;
  label: string;
  state: HealthState;
  latency_ms?: number;
  last_check_at?: string;
  last_error?: string;
  detail?: string;
};

const SUBSYSTEM_ICON: Record<string, LucideIcon> = {
  control_plane: ServerCog,
  controlplane: ServerCog,
  temporal: Workflow,
  postgres: Database,
  redis: Network,
  neo4j: Network,
  minio: HardDrive,
};

const INTEGRATION_LABEL: Record<IntegrationProvider, string> = {
  github: "GitHub",
  sentry: "Sentry",
  argocd: "ArgoCD",
  slack: "Slack",
  datadog: "Datadog",
  pagerduty: "PagerDuty",
};

const POLL_MS = 30_000;

function SubsystemCard({ row }: { row: SubsystemRow }) {
  const Icon = SUBSYSTEM_ICON[row.key] ?? ServerCog;
  const dot =
    row.state === "healthy"
      ? "bg-emerald-500"
      : row.state === "degraded"
        ? "bg-amber-500"
        : row.state === "down"
          ? "bg-red-500"
          : "bg-[var(--color-muted-foreground)]/40";
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <div className="flex items-center justify-between">
        <span className="inline-flex h-8 w-8 items-center justify-center rounded-md bg-[var(--color-muted)] text-[var(--color-muted-foreground)]">
          <Icon className="h-4 w-4" />
        </span>
        <span
          aria-hidden
          className={cn(
            "inline-block h-2.5 w-2.5 rounded-full",
            dot,
            row.state === "healthy" && "animate-pulse",
          )}
        />
      </div>
      <p className="mt-3 text-sm font-medium text-[var(--color-foreground)]">
        {row.label}
      </p>
      <div className="mt-1 space-y-0.5">
        <p className="text-[11px] font-mono text-[var(--color-muted-foreground)]">
          {typeof row.latency_ms === "number"
            ? `${row.latency_ms}ms latency`
            : "Latency —"}
        </p>
        <p className="text-[11px] text-[var(--color-muted-foreground)]">
          Last check {formatRelative(row.last_check_at)}
        </p>
        {row.last_error && (
          <p className="mt-1 truncate text-[11px] text-red-700 dark:text-red-300">
            {row.last_error}
          </p>
        )}
      </div>
    </div>
  );
}

function segmentsFor(
  subsystems: SubsystemRow[],
  connections: IntegrationConnection[],
): OperationalSegment[] {
  const subSegs: OperationalSegment[] = subsystems.map((s) => ({
    label: s.label,
    started_at: s.last_check_at ?? new Date().toISOString(),
    finished_at: s.last_check_at,
    duration_ms: s.latency_ms,
    status:
      s.state === "healthy"
        ? "succeeded"
        : s.state === "degraded"
          ? "running"
          : s.state === "down"
            ? "failed"
            : "pending",
    detail: s.detail ?? s.last_error ?? `${s.state.toUpperCase()} sub-system probe`,
  }));
  const intSegs: OperationalSegment[] = connections.map((c) => {
    const status: SegmentStatus =
      c.health.state === "healthy"
        ? "succeeded"
        : c.health.state === "degraded"
          ? "running"
          : c.health.state === "down"
            ? "failed"
            : "skipped";
    return {
      label: INTEGRATION_LABEL[c.provider] ?? c.provider,
      started_at: c.health.last_check_at ?? new Date().toISOString(),
      finished_at: c.health.last_check_at,
      duration_ms: c.health.latency_ms,
      status,
      detail: c.health.last_error ?? (c.connected ? `Connected · ${c.health.state}` : "Not connected"),
    };
  });
  return [...subSegs, ...intSegs];
}

export function HealthClient({
  subsystems,
  connections: initialConnections,
}: {
  subsystems: SubsystemRow[] | null;
  connections: IntegrationConnection[];
}) {
  const [connections, setConnections] =
    React.useState<IntegrationConnection[]>(initialConnections);

  React.useEffect(() => {
    let cancelled = false;
    const id = window.setInterval(async () => {
      try {
        const next = await integrations.listConnections();
        if (cancelled) return;
        setConnections(next);
      } catch {
        // ignore
      }
    }, POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Observability
        </p>
        <h1 className="text-2xl font-semibold flex items-center gap-2">
          <HeartPulse className="h-5 w-5 text-[var(--color-primary)]" />
          System health
        </h1>
        <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
          Live status for the control-plane plus every connected integration.
          Sub-system probes refresh every 30s on the backend; integration
          probes refresh in this UI every 30s.
        </p>
      </div>

      <section aria-labelledby="subsystems-heading" className="space-y-3">
        <h2
          id="subsystems-heading"
          className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]"
        >
          Sub-systems
        </h2>
        {subsystems === null ? (
          <EmptyState
            icon={AlertOctagon}
            title="System-health endpoint unavailable"
            description="The control-plane /v1/system-health endpoint isn't online yet. Sub-system probes will appear here once the backend ships this surface."
          />
        ) : subsystems.length === 0 ? (
          <EmptyState
            icon={ServerCog}
            title="No sub-system rows yet"
            description="The endpoint returned an empty list. Configure probes in the control-plane to populate this grid."
          />
        ) : (
          <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-4">
            {subsystems.map((s) => (
              <SubsystemCard key={s.key} row={s} />
            ))}
          </div>
        )}
      </section>

      <section aria-labelledby="integrations-heading" className="space-y-3">
        <h2
          id="integrations-heading"
          className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]"
        >
          Integrations
        </h2>
        {connections.length === 0 ? (
          <EmptyState
            icon={Network}
            title="No integrations connected"
            description="Connect a provider from the Integrations page to see live probe data."
          />
        ) : (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
            {connections.map((c) => (
              <div
                key={c.provider}
                className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4"
              >
                <div className="flex items-center justify-between">
                  <p className="text-sm font-medium text-[var(--color-foreground)]">
                    {INTEGRATION_LABEL[c.provider] ?? c.provider}
                  </p>
                  <HealthPill
                    state={c.health.state}
                    latency_ms={c.health.latency_ms}
                    last_check_at={c.health.last_check_at}
                    last_error={c.health.last_error}
                  />
                </div>
                <div className="mt-2 space-y-0.5 text-[11px] text-[var(--color-muted-foreground)]">
                  <p>Last check {formatRelative(c.health.last_check_at)}</p>
                  {c.health.last_error && (
                    <p className="truncate text-red-700 dark:text-red-300">
                      {c.health.last_error}
                    </p>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      <OperationalSegments
        title="Operational segments"
        description="Sub-systems first, integrations second. Status reflects the most recent probe."
        segments={segmentsFor(subsystems ?? [], connections)}
      />
    </div>
  );
}
