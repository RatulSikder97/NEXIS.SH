"use client";

// ActivityClient — org-wide live activity stream.
//
// Strategy:
//   1. Try SSE on /v1/orgs/{org}/activity-stream. If it opens we tail the
//      stream, prepending frames to the local list (cap 200 rows).
//   2. If SSE 404s or errors hard, fall back to polling /v1/audit?limit=50
//      every 2s (lightweight).
//   3. The bottom "Detailed logs" OperationalSegments panel reflects the
//      currently filtered slice so operators can see the same payload-rich
//      timeline regardless of which transport delivered the data.
//
// Filters are client-side only — the SSE endpoint is the firehose, so we
// trim in the UI rather than re-establishing the stream on every chip click.

import * as React from "react";
import {
  Activity,
  AlertOctagon,
  ChevronDown,
  ChevronRight,
  PauseCircle,
  PlayCircle,
  RadioTower,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { EmptyState } from "@/components/empty-state/EmptyState";
import {
  OperationalSegments,
  type OperationalSegment,
} from "@/components/console/OperationalSegments";
import { formatRelative } from "@/lib/agents-format";

export type ActivityRow = {
  id: string;
  org_id?: string;
  workspace_id?: string;
  actor: string;
  action: string;
  target: string;
  metadata?: Record<string, unknown> | null;
  created_at: string;
  // Optional richer fields from the activity-stream SSE endpoint
  agent_role?: string;
  workflow_run_id?: string;
  kind?: string;
  severity?: string;
  message?: string;
};

type Filters = {
  agent: string | null;
  kind: string | null;
  severity: string | null;
  workspace: string | null;
};

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const POLL_MS = 2000;
const MAX_ROWS = 200;

function humanize(s: string): string {
  return s.replace(/[._]/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

function inferKind(row: ActivityRow): string {
  if (row.kind) return row.kind;
  // Heuristic: anything containing "error" or "fail" is an error event.
  if (/error|fail/i.test(row.action)) return "error";
  if (/start|begin|queued/i.test(row.action)) return "start";
  if (/finish|complete|done/i.test(row.action)) return "finish";
  return "log";
}

function inferSeverity(row: ActivityRow): string {
  if (row.severity) return row.severity;
  if (inferKind(row) === "error") return "high";
  return "info";
}

function PayloadViewer({ data }: { data: Record<string, unknown> }) {
  const [open, setOpen] = React.useState(false);
  if (!data || Object.keys(data).length === 0) return null;
  return (
    <div className="mt-2">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="inline-flex items-center gap-1 text-[11px] text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
      >
        {open ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
        Show payload
      </button>
      {open && (
        <pre className="mt-1 max-h-80 overflow-auto rounded-md bg-[var(--color-muted)]/40 p-3 text-[11px] font-mono leading-snug text-[var(--color-foreground)]">
          {JSON.stringify(data, null, 2)}
        </pre>
      )}
    </div>
  );
}

function ActivityRowItem({ row }: { row: ActivityRow }) {
  const kind = inferKind(row);
  const severity = inferSeverity(row);
  const kindPalette: Record<string, string> = {
    start: "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
    log: "bg-[var(--color-muted)] text-[var(--color-muted-foreground)] ring-[var(--color-border)]",
    finish:
      "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
    error: "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
  };
  return (
    <li className="rounded-md border border-[var(--color-border)] bg-[var(--color-card)] p-3">
      <div className="flex flex-wrap items-center gap-2">
        <span
          className={cn(
            "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
            kindPalette[kind] ?? kindPalette.log,
          )}
        >
          {kind}
        </span>
        {severity !== "info" && (
          <span
            className={cn(
              "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
              severity === "high"
                ? "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300"
                : "bg-amber-500/15 text-amber-700 ring-amber-500/30 dark:text-amber-300",
            )}
          >
            {severity}
          </span>
        )}
        <span className="font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {formatRelative(row.created_at)}
        </span>
        {row.agent_role && (
          <span className="font-mono text-[11px] text-[var(--color-foreground)]">
            {row.agent_role}
          </span>
        )}
        {row.workflow_run_id && (
          <a
            href={`/console/incidents/${row.workflow_run_id}`}
            className="font-mono text-[11px] text-[var(--color-primary)] hover:underline"
          >
            run {row.workflow_run_id.slice(0, 8)}
          </a>
        )}
      </div>
      <p className="mt-1 text-sm text-[var(--color-foreground)]">
        {row.message ?? humanize(row.action)}
      </p>
      <p className="mt-0.5 truncate font-mono text-[10px] text-[var(--color-muted-foreground)]">
        {row.target}
      </p>
      {row.metadata && (
        <PayloadViewer data={row.metadata as Record<string, unknown>} />
      )}
    </li>
  );
}

function passesFilter(row: ActivityRow, f: Filters): boolean {
  if (f.agent && row.agent_role !== f.agent) return false;
  if (f.kind && inferKind(row) !== f.kind) return false;
  if (f.severity && inferSeverity(row) !== f.severity) return false;
  if (f.workspace && row.workspace_id !== f.workspace) return false;
  return true;
}

function FilterChip({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors",
        active
          ? "border-[var(--color-primary)] bg-[var(--color-primary)]/10 text-[var(--color-primary)]"
          : "border-[var(--color-border)] bg-[var(--color-card)] text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]",
      )}
    >
      {label}
    </button>
  );
}

function rowsToSegments(rows: ActivityRow[]): OperationalSegment[] {
  // The last 25 filtered rows become the "Detailed logs" timeline. We sort
  // by ts ascending so the segment list reads top-down chronologically —
  // matches operator expectations for an audit trail.
  return rows
    .slice(0, 25)
    .slice()
    .reverse()
    .map((r) => {
      const kind = inferKind(r);
      const status =
        kind === "error"
          ? "failed"
          : kind === "start"
            ? "running"
            : kind === "finish"
              ? "succeeded"
              : "succeeded";
      return {
        label: humanize(r.action),
        started_at: r.created_at,
        finished_at: r.created_at,
        duration_ms: 0,
        status,
        detail: r.message ?? r.target,
        sub_steps: r.metadata
          ? [
              {
                label: "metadata",
                status: "captured",
                payload: r.metadata as Record<string, unknown>,
              },
            ]
          : undefined,
      };
    });
}

export function ActivityClient({
  initial,
  orgId,
}: {
  initial: ActivityRow[];
  orgId: string;
}) {
  const [rows, setRows] = React.useState<ActivityRow[]>(initial);
  const [streamUnavailable, setStreamUnavailable] = React.useState(false);
  const [autoScroll, setAutoScroll] = React.useState(true);
  const [filters, setFilters] = React.useState<Filters>({
    agent: null,
    kind: null,
    severity: null,
    workspace: null,
  });
  const listRef = React.useRef<HTMLOListElement | null>(null);

  // Distinct values for filter chips, derived from the current row list.
  const distinct = React.useMemo(() => {
    const agents = new Set<string>();
    const workspaces = new Set<string>();
    for (const r of rows) {
      if (r.agent_role) agents.add(r.agent_role);
      if (r.workspace_id) workspaces.add(r.workspace_id);
    }
    return {
      agents: Array.from(agents).sort(),
      workspaces: Array.from(workspaces).sort(),
    };
  }, [rows]);

  // Try SSE first; fall back to polling if it errors.
  React.useEffect(() => {
    let cancelled = false;
    let es: EventSource | null = null;
    let pollId: number | null = null;

    function startPolling() {
      if (pollId !== null) return;
      pollId = window.setInterval(async () => {
        try {
          const r = await fetch(`${API}/v1/audit?limit=50`, {
            credentials: "include",
            cache: "no-store",
          });
          if (!r.ok) return;
          const body = (await r.json()) as { rows: ActivityRow[] };
          if (cancelled) return;
          setRows((prev) => {
            const seen = new Set(prev.map((p) => p.id));
            const additions = (body.rows ?? []).filter((r2) => !seen.has(r2.id));
            return [...additions, ...prev].slice(0, MAX_ROWS);
          });
        } catch {
          // ignore — next tick retries
        }
      }, POLL_MS);
    }

    // Stream setup runs in a microtask so any setState calls (e.g. when SSE
    // construction throws synchronously) land outside the effect body and
    // don't trip react-hooks/set-state-in-effect.
    queueMicrotask(() => {
      if (cancelled) return;
      try {
        es = new EventSource(
          `${API}/v1/orgs/${orgId}/activity-stream`,
          { withCredentials: true },
        );
        es.onmessage = (ev) => {
          try {
            const parsed = JSON.parse(ev.data) as ActivityRow;
            if (cancelled) return;
            setRows((prev) => [parsed, ...prev].slice(0, MAX_ROWS));
          } catch {
            // ignore malformed frames
          }
        };
        es.onerror = () => {
          if (cancelled) return;
          // Once the browser auto-reconnect retries fail, mark SSE
          // unavailable and start the fallback poller. We treat any
          // persistent error as "fall back" since the audit poll is cheap.
          setStreamUnavailable(true);
          try {
            es?.close();
          } catch {
            // ignore
          }
          startPolling();
        };
      } catch {
        if (cancelled) return;
        setStreamUnavailable(true);
        startPolling();
      }
    });

    return () => {
      cancelled = true;
      try {
        es?.close();
      } catch {
        // ignore
      }
      if (pollId !== null) window.clearInterval(pollId);
    };
  }, [orgId]);

  React.useEffect(() => {
    if (!autoScroll) return;
    if (!listRef.current) return;
    listRef.current.scrollTop = 0;
  }, [rows, autoScroll]);

  const filtered = rows.filter((r) => passesFilter(r, filters));

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Operations
          </p>
          <h1 className="text-2xl font-semibold">Activity stream</h1>
          <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
            Real-time activity across the org. Frames arrive via SSE when the
            org-wide stream endpoint is online; otherwise we poll the audit
            feed every 2s and degrade gracefully.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setAutoScroll((v) => !v)}
            className="inline-flex items-center gap-1.5 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-1.5 text-xs text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
          >
            {autoScroll ? (
              <>
                <PauseCircle className="h-3 w-3" /> Pause auto-scroll
              </>
            ) : (
              <>
                <PlayCircle className="h-3 w-3" /> Resume auto-scroll
              </>
            )}
          </button>
          <span
            className={cn(
              "inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px] font-medium ring-1",
              streamUnavailable
                ? "bg-amber-500/10 text-amber-700 ring-amber-500/30 dark:text-amber-300"
                : "bg-emerald-500/10 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
            )}
          >
            <RadioTower className="h-3 w-3" />
            {streamUnavailable ? "Polling fallback" : "Live"}
          </span>
        </div>
      </div>

      <section className="space-y-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
        <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Filters
        </p>
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
              Kind
            </span>
            {(["start", "log", "finish", "error"] as const).map((k) => (
              <FilterChip
                key={k}
                label={k}
                active={filters.kind === k}
                onClick={() =>
                  setFilters((f) => ({ ...f, kind: f.kind === k ? null : k }))
                }
              />
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
              Severity
            </span>
            {(["high", "medium", "info"] as const).map((s) => (
              <FilterChip
                key={s}
                label={s}
                active={filters.severity === s}
                onClick={() =>
                  setFilters((f) => ({
                    ...f,
                    severity: f.severity === s ? null : s,
                  }))
                }
              />
            ))}
          </div>
          {distinct.agents.length > 0 && (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
                Agent
              </span>
              {distinct.agents.map((a) => (
                <FilterChip
                  key={a}
                  label={a}
                  active={filters.agent === a}
                  onClick={() =>
                    setFilters((f) => ({
                      ...f,
                      agent: f.agent === a ? null : a,
                    }))
                  }
                />
              ))}
            </div>
          )}
          {distinct.workspaces.length > 0 && (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
                Workspace
              </span>
              {distinct.workspaces.map((w) => (
                <FilterChip
                  key={w}
                  label={w.slice(0, 8)}
                  active={filters.workspace === w}
                  onClick={() =>
                    setFilters((f) => ({
                      ...f,
                      workspace: f.workspace === w ? null : w,
                    }))
                  }
                />
              ))}
            </div>
          )}
        </div>
      </section>

      <section
        aria-labelledby="stream-heading"
        className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]"
      >
        <header className="flex items-baseline justify-between gap-3 border-b border-[var(--color-border)] px-5 py-3">
          <div>
            <h2
              id="stream-heading"
              className="text-sm font-semibold text-[var(--color-foreground)]"
            >
              Live frames
            </h2>
            <p className="mt-0.5 text-xs text-[var(--color-muted-foreground)]">
              {filtered.length} of {rows.length} rows{" "}
              {filtered.length !== rows.length && "filtered"}
            </p>
          </div>
          {streamUnavailable && (
            <span className="inline-flex items-center gap-1 text-[11px] text-amber-700 dark:text-amber-300">
              <AlertOctagon className="h-3 w-3" /> Stream endpoint unavailable
              — polling audit feed
            </span>
          )}
        </header>
        {filtered.length === 0 ? (
          <div className="p-6">
            <EmptyState
              icon={Activity}
              title="No activity frames"
              description="Either nothing has happened yet or the filters are too tight."
            />
          </div>
        ) : (
          <ol
            ref={listRef}
            className="max-h-[60vh] space-y-2 overflow-y-auto p-3"
          >
            {filtered.map((r) => (
              <ActivityRowItem key={r.id} row={r} />
            ))}
          </ol>
        )}
      </section>

      <OperationalSegments
        title="Operational segments"
        description="Most recent 25 frames after filters, oldest first."
        segments={rowsToSegments(filtered)}
        emptyMessage="No matching segments. Adjust filters to widen the slice."
      />
    </div>
  );
}
