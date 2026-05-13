"use client";

// Phase 8 — Empty-state library.
//
// Single component used by every list/table surface to render the
// data-is-empty branch. Replaces inline "No X yet" placeholders so the
// console has a consistent voice + a hookable CTA pattern.
//
// Each empty state takes:
//   icon        — lucide-react icon (rendered at 24px inside a 40px tile)
//   title       — short headline ("No incidents yet")
//   description — one-sentence subtext explaining what would populate it
//   cta?        — optional primary call to action (button or link)
//   secondary?  — optional secondary action (typically a docs link)
//
// CI enforcement: scripts/check-no-data-strings.sh grep for raw "No data"
// or "No rows" strings under app/(app)/console and exits non-zero if any
// are left. Use this component to render those states instead.

import * as React from "react";
import type { LucideIcon } from "lucide-react";

import { cn } from "@/lib/utils";

export function EmptyState({
  icon: Icon,
  title,
  description,
  cta,
  secondary,
  className,
}: {
  icon: LucideIcon;
  title: string;
  description?: string;
  cta?: React.ReactNode;
  secondary?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      role="status"
      className={cn(
        "flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-[var(--color-border)] bg-[var(--color-card)] p-10 text-center",
        className,
      )}
    >
      <div className="inline-flex h-10 w-10 items-center justify-center rounded-md bg-[var(--color-muted)]/60 text-[var(--color-muted-foreground)]">
        <Icon className="h-5 w-5" aria-hidden />
      </div>
      <div className="space-y-1">
        <p className="text-base font-medium text-[var(--color-foreground)]">
          {title}
        </p>
        {description && (
          <p className="mx-auto max-w-md text-sm text-[var(--color-muted-foreground)]">
            {description}
          </p>
        )}
      </div>
      {(cta || secondary) && (
        <div className="mt-2 flex flex-wrap items-center justify-center gap-2">
          {cta}
          {secondary}
        </div>
      )}
    </div>
  );
}
