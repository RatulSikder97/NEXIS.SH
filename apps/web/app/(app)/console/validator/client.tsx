"use client";

// ValidatorClient — sandbox status + recent runs table.
//
// KPIs: queue depth, in-flight, success rate (24h), avg duration. Each is
// a small card; we surface "—" if the endpoint didn't return it.
//
// The recent-runs table shows patch_sha, status, duration, and the first 3
// lines of stdout / stderr in a collapsible block. Operational segments at
// the bottom timeline the most recent 10 runs.

import * as React from "react";
import {
  AlertOctagon,
  ChevronDown,
  ChevronRight,
  Container,
  Loader2,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { EmptyState } from "@/components/empty-state/EmptyState";
import {
  OperationalSegments,
  type OperationalSegment,
  type SegmentStatus,
} from "@/components/console/OperationalSegments";
import { formatDurationMs, formatRelative } from "@/lib/agents-format";

export type ValidatorStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "timed_out";

export type ValidatorRunRow = {
  id: string;
  workflow_run_id?: string;
  patch_sha?: string;
  status: ValidatorStatus;
  started_at: string;
  finished_at?: string;
  duration_ms?: number;
  stdout_head?: string;
  stderr_head?: string;
  error?: string;
};

type ValidatorListResp = {
  runs: ValidatorRunRow[];
  queue_depth?: number;
  in_flight?: number;
  success_rate_24h?: number;
  avg_duration_ms?: number;
};

const STATUS_PILL: Record<ValidatorStatus, string> = {
  queued: "bg-[var(--color-muted)] text-[var(--color-muted-foreground)] ring-[var(--color-border)]",
  running: "bg-blue-500/15 text-blue-700 ring-blue-500/30 dark:text-blue-300",
  succeeded:
    "bg-emerald-500/15 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300",
  failed: "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
  timed_out: "bg-red-500/15 text-red-700 ring-red-500/30 dark:text-red-300",
};

function StatusPill({ s }: { s: ValidatorStatus }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest ring-1",
        STATUS_PILL[s],
      )}
    >
      {s === "running" && (
        <Loader2 className="h-2.5 w-2.5 animate-spin" aria-hidden />
      )}
      {s}
    </span>
  );
}

function KPICard({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
        {label}
      </p>
      <p className="mt-2 text-2xl font-semibold text-[var(--color-foreground)]">
        {value}
      </p>
      {hint && (
        <p className="mt-1 text-[11px] text-[var(--color-muted-foreground)]">
          {hint}
        </p>
      )}
    </div>
  );
}

function RunRow({ run }: { run: ValidatorRunRow }) {
  const [open, setOpen] = React.useState(false);
  const stdout = run.stdout_head?.trim();
  const stderr = run.stderr_head?.trim();
  const hasOutput = Boolean(stdout || stderr || run.error);
  return (
    <>
      <tr
        className={cn(
          "border-t border-[var(--color-border)]",
          open ? "bg-[var(--color-muted)]/30" : "hover:bg-[var(--color-muted)]/20",
        )}
      >
        <td className="px-3 py-2">
          {hasOutput ? (
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={open}
              aria-label={open ? "Collapse" : "Expand"}
              className="inline-flex h-6 w-6 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
            >
              {open ? (
                <ChevronDown className="h-3 w-3" />
              ) : (
                <ChevronRight className="h-3 w-3" />
              )}
            </button>
          ) : null}
        </td>
        <td className="px-3 py-2 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {formatRelative(run.started_at)}
        </td>
        <td className="px-3 py-2 font-mono text-[11px] text-[var(--color-foreground)]">
          {run.patch_sha ? run.patch_sha.slice(0, 12) : "—"}
        </td>
        <td className="px-3 py-2">
          <StatusPill s={run.status} />
        </td>
        <td className="px-3 py-2 font-mono text-[11px] text-[var(--color-muted-foreground)]">
          {formatDurationMs(run.duration_ms)}
        </td>
        <td className="px-3 py-2 font-mono text-[10px] text-[var(--color-muted-foreground)]">
          {run.workflow_run_id ? run.workflow_run_id.slice(0, 8) : "—"}
        </td>
      </tr>
      {open && hasOutput && (
        <tr className="border-t border-[var(--color-border)] bg-[var(--color-background)]">
          <td colSpan={6} className="px-3 py-3">
            {run.error && (
              <p className="mb-2 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
                {run.error}
              </p>
            )}
            {stdout && (
              <div className="mb-2">
                <p className="mb-1 text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
                  stdout (head)
                </p>
                <pre className="overflow-x-auto rounded-md bg-[var(--color-muted)]/40 p-3 text-[11px] font-mono">
                  {stdout}
                </pre>
              </div>
            )}
            {stderr && (
              <div>
                <p className="mb-1 text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
                  stderr (head)
                </p>
                <pre className="overflow-x-auto rounded-md bg-[var(--color-muted)]/40 p-3 text-[11px] font-mono">
                  {stderr}
                </pre>
              </div>
            )}
          </td>
        </tr>
      )}
    </>
  );
}

