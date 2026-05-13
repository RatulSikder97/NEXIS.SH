"use client";

// Sparkline — tiny inline trend line used inside KpiCards. Pure SVG so the
// component doesn't pull in recharts axes/grid for a feature that doesn't
// need them. Renders a path + a soft gradient fill underneath.
//
// All sizes (width, height, stroke) are intentionally fixed defaults so a
// row of KpiCards stays optically aligned even when one of them has no
// data — the empty state still occupies the same pixel box.

import * as React from "react";

import { cn } from "@/lib/utils";

export type SparklineProps = {
  data: number[];
  width?: number;
  height?: number;
  color?: string;
  // The gradient fill sits underneath the line. We accept a separate
  // fillColor in case the caller wants a different tint than the stroke.
  fillColor?: string;
  className?: string;
  ariaLabel?: string;
};

// pathFor builds an SVG `d` attribute for a smooth-ish polyline. We use
// straight segments rather than curves because they read more honestly
// at small sizes (no overshooting peaks).
function pathFor(values: number[], w: number, h: number, pad: number): string {
  if (values.length === 0) return "";
  if (values.length === 1) {
    const y = h / 2;
    return `M ${pad} ${y} L ${w - pad} ${y}`;
  }
  let min = Infinity;
  let max = -Infinity;
  for (const v of values) {
    if (v < min) min = v;
    if (v > max) max = v;
  }
  if (!Number.isFinite(min) || !Number.isFinite(max) || min === max) {
    const y = h / 2;
    return `M ${pad} ${y} L ${w - pad} ${y}`;
  }
  const dx = (w - pad * 2) / (values.length - 1);
  const range = max - min;
  let d = "";
  for (let i = 0; i < values.length; i++) {
    const x = pad + i * dx;
    const norm = (values[i] - min) / range;
    const y = h - pad - norm * (h - pad * 2);
    d += i === 0 ? `M ${x} ${y}` : ` L ${x} ${y}`;
  }
  return d;
}

function areaFor(values: number[], w: number, h: number, pad: number): string {
  const line = pathFor(values, w, h, pad);
  if (!line) return "";
  // Close the path back to the baseline so the gradient fill makes sense.
  return `${line} L ${w - pad} ${h - pad} L ${pad} ${h - pad} Z`;
}

export function Sparkline({
  data,
  width = 120,
  height = 36,
  color,
  fillColor,
  className,
  ariaLabel,
}: SparklineProps) {
  const safe = React.useMemo(() => {
    if (!Array.isArray(data)) return [] as number[];
    return data.map((d) => (Number.isFinite(d) ? d : 0));
  }, [data]);

  const gradientId = React.useId();
  const stroke = color ?? "var(--color-primary)";
  const fill = fillColor ?? stroke;
  const isFlat = safe.length === 0 || safe.every((v) => v === safe[0]);

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      width={width}
      height={height}
      preserveAspectRatio="none"
      aria-label={ariaLabel ?? "Trend sparkline"}
      role="img"
      className={cn("overflow-visible", className)}
    >
      <defs>
        <linearGradient id={gradientId} x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" stopColor={fill} stopOpacity={isFlat ? 0.08 : 0.25} />
          <stop offset="100%" stopColor={fill} stopOpacity={0} />
        </linearGradient>
      </defs>
      <path
        d={areaFor(safe, width, height, 2)}
        fill={`url(#${gradientId})`}
        stroke="none"
      />
      <path
        d={pathFor(safe, width, height, 2)}
        fill="none"
        stroke={stroke}
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
        opacity={isFlat ? 0.45 : 1}
      />
    </svg>
  );
}
