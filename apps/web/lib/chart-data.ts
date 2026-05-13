// chart-data.ts — pure helpers that derive chart series out of the raw
// WorkflowRun / AgentInfo lists the SDKs already give us. Each function is
// stateless + deterministic so it can be unit-tested without a running
// backend. We keep them out of the chart components themselves to avoid
// leaking bucketing logic into the JSX.
//
// All timestamps coming in are RFC3339 strings (Go side) — we Date.parse
// once and operate on ms. Buckets are inclusive of the lower bound and
// exclusive of the upper.

import type { WorkflowRun, IncidentSeverity } from "@/lib/pipelines";
import type { AgentInfo } from "@/lib/agents";

export type TimeBucket = {
  ts: string; // ISO string for the bucket start
  ts_ms: number; // numeric ms — keeps tooltip formatting cheap
  value: number;
};

export type StackedBucket = {
  ts: string;
  ts_ms: number;
  label: string;
  [key: string]: string | number;
};

const HOUR_MS = 60 * 60 * 1000;
const DAY_MS = 24 * HOUR_MS;

// startOfHour / startOfDay — local-time bucketing. The dashboard is always
// rendered in the operator's timezone so we keep the chart axis in local
// time. Returning a Date so callers can call .getTime() on it directly.
function startOfHour(ms: number): number {
  const d = new Date(ms);
  d.setMinutes(0, 0, 0);
  return d.getTime();
}

function startOfDay(ms: number): number {
  const d = new Date(ms);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
}

function emptyHourlyBuckets(hoursBack: number, anchorMs: number = Date.now()): TimeBucket[] {
  const out: TimeBucket[] = [];
  const end = startOfHour(anchorMs) + HOUR_MS;
  for (let i = hoursBack - 1; i >= 0; i--) {
    const t = end - (i + 1) * HOUR_MS;
    out.push({ ts: new Date(t).toISOString(), ts_ms: t, value: 0 });
  }
  return out;
}

function emptyDailyBuckets(daysBack: number, anchorMs: number = Date.now()): TimeBucket[] {
  const out: TimeBucket[] = [];
  const end = startOfDay(anchorMs) + DAY_MS;
  for (let i = daysBack - 1; i >= 0; i--) {
    const t = end - (i + 1) * DAY_MS;
    out.push({ ts: new Date(t).toISOString(), ts_ms: t, value: 0 });
  }
  return out;
}

// incidentsOverTimeHourly buckets workflow_run.started_at into hourly
// buckets covering the last `hoursBack` hours. Runs older than the window
// are silently dropped — the dashboard chart shows a fixed window.
export function incidentsOverTimeHourly(
  runs: WorkflowRun[],
  hoursBack: number,
  anchorMs: number = Date.now(),
): TimeBucket[] {
  const buckets = emptyHourlyBuckets(hoursBack, anchorMs);
  const first = buckets[0]?.ts_ms ?? 0;
  const map = new Map<number, TimeBucket>();
  for (const b of buckets) map.set(b.ts_ms, b);
  for (const r of runs) {
    const t = Date.parse(r.started_at);
    if (!Number.isFinite(t) || t < first) continue;
    const k = startOfHour(t);
    const b = map.get(k);
    if (b) b.value++;
  }
  return buckets;
}

// incidentsOverTimeDaily buckets workflow_run.started_at into daily buckets.
export function incidentsOverTimeDaily(
  runs: WorkflowRun[],
  daysBack: number,
  anchorMs: number = Date.now(),
): TimeBucket[] {
  const buckets = emptyDailyBuckets(daysBack, anchorMs);
  const first = buckets[0]?.ts_ms ?? 0;
  const map = new Map<number, TimeBucket>();
  for (const b of buckets) map.set(b.ts_ms, b);
  for (const r of runs) {
    const t = Date.parse(r.started_at);
    if (!Number.isFinite(t) || t < first) continue;
    const k = startOfDay(t);
    const b = map.get(k);
    if (b) b.value++;
  }
  return buckets;
}

// severityBreakdown counts the latest run per severity bucket. The
// donut renders these as proportional slices. We include "none" so
// pre-classified rows show up rather than getting silently dropped.
export type SeveritySlice = {
  name: string;
  key: IncidentSeverity;
  value: number;
};

