"use client";

// Console sidebar — operational-grade navigation for an agentic SRE / incident
// recovery surface.
//
// Six sections (Workspace + Fleet + Observability + Integrations + Operations
// + Settings) with a sticky footer that surfaces live system health and
// last-3-activity rotation. Widths still 240px expanded / 64px collapsed so
// the static `ml-60` gutter in ConsoleLayout keeps working without a
// hydration dance.
//
// Live signals on the sidebar:
//   * Incidents — running pipeline count (10s poll), blue pill
//   * Approvals — pending approvals count (10s poll), amber pill
//   * Workflows — same running pipeline count, mirrors Incidents row
//   * Integrations — connected/total (slow 60s poll)
//   * SystemStatusPill — aggregates integration health (30s poll), bottom
//   * ActivityTicker — last 3 audit rows rotating (30s poll), bottom
//
// All polls are cheap and degrade silently on transient errors. Endpoints
// referenced by new sections (system-health, system-status, validator/runs,
// integrations/webhooks, integrations/{provider}/probe) MAY 404 today; pages
// individually handle that and render the EmptyState "Endpoint coming soon".

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Activity,
  AlertTriangle,
  Beaker,
  BookOpen,
  Boxes,
  CheckSquare,
  ChevronsLeft,
  ChevronsRight,
  Container,
  DollarSign,
  Gauge,
  HeartPulse,
  LayoutDashboard,
  Network,
  PlayCircle,
  Plug,
  Radio,
  ScrollText,
  Settings,
  ShieldAlert,
  Webhook,
  Workflow,
  Wrench,
  Users,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { pipelines } from "@/lib/pipelines";
import { approvals } from "@/lib/approvals";
import { integrations } from "@/lib/integrations";
import { projects } from "@/lib/projects";
import { WorkspaceSwitcher } from "@/components/workspaces/WorkspaceSwitcher";
import { SystemStatusPill } from "@/components/console/SystemStatusPill";
import { ActivityTicker } from "@/components/console/ActivityTicker";
import { readyCount as readyDemoScenariosCount } from "@/components/live-demo/SCENARIOS";

type BadgeTone = "info" | "warn" | "ok" | "muted";

type NavItem = {
  href: string;
  label: string;
  icon: LucideIcon;
  // navKey lets a NavLink consumer correlate this row with side-channel
  // state (e.g. a running-count badge on Incidents).
  navKey?:
    | "incidents"
    | "approvals"
    | "workflows"
    | "agents"
    | "integrations"
    | "validator"
    | "projects";
  // Constant badge text (e.g. "9" on Agents) — overrides dynamic counts.
  constantBadge?: string;
  // Optional secondary line shown below the label (expanded mode only).
  secondary?: string;
};

const COOKIE_WORKSPACE = "nexis_workspace";
const INCIDENTS_POLL_MS = 10_000;
const APPROVALS_POLL_MS = 10_000;
const INTEGRATIONS_POLL_MS = 60_000;
const PROJECTS_POLL_MS = 60_000;

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
        // 403 (member role) and transient errors both land here.
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

function useProjectsCount(): number {
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
        const rows = await projects.list(wsId);
        if (cancelled) return;
        setCount(rows.length);
      } catch {
        // swallow — endpoint may not be live yet.
      }
    }
    void tick();
    const id = window.setInterval(tick, PROJECTS_POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);
  return count;
}

function useIntegrationsConnectedSummary(): { connected: number; total: number } {
  const [s, setS] = React.useState<{ connected: number; total: number }>({
    connected: 0,
    total: 6,
  });
  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      try {
        const rows = await integrations.listConnections();
        if (cancelled) return;
        let connected = 0;
        for (const r of rows) if (r.connected) connected++;
        setS({ connected, total: rows.length || 6 });
      } catch {
        // ignore
      }
    }
    void tick();
    const id = window.setInterval(tick, INTEGRATIONS_POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);
  return s;
}

const STORAGE_KEY = "nexis_sidebar_collapsed";

