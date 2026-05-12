// Phase 3 Stage 8 — Settings sub-shell.
//
// Two-column layout under /console/settings: a 200px sub-sidebar with five
// vertical nav items + a content well that grows to fill remaining space.
// The outer ConsoleLayout already supplies the chrome (Sidebar + Topbar);
// this layout sits beneath that and only owns the inner navigation.
//
// Active-state styling is driven by usePathname() — see SettingsNav below.
// Each link uses a plain <a> rather than next/link because typedRoutes would
// require these routes to be statically known via the .next route manifest,
// and they're newly added in this stage.

import { SettingsNav } from "@/components/console/SettingsNav";

export default function SettingsLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-[200px_1fr] gap-8">
      <SettingsNav />
      <div className="min-w-0">{children}</div>
    </div>
  );
}
