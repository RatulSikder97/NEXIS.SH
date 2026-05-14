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
import * as Dialog from "@radix-ui/react-dialog";
import { Menu, X } from "lucide-react";

import { ThemeToggle } from "@/components/ThemeToggle";
import { CommandPalette } from "@/components/console/CommandPalette";
import { NotificationsBell } from "@/components/console/NotificationsBell";
import { Sidebar } from "@/components/console/Sidebar";
import { UserMenu } from "@/components/console/UserMenu";
import { WorkspaceBadge } from "@/components/workspaces/WorkspaceBadge";
import { evalApi, formatTokens, type BudgetStatus } from "@/lib/eval";

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

// COOKIE_WORKSPACE matches the one the Sidebar reads. We re-derive the id
// here instead of threading it through props so the Topbar stays a
// drop-in component the layout can mount without extra wiring.
const COOKIE_WORKSPACE = "nexis_workspace";
const BUDGET_POLL_MS = 30_000;

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

// useBudget polls /v1/workspaces/{ws}/agents/budget every 30s and returns
// the latest snapshot. Returns null until the first response resolves so
// the pill stays hidden until we actually have something to show — we
// want the topbar to look identical to its pre-Stage-8 state when the
// org has never burned a token.
function useBudget(): BudgetStatus | null {
  const [budget, setBudget] = React.useState<BudgetStatus | null>(null);
  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      const wsId = readWorkspaceCookie();
      if (!wsId) {
        if (!cancelled) setBudget(null);
        return;
      }
      try {
        const next = await evalApi.budget(wsId);
        if (!cancelled) setBudget(next);
      } catch {
        // swallow — next tick retries. Don't clear `budget` because a
        // transient 5xx shouldn't make the pill disappear mid-session.
      }
    }
    void tick();
    const id = window.setInterval(tick, BUDGET_POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);
  return budget;
}

function BudgetPill() {
  const b = useBudget();
  // Hide the pill when we have no data, or the org's allowance is 0
  // (defensive — a budget row with allowed_=0 means "uncapped" upstream
  // but we treat it as "don't render" so the topbar doesn't lie).
  if (!b) return null;
  const used = b.used_tokens_in + b.used_tokens_out;
  const allowed = b.allowed_tokens_in + b.allowed_tokens_out;
  if (allowed <= 0 || used === 0) return null;
  const ratio = used / allowed;
  // Three tones: green under 70%, amber 70–90%, red ≥90%. Picked to match
  // the operator's mental model of "fine / heads-up / about to be cut off".
  const tone =
    ratio >= 0.9
      ? "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300"
      : ratio >= 0.7
        ? "bg-amber-500/15 text-amber-700 ring-amber-500/30 dark:text-amber-300"
        : "bg-[var(--color-muted)] text-[var(--color-muted-foreground)] ring-[var(--color-border)]";
  return (
    <span
      title={`Token usage this period · ${used.toLocaleString()} / ${allowed.toLocaleString()}`}
      className={`hidden items-center rounded-full px-2.5 py-1 text-[11px] font-medium ring-1 sm:inline-flex ${tone}`}
    >
      <span className="font-mono">
        {formatTokens(used)}/{formatTokens(allowed)} tokens
      </span>
    </span>
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

// MobileSidebarSheet — the hamburger button + Radix Dialog that wraps the
// Sidebar in `mobile` mode. We keep open-state local to the Topbar because
// it's the only consumer; the Dialog auto-handles focus trap, Esc-to-close,
// overlay click-to-close, and inert-content for screen readers.
function MobileSidebarSheet() {
  const [open, setOpen] = React.useState(false);
  // Close the sheet whenever the route changes — every Link inside the
  // sidebar already fires `onNavigate` for this, but path changes from
  // other sources (workspace switcher, redirect, back/forward) should
  // also dismiss it.
  const pathname = usePathname();
  React.useEffect(() => {
    setOpen(false);
  }, [pathname]);

  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Trigger asChild>
        <button
          type="button"
          aria-label="Open navigation"
          className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-[var(--color-border)] bg-[var(--color-card)] text-[var(--color-foreground)] transition-colors hover:bg-[var(--color-muted)] md:hidden"
        >
          <Menu className="h-4 w-4" aria-hidden />
        </button>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay
          className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 md:hidden"
        />
        <Dialog.Content
          aria-describedby={undefined}
          className="fixed inset-y-0 left-0 z-50 flex h-full w-[80vw] max-w-[300px] flex-col bg-[var(--color-card)] shadow-xl outline-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:slide-out-to-left data-[state=open]:slide-in-from-left data-[state=open]:duration-200 md:hidden"
        >
          <Dialog.Title className="sr-only">Console navigation</Dialog.Title>
          <Dialog.Close asChild>
            <button
              type="button"
              aria-label="Close navigation"
              className="absolute right-2 top-2 inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
            >
              <X className="h-4 w-4" aria-hidden />
            </button>
          </Dialog.Close>
          <Sidebar variant="mobile" onNavigate={() => setOpen(false)} />
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
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
      <header className="sticky top-0 z-20 flex h-14 items-center justify-between gap-3 border-b border-[var(--color-border)] bg-[var(--color-background)]/95 px-4 backdrop-blur md:px-6">
        <div className="flex min-w-0 items-center gap-3">
          <MobileSidebarSheet />
          {currentWorkspace && (
            <>
              <WorkspaceBadge
                name={currentWorkspace.name}
                region={currentWorkspace.region}
              />
              <span
                aria-hidden
                className="hidden h-5 w-px bg-[var(--color-border)] sm:inline-block"
              />
            </>
          )}
          <div className="hidden min-w-0 sm:block">
            <Breadcrumb />
          </div>
        </div>
        <div className="flex items-center gap-2">
          <BudgetPill />
          <button
            type="button"
            onClick={openPalette}
            aria-label="Open command palette"
            className="inline-flex items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-1.5 text-sm text-[var(--color-muted-foreground)] transition-colors hover:border-[var(--color-primary)]/40 hover:text-[var(--color-foreground)]"
          >
            <span>Jump to…</span>
            <kbd className="rounded border border-[var(--color-border)] bg-[var(--color-muted)] px-1.5 py-0.5 text-[10px] font-mono text-[var(--color-muted-foreground)]">
              ⌘K
            </kbd>
          </button>
          <NotificationsBell />
          <ThemeToggle />
          <UserMenu email={userEmail} />
        </div>
      </header>
      <CommandPalette />
    </>
  );
}