// Sections rendered top → bottom. Sticky Settings footer is rendered
// separately so it doesn't get cut off by overflow on tall sidebars.
const SECTION_WORKSPACE: { label: string; items: NavItem[] } = {
  label: "Workspace",
  items: [
    { href: "/console", label: "Dashboard", icon: LayoutDashboard },
    {
      href: "/console/projects",
      label: "Projects",
      icon: Boxes,
      navKey: "projects",
    },
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
    { href: "/console/audit", label: "Audit Log", icon: ScrollText },
  ],
};

const SECTION_FLEET: { label: string; items: NavItem[] } = {
  label: "Fleet",
  items: [
    {
      href: "/console/agents",
      label: "Agents",
      icon: Users,
      navKey: "agents",
      constantBadge: "9",
    },
    {
      href: "/console/workflows",
      label: "Workflows",
      icon: Workflow,
      navKey: "workflows",
    },
    { href: "/console/activity", label: "Activity Stream", icon: Activity },
    {
      href: "/console/validator",
      label: "Validator Sandbox",
      icon: Container,
      navKey: "validator",
    },
  ],
};

const SECTION_OBSERVABILITY: { label: string; items: NavItem[] } = {
  label: "Observability",
  items: [
    { href: "/console/performance", label: "Performance", icon: Gauge },
    { href: "/console/cost", label: "Cost Tracker", icon: DollarSign },
    { href: "/console/health", label: "System Health", icon: HeartPulse },
  ],
};

const SECTION_INTEGRATIONS: { label: string; items: NavItem[] } = {
  label: "Integrations",
  items: [
    {
      href: "/console/integrations",
      label: "All Integrations",
      icon: Plug,
      navKey: "integrations",
    },
    { href: "/console/webhooks", label: "Webhook Activity", icon: Webhook },
    {
      href: "/console/connections",
      label: "Connection Health",
      icon: Network,
    },
  ],
};

const SECTION_OPERATIONS: { label: string; items: NavItem[] } = {
  label: "Operations",
  items: [
    {
      href: "/console/recovery",
      label: "Recovery Pipeline",
      icon: ShieldAlert,
    },
    { href: "/console/eval", label: "Eval Harness", icon: Beaker },
    {
      href: "/console/live-demo",
      label: "Live Demo",
      icon: PlayCircle,
      // Constant badge surfaces the count of fully-wired scenarios so the
      // operator knows how many fault scripts the backend will actually
      // run today. Grows in lockstep with the SCENARIOS catalog.
      constantBadge: String(readyDemoScenariosCount()),
    },
    { href: "/console/knowledge", label: "Knowledge Base", icon: BookOpen },
  ],
};

function BadgePill({
  count,
  tone,
  collapsed,
  label,
}: {
  count: string;
  tone: BadgeTone;
  collapsed: boolean;
  label: string;
}) {
  const classes: Record<BadgeTone, string> = {
    info: "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
    warn: "bg-amber-500/15 text-amber-700 ring-amber-500/30 dark:text-amber-300",
    ok: "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
    muted:
      "bg-[var(--color-muted)] text-[var(--color-muted-foreground)] ring-[var(--color-border)]",
  };
  const dots: Record<BadgeTone, string> = {
    info: "bg-blue-500",
    warn: "bg-amber-500",
    ok: "bg-emerald-500",
    muted: "bg-[var(--color-muted-foreground)]/60",
  };
  if (collapsed) {
    return (
      <span
        aria-label={label}
        className={cn(
          "absolute right-1.5 top-1.5 inline-block h-1.5 w-1.5 rounded-full",
          dots[tone],
        )}
      />
    );
  }
  return (
    <span
      aria-label={label}
      className={cn(
        "ml-auto inline-flex min-w-[1.5rem] items-center justify-center rounded-full px-1.5 py-0.5 text-[10px] font-medium ring-1",
        classes[tone],
      )}
    >
      {count}
    </span>
  );
}

