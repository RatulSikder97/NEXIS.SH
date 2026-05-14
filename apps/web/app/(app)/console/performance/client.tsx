"use client";

// PerformanceClient — KPI strip + per-agent breakdown + activity bar chart.
//
// KPIs:
//   * p50 / p95 / p99 of the slowest agent (we surface "slowest" so the
//     operator gets a worst-case feel; per-agent rows show the rest)
//   * Agent-tier p95 buckets: L1 (Architect/Backend/QA/DevOps/Data), L2
//     (Pathfinder/Synthesiser/Validator), Router (Approval Gate).
//   * Mean tokens/sec across agents (tokens_out / sum of agent durations).
//
// The "stacked bar" is a synthetic per-day activity hint built from the
// recent_runs per agent — split by layer. We render it as a small inline
// bar grid because we don't have a real per-day breakdown without a new
// endpoint, and the rule says "degrade gracefully if not available". The
// component shows the layer-weighted activity distribution.

import * as React from "react";
import {
  Gauge,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { EmptyState } from "@/components/empty-state/EmptyState";
import {
  OperationalSegments,
  type OperationalSegment,
} from "@/components/console/OperationalSegments";
import {
  formatRelative,
  formatTokens,
  formatUSD,
} from "@/lib/agents-format";

export type AgentLayer = "l1" | "l2" | "router" | "detector";
export type AgentStatus = "available" | "degraded" | "disabled";

export type PerformanceAgent = {
  name: string;
  label: string;
  layer: AgentLayer;
  p50_duration_ms: number;
  p95_duration_ms: number;
  recent_runs: number;
  tokens_in: number;
  tokens_out: number;
  cost_cents_exact: number;
  last_seen_at?: string;
  status: AgentStatus;
};

const LAYER_LABEL: Record<AgentLayer, string> = {
  l1: "L1",
  l2: "L2",
  router: "Router",
  detector: "Detector",
};

const LAYER_COLOR: Record<AgentLayer, string> = {
  l1: "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
  l2: "bg-purple-500/15 text-purple-700 ring-purple-500/30 dark:text-purple-300",
  router:
    "bg-amber-500/15 text-amber-700 ring-amber-500/30 dark:text-amber-300",
  detector:
    "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
};

const LAYER_BAR_BG: Record<AgentLayer, string> = {
  l1: "bg-blue-500",
  l2: "bg-purple-500",
  router: "bg-amber-500",
  detector: "bg-emerald-500",
};

function KPICard({
  label,
  value,
  hint,
  icon: Icon,
}: {
  label: string;
  value: string;
  hint?: string;
  icon: LucideIcon;
}) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <div className="flex items-center justify-between">
        <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
          {label}
        </p>
        <Icon className="h-4 w-4 text-[var(--color-muted-foreground)]" />
      </div>
      <p className="mt-2 text-2xl font-semibold text-[var(--color-foreground)]">
        {value}
      </p>
      {hint && (
        <p className="mt-1 text-[11px] text-[var(--color-muted-foreground)]">
          {hint}
        </p>
      )}
    </div>
  );
}

function p99From(agents: PerformanceAgent[]): number {
  // We don't have per-event durations on the page, so we approximate p99
  // as p95 × 1.25 (typical tail multiplier for agent traces in this app).
  // The number is labelled "approx" in the hint so the operator isn't
  // misled.
  let p95max = 0;
  for (const a of agents) p95max = Math.max(p95max, a.p95_duration_ms);
  return Math.round(p95max * 1.25);
}

function layerP95(agents: PerformanceAgent[], layer: AgentLayer): number {
  let max = 0;
  for (const a of agents) {
    if (a.layer !== layer) continue;
    if (a.p95_duration_ms > max) max = a.p95_duration_ms;
  }
  return max;
}

function tokensPerSec(agents: PerformanceAgent[]): number {
  let totalOut = 0;
  let totalSeconds = 0;
  for (const a of agents) {
    totalOut += a.tokens_out;
    totalSeconds += (a.p50_duration_ms * a.recent_runs) / 1000;
  }
  if (totalSeconds <= 0) return 0;
  return totalOut / totalSeconds;
}

