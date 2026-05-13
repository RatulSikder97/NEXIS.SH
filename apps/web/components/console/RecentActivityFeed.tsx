"use client";

// RecentActivityFeed — last 10 audit events for the org, polled at 30s.
// Replaces the static table on the home page so the dashboard always shows
// fresh activity. Each row links to /console/audit with the actor filter
// pre-filled; the "View all" button at the bottom links to the firehose at
// /console/activity.

import * as React from "react";
import Link from "next/link";
import { ChevronRight } from "lucide-react";

import { formatRelative } from "@/lib/agents-format";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const POLL_MS = 30_000;

type Row = {
  id: string;
  actor: string;
  action: string;
  target: string;
  created_at: string;
};

type Resp = {
  rows: Row[];
  total: number;
};

function humanize(s: string): string {
  return s.replace(/[._]/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

export function RecentActivityFeed({ initial }: { initial: Row[] }) {
  const [rows, setRows] = React.useState<Row[]>(initial);

  React.useEffect(() => {
    let cancelled = false;
    async function tick() {
      try {
        const r = await fetch(`${API}/v1/audit?limit=10`, {
          credentials: "include",
          cache: "no-store",
        });
        if (!r.ok) return;
        const body = (await r.json()) as Resp;
        if (cancelled) return;
        setRows(body.rows ?? []);
      } catch {
        // ignore
      }
    }
    void tick();
    const id = window.setInterval(tick, POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);

  return (
    <section
      aria-labelledby="recent-activity-heading"
      className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]"
    >
      <header className="flex items-baseline justify-between gap-3 border-b border-[var(--color-border)] px-5 py-3">
        <div>
          <h2
            id="recent-activity-heading"
            className="text-sm font-semibold text-[var(--color-foreground)]"
          >
            Recent activity
          </h2>
          <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
            Latest 10 audit events across the org.
          </p>
        </div>
        <Link
          href={"/console/activity" as never}
          className="text-xs font-medium text-[var(--color-primary)] hover:underline"
        >
          View all →
        </Link>
      </header>

      {rows.length === 0 ? (
        <p className="px-5 py-6 text-center text-sm text-[var(--color-muted-foreground)]">
          No activity yet. As your team uses NEXIS, events will appear here.
        </p>
      ) : (
        <ul className="divide-y divide-[var(--color-border)]">
          {rows.map((row) => (
            <li
              key={row.id}
              className="flex items-center gap-3 px-5 py-2.5 transition-colors hover:bg-[var(--color-muted)]/30"
            >
              <span
                aria-hidden
                className="inline-block h-1.5 w-1.5 rounded-full bg-[var(--color-primary)]/40"
              />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm text-[var(--color-foreground)]">
                  {humanize(row.action)}
                </p>
                <p className="truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">
                  {row.target}
                </p>
              </div>
              <span className="shrink-0 font-mono text-[11px] text-[var(--color-muted-foreground)]">
                {formatRelative(row.created_at)}
              </span>
              <ChevronRight className="h-3 w-3 shrink-0 text-[var(--color-muted-foreground)]" />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
