"use client";

// ActivityTicker — tiny scrolling preview of the last 3 audit events, sat
// above the SystemStatusPill in the sidebar footer. The full firehose lives
// at /console/activity; this widget exists so the operator can spot fresh
// activity without leaving their current page.
//
// Data source: GET /v1/audit?limit=3 (the org-wide audit feed we already
// use on the home page). We poll every 30s. The control-plane will later
// expose a richer activity stream — wire-shape parity is intentional so
// swapping data sources is a 1-line change.
//
// The ticker rotates through the 3 newest rows on a 4s interval; clicking
// any row navigates to /console/activity. Collapsed sidebars hide this
// widget entirely (the dot on the status pill is sufficient signal).

import * as React from "react";
import Link from "next/link";
import type { Route } from "next";

import { cn } from "@/lib/utils";
import { formatRelative } from "@/lib/agents-format";

const POLL_MS = 30_000;
const ROTATE_MS = 4_000;

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

type ActivityRow = {
  id: string;
  actor: string;
  action: string;
  target: string;
  created_at: string;
};

type AuditResp = {
  rows: ActivityRow[];
  total: number;
};

function useRecentActivity(): ActivityRow[] {
  const [rows, setRows] = React.useState<ActivityRow[]>([]);
  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      try {
        const r = await fetch(`${API}/v1/audit?limit=3`, {
          credentials: "include",
          cache: "no-store",
        });
        if (!r.ok) return;
        const body = (await r.json()) as AuditResp;
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
  return rows;
}

function summarize(row: ActivityRow): string {
  // Convert "agent.run.completed" → "Agent run completed". Compact enough
  // for the 240px sidebar.
  const action = row.action
    .replace(/[._]/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
  return action;
}

export function ActivityTicker({ collapsed }: { collapsed: boolean }) {
  const rows = useRecentActivity();
  const [idx, setIdx] = React.useState(0);

  React.useEffect(() => {
    if (rows.length <= 1) return;
    const id = window.setInterval(() => {
      setIdx((i) => (i + 1) % rows.length);
    }, ROTATE_MS);
    return () => window.clearInterval(id);
  }, [rows.length]);

  if (collapsed) return null;
  if (rows.length === 0) return null;
  const row = rows[idx % rows.length];

  return (
    <Link
      href={"/console/activity" as Route}
      aria-label="View activity stream"
      className={cn(
        "block rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-2.5 py-1.5 text-[11px] text-[var(--color-muted-foreground)] transition-colors hover:bg-[var(--color-muted)]",
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="truncate text-[var(--color-foreground)]">
          {summarize(row)}
        </span>
        <span className="shrink-0 font-mono text-[10px]">
          {formatRelative(row.created_at)}
        </span>
      </div>
      <p className="mt-0.5 truncate text-[10px] font-mono">
        {row.target}
      </p>
    </Link>
  );
}
