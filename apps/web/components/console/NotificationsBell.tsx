"use client";

// NotificationsBell — topbar dropdown surfacing the 5 most recent audit
// events. Polls /v1/audit?limit=5 every 30s. The bell glows amber when there
// are unread events (we use a sessionStorage cursor of last-seen id) and
// resets the moment the dropdown opens.
//
// This is intentionally backed by the audit feed today; once the org-wide
// activity stream lands the data source flips with a 1-line change (the SSE
// frame shape is a superset). "View all" links to /console/activity.

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";
import { Bell, ExternalLink } from "lucide-react";

import { cn } from "@/lib/utils";
import { formatRelative } from "@/lib/agents-format";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const POLL_MS = 30_000;
const STORAGE_KEY = "nexis_notifications_seen";

type Row = {
  id: string;
  actor: string;
  action: string;
  target: string;
  created_at: string;
};

function humanize(action: string): string {
  return action.replace(/[._]/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

function useNotifications(): {
  rows: Row[];
  unread: number;
  markSeen: () => void;
} {
  const [rows, setRows] = React.useState<Row[]>([]);
  const [seenId, setSeenId] = React.useState<string | null>(() => {
    if (typeof window === "undefined") return null;
    try {
      return window.sessionStorage.getItem(STORAGE_KEY);
    } catch {
      return null;
    }
  });

  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      try {
        const r = await fetch(`${API}/v1/audit?limit=5`, {
          credentials: "include",
          cache: "no-store",
        });
        if (!r.ok) return;
        const body = (await r.json()) as { rows: Row[] };
        if (cancelled) return;
        setRows(body.rows ?? []);
      } catch {
        // ignore — next tick retries
      }
    }
    void tick();
    const id = window.setInterval(tick, POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);

  let unread = 0;
  for (const r of rows) {
    if (seenId === r.id) break;
    unread++;
  }

  function markSeen() {
    if (rows.length === 0) return;
    const newest = rows[0].id;
    try {
      window.sessionStorage.setItem(STORAGE_KEY, newest);
    } catch {
      // ignore
    }
    setSeenId(newest);
  }

  return { rows, unread, markSeen };
}

export function NotificationsBell() {
  const { rows, unread, markSeen } = useNotifications();
  const [open, setOpen] = React.useState(false);
  const ref = React.useRef<HTMLDivElement | null>(null);

  React.useEffect(() => {
    if (!open) return;
    function onClick(e: MouseEvent) {
      if (!ref.current) return;
      if (!ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    window.addEventListener("mousedown", onClick);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onClick);
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);

  function toggle() {
    const next = !open;
    setOpen(next);
    if (next) markSeen();
  }

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={toggle}
        aria-label={
          unread > 0
            ? `${unread} unread notifications`
            : "Open notifications"
        }
        className="relative inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
      >
        <Bell className="h-4 w-4" />
        {unread > 0 && (
          <span
            aria-hidden
            className={cn(
              "absolute right-1 top-1 inline-flex h-4 min-w-[1rem] items-center justify-center rounded-full bg-amber-500 px-1 text-[9px] font-semibold text-white ring-2 ring-[var(--color-background)]",
            )}
          >
            {unread > 9 ? "9+" : unread}
          </span>
        )}
      </button>
      {open && (
        <div
          role="menu"
          className="absolute right-0 z-50 mt-2 w-80 overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] shadow-xl"
        >
          <div className="border-b border-[var(--color-border)] px-3 py-2">
            <p className="text-[11px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
              Recent activity
            </p>
          </div>
          {rows.length === 0 ? (
            <p className="px-3 py-6 text-center text-xs text-[var(--color-muted-foreground)]">
              Nothing new — your audit feed is quiet.
            </p>
          ) : (
            <ul className="max-h-80 overflow-y-auto py-1">
              {rows.map((row) => (
                <li
                  key={row.id}
                  className="border-b border-[var(--color-border)]/50 px-3 py-2 last:border-b-0"
                >
                  <div className="flex items-baseline justify-between gap-2">
                    <span className="truncate text-xs font-medium text-[var(--color-foreground)]">
                      {humanize(row.action)}
                    </span>
                    <span className="shrink-0 font-mono text-[10px] text-[var(--color-muted-foreground)]">
                      {formatRelative(row.created_at)}
                    </span>
                  </div>
                  <p className="mt-0.5 truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">
                    {row.target}
                  </p>
                </li>
              ))}
            </ul>
          )}
          <Link
            href={"/console/activity" as Route}
            onClick={() => setOpen(false)}
            className="flex items-center justify-between gap-2 border-t border-[var(--color-border)] bg-[var(--color-muted)]/30 px-3 py-2 text-xs font-medium text-[var(--color-foreground)] hover:bg-[var(--color-muted)]"
          >
            <span>View all activity</span>
            <ExternalLink className="h-3 w-3" />
          </Link>
        </div>
      )}
    </div>
  );
}
