"use client";

// Phase 3.5 Stage 7 — Workspace switcher pinned in the sidebar.
//
// Reads the workspace list client-side on mount (so it stays fresh after a
// create / suspend in another tab). The "current" workspace is whatever
// the `nexis_workspace` cookie points at; if absent or stale, we fall back
// to the first ready workspace (or the first row, just to keep something
// visible while provisioning).
//
// Selecting an item writes the cookie and calls router.refresh() so the
// server console layout re-runs its current-resolution logic.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import { useRouter } from "next/navigation";
import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { ChevronsUpDown, Plus } from "lucide-react";

import { cn } from "@/lib/utils";
import { workspaces, type Workspace, type WorkspaceStatus } from "@/lib/workspaces";

const COOKIE = "nexis_workspace";

function readCookie(name: string): string | null {
  if (typeof document === "undefined") return null;
  const m = document.cookie.match(
    new RegExp("(?:^|; )" + name.replace(/[.$?*|{}()[\]\\/+^]/g, "\\$&") + "=([^;]*)"),
  );
  return m ? decodeURIComponent(m[1]) : null;
}

function writeCookie(name: string, value: string) {
  document.cookie = `${name}=${encodeURIComponent(value)}; path=/; max-age=${60 * 60 * 24 * 365}; samesite=lax`;
}

const DOT_COLOR: Record<WorkspaceStatus, string> = {
  ready: "bg-emerald-500",
  provisioning: "bg-amber-500",
  error: "bg-red-500",
  suspended: "bg-[var(--color-muted-foreground)]/40",
};

function StatusDot({ status }: { status: WorkspaceStatus }) {
  return (
    <span
      aria-hidden
      className={cn("inline-block h-2 w-2 rounded-full", DOT_COLOR[status])}
    />
  );
}

function pickCurrent(list: Workspace[], cookieId: string | null): Workspace | null {
  if (list.length === 0) return null;
  const byCookie = cookieId ? list.find((w) => w.id === cookieId) : undefined;
  if (byCookie) return byCookie;
  const ready = list.find((w) => w.status === "ready");
  return ready ?? list[0];
}

export function WorkspaceSwitcher({ collapsed = false }: { collapsed?: boolean }) {
  const router = useRouter();
  const [list, setList] = React.useState<Workspace[] | null>(null);
  const [currentId, setCurrentId] = React.useState<string | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    workspaces
      .list()
      .then((ws) => {
        if (cancelled) return;
        setList(ws);
        const cookieId = readCookie(COOKIE);
        const picked = pickCurrent(ws, cookieId);
        setCurrentId(picked?.id ?? null);
      })
      .catch(() => {
        if (!cancelled) setList([]);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  function selectWorkspace(id: string) {
    writeCookie(COOKIE, id);
    setCurrentId(id);
    router.refresh();
  }

  const current = list && currentId ? list.find((w) => w.id === currentId) ?? null : null;

  // Loading skeleton — keep height stable so the sidebar doesn't reflow when
  // the fetch resolves.
  if (!list) {
    return (
      <div
        className={cn(
          "mx-2 mb-1 mt-2 flex items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-card)]",
          collapsed ? "h-10 justify-center" : "h-12 px-3",
        )}
      >
        <span className="inline-block h-2 w-2 rounded-full bg-[var(--color-border)]" aria-hidden />
      </div>
    );
  }

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          aria-label={current ? `Workspace: ${current.name}` : "Choose workspace"}
          className={cn(
            "mx-2 mb-1 mt-2 flex w-[calc(100%-1rem)] items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] text-left text-sm transition-colors hover:bg-[var(--color-muted)]",
            collapsed ? "h-10 justify-center px-2" : "h-12 px-3",
          )}
        >
          {current ? <StatusDot status={current.status} /> : (
            <span className="inline-block h-2 w-2 rounded-full bg-[var(--color-muted-foreground)]/40" />
          )}
          {!collapsed && (
            <>
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium text-[var(--color-foreground)]">
                  {current?.name ?? "No workspace"}
                </div>
                <div className="truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">
                  {current?.region ?? "—"}
                </div>
              </div>
              <ChevronsUpDown className="h-3.5 w-3.5 shrink-0 text-[var(--color-muted-foreground)]" />
            </>
          )}
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="start"
          sideOffset={6}
          className="z-50 min-w-[240px] rounded-md border border-[var(--color-border)] bg-[var(--color-card)] p-1 shadow-md"
        >
          <DropdownMenu.Label className="px-2 py-1.5 text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Workspaces
          </DropdownMenu.Label>
          {list.length === 0 && (
            <p className="px-2 py-2 text-xs text-[var(--color-muted-foreground)]">
              No workspaces yet.
            </p>
          )}
          {list.map((w) => (
            <DropdownMenu.Item
              key={w.id}
              onSelect={(e) => {
                e.preventDefault();
                selectWorkspace(w.id);
              }}
              className={cn(
                "flex w-full cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm outline-none hover:bg-[var(--color-muted)] focus:bg-[var(--color-muted)]",
                w.id === currentId && "bg-[var(--color-muted)]/50",
              )}
            >
              <StatusDot status={w.status} />
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm">{w.name}</div>
                <div className="truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">
                  {w.region}
                </div>
              </div>
            </DropdownMenu.Item>
          ))}
          <DropdownMenu.Separator className="my-1 h-px bg-[var(--color-border)]" />
          <DropdownMenu.Item asChild>
            <Link
              href={"/console/settings/workspaces" as Route}
              className="flex w-full cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm outline-none hover:bg-[var(--color-muted)] focus:bg-[var(--color-muted)]"
            >
              <Plus className="h-4 w-4" />
              Manage workspaces
            </Link>
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