function activityBars(agents: PerformanceAgent[]) {
  // Per-layer activity distribution (recent_runs split by layer). We render
  // a single stacked horizontal bar showing the relative share of L1 / L2 /
  // Router / Detector activity over the rollup window.
  const buckets: Record<AgentLayer, number> = {
    l1: 0,
    l2: 0,
    router: 0,
    detector: 0,
  };
  for (const a of agents) buckets[a.layer] += a.recent_runs;
  const total =
    buckets.l1 + buckets.l2 + buckets.router + buckets.detector;
  return { buckets, total };
}

function segmentsFor(agents: PerformanceAgent[]): OperationalSegment[] {
  // Build a "perf segment" per agent so the operator can scan tail latency
  // alongside the run counts.
  return agents
    .slice()
    .sort((a, b) => b.p95_duration_ms - a.p95_duration_ms)
    .map((a) => ({
      label: `${a.label} · ${LAYER_LABEL[a.layer]}`,
      started_at: a.last_seen_at ?? new Date().toISOString(),
      finished_at: a.last_seen_at,
      duration_ms: a.p95_duration_ms,
      status:
        a.status === "available"
          ? "succeeded"
          : a.status === "degraded"
            ? "failed"
            : "skipped",
      detail: `p50 ${a.p50_duration_ms}ms · p95 ${a.p95_duration_ms}ms · ${a.recent_runs} runs · ${formatTokens(
        a.tokens_in + a.tokens_out,
      )} tokens`,
    }));
}

