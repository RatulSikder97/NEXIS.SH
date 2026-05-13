"use client";

// Phase 3 Stage 7 — Integration card.
//
// Single tile in the 3-column grid on /console/integrations. Renders the
// provider name, a one-line description, a status badge (Connected / Pending
// / Error / Disconnected / Coming Soon) and a primary CTA. The CTA text
// flips between "Configure" and "Manage" depending on whether the
// integration is currently connected.
//
// Click handling is delegated to the parent — IntegrationsClient owns the
// dialog and decides which Configure form to render based on `provider`.

import * as React from "react";

import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import type { Integration } from "@/lib/integrations";

export type IntegrationCardProps = {
  provider: Integration["provider"] | "datadog" | "pagerduty" | "slack";
  name: string;
  description: string;
  status?: Integration["status"];
  comingSoon?: boolean;
  onConfigure?: () => void;
  // Wave 1 — when supplied, replaces the legacy status badge entirely (e.g.
  // with a <HealthPill/>). When omitted the original StatusBadge is rendered
  // so legacy callers keep working unchanged.
  statusSlot?: React.ReactNode;
};

const STATUS_STYLES: Record<NonNullable<Integration["status"]>, string> = {
  connected:
    "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 ring-1 ring-inset ring-emerald-500/20",
  pending:
    "bg-amber-500/10 text-amber-600 dark:text-amber-400 ring-1 ring-inset ring-amber-500/20",
  error:
    "bg-red-500/10 text-red-600 dark:text-red-400 ring-1 ring-inset ring-red-500/20",
  disconnected:
    "bg-[var(--color-muted)] text-[var(--color-muted-foreground)] ring-1 ring-inset ring-[var(--color-border)]",
};

function StatusBadge({
  status,
  comingSoon,
}: {
  status?: Integration["status"];
  comingSoon?: boolean;
}) {
  if (comingSoon) {
    return (
      <span className="inline-flex items-center rounded-full bg-[var(--color-muted)] px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest text-[var(--color-muted-foreground)] ring-1 ring-inset ring-[var(--color-border)]">
        Coming soon
      </span>
    );
  }
  const effective = status ?? "disconnected";
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest",
        STATUS_STYLES[effective],
      )}
    >
      {effective}
    </span>
  );
}

export function IntegrationCard({
  name,
  description,
  status,
  comingSoon,
  onConfigure,
  statusSlot,
}: IntegrationCardProps) {
  const connected = status === "connected";
  return (
    <div className="flex h-full flex-col justify-between gap-4 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
      <div className="space-y-2">
        <div className="flex items-start justify-between gap-2">
          <h3 className="text-base font-semibold text-[var(--color-foreground)]">
            {name}
          </h3>
          {statusSlot ?? <StatusBadge status={status} comingSoon={comingSoon} />}
        </div>
        <p className="text-sm text-[var(--color-muted-foreground)]">
          {description}
        </p>
      </div>
      <div>
        <Button
          variant={connected ? "outline" : "default"}
          size="sm"
          disabled={comingSoon}
          onClick={onConfigure}
        >
          {comingSoon ? "Unavailable" : connected ? "Manage" : "Configure"}
        </Button>
      </div>
    </div>
  );
}
