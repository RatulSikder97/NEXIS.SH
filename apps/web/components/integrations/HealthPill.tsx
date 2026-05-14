"use client";

// Real-integrations Wave 1 — live health pill rendered on each integration
// card. Reads the new `IntegrationHealth` DTO the control-plane returns from
// /v1/integrations (Task 1 of the plan).
//
// Visual language matches the status dots on /console/agents:
//   * inline-flex, gap-1.5, text-xs
//   * 8px circular dot (h-2 w-2) — the `healthy` dot pulses softly so
//     operators can tell from across the room that the probe is fresh.
//   * Full last-error text is truncated to 40 chars in the visible label and
//     surfaced via the native `title` tooltip on hover (no Radix Tooltip in
//     this codebase yet — adding the dep just for this is overkill).
//
// We intentionally don't render `last_check_at` in the pill body. The card
// is small and the freshness indicator is more useful inline; the timestamp
// is exposed via the same title tooltip for keyboard users.

import * as React from "react";

import { cn } from "@/lib/utils";

export type HealthState =
  | "healthy"
  | "degraded"
  | "down"
  | "disconnected"
  | "unknown";

type Props = {
  state: HealthState;
  latency_ms?: number;
  last_check_at?: string;
  last_error?: string;
};

const DOT_COLOR: Record<HealthState, string> = {
  healthy: "bg-emerald-500",
  degraded: "bg-amber-500",
  down: "bg-red-500",
  disconnected: "bg-[var(--color-muted-foreground)]/40",
  unknown: "bg-[var(--color-muted-foreground)]/40",
};

const LABEL_COLOR: Record<HealthState, string> = {
  healthy: "text-emerald-600 dark:text-emerald-400",
  degraded: "text-amber-600 dark:text-amber-400",
  down: "text-red-600 dark:text-red-400",
  disconnected: "text-[var(--color-muted-foreground)]",
  unknown: "text-[var(--color-muted-foreground)]",
};

function truncate(s: string, max: number): string {
  if (s.length <= max) return s;
  return s.slice(0, max - 1) + "…"; // ellipsis
}

function buildTitle(props: Props): string | undefined {
  const parts: string[] = [];
  if (props.last_error) parts.push(`Error: ${props.last_error}`);
  if (props.last_check_at) parts.push(`Checked: ${props.last_check_at}`);
  if (typeof props.latency_ms === "number")
    parts.push(`Latency: ${props.latency_ms}ms`);
  return parts.length > 0 ? parts.join("\n") : undefined;
}

function renderLabel({
  state,
  latency_ms,
  last_error,
}: Props): string {
  switch (state) {
    case "healthy":
      return typeof latency_ms === "number"
        ? `Healthy ${latency_ms}ms`
        : "Healthy";
    case "degraded":
      return last_error
        ? `Degraded — ${truncate(last_error, 40)}`
        : "Degraded";
    case "down":
      return "Down";
    case "disconnected":
      return "Disconnected";
    case "unknown":
      return "Unknown";
  }
}

export function HealthPill(props: Props) {
  const { state } = props;
  const title = buildTitle(props);
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-xs",
        LABEL_COLOR[state],
      )}
      aria-label={`health ${state}`}
      title={title}
    >
      <span
        aria-hidden
        className={cn(
          "inline-block h-2 w-2 rounded-full",
          DOT_COLOR[state],
          state === "healthy" && "animate-pulse",
        )}
      />
      <span className="truncate">{renderLabel(props)}</span>
    </span>
  );
}