function NavLink({
  item,
  collapsed,
  active,
  badge,
  badgeTone = "info",
  badgeLabel,
  onNavigate,
}: {
  item: NavItem;
  collapsed: boolean;
  active: boolean;
  badge?: string;
  badgeTone?: BadgeTone;
  badgeLabel?: string;
  onNavigate?: () => void;
}) {
  const Icon = item.icon;
  const showBadge = typeof badge === "string" && badge.length > 0;
  const labelSuffix = badgeLabel ?? "items";
  return (
    <Link
      // typedRoutes treats the href as Route; cast through unknown for the
      // narrow string union we store in NavItem.
      href={item.href as unknown as never}
      title={collapsed ? item.label : undefined}
      aria-current={active ? "page" : undefined}
      onClick={onNavigate}
      className={cn(
        "group relative flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors",
        active
          ? "bg-[var(--color-muted)]/70 text-[var(--color-foreground)] font-medium"
          : "text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)]/60 hover:text-[var(--color-foreground)]",
      )}
    >
      {/* 2px left ring for active rows — less heavy than full bg-fill */}
      {active && !collapsed && (
        <span
          aria-hidden
          className="absolute inset-y-1 left-0 w-[2px] rounded-r-full bg-[var(--color-primary)]"
        />
      )}
      <Icon
        className={cn(
          "h-4 w-4 shrink-0",
          active ? "text-[var(--color-foreground)]" : undefined,
        )}
      />
      {!collapsed && (
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate leading-none">{item.label}</span>
          {item.secondary && (
            <span className="mt-0.5 truncate text-[10px] text-[var(--color-muted-foreground)]">
              {item.secondary}
            </span>
          )}
        </span>
      )}
      {showBadge && (
        <BadgePill
          count={badge}
          tone={badgeTone}
          collapsed={collapsed}
          label={`${badge} ${labelSuffix}`}
        />
      )}
    </Link>
  );
}

function SectionLabel({
  label,
  collapsed,
}: {
  label: string;
  collapsed: boolean;
}) {
  if (collapsed) {
    return (
      <div
        className="my-2 border-t border-[var(--color-border)]"
        aria-hidden
      />
    );
  }
  return (
    <p className="px-3 pb-1 pt-4 text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
      {label}
    </p>
  );
}

// Module-level "tick" counter so external mutations via toggle() can wake
// every subscriber on the page. Required by useSyncExternalStore.
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

function badgeFor(
  navKey: NavItem["navKey"],
  signals: {
    incidents: number;
    approvals: number;
    integrationsConnected: number;
    integrationsTotal: number;
    projects: number;
  },
): { count?: string; tone: BadgeTone; label: string } | null {
  switch (navKey) {
    case "incidents":
      return signals.incidents > 0
        ? { count: String(signals.incidents), tone: "info", label: "running" }
        : null;
    case "workflows":
      return signals.incidents > 0
        ? { count: String(signals.incidents), tone: "info", label: "running" }
        : null;
    case "approvals":
      return signals.approvals > 0
        ? { count: String(signals.approvals), tone: "warn", label: "pending" }
        : null;
    case "integrations":
      return {
        count: `${signals.integrationsConnected}/${signals.integrationsTotal}`,
        tone:
          signals.integrationsConnected === signals.integrationsTotal
            ? "ok"
            : signals.integrationsConnected === 0
              ? "muted"
              : "info",
        label: "connected",
      };
    case "projects":
      return signals.projects > 0
        ? { count: String(signals.projects), tone: "muted", label: "projects" }
        : null;
    default:
      return null;
  }
}

type SidebarVariant = "desktop" | "mobile";

