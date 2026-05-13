"use client";

// HeatmapChart — incidents-by-hour-of-day × day-of-week. Recharts doesn't
// ship a heatmap, so we render a plain SVG grid with Tailwind utility
// classes and inline fill levels.
//
// The colour intensity is bucketed (0..4) instead of continuous — sharper
// visual contrast and easier to read at a glance than a smooth gradient.
// The colour stops live in `chartColors().primary` with explicit
// `color-mix` percentages so light + dark mode both look right.

import * as React from "react";

import { useThemedColors } from "./useThemedColors";
import { EmptyChartState } from "./ChartCard";

export type HeatmapChartProps = {
  // matrix[row][col] = count. Rows = day-of-week (0..6), cols = hour (0..23).
  data: number[][];
  max: number;
  xLabels: string[];
  yLabels: string[];
  height?: number;
  ariaLabel?: string;
  emptyMessage?: string;
};

function bucketFor(value: number, max: number): number {
  if (max <= 0 || value <= 0) return 0;
  const pct = value / max;
  if (pct < 0.05) return 0;
  if (pct < 0.25) return 1;
  if (pct < 0.5) return 2;
  if (pct < 0.75) return 3;
  return 4;
}

export function HeatmapChart({
  data,
  max,
  xLabels,
  yLabels,
  height = 240,
  ariaLabel,
  emptyMessage,
}: HeatmapChartProps) {
  const palette = useThemedColors();
  const isEmpty = max <= 0;

  if (isEmpty) {
    return (
      <EmptyChartState
        message={emptyMessage ?? "No activity in this window yet."}
        height={height}
      />
    );
  }

  // The bucket → opacity mapping uses color-mix against the card so the
  // empty cell blends in but the busy cell is solid primary.
  const stops = [
    `color-mix(in srgb, ${palette.primary} 4%, transparent)`,
    `color-mix(in srgb, ${palette.primary} 22%, transparent)`,
    `color-mix(in srgb, ${palette.primary} 42%, transparent)`,
    `color-mix(in srgb, ${palette.primary} 65%, transparent)`,
    palette.primary,
  ];

  const rowCount = data.length;
  const colCount = data[0]?.length ?? 0;
  const rowLabelWidth = 36;
  const xLabelHeight = 18;
  const cellGap = 2;

  return (
    <div
      style={{ width: "100%", minHeight: height }}
      aria-label={ariaLabel ?? "Activity heatmap by hour-of-day and day-of-week"}
      className="relative"
    >
      <div
        className="grid items-stretch"
        style={{
          gridTemplateColumns: `${rowLabelWidth}px 1fr`,
          gridTemplateRows: `${xLabelHeight}px 1fr`,
          gap: 4,
          height,
        }}
      >
        <div />
        <div
          className="flex"
          style={{ paddingLeft: 2 }}
        >
          {xLabels.map((l, i) => {
            const showLabel = i % 3 === 0;
            return (
              <div
                key={l}
                className="flex-1 text-center text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]"
              >
                {showLabel ? l : ""}
              </div>
            );
          })}
        </div>
        <div
          className="flex flex-col"
          style={{ gap: cellGap }}
        >
          {yLabels.map((l) => (
            <div
              key={l}
              className="flex flex-1 items-center justify-end pr-2 text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]"
            >
              {l}
            </div>
          ))}
        </div>
        <div
          className="grid"
          style={{
            gridTemplateColumns: `repeat(${colCount}, minmax(0, 1fr))`,
            gridTemplateRows: `repeat(${rowCount}, minmax(0, 1fr))`,
            gap: cellGap,
          }}
        >
          {data.map((row, rIdx) =>
            row.map((cell, cIdx) => {
              const b = bucketFor(cell, max);
              return (
                <div
                  key={`${rIdx}-${cIdx}`}
                  role="img"
                  aria-label={`${yLabels[rIdx]} ${xLabels[cIdx]} — ${cell} incident${cell === 1 ? "" : "s"}`}
                  title={`${yLabels[rIdx]} ${xLabels[cIdx]} · ${cell} incident${cell === 1 ? "" : "s"}`}
                  className="rounded-sm border border-transparent transition-colors hover:border-[var(--color-primary)]"
                  style={{ background: stops[b], minHeight: 12 }}
                />
              );
            }),
          )}
        </div>
      </div>
      <div className="mt-3 flex items-center justify-end gap-2 text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
        <span>Low</span>
        <div className="flex gap-0.5">
          {stops.map((c, i) => (
            <span
              key={i}
              className="h-3 w-4 rounded-sm border border-[var(--color-border)]"
              style={{ background: c }}
            />
          ))}
        </div>
        <span>High</span>
      </div>
    </div>
  );
}