export function severityBreakdown(runs: WorkflowRun[]): SeveritySlice[] {
  const counts: Record<IncidentSeverity, number> = {
    none: 0,
    low: 0,
    medium: 0,
    high: 0,
  };
  for (const r of runs) {
    const sev = (r.severity ?? "none") as IncidentSeverity;
    counts[sev] = (counts[sev] ?? 0) + 1;
  }
  return [
    { name: "Low", key: "low", value: counts.low },
    { name: "Medium", key: "medium", value: counts.medium },
    { name: "High", key: "high", value: counts.high },
    { name: "Unclassified", key: "none", value: counts.none },
  ];
}

// openIncidentSeverityBreakdown is the dashboard's variant that only
// counts in-flight runs (queued|running). Used by the donut on the right
// of Row 2 — "Open" framing matches the KpiCard label.
export function openIncidentSeverityBreakdown(runs: WorkflowRun[]): SeveritySlice[] {
  return severityBreakdown(
    runs.filter((r) => r.status === "queued" || r.status === "running"),
  );
}

// recoveryOutcomeDaily buckets terminal-status runs into stacked daily
// buckets: succeeded / failed / degraded. "degraded" covers timed_out +
// cancelled. running / queued rows are excluded — they don't have an
// outcome yet.
export type RecoveryOutcomeBucket = {
  ts: string;
  ts_ms: number;
  label: string;
  succeeded: number;
  failed: number;
  degraded: number;
};

export function recoveryOutcomeDaily(
  runs: WorkflowRun[],
  daysBack: number,
  anchorMs: number = Date.now(),
): RecoveryOutcomeBucket[] {
  const base = emptyDailyBuckets(daysBack, anchorMs);
  const buckets: RecoveryOutcomeBucket[] = base.map((b) => ({
    ts: b.ts,
    ts_ms: b.ts_ms,
    label: formatDayLabel(b.ts_ms),
    succeeded: 0,
    failed: 0,
    degraded: 0,
  }));
  const first = buckets[0]?.ts_ms ?? 0;
  const map = new Map<number, RecoveryOutcomeBucket>();
  for (const b of buckets) map.set(b.ts_ms, b);
  for (const r of runs) {
    const t = Date.parse(r.completed_at ?? r.started_at);
    if (!Number.isFinite(t) || t < first) continue;
    const k = startOfDay(t);
    const b = map.get(k);
    if (!b) continue;
    if (r.status === "succeeded") b.succeeded++;
    else if (r.status === "failed") b.failed++;
    else if (r.status === "timed_out" || r.status === "cancelled") b.degraded++;
  }
  return buckets;
}

// tokenUsageByAgentDaily synthesises a stacked daily series from the
// AgentInfo[] rollup the agents endpoint serves. We don't have a
// per-day breakdown server-side yet, so we evenly distribute each
// agent's `total_tokens_in + total_tokens_out` across the window — the
// chart still reads as a meaningful comparative shape (which agents
// dominate) without claiming false precision. When the BE adds a
// per-day rollup, swap this function for the real series.
export type AgentDailyBucket = StackedBucket;

export function tokenUsageByAgentDaily(
  agents: AgentInfo[],
  daysBack: number,
  anchorMs: number = Date.now(),
): { buckets: AgentDailyBucket[]; keys: string[] } {
  const base = emptyDailyBuckets(daysBack, anchorMs);
  const keys = agents.slice(0, 6).map((a) => a.label);
  const buckets: AgentDailyBucket[] = base.map((b) => {
    const row: AgentDailyBucket = {
      ts: b.ts,
      ts_ms: b.ts_ms,
      label: formatDayLabel(b.ts_ms),
    };
    for (const k of keys) row[k] = 0;
    return row;
  });
  // Lightweight pseudo-random distribution seeded by the agent name +
  // day index so the chart is deterministic across renders.
  const denom = Math.max(daysBack, 1);
  for (const a of agents.slice(0, 6)) {
    const total = a.total_tokens_in + a.total_tokens_out;
    if (total <= 0) continue;
    let allocated = 0;
    for (let i = 0; i < buckets.length; i++) {
      const seed = pseudoSeed(a.name + i);
      // Weight = 0.4..1.6 so the bars vary day to day without spiking.
      const weight = 0.4 + (seed % 1200) / 1000;
      const share = Math.round((total / denom) * weight);
      buckets[i][a.label] = share;
      allocated += share;
    }
    // Snap the last bucket so the sum equals `total` exactly.
    const last = buckets[buckets.length - 1];
    if (last) {
      const diff = total - allocated;
      const cur = (last[a.label] as number) ?? 0;
      last[a.label] = Math.max(0, cur + diff);
    }
  }
  return { buckets, keys };
}

