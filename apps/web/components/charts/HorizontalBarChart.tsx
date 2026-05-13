"use client";

// HorizontalBarChart — used by the "Top projects" panel on the incidents
// page. Renders horizontal bars sorted by count with a small severity-tint
// breakdown rendered inline on each bar. Pure SVG/CSS — no recharts so
// we can put per-bar severity stacks under our own control.

import * as React from "react";

import { useThemedColors } from "./useThemedColors";
import { SEVERITY_COLOURS } from "@/lib/chart-theme";
import { EmptyChartState } from "./ChartCard";

export type HorizontalBarRow = {
  project: string;
  count: number;
  severity_high: number;
  severity_medium: number;
  severity_low: number;
};

export type HorizontalBarChartProps = {
  data: HorizontalBarRow[];
  height?: number;
  emptyMessage?: string;
  ariaLabel?: string;
};

export function HorizontalBarChart({
  data,
  height = 288,
  emptyMessage,
  ariaLabel,
}: HorizontalBarChartProps) {
  const palette = useThemedColors();
  if (data.length === 0) {
    return (
      <EmptyChartState
        message={emptyMessage ?? "No data in this window yet."}
        height={height}
      />
    );
  }

  const max = data.reduce((m, r) => Math.max(m, r.count), 0);

  return (
    <div
      style={{ width: "100%", minHeight: height }}
      aria-label={ariaLabel ?? "Top projects by incidents"}
      className="flex flex-col gap-2"
    >
      {data.map((row) => {
        const widthPct = max > 0 ? (row.count / max) * 100 : 0;
        const high = row.severity_high;
        const med = row.severity_medium;
        const low = row.severity_low;
        const unclassified = Math.max(0, row.count - high - med - low);
        const total = Math.max(1, row.count);
        return (
          <div key={row.project} className="space-y-1">
            <div className="flex items-baseline justify-between gap-3 text-xs">
              <span className="truncate font-medium text-[var(--color-foreground)]">
                {row.project}
              </span>
              <span className="font-mono text-[var(--color-muted-foreground)]">
                {row.count}
              </span>
            </div>
            <div
              className="relative h-3 w-full overflow-hidden rounded-full bg-[var(--color-muted)]/40"
              role="img"
              aria-label={`${row.project}: ${row.count} incidents — ${high} high, ${med} medium, ${low} low`}
            >
              <div
                className="absolute inset-y-0 left-0 flex overflow-hidden rounded-full"
                style={{ width: `${widthPct}%` }}
              >
                <div
                  style={{
                    width: `${(high / total) * 100}%`,
                    background: SEVERITY_COLOURS.high,
                  }}
                />
                <div
                  style={{
                    width: `${(med / total) * 100}%`,
                    background: SEVERITY_COLOURS.medium,
                  }}
                />
                <div
                  style={{
                    width: `${(low / total) * 100}%`,
                    background: SEVERITY_COLOURS.low,
                  }}
                />
                <div
                  style={{
                    width: `${(unclassified / total) * 100}%`,
                    background: palette.mutedForeground,
                    opacity: 0.45,
                  }}
                />
              </div>
            </div>
          </div>
        );
      })}
      <div className="mt-2 flex items-center justify-end gap-3 text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
        <span className="flex items-center gap-1">
          <span
            aria-hidden
            className="inline-block h-2 w-2 rounded-sm"
            style={{ background: SEVERITY_COLOURS.high }}
          />
          High
        </span>
        <span className="flex items-center gap-1">
          <span
            aria-hidden
            className="inline-block h-2 w-2 rounded-sm"
            style={{ background: SEVERITY_COLOURS.medium }}
          />
          Medium
        </span>
        <span className="flex items-center gap-1">
          <span
            aria-hidden
            className="inline-block h-2 w-2 rounded-sm"
            style={{ background: SEVERITY_COLOURS.low }}
          />
          Low
        </span>
      </div>
    </div>
  );
}
