"use client";

// Phase 3 Stage 8 — Settings sub-navigation.
//
// Five items in a fixed order: Profile, Organization, Members & Roles,
// API Keys, Preferences. Active-state styling matches the main sidebar.
//
// We use plain <a> anchors here (not next/link) because typedRoutes is
// enabled and these routes are added in the same patch as this nav — Next
// would reject Link to a not-yet-known route at build time. Plain anchors
// fully reload the route, which is fine for settings pages.

import * as React from "react";
import { usePathname } from "next/navigation";

import { cn } from "@/lib/utils";

type Item = { href: string; label: string };

const ITEMS: Item[] = [
  { href: "/console/settings/profile", label: "Profile" },
  { href: "/console/settings/organization", label: "Organization" },
  { href: "/console/settings/members", label: "Members & Roles" },
  { href: "/console/settings/workspaces", label: "Workspaces" },
  { href: "/console/settings/api-keys", label: "API Keys" },
  { href: "/console/settings/billing", label: "Billing" },
  { href: "/console/settings/preferences", label: "Preferences" },
];

export function SettingsNav() {
  const pathname = usePathname() ?? "";
  return (
    <nav aria-label="Settings sections" className="space-y-1">
      <p className="px-3 pb-2 text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
        Settings
      </p>
      {ITEMS.map((it) => {
        const active = pathname === it.href || pathname.startsWith(it.href + "/");
        return (
          <a
            key={it.href}
            href={it.href}
            aria-current={active ? "page" : undefined}
            className={cn(
              "block rounded-md px-3 py-2 text-sm transition-colors",
              active
                ? "bg-[var(--color-muted)] font-medium text-[var(--color-foreground)]"
                : "text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]",
            )}
          >
            {it.label}
          </a>
        );
      })}
    </nav>
  );
}