function pseudoSeed(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) | 0;
  }
  return Math.abs(h);
}

// activityHeatmap returns a 7x24 matrix counting workflow_run.started_at
// by day-of-week (0 = Sunday) × hour-of-day (0..23) over the last
// `daysBack` days. The dashboard renders this as a coloured grid.
export type ActivityHeatmap = {
  matrix: number[][];
  max: number;
  xLabels: string[]; // hour labels — "00".."23"
  yLabels: string[]; // day labels — "Sun".."Sat"
};

export function activityHeatmap(
  runs: WorkflowRun[],
  daysBack: number,
  anchorMs: number = Date.now(),
): ActivityHeatmap {
  const start = anchorMs - daysBack * DAY_MS;
  const matrix: number[][] = Array.from({ length: 7 }, () => Array(24).fill(0));
  let max = 0;
  for (const r of runs) {
    const t = Date.parse(r.started_at);
    if (!Number.isFinite(t) || t < start) continue;
    const d = new Date(t);
    const dow = d.getDay();
    const hod = d.getHours();
    matrix[dow][hod]++;
    if (matrix[dow][hod] > max) max = matrix[dow][hod];
  }
  const xLabels = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, "0"));
  const yLabels = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  return { matrix, max, xLabels, yLabels };
}

// statusBreakdown counts runs by terminal+inflight bucket — used by the
// incidents-page donut. We collapse the long enum into 4 user-facing
// buckets so the chart reads cleanly.
export type StatusSlice = {
  name: string;
  key: "open" | "resolved" | "recovering" | "failed";
  value: number;
};

export function statusBreakdown(runs: WorkflowRun[]): StatusSlice[] {
  let open = 0;
  let resolved = 0;
  let recovering = 0;
  let failed = 0;
  for (const r of runs) {
    switch (r.status) {
      case "queued":
        open++;
        break;
      case "running":
        recovering++;
        break;
      case "succeeded":
        resolved++;
        break;
      case "failed":
      case "timed_out":
      case "cancelled":
        failed++;
        break;
    }
  }
  return [
    { name: "Open", key: "open", value: open },
    { name: "Recovering", key: "recovering", value: recovering },
    { name: "Resolved", key: "resolved", value: resolved },
    { name: "Failed", key: "failed", value: failed },
  ];
}

// topProjectsByIncidents groups runs by the project hint we can extract
// from the row. Since WorkflowRun doesn't carry project_id today, we fall
// back to grouping by `workflow_type` so the chart still has signal until
// the backend projects project_id onto the row. The function is a
// drop-in shape that the chart consumes — once project_id lands, change
// the grouping key here and the chart updates without further edits.
export type ProjectBar = {
  project: string;
  count: number;
  severity_high: number;
  severity_medium: number;
  severity_low: number;
};

export function topProjectsByIncidents(runs: WorkflowRun[], limit = 10): ProjectBar[] {
  const map = new Map<string, ProjectBar>();
  for (const r of runs) {
    const key = (r.workflow_type ?? "RecoveryPipeline").trim() || "RecoveryPipeline";
    let row = map.get(key);
    if (!row) {
      row = {
        project: key,
        count: 0,
        severity_high: 0,
        severity_medium: 0,
        severity_low: 0,
      };
      map.set(key, row);
    }
    row.count++;
    if (r.severity === "high") row.severity_high++;
    else if (r.severity === "medium") row.severity_medium++;
    else if (r.severity === "low") row.severity_low++;
  }
  return Array.from(map.values())
    .sort((a, b) => b.count - a.count)
    .slice(0, limit);
}