export function Sidebar({
  variant = "desktop",
  onNavigate,
}: {
  /**
   * `desktop` (default): renders the chrome we ship on >=md viewports — a
   * fixed-position rail with localStorage-driven collapse.
   *
   * `mobile`: renders inside a Sheet (Radix Dialog) opened from the
   * Topbar hamburger. Drops the `fixed` positioning so the Dialog content
   * owns layout, forces the rail expanded (no collapse toggle on a
   * temporary panel), and reports link clicks via `onNavigate` so the
   * caller can dismiss the sheet.
   */
  variant?: SidebarVariant;
  onNavigate?: () => void;
} = {}) {
  const pathname = usePathname();
  const persistedCollapsed = React.useSyncExternalStore(
    subscribe,
    readCollapsed,
    () => false,
  );
  // Mobile always renders expanded — a temporary panel that hides the
  // labels would be doubly cramped.
  const collapsed = variant === "mobile" ? false : persistedCollapsed;
  const runningIncidents = useRunningPipelineCount();
  const pendingApprovals = usePendingApprovalsCount();
  const integrationsSummary = useIntegrationsConnectedSummary();
  const projectsCount = useProjectsCount();
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

  const width =
    variant === "mobile" ? "w-full" : collapsed ? "w-16" : "w-60";
  const containerClass =
    variant === "mobile"
      ? "flex h-full min-h-0 flex-col bg-[var(--color-card)]"
      : cn(
          "fixed inset-y-0 left-0 z-30 hidden min-h-screen flex-col border-r border-[var(--color-border)] bg-[var(--color-card)] md:flex",
          width,
        );

  function isActive(href: string) {
    if (!pathname) return false;
    if (href === "/console") return pathname === "/console";
    return pathname === href || pathname.startsWith(href + "/");
  }

  const signals = {
    incidents: runningIncidents,
    approvals: pendingApprovals,
    integrationsConnected: integrationsSummary.connected,
    integrationsTotal: integrationsSummary.total,
    projects: projectsCount,
  };

  const sections = [
    SECTION_WORKSPACE,
    SECTION_FLEET,
    SECTION_OBSERVABILITY,
    SECTION_INTEGRATIONS,
    SECTION_OPERATIONS,
  ];

  return (
    <aside className={containerClass}>
      <div className="flex h-14 shrink-0 items-center justify-between border-b border-[var(--color-border)] px-3">
        {!collapsed && (
          <span className="flex items-center gap-2 text-sm font-semibold tracking-tight text-[var(--color-foreground)]">
            <Wrench className="h-4 w-4 text-[var(--color-primary)]" />
            NEXIS
          </span>
        )}
        {variant === "desktop" && (
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
        )}
      </div>

      <WorkspaceSwitcher collapsed={collapsed} />

      <nav
        className="flex-1 overflow-y-auto px-2 py-2"
        aria-label="Console navigation"
      >
        {sections.map((section, sIdx) => (
          <div key={section.label} className={sIdx === 0 ? "" : ""}>
            {sIdx > 0 && <SectionLabel label={section.label} collapsed={collapsed} />}
            {sIdx === 0 && (
              <SectionLabel label={section.label} collapsed={collapsed} />
            )}
            <ul className="space-y-0.5">
              {section.items.map((item) => {
                const dynamic = badgeFor(item.navKey, signals);
                const badge = item.constantBadge ?? dynamic?.count;
                const tone: BadgeTone =
                  item.constantBadge !== undefined
                    ? "muted"
                    : (dynamic?.tone ?? "info");
                const label = dynamic?.label ?? "items";
                return (
                  <li key={item.href}>
                    <NavLink
                      item={item}
                      collapsed={collapsed}
                      active={isActive(item.href)}
                      badge={badge}
                      badgeTone={tone}
                      badgeLabel={label}
                      onNavigate={onNavigate}
                    />
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </nav>

      <div className="shrink-0 border-t border-[var(--color-border)] px-2 py-2">
        <NavLink
          item={{
            href: "/console/settings/profile",
            label: "Settings",
            icon: Settings,
          }}
          collapsed={collapsed}
          active={isActive("/console/settings")}
          onNavigate={onNavigate}
        />
      </div>

      <div className="shrink-0 space-y-1.5 border-t border-[var(--color-border)] bg-[var(--color-card)] px-2 py-2">
        {!collapsed && (
          <p className="px-1 pb-0.5 text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
            <Radio className="mr-1 inline h-3 w-3" aria-hidden /> Live
          </p>
        )}
        <ActivityTicker collapsed={collapsed} />
        <SystemStatusPill collapsed={collapsed} />
      </div>
    </aside>
  );
}
