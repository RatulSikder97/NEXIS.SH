// Phase 6 Stage 9 — SeverityPill.
//
// List-side severity indicator for the incidents table. Distinct from
// components/approvals/SeverityBadge: that one is keyed to the strict
// ApprovalSeverity (low|medium|high) used inside the approval gate, while
// this pill also handles the pre-classification "none" state — when the
// Synthesiser hasn't fired yet we still want a stable cell width and a
// neutral grey indicator instead of an empty cell.
//
// The visible label is the verb-y "low / medium / high / —" so the column
// reads cleanly at a glance; the dot reinforces the colour for users with
// imperfect colour vision.
//
// Also exports the `awaitingApproval` boolean for the optional
// "Awaiting approval" indicator the parent row appends: true when the
// pipeline is in-flight AND the severity has escalated to medium|high,
// signalling a gate that's blocked on a human.

import * as React from "react";

import { cn } from "@/lib/utils";
import type { IncidentSeverity } from "@/lib/pipelines";

type Palette = {
  bg: string;
  fg: string;
  ring: string;
  dot: string;
  label: string;
};

const PALETTE: Record<IncidentSeverity, Palette> = {
  none: {
    bg: "bg-[var(--color-muted)]",
    fg: "text-[var(--color-muted-foreground)]",
    ring: "ring-[var(--color-border)]",
    dot: "bg-[var(--color-muted-foreground)]/40",
    label: "—",
  },
  low: {
    bg: "bg-emerald-500/15",
    fg: "text-emerald-700 dark:text-emerald-300",
    ring: "ring-emerald-500/30",
    dot: "bg-emerald-500",
    label: "Low",
  },
  medium: {
    bg: "bg-amber-500/15",
    fg: "text-amber-700 dark:text-amber-300",
    ring: "ring-amber-500/30",
    dot: "bg-amber-500",
    label: "Medium",
  },
  high: {
    bg: "bg-red-500/15",
    fg: "text-red-700 dark:text-red-300",
    ring: "ring-red-500/30",
    dot: "bg-red-500",
    label: "High",
  },
};

export function SeverityPill({
  severity,
  className,
}: {
  severity: IncidentSeverity | undefined;
  className?: string;
}) {
  const key: IncidentSeverity = severity ?? "none";
  const p = PALETTE[key] ?? PALETTE.none;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1",
        p.bg,
        p.fg,
        p.ring,
        className,
      )}
      aria-label={`Severity ${p.label}`}
    >
      <span
        className={cn("inline-block h-1.5 w-1.5 rounded-full", p.dot)}
        aria-hidden
      />
      {p.label}
    </span>
  );
}
