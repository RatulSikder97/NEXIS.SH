"use client";

// Phase 3 Stage 6 — Console topbar.
//
// Sticky 56px header sitting above the main column. Renders a breadcrumb
// derived from `usePathname()` (segments after /console), a button that
// opens the command palette (also bound to ⌘K), and the theme toggle.
//
// The CommandPalette component manages its own open state via the global
// ⌘K listener; clicking the trigger button dispatches a synthetic keyboard
// event so we share that single source of truth without prop-drilling.

import * as React from "react";
import { usePathname } from "next/navigation";

import { ThemeToggle } from "@/components/ThemeToggle";
import { CommandPalette } from "@/components/console/CommandPalette";
import { UserMenu } from "@/components/console/UserMenu";
import { WorkspaceBadge } from "@/components/workspaces/WorkspaceBadge";

function humanize(segment: string): string {
  if (!segment) return "";
  return segment
    .replace(/-/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

function Breadcrumb() {
  const pathname = usePathname() ?? "/console";
  const parts = pathname.split("/").filter(Boolean);
  // Drop the leading "console" so the crumb reads "Home › Incidents"
  // rather than "Console › Incidents".
  const rest = parts.slice(1);
  const crumbs = rest.length === 0 ? ["Home"] : rest.map(humanize);

  return (
    <nav aria-label="Breadcrumb" className="flex items-center gap-1.5 text-sm">
      {crumbs.map((c, i) => (
        <React.Fragment key={`${c}-${i}`}>
          {i > 0 && (
            <span className="text-[var(--color-muted-foreground)]" aria-hidden>
              /
            </span>
          )}
          <span
            className={
              i === crumbs.length - 1
                ? "font-medium text-[var(--color-foreground)]"
                : "text-[var(--color-muted-foreground)]"
            }
          >
            {c}
          </span>
        </React.Fragment>
      ))}
    </nav>
  );
}

function openPalette() {
  // Synthesize the same ⌘K shortcut the palette listens for. Keeps the
  // open-state owned by CommandPalette without prop drilling.
  const isMac =
    typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
  const ev = new KeyboardEvent("keydown", {
    key: "k",
    metaKey: isMac,
    ctrlKey: !isMac,
    bubbles: true,
  });
  window.dispatchEvent(ev);
}

export function Topbar({
  userEmail,
  currentWorkspace,
}: {
  userEmail: string;
  currentWorkspace?: { name: string; region: string };
}) {
  return (
    <>
      <header className="sticky top-0 z-20 flex h-14 items-center justify-between border-b border-[var(--color-border)] bg-[var(--color-background)]/95 px-6 backdrop-blur">
        <div className="flex items-center gap-3">
          {currentWorkspace && (
            <>
              <WorkspaceBadge
                name={currentWorkspace.name}
                region={currentWorkspace.region}
              />
              <span
                aria-hidden
                className="h-5 w-px bg-[var(--color-border)]"
              />
            </>
          )}
          <Breadcrumb />
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={openPalette}
            aria-label="Open command palette"
            className="inline-flex items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-1.5 text-sm text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
          >
            <span>Search…</span>
            <kbd className="rounded border border-[var(--color-border)] bg-[var(--color-muted)] px-1.5 py-0.5 text-[10px] font-mono text-[var(--color-muted-foreground)]">
              ⌘K
            </kbd>
          </button>
          <ThemeToggle />
          <UserMenu email={userEmail} />
        </div>
      </header>
      <CommandPalette />
    </>
  );
}
