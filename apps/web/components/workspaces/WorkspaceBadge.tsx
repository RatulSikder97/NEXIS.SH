"use client";

// Phase 3.5 Stage 7 — Compact workspace badge for the Topbar.
//
// Shows the current workspace as a small pill: continent flag + name +
// monospace region id. Rendered to the left of the breadcrumb so the user
// always knows which tenant view they're looking at.

import { cn } from "@/lib/utils";

const FLAGS: Record<string, string> = {
  "us-east-1": "🇺🇸",
  "us-west-2": "🇺🇸",
  "eu-west-1": "🇪🇺",
  "eu-central-1": "🇪🇺",
  "ap-southeast-1": "🇸🇬",
  "ap-south-1": "🇮🇳",
};

export function WorkspaceBadge({
  name,
  region,
  className,
}: {
  name: string;
  region: string;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-2 py-1 text-xs",
        className,
      )}
      aria-label={`Workspace ${name} in ${region}`}
    >
      <span aria-hidden>{FLAGS[region] ?? "🌐"}</span>
      <span className="font-medium text-[var(--color-foreground)]">{name}</span>
      <span className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
        {region}
      </span>
    </span>
  );
}