function segmentsFor(rows: ValidatorRunRow[]): OperationalSegment[] {
  return rows.slice(0, 10).map((r) => {
    const status: SegmentStatus =
      r.status === "succeeded"
        ? "succeeded"
        : r.status === "failed" || r.status === "timed_out"
          ? "failed"
          : r.status === "running"
            ? "running"
            : "pending";
    return {
      label: `Validator · ${r.patch_sha?.slice(0, 8) ?? r.id.slice(0, 8)}`,
      started_at: r.started_at,
      finished_at: r.finished_at,
      duration_ms: r.duration_ms,
      status,
      detail:
        r.error ??
        (r.status === "succeeded"
          ? "Patch validated cleanly."
          : r.status === "failed"
            ? r.stderr_head ?? "Validator reported failure."
            : r.status === "running"
              ? "Sandbox in progress."
              : "Awaiting sandbox slot."),
    };
  });
}

export function ValidatorClient({
  initial,
}: {
  initial: ValidatorListResp | null;
}) {
  if (initial === null) {
    return (
      <div className="space-y-6">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Fleet
          </p>
          <h1 className="text-2xl font-semibold">Validator sandbox</h1>
          <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
            Sandbox queue + recent execution runs from the Validator agent.
          </p>
        </div>
        <EmptyState
          icon={AlertOctagon}
          title="Validator stats unavailable"
          description="The control-plane endpoint /v1/validator/runs isn't online yet. Sandbox data will populate once the backend ships this surface."
        />
      </div>
    );
  }

  const runs = initial.runs ?? [];
  const successRate =
    typeof initial.success_rate_24h === "number"
      ? `${Math.round(initial.success_rate_24h * 100)}%`
      : "—";
  const avgDuration =
    typeof initial.avg_duration_ms === "number"
      ? formatDurationMs(initial.avg_duration_ms)
      : "—";

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Fleet
        </p>
        <h1 className="text-2xl font-semibold">Validator sandbox</h1>
        <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
          Sandbox queue depth and the 20 most recent validator runs.
        </p>
      </div>

      <section
        aria-labelledby="validator-kpi-heading"
        className="grid grid-cols-2 gap-3 lg:grid-cols-4"
      >
        <h2 id="validator-kpi-heading" className="sr-only">
          Validator KPIs
        </h2>
        <KPICard
          label="Queue depth"
          value={String(initial.queue_depth ?? 0)}
          hint="Patches waiting for a slot"
        />
        <KPICard
          label="In flight"
          value={String(initial.in_flight ?? 0)}
          hint="Sandboxes running now"
        />
        <KPICard
          label="Success rate (24h)"
          value={successRate}
          hint="succeeded / total"
        />
        <KPICard
          label="Avg duration"
          value={avgDuration}
          hint="Mean sandbox run"
        />
      </section>

      {runs.length === 0 ? (
        <EmptyState
          icon={Container}
          title="No validator runs yet"
          description="When the Validator activity fires on a recovery pipeline, runs will appear here with stdout / stderr previews."
        />
      ) : (
        <section
          aria-labelledby="validator-runs-heading"
          className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]"
        >
          <header className="border-b border-[var(--color-border)] px-5 py-3">
            <h2
              id="validator-runs-heading"
              className="text-sm font-semibold text-[var(--color-foreground)]"
            >
              Recent runs
            </h2>
          </header>
          <div className="overflow-x-auto">
          <table className="w-full min-w-[720px] text-sm">
            <thead className="bg-[var(--color-muted)]/40 text-left text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
              <tr>
                <th className="w-8 px-3 py-2" aria-label="Expand" />
                <th className="px-3 py-2 font-medium">Started</th>
                <th className="px-3 py-2 font-medium">Patch SHA</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Duration</th>
                <th className="px-3 py-2 font-medium">Workflow run</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((r) => (
                <RunRow key={r.id} run={r} />
              ))}
            </tbody>
          </table>
          </div>
        </section>
      )}

      <OperationalSegments
        title="Operational segments"
        description="Most recent 10 validator runs."
        segments={segmentsFor(runs)}
        emptyMessage="No segments to surface — validator hasn't run yet."
      />
    </div>
  );
}