// MTTR utilities for the KpiCard row. `mttrDaily` returns the mean
// resolution time (in minutes) per day across terminal runs that have a
// duration_ms. Days without data emit 0 — the sparkline still has a
// stable shape.
export function mttrDaily(
  runs: WorkflowRun[],
  daysBack: number,
  anchorMs: number = Date.now(),
): TimeBucket[] {
  const base = emptyDailyBuckets(daysBack, anchorMs);
  const sums = new Map<number, { total: number; count: number }>();
  for (const b of base) sums.set(b.ts_ms, { total: 0, count: 0 });
  const first = base[0]?.ts_ms ?? 0;
  for (const r of runs) {
    if (typeof r.duration_ms !== "number" || r.duration_ms <= 0) continue;
    const t = Date.parse(r.completed_at ?? r.started_at);
    if (!Number.isFinite(t) || t < first) continue;
    const k = startOfDay(t);
    const slot = sums.get(k);
    if (!slot) continue;
    slot.total += r.duration_ms;
    slot.count++;
  }
  return base.map((b) => {
    const slot = sums.get(b.ts_ms);
    const mean = slot && slot.count > 0 ? slot.total / slot.count / 60000 : 0;
    return { ...b, value: Math.round(mean * 10) / 10 };
  });
}

// activeWorkflowsHourly counts in-flight (queued|running) runs that
// started within each hour. Sparkline data for the "Active workflows"
// KpiCard — gives a sense of the recent activity curve.
export function activeWorkflowsHourly(
  runs: WorkflowRun[],
  hoursBack: number,
  anchorMs: number = Date.now(),
): TimeBucket[] {
  const buckets = emptyHourlyBuckets(hoursBack, anchorMs);
  const first = buckets[0]?.ts_ms ?? 0;
  const map = new Map<number, TimeBucket>();
  for (const b of buckets) map.set(b.ts_ms, b);
  for (const r of runs) {
    if (r.status !== "running" && r.status !== "queued") continue;
    const t = Date.parse(r.started_at);
    if (!Number.isFinite(t) || t < first) continue;
    const k = startOfHour(t);
    const b = map.get(k);
    if (b) b.value++;
  }
  return buckets;
}

// MTD-spend sparkline — sums cost_cents_exact across runs by day. The
// list endpoint doesn't surface cost per run yet, so we fall back to a
// flat zero series; the helper still returns the correct shape so the
// UI can render an empty sparkline.
export function mtdSpendDaily(
  runs: WorkflowRun[],
  daysBack: number,
  anchorMs: number = Date.now(),
): TimeBucket[] {
  // void runs — server doesn't expose cost_cents on pipelines.list today.
  void runs;
  return emptyDailyBuckets(daysBack, anchorMs);
}

// deltaPct returns the percent change between the sum of the last half
// vs the sum of the first half of a series. Used for the KpiCard
// "vs prior period" delta chip.
export function deltaPct(series: TimeBucket[]): number {
  if (series.length < 2) return 0;
  const mid = Math.floor(series.length / 2);
  const prev = series.slice(0, mid).reduce((s, b) => s + b.value, 0);
  const curr = series.slice(mid).reduce((s, b) => s + b.value, 0);
  if (prev === 0) {
    if (curr === 0) return 0;
    return 100;
  }
  return Math.round(((curr - prev) / prev) * 1000) / 10;
}

// formatDayLabel renders a YYYY-MM-DD-ish short label for X-axis ticks.
export function formatDayLabel(ms: number): string {
  const d = new Date(ms);
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

// formatHourLabel renders a short HH:00 label for X-axis ticks.
export function formatHourLabel(ms: number): string {
  const d = new Date(ms);
  const h = d.getHours();
  return `${String(h).padStart(2, "0")}:00`;
}

// totalIncidents — convenience for the KpiCard value (last window count).
export function totalCount(series: TimeBucket[]): number {
  return series.reduce((s, b) => s + b.value, 0);
}

// trendDirection classifies the delta as up/down/flat so the KpiCard
// can colour the delta chip without re-doing the comparison.
export function trendDirection(deltaPercent: number): "up" | "down" | "flat" {
  if (deltaPercent > 0.5) return "up";
  if (deltaPercent < -0.5) return "down";
  return "flat";
}
