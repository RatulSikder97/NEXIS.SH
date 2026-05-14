// Phase 6 Stage 9 — AgentSummaryCard.
//
// Small reusable card that summarises one L2 agent's last finish payload on
// the incident detail page. Three call sites today (Pathfinder, Synthesiser,
// Validator) share the same outer chrome: an agent icon + label, a body slot
// for agent-specific fields, and a graceful "—" placeholder when the
// activity hasn't reached a terminal frame yet.
//
// The card is intentionally compact + light on chrome so a 3-card row reads
// as a single header band above the timeline rather than competing with it.
//
// Confidence pill is exported alongside since the Pathfinder card consumes
// it; keeping the helper local to the file lets both summary cards share
// the same colour palette without an extra component module.

import * as React from "react";

import { AgentIcon } from "@/components/pipelines/AgentIcon";
import { cn } from "@/lib/utils";

// AgentSummaryCard renders the outer chrome. `body` is the agent-specific
// content (selected agents, root cause, etc.); when `body` is null/empty
// the card collapses to a centred "—" placeholder so the row keeps height.
export function AgentSummaryCard({
  role,
  label,
  body,
  className,
}: {
  role: string;
  label: string;
  body: React.ReactNode;
  className?: string;
}) {
  const hasBody = body !== null && body !== undefined && body !== "";
  return (
    <div
      className={cn(
        "flex min-w-0 flex-col rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4",
        className,
      )}
    >
      <div className="flex items-center gap-2">
        <AgentIcon
          role={role}
          className="h-4 w-4 text-[var(--color-muted-foreground)]"
        />
        <span className="text-xs font-medium uppercase tracking-widest text-[var(--color-muted-foreground)]">
          {label}
        </span>
      </div>
      <div className="mt-2 min-h-[2.25rem] text-sm text-[var(--color-foreground)]">
        {hasBody ? (
          body
        ) : (
          <span className="text-[var(--color-muted-foreground)]">—</span>
        )}
      </div>
    </div>
  );
}

// ConfidencePill renders a 0..1 confidence float as a compact pill. The
// colour bucket mirrors the severity palette (low/medium/high) so users
// can read confidence at the same visual cadence as risk.
//
// Inputs > 1 are treated as percent already; anything non-finite renders
// as a neutral grey placeholder so we never crash on malformed payloads.
export function ConfidencePill({
  confidence,
  className,
}: {
  confidence: number | undefined;
  className?: string;
}) {
  if (
    typeof confidence !== "number" ||
    !Number.isFinite(confidence) ||
    confidence < 0
  ) {
    return (
      <span
        className={cn(
          "inline-flex items-center rounded-full bg-[var(--color-muted)] px-2 py-0.5 text-[10px] font-medium text-[var(--color-muted-foreground)] ring-1 ring-[var(--color-border)]",
          className,
        )}
      >
        confidence —
      </span>
    );
  }
  const pct = confidence > 1 ? confidence : confidence * 100;
  const rounded = Math.round(pct);
  // Buckets match StatusPill / SeverityBadge palette anchors.
  let palette: { bg: string; fg: string; ring: string };
  if (rounded >= 80) {
    palette = {
      bg: "bg-emerald-500/15",
      fg: "text-emerald-700 dark:text-emerald-300",
      ring: "ring-emerald-500/30",
    };
  } else if (rounded >= 50) {
    palette = {
      bg: "bg-amber-500/15",
      fg: "text-amber-700 dark:text-amber-300",
      ring: "ring-amber-500/30",
    };
  } else {
    palette = {
      bg: "bg-red-500/15",
      fg: "text-red-700 dark:text-red-300",
      ring: "ring-red-500/30",
    };
  }
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium ring-1",
        palette.bg,
        palette.fg,
        palette.ring,
        className,
      )}
    >
      {rounded}% confidence
    </span>
  );
}
