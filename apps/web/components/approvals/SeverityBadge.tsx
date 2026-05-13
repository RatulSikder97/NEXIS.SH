// Phase 6 Stage 9 — SeverityBadge.
//
// Compact rounded pill that renders an ApprovalSeverity with a colour
// keyed to the risk bucket:
//   low    → emerald (auto-approved, no review needed)
//   medium → amber   (timeout-to-approve in 2 min)
//   high   → red     (human decision required)
//
// Mirrors the StatusPill shape from Phase 4 so the approvals table reads
// visually consistent with the incidents list.

import * as React from "react";

import { cn } from "@/lib/utils";
import {
  SEVERITY_LABEL,
  type ApprovalSeverity,
} from "@/lib/approvals";

type Palette = {
  bg: string;
  fg: string;
  ring: string;
  dot: string;
};

const PALETTE: Record<ApprovalSeverity, Palette> = {
  low: {
    bg: "bg-emerald-500/15",
    fg: "text-emerald-700 dark:text-emerald-300",
    ring: "ring-emerald-500/30",
    dot: "bg-emerald-500",
  },
  medium: {
    bg: "bg-amber-500/15",
    fg: "text-amber-700 dark:text-amber-300",
    ring: "ring-amber-500/30",
    dot: "bg-amber-500",
  },
  high: {
    bg: "bg-red-500/15",
    fg: "text-red-700 dark:text-red-300",
    ring: "ring-red-500/30",
    dot: "bg-red-500",
  },
};

export function SeverityBadge({
  severity,
  className,
}: {
  severity: ApprovalSeverity;
  className?: string;
}) {
  const p = PALETTE[severity] ?? PALETTE.medium;
  const label = SEVERITY_LABEL[severity] ?? severity;
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
        className={cn("inline-block h-1.5 w-1.5 rounded-full", p.dot)}
        aria-hidden
      />
      {label}
    </span>
  );
}
