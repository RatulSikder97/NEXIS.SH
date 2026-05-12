// Phase 3 Stage 6 — `(app)` route group is now a transparent passthrough.
//
// In Phase 2 this layout rendered a header + logout button shared by the
// /dashboard page. Phase 3 collapses /dashboard into a redirect to /console
// and the console route owns its own full-bleed shell (Sidebar + Topbar +
// CommandPalette) in `app/(app)/console/layout.tsx`. To avoid double-
// wrapping (and the resulting header/topbar collision), this layout is
// reduced to a no-op fragment.

export default function AppLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <>{children}</>;
}
