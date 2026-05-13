"use client";

// SystemStatusPill — sticky-at-bottom-of-sidebar live status badge.
//
// Aggregates integration health (GET /v1/integrations) every 30s and renders
// one of three states:
//   * "All systems" / emerald — every connected integration is healthy
//   * "Degraded · N" / amber — at least one connection is degraded but none
//     are down; we surface the count of non-healthy connections
//   * "Down · N" / red — at least one connection is hard-down
//
// Clicking the pill navigates to /console/health. On the collapsed sidebar
// we render only the status dot. The endpoint may not exist yet — we degrade
// to "Unknown" silently so the pill never blocks the layout.
//
// We deliberately do NOT mount inside the Sidebar.tsx scrollable nav so the
// pill stays anchored at the bottom even on long sidebars.

import * as React from "react";
import Link from "next/link";

import { cn } from "@/lib/utils";
import { integrations } from "@/lib/integrations";

const STATUS_POLL_MS = 30_000;

type SystemState = "ok" | "degraded" | "down" | "unknown";

type Summary = {
  state: SystemState;
  degraded_count: number;
  down_count: number;
  connected_count: number;
};

function useSystemSummary(): Summary {
  const [summary, setSummary] = React.useState<Summary>({
    state: "unknown",
    degraded_count: 0,
    down_count: 0,
    connected_count: 0,
  });

  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      try {
        const rows = await integrations.listConnections();
        if (cancelled) return;
        let degraded = 0;
        let down = 0;
        let connected = 0;
        for (const r of rows) {
          if (!r.connected) continue;
          connected++;
          if (r.health.state === "down") down++;
          else if (r.health.state === "degraded") degraded++;
        }
        const state: SystemState =
          connected === 0
            ? "unknown"
            : down > 0
              ? "down"
              : degraded > 0
                ? "degraded"
                : "ok";
        setSummary({
          state,
          degraded_count: degraded,
          down_count: down,
          connected_count: connected,
        });
      } catch {
        // swallow — keep showing the previous summary, the next tick retries.
      }
    }
    void tick();
    const id = window.setInterval(tick, STATUS_POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);

  return summary;
}

export function SystemStatusPill({ collapsed }: { collapsed: boolean }) {
  const s = useSystemSummary();

  const palette: Record<SystemState, { dot: string; bg: string; label: string }> = {
    ok: {
      dot: "bg-emerald-500",
      bg: "bg-emerald-500/10 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
      label: "All systems",
    },
    degraded: {
      dot: "bg-amber-500",
      bg: "bg-amber-500/10 text-amber-700 ring-amber-500/30 dark:text-amber-300",
      label: `Degraded · ${s.degraded_count}`,
    },
    down: {
      dot: "bg-red-500",
      bg: "bg-red-500/10 text-red-700 ring-red-500/30 dark:text-red-300",
      label: `Down · ${s.down_count}`,
    },
    unknown: {
      dot: "bg-zinc-400",
      bg: "bg-[var(--color-muted)] text-[var(--color-muted-foreground)] ring-[var(--color-border)]",
      label: "Status unknown",
    },
  };

  const p = palette[s.state];

  if (collapsed) {
    return (
      <Link
        href={"/console/health" as never}
        title={p.label}
        aria-label={`System status: ${p.label}`}
        className="flex items-center justify-center py-2"
      >
        <span
          aria-hidden
          className={cn(
            "inline-block h-2.5 w-2.5 rounded-full ring-2 ring-[var(--color-card)]",
            p.dot,
            s.state === "ok" && "animate-pulse",
          )}
        />
      </Link>
    );
  }

  return (
    <Link
      href={"/console/health" as never}
      aria-label={`System status: ${p.label}`}
      className={cn(
        "flex items-center gap-2 rounded-md px-2.5 py-1.5 text-xs font-medium ring-1 transition-colors hover:bg-[var(--color-muted)]",
        p.bg,
      )}
    >
      <span
        aria-hidden
        className={cn(
          "inline-block h-2 w-2 rounded-full",
          p.dot,
          s.state === "ok" && "animate-pulse",
        )}
      />
      <span className="truncate">{p.label}</span>
    </Link>
  );
}
