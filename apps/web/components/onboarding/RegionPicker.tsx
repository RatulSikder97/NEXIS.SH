"use client";

// Phase 3.5 Stage 6 — Region picker for the onboarding wizard.
//
// Six fake regions surfaced as a 2/3-col grid of cards. Each card shows a
// continent flag, the human-readable region name, and the region id in
// monospace. Selected state lifts the border + faintly tints the bg with
// --color-primary. Keyboard navigation is the default tab order — every
// card is a <button> so screen readers + keyboard users get free a11y.

import { cn } from "@/lib/utils";
import type { Region } from "@/lib/workspaces";

// Continent flag mapping — fixed per the Phase 3.5 spec. "🇸🇬" stands in for
// "ap-southeast-1" (Singapore) and "🇮🇳" for "ap-south-1" (Mumbai) so the
// two APAC regions read distinctly. Unknown ids fall back to a generic globe.
const FLAGS: Record<string, string> = {
  "us-east-1": "🇺🇸",
  "us-west-2": "🇺🇸",
  "eu-west-1": "🇪🇺",
  "eu-central-1": "🇪🇺",
  "ap-southeast-1": "🇸🇬",
  "ap-south-1": "🇮🇳",
};

export function RegionPicker({
  regions,
  selected,
  onSelect,
}: {
  regions: Region[];
  selected: string | null;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
      {regions.map((r) => (
        <button
          key={r.id}
          type="button"
          onClick={() => onSelect(r.id)}
          aria-pressed={selected === r.id}
          className={cn(
            "rounded-lg border p-4 text-left transition-colors",
            selected === r.id
              ? "border-[var(--color-primary)] bg-[var(--color-primary)]/5"
              : "border-[var(--color-border)] bg-[var(--color-card)] hover:bg-[var(--color-muted)]",
          )}
        >
          <div className="text-2xl mb-2" aria-hidden>
            {FLAGS[r.id] ?? "🌐"}
          </div>
          <div className="text-sm font-medium text-[var(--color-foreground)]">
            {r.name}
          </div>
          <div className="mt-1 font-mono text-xs text-[var(--color-muted-foreground)]">
            {r.id}
          </div>
        </button>
      ))}
    </div>
  );
}