export function PerformanceClient({ agents }: { agents: PerformanceAgent[] }) {
  if (agents.length === 0) {
    return (
      <div className="space-y-6">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Observability
          </p>
          <h1 className="text-2xl font-semibold">Performance</h1>
          <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
            Latency and tokens/sec rolled up across the agent fleet.
          </p>
        </div>
        <EmptyState
          icon={Gauge}
          title="No agent telemetry yet"
          description="Once an agent runs, p50 / p95 / token rate appear here."
        />
      </div>
    );
  }

  let p50max = 0;
  let p95max = 0;
  for (const a of agents) {
    p50max = Math.max(p50max, a.p50_duration_ms);
    p95max = Math.max(p95max, a.p95_duration_ms);
  }
  const p99 = p99From(agents);
  const l1p95 = layerP95(agents, "l1");
  const l2p95 = layerP95(agents, "l2");
  const routerP95 = layerP95(agents, "router");
  const tps = tokensPerSec(agents);
  const { buckets, total } = activityBars(agents);

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Observability
        </p>
        <h1 className="text-2xl font-semibold">Performance</h1>
        <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
          Tail latency, agent-tier p95, and token throughput. Numbers are
          rolled up across every workspace you can see; click an agent to
          drill into its run log.
        </p>
      </div>

      <section
        aria-labelledby="perf-kpi-heading"
        className="grid grid-cols-2 gap-3 lg:grid-cols-5"
      >
        <h2 id="perf-kpi-heading" className="sr-only">
          Performance KPIs
        </h2>
        <KPICard
          label="Fleet p50"
          value={`${p50max}ms`}
          hint="Slowest p50"
          icon={Gauge}
        />
        <KPICard
          label="Fleet p95"
          value={`${p95max}ms`}
          hint="Slowest p95"
          icon={Gauge}
        />
        <KPICard
          label="Fleet p99 (approx)"
          value={`${p99}ms`}
          hint="p95 × 1.25"
          icon={Gauge}
        />
        <KPICard
          label="L1 / L2 / Router p95"
          value={`${l1p95}/${l2p95}/${routerP95}ms`}
          hint="Worst-case per tier"
          icon={Gauge}
        />
        <KPICard
          label="Tokens / sec"
          value={tps > 0 ? tps.toFixed(1) : "—"}
          hint="Out tokens / total time"
          icon={Gauge}
        />
      </section>

      <section
        aria-labelledby="perf-activity-heading"
        className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5"
      >
        <header className="mb-3">
          <h2
            id="perf-activity-heading"
            className="text-sm font-semibold text-[var(--color-foreground)]"
          >
            Activity by tier
          </h2>
          <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
            Recent runs (7d) per agent layer.
          </p>
        </header>
        <div className="space-y-2">
          {(["l1", "l2", "router", "detector"] as const).map((layer) => {
            const count = buckets[layer];
            const pct = total > 0 ? (count / total) * 100 : 0;
            return (
              <div key={layer} className="flex items-center gap-3">
                <span
                  className={cn(
                    "inline-flex w-16 items-center justify-center rounded px-1.5 py-0.5 text-[10px] font-medium ring-1",
                    LAYER_COLOR[layer],
                  )}
                >
                  {LAYER_LABEL[layer]}
                </span>
                <div className="relative h-4 flex-1 overflow-hidden rounded-full bg-[var(--color-muted)]">
                  <div
                    className={cn(
                      "absolute inset-y-0 left-0 transition-all",
                      LAYER_BAR_BG[layer],
                    )}
                    style={{ width: `${Math.min(pct, 100)}%` }}
                    aria-hidden
                  />
                </div>
                <span className="w-16 text-right font-mono text-[11px] text-[var(--color-muted-foreground)]">
                  {count} runs
                </span>
              </div>
            );
          })}
        </div>
      </section>

      <section
        aria-labelledby="perf-table-heading"
        className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]"
      >
        <header className="border-b border-[var(--color-border)] px-5 py-3">
          <h2
            id="perf-table-heading"
            className="text-sm font-semibold text-[var(--color-foreground)]"
          >
            Per-agent rollup
          </h2>
        </header>
        <div className="overflow-x-auto">
        <table className="w-full min-w-[820px] text-sm">
          <thead className="bg-[var(--color-muted)]/40 text-left text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
            <tr>
              <th className="px-3 py-2 font-medium">Agent</th>
              <th className="px-3 py-2 font-medium">Layer</th>
              <th className="px-3 py-2 font-medium">p50</th>
              <th className="px-3 py-2 font-medium">p95</th>
              <th className="px-3 py-2 font-medium">Runs (7d)</th>
              <th className="px-3 py-2 font-medium">Tokens</th>
              <th className="px-3 py-2 font-medium">Cost</th>
              <th className="px-3 py-2 font-medium">Last seen</th>
            </tr>
          </thead>
          <tbody>
            {agents
              .slice()
              .sort((a, b) => b.p95_duration_ms - a.p95_duration_ms)
              .map((a) => (
                <tr
                  key={a.name}
                  className="border-t border-[var(--color-border)] hover:bg-[var(--color-muted)]/20"
                >
                  <td className="px-3 py-2 text-xs">
                    <p className="font-medium text-[var(--color-foreground)]">
                      {a.label}
                    </p>
                    <p className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
                      {a.name}
                    </p>
                  </td>
                  <td className="px-3 py-2">
                    <span
                      className={cn(
                        "inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium ring-1",
                        LAYER_COLOR[a.layer],
                      )}
                    >
                      {LAYER_LABEL[a.layer]}
                    </span>
                  </td>
                  <td className="px-3 py-2 font-mono text-xs text-[var(--color-muted-foreground)]">
                    {a.p50_duration_ms > 0 ? `${a.p50_duration_ms}ms` : "—"}
                  </td>
                  <td className="px-3 py-2 font-mono text-xs text-[var(--color-muted-foreground)]">
                    {a.p95_duration_ms > 0 ? `${a.p95_duration_ms}ms` : "—"}
                  </td>
                  <td className="px-3 py-2 font-mono text-xs text-[var(--color-muted-foreground)]">
                    {a.recent_runs}
                  </td>
                  <td className="px-3 py-2 font-mono text-xs text-[var(--color-muted-foreground)]">
                    {formatTokens(a.tokens_in + a.tokens_out)}
                  </td>
                  <td className="px-3 py-2 font-mono text-xs text-[var(--color-muted-foreground)]">
                    {a.cost_cents_exact > 0
                      ? formatUSD(a.cost_cents_exact)
                      : "—"}
                  </td>
                  <td className="px-3 py-2 font-mono text-xs text-[var(--color-muted-foreground)]">
                    {formatRelative(a.last_seen_at)}
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
        </div>
      </section>

      <OperationalSegments
        title="Operational segments"
        description="Agents sorted by p95 latency descending."
        segments={segmentsFor(agents)}
      />
    </div>
  );
}
