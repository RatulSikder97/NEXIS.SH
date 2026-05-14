"use client";

// DashboardCharts — operations dashboard chart island.
//
// Pulls together rows 1..4 of the console home page:
//   Row 1: 4 KpiCards (Open incidents, MTTR, Active workflows, MTD spend)
//   Row 2: Incidents over time (2/3) + Severity breakdown donut (1/3)
//   Row 3: Recovery outcomes stacked bar + Token usage by agent stacked bar
//   Row 4: Activity heatmap (incidents by hour-of-day × day-of-week)
//
// The component receives the workspace id + initial data from the server
// page so the first paint already has signal. After mount it polls every
// 60s — slow enough to be cheap on idle tabs, fast enough to show new
// incidents within a minute on an active operator screen.

import * as React from "react";

import { ChartCard, RangeChip } from "@/components/charts/ChartCard";
import { DonutChart } from "@/components/charts/DonutChart";
import { HeatmapChart } from "@/components/charts/HeatmapChart";
import { KpiCard } from "@/components/charts/KpiCard";
import { LineAreaChart } from "@/components/charts/LineAreaChart";
import { StackedBarChart } from "@/components/charts/StackedBarChart";
import { SEVERITY_COLOURS, chartColors } from "@/lib/chart-theme";
import { agents, type AgentInfo } from "@/lib/agents";
import { pipelines, type WorkflowRun } from "@/lib/pipelines";
import {
  activeWorkflowsHourly,
  activityHeatmap,
  deltaPct,
  formatDayLabel,
  formatHourLabel,
  incidentsOverTimeDaily,
  incidentsOverTimeHourly,
  mtdSpendDaily,
  mttrDaily,
  openIncidentSeverityBreakdown,
  recoveryOutcomeDaily,
  tokenUsageByAgentDaily,
  totalCount,
} from "@/lib/chart-data";

const POLL_MS = 60_000;

export type DashboardChartsProps = {
  workspaceId: string;
  initialRuns: WorkflowRun[];
  initialAgents: AgentInfo[];
};

function useLivePalette() {
  const [palette, setPalette] = React.useState(() => chartColors());
  React.useEffect(() => {
    if (typeof document === "undefined") return;
    // Only update on actual class-list flips; the initializer above
    // already gives us the right palette on mount.
    const obs = new MutationObserver(() => setPalette(chartColors()));
    obs.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    return () => obs.disconnect();
  }, []);
  return palette;
}

function formatMinutes(min: number): string {
  if (!Number.isFinite(min) || min <= 0) return "—";
  if (min < 1) return `${(min * 60).toFixed(0)}s`;
  if (min < 60) return `${min.toFixed(1)}m`;
  const h = Math.floor(min / 60);
  const m = Math.round(min % 60);
  return `${h}h ${m}m`;
}

