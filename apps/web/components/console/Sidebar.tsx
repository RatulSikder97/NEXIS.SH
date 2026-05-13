"use client";

// Phase 3 Stage 6 — Console sidebar.
//
// Two sections (Workspace + Platform) with a pinned Settings footer.
// Collapsed/expanded state is persisted in localStorage under
// "nexis_sidebar_collapsed". Width is 240px expanded / 64px collapsed.
//
// NOTE: the surrounding ConsoleLayout pads the main column with a static
// `ml-60` (240px). When the sidebar collapses, the main column does NOT
// shrink — toggling collapse only hides the labels, not the gutter. This is
// a deliberate compromise for Phase 3 to keep the layout server-renderable
// and avoid a hydration dance. Stage 7+ may revisit if collapse becomes a
// daily-driver feature.

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Activity,
  AlertTriangle,
  CheckSquare,
  ChevronsLeft,
  ChevronsRight,
  FileText,
  Home,
  PlayCircle,
  Plug,
  Settings,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { WorkspaceSwitcher } from "@/components/workspaces/WorkspaceSwitcher";

type NavItem = {
  href: string;
  label: string;
  icon: LucideIcon;
};

const WORKSPACE: NavItem[] = [
  { href: "/console", label: "Home", icon: Home },
  { href: "/console/incidents", label: "Incidents", icon: AlertTriangle },
  { href: "/console/approvals", label: "Approvals", icon: CheckSquare },
  { href: "/console/audit", label: "Audit", icon: FileText },
];

const PLATFORM: NavItem[] = [
  { href: "/console/agents", label: "Agents", icon: Activity },
  { href: "/console/integrations", label: "Integrations", icon: Plug },
  { href: "/console/live-demo", label: "Live Demo", icon: PlayCircle },
];

const STORAGE_KEY = "nexis_sidebar_collapsed";

function NavLink({
  item,
  collapsed,
  active,
}: {
  item: NavItem;
  collapsed: boolean;
  active: boolean;
}) {
  const Icon = item.icon;
  return (
    <Link
      // typedRoutes treats the href as Route; cast through unknown for the
      // narrow string union we store in NavItem.
      href={item.href as unknown as never}
      title={collapsed ? item.label : undefined}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors",
        active
          ? "bg-[var(--color-muted)] text-[var(--color-foreground)] font-medium"
          : "text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]",
      )}
    >
      <Icon className="h-4 w-4 shrink-0" />
      {!collapsed && <span className="truncate">{item.label}</span>}
    </Link>
  );
}

function SectionLabel({ label, collapsed }: { label: string; collapsed: boolean }) {
  if (collapsed) {
    return <div className="my-2 border-t border-[var(--color-border)]" aria-hidden />;
  }
  return (
    <p className="px-3 pb-1 pt-3 text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
      {label}
    </p>
  );
}

// Module-level "tick" counter so external mutations via toggle() can wake
// every subscriber on the page. (Only one Sidebar mounts at a time, but
// keeping this stable across renders is required by useSyncExternalStore.)
const tickListeners = new Set<() => void>();
let tick = 0;
function emit() {
  tick++;
  tickListeners.forEach((l) => l());
}
function subscribe(listener: () => void) {
  tickListeners.add(listener);
  return () => {
    tickListeners.delete(listener);
  };
}
function readCollapsed(): boolean {
  try {
    return window.localStorage.getItem(STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

export function Sidebar() {
  const pathname = usePathname();
  // useSyncExternalStore is the React-blessed way to subscribe to an
  // external sync source (localStorage) without tripping the
  // set-state-in-effect lint rule. The server snapshot is `false`
  // (expanded) so SSR matches the static `ml-60` gutter on the main column.
  const collapsed = React.useSyncExternalStore(
    subscribe,
    readCollapsed,
    () => false,
  );
  // Force-read `tick` so the IDE doesn't strip the unused import; the
  // subscribe callback already triggers re-renders.
  void tick;

  function toggle() {
    const next = !collapsed;
    try {
      window.localStorage.setItem(STORAGE_KEY, next ? "1" : "0");
    } catch {
      // ignore
    }
    emit();
  }

  const width = collapsed ? "w-16" : "w-60";

  function isActive(href: string) {
    if (!pathname) return false;
    if (href === "/console") return pathname === "/console";
    return pathname === href || pathname.startsWith(href + "/");
  }

  return (
    <aside
      className={cn(
        "fixed inset-y-0 left-0 z-30 flex flex-col border-r border-[var(--color-border)] bg-[var(--color-card)]",
        width,
      )}
    >
      <div className="flex h-14 items-center justify-between border-b border-[var(--color-border)] px-3">
        {!collapsed && (
          <span className="text-sm font-semibold tracking-tight text-[var(--color-foreground)]">
            NEXIS
          </span>
        )}
        <button
          type="button"
          onClick={toggle}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          className="inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
        >
          {collapsed ? (
            <ChevronsRight className="h-4 w-4" />
          ) : (
            <ChevronsLeft className="h-4 w-4" />
          )}
        </button>
      </div>

      <WorkspaceSwitcher collapsed={collapsed} />

      <nav className="flex-1 overflow-y-auto px-2 py-2">
        <ul className="space-y-0.5">
          {WORKSPACE.map((item) => (
            <li key={item.href}>
              <NavLink item={item} collapsed={collapsed} active={isActive(item.href)} />
            </li>
          ))}
        </ul>

        <SectionLabel label="Platform" collapsed={collapsed} />
        <ul className="space-y-0.5">
          {PLATFORM.map((item) => (
            <li key={item.href}>
              <NavLink item={item} collapsed={collapsed} active={isActive(item.href)} />
            </li>
          ))}
        </ul>
      </nav>

      <div className="border-t border-[var(--color-border)] px-2 py-2">
        <NavLink
          item={{ href: "/console/settings/profile", label: "Settings", icon: Settings }}
          collapsed={collapsed}
          active={isActive("/console/settings")}
        />
      </div>
    </aside>
  );
}
