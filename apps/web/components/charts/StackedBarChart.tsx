"use client";

// StackedBarChart — daily breakdown stacked across N series. Used by:
//   * Recovery success rate (succeeded / failed / degraded)
//   * Token usage by agent (one bar segment per agent)
//
// The component is layout-agnostic: pass a list of `keys` you want
// stacked and parallel `colors`. Each row in `data` must carry every key
// (use 0 for "no value") so Recharts doesn't collapse the stack.

import * as React from "react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { EmptyChartState } from "./ChartCard";
import { useThemedColors } from "./useThemedColors";

export type StackedBarChartProps = {
  data: Array<Record<string, string | number>>;
  keys: string[];
  colors: string[];
  xKey: string; // field used for x-axis label
  height?: number;
  // When true, render a small legend below the bars. Defaults to true.
  legend?: boolean;
  valueFormatter?: (n: number) => string;
  emptyMessage?: string;
  ariaLabel?: string;
};

function ChartTooltip({
  active,
  payload,
  label,
  valueFormatter,
}: {
  active?: boolean;
  payload?: Array<{ value: number; name: string; color: string }>;
  label?: string;
  valueFormatter?: (n: number) => string;
}) {
  if (!active || !payload || payload.length === 0) return null;
  const fmt = valueFormatter ?? ((n: number) => String(n));
  const total = payload.reduce((s, p) => s + (p.value ?? 0), 0);
  return (
    <div
      role="tooltip"
      className="rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-2 text-xs shadow-md"
    >
      <p className="font-medium text-[var(--color-foreground)]">{label}</p>
      <ul className="mt-1 space-y-0.5">
        {payload.map((p) => (
          <li
            key={p.name}
            className="flex items-center justify-between gap-3 text-[var(--color-muted-foreground)]"
          >
            <span className="flex items-center gap-2">
              <span
                aria-hidden
                className="inline-block h-2 w-2 rounded-sm"
                style={{ background: p.color }}
              />
              {p.name}
            </span>
            <span className="font-mono text-[var(--color-foreground)]">
              {fmt(p.value)}
            </span>
          </li>
        ))}
      </ul>
      <p className="mt-1 border-t border-[var(--color-border)] pt-1 text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
        Total{" "}
        <span className="ml-1 font-mono text-[var(--color-foreground)]">
          {fmt(total)}
        </span>
      </p>
    </div>
  );
}

export function StackedBarChart({
  data,
  keys,
  colors,
  xKey,
  height = 288,
  legend = true,
  valueFormatter,
  emptyMessage,
  ariaLabel,
}: StackedBarChartProps) {
  const palette = useThemedColors();

  const isEmpty =
    data.length === 0 ||
    data.every((row) =>
      keys.every((k) => {
        const v = row[k];
        return typeof v !== "number" || v === 0;
      }),
    );

  if (isEmpty) {
    return (
      <EmptyChartState
        message={emptyMessage ?? "No data in this window yet."}
        height={height}
      />
    );
  }

  return (
    <div
      style={{ width: "100%", height }}
      aria-label={ariaLabel ?? "Stacked bar chart"}
    >
      <ResponsiveContainer width="100%" height="100%">
        <BarChart
          data={data}
          margin={{ top: 8, right: 8, left: 0, bottom: 0 }}
          barCategoryGap="22%"
        >
          <CartesianGrid
            stroke={palette.border}
            strokeDasharray="3 3"
            vertical={false}
          />
          <XAxis
            dataKey={xKey}
            tick={{ fill: palette.mutedForeground, fontSize: 11 }}
            stroke={palette.border}
            tickLine={false}
            axisLine={false}
            minTickGap={20}
          />
          <YAxis
            tick={{ fill: palette.mutedForeground, fontSize: 11 }}
            stroke={palette.border}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) =>
              valueFormatter ? valueFormatter(Number(v)) : String(v)
            }
            width={36}
          />
          <Tooltip
            cursor={{ fill: palette.mutedForeground, fillOpacity: 0.06 }}
            content={<ChartTooltip valueFormatter={valueFormatter} />}
          />
          {legend && (
            <Legend
              iconType="circle"
              iconSize={8}
              wrapperStyle={{ fontSize: 11, color: palette.mutedForeground }}
            />
          )}
          {keys.map((k, i) => (
            <Bar
              key={k}
              dataKey={k}
              stackId="a"
              fill={colors[i] ?? palette.series[i % palette.series.length]}
              radius={
                i === keys.length - 1
                  ? ([4, 4, 0, 0] as [number, number, number, number])
                  : 0
              }
              isAnimationActive={false}
            />
          ))}
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
