"use client";

// DonutChart — categorical proportion with a centred label/value. Used
// for severity breakdown and status distribution. We use Recharts'
// `PieChart` with `innerRadius` to get the donut shape.
//
// The chart is responsive: it fills the available width and uses a
// matching height so the donut stays circular regardless of card size.

import * as React from "react";
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from "recharts";

import { EmptyChartState } from "./ChartCard";

export type DonutSlice = {
  name: string;
  value: number;
  color: string;
};

export type DonutChartProps = {
  data: DonutSlice[];
  centerLabel: string;
  centerValue: string;
  height?: number;
  emptyMessage?: string;
  ariaLabel?: string;
  // valueFormatter is used in the tooltip; defaults to a count.
  valueFormatter?: (n: number) => string;
};

function ChartTooltip({
  active,
  payload,
  total,
  valueFormatter,
}: {
  active?: boolean;
  payload?: Array<{ name: string; value: number; payload: DonutSlice }>;
  total: number;
  valueFormatter?: (n: number) => string;
}) {
  if (!active || !payload || payload.length === 0) return null;
  const slice = payload[0];
  if (!slice) return null;
  const fmt = valueFormatter ?? ((n: number) => String(n));
  const pct = total > 0 ? Math.round((slice.value / total) * 1000) / 10 : 0;
  return (
    <div
      role="tooltip"
      className="rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-2 text-xs shadow-md"
    >
      <p className="flex items-center gap-2 font-medium text-[var(--color-foreground)]">
        <span
          aria-hidden
          className="inline-block h-2 w-2 rounded-sm"
          style={{ background: slice.payload.color }}
        />
        {slice.name}
      </p>
      <p className="mt-0.5 text-[var(--color-muted-foreground)]">
        <span className="font-mono text-[var(--color-foreground)]">
          {fmt(slice.value)}
        </span>{" "}
        <span className="text-[10px] uppercase tracking-widest">
          ({pct.toFixed(1)}%)
        </span>
      </p>
    </div>
  );
}

export function DonutChart({
  data,
  centerLabel,
  centerValue,
  height = 288,
  emptyMessage,
  ariaLabel,
  valueFormatter,
}: DonutChartProps) {
  const total = data.reduce((s, d) => s + (d.value ?? 0), 0);

  if (total === 0) {
    return (
      <EmptyChartState
        message={emptyMessage ?? "No data in this window yet."}
        height={height}
      />
    );
  }

  // Filter slices with zero so they don't show up in the legend rendering
  // we draw under the chart.
  const filtered = data.filter((d) => d.value > 0);

  return (
    <div
      style={{ width: "100%", height }}
      aria-label={ariaLabel ?? `${centerLabel} donut chart`}
      className="relative"
    >
      <ResponsiveContainer width="100%" height="100%">
        <PieChart>
          <Pie
            data={filtered}
            dataKey="value"
            nameKey="name"
            innerRadius="58%"
            outerRadius="82%"
            paddingAngle={2}
            stroke="var(--color-card)"
            strokeWidth={2}
            isAnimationActive={false}
          >
            {filtered.map((d) => (
              <Cell key={d.name} fill={d.color} />
            ))}
          </Pie>
          <Tooltip
            content={
              <ChartTooltip total={total} valueFormatter={valueFormatter} />
            }
          />
        </PieChart>
      </ResponsiveContainer>
      <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center text-center">
        <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
          {centerLabel}
        </p>
        <p className="mt-0.5 text-3xl font-semibold text-[var(--color-foreground)]">
          {centerValue}
        </p>
      </div>
      <ul className="mt-3 flex flex-wrap items-center justify-center gap-x-4 gap-y-1 text-[11px] text-[var(--color-muted-foreground)]">
        {data.map((d) => {
          const pct = total > 0 ? (d.value / total) * 100 : 0;
          return (
            <li key={d.name} className="flex items-center gap-1.5">
              <span
                aria-hidden
                className="inline-block h-2 w-2 rounded-sm"
                style={{ background: d.color }}
              />
              <span>{d.name}</span>
              <span className="font-mono text-[var(--color-foreground)]">
                {d.value}
              </span>
              <span className="text-[10px]">({pct.toFixed(0)}%)</span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
