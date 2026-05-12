// Phase 3 Stage 6 — Console shell layout.
//
// Two-column layout: fixed Sidebar on the left (240px) + a main column with
// a sticky Topbar above the scrollable content area. The CommandPalette is
// rendered as part of the Topbar tree.
//
// Note: the main column uses a static `ml-60` gutter (240px). The Sidebar
// component persists a collapsed/expanded preference in localStorage but
// the main column does NOT shrink when collapsed — keeping the gutter
// server-renderable avoids hydration flicker. This is a deliberate
// compromise for Phase 3; revisit if collapse becomes a daily-driver
// feature.

import * as React from "react";

import { Sidebar } from "@/components/console/Sidebar";
import { Topbar } from "@/components/console/Topbar";

export default function ConsoleLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-[var(--color-background)] text-[var(--color-foreground)]">
      <Sidebar />
      <div className="ml-60">
        <Topbar />
        <main className="mx-auto max-w-[1440px] px-6 py-6">{children}</main>
      </div>
    </div>
  );
}
