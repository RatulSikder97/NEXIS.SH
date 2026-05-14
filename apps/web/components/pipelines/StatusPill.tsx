// Phase 4 Stage 7 — StatusPill.
//
// Compact rounded pill that renders a WorkflowRun status with a colour
// keyed to the lifecycle stage:
//   queued    → amber (waiting on Temporal to pick it up)
//   running   → blue, with a soft pulse so the table catches the eye
//   succeeded → green
//   failed    → red
//   timed_out → red (same UX as failed; deadline reached upstream)
//   cancelled → zinc/gray
//
// Pulse animation is gated by usePrefersReducedMotion so a user with
// reduced-motion preferences sees a static colour swatch.

"use client";

import * as React from "react";

import { cn } from "@/lib/utils";
import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";
import type { WorkflowRunStatus } from "@/lib/pipelines";

type Palette = {
  // bg/fg/ring are passed verbatim to className strings — keep them as
  // literal Tailwind classes so the JIT picks them up at build time.
  bg: string;
  fg: string;
  ring: string;
  // dot is the solid colour used for the lifecycle indicator dot. Kept
  // separate from `fg` because the dot wants a single colour value (not a
  // light/dark text pair) so Tailwind's content scanner sees a clean class.
  dot: string;
  label: string;
};

const PALETTE: Record<WorkflowRunStatus, Palette> = {
  queued: {
    bg: "bg-amber-500/15",
    fg: "text-amber-700 dark:text-amber-300",
    ring: "ring-amber-500/30",
    dot: "bg-amber-500",
    label: "Queued",
  },
  running: {
    bg: "bg-blue-500/15",
    fg: "text-blue-700 dark:text-blue-300",
    ring: "ring-blue-500/40",
    dot: "bg-blue-500",
    label: "Running",
  },
  succeeded: {
    bg: "bg-emerald-500/15",
    fg: "text-emerald-700 dark:text-emerald-300",
    ring: "ring-emerald-500/30",
    dot: "bg-emerald-500",
    label: "Succeeded",
  },
  failed: {
    bg: "bg-red-500/15",
    fg: "text-red-700 dark:text-red-300",
    ring: "ring-red-500/30",
    dot: "bg-red-500",
    label: "Failed",
  },
  timed_out: {
    bg: "bg-red-500/15",
    fg: "text-red-700 dark:text-red-300",
    ring: "ring-red-500/30",
    dot: "bg-red-500",
    label: "Timed out",
  },
  cancelled: {
    bg: "bg-[var(--color-muted)]",
    fg: "text-[var(--color-muted-foreground)]",
    ring: "ring-[var(--color-border)]",
    dot: "bg-[var(--color-muted-foreground)]",
    label: "Cancelled",
  },
};

export function StatusPill({
  status,
  className,
}: {
  status: WorkflowRunStatus;
  className?: string;
}) {
  const reduced = usePrefersReducedMotion();
  const p = PALETTE[status] ?? PALETTE.queued;
  const animate = status === "running" && !reduced;

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1",
        p.bg,
        p.fg,
        p.ring,
        className,
      )}
    >
      <span
        className={cn(
          "inline-block h-1.5 w-1.5 rounded-full",
          p.dot,
          // Tailwind's animate-pulse opacity sweep reads clearly on the
          // dot without distracting the rest of the row.
          animate && "animate-pulse",
        )}
        aria-hidden
      />
      {p.label}
    </span>
  );
}
