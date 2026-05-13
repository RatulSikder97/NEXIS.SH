"use client";

// KpiCard — hero stat tile used at the top of the dashboard / incidents
// pages. Combines:
//   * Eyebrow label (uppercase, muted)
//   * Big numeric value
//   * Optional inline delta chip ("+12.4%" vs the prior period)
//   * Sparkline of the underlying series
//   * Optional href so the whole card is a navigation target
//
// The visual rhythm is tuned for a 4-card row: ~h-32 sparkline, generous
// vertical padding, hover surface that lifts the card a touch.

import * as React from "react";
import Link from "next/link";
import { ArrowDownRight, ArrowRight, ArrowUpRight } from "lucide-react";

import { cn } from "@/lib/utils";
import { Sparkline } from "./Sparkline";

export type KpiCardProps = {
  label: string;
  value: string;
  hint?: string;
  href?: string;
  // delta is a percent change; pass undefined to hide the chip.
  delta?: number;
  // higherIsBetter flips the delta colour mapping. Default: false
  // (incidents/spend going up is bad).
  higherIsBetter?: boolean;
  sparkline: number[];
  // Sparkline tint. Defaults to --color-primary.
  color?: string;
  // ariaLabel describes what the card represents end-to-end.
  ariaLabel?: string;
  className?: string;
};

function formatDelta(v: number): string {
  const sign = v > 0 ? "+" : "";
  return `${sign}${v.toFixed(1)}%`;
}

function deltaTone(
  delta: number,
  higherIsBetter: boolean,
): "good" | "bad" | "flat" {
  if (Math.abs(delta) < 0.5) return "flat";
  if (delta > 0) return higherIsBetter ? "good" : "bad";
  return higherIsBetter ? "bad" : "good";
}

function DeltaChip({
  delta,
  higherIsBetter,
}: {
  delta: number;
  higherIsBetter: boolean;
}) {
  const tone = deltaTone(delta, higherIsBetter);
  const map: Record<typeof tone, string> = {
    good:
      "bg-emerald-500/10 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
    bad: "bg-red-500/10 text-red-700 ring-red-500/30 dark:text-red-300",
    flat:
      "bg-[var(--color-muted)]/40 text-[var(--color-muted-foreground)] ring-[var(--color-border)]",
  };
  const Icon =
    Math.abs(delta) < 0.5
      ? ArrowRight
      : delta > 0
      ? ArrowUpRight
      : ArrowDownRight;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium ring-1",
        map[tone],
      )}
    >
      <Icon className="h-3 w-3" />
      {formatDelta(delta)}
    </span>
  );
}

function KpiBody({
  label,
  value,
  hint,
  delta,
  higherIsBetter,
  sparkline,
  color,
}: Pick<
  KpiCardProps,
  "label" | "value" | "hint" | "delta" | "higherIsBetter" | "sparkline" | "color"
>) {
  return (
    <>
      <div className="flex items-center justify-between gap-2">
        <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
          {label}
        </p>
        {typeof delta === "number" && (
          <DeltaChip delta={delta} higherIsBetter={higherIsBetter ?? false} />
        )}
      </div>
      <div className="mt-2 flex items-baseline gap-2">
        <p className="text-3xl font-semibold text-[var(--color-foreground)]">
          {value}
        </p>
      </div>
      <div className="mt-3 flex items-end justify-between gap-3">
        {hint ? (
          <p className="text-xs text-[var(--color-muted-foreground)]">{hint}</p>
        ) : (
          <span />
        )}
        <Sparkline
          data={sparkline}
          width={120}
          height={32}
          color={color ?? "var(--color-primary)"}
          ariaLabel={`${label} trend sparkline`}
        />
      </div>
    </>
  );
}

export function KpiCard({
  label,
  value,
  hint,
  href,
  delta,
  higherIsBetter,
  sparkline,
  color,
  ariaLabel,
  className,
}: KpiCardProps) {
  const shell =
    "block rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-5 transition-shadow hover:shadow-sm";
  const hoverable = href
    ? "hover:border-[var(--color-primary)]/50"
    : "";
  const body = (
    <KpiBody
      label={label}
      value={value}
      hint={hint}
      delta={delta}
      higherIsBetter={higherIsBetter}
      sparkline={sparkline}
      color={color}
    />
  );

  if (href) {
    return (
      <Link
        href={href as never}
        aria-label={ariaLabel ?? label}
        className={cn(shell, hoverable, "group", className)}
      >
        {body}
      </Link>
    );
  }
  return (
    <div
      aria-label={ariaLabel ?? label}
      className={cn(shell, className)}
    >
      {body}
    </div>
  );
}