export function DashboardCharts({
  workspaceId,
  initialRuns,
  initialAgents,
}: DashboardChartsProps) {
  const [runs, setRuns] = React.useState<WorkflowRun[]>(initialRuns);
  const [agentRows, setAgentRows] = React.useState<AgentInfo[]>(initialAgents);
  const palette = useLivePalette();

  React.useEffect(() => {
    if (!workspaceId) return;
    let cancelled = false;
    async function tick() {
      try {
        const [r, a] = await Promise.all([
          pipelines.list(workspaceId, 200).catch(() => [] as WorkflowRun[]),
          agents.list(workspaceId).catch(() => [] as AgentInfo[]),
        ]);
        if (cancelled) return;
        setRuns(r);
        setAgentRows(a);
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
  }, [workspaceId]);

  // ---------- Row 1: KPI sparklines ----------
  const incidents14d = React.useMemo(
    () => incidentsOverTimeDaily(runs, 14),
    [runs],
  );
  const incidents28d = React.useMemo(
    () => incidentsOverTimeDaily(runs, 28),
    [runs],
  );
  const mttr7d = React.useMemo(() => mttrDaily(runs, 7), [runs]);
  const active24h = React.useMemo(
    () => activeWorkflowsHourly(runs, 24),
    [runs],
  );
  const spend14d = React.useMemo(() => mtdSpendDaily(runs, 14), [runs]);

  const openIncidents = runs.filter(
    (r) => r.status === "queued" || r.status === "running",
  ).length;
  const activeNow = openIncidents;

  // For the delta we compare last-14d vs prior-14d using the 28d series.
  const openDelta = React.useMemo(
    () => deltaPct(incidents28d.slice(-28)),
    [incidents28d],
  );

  const mttrLast = mttr7d.length > 0 ? mttr7d[mttr7d.length - 1].value : 0;
  const mttrPrev =
    mttr7d.length > 1 ? mttr7d.slice(0, -1).reduce((s, b) => s + b.value, 0) / Math.max(1, mttr7d.length - 1) : 0;
  const mttrDelta =
    mttrPrev > 0
      ? Math.round(((mttrLast - mttrPrev) / mttrPrev) * 1000) / 10
      : 0;

  const activeDelta = React.useMemo(
    () => deltaPct(active24h),
    [active24h],
  );

  // ---------- Row 2 ----------
  const incidentsHourly7d = React.useMemo(
    () => incidentsOverTimeHourly(runs, 24 * 7),
    [runs],
  );
  const severitySlices = React.useMemo(() => {
    const breakdown = openIncidentSeverityBreakdown(runs);
    return breakdown.map((b) => ({
      name: b.name,
      value: b.value,
      color:
        b.key === "high"
          ? SEVERITY_COLOURS.high
          : b.key === "medium"
          ? SEVERITY_COLOURS.medium
          : b.key === "low"
          ? SEVERITY_COLOURS.low
          : SEVERITY_COLOURS.none,
    }));
  }, [runs]);
  const severityTotal = severitySlices.reduce((s, d) => s + d.value, 0);

  // ---------- Row 3 ----------
  const recovery14d = React.useMemo(
    () => recoveryOutcomeDaily(runs, 14),
    [runs],
  );
  const tokenUsage = React.useMemo(
    () => tokenUsageByAgentDaily(agentRows, 14),
    [agentRows],
  );

  // ---------- Row 4 ----------
  const heatmap = React.useMemo(() => activityHeatmap(runs, 30), [runs]);

  // ---------- KPI deltas + sparkline data shaping ----------
  const incidentsSpark = incidents14d.map((b) => b.value);
  const mttrSpark = mttr7d.map((b) => b.value);
  const activeSpark = active24h.map((b) => b.value);
  const spendSpark = spend14d.map((b) => b.value);

  const totalIncidents14d = totalCount(incidents14d);

  return (
    <section
      aria-labelledby="dashboard-charts-heading"
      className="space-y-4"
    >
      <h2 id="dashboard-charts-heading" className="sr-only">
        Operational metrics
      </h2>

      {/* Row 1 — Hero KPIs */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard
          label="Open incidents"
          value={String(openIncidents)}
          hint={`${totalIncidents14d} total last 14d`}
          href="/console/incidents"
          delta={openDelta}
          higherIsBetter={false}
          sparkline={incidentsSpark}
          color={palette.destructive}
          ariaLabel={`Open incidents · ${openIncidents}`}
        />
        <KpiCard
          label="MTTR · 7d"
          value={formatMinutes(mttrLast)}
          hint="Mean time to recover"
          href="/console/incidents"
          delta={mttrDelta}
          higherIsBetter={false}
          sparkline={mttrSpark.length === 0 ? [0] : mttrSpark}
          color={palette.warning}
          ariaLabel={`MTTR · 7 day mean ${formatMinutes(mttrLast)}`}
        />
        <KpiCard
          label="Active workflows"
          value={String(activeNow)}
          hint={activeNow > 0 ? "Currently running" : "Idle"}
          href="/console/workflows"
          delta={activeDelta}
          higherIsBetter
          sparkline={activeSpark}
          color={palette.primary}
          ariaLabel={`Active workflows · ${activeNow}`}
        />
        <KpiCard
          label="MTD spend"
          value="—"
          hint="No cost data yet"
          href="/console/cost"
          sparkline={spendSpark.length === 0 ? [0] : spendSpark}
          color={palette.success}
          ariaLabel="Month-to-date spend"
        />
      </div>

      {/* Row 2 — incidents-over-time + severity donut */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <ChartCard
            eyebrow="Trend"
            title="Incidents over time"
            description="Hourly buckets across the last 7 days."
            rangeChip={<RangeChip label="Last 7d" />}
            ariaLabel="Hourly incident count over the last 7 days"
          >
            <LineAreaChart
              data={incidentsHourly7d}
              label="Incidents over time"
              color={palette.primary}
              xTickFormatter={(ms) => {
                const d = new Date(ms);
                // For a 7d hourly window we want day labels, but include
                // hour for the first/last tick so the operator can orient.
                return d.toLocaleDateString(undefined, {
                  weekday: "short",
                  day: "numeric",
                });
              }}
              valueFormatter={(n) => String(n)}
              emptyMessage="No incidents in the last 7 days."
            />
          </ChartCard>
        </div>
        <ChartCard
          eyebrow="Distribution"
          title="Severity breakdown"
          description="Open incidents grouped by severity."
          rangeChip={<RangeChip label="Open" />}
          ariaLabel="Severity breakdown of open incidents"
        >
          <DonutChart
            data={severitySlices}
            centerLabel="Open"
            centerValue={String(severityTotal)}
            height={232}
            emptyMessage="No open incidents."
            ariaLabel="Open incidents by severity"
          />
        </ChartCard>
      </div>

      {/* Row 3 — recovery outcomes + token usage */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ChartCard
          eyebrow="Outcomes"
          title="Recovery success rate"
          description="Daily breakdown of recovery pipeline outcomes."
          rangeChip={<RangeChip label="Last 14d" />}
          ariaLabel="Daily recovery outcomes"
        >
          <StackedBarChart
            data={recovery14d}
            keys={["succeeded", "failed", "degraded"]}
            colors={[palette.success, palette.destructive, palette.warning]}
            xKey="label"
            emptyMessage="No completed pipelines in the last 14 days."
          />
        </ChartCard>
        <ChartCard
          eyebrow="Cost telemetry"
          title="Token usage by agent"
          description="Synthesised daily distribution from the 14d rollup."
          rangeChip={<RangeChip label="Last 14d" />}
          ariaLabel="Daily token usage stacked by agent role"
          footer={
            agentRows.length === 0
              ? "Awaiting agent telemetry."
              : "Per-day shape is interpolated from the 14d total — exact daily series ships with the cost endpoint."
          }
        >
          <StackedBarChart
            data={tokenUsage.buckets}
            keys={tokenUsage.keys}
            colors={palette.series}
            xKey="label"
            valueFormatter={(n) => {
              if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
              if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
              return String(n);
            }}
            emptyMessage="No agent telemetry yet."
          />
        </ChartCard>
      </div>

      {/* Row 4 — heatmap */}
      <ChartCard
        eyebrow="Patterns"
        title="Activity heatmap"
        description="Incident volume by hour-of-day and day-of-week, last 30 days."
        rangeChip={<RangeChip label="Last 30d" />}
        ariaLabel="Activity heatmap over the last 30 days"
      >
        <HeatmapChart
          data={heatmap.matrix}
          max={heatmap.max}
          xLabels={heatmap.xLabels}
          yLabels={heatmap.yLabels}
          height={220}
          emptyMessage="No incidents in the last 30 days."
        />
      </ChartCard>
    </section>
  );
}

// Re-export sub-helpers used outside this file (none today). Suppress
// unused warning for formatHourLabel since the dashboard tooltip uses a
// localised formatter inline.
void formatHourLabel;
void formatDayLabel;
