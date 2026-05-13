"use client";

// Integration card — brand-logo-led tile in the /console/integrations grid.
//
// Layout: top-left 40px brand logo on a subtle tinted square, top-right
// status badge / HealthPill. Below: provider name, one-line description,
// metadata strip (region/installation id), CTA row. Hover lifts the border
// to brand-primary for grab-ability.

import * as React from "react";

import { Button } from "@/components/ui/Button";
import { ProviderLogo } from "@/components/integrations/ProviderLogo";
import { cn } from "@/lib/utils";
import type { Integration } from "@/lib/integrations";

export type IntegrationCardProps = {
  provider: Integration["provider"] | "datadog" | "pagerduty" | "slack";
  name: string;
  description: string;
  status?: Integration["status"];
  comingSoon?: boolean;
  onConfigure?: () => void;
  // Wave 1 — when supplied, replaces the legacy status badge entirely
  // (e.g. with a <HealthPill/>). When omitted, the default badge renders.
  statusSlot?: React.ReactNode;
  // Optional metadata: installation id, region, last-sync — rendered as a
  // small monospace strip above the CTA.
  metadata?: string;
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

const LOGO_BG: Record<IntegrationCardProps["provider"], string> = {
  github: "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900",
  sentry: "bg-[#362D59]/10 text-[#362D59] dark:bg-[#362D59]/20 dark:text-[#A89FFF]",
  argocd: "bg-[#EF7B4D]/10 text-[#EF7B4D]",
  slack: "bg-white ring-1 ring-inset ring-[var(--color-border)] dark:bg-zinc-900",
  datadog: "bg-[#632CA6]/10 text-[#632CA6] dark:bg-[#632CA6]/20 dark:text-[#B594E0]",
  pagerduty: "bg-[#06AC38]/10 text-[#06AC38] dark:bg-[#06AC38]/15 dark:text-[#5BD680]",
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
  provider,
  name,
  description,
  status,
  comingSoon,
  onConfigure,
  statusSlot,
  metadata,
}: IntegrationCardProps) {
  const connected = status === "connected";
  return (
    <div
      className={cn(
        "group flex h-full flex-col justify-between gap-5 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-5 transition-colors",
        !comingSoon && "hover:border-[var(--color-primary)]/40 hover:shadow-sm",
      )}
    >
      {/* Header: logo + status */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <div
            className={cn(
              "flex h-10 w-10 shrink-0 items-center justify-center rounded-lg",
              LOGO_BG[provider],
            )}
          >
            <ProviderLogo provider={provider} className="h-6 w-6 flex items-center justify-center" />
          </div>
          <div className="min-w-0">
            <h3 className="text-sm font-semibold leading-tight text-[var(--color-foreground)]">
              {name}
            </h3>
            <p className="text-[11px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
              {connected ? "Active" : comingSoon ? "Roadmap" : "Available"}
            </p>
          </div>
        </div>
        <div className="shrink-0">
          {statusSlot ?? <StatusBadge status={status} comingSoon={comingSoon} />}
        </div>
      </div>

      {/* Description */}
      <p className="text-sm leading-relaxed text-[var(--color-muted-foreground)]">
        {description}
      </p>

      {/* Optional metadata strip (installation id / region / last-sync) */}
      {metadata ? (
        <div className="rounded-md bg-[var(--color-muted)]/40 px-2.5 py-1.5 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {metadata}
        </div>
      ) : null}

      {/* CTA */}
      <div className="flex items-center justify-between gap-2">
        <Button
          variant={connected ? "outline" : "default"}
          size="sm"
          disabled={comingSoon}
          onClick={onConfigure}
        >
          {comingSoon ? "Unavailable" : connected ? "Manage" : "Configure"}
        </Button>
        {!comingSoon && connected ? (
          <span className="text-[11px] text-emerald-600 dark:text-emerald-400">
            ✓ Connected
          </span>
        ) : null}
      </div>
    </div>
  );
}
