"use client";

// LineAreaChart — time-series line with a soft gradient area underneath.
// Used for "Incidents over time" + "Recoveries last 30d". Renders fully
// responsive via Recharts' `ResponsiveContainer`. The colour palette is
// pulled from `chartColors()` on mount + re-pulled when next-themes
// toggles the document class list so the chart re-paints on theme flip.

import * as React from "react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { EmptyChartState } from "./ChartCard";
import { useThemedColors } from "./useThemedColors";

export type LineAreaPoint = {
  ts: string;
  ts_ms: number;
  value: number;
};

export type LineAreaChartProps = {
  data: LineAreaPoint[];
  label: string;
  color?: string;
  height?: number;
  valueFormatter?: (n: number) => string;
  xTickFormatter?: (ms: number) => string;
  // When all values are zero we render the empty state instead so the
  // chart card doesn't lie about activity.
  emptyMessage?: string;
};

function defaultTickFmt(ms: number): string {
  const d = new Date(ms);
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function ChartTooltip({
  active,
  payload,
  label,
  valueFormatter,
  xTickFormatter,
}: {
  active?: boolean;
  payload?: Array<{ value: number; payload: LineAreaPoint }>;
  label?: number | string;
  valueFormatter?: (n: number) => string;
  xTickFormatter?: (ms: number) => string;
}) {
  if (!active || !payload || payload.length === 0) return null;
  const point = payload[0];
  if (!point) return null;
  const ts =
    typeof point.payload.ts_ms === "number"
      ? point.payload.ts_ms
      : Date.parse(point.payload.ts);
  const ms = typeof label === "number" ? label : ts;
  const fmt = valueFormatter ?? ((n: number) => String(n));
  const x = xTickFormatter ?? defaultTickFmt;
  return (
    <div
      role="tooltip"
      className="rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-2 text-xs shadow-md"
    >
      <p className="font-medium text-[var(--color-foreground)]">
        {x(ms)}
      </p>
      <p className="mt-0.5 text-[var(--color-muted-foreground)]">
        {fmt(point.value)}
      </p>
    </div>
  );
}

export function LineAreaChart({
  data,
  label,
  color,
  height = 288,
  valueFormatter,
  xTickFormatter,
  emptyMessage,
}: LineAreaChartProps) {
  const palette = useThemedColors();
  const stroke = color ?? palette.primary;
  const gradId = React.useId();

  const isEmpty = data.length === 0 || data.every((d) => d.value === 0);
  if (isEmpty) {
    return (
      <EmptyChartState
        message={emptyMessage ?? "No activity in this window yet."}
        height={height}
      />
    );
  }

  return (
    <div style={{ width: "100%", height }} aria-label={`${label} chart`}>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart
          data={data}
          margin={{ top: 8, right: 8, left: 0, bottom: 0 }}
        >
          <defs>
            <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={stroke} stopOpacity={0.35} />
              <stop offset="100%" stopColor={stroke} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid
            stroke={palette.border}
            strokeDasharray="3 3"
            vertical={false}
          />
          <XAxis
            dataKey="ts_ms"
            type="number"
            domain={["dataMin", "dataMax"]}
            tickFormatter={xTickFormatter ?? defaultTickFmt}
            tick={{ fill: palette.mutedForeground, fontSize: 11 }}
            stroke={palette.border}
            tickLine={false}
            axisLine={false}
            minTickGap={32}
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
            cursor={{ stroke: palette.border, strokeWidth: 1 }}
            content={
              <ChartTooltip
                valueFormatter={valueFormatter}
                xTickFormatter={xTickFormatter}
              />
            }
          />
          <Area
            type="monotone"
            dataKey="value"
            stroke={stroke}
            strokeWidth={2}
            fill={`url(#${gradId})`}
            isAnimationActive={false}
            activeDot={{ r: 4, stroke: palette.card, strokeWidth: 2 }}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
