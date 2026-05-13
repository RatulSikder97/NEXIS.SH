// ChartCard — shared shell every dashboard chart sits in.
//
// Provides:
//   * Consistent border + background + padding so the chart row reads as a
//     single cohesive surface.
//   * Header with a tiny uppercase eyebrow + bolder title + optional
//     right-aligned action (time-range chip / "View all" link).
//   * Optional footer for the source / hint string.
//
// The visual quality bar — generous padding, soft shadow on hover, sticky
// header — is what separates this from a plain <div>.

import * as React from "react";

import { cn } from "@/lib/utils";

export type ChartCardProps = {
  eyebrow?: string;
  title: string;
  description?: string;
  rangeChip?: React.ReactNode;
  action?: React.ReactNode;
  footer?: React.ReactNode;
  className?: string;
  children: React.ReactNode;
  // ariaLabel describes what the chart conveys for screen readers.
  ariaLabel?: string;
};

export function ChartCard({
  eyebrow,
  title,
  description,
  rangeChip,
  action,
  footer,
  className,
  children,
  ariaLabel,
}: ChartCardProps) {
  return (
    <section
      aria-label={ariaLabel ?? title}
      className={cn(
        "flex flex-col gap-4 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-5 transition-shadow hover:shadow-sm",
        className,
      )}
    >
      <header className="flex items-start justify-between gap-4">
        <div>
          {eyebrow && (
            <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
              {eyebrow}
            </p>
          )}
          <h3 className="mt-1 text-base font-semibold text-[var(--color-foreground)]">
            {title}
          </h3>
          {description && (
            <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
              {description}
            </p>
          )}
        </div>
        <div className="flex items-center gap-2">
          {rangeChip}
          {action}
        </div>
      </header>
      <div className="flex-1">{children}</div>
      {footer && (
        <div className="border-t border-[var(--color-border)] pt-3 text-[11px] text-[var(--color-muted-foreground)]">
          {footer}
        </div>
      )}
    </section>
  );
}

// RangeChip is a tiny pill used in the header for the "Last 7 days" hint.
export function RangeChip({ label }: { label: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-[var(--color-border)] bg-[var(--color-muted)]/40 px-2.5 py-0.5 text-[10px] font-medium uppercase tracking-widest text-[var(--color-muted-foreground)]">
      {label}
    </span>
  );
}

// EmptyChartState fills the body of a chart card when there's nothing to
// render — keeps the page from collapsing while data loads in.
export function EmptyChartState({
  message,
  height = 288,
}: {
  message: string;
  height?: number;
}) {
  return (
    <div
      style={{ height }}
      className="flex flex-col items-center justify-center gap-2 rounded-md border border-dashed border-[var(--color-border)] bg-[var(--color-muted)]/30"
    >
      <div className="h-px w-20 bg-[var(--color-border)]" />
      <p className="text-xs text-[var(--color-muted-foreground)]">{message}</p>
    </div>
  );
}
