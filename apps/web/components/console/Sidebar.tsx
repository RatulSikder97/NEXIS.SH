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
  Beaker,
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
import { pipelines } from "@/lib/pipelines";
import { approvals } from "@/lib/approvals";
import { WorkspaceSwitcher } from "@/components/workspaces/WorkspaceSwitcher";

type NavItem = {
  href: string;
  label: string;
  icon: LucideIcon;
  // navKey lets a NavLink consumer correlate this row with side-channel
  // state (e.g. a running-count badge on Incidents) without leaking that
  // logic into the static config table.
  navKey?: "incidents" | "approvals";
};

const WORKSPACE: NavItem[] = [
  { href: "/console", label: "Home", icon: Home },
  {
    href: "/console/incidents",
    label: "Incidents",
    icon: AlertTriangle,
    navKey: "incidents",
  },
  {
    href: "/console/approvals",
    label: "Approvals",
    icon: CheckSquare,
    navKey: "approvals",
  },
  { href: "/console/audit", label: "Audit", icon: FileText },
];

// COOKIE_WORKSPACE mirrors the value WorkspaceSwitcher reads/writes so the
// running-pipelines polling badge queries the same workspace the rest of
// the console is scoped to.
const COOKIE_WORKSPACE = "nexis_workspace";
const INCIDENTS_POLL_MS = 10_000;
// Phase 6 Stage 9 — pending-approvals badge polls at the same cadence as
// running-incidents. The two badges live next to each other in the sidebar
// so a single 10s heartbeat keeps the operator's "what needs my attention?"
// surface fresh without spamming the control-plane.
const APPROVALS_POLL_MS = 10_000;

function readWorkspaceCookie(): string | null {
  if (typeof document === "undefined") return null;
  const m = document.cookie.match(
    new RegExp(
      "(?:^|; )" +
        COOKIE_WORKSPACE.replace(/[.$?*|{}()[\]\\/+^]/g, "\\$&") +
        "=([^;]*)",
    ),
  );
  return m ? decodeURIComponent(m[1]) : null;
}

// useRunningPipelineCount polls /v1/workspaces/{ws}/pipelines every 10s and
// returns the count of queued|running rows. Returns 0 (not null) until the
// first fetch resolves so the badge stays out of the layout until we know
// there's something to show.
function useRunningPipelineCount(): number {
  const [count, setCount] = React.useState(0);
  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      const wsId = readWorkspaceCookie();
      if (!wsId) {
        if (!cancelled) setCount(0);
        return;
      }
      try {
        const rows = await pipelines.list(wsId, 50);
        if (cancelled) return;
        let n = 0;
        for (const r of rows) {
          if (r.status === "queued" || r.status === "running") n++;
        }
        setCount(n);
      } catch {
        // swallow — the next tick will retry.
      }
    }
    void tick();
    const id = window.setInterval(tick, INCIDENTS_POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);
  return count;
}

// usePendingApprovalsCount polls /v1/workspaces/{ws}/approvals/pending every
// 10s and returns the row count. The endpoint is owner|admin only — for
// regular members the call returns 403 and we degrade to 0 silently so the
// badge simply never appears for non-privileged viewers. We do NOT push the
// 403 through the standard error path because the sidebar mounts on every
// console page and surfacing a perpetual "forbidden" toast would be noisy.
function usePendingApprovalsCount(): number {
  const [count, setCount] = React.useState(0);
  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      const wsId = readWorkspaceCookie();
      if (!wsId) {
        if (!cancelled) setCount(0);
        return;
      }
      try {
        const rows = await approvals.pending(wsId);
        if (cancelled) return;
        setCount(rows.length);
      } catch {
        // 403 (member role) and transient errors both land here. The next
        // tick will retry; in the 403 case it keeps failing harmlessly.
      }
    }
    void tick();
    const id = window.setInterval(tick, APPROVALS_POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);
  return count;
}

const PLATFORM: NavItem[] = [
  { href: "/console/agents", label: "Agents", icon: Activity },
  // Phase 5 Stage 8 — Eval surface compares OpenAI vs Ollama for the
  // same scenario. Slotted between Agents and Integrations so the
  // "Platform" section reads top-down as observability → tooling.
  { href: "/console/eval", label: "Eval", icon: Beaker },
  { href: "/console/integrations", label: "Integrations", icon: Plug },
  { href: "/console/live-demo", label: "Live Demo", icon: PlayCircle },
];

const STORAGE_KEY = "nexis_sidebar_collapsed";

function NavLink({
  item,
  collapsed,
  active,
  badge,
  // badgeTone toggles the colour family: "info" (blue) for running counts,
  // "warn" (amber) for "action required" counts like pending approvals.
  badgeTone = "info",
  badgeLabel,
}: {
  item: NavItem;
  collapsed: boolean;
  active: boolean;
  // Optional positive integer rendered as a pill next to the label (or as
  // a small superscript dot when the sidebar is collapsed).
  badge?: number;
  badgeTone?: "info" | "warn";
  // badgeLabel overrides the aria-label suffix. Defaults to "running" to
  // preserve the Phase 4 behaviour for the Incidents row.
  badgeLabel?: string;
}) {
  const Icon = item.icon;
  const showBadge = typeof badge === "number" && badge > 0;
  const badgeClasses =
    badgeTone === "warn"
      ? "bg-amber-500/15 text-amber-700 ring-amber-500/30 dark:text-amber-300"
      : "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300";
  const dotClass = badgeTone === "warn" ? "bg-amber-500" : "bg-blue-500";
  const labelSuffix = badgeLabel ?? "running";
  return (
    <Link
      // typedRoutes treats the href as Route; cast through unknown for the
      // narrow string union we store in NavItem.
      href={item.href as unknown as never}
      title={collapsed ? item.label : undefined}
      aria-current={active ? "page" : undefined}
      className={cn(
        "relative flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors",
        active
          ? "bg-[var(--color-muted)] text-[var(--color-foreground)] font-medium"
          : "text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]",
      )}
    >
      <Icon className="h-4 w-4 shrink-0" />
      {!collapsed && <span className="truncate">{item.label}</span>}
      {!collapsed && showBadge && (
        <span
          aria-label={`${badge} ${labelSuffix}`}
          className={cn(
            "ml-auto inline-flex min-w-[1.25rem] items-center justify-center rounded-full px-1.5 py-0.5 text-[10px] font-medium ring-1",
            badgeClasses,
          )}
        >
          {badge}
        </span>
      )}
      {collapsed && showBadge && (
        <span
          aria-label={`${badge} ${labelSuffix}`}
          className={cn(
            "absolute right-1 top-1 inline-block h-1.5 w-1.5 rounded-full",
            dotClass,
          )}
        />
      )}
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
  // Running-pipelines count is hoisted out of the per-NavLink render so the
  // 10s poll runs exactly once for the whole sidebar.
  const runningIncidents = useRunningPipelineCount();
  // Pending-approvals count drives an amber badge on the Approvals row.
  // Owner|admin only — degrades silently to 0 for other roles.
  const pendingApprovals = usePendingApprovalsCount();
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
              <NavLink
                item={item}
                collapsed={collapsed}
                active={isActive(item.href)}
                badge={
                  item.navKey === "incidents" ? runningIncidents : undefined
                }
              />
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
